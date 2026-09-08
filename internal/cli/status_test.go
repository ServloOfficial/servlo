package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/authz"
	"github.com/ServloOfficial/servlo/internal/config"
)

// captureStdout runs fn with os.Stdout redirected into a buffer and returns
// everything fn wrote. Used to assert on the text printed by the [Remote Access]
// section of `servlo status`.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	return <-done
}

func TestPrintRemoteAccessStatus(t *testing.T) {
	cases := []struct {
		name       string
		claimed    bool
		wantSubstr []string
		notSubstr  []string
	}{
		{
			// The sites answer on every interface and the managed services
			// answer on none of them. Neither is a setting, so the section
			// says both plainly rather than reporting a state.
			//
			// The dashboard line reports whether the panel has been claimed.
			// It used to report a pre-S5.5 gate that turned remote clients
			// away until a password was set; that gate is gone and every
			// request needs a session now, so saying otherwise told an
			// operator they were behind a door that was not there.
			name: "panel not claimed yet",
			wantSubstr: []string{
				"Sites served on every interface",
				"Managed services (loopback-only, always)",
				"Dashboard has no account yet",
				"servlo users add",
			},
			notSubstr: []string{"403", "Basic auth"},
		},
		{
			name:    "panel claimed",
			claimed: true,
			wantSubstr: []string{
				"Sites served on every interface",
				"every request needs a session",
				"Managed services (loopback-only, always)",
			},
			notSubstr: []string{"403", "Basic auth"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			if tc.claimed {
				accounts, err := authz.OpenAccounts()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := accounts.Create("operator", "a-long-enough-password", authz.RoleAdmin); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &config.GlobalConfig{}

			out := captureStdout(t, func() {
				printRemoteAccessStatus(cfg)
			})

			for _, want := range tc.wantSubstr {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\nfull output:\n%s", want, out)
				}
			}
			for _, unwanted := range tc.notSubstr {
				if strings.Contains(out, unwanted) {
					t.Errorf("output still mentions the removed gate (%q)\nfull output:\n%s", unwanted, out)
				}
			}
		})
	}
}
