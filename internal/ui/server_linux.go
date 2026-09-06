//go:build linux

package ui

import (
	"os/exec"
	"strings"
)

func listActiveUnitsBySuffix(pattern, prefix string) []string {
	out, err := exec.Command("systemctl", "--user", "list-units", "--state=active",
		"--no-legend", "--plain", pattern).Output()
	if err != nil {
		return nil
	}
	var sites []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := strings.TrimSuffix(strings.TrimSuffix(fields[0], ".service"), ".timer")
		siteName := strings.TrimPrefix(unit, prefix)
		if siteName != unit && siteName != "" {
			sites = append(sites, siteName)
		}
	}
	return sites
}
