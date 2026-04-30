package control

import (
	"encoding/json"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/audit"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/health"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
)

type AuditHandlers struct {
	ConfigPath    string
	DataDir       string
	CurrentConfig CurrentConfigFunc
	RunHealth     func() *health.HealthResult
	CoreState     func() string
	Now           func() time.Time
}

func (h AuditHandlers) Audit(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	var healthResult *health.HealthResult
	if h.RunHealth != nil {
		healthResult = h.RunHealth()
	}
	var cfg *config.Config
	if h.CurrentConfig != nil {
		cfg = h.CurrentConfig()
	}
	coreState := ""
	if h.CoreState != nil {
		coreState = h.CoreState()
	}
	return audit.BuildReport(cfg, h.ConfigPath, h.DataDir, healthResult, coreState, h.now()), nil
}

func (h AuditHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}
