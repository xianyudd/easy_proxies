package boxmgr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
	"easy_proxies/internal/nodesource"
)

func localTestManager(t *testing.T) *Manager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("mode: pool\nnodes:\n  - name: inline\n    uri: http://127.0.0.1:1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m := New(c, monitor.Config{})
	m.currentBox = &fakeBox{}
	m.state = StateRunning
	m.baseCtx = context.Background()
	m.portReleaseTimeout = time.Millisecond
	m.boxFactory = func(context.Context, *config.Config) (boxInstance, error) { return &fakeBox{}, nil }
	return m
}
func TestLocalPreviewCountsOtherOwnersNotSelf(t *testing.T) {
	m := localTestManager(t)
	ctx := context.Background()
	if err := m.CommitSubscriptionNodes(nil, []config.NodeConfig{{URI: "http://127.0.0.1:1", SubscriptionName: "subscription", SubscriptionID: strings.Repeat("a", 64)}}); err != nil {
		t.Fatal(err)
	}
	s, err := m.SaveLocalSource(ctx, "", 0, "self", true, "http://127.0.0.1:2\nhttp://127.0.0.1:3")
	if err != nil {
		t.Fatal(err)
	}
	id := s.Sources[0].ID
	if err = m.ApplyLocalSources(ctx, 1); err != nil {
		t.Fatal(err)
	}
	s, err = m.SaveLocalSource(ctx, "", 1, "other", true, "http://127.0.0.1:3")
	if err != nil {
		t.Fatal(err)
	}
	revision := s.Revision
	p, err := m.PreviewLocalSource(ctx, id, &revision, "http://127.0.0.1:1\nhttp://127.0.0.1:2\nhttp://127.0.0.1:3#x\nhttp://127.0.0.1:3#duplicate")
	if err != nil {
		t.Fatal(err)
	}
	if p.SharedCount != 2 || p.SharedNonLocal != 1 || p.SharedDraft != 1 || p.SharedActive != 0 {
		t.Fatalf("preview includes self or duplicates: %+v", p)
	}
	if err = m.ApplyLocalSources(ctx, revision); err != nil {
		t.Fatal(err)
	}
	s, err = m.DeleteLocalSource(ctx, s.Sources[1].ID, revision)
	if err != nil {
		t.Fatal(err)
	}
	revision = s.Revision
	p, err = m.PreviewLocalSource(ctx, id, &revision, "http://127.0.0.1:3")
	if err != nil {
		t.Fatal(err)
	}
	if p.SharedCount != 0 || p.SharedDraft != 0 || p.SharedActive != 1 {
		t.Fatalf("active and draft ownership conflated: %+v", p)
	}
	stale := 0
	if _, err = m.PreviewLocalSource(ctx, id, &stale, "http://127.0.0.1:3"); !errors.Is(err, config.ErrLocalSourceConflict) {
		t.Fatal("stale preview accepted")
	}
	if _, err = m.PreviewLocalSource(ctx, "missing", nil, "http://127.0.0.1:3"); !errors.Is(err, config.ErrLocalSourceNotFound) {
		t.Fatal("missing source accepted")
	}
}

