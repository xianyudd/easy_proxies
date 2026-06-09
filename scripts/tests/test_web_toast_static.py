from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
TOAST = ROOT / "web" / "src" / "components" / "ui" / "Toast.tsx"


def test_toast_replaces_clear_timer_when_messages_arrive_quickly():
    text = TOAST.read_text()
    assert "let toastTimer: number | undefined" in text
    assert "window.clearTimeout(toastTimer)" in text
    assert "toastTimer = window.setTimeout" in text
    assert "toastTimer = undefined" in text
    assert "window.setTimeout(() => set({ message: '' }), 2600)" not in text


if __name__ == "__main__":
    test_toast_replaces_clear_timer_when_messages_arrive_quickly()
