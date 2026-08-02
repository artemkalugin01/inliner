package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/aokalugin/inliner/inliner-core/internal/version"
)

func TestVersionCommand(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version command failed: %v\n%s", err, output)
	}

	if strings.TrimSpace(string(output)) != version.Core {
		t.Fatalf("output = %q, want %q", strings.TrimSpace(string(output)), version.Core)
	}
}

func TestMissingCommandShowsUsage(t *testing.T) {
	cmd := exec.Command("go", "run", ".")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("command succeeded, want failure; output=%s", output)
	}

	if !strings.Contains(string(output), "usage: inliner-core <stdio|test-ollama|version>") {
		t.Fatalf("output = %q, want usage", output)
	}
}
