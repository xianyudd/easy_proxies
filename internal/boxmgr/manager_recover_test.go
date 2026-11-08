package boxmgr

import (
	"errors"
	"strings"
	"testing"

	"github.com/sagernet/sing-box"
)

// TestCallWithBoxNew_ConvertsPanicToError covers the real recover path used by
// newBoxRecover: a panic during sing-box initialization must become an error
// instead of crashing the process. Malformed subscription nodes can make the
// sing-box library panic while formatting its own error (stringifying a
// *string); without the guard a single bad node crashes the whole service.
func TestCallWithBoxNew_ConvertsPanicToError(t *testing.T) {
	instance, err := callWithBoxNew(func() (*box.Box, error) {
		panic("unknown value")
	})
	if err == nil {
		t.Fatalf("expected error from panic, got nil")
	}
	if instance != nil {
		t.Fatalf("expected nil instance after panic, got %v", instance)
	}
	if !strings.Contains(err.Error(), "sing-box panicked during initialization") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestCallWithBoxNew_PassesThroughNormalError(t *testing.T) {
	sentinel := errors.New("boom")
	instance, err := callWithBoxNew(func() (*box.Box, error) {
		return nil, sentinel
	})
	if err != sentinel {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if instance != nil {
		t.Fatalf("expected nil instance, got %v", instance)
	}
}

func TestCallWithBoxNew_NoErrorOnSuccess(t *testing.T) {
	instance, err := callWithBoxNew(func() (*box.Box, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if instance != nil {
		t.Fatalf("expected nil instance, got %v", instance)
	}
}