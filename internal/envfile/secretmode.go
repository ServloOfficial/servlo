package envfile

import (
	"io/fs"
	"os"
)

// SecretMode is the mode a site's env file is left at.
//
// These files are where servlo puts the database password it generated for the
// site, along with the application key, the SMTP credentials and whatever else
// a service preset injects. CLAUDE.md section 3.7 fixes them at 0600, and
// internal/sitefs says so in a comment as though it were already true, but
// nothing enforced it: `servlo env` created a .env at 0644, and every writer
// here preserved whatever mode it found, so credentials servlo wrote itself sat
// world readable for the life of the site.
//
// All sites run as one Linux user (PRD section 6), so this is not what keeps
// one site out of another's database. It keeps them out of every other account
// on the machine, which on a droplet that has ever had a second login is the
// difference between a password and a public file.
const SecretMode fs.FileMode = 0o600

// secure narrows path to SecretMode, leaving anything already at least that
// tight alone.
//
// Narrowing rather than setting, because an operator who has taken the group
// bit off entirely meant it, and a file that is already 0400 does not need
// servlo to make it writable again.
func secure(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&^SecretMode == 0 {
		return nil
	}
	return os.Chmod(path, info.Mode().Perm()&SecretMode)
}
