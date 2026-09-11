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
	"encoding/hex"
	"fmt"
	"net"
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
// The address is decoded rather than compared as text. Matching the two fixed
// spellings of "localhost" missed the rest of 127.0.0.0/8, and the one that
// matters there is 127.0.0.53, where the local DNS stub Ubuntu runs listens on
// every machine, so port 53 was reported as facing the world on every install
// servlo has ever done. The same shortcut missed ::ffff:127.0.0.1, which is how
// a loopback listener on a dual-stack socket appears.
//
// An address that will not decode is reported rather than hidden, which is the
// direction to err in on a page whose whole job is to say what is exposed.
func loopbackOnly(addr string) bool {
	ip := parseProcAddr(addr)
	return ip != nil && ip.IsLoopback()
}

// parseProcAddr turns the hex address /proc writes into the address it means.
//
// The kernel writes it as 32-bit words in the host's byte order, so on every
// machine servlo runs on each word's four bytes arrive backwards: one word for
// IPv4, four for IPv6.
func parseProcAddr(hexAddr string) net.IP {
	if len(hexAddr) == 0 || len(hexAddr)%8 != 0 {
		return nil
	}
	raw, err := hex.DecodeString(hexAddr)
	if err != nil {
		return nil
	}
	if len(raw) != net.IPv4len && len(raw) != net.IPv6len {
		return nil
	}
	for w := 0; w < len(raw); w += 4 {
		raw[w], raw[w+1], raw[w+2], raw[w+3] = raw[w+3], raw[w+2], raw[w+1], raw[w]
	}
	return net.IP(raw)
}
