package ui

import (
	"testing"

	servloUpdate "github.com/ServloOfficial/servlo/internal/update"
)

// TestBuildVersionResponse_StripsLeadingV pins the fix for "Servlo vv1.19.2
// is available" — the GitHub tag is e.g. "v1.19.2" but the Svelte banner
// template already prepends "v", so the wire data must be bare.
func TestBuildVersionResponse_StripsLeadingV(t *testing.T) {
	resp := buildVersionResponse("1.19.1", &servloUpdate.UpdateInfo{LatestVersion: "v1.19.2"})
	if resp.Latest != "1.19.2" {
		t.Errorf("Latest = %q, want %q (no leading v)", resp.Latest, "1.19.2")
	}
	if !resp.HasUpdate {
		t.Errorf("HasUpdate should be true when info is non-nil")
	}
}

// TestBuildVersionResponse_NoUpdateLeavesLatestEmpty keeps the banner hidden
// when no update is available — no Latest, no HasUpdate.
func TestBuildVersionResponse_NoUpdateLeavesLatestEmpty(t *testing.T) {
	resp := buildVersionResponse("1.19.1", nil)
	if resp.Latest != "" || resp.HasUpdate {
		t.Errorf("expected zero-update response, got %+v", resp)
	}
	if resp.Current != "1.19.1" {
		t.Errorf("Current should be passed through, got %q", resp.Current)
	}
}

// TestBuildVersionResponse_HandlesPrereleaseTag covers the beta channel
// where the tag is e.g. "v1.20.0-beta.1" — strip-v still applies.
func TestBuildVersionResponse_HandlesPrereleaseTag(t *testing.T) {
	resp := buildVersionResponse("1.20.0-beta.1", &servloUpdate.UpdateInfo{LatestVersion: "v1.20.0-beta.2"})
	if resp.Latest != "1.20.0-beta.2" {
		t.Errorf("prerelease Latest mishandled, got %q", resp.Latest)
	}
}
