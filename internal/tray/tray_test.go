//go:build !nogui

package tray

import (
	"os/exec"
	"testing"
)

func TestOffOn_FlipsState(t *testing.T) {
	if got := offOn(true); got != "off" {
		t.Errorf("offOn(true) = %q, want %q", got, "off")
	}
	if got := offOn(false); got != "on" {
		t.Errorf("offOn(false) = %q, want %q", got, "on")
	}
}

func TestRunAndRefresh_CallsRefreshAfterCommand(t *testing.T) {
	called := false
	runAndRefresh(exec.Command("true"), func() { called = true })
	if !called {
		t.Error("refresh should be called after command exits")
	}
}

func TestRunAndRefresh_CallsRefreshEvenWhenCommandFails(t *testing.T) {
	called := false
	runAndRefresh(exec.Command("false"), func() { called = true })
	if !called {
		t.Error("refresh should be called even if the command fails so the menu still redraws")
	}
}
