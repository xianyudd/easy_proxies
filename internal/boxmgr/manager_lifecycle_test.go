package boxmgr

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
)

// blockingBox is a controllable fake box: Start/Close can be gated on channels
// so tests can hold a reload or a startup at a precise lifecycle point.
type blockingBox struct {
	mu sync.Mutex

	startBlock chan struct{} // non-nil: Start waits until closed
	closeBlock chan struct{} // non-nil: Close waits until closed

	startEntered chan struct{} // closed when Start is invoked
	closeEntered chan struct{} // closed when Close is invoked

	started  bool
	closed   bool
	closeErr error
}

func (b *blockingBox) Start() error {
	if b.startEntered != nil {
		close(b.startEntered)
	}
	if b.startBlock != nil {
		<-b.startBlock
	}
	b.mu.Lock()
	b.started = true
	b.mu.Unlock()
	return nil
}

func (b *blockingBox) Close() error {
	if b.closeEntered != nil {
		close(b.closeEntered)
	}
	if b.closeBlock != nil {
		<-b.closeBlock
	}
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	return b.closeErr
}

func (b *blockingBox) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *blockingBox) isStarted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.started
}

// lifecycleTestCfg returns a valid multi-port config for lifecycle tests.
func lifecycleTestCfg(name string, port uint16) *config.Config {
	cfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: port,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{{
			Name:   name,
			URI:    "http://127.0.0.1:1",
			Port:   port,
			Source: config.NodeSourceInline,
		}},
	}
	path, err := os.MkdirTemp("", "boxmgr-lifecycle-*")
	if err != nil {
		panic(err)
	}
	cfg.SetFilePath(filepath.Join(path, "config.yaml"))
	return cfg
}

// TestReloadDuringStartWaitsForStartAndDoesNotReportNotStarted ensures a reload
// that races the tail of Start must wait for startup to finish and then succeed,
// instead of observing currentBox==nil and failing with "manager not started".
func TestReloadDuringStartWaitsForStartAndDoesNotReportNotStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initBox := &blockingBox{startBlock: make(chan struct{}), startEntered: make(chan struct{})}
	reloadBox := &blockingBox{}
	mgr := New(lifecycleTestCfg("first", 30000), monitor.Config{})
	createdN := 0
	mgr.boxFactory = func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		if cfg == nil {
			return nil, errors.New("config is nil")
		}
		createdN++
		if createdN == 1 {
			return initBox, nil
		}
		return reloadBox, nil
	}

	startDone := make(chan error, 1)
	go func() { startDone <- mgr.Start(ctx) }()
	<-initBox.startEntered // Start is now blocked inside instance.Start()

	newCfg := lifecycleTestCfg("second", 30001)
	reloadErr := make(chan error, 1)
	go func() { reloadErr <- mgr.Reload(newCfg) }()

	// A reload racing a still-starting manager must not bail out early.
	select {
	case err := <-reloadErr:
		t.Fatalf("Reload while Start was in progress returned %v; want it to wait for startup and succeed", err)
	case <-time.After(300 * time.Millisecond):
		// Still waiting, as required.
	}

	close(initBox.startBlock)
	if err := <-startDone; err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := <-reloadErr; err != nil {
		t.Fatalf("Reload after waiting for Start failed: %v", err)
	}
}

