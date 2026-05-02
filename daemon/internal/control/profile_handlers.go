package control

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	applytx "github.com/youtubediscord/RKNnoVPN/daemon/internal/apply"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/subscription"
)

type ProfileHandlers struct {
	CurrentConfig         CurrentConfigFunc
	PersistConfigMutation PersistConfigMutationFunc
	RuntimeStatus         RuntimeStatusFunc
	SubscriptionClient    subscription.Client
	ImportBatches         *ImportBatchStore
	SubscriptionPreviews  *SubscriptionPreviewCache
	Now                   func() time.Time
}

var defaultImportBatchStore = NewImportBatchStore()
var defaultSubscriptionPreviewCache = NewSubscriptionPreviewCache()

func (h ProfileHandlers) ProfileGet(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	return current, nil
}

func (h ProfileHandlers) ProfileApply(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeProfileApplyParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	return h.applyProfile(request.Profile, request.Reload, "profile.apply", -1)
}

func (h ProfileHandlers) ProfileImportNodes(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeImportNodesParams(params, h.now())
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	next, stats := profiledoc.MergeNodes(current, request.Nodes)
	result, rpcErr := h.applyProfile(next, request.Reload, "profile.importNodes", len(request.Nodes))
	if rpcErr != nil {
		return nil, rpcErr
	}
	if obj, ok := result.(map[string]interface{}); ok {
		obj["imported"] = len(request.Nodes)
		obj["merge"] = stats
	}
	return result, nil
}

func (h ProfileHandlers) ProfileImportNodesBatch(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeImportNodesBatchParams(params, h.now())
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	state, err := h.importBatchStore().Add(request.BatchID, request.TotalBatches, request.Nodes, h.now())
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	return map[string]interface{}{
		"batchId":         state.BatchID,
		"totalBatches":    state.TotalBatches,
		"receivedBatches": state.ReceivedBatches,
		"receivedNodes":   state.ReceivedNodes,
		"ready":           state.Ready,
	}, nil
}

func (h ProfileHandlers) ProfileCommitImportBatch(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeCommitImportBatchParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	nodes, state, err := h.importBatchStore().Commit(request.BatchID, h.now())
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error(), Data: map[string]interface{}{
			"batchId":         state.BatchID,
			"totalBatches":    state.TotalBatches,
			"receivedBatches": state.ReceivedBatches,
			"receivedNodes":   state.ReceivedNodes,
			"ready":           state.Ready,
		}}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	next, stats := profiledoc.MergeNodes(current, nodes)
	result, rpcErr := h.applyProfile(next, request.Reload, "profile.commitImportBatch", len(nodes))
	if rpcErr != nil {
		return nil, rpcErr
	}
	if obj, ok := result.(map[string]interface{}); ok {
		obj["batchId"] = state.BatchID
		obj["totalBatches"] = state.TotalBatches
		obj["imported"] = len(nodes)
		obj["merge"] = stats
	}
	return result, nil
}

func (h ProfileHandlers) ProfileSetActiveNode(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeSetActiveNodeParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	if current.ActiveNodeID == request.NodeID {
		if _, err := profiledoc.SetActiveNode(current, request.NodeID); err != nil {
			return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
		}
		return h.profileNoop(current, "profile.setActiveNode", 0)
	}
	next, err := profiledoc.SetActiveNode(current, request.NodeID)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	return h.applyProfile(next, request.Reload, "profile.setActiveNode", 1)
}

func (h ProfileHandlers) ProfilePatch(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeProfilePatchParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	if request.HasActiveNodeID {
		activeOnlyPatch := request.Routing == nil && request.DNS == nil && request.Inbounds == nil
		if activeOnlyPatch && current.ActiveNodeID == request.ActiveNodeID {
			if request.ActiveNodeID != "" {
				if _, err := profiledoc.SetActiveNode(current, request.ActiveNodeID); err != nil {
					return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
				}
			}
			return h.profileNoop(current, "profile.patch", 0)
		}
		if request.ActiveNodeID == "" {
			current = profiledoc.ClearActiveNode(current)
		} else {
			current, err = profiledoc.SetActiveNode(current, request.ActiveNodeID)
			if err != nil {
				return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
			}
		}
	}
	if request.Routing != nil {
		current.Routing = *request.Routing
	}
	if request.DNS != nil {
		current.DNS = *request.DNS
	}
	if request.Inbounds != nil {
		current.Inbounds = *request.Inbounds
	}
	return h.applyProfile(current, request.Reload, "profile.patch", -1)
}