func TestLocalSourcesDraftApplyAndOrdinaryReload(t *testing.T) {
	m := localTestManager(t)
	ctx := context.Background()
	state, err := m.SaveLocalSource(ctx, "", 0, "Europe", true, "http://127.0.0.1:2#offline")
	if err != nil || !state.Pending {
		t.Fatalf("draft: %v", err)
	}
	if len(m.cfg.Nodes) != 1 {
		t.Fatal("draft materialized early")
	}
	if err = m.TriggerReload(ctx); err != nil {
		t.Fatal(err)
	}
	if len(m.cfg.Nodes) != 1 {
		t.Fatal("ordinary reload consumed draft")
	}
	if err = m.ApplyLocalSources(ctx, state.Revision); err != nil {
		t.Fatal(err)
	}
	if len(m.cfg.Nodes) != 2 {
		t.Fatal("local node missing")
	}
	active, err := m.LocalSources(ctx)
	if err != nil || active.Pending || active.ActiveRevision != 1 {
		t.Fatalf("active: %+v %v", active, err)
	}
	id := active.Sources[0].ID
	if _, err = m.UpdateNode(ctx, "offline", config.NodeConfig{URI: "http://other.test"}); err == nil {
		t.Fatal("legacy edit changed local member")
	}
	if err = m.DeleteNode(ctx, "offline"); err == nil {
		t.Fatal("legacy delete changed local member")
	}
	if _, err = m.DeleteLocalSource(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	if err = m.ApplyLocalSources(ctx, 1); !errors.Is(err, config.ErrLocalSourceConflict) {
		t.Fatalf("stale apply: %v", err)
	}
	if err = m.ApplyLocalSources(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if len(m.cfg.Nodes) != 1 {
		t.Fatal("delete did not remove local node")
	}
}
func TestLocalSourcesFailureKeepsDraftAndOldRuntime(t *testing.T) {
	m := localTestManager(t)
	ctx := context.Background()
	_, err := m.SaveLocalSource(ctx, "", 0, "Europe", true, "http://127.0.0.1:2")
	if err != nil {
		t.Fatal(err)
	}
	m.boxFactory = func(_ context.Context, c *config.Config) (boxInstance, error) {
		if len(c.Nodes) > 1 {
			return nil, errors.New("synthetic failure")
		}
		return &fakeBox{}, nil
	}
	if err = m.ApplyLocalSources(ctx, 1); err == nil {
		t.Fatal("expected failure")
	}
	s, _ := m.LocalSources(ctx)
	if !s.Pending || len(m.cfg.Nodes) != 1 || m.State() != StateRunning {
		t.Fatal("rollback/draft lost")
	}
}
func TestLocalSourceMetadataOnlyPreservesMembers(t *testing.T) {
	m := localTestManager(t)
	ctx := context.Background()
	s, err := m.SaveLocalSource(ctx, "", 0, "Europe", true, "http://127.0.0.1:2")
	if err != nil {
		t.Fatal(err)
	}
	s, err = m.SaveLocalSource(ctx, s.Sources[0].ID, 1, "Renamed", false, "")
	if err != nil || s.Sources[0].Name != "Renamed" || s.Sources[0].Enabled || len(s.Sources[0].Nodes) != 1 {
		t.Fatalf("metadata-only update lost members: %v", err)
	}
}

func TestLocalApplyPersistenceFailureAndRollback(t *testing.T) {
	for _, rollbackFails := range []bool{false, true} {
		t.Run(fmt.Sprint(rollbackFails), func(t *testing.T) {
			m := localTestManager(t)
			ctx := context.Background()
			if _, err := m.SaveLocalSource(ctx, "", 0, "Europe", true, "http://127.0.0.1:2"); err != nil {
				t.Fatal(err)
			}
			lock := m.cfg.FilePath() + ".lock"
			injected := false
			m.boxFactory = func(_ context.Context, c *config.Config) (boxInstance, error) {
				if c.LocalSourcesRevision > 0 && !injected {
					injected = true
					if err := os.Remove(lock); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(lock, 0700); err != nil {
						t.Fatal(err)
					}
				} else {
					_ = os.Remove(lock)
					if rollbackFails {
						return nil, errors.New("synthetic rollback failure")
					}
				}
				return &fakeBox{}, nil
			}
			err := m.ApplyLocalSources(ctx, 1)
			if err == nil {
				t.Fatal("persistence failure reported success")
			}
			if rollbackFails && !strings.Contains(err.Error(), "rollback also failed") {
				t.Fatal("rollback failure hidden")
			}
			s, e := m.LocalSources(ctx)
			if e != nil || !s.Pending || s.ActiveRevision != 0 {
				t.Fatal("failed persistence discarded draft")
			}
			if !rollbackFails && (m.State() != StateRunning || len(m.cfg.Nodes) != 1) {
				t.Fatal("previous runtime not restored")
			}
		})
	}
}

func TestConfigSnapshotCoordinatesSettingsEdits(t *testing.T) {
	m := localTestManager(t)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			unlock := m.LockConfigEdits()
			m.cfg.LocalSourcesRevision = i
			unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = m.ConfigSnapshot()
		}
	}()
	wg.Wait()
}

func TestLocalApplyRejectsEmptyRuntime(t *testing.T) {
	m := localTestManager(t)
	ctx := context.Background()
	m.cfg.Nodes = nil
	s, err := m.SaveLocalSource(ctx, "", 0, "disabled", false, "http://127.0.0.1:2")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.ApplyLocalSources(ctx, s.Revision); err == nil {
		t.Fatal("zero runtime accepted")
	}
}

