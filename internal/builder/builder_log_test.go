package builder

import (
	"bytes"
	"log"
	"reflect"
	"strings"
	"testing"

	"easy_proxies/internal/config"
)

// captureLog runs fn with the package-level log writer redirected to a buffer
// and returns what was written. 本辅助函数仅用于断言日志输出的测试；这些测试
// 不得使用 t.Parallel，以避免与其他并行测试共享全局 log writer。
func captureLog(fn func()) string {
	var buf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	}()
	fn()
	return buf.String()
}

// logTestConfig returns a small self-contained hybrid-mode config whose build
// exercises both the pool and multi-port entry points. URIs point at localhost
// so Build performs no network I/O.
func logTestConfig(level string) *config.Config {
	return &config.Config{
		Mode:     "hybrid",
		LogLevel: level,
		Listener: config.ListenerConfig{Address: "127.0.0.1", Port: 12080},
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			Username: "u",
			Password: "p",
		},
		Pool:  config.PoolConfig{Mode: "sequential"},
		GeoIP: config.GeoIPConfig{Enabled: false},
		Nodes: []config.NodeConfig{
			{Name: "alpha", URI: "http://127.0.0.1:18080", Port: 20001},
			{Name: "beta", URI: "http://127.0.0.1:18081", Port: 20002},
		},
	}
}

func TestBuildDefaultLogLevelKeepsSummaryAndSuppressesPerNodeLines(t *testing.T) {
	out := captureLog(func() {
		opts, err := Build(logTestConfig("warn"))
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		if len(opts.Outbounds) == 0 {
			t.Fatal("Build returned no outbounds")
		}
	})

	// 摘要行必须保留
	if !strings.Contains(out, "🔌 Multi-Port Entry Points: 2 nodes") {
		t.Fatalf("multi-port summary line missing; got:\n%s", out)
	}
	if !strings.Contains(out, "🌐 Pool Entry Point:") {
		t.Fatalf("pool summary line missing; got:\n%s", out)
	}
	// 整页横幅与 per-node 行不得出现
	if strings.Contains(out, "📡 Proxy Links:") {
		t.Fatalf("full banner header must be suppressed at default level; got:\n%s", out)
	}
	if strings.Contains(out, "       HTTP:   http://") {
		t.Fatalf("per-node HTTP lines must be suppressed at default level; got:\n%s", out)
	}
	if strings.Contains(out, "       SOCKS5: socks5://") {
		t.Fatalf("per-node SOCKS5 lines must be suppressed at default level; got:\n%s", out)
	}
	if strings.Contains(out, "   • ") {
		t.Fatalf("per-node pool bullets must be suppressed at default level; got:\n%s", out)
	}
}

func TestBuildDebugLogLevelPrintsFullPerNodeBanner(t *testing.T) {
	out := captureLog(func() {
		if _, err := Build(logTestConfig("debug")); err != nil {
			t.Fatalf("Build failed: %v", err)
		}
	})

	if !strings.Contains(out, "📡 Proxy Links:") {
		t.Fatalf("full banner header must appear at debug level; got:\n%s", out)
	}
	if !strings.Contains(out, "🔌 Multi-Port Entry Points (2 nodes):") {
		t.Fatalf("multi-port banner header must appear at debug level; got:\n%s", out)
	}
	if !strings.Contains(out, "       HTTP:   http://") {
		t.Fatalf("per-node HTTP lines must appear at debug level; got:\n%s", out)
	}
	if !strings.Contains(out, "   [20001] alpha") {
		t.Fatalf("per-node port line must appear at debug level; got:\n%s", out)
	}
}

func TestBuildResultIndependentOfLogLevel(t *testing.T) {
	optsWarn, err := Build(logTestConfig("warn"))
	if err != nil {
		t.Fatalf("warn build failed: %v", err)
	}
	optsDebug, err := Build(logTestConfig("debug"))
	if err != nil {
		t.Fatalf("debug build failed: %v", err)
	}

	// LogOptions.Level 是配置的透传字段，属于预期差异；其余构建产物
	// （inbounds/outbounds/route/DNS）必须与日志级别无关。
	optsWarn.Log.Level = ""
	optsDebug.Log.Level = ""

	if !reflect.DeepEqual(optsWarn, optsDebug) {
		t.Fatal("build output must not depend on log level")
	}
}