func (h ProfileHandlers) ProfileNodeUpsert(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeNodeUpsertParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	next, err := profiledoc.UpsertNode(current, request.Node)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	return h.applyProfile(next, request.Reload, "profile.node.upsert", 1)
}

func (h ProfileHandlers) ProfileNodeRemove(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeNodeRemoveParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	next, err := profiledoc.RemoveNode(current, request.NodeID)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	return h.applyProfile(next, request.Reload, "profile.node.remove", 1)
}

func (h ProfileHandlers) ProfileRoutingPatch(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeRoutingPatchParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	current.Routing = request.Routing
	return h.applyProfile(current, request.Reload, "profile.routing.patch", -1)
}

func (h ProfileHandlers) ProfileDNSPatch(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeDNSPatchParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	current.DNS = request.DNS
	return h.applyProfile(current, request.Reload, "profile.dns.patch", -1)
}

func (h ProfileHandlers) ProfileInboundPatch(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeInboundPatchParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	current.Inbounds = request.Inbounds
	return h.applyProfile(current, request.Reload, "profile.inbound.patch", -1)
}

func (h ProfileHandlers) SubscriptionPreview(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	return h.SubscriptionPreviewContext(context.Background(), params)
}

func (h ProfileHandlers) SubscriptionPreviewContext(ctx context.Context, params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeSubscriptionURLParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	preview, err := h.subscriptionClient().PreviewContext(ctx, request.URL, current)
	if err != nil {
		return nil, subscriptionRPCError(request.URL, preview.FetchStatus, preview.FetchHeaders, nil, err)
	}
	previewID, cacheErr := h.subscriptionPreviewCache().Put(preview, h.now())
	if cacheErr != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: cacheErr.Error()}
	}
	preview.PreviewID = previewID
	return preview, nil
}

func (h ProfileHandlers) SubscriptionRefresh(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	return h.SubscriptionRefreshContext(context.Background(), params)
}

func (h ProfileHandlers) SubscriptionRefreshContext(ctx context.Context, params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeSubscriptionURLParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	refresh, err := h.subscriptionClient().ApplyRefreshContext(ctx, request.URL, current)
	if err != nil {
		return nil, subscriptionRPCError(request.URL, refresh.FetchStatus, refresh.FetchHeaders, &refresh, err)
	}
	return h.applySubscriptionRefresh(refresh, "subscription.refresh")
}

func (h ProfileHandlers) SubscriptionCommitPreview(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeCommitSubscriptionPreviewParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	preview, ok := h.subscriptionPreviewCache().Take(request.PreviewID, h.now())
	if !ok {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: "subscription preview is missing or expired"}
	}
	refresh, err := subscription.ApplyPreview(current, preview)
	if err != nil {
		return nil, subscriptionRPCError(preview.Source.URL, refresh.FetchStatus, refresh.FetchHeaders, &refresh, err)
	}
	return h.applySubscriptionRefresh(refresh, "subscription.commitPreview")
}

func (h ProfileHandlers) applySubscriptionRefresh(refresh subscription.RefreshResult, action string) (interface{}, *ipc.RPCError) {
	result, applyErr := h.applyProfile(refresh.Profile, true, action, len(refresh.Nodes))
	if applyErr != nil {
		return nil, applyErr
	}
	if obj, ok := result.(map[string]interface{}); ok {
		response := refresh.Response()
		obj["source"] = response.Source
		obj["subscription"] = response.Subscription
		obj["imported"] = response.Imported
		obj["parseFailures"] = response.ParseFailures
		obj["rejected"] = response.Rejected
		obj["rejectedNodes"] = response.RejectedNodes
		obj["merge"] = response.Merge
	}
	return result, nil
}

func (h ProfileHandlers) currentProfile() (profiledoc.Document, *ipc.RPCError) {
	current, rpcErr := h.currentConfig()
	if rpcErr != nil {
		return profiledoc.Document{}, rpcErr
	}
	return profiledoc.FromConfig(current), nil
}

