package sitecron

import (
	"fmt"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/logcolor"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/systemd"
)

// unitPrefix is the shape of every cron unit's name. The site comes before the
// entry so the name cannot be read as a worker of a site named after the entry:
// the orphan-worker scan matches "servlo-<worker>-<site>", and a cron unit must
// never look like a worker nobody declared.
const unitPrefix = "servlo-cron-"

// UnitName is the systemd unit this entry's service and timer share.
func UnitName(siteName, id string) string {
	return unitPrefix + siteName + "-" + id
}

// UnitPrefixFor is what every unit belonging to this site starts with, which is
// how a stale unit left by a deleted entry is found again.
func UnitPrefixFor(siteName string) string {
	return unitPrefix + siteName + "-"
}

// ServiceUnit is the oneshot that runs the command once, when the timer says so.
//
// It execs into the site's container the same way a worker does, so a scheduled
// command sees exactly the PHP, the extensions and the working directory the
// site itself runs on. A site with no container has nowhere to run it and is
// refused rather than given a unit that fails on every tick.
func ServiceUnit(site config.Site, e config.CronEntry) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	if err := validateUnitFields(site); err != nil {
		return "", err
	}
	container := podman.SiteContainerName(site, site.PHPVersion)
	if container == "" {
		return "", fmt.Errorf("%s runs on the host and has no container for a scheduled command to run in", site.Name)
	}

	// Output goes to the journal unless the entry says otherwise, because the
	// journal is where the panel reads the last run from. An entry that runs
	// every minute and prints every time can turn it off, and then there is
	// genuinely nothing to show, which the panel says.
	output := "StandardOutput=null\nStandardError=null\n"
	if e.CaptureOutput {
		output = ""
	}

	return fmt.Sprintf(`[Unit]
Description=Servlo cron %s (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=oneshot
%sExecStart=%s exec -w %s --env=SERVLO_SITE=%s %s%s /bin/sh -c "%s"
`,
		e.Name, site.Name,
		container, container,
		output,
		podman.PodmanBin(),
		podman.ShellQuote(site.Path),
		site.Name,
		execColorArgs(),
		container,
		systemdArg(e.Command),
	), nil
}

// TimerUnit is when the oneshot runs.
func TimerUnit(site config.Site, e config.CronEntry) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	if err := validateUnitFields(site); err != nil {
		return "", err
	}
	if strings.TrimSpace(e.Calendar) == "" {
		return "", fmt.Errorf("%q has no translated schedule", e.Name)
	}
	if config.ContainsUnitInjectionChars(e.Calendar) {
		return "", fmt.Errorf("a schedule must not contain a newline or a NUL: every line of it is a line of a unit file")
	}

	unit := UnitName(site.Name, e.ID)
	// Persistent so a droplet that was off at 03:00 still runs the nightly job
	// when it comes back, rather than skipping the day silently. AccuracySec
	// because systemd otherwise spreads timers by up to a minute, which for a
	// minutely entry means it sometimes does not run at all.
	return fmt.Sprintf(`[Unit]
Description=Servlo cron %s timer (%s)

[Timer]
OnCalendar=%s
Persistent=true
AccuracySec=1s
Unit=%s.service

[Install]
WantedBy=timers.target
`, e.Name, site.Name, e.Calendar, unit), nil
}

// validateUnitFields refuses the site-side values that land in a unit body. The
// entry's own fields are checked by CronEntry.Validate.
func validateUnitFields(site config.Site) error {
	for what, value := range map[string]string{
		"site name": site.Name,
		"site path": site.Path,
	} {
		if config.ContainsUnitInjectionChars(value) {
			return fmt.Errorf("the %s must not contain a newline or a NUL", what)
		}
	}
	return nil
}

// execColorArgs keeps the command's output coloured through the pipe systemd
// puts it on, the same way a worker's is, so the panel shows what a terminal
// would have shown.
func execColorArgs() string {
	args := logcolor.PodmanExecArgs()
	if len(args) == 0 {
		return ""
	}
	return strings.Join(args, " ") + " "
}

// systemdArg escapes a command so systemd hands it to the shell as one
// argument, unchanged.
//
// Three characters matter. A backslash and a double quote both end or alter the
// quoted argument the command sits inside. The per cent sign is the third, and
// it is escaped through internal/systemd because every unit servlo writes an
// operator's command into has the same problem with it. A dollar sign would be
// a fourth, but there is no escape for it that systemd documents, so
// CronEntry.Validate refuses it instead.
func systemdArg(command string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
	)
	return systemd.EscapeSpecifiers(r.Replace(command))
}
