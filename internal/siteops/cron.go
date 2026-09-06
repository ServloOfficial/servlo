package siteops

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/envfile"
	"github.com/ServloOfficial/servlo/internal/sitecron"
)

// SaveCron adds or replaces one of a site's scheduled commands.
//
// The unit pair is written before the registry, and the registry rolls the unit
// back if it cannot be saved. The order matters in one direction only: a timer
// with no entry beside it is a command running where nobody can see it, and an
// entry with no timer is a row that plainly says it has never run.
func SaveCron(site *config.Site, in config.CronEntry) (config.CronEntry, error) {
	entry := config.CronEntry{
		ID:            strings.TrimSpace(in.ID),
		Name:          strings.TrimSpace(in.Name),
		Command:       strings.TrimSpace(in.Command),
		Schedule:      strings.TrimSpace(in.Schedule),
		CaptureOutput: in.CaptureOutput,
		Disabled:      in.Disabled,
		Managed:       in.Managed,
	}
	if entry.ID == "" {
		entry.ID = newCronID(site, entry.Name)
	}

	calendar, err := sitecron.NormalizeSchedule(entry.Schedule)
	if err != nil {
		return config.CronEntry{}, err
	}
	entry.Calendar = calendar
	if err := entry.Validate(); err != nil {
		return config.CronEntry{}, err
	}

	existing, replacing := site.FindCron(entry.ID)
	if replacing && existing.Managed && !entry.Managed {
		return config.CronEntry{}, fmt.Errorf("%q is installed by the framework and is changed with its own switch", existing.Name)
	}

	updated := *site
	updated.Cron = replaceCron(site.Cron, entry)

	if err := sitecron.Apply(updated, entry); err != nil {
		return config.CronEntry{}, err
	}
	if err := config.AddSite(updated); err != nil {
		// The unit is on the machine and the entry is not in the registry, which
		// is the one combination that hides a running command. Take it back off.
		_ = sitecron.Remove(site.Name, entry.ID)
		return config.CronEntry{}, fmt.Errorf("updating site registry: %w", err)
	}
	*site = updated
	return entry, nil
}

// DeleteCron removes a scheduled command and everything systemd holds for it.
//
// The unit goes first. If it cannot be removed the entry stays, because a row
// in the panel is the only way an operator would ever find the timer that is
// still there.
func DeleteCron(site *config.Site, id string) error {
	entry, ok := site.FindCron(id)
	if !ok {
		return fmt.Errorf("this site has no scheduled command called %q", id)
	}
	if err := sitecron.Remove(site.Name, entry.ID); err != nil {
		return err
	}

	updated := *site
	updated.Cron = nil
	for _, e := range site.Cron {
		if e.ID != id {
			updated.Cron = append(updated.Cron, e)
		}
	}
	if err := config.AddSite(updated); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}
	*site = updated
	return nil
}

// RemoveSiteCron takes every one of a site's schedules off the machine, leaving
// the entries in the registry. Used when the site itself is going away, where
// there is no point writing a registry that is about to be deleted.
func RemoveSiteCron(site *config.Site) {
	for _, e := range site.Cron {
		_ = sitecron.Remove(site.Name, e.ID)
	}
}

// replaceCron puts the entry where its predecessor was, so editing a schedule
// does not move the row.
func replaceCron(existing []config.CronEntry, entry config.CronEntry) []config.CronEntry {
	out := make([]config.CronEntry, 0, len(existing)+1)
	replaced := false
	for _, e := range existing {
		if e.ID == entry.ID {
			out = append(out, entry)
			replaced = true
			continue
		}
		out = append(out, e)
	}
	if !replaced {
		out = append(out, entry)
	}
	return out
}

// newCronID derives an identifier from the name and keeps going until it is one
// the site is not already using, since the identifier names a unit file.
func newCronID(site *config.Site, name string) string {
	base := config.CronID(name)
	if base == "" {
		base = "job"
	}
	id := base
	for n := 2; ; n++ {
		if _, taken := site.FindCron(id); !taken {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, n)
	}
}