func (h ProfileHandlers) applyProfile(doc profiledoc.Document, reload bool, action string, updated int) (interface{}, *ipc.RPCError) {
	before, hasBefore := h.runtimeStatus()
	if !hasBefore {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "runtime status provider is not configured"}
	}
	current, rpcErr := h.currentConfig()
	if rpcErr != nil {
		return nil, rpcErr
	}
	nextCfg, warnings, err := profiledoc.ApplyToConfig(current, doc)
	if err != nil {
		return nil, ProfileValidationRPCError(action, before, err, warnings, updated)
	}
	mutation, err := h.persistConfigMutation(nextCfg, reload, action)
	if err != nil {
		status, ok := h.runtimeStatus()
		if !ok {
			status = before
		}
		return nil, ProfileRPCErrorSaved(action, err, mutation.ConfigSaved, status, before, warnings, updated)
	}
	status, ok := h.runtimeStatus()
	if !ok {
		status = before
	}
	result := ProfileSuccess(action, reload, mutation.RuntimeWasRunning, status, before, warnings, updated)
	result["profile"] = profiledoc.FromConfig(nextCfg)
	return result, nil
}

func (h ProfileHandlers) profileNoop(doc profiledoc.Document, action string, updated int) (interface{}, *ipc.RPCError) {
	status, ok := h.runtimeStatus()
	if !ok {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "runtime status provider is not configured"}
	}
	result := ProfileOperation(
		action,
		"noop",
		false,
		false,
		"not_requested",
		status.AppliedState.Generation,
		status.AppliedState.Generation,
		"",
		"",
		nil,
		nil,
		updated,
	)
	result["ok"] = true
	result["runtimeStatus"] = status
	result["profile"] = doc
	return result, nil
}

func (h ProfileHandlers) currentConfig() (*config.Config, *ipc.RPCError) {
	if h.CurrentConfig == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "config state provider is not configured"}
	}
	current := h.CurrentConfig()
	if current == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "config state is not available"}
	}
	return current, nil
}

func (h ProfileHandlers) persistConfigMutation(nextCfg *config.Config, reload bool, action string) (applytx.ConfigTransactionResult, error) {
	if h.PersistConfigMutation == nil {
		return applytx.ConfigTransactionResult{}, fmt.Errorf("profile mutation callback is not configured")
	}
	return h.PersistConfigMutation(nextCfg, reload, action)
}

func (h ProfileHandlers) runtimeStatus() (runtimev2.Status, bool) {
	if h.RuntimeStatus == nil {
		return runtimev2.Status{}, false
	}
	return h.RuntimeStatus()
}

func (h ProfileHandlers) subscriptionClient() subscription.Client {
	client := h.SubscriptionClient
	if client.Fetcher == nil {
		client.Fetcher = subscription.FetcherFunc(subscription.FetchURL)
	}
	if client.Now == nil {
		client.Now = time.Now
	}
	if h.Now != nil {
		client.Now = h.Now
	}
	return client
}

func (h ProfileHandlers) importBatchStore() *ImportBatchStore {
	if h.ImportBatches != nil {
		return h.ImportBatches
	}
	return defaultImportBatchStore
}

func (h ProfileHandlers) subscriptionPreviewCache() *SubscriptionPreviewCache {
	if h.SubscriptionPreviews != nil {
		return h.SubscriptionPreviews
	}
	return defaultSubscriptionPreviewCache
}

func (h ProfileHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func subscriptionRPCError(rawURL string, status int, headers map[string]string, refresh *subscription.RefreshResult, err error) *ipc.RPCError {
	code := ipc.CodeInternalError
	switch subscription.ClassifyError(rawURL, err) {
	case subscription.ErrorInvalidParams:
		code = ipc.CodeInvalidParams
	case subscription.ErrorConfig:
		code = ipc.CodeConfigError
	}
	data := map[string]interface{}{
		"status":  status,
		"headers": headers,
	}
	if refresh != nil {
		data["parseFailures"] = refresh.ParseFailures
		data["rejected"] = len(refresh.RejectedNodes)
		data["rejectedNodes"] = refresh.RejectedNodes
		data["subscription"] = refresh.Subscription
		data["source"] = refresh.Source
	}
	return &ipc.RPCError{
		Code:    code,
		Message: err.Error(),
		Data:    data,
	}
}
