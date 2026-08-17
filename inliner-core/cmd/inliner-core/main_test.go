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

	if !strings.Contains(string(output), "usage: inliner-core <stdio|debug|test-ollama|version>") {
		t.Fatalf("output = %q, want usage", output)
	}
}

func TestParseDebugArgs(t *testing.T) {
	verbose, err := parseDebugArgs(nil)
	if err != nil || verbose {
		t.Fatalf("parseDebugArgs(nil) = %v, %v; want false, nil", verbose, err)
	}
	verbose, err = parseDebugArgs([]string{"--verbose"})
	if err != nil || !verbose {
		t.Fatalf("parseDebugArgs(--verbose) = %v, %v; want true, nil", verbose, err)
	}
	if _, err := parseDebugArgs([]string{"--responses"}); err == nil {
		t.Fatal("parseDebugArgs accepted unknown option")
	}
}
