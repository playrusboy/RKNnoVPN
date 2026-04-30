package control

import (
	"encoding/json"
	"path/filepath"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/diagnostics"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
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
	singBoxPath := filepath.Join(h.DataDir, "bin", "sing-box")
	return addIPCContractFields(map[string]interface{}{
		"daemon":            h.Version,
		"core":              h.Version,
		"daemonctl":         h.Version,
		"module":            diagnostics.ReadModuleVersion(),
		"current_release":   diagnostics.ReleaseIntegrityReport(h.DataDir),
		"sing_box":          diagnostics.SingBoxVersion(singBoxPath, 20, exec),
		"control_protocol":  ProtocolVersion,
		"panel_min_version": h.Version,
	}), nil
}
