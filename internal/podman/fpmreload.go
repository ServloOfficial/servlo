package podman

// ReloadFPMPools makes an FPM master pick up a pool that was just written or
// removed.
//
// SIGUSR2 rather than restarting the unit, because a restart drops every
// in-flight request on every site sharing the container. FPM's own reload
// re-reads the configuration, starts new workers and lets the running ones
// finish what they are doing first, so changing one site's upload limit does
// not interrupt another site's checkout.
//
// A container that is not running is not an error: its pools are read when it
// next starts, which is the ordinary state for a version nothing is serving
// yet.
func ReloadFPMPools(container string) error {
	if !ContainerRunningQuiet(container) {
		return nil
	}
	return RunSilent("kill", "--signal", "USR2", container)
}
