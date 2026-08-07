package linker

import "testing"

// The panel is on the public internet and its caller is a click, so the policy
// it links under is narrower than the CLI's in the places where a click is not
// the same thing as a person at a terminal.
func TestPanelPolicy_GrantsWhatAnAddSiteFormNeeds(t *testing.T) {
	p := PanelPolicy("example.com")

	if p.Domain != "example.com" {
		t.Errorf("Domain = %q, want the domain the form gave", p.Domain)
	}
	// The click is the consent, and there is nobody to ask a follow-up.
	if !p.AssumeYes {
		t.Error("AssumeYes = false, but a submitted form is the consent")
	}
	if p.Prompt != nil {
		t.Error("Prompt is non-nil: there is no terminal to answer it")
	}
	// Pinning .php-version and writing the domain back is what makes the site
	// reproducible, and the operator asked for the site.
	if !p.ProjectWrites {
		t.Error("ProjectWrites = false, so the site would not pin its own versions")
	}
	if !p.Services {
		t.Error("Services = false, so a framework's required services would not start")
	}
	if !p.ImageBuild {
		t.Error("ImageBuild = false, so a site needing an uninstalled PHP could not be served")
	}
}

// The two refusals are the point of having a separate policy at all.
func TestPanelPolicy_RefusesWhatAClickCannotConsentTo(t *testing.T) {
	p := PanelPolicy("example.com")

	// A repository's own dev-server command and its inline service containers
	// run on the host. "Add site" is not consent to execute what the repo
	// authored, and the browser has no prompt to ask for it properly.
	if p.RepoCommands {
		t.Error("RepoCommands = true: adding a site would run code the repository chose")
	}
	// Issuance is gated on a live DNS check that the domain points here, which
	// a brand new site has not passed. Get SSL is its own button for a reason.
	if p.Certs {
		t.Error("Certs = true: a new site would ask a CA for a certificate before DNS resolves")
	}
}
