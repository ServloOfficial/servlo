package cli

// recommendedVMMemoryMiB picks Podman Machine memory based on host RAM and
// the active worker runtime mode. 8 GB is the minimum host RAM tier; anything
// below (including detection failures) uses the 8 GB values.
//
// Container mode (workers each get a dedicated container): max 6144 MiB.
// Exec mode (workers share the FPM container): max 4096 MiB — a ⅔ ratio
// applied proportionally across all tiers.
//
//	Host RAM      Container   Exec
//	≤ 8 GB         3072 MiB  2048 MiB
//	9 – 31 GB      4096 MiB  3072 MiB
//	≥ 32 GB        6144 MiB  4096 MiB
//
// int64 matches strconv.ParseInt at the comparison site.
func recommendedVMMemoryMiB(hostMemoryGiB int, execMode bool) int64 {
	switch {
	case hostMemoryGiB >= 32:
		if execMode {
			return 4096
		}
		return 6144
	case hostMemoryGiB >= 9:
		if execMode {
			return 3072
		}
		return 4096
	default: // ≤ 8 GB or detection failed — 8 GB is the floor
		if execMode {
			return 2048
		}
		return 3072
	}
}
