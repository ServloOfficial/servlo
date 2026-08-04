//go:build darwin

package cli

func serviceStartHint(unit string) string {
	return "servlo start"
}

func serviceStatusHint(unit string) string {
	return "servlo start  |  check: launchctl print gui/$(id -u)/com.servlo." + unit
}

func dnsRestartHint() string {
	return "run 'servlo install' to reconfigure DNS"
}

func podmanDaemonHint() string {
	return "podman machine start"
}
