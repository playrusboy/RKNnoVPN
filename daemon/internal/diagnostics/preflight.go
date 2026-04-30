package diagnostics

import (
	"fmt"
	"path/filepath"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
)

type RuntimePreflight struct {
	ModuleDir        string                `json:"moduleDir"`
	ExpectedBinaries map[string]FileStatus `json:"expectedBinaries"`
	LegacyPaths      map[string]FileStatus `json:"legacyPaths,omitempty"`
	OK               bool                  `json:"ok"`
	Issues           []string              `json:"issues,omitempty"`
	Warnings         []string              `json:"warnings,omitempty"`
}

func RuntimePreflightReport(dataDir string) RuntimePreflight {
	paths := modulecontract.NewPaths(dataDir)
	report := RuntimePreflight{
		ModuleDir:        paths.Dir(),
		ExpectedBinaries: make(map[string]FileStatus),
		LegacyPaths:      make(map[string]FileStatus),
		OK:               true,
	}

	for _, name := range []string{"sing-box", "daemon", "daemonctl"} {
		status := StatFile(filepath.Join(paths.BinDir(), name), true)
		report.ExpectedBinaries[name] = status
		if !status.Exists {
			report.OK = false
			report.Issues = append(report.Issues, fmt.Sprintf("expected binary %s is missing at %s", name, status.Path))
			continue
		}
		if !status.Executable {
			report.OK = false
			report.Issues = append(report.Issues, fmt.Sprintf("expected binary %s is not executable at %s", name, status.Path))
		}
	}

	for label, path := range map[string]string{
		"old_privstack_sign_box":        "/data/adb/privstack/bin/sign-box",
		"old_privstack_sing_box":        "/data/adb/privstack/bin/sing-box",
		"old_privstack_module_sign_box": "/data/adb/modules/privstack/bin/sign-box",
		"old_privstack_module_sing_box": "/data/adb/modules/privstack/bin/sing-box",
		"misspelled_rknnovpn_sign_box":  filepath.Join(paths.BinDir(), "sign-box"),
		"misspelled_current_sign_box":   filepath.Join(paths.Dir(), "current", "bin", "sign-box"),
		"misspelled_release_sign_box":   filepath.Join(paths.ReleasesDir(), "current", "bin", "sign-box"),
	} {
		status := StatFile(path, true)
		report.LegacyPaths[label] = status
		if status.Exists {
			report.Warnings = append(report.Warnings, fmt.Sprintf("legacy/misspelled sing-box path exists: %s", status.Path))
		}
	}
	if len(report.Warnings) == 0 {
		report.LegacyPaths = nil
	}
	return report
}
