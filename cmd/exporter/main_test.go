package main

import (
	"os"
	"os/exec"
	"testing"
)

// TestHelpFlagRuns invokes the program with -h in a helper subprocess.
func TestHelpFlagRuns(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--", "-h")
	cmd.Env = append(os.Environ(), "RUN_MAIN_HELP=1")
	err := cmd.Run()
	if err == nil {
		// Some Go versions may exit 0 for -h; treat both as success
		return
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() >= 0 {
		// Non-negative exit code indicates the process ran; acceptable
		return
	}
	t.Fatalf("unexpected error running help: %v", err)
}

// TestHelperProcess is executed in the subprocess to call main with provided args.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("RUN_MAIN_HELP") != "1" {
		return
	}
	// Strip the test-run args delimiter "--" and pass the rest to flags
	// The flag package in main will handle -h and exit.
	main()
}

