package dotnet

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"

	"github.com/matzehuels/stacktower/pkg/dag"
	"github.com/matzehuels/stacktower/pkg/deps"
)

// PackagesConfig parses packages.config files (legacy .NET Framework format).
// Format: XML file listing NuGet package dependencies with versions.
//
// Example packages.config:
//
//	<?xml version="1.0" encoding="utf-8"?>
//	<packages>
//	  <package id="Newtonsoft.Json" version="13.0.3" targetFramework="net472" />
//	  <package id="System.Memory" version="4.5.5" targetFramework="net472" />
//	</packages>
type PackagesConfig struct {
	resolver deps.Resolver
}

// Type returns the manifest type identifier.
func (p *PackagesConfig) Type() string {
	return "packages"
}

// IncludesTransitive returns true if a resolver is available to fetch transitive dependencies.
func (p *PackagesConfig) IncludesTransitive() bool {
	return p.resolver != nil
}

// Supports checks if the filename matches this parser.
func (p *PackagesConfig) Supports(name string) bool {
	return name == "packages.config"
}

// Parse reads a packages.config file and builds a dependency graph.
// It only includes direct dependencies; transitive dependencies must be resolved via the registry.
func (p *PackagesConfig) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	// Apply defaults to options (including no-op logger if nil)
	opts = opts.WithDefaults()

	// Read the XML file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read packages.config: %w", err)
	}

	// Parse XML structure
	var pkgConfig packagesConfigXML
	if err := xml.Unmarshal(data, &pkgConfig); err != nil {
		return nil, fmt.Errorf("failed to parse packages.config XML: %w", err)
	}

	// Create graph
	g := dag.New(nil)

	// Add root node (project) - we use a placeholder since packages.config doesn't contain project name
	rootID := projectRoot
	g.AddNode(dag.Node{ID: rootID, Row: 0})

	// Collect direct dependencies
	var directDeps []string
	for _, pkg := range pkgConfig.Packages {
		directDeps = append(directDeps, pkg.ID)
	}

	// If resolver is available, fetch transitive dependencies
	if p.resolver != nil {
		var err error
		g, err = p.resolve(context.Background(), directDeps, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve dependencies: %w", err)
		}
	} else {
		// Without resolver, just add direct dependencies
		for _, pkg := range pkgConfig.Packages {
			// Create node with version metadata
			meta := dag.Metadata{"version": pkg.Version}
			g.AddNode(dag.Node{ID: pkg.ID, Meta: meta})
			g.AddEdge(dag.Edge{From: rootID, To: pkg.ID})
		}
	}

	return &deps.ManifestResult{
		Graph:              g,
		Type:               p.Type(),
		IncludesTransitive: p.IncludesTransitive(),
		RootPackage:        "", // packages.config doesn't include project name
	}, nil
}

// XML structure for packages.config
type packagesConfigXML struct {
	XMLName  xml.Name          `xml:"packages"`
	Packages []packagesPackage `xml:"package"`
}

type packagesPackage struct {
	ID              string `xml:"id,attr"`
	Version         string `xml:"version,attr"`
	TargetFramework string `xml:"targetFramework,attr"`
}

// resolve fetches transitive dependencies for all direct dependencies.
// It merges the sub-graphs from each package into a single graph.
func (p *PackagesConfig) resolve(ctx context.Context, pkgs []string, opts deps.Options) (*dag.DAG, error) {
	merged := dag.New(nil)
	_ = merged.AddNode(dag.Node{ID: projectRoot, Meta: dag.Metadata{"virtual": true}})

	for _, pkg := range pkgs {
		g, err := p.resolver.Resolve(ctx, pkg, opts)
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
