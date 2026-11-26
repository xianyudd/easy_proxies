//go:build with_clash_api

package boxmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
)

// Exercises a real sing-box instance through the actual management socket.
// All endpoints and nodes are synthetic loopback addresses; no browser is used.
func TestLocalSourcesRealHTTPRuntimeLifecycle(t *testing.T) {
	freeAddr := func() string {
		t.Helper()
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		a := l.Addr().String()
		_ = l.Close()
		return a
	}
	adminAddr, proxyAddr, clashAddr := freeAddr(), freeAddr(), freeAddr()
	_, proxyPort, _ := net.SplitHostPort(proxyAddr)
	path := filepath.Join(t.TempDir(), "config.yaml")
	text := fmt.Sprintf("mode: pool\nlistener:\n  address: 127.0.0.1\n  port: %s\nmanagement:\n  enabled: true\n  listen: %s\n  clash_api_listen: %s\n  probe_target: http://127.0.0.1:1\n  password: ep02-synthetic-password\n  api_keys:\n    - {name: admin, key: epk_ep02_synthetic_admin, role: admin}\n    - {name: read, key: epk_ep02_synthetic_read, role: read}\nsubscription_refresh:\n  health_check_timeout: 50ms\ngeoip:\n  enabled: false\nfree_proxy_cache:\n  enabled: false\nnodes:\n  - {name: inline, uri: 'http://127.0.0.1:1'}\n", proxyPort, adminAddr, clashAddr)
	if e := os.WriteFile(path, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var m *Manager
	start := func() {
		t.Helper()
		c, e := config.Load(path)
		if e != nil {
			t.Fatal(e)
		}
		m = New(c, monitor.Config{Enabled: true, Listen: adminAddr, ProbeTarget: "http://127.0.0.1:1"}, WithPortReleaseTimeout(time.Millisecond))
		if e = m.Start(ctx); e != nil {
			t.Fatal(e)
		}
	}
	start()
	defer func() {
		if m != nil {
			_ = m.Close()
		}
	}()
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	call := func(method, suffix string, body any, key string, want int) map[string]any {
		t.Helper()
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req, e := http.NewRequest(method, "http://"+adminAddr+"/api/"+suffix, bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		data, e := io.ReadAll(res.Body)
		if e != nil {
			t.Fatal(e)
		}
		if res.StatusCode != want {
			t.Fatalf("%s %s: status %d want %d: %s", method, suffix, res.StatusCode, want, data)
		}
		out := map[string]any{}
		_ = json.Unmarshal(data, &out)
		return out
	}
	const admin = "epk_ep02_synthetic_admin"
	for _, key := range []string{"", "epk_ep02_synthetic_read"} {
		want := 401
		if key != "" {
			want = 403
		}
		call("GET", "local-sources", nil, key, want)
		call("GET", "local-sources/missing", nil, key, want)
	}
	call("GET", "local-sources/preview", nil, admin, 405)
	call("GET", "local-sources/apply", nil, admin, 405)
	call("PUT", "local-sources", nil, admin, 405)
	call("POST", "local-sources", map[string]any{"name": "missing revision"}, admin, 400)
	call("POST", "local-sources/apply", map[string]any{}, admin, 400)
	call("POST", "local-sources/preview", map[string]any{"content": "http://127.0.0.1:2#offline\nhttp://127.0.0.1:2#duplicate"}, admin, 200)
	saved := call("POST", "local-sources", map[string]any{"expected_revision": 0, "name": "Europe", "enabled": true, "content": "http://127.0.0.1:1#shared\nhttp://127.0.0.1:2#offline"}, admin, 200)
	id := saved["sources"].([]any)[0].(map[string]any)["id"].(string)
	for _, method := range []string{"POST", "PUT"} {
		suffix := "local-sources"
		if method == "PUT" {
			suffix += "/" + id
		}
		bad := call(method, suffix, map[string]any{"expected_revision": 1, "name": "invalid", "content": string(rune(0xfeff)) + "# comment\n\nhttp://private-user:private-pass@127.0.0.1:0"}, admin, 400)
		if bad["line"] != float64(3) || bad["reason"] == "" {
			t.Fatal("save lost safe line number")
		}
		encoded, _ := json.Marshal(bad)
		if bytes.Contains(encoded, []byte("private-")) {
			t.Fatal("save error leaked URI")
		}
	}
	preview := call("POST", "local-sources/preview", map[string]any{"expected_revision": 1, "source_id": id, "content": "http://127.0.0.1:1\nhttp://127.0.0.1:2"}, admin, 200)
	if preview["shared_count"] != float64(1) || preview["shared_non_local"] != float64(1) || preview["shared_draft"] != float64(0) {
		t.Fatal("HTTP preview counted self or lost shared inline")
	}
	detail := call("GET", "local-sources/"+id, nil, admin, 200)
	if len(detail["member_health"].([]any)) != 2 || detail["member_health"].([]any)[1].(map[string]any)["status"] != "unapplied" {
		t.Fatal("HTTP draft member health missing")
	}

	call("GET", "local-sources/missing", nil, admin, 404)
	call("PUT", "local-sources/missing", map[string]any{"expected_revision": 1, "name": "missing", "content": "http://127.0.0.1:2"}, admin, 404)
	call("DELETE", "local-sources/missing", map[string]any{"expected_revision": 1}, admin, 404)
	call("POST", "local-sources", map[string]any{"expected_revision": 0, "name": "stale", "content": "http://127.0.0.1:3"}, admin, 409)
	if len(m.ConfigSnapshot().Nodes) != 1 {
		t.Fatal("save materialized draft")
	}
	if e := m.TriggerReload(ctx); e != nil {
		t.Fatal(e)
	}
	if len(m.ConfigSnapshot().Nodes) != 1 {
		t.Fatal("ordinary reload consumed draft")
	}
	if e := m.Close(); e != nil {
		t.Fatal(e)
	}
	start()
	if len(m.ConfigSnapshot().Nodes) != 1 {
		t.Fatal("restart consumed draft")
	}
	apply := func(rev int) {
		t.Helper()
		call("POST", "local-sources/apply", map[string]any{"expected_revision": rev}, admin, 202)
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			status := call("GET", "reload/status", nil, admin, 200)
			if status["state"] == "failed" {
				t.Fatalf("apply failed: %v", status)
			}
			if status["state"] == "succeeded" && status["local_revision"] == float64(rev) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("apply timed out")
	}
	apply(1)
	if c := m.ConfigSnapshot(); len(c.Nodes) != 2 || len(c.LocalSources[0].Nodes) != 2 {
		t.Fatal("offline/shared membership lost or duplicated")
	}
	call("POST", "local-sources/apply", map[string]any{"expected_revision": 1}, admin, 409)
	if e := m.Close(); e != nil {
		t.Fatal(e)
	}
	start()
	if len(m.ConfigSnapshot().Nodes) != 2 {
		t.Fatal("active source lost on restart")
	}
	call("PUT", "local-sources/"+id, map[string]any{"expected_revision": 1, "name": "Renamed", "enabled": true, "content": "http://127.0.0.1:1#shared\nhttp://127.0.0.1:3#replacement"}, admin, 200)
	apply(2)
	// Exercise the shared settings commit gate through its actual HTTP route.
	call("PUT", "settings", map[string]any{"log_level": "info"}, admin, 200)
	call("PUT", "local-sources/"+id, map[string]any{"expected_revision": 2, "name": "Renamed", "enabled": false}, admin, 200)
	apply(3)
	if len(m.ConfigSnapshot().Nodes) != 1 {
		t.Fatal("disabled source changed shared inline")
	}
	state := call("DELETE", "local-sources/"+id, map[string]any{"expected_revision": 3}, admin, 200)
	if state["pending"] != true || len(state["sources"].([]any)) != 0 {
		t.Fatal("empty draft not pending")
	}
	apply(4)
	state = call("GET", "local-sources", nil, admin, 200)
	if state["pending"] != false || state["active_revision"] != float64(4) {
		t.Fatal("application not persisted")
	}
	t.Log("real sing-box + HTTP: admin/read auth, preview, create, stale rejection, draft reload/restart, apply, shared/offline members, applied restart, replace, disable, delete-last: PASS")
}
