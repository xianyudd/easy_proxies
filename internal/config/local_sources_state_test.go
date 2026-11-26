package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAtomicWritePrecommitErrorsAndNewFileRollback(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{filepath.Join(dir, "missing", "config.yaml"), dir} {
		called := false
		if err := atomicConfigWriteWithSync(path, []byte("candidate"), func(*os.File) error { called = true; return nil }); err == nil || called {
			t.Fatalf("precommit error was ignored for %s", path)
		}
	}
	parentFile := filepath.Join(dir, "parent-file")
	if err := os.WriteFile(parentFile, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := atomicConfigWrite(filepath.Join(parentFile, "config.yaml"), []byte("candidate")); err == nil {
		t.Fatal("non-directory parent accepted")
	}
	if b, _ := os.ReadFile(parentFile); string(b) != "original" {
		t.Fatal("precommit failure changed original")
	}
	for _, failRemove := range []bool{false, true} {
		t.Run(fmt.Sprint(failRemove), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "new.yaml")
			calls := 0
			err := atomicConfigWriteWithSync(path, []byte("candidate"), func(*os.File) error {
				calls++
				if calls == 1 {
					if failRemove {
						if e := os.Remove(path); e != nil {
							t.Fatal(e)
						}
						if e := os.Mkdir(path, 0700); e != nil {
							t.Fatal(e)
						}
						if e := os.WriteFile(filepath.Join(path, "block"), []byte("x"), 0600); e != nil {
							t.Fatal(e)
						}
					}
					return errors.New("synthetic sync failure")
				}
				return nil
			})
			if err == nil {
				t.Fatal("failed sync reported success")
			}
			if failRemove {
				if !errors.Is(err, ErrLocalCommitUncertain) {
					t.Fatal("failed deletion not reported uncertain")
				}
			} else {
				if _, e := os.Stat(path); !os.IsNotExist(e) {
					t.Fatal("new candidate not removed on rollback")
				}
				if calls != 2 {
					t.Fatal("rollback directory not synced")
				}
			}
		})
	}
}

func TestLocalCompositionPreservesSources(t *testing.T) {
	uri := "http://node.test:8080"
	base := []NodeConfig{{URI: uri, Name: "remote", Source: NodeSourceSubscription, SubscriptionName: "subscription", Port: 31001}, {URI: "http://inline.test:8080", Source: NodeSourceInline}}
	sources := []LocalSource{{ID: "a", Name: "local", Enabled: true, Nodes: []LocalSourceNode{{URI: uri + "#local"}, {URI: "http://offline.test:8080"}}}}
	got, err := ComposeLocalSources(base, sources)
	if err != nil || len(got) != 3 {
		t.Fatalf("composition: count=%d error=%v", len(got), err)
	}
	if got[0].SubscriptionName != "subscription" || len(got[0].LocalSources) != 1 || got[0].Port != 31001 {
		t.Fatal("lost subscription ownership or port")
	}
	again, err := ComposeLocalSources(got, sources)
	if err != nil || len(again) != 3 || len(again[0].LocalSources) != 1 {
		t.Fatal("not idempotent")
	}
	sources[0].Enabled = false
	disabled, err := ComposeLocalSources(got, sources)
	if err != nil || len(disabled) != 2 || len(disabled[0].LocalSources) != 0 {
		t.Fatal("disable removed another source")
	}
	sources[0].Enabled = true
	sources[0].Nodes[0].URI = "http://NODE.test:8080"
	if _, err = ComposeLocalSources(base, sources); err == nil {
		t.Fatal("key collision accepted")
	}
}

func TestLocalDraftAtomicState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("nodes:\n  - uri: http://inline.test:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{filePath: path}
	src := []LocalSource{{ID: "a", Name: "local", Enabled: true, Nodes: []LocalSourceNode{{URI: "http://offline.test:8080"}}}}
	state, err := cfg.SaveLocalSourcesDraft(0, src)
	if err != nil || state.Revision != 1 || !state.Pending {
		t.Fatalf("save: %+v %v", state, err)
	}
	if _, err = cfg.SaveLocalSourcesDraft(0, src); !errors.Is(err, ErrLocalSourceConflict) {
		t.Fatalf("stale: %v", err)
	}
	// Settings and node saves must preserve the latest draft, not their stale object.
	cfg.ExternalIP = "127.0.0.1"
	if err = cfg.SaveSettings(); err != nil {
		t.Fatal(err)
	}
	cfg.Nodes = []NodeConfig{{URI: "http://inline.test:8080", Source: NodeSourceInline}}
	if err = cfg.SaveNodes(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Nodes) != 1 || loaded.LocalSourcesDraft == nil {
		t.Fatal("restart consumed or lost draft")
	}
	if err = cfg.CommitLocalSources(1, src); err != nil {
		t.Fatal(err)
	}
	loaded, err = Load(path)
	if err != nil || len(loaded.Nodes) != 2 || loaded.LocalSourcesDraft != nil {
		t.Fatalf("active not restored: %v", err)
	}
	state, err = cfg.SaveLocalSourcesDraft(1, []LocalSource{})
	if err != nil || !state.Pending || len(state.Sources) != 0 {
		t.Fatal("empty draft lost")
	}
	if err = cfg.CommitLocalSources(2, []LocalSource{}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions widened: %o", info.Mode().Perm())
	}
}

