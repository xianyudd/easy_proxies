package boxmgr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
)

func TestCreateNodeRejectsInvalidURI(t *testing.T) {
	cfg := &config.Config{Mode: "multi-port", MultiPort: config.MultiPortConfig{BasePort: 18080, Username: "u", Password: "p"}}
	cfg.SetFilePath(t.TempDir() + "/config.yaml")
	mgr := New(cfg, monitor.Config{})

	_, err := mgr.CreateNode(context.Background(), config.NodeConfig{Name: "bad", URI: "not-a-uri"})
	if !errors.Is(err, monitor.ErrInvalidNode) {
		t.Fatalf("CreateNode error = %v, want ErrInvalidNode", err)
	}
}

func TestUpdateNodeRejectsInvalidURI(t *testing.T) {
	cfg := &config.Config{
		Mode:      "multi-port",
		MultiPort: config.MultiPortConfig{BasePort: 18080, Username: "u", Password: "p"},
		Nodes:     []config.NodeConfig{{Name: "old", URI: "http://127.0.0.1:18080", Port: 18080, Source: config.NodeSourceInline}},
	}
	cfg.SetFilePath(t.TempDir() + "/config.yaml")
	mgr := New(cfg, monitor.Config{})

	_, err := mgr.UpdateNode(context.Background(), "old", config.NodeConfig{Name: "old", URI: "http://"})
	if !errors.Is(err, monitor.ErrInvalidNode) {
		t.Fatalf("UpdateNode error = %v, want ErrInvalidNode", err)
	}
	if got := cfg.Nodes[0].URI; !strings.EqualFold(got, "http://127.0.0.1:18080") {
		t.Fatalf("node URI changed after rejected update: %q", got)
	}
}

func TestTriggerReloadWithNilConfigReturnsError(t *testing.T) {
	mgr := New(nil, monitor.Config{})

	err := mgr.TriggerReload(context.Background())

	if err == nil || !strings.Contains(err.Error(), "config is not initialized") {
		t.Fatalf("TriggerReload error = %v, want config unavailable", err)
	}
}

func TestPortConflictRetriesBeforeReassign(t *testing.T) {
	for retry := 0; retry < transientPortConflictRetries; retry++ {
		if !shouldRetryTransientPortConflict(retry) {
			t.Fatalf("retry %d should wait before permanent reassignment", retry)
		}
	}
	if shouldRetryTransientPortConflict(transientPortConflictRetries) {
		t.Fatalf("retry %d should allow reassignment", transientPortConflictRetries)
	}
}
