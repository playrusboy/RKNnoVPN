package control

import (
	"errors"
	"sort"
	"strings"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
)

type Registrar interface {
	Register(method string, handler ipc.Handler)
}

type HandlerGroups struct {
	App         AppHandlers
	Audit       AuditHandlers
	Config      ConfigHandlers
	Diagnostics DiagnosticsHandlers
	Logs        LogHandlers
	Meta        MetaHandlers
	Profile     ProfileHandlers
	Runtime     RuntimeHandlers
	Update      UpdateHandlers
}

func RegisterDaemonHandlers(registrar Registrar, groups HandlerGroups) error {
	return RegisterContractHandlers(registrar, groups.ContractHandlers())
}

func (g HandlerGroups) ContractHandlers() map[string]ipc.Handler {
	return map[string]ipc.Handler{
		"app.list":                  g.App.AppList,
		"app.resolveUid":            g.App.ResolveUID,
		"audit":                     g.Audit.Audit,
		"backend.applyDesiredState": g.Runtime.BackendApplyDesiredState,
		"backend.reset":             g.Runtime.BackendReset,
		"backend.restart":           g.Runtime.BackendRestart,
		"backend.start":             g.Runtime.BackendStart,
		"backend.status":            g.Runtime.BackendStatus,
		"backend.stop":              g.Runtime.BackendStop,
		"config-import":             g.Config.ConfigImport,
		"config-list":               g.Config.ConfigList,
		"diagnostics.health":        g.Runtime.DiagnosticsHealth,
		"diagnostics.testNodes":     g.Runtime.DiagnosticsTestNodes,
		"diagnostics.report":        g.Diagnostics.DiagnosticsReport,
		"ipc.contract":              g.Meta.IPCContract,
		"logs":                      g.Logs.Logs,
		"profile.apply":             g.Profile.ProfileApply,
		"profile.get":               g.Profile.ProfileGet,
		"profile.importNodes":       g.Profile.ProfileImportNodes,
		"profile.setActiveNode":     g.Profile.ProfileSetActiveNode,
		"self-check":                g.Diagnostics.SelfCheck,
		"subscription.preview":      g.Profile.SubscriptionPreview,
		"subscription.refresh":      g.Profile.SubscriptionRefresh,
		"update-check":              g.Update.UpdateCheck,
		"update-download":           g.Update.UpdateDownload,
		"update-install":            g.Update.UpdateInstall,
		"version":                   g.Meta.VersionInfo,
	}
}

func RegisterContractHandlers(registrar Registrar, handlers map[string]ipc.Handler) error {
	if registrar == nil {
		return errors.New("ipc registrar is not configured")
	}
	contractMethods := make(map[string]bool, len(handlers))
	var missing []string
	for _, contract := range ipc.MethodContracts() {
		contractMethods[contract.Method] = true
		if _, ok := handlers[contract.Method]; !ok {
			missing = append(missing, contract.Method)
		}
	}
	var extra []string
	for method := range handlers {
		if !contractMethods[method] {
			extra = append(extra, method)
		}
	}
	var nilHandlers []string
	for method, handler := range handlers {
		if handler == nil {
			nilHandlers = append(nilHandlers, method)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(nilHandlers)
	if len(missing) > 0 || len(extra) > 0 || len(nilHandlers) > 0 {
		parts := make([]string, 0, 3)
		if len(missing) > 0 {
			parts = append(parts, "contract method(s) without daemon handlers: "+strings.Join(missing, ", "))
		}
		if len(extra) > 0 {
			parts = append(parts, "daemon handler(s) without contract methods: "+strings.Join(extra, ", "))
		}
		if len(nilHandlers) > 0 {
			parts = append(parts, "nil daemon handler(s): "+strings.Join(nilHandlers, ", "))
		}
		return errors.New(strings.Join(parts, "; "))
	}
	for _, contract := range ipc.MethodContracts() {
		registrar.Register(contract.Method, handlers[contract.Method])
	}
	return nil
}