func TestLocalCompositionBaseDuplicatesAndPortConflicts(t *testing.T) {
	uri := "http://node.test:8080"
	base := []NodeConfig{{URI: uri, Source: NodeSourceInline}, {URI: uri, Source: NodeSourceSubscription, SubscriptionName: "remote", SubscriptionID: "sub-id"}}
	got, err := ComposeLocalSources(base, nil)
	if err != nil || len(got) != 1 || got[0].SubscriptionName != "remote" {
		t.Fatalf("base dedupe: count=%d error=%v", len(got), err)
	}
	base[1].URI = "http://NODE.test:8080"
	if _, err = ComposeLocalSources(base, nil); err == nil {
		t.Fatal("base collision accepted")
	}
	src := []LocalSource{{ID: "a", Name: "a", Enabled: true, Nodes: []LocalSourceNode{{URI: uri, Port: 31001}}}, {ID: "b", Name: "b", Enabled: true, Nodes: []LocalSourceNode{{URI: uri, Port: 31002}}}}
	if _, err = ComposeLocalSources(nil, src); err == nil {
		t.Fatal("conflicting explicit ports accepted")
	}
}

func TestLoadReservesLocalMemberPorts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "mode: pool\nmulti_port:\n  base_port: 31001\nnodes:\n  - uri: http://inline.test\nlocal_sources:\n  - id: a\n    name: local\n    enabled: true\n    nodes:\n      - uri: http://local.test\n        port: 31001\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Nodes[0].Port == c.Nodes[1].Port || c.Nodes[1].Port != 31001 {
		t.Fatal("restart reused reserved local port")
	}
}

func TestDisabledLocalAllowsFreeSourceTakeover(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "free.txt")
	if err := os.WriteFile(cache, []byte("http://node.test:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	body := "mode: pool\nfree_proxy_cache:\n  enabled: false\nfree_proxy_sources:\n  - name: free\n    file: " + cache + "\nlocal_sources:\n  - id: a\n    name: a\n    enabled: true\n    nodes:\n      - uri: http://node.test:8080\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Nodes = []NodeConfig{{URI: "http://node.test:8080", Source: NodeSourceLocal}}
	c.LocalSources[0].Enabled = false
	if err = c.NormalizeWithPortMap(nil); err != nil {
		t.Fatal(err)
	}
	if len(c.Nodes) != 1 || c.Nodes[0].Source != NodeSourceFreeProxy {
		t.Fatal("free source could not take over")
	}
}

func TestAtomicWriteDirectorySyncFailureRestoresOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	old := []byte("local_sources_draft: {revision: 1, sources: []}\n")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	err := atomicConfigWriteWithSync(path, []byte("local_sources_revision: 1\n"), func(*os.File) error { return errors.New("synthetic directory sync failure") })
	if err == nil {
		t.Fatal("durability failure hidden")
	}
	after, e := os.ReadFile(path)
	if e != nil || string(after) != string(old) {
		t.Fatal("failed persistence lost original active/draft")
	}
}

func TestAtomicRecoveryPreservesPermissionsAndFailedRollback(t *testing.T) {
	for _, failRollback := range []bool{false, true} {
		t.Run(fmt.Sprint(failRollback), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte("original"), 0400); err != nil {
				t.Fatal(err)
			}
			err := atomicConfigWriteWithSync(path, []byte("candidate"), func(*os.File) error {
				if failRollback {
					if e := os.Remove(path); e != nil {
						t.Fatal(e)
					}
					if e := os.Mkdir(path, 0700); e != nil {
						t.Fatal(e)
					}
				}
				return errors.New("synthetic sync failure")
			})
			if err == nil {
				t.Fatal("sync failure hidden")
			}
			target := path
			if failRollback {
				if !errors.Is(err, ErrLocalCommitUncertain) {
					t.Fatal("rollback uncertainty not reported")
				}
				backups, _ := filepath.Glob(filepath.Join(dir, ".config-recovery-*"))
				if len(backups) != 1 {
					t.Fatal("failed rollback discarded recovery copy")
				}
				target = backups[0]
			}
			info, e := os.Stat(target)
			if e != nil || info.Mode().Perm() != 0400 {
				t.Fatal("recovery broadened permissions")
			}
			data, e := os.ReadFile(target)
			if e != nil || string(data) != "original" {
				t.Fatal("recovery lost original bytes")
			}
		})
	}
}

func TestAtomicRecoverySecondSyncFailureIsNotSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := atomicConfigWriteWithSync(path, []byte("new"), func(*os.File) error { calls++; return errors.New("synthetic durability failure") })
	if err == nil || calls != 2 {
		t.Fatalf("recovery sync not observable: calls=%d error=%v", calls, err)
	}
	if errors.Is(err, ErrLocalCommitUncertain) {
		t.Fatal("restored bytes misreported as new commit")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "old" {
		t.Fatal("old bytes not restored")
	}
}

func TestLocalDraftConcurrentRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{filePath: path}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := cfg.SaveLocalSourcesDraft(0, []LocalSource{}); results <- err }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrLocalSourceConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("successful writers=%d", success)
	}
}
