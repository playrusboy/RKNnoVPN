package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/diagnostics"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

const runtimeV2CompatibilityMaxAge = 5 * time.Minute

func (d *daemon) initRuntimeV2() {
	d.runtimeV2 = runtimev2.NewOrchestrator(
		d.desiredStateV2(),
		newRootRuntimeBackend(d),
	)
	d.runtimeV2.SetStatusObserver(func(status runtimev2.Status) {
		if err := runtimev2.WriteRuntimeState(d.dataDir, status); err != nil {
			log.Printf("runtime_state persist failed: %v", err)
		}
	})
	d.refreshRuntimeV2Compatibility()
	if err := runtimev2.WriteRuntimeState(d.dataDir, d.runtimeV2.Status()); err != nil {
		log.Printf("runtime_state initial persist failed: %v", err)
	}
}

func (d *daemon) maybeRefreshRuntimeV2Compatibility() {
	if d.cachedReleaseIntegrityValid(runtimeV2CompatibilityMaxAge) {
		return
	}
	d.refreshRuntimeV2Compatibility()
}

func (d *daemon) refreshRuntimeV2Compatibility() {
	if d.runtimeV2 == nil {
		return
	}
	release := d.cachedReleaseIntegrity(0)
	moduleVersion := diagnostics.ReadModuleVersion()["version"]
	daemonPID, socketInode := control.DaemonIdentity(modulecontract.NewPaths(d.dataDir).DaemonSocket())
	fingerprint := control.CompatibilityFingerprint(Version, moduleVersion, ipc.ContractHash(), daemonPID, socketInode)
	contracts := ipc.MethodContracts()
	methods := make([]runtimev2.MethodCapability, 0, len(contracts))
	for _, contract := range contracts {
		methods = append(methods, runtimev2.MethodCapability{
			Method:        contract.Method,
			Capability:    contract.Capability,
			Compatibility: contract.Compatibility,
			Mutating:      contract.Mutating,
			Async:         contract.Async,
		})
	}
	d.runtimeV2.SetCompatibility(runtimev2.CompatibilityStatus{
		DaemonVersion:            Version,
		ModuleVersion:            moduleVersion,
		CurrentReleaseVersion:    release.Version,
		CurrentReleaseOK:         release.OK,
		CurrentReleaseError:      releaseIntegrityStatusDetail(release),
		ControlProtocolVersion:   control.ProtocolVersion,
		SchemaVersion:            config.CurrentSchemaVersion,
		IPCContractVersion:       ipc.ContractVersion(),
		ContractHash:             ipc.ContractHash(),
		CompatibilityFingerprint: fingerprint,
		DaemonPID:                daemonPID,
		SocketInode:              socketInode,
		PanelMinVersion:          Version,
		Capabilities:             ipc.SupportedCapabilities(),
		SupportedMethods:         ipc.SupportedMethods(),
		APKRequiredMethods:       ipc.APKRequiredMethods(),
		ErrorCodes:               ipc.ErrorCodes(),
		CompatibilityPolicies:    ipc.CompatibilityPolicies(),
		OperationPolicies:        runtimeOperationPolicies(ipc.OperationPolicies()),
		Methods:                  methods,
	})
}

func (d *daemon) cachedReleaseIntegrity(maxAge time.Duration) diagnostics.ReleaseIntegrity {
	if d.cachedReleaseIntegrityValid(maxAge) {
		d.compatibilityMu.Lock()
		defer d.compatibilityMu.Unlock()
		return d.releaseIntegrity.value
	}

	report := diagnostics.ReleaseIntegrityReport(d.dataDir)
	releasePath, manifestPath, manifestMTime, manifestSize := releaseIntegrityFingerprintFromReport(report)

	d.compatibilityMu.Lock()
	d.releaseIntegrity = cachedReleaseIntegrity{
		value:         report,
		releasePath:   releasePath,
		manifestPath:  manifestPath,
		manifestMTime: manifestMTime,
		manifestSize:  manifestSize,
		checkedAt:     time.Now(),
	}
	d.compatibilityMu.Unlock()
	return report
}

func (d *daemon) cachedReleaseIntegrityValid(maxAge time.Duration) bool {
	d.compatibilityMu.Lock()
	cache := d.releaseIntegrity
	d.compatibilityMu.Unlock()
	if cache.checkedAt.IsZero() {
		return false
	}
	if maxAge <= 0 {
		return false
	}
	if time.Since(cache.checkedAt) >= maxAge {
		return false
	}

	releasePath, manifestPath, manifestMTime, manifestSize := releaseIntegrityFingerprint(d.dataDir)
	return cache.releasePath == releasePath &&
		cache.manifestPath == manifestPath &&
		cache.manifestMTime.Equal(manifestMTime) &&
		cache.manifestSize == manifestSize
}

func releaseIntegrityFingerprintFromReport(report diagnostics.ReleaseIntegrity) (string, string, time.Time, int64) {
	if report.ManifestPath == "" {
		return report.ReleasePath, "", time.Time{}, -1
	}
	info, err := os.Stat(report.ManifestPath)
	if err != nil {
		return report.ReleasePath, report.ManifestPath, time.Time{}, -1
	}
	return report.ReleasePath, report.ManifestPath, info.ModTime(), info.Size()
}

func releaseIntegrityFingerprint(dataDir string) (string, string, time.Time, int64) {
	currentPath := filepath.Join(dataDir, "current")
	releasePath, err := filepath.EvalSymlinks(currentPath)
	if err != nil {
		return "", "", time.Time{}, -1
	}
	manifestPath := filepath.Join(releasePath, "install-manifest.json")
	info, err := os.Stat(manifestPath)
	if err != nil {
		return releasePath, manifestPath, time.Time{}, -1
	}
	return releasePath, manifestPath, info.ModTime(), info.Size()
}

func runtimeOperationPolicies(policies map[string]ipc.OperationPolicy) map[string]runtimev2.OperationPolicy {
	if len(policies) == 0 {
		return nil
	}
	result := make(map[string]runtimev2.OperationPolicy, len(policies))
	for operationType, policy := range policies {
		result[operationType] = runtimev2.OperationPolicy{
			Stages:     append([]string(nil), policy.Stages...),
			ErrorCodes: append([]string(nil), policy.ErrorCodes...),
		}
	}
	return result
}

func releaseIntegrityStatusDetail(release diagnostics.ReleaseIntegrity) string {
	if release.OK {
		return ""
	}
	details := make([]string, 0, 4)
	if release.Error != "" {
		details = append(details, release.Error)
	}
	if release.MissingCurrent {
		details = append(details, "current release link missing")
	}
	if release.MissingManifest {
		details = append(details, "manifest missing")
	}
	if len(release.MissingFiles) > 0 {
		details = append(details, fmt.Sprintf("%d file(s) missing", len(release.MissingFiles)))
	}
	if len(release.Mismatches) > 0 {
		details = append(details, fmt.Sprintf("%d file hash mismatch(es)", len(release.Mismatches)))
	}
	return strings.Join(details, "; ")
}
