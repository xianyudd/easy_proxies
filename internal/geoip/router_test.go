package geoip

import (
	"context"
	"log"
	"net"
	"io"
	"testing"
)

func TestRouterStartReturnsBindError(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	router := NewRouter(RouterConfig{Listen: "127.0.0.1", Port: port}, log.New(io.Discard, "", 0))
	if err := router.Start(context.Background()); err == nil {
		t.Fatal("expected bind error, got nil")
	}
}
