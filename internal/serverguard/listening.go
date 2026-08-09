// Package serverguard reports what this server looks like from outside, and
// prints the commands that change it.
//
// Almost everything here needs root: ufw, fail2ban and sshd all do. Servlo does
// not run as root and does not ask for it (CLAUDE.md §3.2), so the division is
// deliberate. What servlo can read for itself, it reads and reports honestly.
// What needs privilege, it writes out as the exact command for a person to run,
// and then checks the result afterwards.
//
// The one thing servlo can always determine without help is which ports are
// actually listening on an address the internet can reach, because the kernel
// publishes that in /proc. That is also the fact that matters most: a firewall
// rule is a claim, and an open socket is the truth.
package serverguard

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// procTables are where the kernel publishes TCP sockets, one per address
// family.
var procTables = []string{"/proc/net/tcp", "/proc/net/tcp6"}

// listenState is the value /proc uses for a socket in LISTEN.
const listenState = "0A"

// Listening is every TCP port on this server bound to an address other than
// loopback, sorted.
//
// Loopback listeners are left out on purpose. A service bound to 127.0.0.1 is
// not reachable from anywhere else, and servlo runs a great many of them, so
// listing them would bury the handful that are actually exposed.
func Listening() ([]int, error) { return listeningFrom(procTables) }

func listeningFrom(paths []string) ([]int, error) {
	seen := map[int]bool{}
	var read int
	var last error
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			// A missing table for one address family is ordinary: a server with
			// IPv6 disabled has no tcp6, and refusing to answer at all would
			// hide the IPv4 ports it does have.
			last = fmt.Errorf("reading %s: %w", path, err)
			continue
		}
		err = scanTable(f, seen)
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		read++
	}
	if read == 0 {
		// None of them. Reporting no open ports because nothing could be read
		// is the most reassuring possible lie.
		return nil, last
	}
	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports, nil
}

func scanTable(f *os.File, seen map[int]bool) error {
	scanner := bufio.NewScanner(f)
	scanner.Scan() // the header row
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[3] != listenState {
			continue
		}
		addr, portHex, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		if loopbackOnly(addr) {
			continue
		}
		port, err := strconv.ParseUint(portHex, 16, 32)
		if err != nil {
			continue
		}
		seen[int(port)] = true
	}
	return scanner.Err()
}

// loopbackOnly reports an address nothing outside this machine can reach.
//
// The kernel writes the address little-endian per word and in hex, so 127.0.0.1
// is 0100007F and ::1 is a run of zeroes ending in 01. Comparing the text is
// enough here: the two forms are fixed, and the alternative is decoding an
// address only to throw it away.
func loopbackOnly(addr string) bool {
	switch strings.ToUpper(addr) {
	case "0100007F", "00000000000000000000000001000000":
		return true
	}
	return false
}