func TestSharedOriginsAfterSubscriptionWithdrawal(t *testing.T) {
	m := localTestManager(t)
	ctx := context.Background()
	m.cfg.Subscriptions = []string{"http://127.0.0.1:1/sub"}
	if err := m.cfg.SaveSubscriptionSettings(); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitSubscriptionNodes(m.cfg.Subscriptions, []config.NodeConfig{{URI: "http://127.0.0.1:1", SubscriptionName: "subscription", SubscriptionID: strings.Repeat("a", 64)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveLocalSource(ctx, "", 0, "Europe", true, "http://127.0.0.1:1#shared"); err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyLocalSources(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitSubscriptionNodes(m.cfg.Subscriptions, nil); err != nil {
		t.Fatal(err)
	}
	if len(m.cfg.Nodes) != 1 || m.cfg.Nodes[0].SubscriptionName != "" || len(m.cfg.Nodes[0].LocalSources) != 1 {
		t.Fatal("withdrawal erased remaining owners or left stale subscription")
	}
	for i := 0; i < 3; i++ {
		if err := m.cfg.ComposeLocalNodes(); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.cfg.SaveNodes(); err != nil {
		t.Fatal(err)
	}
	restarted, err := config.Load(m.cfg.FilePath())
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.Nodes) != 1 || len(restarted.Nodes[0].LocalSources) != 1 || restarted.Nodes[0].SubscriptionName != "" {
		t.Fatal("restart lost owners")
	}
}

func TestOrdinaryReloadRefreshesFreeSourcesWithoutApplyingDraft(t *testing.T) {
	m := localTestManager(t)
	ctx := context.Background()
	path := filepath.Join(filepath.Dir(m.cfg.FilePath()), "free.txt")
	if err := os.WriteFile(path, []byte("http://127.0.0.1:3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m.cfg.FreeProxySources = []nodesource.SourceConfig{{Name: "free", File: path, Format: "txt"}}
	no := false
	m.cfg.FreeProxyCache.Enabled = &no
	m.cfg.FreeProxyFilter.Enabled = false
	if _, err := m.SaveLocalSource(ctx, "", 0, "draft", true, "http://127.0.0.1:2"); err != nil {
		t.Fatal(err)
	}
	if err := m.TriggerReload(ctx); err != nil {
		t.Fatal(err)
	}
	if len(m.cfg.Nodes) != 2 || m.cfg.Nodes[1].Source != config.NodeSourceFreeProxy || len(m.cfg.LocalSources) != 0 {
		t.Fatal("ordinary reload ignored free source or consumed draft")
	}
}

func TestLocalSourceErrorsAndFreeRefresh(t *testing.T) {
	m := localTestManager(t)
	ctx := context.Background()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.LocalSources(cancelled); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err := m.SaveLocalSource(cancelled, "", 0, "name", true, "http://127.0.0.1:2"); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err := m.DeleteLocalSource(cancelled, "x", 0); err == nil {
		t.Fatal("cancel ignored")
	}
	if err := m.ApplyLocalSources(cancelled, 0); err == nil {
		t.Fatal("cancel ignored")
	}
	if err := m.ReloadFreeProxySources(cancelled); err == nil {
		t.Fatal("cancel ignored")
	}
	m.commitMu.Lock()
	if _, err := m.SaveLocalSource(ctx, "", 0, "name", true, "http://127.0.0.1:2"); !errors.Is(err, config.ErrLocalSourceBusy) {
		t.Fatal(err)
	}
	if _, err := m.DeleteLocalSource(ctx, "x", 0); !errors.Is(err, config.ErrLocalSourceBusy) {
		t.Fatal(err)
	}
	if err := m.ApplyLocalSources(ctx, 0); !errors.Is(err, config.ErrLocalSourceBusy) {
		t.Fatal(err)
	}
	m.commitMu.Unlock()
	if _, err := m.SaveLocalSource(ctx, "", 0, "name", true, "invalid"); err == nil {
		t.Fatal("invalid URI accepted")
	}
	if _, err := m.SaveLocalSource(ctx, "missing", 0, "name", true, "http://127.0.0.1:2"); !errors.Is(err, config.ErrLocalSourceNotFound) {
		t.Fatal(err)
	}
	if _, err := m.DeleteLocalSource(ctx, "missing", 0); !errors.Is(err, config.ErrLocalSourceNotFound) {
		t.Fatal(err)
	}
	if _, err := m.DeleteLocalSource(ctx, "missing", 1); !errors.Is(err, config.ErrLocalSourceConflict) {
		t.Fatal(err)
	}
	if err := m.ReloadFreeProxySources(ctx); err != nil {
		t.Fatal(err)
	}
	if len(m.ConfigSnapshot().Nodes) != 1 {
		t.Fatal("free refresh lost inline node")
	}
}

func TestSubscriptionCommitUsesLatestActive(t *testing.T) {
	m := localTestManager(t)
	ctx := context.Background()
	m.cfg.Subscriptions = []string{"https://subscription.test/synthetic"}
	before := m.ConfigSnapshot()
	if _, err := m.SaveLocalSource(ctx, "", 0, "Europe", true, "http://127.0.0.1:2"); err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyLocalSources(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitSubscriptionNodes(before.Subscriptions, []config.NodeConfig{{URI: "http://127.0.0.1:3"}}); err != nil {
		t.Fatal(err)
	}
	if len(m.cfg.Nodes) != 3 || len(m.cfg.LocalSources) != 1 {
		t.Fatal("refresh lost inline/local nodes")
	}
	if err := m.CommitSubscriptionNodes([]string{"stale"}, []config.NodeConfig{{URI: "http://127.0.0.1:4"}}); !errors.Is(err, config.ErrLocalSourceConflict) {
		t.Fatal("stale fetch committed")
	}
}
