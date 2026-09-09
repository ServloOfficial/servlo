//go:build linux

package podman

// PlatformPodmanArgs is a no-op. amd64 pulls every image servlo uses natively,
// so nothing needs an extra platform argument today.
func PlatformPodmanArgs(_, _ string) string {
	return ""
}

// PlatformPullArgs is a no-op (see PlatformPodmanArgs).
func PlatformPullArgs(_ string) []string {
	return nil
}

// PlatformImage is a no-op. amd64 runs postgis natively; arm64 would need the
// rewriteArm64Image swap behind a runtime.GOARCH guard, which is why that
// function is kept and unwired.
func PlatformImage(image string) string {
	return image
}
