package updater

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type InstallArtifacts struct {
	UpdateDir    string
	ModulePath   string
	ApkPath      string
	ModuleExists bool
	ApkExists    bool
}

func ResolveInstallArtifacts(dataDir string, params *json.RawMessage) (InstallArtifacts, error) {
	updateDir := filepath.Join(dataDir, "update")
	expectedModulePath := filepath.Join(updateDir, "module.zip")
	expectedApkPath := filepath.Join(updateDir, "panel.apk")

	request := struct {
		ModulePath string `json:"module_path"`
		ApkPath    string `json:"apk_path"`
	}{
		ModulePath: expectedModulePath,
		ApkPath:    expectedApkPath,
	}
	if params != nil {
		if err := json.Unmarshal(*params, &request); err != nil {
			return InstallArtifacts{}, fmt.Errorf("invalid params: %w", err)
		}
		if request.ModulePath == "" {
			request.ModulePath = expectedModulePath
		}
		if request.ApkPath == "" {
			request.ApkPath = expectedApkPath
		}
	}

	modulePath := filepath.Clean(request.ModulePath)
	apkPath := filepath.Clean(request.ApkPath)
	if modulePath != filepath.Clean(expectedModulePath) || apkPath != filepath.Clean(expectedApkPath) {
		return InstallArtifacts{}, fmt.Errorf("update-install only accepts verified artifacts from the canonical update directory")
	}

	moduleExists := fileExists(modulePath)
	apkExists := fileExists(apkPath)
	if !moduleExists && !apkExists {
		return InstallArtifacts{}, fmt.Errorf("no downloaded update files found")
	}
	if moduleExists != apkExists {
		return InstallArtifacts{}, fmt.Errorf("this update requires both module and APK artifacts")
	}

	return InstallArtifacts{
		UpdateDir:    updateDir,
		ModulePath:   modulePath,
		ApkPath:      apkPath,
		ModuleExists: moduleExists,
		ApkExists:    apkExists,
	}, nil
}

func ResolveVerifiedInstallArtifacts(dataDir string, params *json.RawMessage) (InstallArtifacts, error) {
	artifacts, err := ResolveInstallArtifacts(dataDir, params)
	if err != nil {
		return InstallArtifacts{}, err
	}
	if err := VerifyDownloadedUpdate(artifacts.ModulePath, artifacts.ApkPath); err != nil {
		return InstallArtifacts{}, fmt.Errorf("update artifacts are not checksum-verified: %w", err)
	}
	verifiedManifest, err := ReadVerifiedUpdateManifest(artifacts.UpdateDir)
	if err != nil {
		return InstallArtifacts{}, fmt.Errorf("update artifacts are not manifest-verified: %w", err)
	}
	modulePreflight, err := PreflightModuleUpdate(artifacts.ModulePath, dataDir)
	if err != nil {
		return InstallArtifacts{}, fmt.Errorf("module update preflight failed: %w", err)
	}
	if modulePreflight.Version != NormalizeVersionTag(verifiedManifest.LatestVersion) {
		return InstallArtifacts{}, fmt.Errorf(
			"module version %s does not match verified update version %s",
			modulePreflight.Version,
			NormalizeVersionTag(verifiedManifest.LatestVersion),
		)
	}
	return artifacts, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
