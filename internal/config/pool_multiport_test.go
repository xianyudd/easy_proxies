package config

import (
	"testing"
	"time"
)

func TestEffectiveMultiportDefaults(t *testing.T) {
	p := PoolConfig{FailureThreshold: 3, BlacklistDuration: 24 * time.Hour}
	if p.EffectiveMultiportFailureThreshold() != 3 {
		t.Fatalf("threshold inherit: got %d", p.EffectiveMultiportFailureThreshold())
	}
	if p.EffectiveMultiportBlacklistDuration() != 10*time.Minute {
		t.Fatalf("duration default: got %v", p.EffectiveMultiportBlacklistDuration())
	}
	p.MultiportFailureThreshold = 5
	p.MultiportBlacklistDuration = 2 * time.Minute
	if p.EffectiveMultiportFailureThreshold() != 5 {
		t.Fatalf("threshold override")
	}
	if p.EffectiveMultiportBlacklistDuration() != 2*time.Minute {
		t.Fatalf("duration override")
	}
}
