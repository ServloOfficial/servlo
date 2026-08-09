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
