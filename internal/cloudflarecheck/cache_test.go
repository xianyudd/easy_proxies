package cloudflarecheck

import (
	"testing"
	"time"
)

func TestCacheGetSetCopy(t *testing.T) {
	cache := NewCache(time.Hour)
	cache.Set("node", Result{NodeTag: "node", Score: 88})
	got, ok := cache.Get("node")
	if !ok || !got.Cached || got.Score != 88 {
		t.Fatalf("unexpected cache hit: ok=%v %#v", ok, got)
	}
	got.Score = 1
	got2, _ := cache.Get("node")
	if got2.Score == 1 {
		t.Fatal("cache returned shared mutable value")
	}
}

func TestCacheClear(t *testing.T) {
	cache := NewCache(time.Hour)
	cache.Set("node", Result{NodeTag: "node"})
	cache.Clear()
	if _, ok := cache.Get("node"); ok {
		t.Fatal("expected cache clear")
	}
}
