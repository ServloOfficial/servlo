package cli

import (
	"strings"
	"testing"
)

// The php shim returns the child's exit code as its own, which is the whole
// point of a shim: `php -r 'exit(3);'` must exit 3. The cost is that podman's
// own 125, which means "I could not run the container", is indistinguishable
// from a program that chose to exit 125.
//
// A fresh install used to leave PHP-FPM restarting in a loop, so the first
// `servlo php -v` an operator typed printed nothing whatsoever and exited 125.
// What separates the two cases is whether the container is up afterwards.
func TestExplainPodmanFailure(t *testing.T) {
	origRunning := fpmContainerRunning
	t.Cleanup(func() { fpmContainerRunning = origRunning })

	cases := []struct {
		name    string
		code    int
		running bool
		wantErr bool
	}{
		{"podman could not run it and the container is down", 125, false, true},
		{"a script exited 125 of its own accord", 125, true, false},
		{"an ordinary script failure is left alone", 1, false, false},
		{"a script exiting 3 is left alone", 3, false, false},
		{"success is left alone", 0, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fpmContainerRunning = func(string) (bool, error) { return tc.running, nil }
			err := explainPodmanFailure(tc.code, "8.5", "servlo-php85-fpm")
			if tc.wantErr && err == nil {
				t.Fatal("expected an explanation, got silence — which is the bug")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no explanation, got %v", err)
			}
			if tc.wantErr {
				if !strings.Contains(err.Error(), "not running") {
					t.Errorf("the message should say the container is not running, got %q", err)
				}
				if !strings.Contains(err.Error(), "8.5") {
					t.Errorf("the message should name the version, got %q", err)
				}
			}
		})
	}
}
