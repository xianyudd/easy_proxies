package geoip

import (
	"context"
	"log"
	"runtime"
	"testing"
	"time"
)

func TestRouterStopCancelsWatcherAndIsIdempotent(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		router := NewRouter(RouterConfig{Listen: "127.0.0.1", Port: 0}, log.Default())
		if err := router.Start(context.Background()); err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
		if err := router.Stop(); err != nil {
			t.Fatalf("Stop returned error: %v", err)
		}
		if err := router.Stop(); err != nil {
			t.Fatalf("second Stop returned error: %v", err)
		}
		if router.server != nil || router.stopCancel != nil {
			t.Fatalf("router retained lifecycle handles after Stop: server=%v cancel=%v", router.server, router.stopCancel)
		}
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+3 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("router start/stop leaked goroutines: before=%d after=%d", before, runtime.NumGoroutine())
}
