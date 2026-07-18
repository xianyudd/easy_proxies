package monitor

import (
	"encoding/json"
	"os"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/nodesource"
)

func newAPIKeyTestServer(t *testing.T, keys []config.APIKeyConfig, password string) *Server {
	t.Helper()
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(NodeInfo{Tag: "n1", Name: "N1", URI: "http://1.1.1.1:80", ListenAddress: "127.0.0.1", Port: 31001, Region: "jp"}).MarkInitialCheckDone(true)

	srv := NewServer(Config{
		Enabled:     true,
		Listen:      "127.0.0.1:0",
		Password:    password,
		APIKeys:     keys,
		CORSOrigins: []string{"https://app.example.com"},
	}, mgr, nil)
	if srv == nil {
		t.Fatal("expected server")
	}
	return srv
}

func TestAPIKeyReadCanExtractButNotReload(t *testing.T) {
	keys := []config.APIKeyConfig{
		{Name: "reader", Key: "epk_read_test", Role: "read"},
		{Name: "ops", Key: "epk_admin_test", Role: "admin"},
	}
	srv := newAPIKeyTestServer(t, keys, "")

	// no creds → 401 when keys configured
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nodes", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d body=%s", rec.Code, rec.Body.String())
	}

	// read key can list nodes
	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	req.Header.Set("X-API-Key", "epk_read_test")
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read nodes status=%d body=%s", rec.Code, rec.Body.String())
	}

	// read key cannot export (credentials dump) or reload
	for _, path := range []string{"/api/export?scheme=http", "/api/reload"} {
		method := http.MethodGet
		if path == "/api/reload" {
			method = http.MethodPost
		}
		req = httptest.NewRequest(method, path, nil)
		req.Header.Set("X-API-Key", "epk_read_test")
		rec = httptest.NewRecorder()
		srv.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("read %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	// admin key can export
	req = httptest.NewRequest(http.MethodGet, "/api/export?scheme=http", nil)
	req.Header.Set("X-API-Key", "epk_admin_test")
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin export status=%d body=%s", rec.Code, rec.Body.String())
	}

	// admin key can hit reload endpoint (may fail for missing nodeMgr, but not 401/403)
	req = httptest.NewRequest(http.MethodPost, "/api/reload", nil)
	req.Header.Set("X-API-Key", "epk_admin_test")
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("admin reload should pass auth, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBogusAPIKeyHeaderDoesNotBlockOpenMode(t *testing.T) {
	srv := newAPIKeyTestServer(t, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	req.Header.Set("X-API-Key", "not-a-real-key")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("open mode with bogus key header status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCORSWildcardEchoesOrigin(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{
		Enabled:     true,
		Listen:      "127.0.0.1:0",
		CORSOrigins: []string{"*"},
	}, mgr, nil)
	req := httptest.NewRequest(http.MethodOptions, "/api/nodes", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://evil.example" {
		t.Fatalf("expected echoed origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credentials=%q", got)
	}
}

func TestMergeAPIKeysPreservesSecretOnRoundTrip(t *testing.T) {
	existing := []config.APIKeyConfig{
		{Name: "reader", Key: "epk_secret_value", Role: "read"},
		{Name: "ops", Key: "epk_admin_value", Role: "admin"},
	}
	// Simulate GET→PUT with redacted keys (empty Key).
	incoming := []config.APIKeyConfig{
		{Name: "reader", Key: "", Role: "read"},
		{Name: "ops", Key: "", Role: "admin"},
	}
	merged, err := mergeAPIKeys(existing, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 {
		t.Fatalf("len=%d", len(merged))
	}
	if merged[0].Key != "epk_secret_value" || merged[1].Key != "epk_admin_value" {
		t.Fatalf("secrets not preserved: %+v", merged)
	}

	// Empty list clears.
	cleared, err := mergeAPIKeys(existing, []config.APIKeyConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared) != 0 {
		t.Fatalf("expected clear, got %+v", cleared)
	}

	// New name without key fails.
	if _, err := mergeAPIKeys(existing, []config.APIKeyConfig{{Name: "new", Role: "read"}}); err == nil {
		t.Fatal("expected error for new entry without key")
	}
}

func TestReadKeyCanGetCachesButNotClear(t *testing.T) {
	keys := []config.APIKeyConfig{{Name: "reader", Key: "epk_read_test", Role: "read"}}
	srv := newAPIKeyTestServer(t, keys, "")

	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/cache", nil)
	req.Header.Set("X-API-Key", "epk_read_test")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read cache GET status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/cloudflare/cache", nil)
	req.Header.Set("X-API-Key", "epk_read_test")
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read cache DELETE status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestInvalidBearerFallsThroughToSessionCookie(t *testing.T) {
	keys := []config.APIKeyConfig{{Name: "reader", Key: "epk_read_test", Role: "read"}}
	srv := newAPIKeyTestServer(t, keys, "secret")

	// login for session
	body := strings.NewReader(`{"password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth status=%d body=%s", rec.Code, rec.Body.String())
	}
	var authResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &authResp); err != nil {
		t.Fatal(err)
	}
	token, _ := authResp["token"].(string)
	if token == "" {
		t.Fatal("expected token")
	}

	// stale Authorization + valid session cookie
	req = httptest.NewRequest(http.MethodGet, "/api/export?scheme=http", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie should win after bad bearer: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPasswordlessWithAPIKeysRefusesAuthMint(t *testing.T) {
	// Runtime can be misconfigured; /api/auth must not mint admin sessions.
	keys := []config.APIKeyConfig{{Name: "reader", Key: "epk_read_test", Role: "read"}}
	srv := newAPIKeyTestServer(t, keys, "")

	req := httptest.NewRequest(http.MethodPost, "/api/auth", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("auth status=%d body=%s want 503", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("must not issue token: %s", rec.Body.String())
	}
}

func TestNormalizeRequiresPasswordWithAPIKeys(t *testing.T) {
	cfg := &config.Config{Management: config.ManagementConfig{
		APIKeys: []config.APIKeyConfig{{Name: "a", Key: "k1", Role: "read"}},
	}}
	if err := cfg.NormalizeManagementAuth(); err == nil {
		t.Fatal("expected password required error")
	}
	cfg.Management.Password = "secret"
	if err := cfg.NormalizeManagementAuth(); err != nil {
		t.Fatal(err)
	}
}

func TestReadKeyCannotRevealExtractor(t *testing.T) {
	keys := []config.APIKeyConfig{
		{Name: "reader", Key: "epk_read_test", Role: "read"},
		{Name: "ops", Key: "epk_admin_test", Role: "admin"},
	}
	srv := newAPIKeyTestServer(t, keys, "secret")
	srv.SetConfig(&config.Config{
		Mode: "hybrid",
		Listener: config.ListenerConfig{Address: "127.0.0.1", Port: 2323, Username: "user", Password: "pass"},
		Management: config.ManagementConfig{Password: "secret", APIKeys: keys},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/extractor?region=all&mode=pool&format=http_url&count=1&reveal=true", nil)
	req.Header.Set("X-API-Key", "epk_read_test")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read reveal status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/extractor?region=all&mode=pool&format=http_url&count=1&reveal=true", nil)
	req.Header.Set("X-API-Key", "epk_admin_test")
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin reveal status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMergeAPIKeysNilPreservesExisting(t *testing.T) {
	existing := []config.APIKeyConfig{{Name: "reader", Key: "epk_secret", Role: "read"}}
	merged, err := mergeAPIKeys(existing, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || merged[0].Key != "epk_secret" {
		t.Fatalf("nil should preserve: %+v", merged)
	}
}

func TestReadKeyStripsNodeURI(t *testing.T) {
	keys := []config.APIKeyConfig{{Name: "reader", Key: "epk_read_test", Role: "read"}}
	srv := newAPIKeyTestServer(t, keys, "")
	// ensure node has a secret-looking URI
	srv.mgr.Register(NodeInfo{Tag: "secret", Name: "Secret", URI: "hysteria2://secret@1.1.1.1:443", Port: 31099}).MarkInitialCheckDone(true)

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	req.Header.Set("X-API-Key", "epk_read_test")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hysteria2://secret") {
		t.Fatalf("read nodes leaked uri: %s", rec.Body.String())
	}
}

func TestReadSettingsRedactsFreeProxySourceURL(t *testing.T) {
	keys := []config.APIKeyConfig{{Name: "reader", Key: "epk_read_test", Role: "read"}}
	srv := newAPIKeyTestServer(t, keys, "")
	cfg := &config.Config{
		Management: config.ManagementConfig{
			APIKeys: keys,
		},
		FreeProxySources: []nodesource.SourceConfig{
			{Name: "src", URL: "https://example.com/list?token=secret", Format: "txt"},
		},
	}
	// mark enabled pointer
	en := true
	cfg.FreeProxySources[0].Enabled = &en
	srv.SetConfig(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Header.Set("X-API-Key", "epk_read_test")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "token=secret") {
		t.Fatalf("read settings leaked free proxy url: %s", rec.Body.String())
	}
}

func TestAPIKeyBearerAndSessionStillWork(t *testing.T) {
	keys := []config.APIKeyConfig{{Name: "reader", Key: "epk_read_test", Role: "read"}}
	srv := newAPIKeyTestServer(t, keys, "secret")

	// password login
	body := strings.NewReader(`{"password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth status=%d body=%s", rec.Code, rec.Body.String())
	}
	var authResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &authResp); err != nil {
		t.Fatal(err)
	}
	token, _ := authResp["token"].(string)
	if token == "" {
		t.Fatal("expected session token")
	}

	// session bearer is admin
	req = httptest.NewRequest(http.MethodPost, "/api/reload", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("session admin reload auth failed: %d %s", rec.Code, rec.Body.String())
	}

	// API key via Bearer
	req = httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	req.Header.Set("Authorization", "Bearer epk_read_test")
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer api key status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOpenModeWithoutKeysStillWorks(t *testing.T) {
	srv := newAPIKeyTestServer(t, nil, "")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nodes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("open mode status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCORSHeadersWhenConfigured(t *testing.T) {
	srv := newAPIKeyTestServer(t, nil, "")
	req := httptest.NewRequest(http.MethodOptions, "/api/nodes", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("options status=%d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("cors origin=%q", got)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "X-API-Key") {
		t.Fatalf("missing X-API-Key allow header: %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestSettingsMasksAPIKeys(t *testing.T) {
	keys := []config.APIKeyConfig{{Name: "reader", Key: "epk_secret_value", Role: "read"}}
	srv := newAPIKeyTestServer(t, keys, "")
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Header.Set("X-API-Key", "epk_secret_value")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "epk_secret_value") {
		t.Fatalf("settings leaked raw api key: %s", rec.Body.String())
	}
}

func TestNormalizeManagementAPIKeys(t *testing.T) {
	cfg := &config.Config{Management: config.ManagementConfig{
		Password: "secret",
		APIKeys: []config.APIKeyConfig{
			{Name: "a", Key: " k1 ", Role: "READ"},
			{Name: "b", Key: "k2", Role: "admin"},
		},
		CORSOrigins: []string{" https://a.com ", "https://a.com", ""},
	}}
	if err := cfg.NormalizeManagementAuth(); err != nil {
		t.Fatal(err)
	}
	if cfg.Management.APIKeys[0].Role != "read" || cfg.Management.APIKeys[0].Key != "k1" {
		t.Fatalf("unexpected key0: %+v", cfg.Management.APIKeys[0])
	}
	if len(cfg.Management.CORSOrigins) != 1 || cfg.Management.CORSOrigins[0] != "https://a.com" {
		t.Fatalf("cors=%v", cfg.Management.CORSOrigins)
	}

	cfg.Management.APIKeys = []config.APIKeyConfig{{Key: "same", Role: "read"}, {Key: "same", Role: "admin"}}
	if err := cfg.NormalizeManagementAuth(); err == nil {
		t.Fatal("expected duplicate key error")
	}
}


func TestClearPasswordRejectedWhenAPIKeysPresent(t *testing.T) {
	keys := []config.APIKeyConfig{{Name: "reader", Key: "epk_read_test", Role: "read"}}
	srv := newAPIKeyTestServer(t, keys, "secret")
	cfg := &config.Config{
		Management: config.ManagementConfig{
			Listen:   "127.0.0.1:0",
			Password: "secret",
			APIKeys:  keys,
		},
	}
	cfg.SetFilePath(t.TempDir() + "/config.yaml")
	// minimal save path: write empty yaml so SaveSettings can round-trip
	if err := os.WriteFile(cfg.FilePath(), []byte("mode: pool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.SetConfig(cfg)

	// login as admin session
	req := httptest.NewRequest(http.MethodPost, "/api/auth", strings.NewReader(`{"password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %d %s", rec.Code, rec.Body.String())
	}
	var auth map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &auth)
	token, _ := auth["token"].(string)

	body := `{"management":{"clear_password":true}}`
	req = httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("clear password status=%d body=%s", rec.Code, rec.Body.String())
	}
	if srv.cfg.Password != "secret" {
		t.Fatalf("runtime password should remain secret, got %q", srv.cfg.Password)
	}
}

func TestCreateAPIKeyAutoGenerate(t *testing.T) {
	keys := []config.APIKeyConfig{}
	srv := newAPIKeyTestServer(t, keys, "secret")
	dir := t.TempDir()
	cfgPath := dir + "/config.yaml"
	if err := os.WriteFile(cfgPath, []byte("mode: pool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Management: config.ManagementConfig{Password: "secret", Listen: "127.0.0.1:0"}}
	cfg.SetFilePath(cfgPath)
	srv.SetConfig(cfg)

	// login session
	req := httptest.NewRequest(http.MethodPost, "/api/auth", strings.NewReader(`{"password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %d %s", rec.Code, rec.Body.String())
	}
	var auth map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &auth)
	token, _ := auth["token"].(string)

	// create with empty body -> default read
	req = httptest.NewRequest(http.MethodPost, "/api/management/api-keys", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	ak, _ := created["api_key"].(map[string]any)
	key, _ := ak["key"].(string)
	role, _ := ak["role"].(string)
	name, _ := ak["name"].(string)
	if !strings.HasPrefix(key, "epk_") || len(key) < 20 {
		t.Fatalf("bad key %q", key)
	}
	if role != "read" {
		t.Fatalf("default role=%s", role)
	}
	if name == "" {
		t.Fatal("empty name")
	}

	// use generated key
	req = httptest.NewRequest(http.MethodGet, "/api/nodes?page=1&page_size=1", nil)
	req.Header.Set("X-API-Key", key)
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("use key %d %s", rec.Code, rec.Body.String())
	}

	// list returns key for admin (UI mask+copy); settings GET stays redacted
	req = httptest.NewRequest(http.MethodGet, "/api/management/api-keys", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), key) {
		t.Fatalf("admin list should include key for copy UI: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hint") {
		t.Fatalf("admin list should include hint: %s", rec.Body.String())
	}
	// settings GET still redacts raw keys
	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), key) {
		t.Fatalf("settings leaked plaintext key")
	}

	// delete
	req = httptest.NewRequest(http.MethodDelete, "/api/management/api-keys?name="+name, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete %d %s", rec.Code, rec.Body.String())
	}

	// key no longer works
	req = httptest.NewRequest(http.MethodGet, "/api/nodes?page=1&page_size=1", nil)
	req.Header.Set("X-API-Key", key)
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("deleted key still works: %d", rec.Code)
	}
}

func TestCreateAPIKeyRequiresPassword(t *testing.T) {
	srv := newAPIKeyTestServer(t, nil, "")
	dir := t.TempDir()
	cfgPath := dir + "/config.yaml"
	_ = os.WriteFile(cfgPath, []byte("mode: pool\n"), 0o644)
	cfg := &config.Config{Management: config.ManagementConfig{Listen: "127.0.0.1:0"}}
	cfg.SetFilePath(cfgPath)
	srv.SetConfig(cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/management/api-keys", strings.NewReader(`{"role":"read"}`))
	req.Header.Set("Content-Type", "application/json")
	// open mode admin principal via middleware
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthPasswordRevealAndLongSessionPersist(t *testing.T) {
	srv := newAPIKeyTestServer(t, nil, "secret-pass")
	dir := t.TempDir()
	cfgPath := dir + "/config.yaml"
	if err := os.WriteFile(cfgPath, []byte("mode: pool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Management: config.ManagementConfig{Password: "secret-pass", Listen: "127.0.0.1:0"}}
	cfg.SetFilePath(cfgPath)
	srv.SetConfig(cfg)

	// login
	req := httptest.NewRequest(http.MethodPost, "/api/auth", strings.NewReader(`{"password":"secret-pass"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %d %s", rec.Code, rec.Body.String())
	}
	var auth map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &auth)
	token, _ := auth["token"].(string)
	if token == "" {
		t.Fatal("empty token")
	}
	// cookie max-age roughly 30d
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "session_token" {
			found = true
			if c.MaxAge < int((20 * 24 * time.Hour).Seconds()) {
				t.Fatalf("expected long max-age, got %d", c.MaxAge)
			}
		}
	}
	if !found {
		t.Fatal("missing session cookie")
	}
	// session file written
	store := dir + "/.sessions.json"
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("session store missing: %v", err)
	}

	// reveal password
	req = httptest.NewRequest(http.MethodGet, "/api/auth/password", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("password %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "secret-pass") {
		t.Fatalf("password not returned: %s", rec.Body.String())
	}

	// reload sessions from disk into a new map via load
	srv.sessionMu.Lock()
	srv.sessions = make(map[string]*Session)
	srv.sessionMu.Unlock()
	srv.loadSessionsFromDisk()
	if !srv.validateSession(token) {
		t.Fatal("session should survive reload from disk")
	}
}
