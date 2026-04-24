package main

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandExitCodeUsesTypedExitError(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	err := silentCommandExit(cmd)

	if got := commandExitCode(err); got != 1 {
		t.Fatalf("commandExitCode() = %d, want 1", got)
	}
	if !cmd.SilenceErrors {
		t.Fatal("silentCommandExit should suppress duplicate Cobra error output")
	}
	if !cmd.SilenceUsage {
		t.Fatal("silentCommandExit should suppress usage output for rendered errors")
	}
}

func TestCommandExitCodeUsesCustomTypedExitError(t *testing.T) {
	if got := commandExitCode(commandExitError{code: 7}); got != 7 {
		t.Fatalf("commandExitCode() = %d, want 7", got)
	}
}

func TestCommandExitCodeDefaultsToOne(t *testing.T) {
	if got := commandExitCode(errors.New("boom")); got != 1 {
		t.Fatalf("commandExitCode() = %d, want 1", got)
	}
}
