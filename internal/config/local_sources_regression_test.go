package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalPortsSkipOccupiedAndReservedTogether(t *testing.T) {
	for _, mode := range []string{"multi-port", "hybrid"} {
		for _, reload := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reload=%v", mode, reload), func(t *testing.T) {
				l, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				p := l.Addr().(*net.TCPAddr).Port
				if p > 65532 {
					t.Skip("ephemeral port too close to limit")
				}
				c := &Config{Mode: mode, MultiPort: MultiPortConfig{Address: "127.0.0.1", BasePort: uint16(p)}, Listener: ListenerConfig{Address: "127.0.0.1", Port: uint16(p + 2)}, Nodes: []NodeConfig{{URI: "http://127.0.0.1:1", Source: NodeSourceInline}}, LocalSources: []LocalSource{{ID: "local", Name: "local", Enabled: true, Nodes: []LocalSourceNode{{URI: "http://127.0.0.1:2", Port: uint16(p + 1)}}}}}
				if reload {
					err = c.NormalizeWithPortMap(nil)
				} else {
					err = c.normalize()
				}
				if err != nil {
					t.Fatal(err)
				}
				if c.Nodes[0].Port == uint16(p) || c.Nodes[0].Port == uint16(p+1) || (mode == "hybrid" && c.Nodes[0].Port == uint16(p+2)) {
					t.Fatal("assigned occupied or reserved port")
				}
			})
		}
	}
}

func TestSharedInlineSubscriptionSurvivesSaveAndOfflineRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cache := filepath.Join(dir, "nodes.txt")
	if err := os.WriteFile(path, []byte("mode: pool\nnodes_file: "+cache+"\nsubscriptions:\n  - http://127.0.0.1:1/unavailable\nnodes:\n  - {name: inline, uri: 'http://127.0.0.1:2'}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sub := NodeConfig{Name: "subscription member", URI: "http://127.0.0.1:2", Source: NodeSourceSubscription, SubscriptionName: "synthetic subscription", SubscriptionID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if err := WriteSubscriptionNodes(cache, []NodeConfig{sub}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Nodes) != 1 {
		t.Fatal("shared node not deduplicated")
	}
	if err = c.SaveNodes(); err != nil {
		t.Fatal(err)
	}
	cached, err := loadNodesFromFile(cache)
	if err != nil || len(cached) != 1 {
		t.Fatal("shared subscription membership erased from cache")
	}
	restarted, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.Nodes) != 1 || restarted.Nodes[0].Source != NodeSourceInline || restarted.Nodes[0].SubscriptionName != sub.SubscriptionName {
		t.Fatal("offline restart lost inline/subscription attribution")
	}
}
