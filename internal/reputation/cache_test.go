package reputation

import (
	"testing"
	"time"
)

func TestCacheGetSetMasksCachedFlag(t *testing.T) {
	c := NewCache(time.Hour)
	c.Set("1.1.1.1", &Result{IP: "1.1.1.1", RiskScore: 10})
	got, ok := c.Get("1.1.1.1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !got.Cached {
		t.Fatal("expected cached flag")
	}
	got.RiskScore = 99
	got2, _ := c.Get("1.1.1.1")
	if got2.RiskScore == 99 {
		t.Fatal("cache returned shared pointer")
	}
}
