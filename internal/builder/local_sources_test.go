package builder

import (
	"easy_proxies/internal/config"
	"strings"
	"testing"
)

func TestLocalMemberBuildFailureIsAtomicAndRedacted(t *testing.T) {
	c := &config.Config{Mode: "pool", Nodes: []config.NodeConfig{
		{Name: "base", URI: "http://127.0.0.1:1"},
		{Name: "synthetic-sensitive-name", URI: "trojan://synthetic-sensitive-credential@127.0.0.1:2?type=unsupported", LocalSources: []config.LocalSourceRef{{ID: "source", Name: "local"}}},
	}}
	_, err := Build(c)
	if err == nil {
		t.Fatal("invalid local member silently skipped")
	}
	if strings.Contains(err.Error(), "synthetic-sensitive") {
		t.Fatal("build error exposed member data")
	}
}
