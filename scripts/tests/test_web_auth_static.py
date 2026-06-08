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
    assert "enabled: authenticated === 'unknown' || authenticated === 'authenticated'" in app
    assert "if (authenticated === 'unknown')" in app
    assert "return <LoginPage />" in app


if __name__ == "__main__":
    test_web_does_not_render_protected_app_before_auth_probe_resolves()
