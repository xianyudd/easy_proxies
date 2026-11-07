package boxmgr

// START-1B regression tests for the port-mapping / assignment layer.
// The deployment evidence (2026-11-08) showed:
//   - waitForListenPortsReleased never complained while the SAME node collided
//     with port 24034 on every reload and was reassigned 24034->24035 each time;
//   - after reload the same process listened on both old and new ports;
//   - the reassignment (a write into the in-memory newCfg) was not survivable:
//     the next reload's port map came from a stale copy that still carried the
//     pre-move port.

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
)

func portSet(ports []listenPort) map[uint16]bool {
	out := make(map[uint16]bool, len(ports))
	for _, p := range ports {
		out[p.port] = true
	}
	return out
}

// TestConfigListenPortsCoversAllNodePortsIncludingFreeProxy asserts the
// wait/precondition set covers every node with a real Port != 0 in
// multi-port/hybrid mode. Free-proxy pool nodes are skipped by the builder's
// per-node inbound loop, but if a config gives one a Port it must still be
// waited on: waiting on an extra free port costs nothing, missing one real
// listener port is exactly what caused silent port relocation in production.
func TestConfigListenPortsCoversAllNodePortsIncludingFreeProxy(t *testing.T) {
	cfg := &config.Config{
		Mode:     "hybrid",
		Listener: config.ListenerConfig{Address: "127.0.0.1", Port: 2323, Username: "u", Password: "p"},
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: 30200,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{
			{Name: "inline", URI: "http://127.0.0.1:1", Port: 30234, Source: config.NodeSourceInline},
			{Name: "sub", URI: "http://127.0.0.1:2", Port: 30236, Source: config.NodeSourceSubscription},
			{Name: "free-with-port", URI: "http://127.0.0.1:3", Port: 30300, Source: config.NodeSourceFreeProxy},
			{Name: "free-no-port", URI: "http://127.0.0.1:4", Port: 0, Source: config.NodeSourceFreeProxy},
		},
	}
	got := portSet(configListenPorts(cfg))
	for _, want := range []uint16{2323, 30234, 30236, 30300} {
		if !got[want] {
			t.Fatalf("configListenPorts missing port %d (got %v); the wait set must cover every bound port", want, got)
		}
	}
	if got[0] {
		t.Fatalf("configListenPorts contains port 0 for a free-proxy node without a port")
	}
}

// TestReloadWithStalePortMapKeepsReassignedPort reproduces the app-side hot
// reload contract: the caller hands ReloadWithPortMap a port map built from a
// stale config copy (the app holds the original startup config, which never
// learns about runtime reassignment). The manager must treat its own current
// port table as authoritative so a moved port (30240->30241) survives the next
// reload instead of reverting to 30240 and re-triggering the conflict.
func TestReloadWithStalePortMapKeepsReassignedPort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	key := "http://127.0.0.1:1"
	firstPort, secondPort := uint16(30240), uint16(30241)

	initCfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: firstPort,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{{
			Name: "node", URI: key, Port: firstPort, Source: config.NodeSourceInline,
		}},
	}
	initCfg.SetFilePath(t.TempDir() + "/config.yaml")

	mgr := New(initCfg, monitor.Config{})

	// Shared box for every reload build. Its Start fails while the config still
	// carries 30240: 4 failures exhaust the transient retry budget and force the
	// reassignment path; the 5th build (with 30241) succeeds.
	box := &recordingConflictBox{conflictPort: firstPort}
	var builtPort uint16      // port the last build shipped to the box
	var firstBuiltPort uint16 // port of the first build of the current phase
	var captureFirst bool     // set before reload 2 to capture its first build
	created := 0

	mgr.boxFactory = func(_ context.Context, c *config.Config) (boxInstance, error) {
		if c == nil || len(c.Nodes) == 0 {
			return nil, errConfigUnavailable
		}
		created++
		if created == 1 {
			// 初始实例：不想触发冲突路径，直接成功。
			return &blockingBox{}, nil
		}
		builtPort = c.Nodes[0].Port
		if captureFirst {
			firstBuiltPort = builtPort
			captureFirst = false
		}
		box.setTarget(builtPort)
		return box, nil
	}

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// First reload: node still on 30240, port held by the old generation -> WARN
	// x3, retry budget exhausted, reassign to 30241. This is the v1-confirmed
	// path and must keep working.
	reloadCfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: firstPort,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{{
			Name: "node", URI: key, Port: firstPort, Source: config.NodeSourceInline,
		}},
	}
	reloadCfg.SetFilePath(t.TempDir() + "/config.yaml")
	box.reset()
	if err := mgr.ReloadWithPortMap(reloadCfg, map[string]uint16{key: firstPort}); err != nil {
		t.Fatalf("ReloadWithPortMap (first, conflicting): %v", err)
	}
	if got := box.startCount(); got != 5 {
		t.Fatalf("first reload started the box %d times; want 4 transient retries + 1 reassigned build", got)
	}
	if got := mgr.CurrentPortMap(); got[key] != secondPort {
		t.Fatalf("CurrentPortMap after reassign = %d for %q, want %d", got[key], key, secondPort)
	}

	// Second reload simulates the app's hot-reload: fresh config (node has no
	// port yet) plus a STALE map carrying the pre-move port. The manager must
	// merge its current table (30241) over the stale entry (30240) and build
	// with 30241, starting cleanly on the very first attempt.
	captureFirst = true
	box.reset()
	freshCfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: firstPort,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{{
			Name: "node", URI: key, Port: 0, Source: config.NodeSourceInline,
		}},
	}
	if err := mgr.ReloadWithPortMap(freshCfg, map[string]uint16{key: firstPort}); err != nil {
		t.Fatalf("ReloadWithPortMap (second, stale map): %v", err)
	}
	if got := box.startCount(); got != 1 {
		t.Fatalf("second reload started the box %d times; a preserved port must build cleanly once", got)
	}
	if firstBuiltPort != secondPort {
		t.Fatalf("second reload built the box with port %d, want preserved reassigned port %d (stale map reverted the move)",
			firstBuiltPort, secondPort)
	}
	if got := mgr.CurrentPortMap()[key]; got != secondPort {
		t.Fatalf("CurrentPortMap after second reload = %d, want %d", got, secondPort)
	}
	if builtPort != secondPort {
		t.Fatalf("final box port = %d, want %d; the moved port must survive", builtPort, secondPort)
	}
}