// TestConcurrentReloadsAreSerializedAndNeverReportNotStarted drives two reloads
// that overlap: the second must be serialized behind the first and succeed,
// never failing with "manager not started" because currentBox was temporarily
// cleared by the first.
func TestConcurrentReloadsAreSerializedAndNeverReportNotStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := &blockingBox{startBlock: make(chan struct{}), startEntered: make(chan struct{})}
	mgr := New(lifecycleTestCfg("initial", 30002), monitor.Config{})
	var factoryMu sync.Mutex
	boxCount := 0
	mgr.boxFactory = func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		if cfg == nil {
			return nil, errors.New("config is nil")
		}
		factoryMu.Lock()
		boxCount++
		n := boxCount
		factoryMu.Unlock()
		switch n {
		case 1:
			return &blockingBox{}, nil
		case 2:
			// First reload's new instance: its Start is gated so the second
			// reload provably overlaps the first.
			return first, nil
		default:
			// Second reload must get a fresh instance, never "manager not started".
			return &blockingBox{}, nil
		}
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	reloadCfgA := lifecycleTestCfg("a", 30003)
	reloadCfgB := lifecycleTestCfg("b", 30004)
	firstErr := make(chan error, 1)
	secondErr := make(chan error, 1)

	go func() { firstErr <- mgr.Reload(reloadCfgA) }()
	<-first.startEntered // first reload is now mid-build (old box already closed)

	go func() { secondErr <- mgr.Reload(reloadCfgB) }()

	// The second reload must not fail while the first is in flight.
	select {
	case err := <-secondErr:
		t.Fatalf("second Reload overlapped an active reload and returned %v; want serialization", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(first.startBlock)
	if err := <-firstErr; err != nil {
		t.Fatalf("first Reload failed: %v", err)
	}
	if err := <-secondErr; err != nil {
		t.Fatalf("second Reload failed after serialization: %v", err)
	}
}

// TestCloseDuringReloadDoesNotLeakRunningBox verifies that a Close arriving
// while a reload is mid-build ends with every created box closed: no instance
// may keep running after Close returns and the manager must end in a closed
// state rather than assigning the new box post-Close.
func TestCloseDuringReloadDoesNotLeakRunningBox(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initialBox := &blockingBox{}
	inFlight := &blockingBox{startBlock: make(chan struct{}), startEntered: make(chan struct{})}
	mgr := New(lifecycleTestCfg("initial", 30005), monitor.Config{})
	created := make([]*blockingBox, 0, 2)
	var createdMu sync.Mutex
	createdN := 0
	mgr.boxFactory = func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		if cfg == nil {
			return nil, errors.New("config is nil")
		}
		createdMu.Lock()
		defer createdMu.Unlock()
		createdN++
		box := initialBox
		if createdN > 1 {
			box = inFlight
		}
		created = append(created, box)
		return box, nil
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	reloadDone := make(chan error, 1)
	go func() { reloadDone <- mgr.Reload(lifecycleTestCfg("new", 30006)) }()
	<-inFlight.startEntered // reload is mid-build, holding the lifecycle

	closeDone := make(chan error, 1)
	go func() { closeDone <- mgr.Close() }()

	// Deterministic ordering: release the reload, let both finish.
	time.Sleep(100 * time.Millisecond)
	close(inFlight.startBlock)

	if err := <-reloadDone; err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	createdMu.Lock()
	defer createdMu.Unlock()
	for i, box := range created {
		if box.isStarted() && !box.isClosed() {
			t.Fatalf("box %d was started but never closed: leaked running instance after Close", i)
		}
	}
}

// TestReloadClosesOldBoxBeforeBuildingNewBox is a regression guard: the new
// box must never be constructed before the old one is closed.
func TestReloadClosesOldBoxBeforeBuildingNewBox(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oldBox := &blockingBox{closeBlock: make(chan struct{}), closeEntered: make(chan struct{})}
	newBox := &blockingBox{startEntered: make(chan struct{})}
	mgr := New(lifecycleTestCfg("initial", 30007), monitor.Config{})
	created := false
	mgr.boxFactory = func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		if cfg == nil {
			return nil, errors.New("config is nil")
		}
		if created {
			return newBox, nil
		}
		created = true
		return oldBox, nil
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	reloadDone := make(chan error, 1)
	go func() { reloadDone <- mgr.Reload(lifecycleTestCfg("new", 30008)) }()
	<-oldBox.closeEntered // old box close has begun and is blocked

	select {
	case <-newBox.startEntered:
		t.Fatal("new box started before old box finished closing")
	case <-reloadDone:
		t.Fatal("reload completed while old box Close was still blocked")
	case <-time.After(150 * time.Millisecond):
		// Correctly waiting on the old box Close.
	}

	close(oldBox.closeBlock)
	if err := <-reloadDone; err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
}

// TestReloadBeforeStartReturnsErrManagerNotStarted ensures a reload on a
// manager that never started gets an explicit, typed not-started error.
func TestReloadBeforeStartReturnsErrManagerNotStarted(t *testing.T) {
	mgr := New(lifecycleTestCfg("initial", 30100), monitor.Config{})
	err := mgr.Reload(lifecycleTestCfg("new", 30101))
	if !errors.Is(err, ErrManagerNotStarted) {
		t.Fatalf("Reload before Start = %v, want ErrManagerNotStarted", err)
	}
	if got := mgr.State(); got != StateNotStarted {
		t.Fatalf("State() = %v, want not_started", got)
	}
}

// TestClosedManagerRejectsReload verifies Close ends the lifecycle and reloads
// after Close fail with a typed closed error and never build a box.
func TestClosedManagerRejectsReload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := New(lifecycleTestCfg("initial", 30102), monitor.Config{})
	built := 0
	mgr.boxFactory = func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		built++
		return &blockingBox{}, nil
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := mgr.State(); got != StateClosed {
		t.Fatalf("State() after Close = %v, want closed", got)
	}
	builtBefore := built
	err := mgr.Reload(lifecycleTestCfg("new", 30103))
	if !errors.Is(err, ErrBoxManagerClosed) {
		t.Fatalf("Reload after Close = %v, want ErrBoxManagerClosed", err)
	}
	if built != builtBefore {
		t.Fatalf("Reload after Close built a box; built count = %d", built)
	}
}

