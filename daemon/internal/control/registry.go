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

func (g HandlerGroups) ContractHandlers() map[string]ipc.Handler {
	return map[string]ipc.Handler{
		"app.list":                  ipc.WithoutContext(g.App.AppList),
		"app.resolveUid":            ipc.WithoutContext(g.App.ResolveUID),
		"audit":                     ipc.WithoutContext(g.Audit.Audit),
		"backend.applyDesiredState": ipc.WithoutContext(g.Runtime.BackendApplyDesiredState),
		"backend.reset":             ipc.WithoutContext(g.Runtime.BackendReset),
		"backend.restart":           ipc.WithoutContext(g.Runtime.BackendRestart),
		"backend.start":             ipc.WithoutContext(g.Runtime.BackendStart),
		"backend.status":            ipc.WithoutContext(g.Runtime.BackendStatus),
		"backend.stop":              ipc.WithoutContext(g.Runtime.BackendStop),
		"compat.check":              ipc.WithoutContext(g.Meta.CompatibilityCheck),
		"config-import":             ipc.WithoutContext(g.Config.ConfigImport),
		"config-list":               ipc.WithoutContext(g.Config.ConfigList),
		"diagnostics.health":        ipc.WithoutContext(g.Diagnostics.DiagnosticsHealth),
		"diagnostics.testNodes":     ipc.WithoutContext(g.Diagnostics.DiagnosticsTestNodes),
		"diagnostics.report":        ipc.WithoutContext(g.Diagnostics.DiagnosticsReport),
		"ipc.contract":              ipc.WithoutContext(g.Meta.IPCContract),
		"logs":                      ipc.WithoutContext(g.Logs.Logs),
		"profile.apply":             ipc.WithoutContext(g.Profile.ProfileApply),
		"profile.commitImportBatch": ipc.WithoutContext(g.Profile.ProfileCommitImportBatch),
		"profile.dns.patch":         ipc.WithoutContext(g.Profile.ProfileDNSPatch),
		"profile.get":               ipc.WithoutContext(g.Profile.ProfileGet),
		"profile.inbound.patch":     ipc.WithoutContext(g.Profile.ProfileInboundPatch),
		"profile.importNodes":       ipc.WithoutContext(g.Profile.ProfileImportNodes),
		"profile.importNodesBatch":  ipc.WithoutContext(g.Profile.ProfileImportNodesBatch),
		"profile.node.remove":       ipc.WithoutContext(g.Profile.ProfileNodeRemove),
		"profile.node.upsert":       ipc.WithoutContext(g.Profile.ProfileNodeUpsert),
		"profile.patch":             ipc.WithoutContext(g.Profile.ProfilePatch),
		"profile.routing.patch":     ipc.WithoutContext(g.Profile.ProfileRoutingPatch),
		"profile.setActiveNode":     ipc.WithoutContext(g.Profile.ProfileSetActiveNode),
		"self-check":                ipc.WithoutContext(g.Diagnostics.SelfCheck),
		"subscription.preview":      ipc.WithoutContext(g.Profile.SubscriptionPreview),
		"subscription.refresh":      ipc.WithoutContext(g.Profile.SubscriptionRefresh),
		"update-check":              ipc.WithoutContext(g.Update.UpdateCheck),
		"update-download":           ipc.WithoutContext(g.Update.UpdateDownload),
		"update-install":            ipc.WithoutContext(g.Update.UpdateInstall),
		"version":                   ipc.WithoutContext(g.Meta.VersionInfo),
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
