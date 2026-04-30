package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIVersion(t *testing.T) {
	binary := buildBinary(t)

	out, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("version failed: %v\n%s", err, out)
	}
	if got := string(out); got != "lumi 0.1.0\n" {
		t.Errorf("version = %q, want %q", got, "lumi 0.1.0\n")
	}
}

func TestCLIHelp(t *testing.T) {
	binary := buildBinary(t)

	out, err := exec.Command(binary, "help").CombinedOutput()
	if err != nil {
		t.Fatalf("help failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "lumi") {
		t.Errorf("help output missing 'lumi': %s", output)
	}
	if !strings.Contains(output, "test") {
		t.Errorf("help output missing 'test': %s", output)
	}
}

func TestCLITest(t *testing.T) {
	binary := buildBinary(t)

	// Get absolute path to testdata
	testdata, _ := filepath.Abs("../../testdata")

	// Run tests from repo root so lib/ is found
	repoRoot, _ := filepath.Abs("../..")
	runCmd := exec.Command(binary, "test", "-v", testdata)
	runCmd.Dir = repoRoot
	out, err := runCmd.CombinedOutput()

	if err != nil {
		t.Fatalf("test command failed: %v\n%s", err, out)
	}

	output := string(out)
	if !strings.Contains(output, "passed") {
		t.Errorf("expected 'passed' in output: %s", output)
	}
	t.Log(output)
}

func TestCLIRun(t *testing.T) {
	binary := buildBinary(t)

	// Create a temp Lua script
	tmpDir := t.TempDir()
	script := filepath.Join(tmpDir, "hello.lua")
	if err := writeFile(script, `print("hello")`); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(binary, "run", script).CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "hello" {
		t.Errorf("run output = %q, want %q", got, "hello")
	}
}

func TestCLIUnknownCommand(t *testing.T) {
	binary := buildBinary(t)

	cmd := exec.Command(binary, "bogus")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit for unknown command")
	}
	if !strings.Contains(string(out), "unknown command") {
		t.Errorf("expected 'unknown command' in output: %s", out)
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "lumi")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return binary
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
