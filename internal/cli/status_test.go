package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

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
		username   string
		passHash   string
		wantSubstr []string
	}{
		{
			// The sites answer on every interface and the managed services
			// answer on none of them. Neither is a setting, so the section
			// says both plainly rather than reporting a state.
			name: "dashboard credentials not set",
			wantSubstr: []string{
				"Sites served on every interface",
				"Managed services (loopback-only, always)",
				"Dashboard remote access",
				"remote clients get 403",
				"servlo remote-control on",
			},
		},
		{
			name:     "dashboard credentials set",
			username: "george",
			passHash: "$2a$10$fakehashfakehashfakehashfakehashfakehashfakehashfake",
			wantSubstr: []string{
				"Sites served on every interface",
				"Dashboard remote access (user: george)",
				"Managed services (loopback-only, always)",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.GlobalConfig{}
			cfg.UI.Username = tc.username
			cfg.UI.PasswordHash = tc.passHash

			out := captureStdout(t, func() {
				printRemoteAccessStatus(cfg)
			})

			for _, want := range tc.wantSubstr {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\nfull output:\n%s", want, out)
				}
			}
		})
	}
}
