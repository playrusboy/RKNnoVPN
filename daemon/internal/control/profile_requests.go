package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
)

type ProfileApplyRequest struct {
	Profile profiledoc.Document
	Reload  bool
}

type ImportNodesRequest struct {
	Nodes  []profiledoc.Node
	Reload bool
}

type ImportNodesBatchRequest struct {
	BatchID      string
	TotalBatches int
	Nodes        []profiledoc.Node
}

type CommitImportBatchRequest struct {
	BatchID string
	Reload  bool
}

type SetActiveNodeRequest struct {
	NodeID string
	Reload bool
}

type ProfilePatchRequest struct {
	Reload          bool
	HasActiveNodeID bool
	ActiveNodeID    string
	Routing         *profiledoc.RoutingConfig
	DNS             *profiledoc.DNSConfig
	Inbounds        *profiledoc.InboundsConfig
}

type NodeUpsertRequest struct {
	Node   profiledoc.Node
	Reload bool
}

type NodeRemoveRequest struct {
	NodeID string
	Reload bool
}

type RoutingPatchRequest struct {
	Routing profiledoc.RoutingConfig
	Reload  bool
}

type DNSPatchRequest struct {
	DNS    profiledoc.DNSConfig
	Reload bool
}

type InboundPatchRequest struct {
	Inbounds profiledoc.InboundsConfig
	Reload   bool
}

type SubscriptionURLRequest struct {
	URL string
}

func DecodeProfileApplyParams(params *json.RawMessage) (ProfileApplyRequest, error) {
	var result ProfileApplyRequest
	result.Reload = true
	if params == nil {
		return result, fmt.Errorf("params required: profile document")
	}
	var p struct {
		Profile *profiledoc.Document `json:"profile"`
		Reload  *bool                `json:"reload"`
	}
	if hasJSONField(*params, "profile") {
		decoder := json.NewDecoder(bytes.NewReader(*params))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&p); err != nil {
			return result, fmt.Errorf("invalid profile.apply params: %w", err)
		}
	} else {
		doc, err := profiledoc.DecodeStrictDocument(*params)
		if err != nil {
			return result, fmt.Errorf("invalid profile: %w", err)
		}
		p.Profile = &doc
	}
	if p.Profile == nil {
		return result, fmt.Errorf("profile is required")
	}
	if p.Reload != nil {
		result.Reload = *p.Reload
	}
	result.Profile = *p.Profile
	return result, nil
}

func DecodeImportNodesParams(params *json.RawMessage, now time.Time) (ImportNodesRequest, error) {
	var result ImportNodesRequest
	result.Reload = true
	if params == nil {
		return result, fmt.Errorf("params required: {\"nodes\": [...]}")
	}
	var p struct {
		Nodes  []profiledoc.Node `json:"nodes"`
		Reload *bool             `json:"reload"`
	}
	if err := json.Unmarshal(*params, &p); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	if len(p.Nodes) == 0 {
		return result, fmt.Errorf("nodes must not be empty")
	}
	if p.Reload != nil {
		result.Reload = *p.Reload
	}
	if now.IsZero() {
		now = time.Now()
	}
	createdAt := now.UnixMilli()
	for i := range p.Nodes {
		p.Nodes[i].Stale = false
		p.Nodes[i].Source = profiledoc.NodeSource{Type: "MANUAL"}
		if p.Nodes[i].CreatedAt == 0 {
			p.Nodes[i].CreatedAt = createdAt
		}
	}
	result.Nodes = p.Nodes
	return result, nil
}

