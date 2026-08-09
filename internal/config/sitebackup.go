package config

// SiteBackup is a site's scheduled backup: when it runs, and how much of the
// history to keep.
//
// Nothing is scheduled until an operator asks for it. A timer nobody switched
// on is disk filling up on a server whose owner does not know it is happening,
// and the panel says plainly which sites have one and which do not.
type SiteBackup struct {
	// Schedule is a systemd calendar expression, normalised from whatever the
	// operator typed. Empty means the site has a retention policy but no timer,
	// which is the state of a site backed up only by hand.
	Schedule string `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	// Disabled keeps the schedule but stops it running, so switching a site's
	// backups off for an afternoon does not mean retyping the schedule after.
	Disabled bool `yaml:"disabled,omitempty" json:"disabled,omitempty"`
	// Verify is when to test-restore the newest archive, as a calendar
	// expression. Empty means never, which is the state of a site whose
	// operator has not asked for the check.
	//
	// Separate from Schedule because it is a different question answered at a
	// different rate: a verify restores the whole database into a scratch copy,
	// which on a large site is real work to do every night to answer something
	// that changes rarely.
	Verify string `yaml:"verify,omitempty" json:"verify,omitempty"`
	// Keep is how many daily, weekly and monthly archives survive a sweep.
	// Absent means servlo's default rather than none, because a site that
	// schedules backups and keeps nothing is never what anyone meant.
	Keep *BackupKeep `yaml:"keep,omitempty" json:"keep,omitempty"`
}

// BackupKeep mirrors backup.Policy without config depending on the backup
// package, which sits above it.
type BackupKeep struct {
	Daily   int `yaml:"daily,omitempty" json:"daily"`
	Weekly  int `yaml:"weekly,omitempty" json:"weekly"`
	Monthly int `yaml:"monthly,omitempty" json:"monthly"`
}
