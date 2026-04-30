package cli

import "testing"

func TestRecommendedVMMemoryMiB(t *testing.T) {
	cases := []struct {
		name     string
		host     int
		execMode bool
		want     int64
	}{
		// container mode
		// container mode
		{"detection failed", 0, false, 3072},
		{"negative invalid", -1, false, 3072},
		{"low-end 4GB", 4, false, 3072},
		{"8GB MacBook", 8, false, 3072},
		{"9GB edge", 9, false, 4096},
		{"16GB laptop", 16, false, 4096},
		{"31GB upper mid", 31, false, 4096},
		{"32GB workstation", 32, false, 6144},
		{"64GB power user", 64, false, 6144},
		// exec mode: ⅔ of container tier (workers share FPM, no per-worker containers)
		{"detection failed exec", 0, true, 2048},
		{"negative invalid exec", -1, true, 2048},
		{"low-end 4GB exec", 4, true, 2048},
		{"8GB MacBook exec", 8, true, 2048},
		{"9GB edge exec", 9, true, 3072},
		{"16GB laptop exec", 16, true, 3072},
		{"31GB upper mid exec", 31, true, 3072},
		{"32GB workstation exec", 32, true, 4096},
		{"64GB power user exec", 64, true, 4096},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := recommendedVMMemoryMiB(c.host, c.execMode); got != c.want {
				t.Errorf("recommendedVMMemoryMiB(%d, execMode=%v) = %d, want %d", c.host, c.execMode, got, c.want)
			}
		})
	}
}