func DecodeImportNodesBatchParams(params *json.RawMessage, now time.Time) (ImportNodesBatchRequest, error) {
	var result ImportNodesBatchRequest
	if params == nil {
		return result, fmt.Errorf("params required: {\"batchId\": \"...\", \"totalBatches\": 1, \"nodes\": [...]}")
	}
	var p struct {
		BatchID      string            `json:"batchId"`
		TotalBatches int               `json:"totalBatches"`
		Nodes        []profiledoc.Node `json:"nodes"`
	}
	if err := json.Unmarshal(*params, &p); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	if p.BatchID == "" {
		return result, fmt.Errorf("batchId is required")
	}
	if p.TotalBatches <= 0 {
		return result, fmt.Errorf("totalBatches must be positive")
	}
	if len(p.Nodes) == 0 {
		return result, fmt.Errorf("nodes must not be empty")
	}
	if now.IsZero() {
		now = time.Now()
	}
	createdAt := now.UnixMilli()
	for i := range p.Nodes {
		p.Nodes[i].Stale = false
		p.Nodes[i].Source = profiledoc.NodeSource{Type: "MANUAL"}
		if p.Nodes[i].CreatedAt == 0 {
			p.Nodes[i].CreatedAt = createdAt
		}
	}
	result.BatchID = p.BatchID
	result.TotalBatches = p.TotalBatches
	result.Nodes = p.Nodes
	return result, nil
}

func DecodeCommitImportBatchParams(params *json.RawMessage) (CommitImportBatchRequest, error) {
	var result CommitImportBatchRequest
	result.Reload = true
	if params == nil {
		return result, fmt.Errorf("params required: {\"batchId\": \"...\"}")
	}
	var p struct {
		BatchID string `json:"batchId"`
		Reload  *bool  `json:"reload"`
	}
	if err := json.Unmarshal(*params, &p); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	if p.BatchID == "" {
		return result, fmt.Errorf("batchId is required")
	}
	if p.Reload != nil {
		result.Reload = *p.Reload
	}
	result.BatchID = p.BatchID
	return result, nil
}

func DecodeSetActiveNodeParams(params *json.RawMessage) (SetActiveNodeRequest, error) {
	var result SetActiveNodeRequest
	result.Reload = true
	if params == nil {
		return result, fmt.Errorf("params required: {\"nodeId\": \"...\"}")
	}
	var p struct {
		NodeID string `json:"nodeId"`
		Reload *bool  `json:"reload"`
	}
	if err := json.Unmarshal(*params, &p); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	if p.NodeID == "" {
		return result, fmt.Errorf("nodeId is required")
	}
	if p.Reload != nil {
		result.Reload = *p.Reload
	}
	result.NodeID = p.NodeID
	return result, nil
}

func DecodeProfilePatchParams(params *json.RawMessage) (ProfilePatchRequest, error) {
	result := ProfilePatchRequest{Reload: true}
	if params == nil {
		return result, fmt.Errorf("params required: profile patch object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(*params, &raw); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	if value, ok := raw["reload"]; ok {
		if err := json.Unmarshal(value, &result.Reload); err != nil {
			return result, fmt.Errorf("invalid reload: %w", err)
		}
	}
	if value, ok := raw["activeNodeId"]; ok {
		result.HasActiveNodeID = true
		if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if err := json.Unmarshal(value, &result.ActiveNodeID); err != nil {
				return result, fmt.Errorf("invalid activeNodeId: %w", err)
			}
		}
	}
	if value, ok := raw["routing"]; ok {
		var routing profiledoc.RoutingConfig
		if err := json.Unmarshal(value, &routing); err != nil {
			return result, fmt.Errorf("invalid routing: %w", err)
		}
		result.Routing = &routing
	}
	if value, ok := raw["dns"]; ok {
		var dns profiledoc.DNSConfig
		if err := json.Unmarshal(value, &dns); err != nil {
			return result, fmt.Errorf("invalid dns: %w", err)
		}
		result.DNS = &dns
	}
	if value, ok := raw["inbounds"]; ok {
		var inbounds profiledoc.InboundsConfig
		if err := json.Unmarshal(value, &inbounds); err != nil {
			return result, fmt.Errorf("invalid inbounds: %w", err)
		}
		result.Inbounds = &inbounds
	}
	if !result.HasActiveNodeID && result.Routing == nil && result.DNS == nil && result.Inbounds == nil {
		return result, fmt.Errorf("profile patch must include at least one supported field")
	}
	return result, nil
}

