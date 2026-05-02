package control

import (
	"encoding/json"
	"path/filepath"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/diagnostics"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
)

type MetaHandlers struct {
	Version string
	DataDir string
	Exec    diagnostics.ExecCommandFunc
}

func (h MetaHandlers) IPCContract(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	return ipc.NewContract(ProtocolVersion, config.CurrentSchemaVersion, ipc.SupportedCapabilities()), nil
}

func (h MetaHandlers) VersionInfo(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	exec := h.Exec
	if exec == nil {
		exec = func(name string, args ...string) (string, error) {
			return "", nil
		}
	}
	modulePaths := modulecontract.NewPaths(h.DataDir)
	moduleVersion := diagnostics.ReadModuleVersion()
	daemonPID, socketInode := DaemonIdentity(modulePaths.DaemonSocket())
	singBoxPath := filepath.Join(modulePaths.BinDir(), "sing-box")
	info := addIPCContractFields(map[string]interface{}{
		"daemon":            h.Version,
		"core":              h.Version,
		"daemonctl":         h.Version,
		"module":            moduleVersion,
		"current_release":   diagnostics.ReleaseIntegrityReport(h.DataDir),
		"runtime_preflight": diagnostics.RuntimePreflightReport(h.DataDir),
		"sing_box":          diagnostics.SingBoxVersion(singBoxPath, 20, exec),
		"control_protocol":  ProtocolVersion,
		"panel_min_version": h.Version,
	})
	return addCompatibilityIdentityFields(info, h.Version, moduleVersion["version"], daemonPID, socketInode), nil
}
