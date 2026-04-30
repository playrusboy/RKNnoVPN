package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimePreflightRequiresCanonicalBinaries(t *testing.T) {
	dataDir := t.TempDir()
	binDir := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sing-box", "daemon", "daemonctl"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/system/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	report := RuntimePreflightReport(dataDir)
	if !report.OK || len(report.Issues) != 0 {
		t.Fatalf("expected clean runtime preflight, got %#v", report)
	}
	if report.ExpectedBinaries["sing-box"].Path != filepath.Join(binDir, "sing-box") {
		t.Fatalf("unexpected sing-box path: %#v", report.ExpectedBinaries["sing-box"])
	}
}

func TestRuntimePreflightFlagsMissingAndNonExecutableBinaries(t *testing.T) {
	dataDir := t.TempDir()
	binDir := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "sing-box"), []byte("binary\n"), 0644); err != nil {
		t.Fatal(err)
	}

	report := RuntimePreflightReport(dataDir)
	if report.OK {
		t.Fatalf("expected failed runtime preflight, got %#v", report)
	}
	text := strings.Join(report.Issues, "\n")
	for _, want := range []string{
		"sing-box is not executable",
		"daemon is missing",
		"daemonctl is missing",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected issue %q in %s", want, text)
		}
	}
}

func TestRuntimePreflightFlagsMisspelledSignBoxInModuleDir(t *testing.T) {
	dataDir := t.TempDir()
	binDir := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sing-box", "daemon", "daemonctl", "sign-box"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/system/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	report := RuntimePreflightReport(dataDir)
	if !report.OK {
		t.Fatalf("misspelled extra binary should be a warning, got %#v", report)
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "sign-box") {
		t.Fatalf("expected sign-box warning, got %#v", report.Warnings)
	}
}
