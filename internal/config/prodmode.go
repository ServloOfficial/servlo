package config

import "time"

// Production mode, and what it is for.
//
// It is one flag that a lot of other decisions read: whether PHP shows errors
// to visitors, whether OPcache trusts its cache without stat-ing every file,
// how a worker restarts when it dies. Those belong together because they are
// the same question asked four ways, and setting them individually is how a
// machine ends up half-production, showing stack traces to the world while
// caching aggressively enough that nobody notices the fix.
//
// Off by default. Turning it on for someone who has not asked hides the errors
// they were about to read.

// ProductionMode reports whether this install is in production mode. A nil
// receiver reads as false, matching the rest of this package: an unreadable
// config should not silently suppress the errors someone is trying to see.
func (c *GlobalConfig) ProductionMode() bool {
	return c != nil && c.Production.Enabled
}

// ProductionSince is when production mode was turned on, zero when it is off.
// Worth keeping because "in production for six months" and "switched on an hour
// ago" are different situations to be debugging in.
func (c *GlobalConfig) ProductionSince() time.Time {
	if c == nil || !c.Production.Enabled {
		return time.Time{}
	}
	return c.Production.Since
}

// SetProductionMode records the flag. Re-enabling keeps the original timestamp,
// so re-running the command does not reset how long the machine has been live;
// disabling clears it, so a machine that was briefly in production months ago
// does not still claim a start date.
func (c *GlobalConfig) SetProductionMode(on bool) {
	if !on {
		c.Production.Enabled = false
		c.Production.Since = time.Time{}
		return
	}
	if !c.Production.Enabled {
		c.Production.Since = time.Now().UTC()
	}
	c.Production.Enabled = true
}
