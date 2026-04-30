package subscription

import (
	"strings"

	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
)

func MergeSubscriptionNodes(current profiledoc.Document, subscription profiledoc.Subscription, incoming []profiledoc.Node) (profiledoc.Document, map[string]int) {
	stats := map[string]int{"added": 0, "updated": 0, "unchanged": 0, "stale": 0}
	subscription.ProviderKey = strings.TrimSpace(subscription.ProviderKey)
	subscription.URL = strings.TrimSpace(subscription.URL)
	if subscription.ProviderKey == "" {
		return current, stats
	}
	for i := range incoming {
		incoming[i].Source.Type = "SUBSCRIPTION"
		incoming[i].Source.ProviderKey = subscription.ProviderKey
		incoming[i].Source.URL = subscription.URL
	}
	next, stats := profiledoc.MergeNodes(current, incoming)
	seenIncoming := map[string]bool{}
	for _, node := range incoming {
		if key := profiledoc.NodeMatchKey(node); key != "" {
			seenIncoming[key] = true
		}
	}
	for i, node := range next.Nodes {
		if !strings.EqualFold(node.Source.Type, "SUBSCRIPTION") || node.Source.ProviderKey != subscription.ProviderKey {
			continue
		}
		if seenIncoming[profiledoc.NodeMatchKey(node)] {
			continue
		}
		if !node.Stale {
			stats["stale"]++
		}
		next.Nodes[i].Stale = true
	}
	if activeNodeIsStaleOrMissing(next.Nodes, next.ActiveNodeID) {
		next.ActiveNodeID = firstLiveNodeID(next.Nodes)
	}
	return next, stats
}

func activeNodeIsStaleOrMissing(nodes []profiledoc.Node, activeNodeID string) bool {
	if activeNodeID == "" {
		return true
	}
	for _, node := range nodes {
		if node.ID == activeNodeID {
			return node.Stale
		}
	}
	return true
}

func firstLiveNodeID(nodes []profiledoc.Node) string {
	for _, node := range nodes {
		if !node.Stale {
			return node.ID
		}
	}
	return ""
}
