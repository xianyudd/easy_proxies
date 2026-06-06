package quality

import "testing"

func TestClassifyResult(t *testing.T) {
	tests := []struct {
		name string
		in   Result
		want Tier
		pool string
	}{
		{
			name: "reject on quick fail",
			in:   Result{Quick: map[string]any{"status": "failed"}},
			want: TierReject,
			pool: "reject_pool",
		},
		{
			name: "rescue on quick only",
			in:   Result{Quick: map[string]any{"status": "ok"}, Success: true},
			want: TierRescue,
			pool: "rescue_pool",
		},
		{
			name: "http only without https",
			in:   Result{Quick: map[string]any{"status": "ok"}, Success: true, CF: map[string]any{"score": 10}, Reputation: map[string]any{"risk_level": "medium"}},
			want: TierHTTPOnly,
			pool: "http_pool",
		},
		{
			name: "recommended with cf and low risk",
			in:   Result{Quick: map[string]any{"status": "ok"}, Success: true, CF: map[string]any{"score": 75, "trace_ok": true}, Reputation: map[string]any{"risk_level": "low"}},
			want: TierRecommended,
			pool: "recommended_pool",
		},
		{
			name: "premium with high cf and low risk",
			in:   Result{Quick: map[string]any{"status": "ok"}, Success: true, CF: map[string]any{"score": 90, "trace_ok": true}, Reputation: map[string]any{"risk_level": "low"}},
			want: TierPremium,
			pool: "strict_pool",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyResult(tc.in)
			if got.Tier != tc.want || got.Pool != tc.pool {
				t.Fatalf("got %#v, want tier=%q pool=%q", got, tc.want, tc.pool)
			}
		})
	}
}
