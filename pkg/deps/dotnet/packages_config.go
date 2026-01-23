package dotnet

import (
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

// IncludesTransitive returns false because packages.config only lists direct dependencies.
func (p *PackagesConfig) IncludesTransitive() bool {
	return false
}

// Supports checks if the filename matches this parser.
func (p *PackagesConfig) Supports(name string) bool {
	return name == "packages.config"
}

// Parse reads a packages.config file and builds a dependency graph.
// It only includes direct dependencies; transitive dependencies must be resolved via the registry.
func (p *PackagesConfig) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
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
	rootID := "__project__"
	g.AddNode(dag.Node{ID: rootID, Row: 0})

	// Add direct dependencies as edges
	for _, pkg := range pkgConfig.Packages {
		g.AddNode(dag.Node{ID: pkg.ID})
		g.AddEdge(dag.Edge{From: rootID, To: pkg.ID})
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
