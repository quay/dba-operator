package vergraph

import (
	dba "github.com/app-sre/dba-operator/api/v1alpha1"
)

type VersionNode struct {
	version *dba.DatabaseMigration
	prev    *VersionNode
}

var NullNode = VersionNode{}

type VersionGraph struct {
	nodes       []*VersionNode
	names       map[string]*VersionNode
	missingFrom map[string][]*VersionNode
}

func NewVersionGraph() *VersionGraph {
	return &VersionGraph{
		nodes:       make([]*VersionNode, 0),
		names:       make(map[string]*VersionNode, 0),
		missingFrom: make(map[string][]*VersionNode),
	}
}

func (vg *VersionGraph) Find(versionName string) *VersionNode {
	return vg.names[versionName]
}

func (vg *VersionGraph) Add(node *dba.DatabaseMigration) {
	previous, ok := vg.names[node.Spec.Previous]

	var wrapped = &VersionNode{version: node, prev: previous}
	if !ok {
		if len(node.Spec.Previous) > 0 {
			missingArray, ok := vg.missingFrom[node.Spec.Previous]
			if !ok {
				missingArray = make([]*VersionNode, 0, 1)
			}
			vg.missingFrom[node.Spec.Previous] = append(missingArray, wrapped)
		} else {
			wrapped.prev = &NullNode
		}
	}

	// Fix the graph
	foundMissing, ok := vg.missingFrom[node.Name]
	if ok {
		for _, oneMissing := range foundMissing {
			oneMissing.prev = wrapped
		}
		delete(vg.missingFrom, node.Name)
	}

	vg.nodes = append(vg.nodes, wrapped)
	vg.names[node.Name] = wrapped
}

func (vg *VersionGraph) Len() int {
	return len(vg.nodes)
}

func (vg *VersionGraph) Missing() int {
	var x = 0
	for _, missing := range vg.missingFrom {
		x += len(missing)
	}

	return x
}
