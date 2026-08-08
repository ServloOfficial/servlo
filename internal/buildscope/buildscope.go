// Package buildscope runs an asset build inside a memory-capped systemd scope.
//
// The failure this exists to prevent: `npm run build` on a 2GB droplet is the
// single hungriest thing servlo ever runs, and when it exhausts the machine the
// kernel's OOM killer picks a victim by its own arithmetic. That victim is
// routinely MySQL, because MySQL is the biggest resident process on the box. So
// one site's oversized build takes every other site's database down with it,
// and the operator sees a database outage rather than a failed deploy.
//
// A scope with MemoryMax moves that decision. The build gets its own cgroup, it
// is the only thing in it, and when it hits the ceiling the cgroup's OOM killer
// kills the build. One deploy fails, loudly, and nothing else on the machine
// notices.
package buildscope

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// MB is a mebibyte, spelled out because every size here is in them.
const MB int64 = 1 << 20

// The cap, as a share of the machine.
//
// Half, floored at 512MB and ceilinged at 4GB. Half leaves the other half for
// the databases, the PHP pools and nginx, which is what has to survive. The
// floor is there because a build capped below 512MB fails on almost any real
// front end, and refusing to build at all is not an improvement on failing
// under load. The ceiling is because past a few gigabytes an asset build is not
// memory hungry, it is broken, and letting it take 8GB on a large droplet only
// delays finding that out.
const (
	shareNumerator   = 1
	shareDenominator = 2
	minLimit         = 512 * MB
	maxLimit         = 4096 * MB
	// unknownHostLimit is used when the machine's memory cannot be read. A
	// conservative number rather than no cap: not knowing the size of the
	// machine is not a reason to let a build have all of it.
	unknownHostLimit = 512 * MB
)

// LimitFor is the memory ceiling for a build on a machine with total bytes of
// RAM.
func LimitFor(total int64) int64 {
	if total <= 0 {
		return unknownHostLimit
	}
	limit := total * shareNumerator / shareDenominator
	return min(max(limit, minLimit), maxLimit)
}

// HostLimit is the ceiling for this machine.
func HostLimit() int64 { return LimitFor(hostTotalRAM()) }

// hostTotalRAM reads MemTotal from /proc/meminfo, in bytes, and returns 0 when
// it cannot.
// HostTotalRAM is this machine's memory in bytes, or 0 when it cannot be read.
func HostTotalRAM() int64 { return hostTotalRAM() }

func hostTotalRAM() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// scopeAvailable reports whether a transient scope can be started, indirected
// so a test does not need a systemd user session.
var scopeAvailable = func() bool {
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return false
	}
	// Present is not the same as usable. In a container with no user manager
	// the binary exists and every invocation fails on the bus, so ask it to do
	// nothing and see whether it can.
	return exec.Command("systemd-run", "--user", "--scope", "--quiet", "--collect", "--", "true").Run() == nil
}

// Available reports whether builds on this machine can be confined.
func Available() bool { return scopeAvailable() }

// unitSafe replaces everything a systemd unit name cannot carry.
var unitSafe = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// UnitName is the scope a site's build runs in. Per site, so two sites building
// at once are two scopes with two ceilings rather than one shared one.
func UnitName(site string) string {
	return "servlo-build-" + unitSafe.ReplaceAllString(site, "-")
}

// Wrap returns cmd confined to a memory-capped scope, or cmd unchanged when
// this machine cannot provide one.
//
// Unchanged rather than refused. A container without a user manager still has
// to be able to deploy, and a panel that stopped building assets because it
// could not confine them would be trading a rare failure for a certain one.
func Wrap(cmd *exec.Cmd, limit int64, unit string) *exec.Cmd {
	if limit <= 0 || !scopeAvailable() {
		return cmd
	}

	args := []string{
		"--user", "--scope", "--quiet",
		// Without --collect the transient unit sticks around after a failure,
		// and the next build with the same name refuses to start.
		"--collect",
		"--unit=" + unit,
		"-p", fmt.Sprintf("MemoryMax=%d", limit),
		// No swap. With swap the build does not fail at the ceiling, it starts
		// paging, and a droplet thrashing for twenty minutes is worse for every
		// site on it than one deploy failing in two. The swap file exists so the
		// system survives pressure, not so one build can ignore its limit.
		"-p", "MemorySwapMax=0",
		"--",
	}
	args = append(args, cmd.Args...)

	scoped := exec.Command("systemd-run", args...)
	scoped.Dir = cmd.Dir
	scoped.Env = cmd.Env
	scoped.Stdin = cmd.Stdin
	scoped.Stdout = cmd.Stdout
	scoped.Stderr = cmd.Stderr
	return scoped
}

// OutOfMemory reports whether err is the build having been stopped at its
// ceiling. Ask it only about a command Wrap actually confined: outside a scope
// these same signals mean somebody killed the build by hand.
//
// Worth telling apart from any other failure. Every other build failure is
// something in the code, and the operator reads the output to find it; this one
// leaves no useful output at all, just a process that stopped, and the fix is a
// bigger machine or a smaller build rather than another try.
//
// Two shapes, because a cgroup memory kill arrives as either. The kernel picks
// a task inside the cgroup and SIGKILLs it, which reaches the caller as a
// signalled death or, through a shell, as exit 137. systemd then applies the
// scope's OOM policy, and where that policy is to stop the unit it SIGTERMs
// whatever is still in it, which arrives as a signal or as exit 143. Which of
// the two the deploy ends up waiting on depends on whether the process it
// spawned was the one the kernel picked, and that is not something servlo gets
// to choose, so both are the same failure.
func OutOfMemory(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}
	if status.Signaled() {
		return status.Signal() == syscall.SIGKILL || status.Signal() == syscall.SIGTERM
	}
	// The same two, as a shell reports them: 128 plus the signal.
	return status.ExitStatus() == 137 || status.ExitStatus() == 143
}

// packageManagers are the commands that build a front end. Toolchain names, not
// framework names: servlo never branches on whether a site is Laravel, but the
// difference between a deploy that compiles assets and one that does not is a
// property of the script an operator wrote, and this is how it shows.
var packageManagers = []string{"npm", "yarn", "pnpm", "bun"}

// buildsAssets reports whether a deploy script compiles a front end.
//
// Commented-out lines do not count, the same way they do not count when
// deciding whether a deploy migrates: a build somebody commented out would
// otherwise warn on every deploy from then on.
func buildsAssets(script string) bool {
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, field := range strings.Fields(line) {
			// Whole words, so `echo npmisgreat` is not a build. Environment
			// assignments ahead of the command are skipped by looking at every
			// field rather than only the first.
			if slices.Contains(packageManagers, strings.TrimPrefix(field, "./")) {
				return true
			}
		}
	}
	return false
}

// Warning is what to tell the operator before a build starts on a machine that
// may be too small for it, or empty when there is nothing worth saying.
//
// Before rather than after, because after is a failed deploy and the useful
// moment has passed. It fires only when both halves are true: this machine is
// at the floor of what servlo will allocate, and this deploy actually compiles
// something. A warning on every deploy is a warning nobody reads.
func Warning(total int64, script string) string {
	limit := LimitFor(total)
	if limit > minLimit || !buildsAssets(script) {
		return ""
	}
	return fmt.Sprintf(
		"This machine is small, so the asset build is capped at %dMB. If it needs more it will be "+
			"stopped there rather than taking the rest of the server with it, and the deploy will fail. "+
			"Building somewhere else and deploying the output avoids that.",
		limit/MB)
}