// PseudoCron is what the panel needs to offer the switch: whether this site's
// framework has a pseudo-cron at all, what it is called, and whether servlo is
// currently doing the work instead.
type PseudoCron struct {
	Available   bool   `json:"available"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	// Replaced is true while the system timer is doing the work and the
	// framework's own page-load scheduler is switched off.
	Replaced bool `json:"replaced"`
	// Schedule and Command are what the replacement runs, shown so the switch
	// is not a promise the operator has to take on trust.
	Schedule string `json:"schedule,omitempty"`
	Command  string `json:"command,omitempty"`
	// Constant names the setting servlo writes into the site's config file.
	Constant string `json:"constant,omitempty"`
	File     string `json:"file,omitempty"`
}

// PseudoCronFor reads the framework's declaration and the site's current state.
func PseudoCronFor(site *config.Site) PseudoCron {
	declared, ok := pseudoCronDeclaration(site)
	if !ok {
		return PseudoCron{}
	}
	_, replaced := site.FindCron(declared.Entry.ID)
	return PseudoCron{
		Available:   true,
		Label:       declared.Label,
		Description: declared.Description,
		Replaced:    replaced,
		Schedule:    declared.Entry.Schedule,
		Command:     declared.Entry.Command,
		Constant:    declared.Constant,
		File:        declared.File,
	}
}

// SetPseudoCron switches between the framework's page-load scheduler and a real
// system timer.
//
// Turning it off is the half worth being careful about: the constant has to go
// back to the value that lets the framework schedule its own work again,
// because a site left with the constant set and the timer removed has no cron
// at all, and nothing about it would look wrong.
func SetPseudoCron(site *config.Site, replace bool) error {
	declared, ok := pseudoCronDeclaration(site)
	if !ok {
		return fmt.Errorf("%s runs no framework with a built-in scheduler servlo can replace", site.Name)
	}
	configPath := filepath.Join(site.Path, filepath.Clean(declared.File))

	if !replace {
		if err := envfile.ApplyPhpConstLiterals(configPath, map[string]string{
			declared.Constant: declared.RestoredValue,
		}); err != nil {
			return fmt.Errorf("restoring %s in %s: %w", declared.Constant, declared.File, err)
		}
		if _, installed := site.FindCron(declared.Entry.ID); !installed {
			return nil
		}
		return DeleteCron(site, declared.Entry.ID)
	}

	if _, err := SaveCron(site, config.CronEntry{
		ID:            declared.Entry.ID,
		Name:          declared.Entry.Name,
		Command:       declared.Entry.Command,
		Schedule:      declared.Entry.Schedule,
		CaptureOutput: declared.Entry.CaptureOutput,
		Managed:       true,
	}); err != nil {
		return err
	}
	// The constant goes last: the timer is what does the work, and switching
	// the framework's own scheduler off before there is a replacement leaves a
	// window with neither.
	if err := envfile.ApplyPhpConstLiterals(configPath, map[string]string{
		declared.Constant: declared.ReplacedValue,
	}); err != nil {
		_ = DeleteCron(site, declared.Entry.ID)
		return fmt.Errorf("setting %s in %s: %w", declared.Constant, declared.File, err)
	}
	return nil
}

// pseudoCronDeclaration is the site framework's block, if it has one that is
// complete enough to act on. A half-written declaration is treated as none:
// writing a constant with no timer, or a timer with no constant, is worse than
// leaving the site as it is.
func pseudoCronDeclaration(site *config.Site) (config.FrameworkPseudoCron, bool) {
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok || fw.PseudoCron == nil {
		return config.FrameworkPseudoCron{}, false
	}
	p := *fw.PseudoCron
	if p.File == "" || p.Constant == "" || p.Entry.ID == "" || p.Entry.Command == "" || p.Entry.Schedule == "" {
		return config.FrameworkPseudoCron{}, false
	}
	return p, true
}
