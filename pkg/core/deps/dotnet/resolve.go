package dotnet

import (
	"context"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
)

// resolveTransitive fetches transitive dependencies for all direct dependencies.
// It merges the sub-graphs from each package into a single graph.
// Pinned version or constraint on each dependency is forwarded to the resolver
// so that PubGrub resolves the exact declared version rather than the latest.
func resolveTransitive(ctx context.Context, resolver deps.Resolver, pkgs []deps.Dependency, opts deps.Options) (*dag.DAG, error) {
	merged := dag.New(nil)
	_ = merged.AddNode(dag.Node{ID: projectRoot, Meta: dag.Metadata{"virtual": true}})

	for _, pkg := range pkgs {
		pkgOpts := opts
		if pkg.Pinned != "" {
			pkgOpts.Version = pkg.Pinned
			pkgOpts.Constraint = ""
		} else if pkg.Constraint != "" {
			pkgOpts.Constraint = pkg.Constraint
			pkgOpts.Version = ""
		}

		g, err := resolver.Resolve(ctx, pkg.Name, pkgOpts)
		if err != nil {
			opts.Logger("resolve failed: %s: %v", pkg.Name, err)
			_ = merged.AddNode(dag.Node{ID: pkg.Name})
			_ = merged.AddEdge(dag.Edge{From: projectRoot, To: pkg.Name})
			continue
		}
		for _, n := range g.Nodes() {
			_ = merged.AddNode(dag.Node{ID: n.ID, Meta: n.Meta})
		}
		for _, e := range g.Edges() {
			_ = merged.AddEdge(dag.Edge{From: e.From, To: e.To})
		}
		_ = merged.AddEdge(dag.Edge{From: projectRoot, To: pkg.Name})
	}

	return merged, nil
}
