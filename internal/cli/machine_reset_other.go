//go:build !darwin

package cli

import "fmt"

// runMachineReset is macOS-only: Linux uses native rootless podman with no VM.
func runMachineReset(_ bool) error {
	fmt.Println("lerd machine reset is only supported on macOS (Podman Machine). On Linux, podman runs natively without a VM.")
	return nil
}
