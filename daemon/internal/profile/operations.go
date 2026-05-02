package profile

import "fmt"

type RuntimeIntent struct {
	BackendKind     string
	FallbackPolicy  string
	ActiveProfileID string
}

func SetActiveNode(current Document, nodeID string) (Document, error) {
	found := false
	for _, node := range current.Nodes {
		if node.ID == nodeID && !node.Stale {
			found = true
			break
		}
	}
	if !found {
		return Document{}, fmt.Errorf("active node is missing or stale")
	}
	current.ActiveNodeID = nodeID
	return current, nil
}

func ClearActiveNode(current Document) Document {
	current.ActiveNodeID = ""
	return current
}

func UpsertNode(current Document, node Node) (Document, error) {
	if node.ID == "" {
		return Document{}, fmt.Errorf("node id is required")
	}
	replaced := false
	for i := range current.Nodes {
		if current.Nodes[i].ID == node.ID {
			current.Nodes[i] = node
			replaced = true
			break
		}
	}
	if !replaced {
		current.Nodes = append(current.Nodes, node)
	}
	if current.ActiveNodeID == "" && !node.Stale {
		current.ActiveNodeID = node.ID
	}
	return current, nil
}

func RemoveNode(current Document, nodeID string) (Document, error) {
	if nodeID == "" {
		return Document{}, fmt.Errorf("node id is required")
	}
	next := current
	next.Nodes = make([]Node, 0, len(current.Nodes))
	removed := false
	for _, node := range current.Nodes {
		if node.ID == nodeID {
			removed = true
			continue
		}
		next.Nodes = append(next.Nodes, node)
	}
	if !removed {
		return Document{}, fmt.Errorf("node %q not found", nodeID)
	}
	if next.ActiveNodeID == nodeID {
		next.ActiveNodeID = ""
		for _, node := range next.Nodes {
			if !node.Stale {
				next.ActiveNodeID = node.ID
				break
			}
		}
	}
	return next, nil
}

func ApplyRuntimeIntent(current Document, intent RuntimeIntent) Document {
	if intent.BackendKind != "" {
		current.Runtime.BackendKind = intent.BackendKind
	}
	if intent.FallbackPolicy != "" {
		current.Runtime.FallbackPolicy = intent.FallbackPolicy
	}
	if intent.ActiveProfileID != "" {
		current.ActiveNodeID = intent.ActiveProfileID
	}
	return current
}
