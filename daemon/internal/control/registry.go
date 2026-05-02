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
		"app.list":                   ipc.WithoutContext(g.App.AppList),
		"app.resolveUid":             ipc.WithoutContext(g.App.ResolveUID),
		"audit":                      ipc.WithoutContext(g.Audit.Audit),
		"backend.applyDesiredState":  ipc.WithoutContext(g.Runtime.BackendApplyDesiredState),
		"backend.reset":              ipc.WithoutContext(g.Runtime.BackendReset),
		"backend.restart":            ipc.WithoutContext(g.Runtime.BackendRestart),
		"backend.start":              ipc.WithoutContext(g.Runtime.BackendStart),
		"backend.status":             ipc.WithoutContext(g.Runtime.BackendStatus),
		"backend.stop":               ipc.WithoutContext(g.Runtime.BackendStop),
		"compat.check":               ipc.WithoutContext(g.Meta.CompatibilityCheck),
		"config-import":              g.Config.ConfigImportContext,
		"config-list":                ipc.WithoutContext(g.Config.ConfigList),
		"diagnostics.health":         g.Diagnostics.DiagnosticsHealthContext,
		"diagnostics.testNodes":      g.Diagnostics.DiagnosticsTestNodesContext,
		"diagnostics.report":         g.Diagnostics.DiagnosticsReportContext,
		"ipc.contract":               ipc.WithoutContext(g.Meta.IPCContract),
		"logs":                       ipc.WithoutContext(g.Logs.Logs),
		"profile.apply":              g.Profile.ProfileApplyContext,
		"profile.commitImportBatch":  g.Profile.ProfileCommitImportBatchContext,
		"profile.dns.patch":          g.Profile.ProfileDNSPatchContext,
		"profile.get":                ipc.WithoutContext(g.Profile.ProfileGet),
		"profile.inbound.patch":      g.Profile.ProfileInboundPatchContext,
		"profile.importNodes":        g.Profile.ProfileImportNodesContext,
		"profile.importNodesBatch":   g.Profile.ProfileImportNodesBatchContext,
		"profile.node.remove":        g.Profile.ProfileNodeRemoveContext,
		"profile.node.upsert":        g.Profile.ProfileNodeUpsertContext,
		"profile.patch":              g.Profile.ProfilePatchContext,
		"profile.routing.patch":      g.Profile.ProfileRoutingPatchContext,
		"profile.setActiveNode":      g.Profile.ProfileSetActiveNodeContext,
		"self-check":                 g.Diagnostics.SelfCheckContext,
		"subscription.commitPreview": g.Profile.SubscriptionCommitPreviewContext,
		"subscription.preview":       g.Profile.SubscriptionPreviewContext,
		"subscription.refresh":       g.Profile.SubscriptionRefreshContext,
		"update-check":               g.Update.UpdateCheckContext,
		"update-download":            g.Update.UpdateDownloadContext,
		"update-install":             g.Update.UpdateInstallContext,
		"version":                    ipc.WithoutContext(g.Meta.VersionInfo),
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
