package serverguard

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// procNet writes a /proc/net/tcp table. Column 2 is the local address as
// kernel hex, column 4 is the socket state, and 0A is LISTEN.
func procNet(t *testing.T, rows ...string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "tcp")
	head := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	if err := os.WriteFile(p, []byte(head+joinLines(rows)), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func joinLines(rows []string) string {
	out := ""
	for _, r := range rows {
		out += r + "\n"
	}
	return out
}

// A port bound to 0.0.0.0 is reachable from the internet if nothing else stops
// it, and that is the fact worth reporting. One on 127.0.0.1 is not, and
// listing it would bury the ones that matter.
func TestListening_ReportsWhatFacesTheWorld(t *testing.T) {
	// 00000000:0050 is 0.0.0.0:80, 0100007F:1F90 is 127.0.0.1:8080.
	path := procNet(t,
		"   0: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1",
		"   1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 2",
		"   2: 00000000:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 3",
	)

	ports, err := listeningFrom([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ports, 80) || !slices.Contains(ports, 443) {
		t.Errorf("ports = %v, want the two that face the world", ports)
	}
	if slices.Contains(ports, 8080) {
		t.Errorf("ports = %v, includes a loopback-only listener", ports)
	}
}

// A socket that is connected rather than listening is somebody's request in
// flight, not an open door.
func TestListening_IgnoresConnectionsThatAreNotListening(t *testing.T) {
	// 01 is ESTABLISHED.
	path := procNet(t,
		"   0: 00000000:1F91 0100007F:D431 01 00000000:00000000 00:00000000 00000000     0        0 4",
	)
	ports, err := listeningFrom([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 0 {
		t.Errorf("ports = %v, want none: nothing there is listening", ports)
	}
}

// The same port on IPv4 and IPv6 is one open door, not two.
func TestListening_CountsAPortOnce(t *testing.T) {
	v4 := procNet(t, "   0: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1")
	v6 := procNet(t, "   0: 00000000000000000000000000000000:0050 "+
		"00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 2")

	ports, err := listeningFrom([]string{v4, v6})
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 || ports[0] != 80 {
		t.Errorf("ports = %v, want just 80", ports)
	}
}

// A server with IPv6 disabled has no tcp6 table, which is ordinary. Refusing
// to answer at all would hide the IPv4 ports it does have, which is the whole
// point of the check.
func TestListening_OneMissingAddressFamilyStillAnswers(t *testing.T) {
	v4 := procNet(t, "   0: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1")

	ports, err := listeningFrom([]string{v4, filepath.Join(t.TempDir(), "tcp6")})
	if err != nil {
		t.Fatalf("a server with no IPv6 could not be checked at all: %v", err)
	}
	if len(ports) != 1 || ports[0] != 80 {
		t.Errorf("ports = %v, want the IPv4 answer", ports)
	}
}

// None of them readable is different. Reporting no open ports because nothing
// could be read is the most reassuring possible lie.
func TestListening_NoTableAtAllIsAnErrorNotAnEmptyAnswer(t *testing.T) {
	dir := t.TempDir()
	if _, err := listeningFrom([]string{filepath.Join(dir, "tcp"), filepath.Join(dir, "tcp6")}); err == nil {
		t.Fatal("a server whose tables could not be read reported no listening ports")
	}
}

// The whole of 127.0.0.0/8 is loopback, not just 127.0.0.1, and the address
// that matters is 127.0.0.53, where the local DNS stub listens on every Ubuntu
// 24.04 machine, which is the only platform servlo installs on. Matching the
// two fixed spellings of "localhost" reported that one as facing the world, so
// the security page listed port 53 as open on every install servlo has done.
//
// A false entry on that page costs more than the page being empty. It is the
// one screen whose whole job is to say what is exposed, and an operator who
// learns that its list always has something in it stops reading it.
func TestListening_KnowsTheRestOfLoopback(t *testing.T) {
	// 3500007F:0035 is 127.0.0.53:53, 0100007F:1F90 is 127.0.0.1:8080,
	// 00000000:0050 is 0.0.0.0:80 and 0A01A8C0:22B8 is 192.168.1.10:8888.
	path := procNet(t,
		"   0: 3500007F:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1",
		"   1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 2",
		"   2: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 3",
		"   3: 0A01A8C0:22B8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 4",
	)

	ports, err := listeningFrom([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ports, 53) {
		t.Errorf("ports = %v, and 53 is the local DNS stub on 127.0.0.53, which nothing outside can reach", ports)
	}
	if slices.Contains(ports, 8080) {
		t.Errorf("ports = %v, includes 127.0.0.1", ports)
	}
	if !slices.Contains(ports, 80) || !slices.Contains(ports, 8888) {
		t.Errorf("ports = %v, want the two that are reachable from off the machine", ports)
	}
}

// The same in the other table. An IPv4 address held in an IPv6 socket is how a
// dual-stack listener shows up, and the mapped form of 127.0.0.1 is still
// 127.0.0.1.
func TestListening_KnowsLoopbackInTheIPv6Table(t *testing.T) {
	// ::1, then ::ffff:127.0.0.1, then :: which is every address there is.
	path := procNet(t,
		"   0: 00000000000000000000000001000000:0BB8 "+ipv6Zero+":0000 0A 00000000:00000000 00:00000000 00000000     0        0 1",
		"   1: 0000000000000000FFFF00000100007F:0BB9 "+ipv6Zero+":0000 0A 00000000:00000000 00:00000000 00000000     0        0 2",
		"   2: "+ipv6Zero+":0BBA "+ipv6Zero+":0000 0A 00000000:00000000 00:00000000 00000000     0        0 3",
	)

	ports, err := listeningFrom([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []int{3000, 3001} {
		if slices.Contains(ports, p) {
			t.Errorf("ports = %v, includes a loopback listener on %d", ports, p)
		}
	}
	if !slices.Contains(ports, 3002) {
		t.Errorf("ports = %v, want the one bound to every address", ports)
	}
}

const ipv6Zero = "00000000000000000000000000000000"
