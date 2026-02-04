package dotnet

import (
	"context"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
)

// resolveTransitive fetches transitive dependencies for all direct dependencies.
// It merges the sub-graphs from each package into a single graph.
func resolveTransitive(ctx context.Context, resolver deps.Resolver, pkgs []string, opts deps.Options) (*dag.DAG, error) {
	merged := dag.New(nil)
	_ = merged.AddNode(dag.Node{ID: projectRoot, Meta: dag.Metadata{"virtual": true}})

	for _, pkg := range pkgs {
		g, err := resolver.Resolve(ctx, pkg, opts)
		if err != nil {
			opts.Logger("resolve failed: %s: %v", pkg, err)
			_ = merged.AddNode(dag.Node{ID: pkg})
			_ = merged.AddEdge(dag.Edge{From: projectRoot, To: pkg})
			continue
		}
		for _, n := range g.Nodes() {
			_ = merged.AddNode(dag.Node{ID: n.ID, Meta: n.Meta})
		}
		for _, e := range g.Edges() {
			_ = merged.AddEdge(dag.Edge{From: e.From, To: e.To})
		}
		_ = merged.AddEdge(dag.Edge{From: projectRoot, To: pkg})
	}

	return merged, nil
}
