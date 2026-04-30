package control

import (
	"encoding/json"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/subscription"
)

type CurrentProfileFunc func() profiledoc.Document

type ApplyProfileFunc func(profiledoc.Document, bool, string, int) (interface{}, *ipc.RPCError)

type ProfileHandlers struct {
	CurrentProfile     CurrentProfileFunc
	ApplyProfile       ApplyProfileFunc
	SubscriptionClient subscription.Client
	Now                func() time.Time
}

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
	next, stats := profiledoc.ImportNodes(current, request.Nodes)
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

func (h ProfileHandlers) ProfileSetActiveNode(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeSetActiveNodeParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	next, err := profiledoc.SetActiveNode(current, request.NodeID)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	return h.applyProfile(next, request.Reload, "profile.setActiveNode", 1)
}

func (h ProfileHandlers) SubscriptionPreview(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeSubscriptionURLParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	preview, err := h.subscriptionClient().Preview(request.URL, current)
	if err != nil {
		return nil, subscriptionRPCError(request.URL, preview.FetchStatus, preview.FetchHeaders, nil, err)
	}
	return preview, nil
}

func (h ProfileHandlers) SubscriptionRefresh(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	request, err := DecodeSubscriptionURLParams(params)
	if err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	current, rpcErr := h.currentProfile()
	if rpcErr != nil {
		return nil, rpcErr
	}
	refresh, err := h.subscriptionClient().ApplyRefresh(request.URL, current)
	if err != nil {
		return nil, subscriptionRPCError(request.URL, refresh.FetchStatus, refresh.FetchHeaders, &refresh, err)
	}
	result, applyErr := h.applyProfile(refresh.Profile, true, "subscription.refresh", len(refresh.Nodes))
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
	if h.CurrentProfile == nil {
		return profiledoc.Document{}, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "profile state provider is not configured"}
	}
	return h.CurrentProfile(), nil
}

func (h ProfileHandlers) applyProfile(doc profiledoc.Document, reload bool, action string, updated int) (interface{}, *ipc.RPCError) {
	if h.ApplyProfile == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "profile apply callback is not configured"}
	}
	return h.ApplyProfile(doc, reload, action, updated)
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
