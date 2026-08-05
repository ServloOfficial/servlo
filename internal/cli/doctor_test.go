package cli

import (
	"strings"
	"testing"
)

func TestPortInUseIn_match(t *testing.T) {
	output := "LISTEN  0  128  127.0.0.1:5300  0.0.0.0:*"
	if !PortInUseIn("5300", output) {
		t.Error("expected port 5300 to be found in output")
	}
}

func TestPortInUseIn_noMatch(t *testing.T) {
	output := "LISTEN  0  128  127.0.0.1:5300  0.0.0.0:*"
	if PortInUseIn("80", output) {
		t.Error("expected port 80 not to be found")
	}
}

func TestPortInUseIn_emptyOutput(t *testing.T) {
	if PortInUseIn("80", "") {
		t.Error("expected false for empty output")
	}
}

func TestPortInUseIn_partialPortMatch(t *testing.T) {
	output := "LISTEN  0  128  127.0.0.1:8080  0.0.0.0:*"
	if PortInUseIn("80", output) {
		t.Error("port 80 should not match :8080")
	}
}

func TestPortInUse_unusedPort(t *testing.T) {
	if PortInUse("59999") {
		t.Error("port 59999 should not be in use")
	}
}

func TestPortListOutput_format(t *testing.T) {
	output := PortListOutput()
	if output != "" && !strings.Contains(output, "LISTEN") && !strings.Contains(output, "State") {
		t.Errorf("PortListOutput should contain LISTEN headers, got: %.100s", output)
	}
}

// S1.4 asks for the port strategy to be confirmed on every run, so the finding
// has to be emitted unconditionally rather than only when something is wrong.
// Its verdict depends on the host and is not asserted here; that logic is
// covered in internal/ports.
func TestDoctorAlwaysReportsThePortStrategy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	rep, err := RunDoctorReport()
	if err != nil {
		t.Fatalf("RunDoctorReport: %v", err)
	}
	for _, f := range rep.Findings {
		if strings.HasPrefix(f.Name, "port strategy") {
			return
		}
	}
	t.Error("doctor reported no port strategy finding")
}

// A failed strategy has to carry the commands that repair it: servlo will not
// run them, so a hint without them leaves the operator with nothing to do.
func TestPortFixHintCarriesTheCommands(t *testing.T) {
	got := portFixHint([]string{"sudo sysctl -w x=1", "sudo sh -c 'echo x'"})
	for _, want := range []string{"sudo sysctl -w x=1", "sudo sh -c 'echo x'"} {
		if !strings.Contains(got, want) {
			t.Errorf("hint %q is missing %q", got, want)
		}
	}
}

func TestPortFixHintWithNothingToRun(t *testing.T) {
	if got := portFixHint(nil); got == "" {
		t.Error("empty hint for a finding with no commands")
	}
}