// TestStartFailureLeavesManagerNotStarted ensures a failed Start leaves a
// clearly observable not-started state instead of a silent half-started one.
func TestStartFailureLeavesManagerNotStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := New(lifecycleTestCfg("initial", 30104), monitor.Config{})
	mgr.boxFactory = func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		return nil, errors.New("boom: cannot create box")
	}
	if err := mgr.Start(ctx); err == nil {
		t.Fatal("Start succeeded, want failure")
	}
	if got := mgr.State(); got != StateNotStarted {
		t.Fatalf("State() after failed Start = %v, want not_started", got)
	}
	if err := mgr.Reload(lifecycleTestCfg("new", 30105)); !errors.Is(err, ErrManagerNotStarted) {
		t.Fatalf("Reload after failed Start = %v, want ErrManagerNotStarted", err)
	}
}

// TestFailedReloadAndRollbackLeavesNotStarted verifies that when both the new
// box build and the rollback fail, the manager lands in a deterministic
// not-started state and reloads afterwards report ErrManagerNotStarted.
func TestFailedReloadAndRollbackLeavesNotStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := New(lifecycleTestCfg("initial", 30106), monitor.Config{})
	built := 0
	mgr.boxFactory = func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		built++
		if built == 1 {
			return &blockingBox{}, nil
		}
		return nil, errors.New("boom: every rebuild fails")
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	err := mgr.Reload(lifecycleTestCfg("new", 30107))
	if err == nil {
		t.Fatal("Reload succeeded, want failure")
	}
	if got := mgr.State(); got != StateNotStarted {
		t.Fatalf("State() after failed reload+rollback = %v, want not_started", got)
	}
	if err := mgr.Reload(lifecycleTestCfg("again", 30108)); !errors.Is(err, ErrManagerNotStarted) {
		t.Fatalf("subsequent Reload = %v, want ErrManagerNotStarted", err)
	}
}

// TestReloadAbortsOnCancelledBaseContext verifies cancel semantics: when the
// startup context is already cancelled, Reload refuses to rebuild, leaves the
// old instance in place and restores StateRunning instead of tearing anything
// down or erroring with a misleading message.
func TestReloadAbortsOnCancelledBaseContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mgr := New(lifecycleTestCfg("initial", 30109), monitor.Config{})
	built := 0
	mgr.boxFactory = func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		built++
		return &blockingBox{}, nil
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel() // 模拟进程退出：启动上下文取消

	builtBefore := built
	err := mgr.Reload(lifecycleTestCfg("new", 30110))
	if err == nil {
		t.Fatal("Reload with cancelled base context succeeded, want error")
	}
	if built != builtBefore {
		t.Fatalf("Reload built a new box despite cancelled base context; built=%d", built)
	}
	if got := mgr.State(); got != StateRunning {
		t.Fatalf("State() after cancelled-context reload = %v, want running (old box kept)", got)
	}
}

// TestWaitForListenPortsReleasedBlocksUntilPortFrees ensures the port-release
// precondition actually waits for the OS-level bindability instead of sleeping
// a fixed amount or pretending the port is free while it is still held.
func TestWaitForListenPortsReleasedBlocksUntilPortFrees(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind test listener: %v", err)
	}
	defer ln.Close()
	port := uint16(ln.Addr().(*net.TCPAddr).Port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: port,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{{
			Name:   "held",
			URI:    "http://127.0.0.1:1",
			Port:   port,
			Source: config.NodeSourceInline,
		}},
	}

	// While the listener is up, the port must be reported as still held.
	if remaining := waitForListenPortsReleased(ctx, cfg, 300*time.Millisecond); len(remaining) == 0 {
		t.Fatal("port reported released while listener still bound it")
	} else if remaining[0].port != port {
		t.Fatalf("reported conflicting port = %d, want %d", remaining[0].port, port)
	}

	// Once the listener closes, the wait must succeed promptly.
	ln.Close()
	if remaining := waitForListenPortsReleased(ctx, cfg, 2*time.Second); len(remaining) != 0 {
		t.Fatalf("port %v still reported held after listener closed", remaining)
	}
}

