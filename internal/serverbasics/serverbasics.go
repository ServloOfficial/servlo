// Package serverbasics is the four things a fresh droplet needs before it is a
// server anyone should put a site on: swap, a known timezone, automatic security
// updates, and fail2ban in front of SSH.
//
// Every one of them needs root, and servlo never runs sudo on its own behalf.
// So this package only ever decides and describes: it produces the exact
// commands, the install prints them, `servlo bootstrap --system` runs them when
// it is already root, and doctor re-checks them on every run because a machine
// drifts and a rebuild forgets.
//
// One thing this package will not do, ever: touch SSH password authentication.
// Hardening someone out of their own droplet is not a service. Servlo adds
// fail2ban in front of SSH and leaves the login method to the operator.
package serverbasics

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Plan is one basic, whether it already holds, and what a human has to run if
// not. The shape mirrors internal/ports deliberately: the install and doctor
// treat every privileged step the same way.
type Plan struct {
	// Name is what doctor and the install call this step.
	Name string
	// Satisfied means the host already does this and Commands is empty.
	Satisfied bool
	// Commands are the privileged operations themselves, written without sudo
	// so that bootstrap, which is already root, runs exactly the operation. The
	// sudo prefix belongs to the printed form and is added by ForHuman.
	Commands []string
	// Detail explains the state in one line, for the operator being asked to
	// run the commands or being told why nothing happened.
	Detail string
}

// swapFilePath is where the swap file goes. /swapfile is the conventional
// location and the one every guide and support answer assumes.
const swapFilePath = "/swapfile"

// swapCap is the most swap servlo will configure. Past this it stops helping: a
// machine swapping 16GB is thrashing, and the fix is fewer sites rather than
// more disk.
const swapCap = 8 * 1024 * 1024 * 1024

// swapSizeFor picks a swap size from installed memory.
//
// The small end is where this matters. A 1GB droplet is the one that falls
// over: composer resolving a dependency tree or an npm build exhausts it, and
// with no swap the OOM killer takes MySQL rather than the build. Doubling the
// RAM there buys real headroom. From 2GB to 8GB, matching RAM is the
// conventional ratio and enough.
func swapSizeFor(ram int64) int64 {
	const twoGiB = 2 * 1024 * 1024 * 1024
	switch {
	case ram <= 0:
		return 0
	case ram < twoGiB:
		// Double, because this is the band that runs out.
		return 2 * ram
	case ram <= swapCap:
		return ram
	default:
		return swapCap
	}
}

// planSwap decides whether this machine needs a swap file.
func planSwap(ram, existingSwap int64) Plan {
	p := Plan{Name: "swap"}
	if existingSwap > 0 {
		p.Satisfied = true
		p.Detail = fmt.Sprintf("%s of swap already configured", humanBytes(existingSwap))
		return p
	}
	if ram <= 0 {
		p.Detail = "could not read this machine's memory, so no swap size was chosen"
		return p
	}

	size := swapSizeFor(ram)
	p.Detail = fmt.Sprintf("no swap; %s of RAM suggests a %s swap file", humanBytes(ram), humanBytes(size))
	p.Commands = []string{
		fmt.Sprintf("fallocate -l %d %s", size, swapFilePath),
		// Owner-only before it is ever swapped to. A world-readable swap file
		// hands every local process the memory of every other one, which on a
		// box running several sites means one compromised site can read
		// another's secrets straight out of it.
		fmt.Sprintf("chmod 600 %s", swapFilePath),
		fmt.Sprintf("mkswap %s", swapFilePath),
		fmt.Sprintf("swapon %s", swapFilePath),
		// Persistence is the half that gets forgotten. swapon works until the
		// reboot that undoes it, and then the machine is back to being
		// OOM-killed with nothing in the logs to explain it.
		fmt.Sprintf("echo '%s none swap sw 0 0' >> /etc/fstab", swapFilePath),
	}
	return p
}

// SwapPlan reads this machine and plans its swap.
func SwapPlan() Plan { return planSwap(totalMemory(), activeSwap()) }

// totalMemory returns installed RAM in bytes, or 0 when it cannot be read.
// Guessing would be worse than doing nothing: a made-up figure could size an
// 8GB swap file onto a 10GB disk.
func totalMemory() int64 {
	return meminfoValue("MemTotal:")
}

// activeSwap returns how much swap is already on, in bytes.
func activeSwap() int64 {
	return meminfoValue("SwapTotal:")
}

// meminfoPath is a var so tests can point it at a fixture.
var meminfoPath = "/proc/meminfo"

func meminfoValue(key string) int64 {
	data, err := os.ReadFile(meminfoPath)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, key) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		// meminfo reports kB.
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// ForHuman renders the commands the way an operator types them, which is with
// sudo in front. Kept separate from Commands so the executed form and the
// printed form cannot drift, and so root never shells out to sudo just to
// re-acquire a privilege it already has.
func (p Plan) ForHuman() []string {
	out := make([]string, 0, len(p.Commands))
	for _, c := range p.Commands {
		out = append(out, "sudo "+c)
	}
	return out
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.0f%c", float64(n)/float64(div), "KMGT"[exp])
}
