package ports

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// unprivStartKey is the sysctl servlo pins, spelled once.
const unprivStartKey = "net.ipv4.ip_unprivileged_port_start"

// What actually decides the value at the next boot.
//
// systemd-sysctl reads every *.conf across the sysctl.d directories, sorts them
// by filename regardless of which directory they came from, and lets the
// lexicographically latest one win. A file of the same name in an earlier
// directory masks the later one entirely, which is how a distribution default is
// switched off rather than edited.
//
// servlo's drop-in is 99-servlo-ports.conf, and Ubuntu ships
// /etc/sysctl.d/99-sysctl.conf as a symlink to /etc/sysctl.conf. That name sorts
// after servlo's, so the single most obvious file for an operator to edit, and
// the one a hardening guide names, decides this setting rather than servlo's.
// Reading only servlo's own file answers whether servlo's drop-in is present and
// permissive, which is not the question Persistent claims to answer.

// sysctlDirs are the drop-in directories, in the order that resolves a name
// collision: the first one holding a given filename is the one that is read.
var sysctlDirs = []string{"/etc/sysctl.d", "/run/sysctl.d", "/usr/lib/sysctl.d"}

// listSysctlFiles returns every drop-in on this host, as a seam for tests.
var listSysctlFiles = defaultListSysctlFiles

func defaultListSysctlFiles() []string {
	var out []string
	for _, dir := range sysctlDirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.conf"))
		if err != nil {
			continue
		}
		out = append(out, matches...)
	}
	return out
}

// unprivStartSetter returns the file whose value the next boot would apply, and
// that value. found is false when nothing on the host sets the key, which is the
// ordinary state of a machine servlo has not been installed on.
func unprivStartSetter() (path string, value int, found bool) {
	// Earlier directory wins for the same filename. Ranked by where the file's
	// directory sits in sysctlDirs rather than by the order the listing happens
	// to arrive in, so the answer does not depend on how the paths were gathered.
	byName := map[string]string{}
	rank := map[string]int{}
	for _, p := range listSysctlFiles() {
		name := filepath.Base(p)
		r := dirRank(p)
		if seen, ok := rank[name]; ok && seen <= r {
			continue
		}
		byName[name], rank[name] = p, r
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	// Last name wins, so walk backwards and stop at the first file that sets it.
	for i := len(names) - 1; i >= 0; i-- {
		p := byName[names[i]]
		body, err := readTextFile(p)
		if err != nil {
			continue
		}
		if v, ok := lastUnprivStartIn(string(body)); ok {
			return p, v, true
		}
	}
	return "", 0, false
}

// lastUnprivStartIn returns the value the key is set to in one file. The last
// assignment wins within a file, the same way it does between them.
func lastUnprivStartIn(body string) (int, bool) {
	value, found := 0, false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(strings.TrimPrefix(key, "-")) != unprivStartKey {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		value, found = v, true
	}
	return value, found
}

// dirRank is a path's position in the search order. A path outside the known
// directories ranks last, so a real drop-in always beats it.
func dirRank(path string) int {
	dir := filepath.Dir(path)
	for i, d := range sysctlDirs {
		if dir == d {
			return i
		}
	}
	return len(sysctlDirs)
}
