from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
APP = ROOT / "web" / "src" / "App.tsx"
STORE = ROOT / "web" / "src" / "store" / "appStore.ts"


def read(path: Path) -> str:
    return path.read_text()


def test_web_does_not_render_protected_app_before_auth_probe_resolves():
    app = read(APP)
    store = read(STORE)
    assert "authenticated: 'unknown'" in store
    assert "type AuthState = 'unknown'|'authenticated'|'unauthenticated'" in store
    assert "getAuthStatus" in app
    assert "getNodesSummary" not in app
    assert "queryClient.setQueryData(['auth-probe']" in app
    assert "authenticated: true" in app
    assert "enabled: authenticated !== 'unauthenticated'" in app
    assert "authProbe.isLoading || authProbe.isFetching" in app
    assert "if (authenticated === 'unknown' || verifyingAuth)" in app
    assert "return <LoginPage />" in app


def test_web_handles_session_expiry_from_any_api_401():
    app = read(APP)
    client = read(ROOT / "web" / "src" / "api" / "client.ts")
    assert "easy-proxies:unauthorized" in client
    assert "window.dispatchEvent(new CustomEvent(UNAUTHORIZED_EVENT" in client
    assert "res.status === 401" in client
    assert "UNAUTHORIZED_EVENT" in app
    assert "queryClient.clear()" in app
    assert "setAuthenticated('unauthenticated')" in app
    assert "window.addEventListener(UNAUTHORIZED_EVENT" in app
    assert "window.removeEventListener(UNAUTHORIZED_EVENT" in app


if __name__ == "__main__":
    test_web_does_not_render_protected_app_before_auth_probe_resolves()
    test_web_handles_session_expiry_from_any_api_401()
