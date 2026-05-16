package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunCLIPrintsTopLevelHelp verifies help exits successfully and describes commands.
func TestRunCLIPrintsTopLevelHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runCLI([]string{"--help"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout.String(), "what-ttft run [flags]") {
		t.Fatalf("stdout missing run usage:\n%s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

// TestRunCLIRejectsUnknownCommand verifies unknown commands return a usage error.
func TestRunCLIRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runCLI([]string{"bogus"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), `unknown command "bogus"`) {
		t.Fatalf("stderr missing unknown command:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}
