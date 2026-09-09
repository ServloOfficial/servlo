package certs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedIssued lays down everything an issuance leaves behind for a domain: the
// certificate, its private key, the progress log of the attempt, and a failure
// record for good measure.
func seedIssued(t *testing.T, domain string) (certPath, keyPath string) {
	t.Helper()
	certPath, keyPath = SitePaths(domain)
	if err := os.MkdirAll(filepath.Dir(certPath), 0755); err != nil {
		t.Fatalf("creating the certs directory: %v", err)
	}
	for _, p := range []string{certPath, keyPath} {
		if err := os.WriteFile(p, []byte("material for "+domain), 0600); err != nil {
			t.Fatalf("writing %s: %v", p, err)
		}
	}
	startProgress(domain)
	noteProgress(domain, "asked the authority for %s", domain)
	recordFailure(domain, os.ErrPermission)
	return certPath, keyPath
}

// A site's private key is the one file on the machine that must not outlive the
// site. Unlink removed it from the wrong directory for long enough that every
// site ever removed left its key behind.
func TestForgetSite_removesTheCertificateAndItsKey(t *testing.T) {
	renewalEnv(t)
	certPath, keyPath := seedIssued(t, "gone.example.com")

	ForgetSite("gone.example.com")

	for _, p := range []string{certPath, keyPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived the site it belonged to (stat error %v)", p, err)
		}
	}
}

// A failure record is kept until an issuance succeeds, and a domain with no
// site can never have one, so an unlink that leaves the record behind leaves
// the panel banner and doctor's TLS line red for good.
func TestForgetSite_clearsTheRenewalFailureRecord(t *testing.T) {
	renewalEnv(t)
	seedIssued(t, "gone.example.com")

	if len(RenewalFailures()) == 0 {
		t.Fatal("the fixture recorded no failure, so this test would pass for the wrong reason")
	}

	ForgetSite("gone.example.com")

	for _, f := range RenewalFailures() {
		if f.Domain == "gone.example.com" {
			t.Errorf("a domain with no site is still reported as failing renewal: %q", f.Reason)
		}
	}
}

func TestForgetSite_removesTheIssuanceProgressLog(t *testing.T) {
	renewalEnv(t)
	seedIssued(t, "gone.example.com")

	if len(Progress("gone.example.com")) == 0 {
		t.Fatal("the fixture wrote no progress, so this test would pass for the wrong reason")
	}

	ForgetSite("gone.example.com")

	if steps := Progress("gone.example.com"); len(steps) != 0 {
		t.Errorf("the issuance log outlived the site: %v", steps)
	}
}

// Every file here is named for a domain, so forgetting one must not be spelled
// as a prefix match or a directory sweep.
func TestForgetSite_leavesEveryOtherDomainAlone(t *testing.T) {
	renewalEnv(t)
	keptCert, keptKey := seedIssued(t, "kept.example.com")
	seedIssued(t, "gone.example.com")

	ForgetSite("gone.example.com")

	for _, p := range []string{keptCert, keptKey} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s went with an unrelated site: %v", p, err)
		}
	}
	if len(Progress("kept.example.com")) == 0 {
		t.Error("an unrelated site lost its issuance log")
	}
	var found bool
	for _, f := range RenewalFailures() {
		if f.Domain == "kept.example.com" {
			found = true
		}
	}
	if !found {
		t.Error("an unrelated site lost its failure record")
	}
}

// SitePaths and SitesDir exist so one place spells where a site's certificate
// lives. They were written for exactly this and then not used: unlink kept its
// own join, missed the sites/ segment when the layout gained one, and removed
// nothing from then on.
//
// So the rule is the directory, not the filename. The join that broke unlink
// was spread over two lines, and a check that reads one line at a time would
// have watched it go in.
//
// internal/config declares CertsDir. internal/nginx is exempt because certs
// imports it, so it cannot import back.
func TestCertsLayout_IsReachedOnlyThroughThisPackage(t *testing.T) {
	exempt := map[string]bool{
		"internal/certs":  true,
		"internal/nginx":  true,
		"internal/config": true,
	}

	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if exempt[filepath.ToSlash(filepath.Dir(rel))] {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "config.CertsDir()") {
				t.Errorf("%s:%d reaches into the certs directory itself: %s\ncall certs.SitePaths or certs.SitesDir, so the layout is spelled once",
					filepath.ToSlash(rel), i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
}