// TestWaitForListenPortsReleasedHonoursCancellation verifies a cancelled
// context ends the port-release wait immediately.
func TestWaitForListenPortsReleasedHonoursCancellation(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind test listener: %v", err)
	}
	defer ln.Close()
	port := uint16(ln.Addr().(*net.TCPAddr).Port)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	// hybrid mode listener port is the one checked by configListenPorts.
	cfg2 := &config.Config{
		Mode:     "hybrid",
		Listener: config.ListenerConfig{Address: "127.0.0.1", Port: port, Username: "u", Password: "p"},
		Nodes:    []config.NodeConfig{},
	}
	start := time.Now()
	if remaining := waitForListenPortsReleased(ctx, cfg2, 5*time.Second); len(remaining) == 0 {
		t.Fatal("cancelled wait should still report held ports, not pretend they are free")
	}
	if since := time.Since(start); since > time.Second {
		t.Fatalf("cancelled wait took %v, want immediate return", since)
	}
}

// failedStartBox fails Start with a bind error for the first n calls, then
// succeeds — used to exercise the transient-retry / reassign machinery without
// touching real sockets.
type failedStartBox struct {
	mu       sync.Mutex
	startErr string
	until    int // fail while call-count <= this
	calls    int
}

func (b *failedStartBox) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	if b.calls <= b.until {
		return errors.New(b.startErr)
	}
	return nil
}

func (b *failedStartBox) Close() error { return nil }

// TestReloadRetriesThenReassignsOnPersistentPortConflict verifies reload keeps
// the WARN + retry + reassign semantics for a port that is genuinely still
// taken: retries happen first, reassignment happens after the retry budget,
// and the reload eventually succeeds with the node moved off the conflict port.
func TestReloadRetriesThenReassignsOnPersistentPortConflict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oldBackoff := transientPortConflictBackoff
	transientPortConflictBackoff = time.Millisecond
	defer func() { transientPortConflictBackoff = oldBackoff }()

	newBox := &failedStartBox{startErr: "listen tcp4 0.0.0.0:24034: bind: address already in use", until: 4}
	mgr := New(lifecycleTestCfg("initial", 30120), monitor.Config{})
	n := 0
	mgr.boxFactory = func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		if cfg == nil {
			return nil, errors.New("config is nil")
		}
		n++
		if n == 1 {
			return &blockingBox{}, nil
		}
		return newBox, nil
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	conflictCfg := lifecycleTestCfg("conflict", 24034)
	if err := mgr.Reload(conflictCfg); err != nil {
		t.Fatalf("Reload with transient port conflict: %v", err)
	}
	// The node must have been moved off 24034 by the reassignment path.
	if conflictCfg.Nodes[0].Port == 24034 {
		t.Fatalf("node port still 24034 after persistent conflict; reassign did not run")
	}
	if got := mgr.State(); got != StateRunning {
		t.Fatalf("State() after conflict reload = %v, want running", got)
	}
}

// TestStartRetriesAndReassignsOnPortConflict verifies the startup path still
// resolves a port conflict by reassigning and retrying instead of failing.
func TestStartRetriesAndReassignsOnPortConflict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startBox := &failedStartBox{startErr: "listen tcp4 0.0.0.0:24034: bind: address already in use", until: 1}
	cfg := lifecycleTestCfg("start-conflict", 24034)
	mgr := New(cfg, monitor.Config{})
	mgr.boxFactory = func(_ context.Context, c *config.Config) (boxInstance, error) {
		if c == nil {
			return nil, errors.New("config is nil")
		}
		return startBox, nil
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start with port conflict: %v", err)
	}
	if cfg.Nodes[0].Port == 24034 {
		t.Fatalf("node port still 24034 after startup reassign; reassign did not run")
	}
	if got := mgr.State(); got != StateRunning {
		t.Fatalf("State() after conflict startup = %v, want running", got)
	}
}