func DecodeNodeUpsertParams(params *json.RawMessage) (NodeUpsertRequest, error) {
	result := NodeUpsertRequest{Reload: true}
	if params == nil {
		return result, fmt.Errorf("params required: {\"node\": {...}}")
	}
	var p struct {
		Node   *profiledoc.Node `json:"node"`
		Reload *bool            `json:"reload"`
	}
	if err := json.Unmarshal(*params, &p); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	if p.Node == nil {
		return result, fmt.Errorf("node is required")
	}
	if p.Reload != nil {
		result.Reload = *p.Reload
	}
	result.Node = *p.Node
	return result, nil
}

func DecodeNodeRemoveParams(params *json.RawMessage) (NodeRemoveRequest, error) {
	result := NodeRemoveRequest{Reload: true}
	if params == nil {
		return result, fmt.Errorf("params required: {\"nodeId\": \"...\"}")
	}
	var p struct {
		NodeID string `json:"nodeId"`
		Reload *bool  `json:"reload"`
	}
	if err := json.Unmarshal(*params, &p); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	if p.NodeID == "" {
		return result, fmt.Errorf("nodeId is required")
	}
	if p.Reload != nil {
		result.Reload = *p.Reload
	}
	result.NodeID = p.NodeID
	return result, nil
}

func DecodeRoutingPatchParams(params *json.RawMessage) (RoutingPatchRequest, error) {
	result := RoutingPatchRequest{Reload: true}
	if params == nil {
		return result, fmt.Errorf("params required: {\"routing\": {...}}")
	}
	var p struct {
		Routing *profiledoc.RoutingConfig `json:"routing"`
		Reload  *bool                     `json:"reload"`
	}
	if err := json.Unmarshal(*params, &p); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	if p.Routing == nil {
		return result, fmt.Errorf("routing is required")
	}
	if p.Reload != nil {
		result.Reload = *p.Reload
	}
	result.Routing = *p.Routing
	return result, nil
}

func DecodeDNSPatchParams(params *json.RawMessage) (DNSPatchRequest, error) {
	result := DNSPatchRequest{Reload: true}
	if params == nil {
		return result, fmt.Errorf("params required: {\"dns\": {...}}")
	}
	var p struct {
		DNS    *profiledoc.DNSConfig `json:"dns"`
		Reload *bool                 `json:"reload"`
	}
	if err := json.Unmarshal(*params, &p); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	if p.DNS == nil {
		return result, fmt.Errorf("dns is required")
	}
	if p.Reload != nil {
		result.Reload = *p.Reload
	}
	result.DNS = *p.DNS
	return result, nil
}

func DecodeInboundPatchParams(params *json.RawMessage) (InboundPatchRequest, error) {
	result := InboundPatchRequest{Reload: true}
	if params == nil {
		return result, fmt.Errorf("params required: {\"inbounds\": {...}}")
	}
	var p struct {
		Inbounds *profiledoc.InboundsConfig `json:"inbounds"`
		Reload   *bool                      `json:"reload"`
	}
	if err := json.Unmarshal(*params, &p); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	if p.Inbounds == nil {
		return result, fmt.Errorf("inbounds is required")
	}
	if p.Reload != nil {
		result.Reload = *p.Reload
	}
	result.Inbounds = *p.Inbounds
	return result, nil
}

func DecodeSubscriptionURLParams(params *json.RawMessage) (SubscriptionURLRequest, error) {
	var result SubscriptionURLRequest
	if params == nil {
		return result, fmt.Errorf("params required: {\"url\": \"https://...\"}")
	}
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(*params, &p); err != nil {
		return result, fmt.Errorf("invalid params: %w", err)
	}
	result.URL = p.URL
	return result, nil
}

func hasJSONField(data []byte, field string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw[field]
	return ok
}
