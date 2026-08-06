package serverbasics

import (
	"strings"
	"testing"
)

const gib = 1024 * 1024 * 1024

// The sizing rule, and why each band is where it is.
//
// A 1GB droplet is the one that actually falls over: composer resolving a
// dependency tree or an npm build will exhaust it, and without swap the OOM
// killer takes MySQL rather than the build. Double the RAM there buys real
// headroom. From 2GB to 8GB, matching RAM is the conventional ratio and enough.
// Above that, more swap stops helping: a machine with 16GB that is swapping
// 16GB is thrashing, and the fix is fewer sites, not more disk.
func TestSwapSizeFor(t *testing.T) {
	cases := []struct {
		ram  int64
		want int64
	}{
		{512 * 1024 * 1024, 1 * gib}, // under 1GB still gets a usable minimum
		{1 * gib, 2 * gib},
		{2 * gib, 2 * gib},
		{4 * gib, 4 * gib},
		{8 * gib, 8 * gib},
		{16 * gib, 8 * gib}, // capped
		{64 * gib, 8 * gib},
	}
	for _, c := range cases {
		if got := swapSizeFor(c.ram); got != c.want {
			t.Errorf("swapSizeFor(%d) = %d, want %d", c.ram, got, c.want)
		}
	}
}

// A droplet that already has swap is left alone. Adding a second swap file to a
// machine someone already configured is the kind of helpfulness that turns into
// a support thread.
func TestPlanSwap_ExistingSwapIsLeftAlone(t *testing.T) {
	plan := planSwap(4*gib, 2*gib)
	if !plan.Satisfied {
		t.Errorf("a machine with swap was planned for again: %v", plan.Commands)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("commands produced for a machine that already swaps: %v", plan.Commands)
	}
}

func TestPlanSwap_NoSwapProducesTheCommands(t *testing.T) {
	plan := planSwap(2*gib, 0)
	if plan.Satisfied {
		t.Fatal("a machine with no swap was reported satisfied")
	}
	joined := strings.Join(plan.Commands, "\n")
	for _, want := range []string{"fallocate", "mkswap", "swapon", "/etc/fstab"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the swap commands do not mention %q:\n%s", want, joined)
		}
	}
}

// The swap file has to be owner-only. A world-readable swap file hands every
// local process the memory of every other one, which on a box running several
// sites means one compromised site can read another's secrets out of it.
func TestPlanSwap_MakesTheFileOwnerOnly(t *testing.T) {
	joined := strings.Join(planSwap(2*gib, 0).Commands, "\n")
	if !strings.Contains(joined, "chmod 600") {
		t.Errorf("the swap file is not made owner-only:\n%s", joined)
	}
}

// Persistence is the half that gets forgotten: swapon works until the reboot
// that undoes it, and then the machine is back to being OOM-killed with nothing
// in the logs to say why.
func TestPlanSwap_PersistsAcrossAReboot(t *testing.T) {
	joined := strings.Join(planSwap(2*gib, 0).Commands, "\n")
	if !strings.Contains(joined, "fstab") {
		t.Errorf("swap is enabled but never persisted:\n%s", joined)
	}
}

// An unreadable meminfo is not a reason to guess. Sizing swap from a made-up
// RAM figure could allocate 8GB on a disk that has 10.
func TestPlanSwap_UnknownRAMDoesNothing(t *testing.T) {
	plan := planSwap(0, 0)
	if len(plan.Commands) != 0 {
		t.Errorf("swap was planned without knowing the machine's memory: %v", plan.Commands)
	}
	if plan.Detail == "" {
		t.Error("the plan does not say why it did nothing")
	}
}
