package monitor

import "testing"

func TestExtractorSnapshotMatchesRegionExtendedAliases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		snap   Snapshot
		region string
		want   bool
	}{
		{
			name:   "switzerland by exact region",
			snap:   Snapshot{NodeInfo: NodeInfo{Region: "ch", Name: "瑞士苏黎世", Country: "Switzerland"}},
			region: "ch",
			want:   true,
		},
		{
			name:   "india by alias fallback",
			snap:   Snapshot{NodeInfo: NodeInfo{Name: "印度孟买", Country: "India"}},
			region: "in",
			want:   true,
		},
		{
			name:   "germany by name alias",
			snap:   Snapshot{NodeInfo: NodeInfo{Name: "德国DE-HY2"}},
			region: "de",
			want:   true,
		},
		{
			name:   "uk by name alias",
			snap:   Snapshot{NodeInfo: NodeInfo{Name: "英国-优化2"}},
			region: "gb",
			want:   true,
		},
		{
			name:   "canada excluded from other",
			snap:   Snapshot{NodeInfo: NodeInfo{Name: "加拿大-优化"}},
			region: "other",
			want:   false,
		},
		{
			name:   "other excludes extended regions",
			snap:   Snapshot{NodeInfo: NodeInfo{Region: "ae", Name: "迪拜"}},
			region: "other",
			want:   false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := extractorSnapshotMatchesRegion(tc.snap, tc.region); got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}
