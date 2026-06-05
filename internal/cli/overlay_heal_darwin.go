//go:build darwin

package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/geodro/lerd/internal/podman"
)

// healOverlayCorruptionIfNeeded restarts the Podman Machine when the service
// start pass failed with the overlay-storage error (see isOverlayStorageError).
// A clean stop+start remounts the VM's container storage, which clears the
// stale-mount form of the corruption. lerd's persistent data is host
// bind-mounted, so the restart is non-destructive. Returns true when a restart
// was performed and the caller should retry the start pass once.
func healOverlayCorruptionIfNeeded(err error) bool {
	if !isOverlayStorageError(err) {
		return false
	}
	restartPodmanMachineForHeal()
	return true
}

// restartPodmanMachineForHeal stops and restarts lerd's Podman Machine so its
// container storage is remounted, then refreshes the restart baseline so the
// next run doesn't mistake this restart for an external one.
func restartPodmanMachineForHeal() {
	name := selectedMachineName()
	if name == "" {
		return
	}
	fmt.Println("  --> Container storage looks stale after an unclean shutdown; restarting the Podman Machine to remount it ...")
	stop := exec.Command(podman.PodmanBin(), "machine", "stop", name)
	stop.Stdout = os.Stdout
	stop.Stderr = os.Stderr
	if err := stop.Run(); err != nil {
		fmt.Printf("  WARN: podman machine stop: %v\n", err)
	}
	// ensurePodmanMachineRunning starts the VM and waits for the API socket.
	ensurePodmanMachineRunning()
	recordMachineLastUp()
}

// reportOverlayHealOutcome prints recovery guidance when the overlay-storage
// error persisted after the automatic machine restart and retry.
func reportOverlayHealOutcome(err error) {
	if !isOverlayStorageError(err) {
		return
	}
	fmt.Println()
	fmt.Println("  Podman Machine container storage is still corrupted after a restart.")
	fmt.Println("  This happens when the host shuts down while the VM is running.")
	fmt.Println("  Your databases and site data are safe — they live on the host, not in the VM.")
	fmt.Println("  Recreate the VM to fix it (images are rebuilt automatically on the next start):")
	fmt.Println("      lerd machine reset")
}
