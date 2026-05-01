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
	incomingOrder := orderedNodeMatchKeys(incoming)
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
	next.Nodes = reorderProviderNodes(next.Nodes, subscription.ProviderKey, incomingOrder)
	if activeNodeIsStaleOrMissing(next.Nodes, next.ActiveNodeID) {
		next.ActiveNodeID = firstLiveNodeID(next.Nodes)
	}
	return next, stats
}

func orderedNodeMatchKeys(nodes []profiledoc.Node) []string {
	keys := make([]string, 0, len(nodes))
	seen := map[string]bool{}
	for _, node := range nodes {
		key := profiledoc.NodeMatchKey(node)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

func reorderProviderNodes(nodes []profiledoc.Node, providerKey string, incomingOrder []string) []profiledoc.Node {
	if providerKey == "" {
		return nodes
	}
	providerByKey := map[string]profiledoc.Node{}
	hasProviderNodes := false
	for _, node := range nodes {
		if !isProviderNode(node, providerKey) {
			continue
		}
		hasProviderNodes = true
		if key := profiledoc.NodeMatchKey(node); key != "" {
			providerByKey[key] = node
		}
	}
	if !hasProviderNodes {
		return nodes
	}

	orderedProvider := make([]profiledoc.Node, 0, len(providerByKey))
	used := map[string]bool{}
	for _, key := range incomingOrder {
		node, ok := providerByKey[key]
		if !ok {
			continue
		}
		orderedProvider = append(orderedProvider, node)
		used[key] = true
	}
	for _, node := range nodes {
		if !isProviderNode(node, providerKey) {
			continue
		}
		key := profiledoc.NodeMatchKey(node)
		if key != "" && used[key] {
			continue
		}
		orderedProvider = append(orderedProvider, node)
	}

	return replaceProviderNodes(nodes, providerKey, orderedProvider)
}

func replaceProviderNodes(nodes []profiledoc.Node, providerKey string, orderedProvider []profiledoc.Node) []profiledoc.Node {
	result := make([]profiledoc.Node, 0, len(nodes))
	inserted := false
	for _, node := range nodes {
		if isProviderNode(node, providerKey) {
			if !inserted {
				result = append(result, orderedProvider...)
				inserted = true
			}
			continue
		}
		result = append(result, node)
	}
	return result
}

func isProviderNode(node profiledoc.Node, providerKey string) bool {
	return strings.EqualFold(node.Source.Type, "SUBSCRIPTION") && node.Source.ProviderKey == providerKey
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