// TestReloadCycleWithDuplicateURIStopsColliding reproduces the deployment's
// signature replay: two subscription entries with the same canonical URI (name
// differs only in the fragment) collapse onto one port via NormalizeWithPortMap,
// so the new sing-box config binds the same port twice, Start keeps failing,
// and reassignment only moves one copy - the other stays on the port, the box
// then listens on BOTH old and new ports, and the next reload repeats it.
// After the fix the config must never contain duplicate listen ports, so the
// reload builds once, starts immediately, and stays conflict-free on the next
// round using the manager's current map.
func TestReloadCycleWithDuplicateURIStopsColliding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	key := "vless://a@example.com:443"
	base := uint16(30250)

	makeDupCfg := func() *config.Config {
		c := &config.Config{
			Mode: "multi-port",
			MultiPort: config.MultiPortConfig{
				Address:  "127.0.0.1",
				BasePort: base,
				Username: "u",
				Password: "p",
			},
			Nodes: []config.NodeConfig{
				{Name: "Singapore", URI: key + "#Singapore"},
				{Name: "Singapore3", URI: key + "#Singapore3"},
			},
		}
		c.SetFilePath(t.TempDir() + "/config.yaml")
		return c
	}

	initCfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: base,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{{
			Name: "Singapore", URI: key, Port: base, Source: config.NodeSourceInline,
		}},
	}
	initCfg.SetFilePath(t.TempDir() + "/config.yaml")

	mgr := New(initCfg, monitor.Config{})

	// dupCheckBox fails Start while c still contains two nodes on one port -
	// the exact sing-box failure (two inbounds on the same listen port).
	dupCheckBox := &dupPortBox{}
	mgr.boxFactory = func(_ context.Context, c *config.Config) (boxInstance, error) {
		if c == nil || len(c.Nodes) == 0 {
			return nil, errConfigUnavailable
		}
		dupCheckBox.setConfig(c)
		return dupCheckBox, nil
	}

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	run := func(tag string, portMap map[string]uint16, cfg *config.Config) {
		dupCheckBox.reset()
		if err := mgr.ReloadWithPortMap(cfg, portMap); err != nil {
			t.Fatalf("%s reload: %v", tag, err)
		}
		if dupCheckBox.starts != 1 {
			t.Fatalf("%s reload started the box %d times; duplicate ports must be resolved before build", tag, dupCheckBox.starts)
		}
		if cfg.Nodes[0].Port == cfg.Nodes[1].Port {
			t.Fatalf("%s reload left duplicate URIs on the same port %d", tag, cfg.Nodes[0].Port)
		}
	}

	// Round 1: a stale map (like any copy that predates runtime moves) forces
	// the preserved port onto the key. The duplicated URI must not end up with
	// two nodes on that port.
	run("round1", map[string]uint16{key: base}, makeDupCfg())

	// Round 2: the subscription manager's flow - fresh nodes, current map.
	// Must also build once and keep distinct ports.
	run("round2", mgr.CurrentPortMap(), makeDupCfg())
}

