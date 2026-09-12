package siteops

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ServloOfficial/servlo/internal/cfgedit"
	"github.com/ServloOfficial/servlo/internal/config"
)

// Each site's deploy script.
//
// It lives in servlo's data directory rather than in the site, and that is the
// point rather than an accident. A script inside the working tree is a file
// `git pull` can change, which for a deploy script means the deploy can rewrite
// the thing running it halfway through. Keeping it outside means a pull cannot
// touch it, a fresh clone of the same repository somewhere else does not
// inherit it, and it is not something an operator can commit by mistake.
//
// It is seeded from the framework's profile and never rewritten afterwards.
// What a particular application needs at deploy time is not knowable from its
// framework, so the template is a starting point and the site's copy is the
// truth.

// siteHandle is the filename half of the path, so a name that could climb out
// of the directory is refused rather than joined into one.
var siteHandle = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,62}[a-zA-Z0-9])?$`)

// DeployScriptDir holds one script per site.
func DeployScriptDir() string {
	return filepath.Join(config.DataDir(), "deploy-scripts")
}

// DeployScriptPath is where a site's script lives.
func DeployScriptPath(site string) string {
	return filepath.Join(DeployScriptDir(), site+".sh")
}

// deployScriptFile is the cfgedit view of one site's script. Backups sit in a
// sibling directory so a restore has something to read and an operator who
// pastes over a working script has a way back.
func deployScriptFile(site string, template string) (cfgedit.File, error) {
	if !siteHandle.MatchString(site) {
		return cfgedit.File{}, fmt.Errorf("%q is not a usable site name", site)
	}
	return cfgedit.File{
		Path:     DeployScriptPath(site),
		BkpDir:   filepath.Join(DeployScriptDir(), "bkp"),
		BkpName:  site + ".sh",
		Template: template,
	}, nil
}

// deployScriptTemplate is what a site starts from: its framework's script under
// a header explaining what the file is.
//
// The header is not decoration. This runs on the server as the servlo user with
// the site as its working directory, and somebody reading it a year later needs
// to know that without going looking.
func deployScriptTemplate(site *config.Site) string {
	header := `#!/bin/sh
# Servlo runs this after ` + "`git pull`" + ` on every deploy of ` + site.PrimaryDomain() + `.
#
# It runs as the servlo user, with the site directory as its working directory,
# and its output streams into the panel. A non-zero exit fails the deploy.
#
# Servlo runs what is below with /bin/sh, which on Ubuntu is dash. It runs the
# body rather than the file, so changing the first line changes nothing: a bash
# feature underneath it fails with a message about the option rather than about
# the shell. Keep it to POSIX sh, or call bash from inside the script.
#
# Servlo seeded this from the framework's profile and will not touch it again.
`
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok {
		return header + `
# This site's framework declares no deploy profile, so there is nothing here
# yet. Add whatever the application needs.
`
	}
	script := strings.TrimSpace(fw.DeployScript())
	if script == "" {
		return header + `
# ` + fw.Label + ` declares an empty deploy script, which for most sites on it
# is right: the pull is the deploy. Add whatever this one needs.
`
	}
	return header + "\n" + script + "\n"
}

// ReadDeployScript returns a site's script, or the seeded template with
// Exists=false when it has never been saved.
func ReadDeployScript(site *config.Site) (cfgedit.Content, error) {
	f, err := deployScriptFile(site.Name, deployScriptTemplate(site))
	if err != nil {
		return cfgedit.Content{}, err
	}
	return f.Read()
}

// SaveDeployScript writes a site's script, keeping a backup of what it
// replaced.
//
// No validation of the contents. It is a shell script an operator wrote to run
// on their own server, and servlo guessing at which commands are reasonable
// would be both wrong and unenforceable. What is checked is the site name,
// which decides the path.
func SaveDeployScript(site *config.Site, content string, backup bool) (cfgedit.SaveResult, error) {
	f, err := deployScriptFile(site.Name, deployScriptTemplate(site))
	if err != nil {
		return cfgedit.SaveResult{}, err
	}
	if strings.ContainsRune(content, 0) {
		return cfgedit.SaveResult{OK: false, Error: "the script contains a NUL byte, which no shell will read"}, nil
	}
	return f.Save(content, cfgedit.SaveOpts{Backup: backup})
}

// ResetDeployScript deletes a site's script, so it goes back to its framework's
// template. Backups are kept.
func ResetDeployScript(site *config.Site) error {
	f, err := deployScriptFile(site.Name, deployScriptTemplate(site))
	if err != nil {
		return err
	}
	return f.Reset(nil)
}

// ListDeployScriptBackups returns a site's script backups, newest first.
func ListDeployScriptBackups(site *config.Site) ([]cfgedit.Backup, error) {
	f, err := deployScriptFile(site.Name, "")
	if err != nil {
		return nil, err
	}
	return f.ListBackups()
}

// ReadDeployScriptBackup returns the bytes of one backup.
func ReadDeployScriptBackup(site *config.Site, name string) ([]byte, error) {
	f, err := deployScriptFile(site.Name, "")
	if err != nil {
		return nil, err
	}
	return f.ReadBackup(name)
}

// RestoreDeployScript puts a backup back over the live script.
func RestoreDeployScript(site *config.Site, name string) (cfgedit.RestoreResult, error) {
	f, err := deployScriptFile(site.Name, "")
	if err != nil {
		return cfgedit.RestoreResult{}, err
	}
	return f.Restore(name, nil)
}

// DeployScriptMigrates reports whether a site's saved script runs its
// framework's migration, which is what decides whether a deploy takes a
// database backup first.
//
// Read from the saved script rather than from the framework's template: the
// operator may have removed the migration, or added one the template never
// had, and the backup has to follow what will actually run.
func DeployScriptMigrates(site *config.Site) (bool, error) {
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok {
		return false, nil
	}
	content, err := ReadDeployScript(site)
	if err != nil {
		return false, err
	}
	return fw.ScriptMigrates(content.Body), nil
}
