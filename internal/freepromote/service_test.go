package freepromote

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"easy_proxies/internal/config"
)

type fakeNodes struct {
	mu         sync.Mutex
	nodes      []config.NodeConfig
	reloads    int
	failCreate bool
	regions    map[string]string
	overrides  map[string]string
}

func (f *fakeNodes) ListConfigNodes(ctx context.Context) ([]config.NodeConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]config.NodeConfig, len(f.nodes))
	copy(out, f.nodes)
	return out, nil
}

func (f *fakeNodes) CreateNode(ctx context.Context, node config.NodeConfig) (config.NodeConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCreate {
		return config.NodeConfig{}, errors.New("create failed")
	}
	node.Source = config.NodeSourceFile
	if node.Port == 0 {
		node.Port = 34001
	}
	f.nodes = append(f.nodes, node)
	return node, nil
}

func (f *fakeNodes) DeleteNode(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, n := range f.nodes {
		if n.Name == name {
			f.nodes = append(f.nodes[:i], f.nodes[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

func (f *fakeNodes) TriggerReload(ctx context.Context) error {
	f.mu.Lock()
	f.reloads++
	f.mu.Unlock()
	return nil
}

func (f *fakeNodes) UpdateNodeRegion(ctx context.Context, name, region, country string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.regions == nil {
		f.regions = map[string]string{}
	}
	f.regions[name] = region + "|" + country
	return nil
}

func (f *fakeNodes) PersistRegionOverride(uri, region string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.overrides == nil {
		f.overrides = map[string]string{}
	}
	f.overrides[uri] = region
	return nil
}

type fakeSnaps struct {
	items []Snapshot
}

func (f fakeSnaps) ListSnapshots() []Snapshot { return f.items }

type fakeQuality struct {
	score  int
	ok     bool
	err    string
	calls  int
	exitIP string
	cfLoc  string
}

func (f *fakeQuality) Check(ctx context.Context, proxyURL, nodeName string) QualityResult {
	f.calls++
	return QualityResult{Score: f.score, OK: f.ok, Error: f.err, ExitIP: f.exitIP, CFLoc: f.cfLoc}
}

func TestRunOncePromotesAndKeepsOnQualityPass(t *testing.T) {
	nodes := &fakeNodes{nodes: []config.NodeConfig{
		{Name: "free-a", URI: "http://10.0.0.1:8080", Source: config.NodeSourceFreeProxy},
	}}
	snaps := fakeSnaps{items: []Snapshot{
		{Name: "free-a", URI: "http://10.0.0.1:8080", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, LastLatencyMs: 100},
	}}
	q := &fakeQuality{score: 90, ok: true}
	demote := true
	svc := NewService(
		func() config.FreeProxyPromoteConfig {
			return config.FreeProxyPromoteConfig{
				Enabled: true, BatchSize: 1, MaxPromoted: 5, MaxLatencyMS: 800, MinSuccessCount: 1,
				RequireCloudflare: true, MinCloudflareScore: 60, DemoteOnFail: &demote, MaxFailureCount: -1, RecentSuccessWithin: -1, NamePrefix: "free-promoted-",
			}
		},
		nodes, snaps, q,
		func() ListenAuth { return ListenAuth{Host: "127.0.0.1", Username: "u", Password: "p"} },
		nil,
	)
	// Avoid long settle in test by calling internal path with short sleep? RunOnce sleeps 5s.
	// Override: temporarily acceptable for correctness; reduce sleep for tests via env?
	// For unit speed, set a test hook - add settleWait field.
	svc.settleWait = 0
	svc.startupWait = 0
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.calls != 1 {
		t.Fatalf("quality calls=%d", q.calls)
	}
	list, _ := nodes.ListConfigNodes(context.Background())
	found := false
	for _, n := range list {
		if n.Source == config.NodeSourceFile && n.Port == 34001 {
			found = true
		}
		if n.Name == "free-a" {
			t.Fatal("free twin should be removed")
		}
	}
	if !found {
		t.Fatalf("promoted node missing: %#v", list)
	}
	if nodes.reloads < 1 {
		t.Fatal("expected reload")
	}
}

func TestRunOnceDemotesOnQualityFail(t *testing.T) {
	nodes := &fakeNodes{nodes: []config.NodeConfig{
		{Name: "free-a", URI: "http://10.0.0.2:8080", Source: config.NodeSourceFreeProxy},
	}}
	snaps := fakeSnaps{items: []Snapshot{
		{Name: "free-a", URI: "http://10.0.0.2:8080", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, LastLatencyMs: 100},
	}}
	q := &fakeQuality{score: 10, ok: false, err: "failed"}
	demote := true
	svc := NewService(
		func() config.FreeProxyPromoteConfig {
			return config.FreeProxyPromoteConfig{
				Enabled: true, BatchSize: 1, MaxPromoted: 5, MaxLatencyMS: 800, MinSuccessCount: 1,
				RequireCloudflare: true, MinCloudflareScore: 60, DemoteOnFail: &demote, MaxFailureCount: -1, RecentSuccessWithin: -1, NamePrefix: "free-promoted-",
			}
		},
		nodes, snaps, q,
		func() ListenAuth { return ListenAuth{Host: "127.0.0.1"} },
		nil,
	)
	svc.settleWait = 0
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, _ := nodes.ListConfigNodes(context.Background())
	for _, n := range list {
		if n.Source != config.NodeSourceFreeProxy {
			t.Fatalf("expected demotion, still have %#v", n)
		}
	}
}

func TestRunOnceFillsRegionFromCFLoc(t *testing.T) {
	nodes := &fakeNodes{nodes: []config.NodeConfig{
		{Name: "free-a", URI: "http://10.0.0.3:8080", Source: config.NodeSourceFreeProxy},
	}}
	snaps := fakeSnaps{items: []Snapshot{
		{Name: "free-a", URI: "http://10.0.0.3:8080", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, LastLatencyMs: 100, Region: "other", Country: "Unknown"},
	}}
	q := &fakeQuality{score: 90, ok: true, exitIP: "1.1.1.1", cfLoc: "US"}
	demote := true
	svc := NewService(
		func() config.FreeProxyPromoteConfig {
			return config.FreeProxyPromoteConfig{
				Enabled: true, BatchSize: 1, MaxPromoted: 5, MaxLatencyMS: 800, MinSuccessCount: 1,
				RequireCloudflare: true, MinCloudflareScore: 60, DemoteOnFail: &demote, MaxFailureCount: -1, RecentSuccessWithin: -1, NamePrefix: "free-promoted-",
			}
		},
		nodes, snaps, q,
		func() ListenAuth { return ListenAuth{Host: "127.0.0.1", Username: "u", Password: "p"} },
		nil,
	)
	svc.settleWait = 0
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(nodes.regions) != 1 {
		t.Fatalf("regions=%#v", nodes.regions)
	}
	for name, v := range nodes.regions {
		if got := v; len(got) < 3 || got[:2] != "us" {
			t.Fatalf("node %s region=%q want us|*", name, v)
		}
	}
	if len(nodes.overrides) != 1 {
		t.Fatalf("overrides=%#v", nodes.overrides)
	}
	for _, region := range nodes.overrides {
		if region != "us" {
			t.Fatalf("override=%q", region)
		}
	}
}

func TestRunOnceInheritsFreeRegionWithoutQuality(t *testing.T) {
	nodes := &fakeNodes{nodes: []config.NodeConfig{
		{Name: "free-a", URI: "http://10.0.0.4:8080", Source: config.NodeSourceFreeProxy},
	}}
	snaps := fakeSnaps{items: []Snapshot{
		{Name: "free-a", URI: "http://10.0.0.4:8080", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, LastLatencyMs: 100, Region: "jp", Country: "Japan"},
	}}
	demote := false
	svc := NewService(
		func() config.FreeProxyPromoteConfig {
			return config.FreeProxyPromoteConfig{
				Enabled: true, BatchSize: 1, MaxPromoted: 5, MaxLatencyMS: 800, MinSuccessCount: 1,
				RequireCloudflare: false, DemoteOnFail: &demote, MaxFailureCount: -1, RecentSuccessWithin: -1, NamePrefix: "free-promoted-",
			}
		},
		nodes, snaps, nil,
		func() ListenAuth { return ListenAuth{Host: "127.0.0.1"} },
		nil,
	)
	svc.settleWait = 0
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// without quality and without port after create fake, still sets region from candidate
	if len(nodes.regions) != 1 {
		t.Fatalf("regions=%#v", nodes.regions)
	}
	for _, v := range nodes.regions {
		if v != "jp|Japan" {
			t.Fatalf("region=%q", v)
		}
	}
}

func TestStartStaysAliveAndPromotesAfterConfigEnabled(t *testing.T) {
	var cfgMu sync.Mutex
	cfg := config.FreeProxyPromoteConfig{
		Enabled: false, BatchSize: 1, MaxPromoted: 5, MaxLatencyMS: 800, MinSuccessCount: 1,
		RequireCloudflare: false, MaxFailureCount: -1, RecentSuccessWithin: -1, NamePrefix: "free-promoted-",
	}
	nodes := &fakeNodes{nodes: []config.NodeConfig{
		{Name: "free-a", URI: "http://10.0.0.5:8080", Source: config.NodeSourceFreeProxy},
	}}
	snaps := fakeSnaps{items: []Snapshot{
		{Name: "free-a", URI: "http://10.0.0.5:8080", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, LastLatencyMs: 100, Region: "jp", Country: "Japan"},
	}}
	svc := NewService(
		func() config.FreeProxyPromoteConfig {
			cfgMu.Lock()
			defer cfgMu.Unlock()
			return cfg
		},
		nodes, snaps, nil,
		func() ListenAuth { return ListenAuth{Host: "127.0.0.1"} },
		nil,
	)
	svc.startupWait = 0
	svc.settleWait = 0
	svc.disabledPoll = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	time.Sleep(25 * time.Millisecond)
	if nodes.reloads != 0 {
		t.Fatalf("reload while disabled=%d", nodes.reloads)
	}

	cfgMu.Lock()
	cfg.Enabled = true
	cfgMu.Unlock()

	eventually(t, 500*time.Millisecond, func() bool {
		list, _ := nodes.ListConfigNodes(context.Background())
		return nodes.reloads == 1 && CountPromoted(list, "free-promoted-") == 1
	})
}

func TestStartIsIdempotentWhileWaitingDisabled(t *testing.T) {
	var cfgMu sync.Mutex
	cfg := config.FreeProxyPromoteConfig{
		Enabled: false, BatchSize: 1, MaxPromoted: 5, MaxLatencyMS: 800, MinSuccessCount: 1,
		RequireCloudflare: false, MaxFailureCount: -1, RecentSuccessWithin: -1, NamePrefix: "free-promoted-",
	}
	nodes := &fakeNodes{nodes: []config.NodeConfig{
		{Name: "free-a", URI: "http://10.0.0.6:8080", Source: config.NodeSourceFreeProxy},
	}}
	snaps := fakeSnaps{items: []Snapshot{
		{Name: "free-a", URI: "http://10.0.0.6:8080", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, LastLatencyMs: 100},
	}}
	svc := NewService(
		func() config.FreeProxyPromoteConfig {
			cfgMu.Lock()
			defer cfgMu.Unlock()
			return cfg
		},
		nodes, snaps, nil,
		func() ListenAuth { return ListenAuth{Host: "127.0.0.1"} },
		nil,
	)
	svc.startupWait = 0
	svc.settleWait = 0
	svc.disabledPoll = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	svc.Start(ctx)

	cfgMu.Lock()
	cfg.Enabled = true
	cfgMu.Unlock()

	eventually(t, 500*time.Millisecond, func() bool {
		return nodes.reloads == 1
	})
}

func TestRunOncePostValidateDemotesWithoutCloudflareRequirement(t *testing.T) {
	nodes := &fakeNodes{nodes: []config.NodeConfig{
		{Name: "free-a", URI: "http://10.0.0.7:8080", Source: config.NodeSourceFreeProxy},
	}}
	snaps := fakeSnaps{items: []Snapshot{
		{Name: "free-a", URI: "http://10.0.0.7:8080", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, LastLatencyMs: 100},
	}}
	q := &fakeQuality{ok: false, err: "bad proxy"}
	demote := true
	svc := NewService(
		func() config.FreeProxyPromoteConfig {
			return config.FreeProxyPromoteConfig{
				Enabled: true, BatchSize: 1, MaxPromoted: 5, MaxLatencyMS: 800, MinSuccessCount: 1,
				RequireCloudflare: false, DemoteOnFail: &demote, MaxFailureCount: -1, RecentSuccessWithin: -1, NamePrefix: "free-promoted-",
			}
		},
		nodes, snaps, q,
		func() ListenAuth { return ListenAuth{Host: "127.0.0.1"} },
		nil,
	)
	svc.settleWait = 0
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, _ := nodes.ListConfigNodes(context.Background())
	for _, n := range list {
		if n.Source != config.NodeSourceFreeProxy {
			t.Fatalf("expected post-validate demotion, still have %#v", n)
		}
	}
	if nodes.reloads != 2 {
		t.Fatalf("reloads=%d want promote+batch-demote", nodes.reloads)
	}
}

func TestRunOnceSkipsCooledDownCandidate(t *testing.T) {
	nodes := &fakeNodes{nodes: []config.NodeConfig{
		{Name: "free-a", URI: "http://10.0.0.8:8080", Source: config.NodeSourceFreeProxy},
	}}
	snaps := fakeSnaps{items: []Snapshot{
		{Name: "free-a", URI: "http://10.0.0.8:8080", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, LastLatencyMs: 100},
	}}
	demote := true
	svc := NewService(
		func() config.FreeProxyPromoteConfig {
			return config.FreeProxyPromoteConfig{
				Enabled: true, BatchSize: 1, MaxPromoted: 5, MaxLatencyMS: 800, MinSuccessCount: 1,
				RequireCloudflare: false, DemoteOnFail: &demote, MaxFailureCount: -1, RecentSuccessWithin: -1, FailedCooldown: time.Hour, NamePrefix: "free-promoted-",
			}
		},
		nodes, snaps, &fakeQuality{ok: false, err: "bad proxy"},
		func() ListenAuth { return ListenAuth{Host: "127.0.0.1"} },
		nil,
	)
	svc.settleWait = 0
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if nodes.reloads != 2 {
		t.Fatalf("reloads=%d want second run skipped by cooldown", nodes.reloads)
	}
}

func TestRunOnceDemotesStalePromotedBeforeSelecting(t *testing.T) {
	promoted := PromotedNodeName("free-promoted-", "http://10.0.0.9:8080")
	nodes := &fakeNodes{nodes: []config.NodeConfig{
		{Name: promoted, URI: "http://10.0.0.9:8080#" + promoted, Source: config.NodeSourceFile},
		{Name: "free-b", URI: "http://10.0.0.10:8080", Source: config.NodeSourceFreeProxy},
	}}
	snaps := fakeSnaps{items: []Snapshot{
		{Name: promoted, URI: "http://10.0.0.9:8080#" + promoted, Source: "nodes_file", Available: false, InitialCheckDone: true, FailureCount: 3},
		{Name: "free-b", URI: "http://10.0.0.10:8080", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, LastLatencyMs: 100, Region: "jp", Country: "Japan"},
	}}
	demote := true
	svc := NewService(
		func() config.FreeProxyPromoteConfig {
			return config.FreeProxyPromoteConfig{
				Enabled: true, BatchSize: 1, MaxPromoted: 1, MaxLatencyMS: 800, MinSuccessCount: 1,
				RequireCloudflare: false, DemoteOnFail: &demote, MaxFailureCount: 1, RecentSuccessWithin: -1, NamePrefix: "free-promoted-",
			}
		},
		nodes, snaps, nil,
		func() ListenAuth { return ListenAuth{Host: "127.0.0.1"} },
		nil,
	)
	svc.settleWait = 0
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, _ := nodes.ListConfigNodes(context.Background())
	if CountPromoted(list, "free-promoted-") != 1 {
		t.Fatalf("promoted count=%d list=%#v", CountPromoted(list, "free-promoted-"), list)
	}
	for _, n := range list {
		if n.Name == promoted {
			t.Fatalf("stale promoted still present: %#v", list)
		}
	}
}

func eventually(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ok() {
		t.Fatal("condition not met before timeout")
	}
}

func TestResolvePromotedRegionPrefersCFLoc(t *testing.T) {
	gotR, gotC := resolvePromotedRegion(Candidate{Region: "jp", Country: "Japan"}, QualityResult{CFLoc: "US", ExitIP: "1.1.1.1"}, true)
	if gotR != "us" {
		t.Fatalf("region=%q country=%q", gotR, gotC)
	}
}