// dupPortBox fails Start whenever its captured config still has two nodes
// sharing one listen port; otherwise it starts successfully.
type dupPortBox struct {
	mu     sync.Mutex
	cfg    *config.Config
	starts int
}

func (b *dupPortBox) setConfig(c *config.Config) {
	b.mu.Lock()
	b.cfg = c
	b.mu.Unlock()
}

func (b *dupPortBox) reset() {
	b.mu.Lock()
	b.starts = 0
	b.mu.Unlock()
}

func (b *dupPortBox) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.starts++
	seen := make(map[uint16]string)
	for _, n := range b.cfg.Nodes {
		if n.Port == 0 {
			continue
		}
		if _, ok := seen[n.Port]; ok {
			return &errBind{port: n.Port}
		}
		seen[n.Port] = n.Name
	}
	return nil
}

func (b *dupPortBox) Close() error { return nil }

type errBind struct{ port uint16 }

func (e *errBind) Error() string {
	return fmt.Sprintf("listen tcp4 127.0.0.1:%d: bind: address already in use", e.port)
}

// recordingConflictBox fails Start whenever the manager asks it to bind
// conflictPort (i.e. the config still carries the port that a stale holder
// has), and succeeds once the config moved off it. startCount lets tests
// observe exactly how many build attempts the manager made.
type recordingConflictBox struct {
	mu           sync.Mutex
	starts       int
	conflictPort uint16
	target       uint16
}

func (b *recordingConflictBox) setTarget(p uint16) {
	b.mu.Lock()
	b.target = p
	b.mu.Unlock()
}

func (b *recordingConflictBox) reset() {
	b.mu.Lock()
	b.starts = 0
	b.mu.Unlock()
}

func (b *recordingConflictBox) startCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.starts
}

func (b *recordingConflictBox) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.starts++
	if b.target == b.conflictPort {
		return fmt.Errorf("listen tcp4 127.0.0.1:%d: bind: address already in use", b.conflictPort)
	}
	return nil
}

func (b *recordingConflictBox) Close() error { return nil }

// TestReassignConflictingPortMovesEveryNodeOnThePort guards the duplicate-
// recovery path: when two nodes hold the same port (duplicate URI collapse, or
// a hand-written conflict), reassignment must move ALL of them to distinct
// ports, not just the first match, or the box keeps failing on the leftover.
func TestReassignConflictingPortMovesEveryNodeOnThePort(t *testing.T) {
	cfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: 30260,
		},
		Nodes: []config.NodeConfig{
			{Name: "a", URI: "http://127.0.0.1:1", Port: 30260, Source: config.NodeSourceInline},
			{Name: "b", URI: "http://127.0.0.1:2", Port: 30260, Source: config.NodeSourceInline},
			{Name: "c", URI: "http://127.0.0.1:3", Port: 30262, Source: config.NodeSourceInline},
		},
	}
	if !reassignConflictingPort(cfg, 30260) {
		t.Fatal("reassignConflictingPort reported nothing to move")
	}
	for _, n := range cfg.Nodes {
		if n.Port == 30260 {
			t.Fatalf("node %q still on the conflicting port 30260", n.Name)
		}
		if n.Name == "b" && n.Port == cfg.Nodes[0].Port {
			t.Fatalf("duplicate nodes moved to the same port %d", n.Port)
		}
	}
}

// TestNormalizeWithPortMapKeepsDuplicateURIsOnDistinctPorts verifies the
// normalization layer itself never produces two nodes on one listen port: a
// portMap-preserved port already taken by an earlier node in the same config
// (hybrid pool port or a duplicated URI) must be left for fresh assignment.
func TestNormalizeWithPortMapKeepsDuplicateURIsOnDistinctPorts(t *testing.T) {
	cfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: 30270,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{
			{Name: "dup-one", URI: "vless://a@example.com:443#one"},
			{Name: "dup-two", URI: "vless://a@example.com:443#two"},
		},
	}
	cfg.SetFilePath(t.TempDir() + "/config.yaml")
	if err := cfg.NormalizeWithPortMap(map[string]uint16{"vless://a@example.com:443": 30270}); err != nil {
		t.Fatalf("NormalizeWithPortMap: %v", err)
	}
	if cfg.Nodes[0].Port == cfg.Nodes[1].Port {
		t.Fatalf("duplicate URIs collapsed onto the same port %d; sing-box would bind it twice and fail", cfg.Nodes[0].Port)
	}
	if cfg.Nodes[0].Port != 30270 && cfg.Nodes[1].Port != 30270 {
		t.Fatalf("preserved port 30270 was lost entirely; ports = %d, %d", cfg.Nodes[0].Port, cfg.Nodes[1].Port)
	}
}
