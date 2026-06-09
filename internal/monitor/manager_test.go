package monitor

import "testing"

func TestManagerClearSourceRemovesOnlyMatchingRuntimeNodes(t *testing.T) {
	mgr, err := NewManager(Config{})
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(NodeInfo{Tag: "sub-a", Source: "subscription"})
	mgr.Register(NodeInfo{Tag: "free-a", Source: "free_proxy"})
	mgr.Register(NodeInfo{Tag: "free-b", Source: "free_proxy"})

	mgr.ClearSource("free_proxy")

	snapshots := mgr.Snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("snapshot len=%d, want 1: %#v", len(snapshots), snapshots)
	}
	if snapshots[0].Tag != "sub-a" || snapshots[0].Source != "subscription" {
		t.Fatalf("unexpected remaining snapshot: %#v", snapshots[0])
	}
}

func TestManagerSetAllowedTagsPrunesAndRejectsStaleRegistrations(t *testing.T) {
	mgr, err := NewManager(Config{})
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(NodeInfo{Tag: "current-a", Source: "subscription"})
	mgr.Register(NodeInfo{Tag: "stale-free", Source: "free_proxy"})

	mgr.SetAllowedTags([]string{"current-a", "current-b"})

	if got := mgr.Register(NodeInfo{Tag: "stale-free", Source: "free_proxy"}); got != nil {
		t.Fatal("stale registration should be rejected")
	}
	if got := mgr.Register(NodeInfo{Tag: "current-b", Source: "subscription"}); got == nil {
		t.Fatal("current registration should be accepted")
	}
	snapshots := mgr.Snapshot()
	if len(snapshots) != 2 {
		t.Fatalf("snapshot len=%d, want 2: %#v", len(snapshots), snapshots)
	}
	for _, snap := range snapshots {
		if snap.Tag == "stale-free" {
			t.Fatalf("stale snapshot remained: %#v", snapshots)
		}
	}
}
