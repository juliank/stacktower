package dotnet

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/dag"
	"github.com/matzehuels/stacktower/pkg/deps"
)

func TestLanguage(t *testing.T) {
	if Language.Name != "dotnet" {
		t.Errorf("Language.Name = %q, want %q", Language.Name, "dotnet")
	}
	if Language.DefaultRegistry != "nuget" {
		t.Errorf("Language.DefaultRegistry = %q, want %q", Language.DefaultRegistry, "nuget")
	}
}

func TestNewResolver(t *testing.T) {
	resolver, err := Language.NewResolver(1 * time.Hour)
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	if resolver == nil {
		t.Fatal("NewResolver() returned nil")
	}
}

func TestPackagesConfig_Supports(t *testing.T) {
	parser := &PackagesConfig{}

	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"exact match", "packages.config", true},
		{"wrong name", "package.json", false},
		{"csproj", "MyApp.csproj", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.Supports(tt.filename)
			if got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestCsProj_Supports(t *testing.T) {
	parser := &CsProj{}

	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"csproj lowercase", "myapp.csproj", true},
		{"csproj uppercase", "MyApp.csproj", true},
		{"csproj mixed", "MyApp.CsProj", true},
		{"packages.config", "packages.config", false},
		{"random file", "readme.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.Supports(tt.filename)
			if got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestPackagesConfig_Parse(t *testing.T) {
	// Create a temporary packages.config file
	content := `<?xml version="1.0" encoding="utf-8"?>
<packages>
  <package id="Newtonsoft.Json" version="13.0.3" targetFramework="net472" />
  <package id="System.Memory" version="4.5.5" targetFramework="net472" />
</packages>`

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "packages.config")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	parser := &PackagesConfig{}
	result, err := parser.Parse(path, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Type != "packages" {
		t.Errorf("result.Type = %q, want %q", result.Type, "packages")
	}

	if result.IncludesTransitive {
		t.Error("result.IncludesTransitive = true, want false")
	}

	// Check that graph has nodes
	if result.Graph == nil {
		t.Fatal("result.Graph is nil")
	}

	g := result.Graph.(*dag.DAG)
	if g.NodeCount() != 3 { // root + 2 packages
		t.Errorf("Graph has %d nodes, want 3", g.NodeCount())
	}

	// Check edges exist - verify root has 2 children
	children := g.Children("__project__")
	if len(children) != 2 {
		t.Errorf("Root has %d children, want 2", len(children))
	}
}

func TestCsProj_Parse(t *testing.T) {
	// Create a temporary .csproj file
	content := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
    <PackageReference Include="Microsoft.Extensions.Logging" Version="8.0.0" />
  </ItemGroup>
</Project>`

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "MyApp.csproj")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	parser := &CsProj{}
	result, err := parser.Parse(path, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Type != "csproj" {
		t.Errorf("result.Type = %q, want %q", result.Type, "csproj")
	}

	if result.IncludesTransitive {
		t.Error("result.IncludesTransitive = true, want false")
	}

	if result.RootPackage != "MyApp" {
		t.Errorf("result.RootPackage = %q, want %q", result.RootPackage, "MyApp")
	}

	// Check that graph has nodes
	if result.Graph == nil {
		t.Fatal("result.Graph is nil")
	}

	g := result.Graph.(*dag.DAG)
	if g.NodeCount() != 3 { // root + 2 packages
		t.Errorf("Graph has %d nodes, want 3", g.NodeCount())
	}

	// Check edges exist - verify root has 2 children
	children := g.Children("__project__")
	if len(children) != 2 {
		t.Errorf("Root has %d children, want 2", len(children))
	}
}

func TestCsProj_Parse_CPM(t *testing.T) {
// Create a temporary directory structure with Directory.Packages.props and .csproj
tmpDir := t.TempDir()
projectDir := filepath.Join(tmpDir, "MyApp")
if err := os.Mkdir(projectDir, 0755); err != nil {
t.Fatalf("Failed to create project directory: %v", err)
}

// Create Directory.Packages.props in solution root
propsContent := `<Project>
  <PropertyGroup>
    <ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally>
  </PropertyGroup>
  <ItemGroup>
    <PackageVersion Include="Newtonsoft.Json" Version="13.0.3" />
    <PackageVersion Include="Microsoft.Extensions.Logging" Version="8.0.0" />
  </ItemGroup>
</Project>`

propsPath := filepath.Join(tmpDir, "Directory.Packages.props")
if err := os.WriteFile(propsPath, []byte(propsContent), 0644); err != nil {
t.Fatalf("Failed to write Directory.Packages.props: %v", err)
}

// Create .csproj without version attributes (CPM style)
csprojContent := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" />
    <PackageReference Include="Microsoft.Extensions.Logging" />
  </ItemGroup>
</Project>`

csprojPath := filepath.Join(projectDir, "MyApp.csproj")
if err := os.WriteFile(csprojPath, []byte(csprojContent), 0644); err != nil {
t.Fatalf("Failed to write .csproj: %v", err)
}

parser := &CsProj{}
result, err := parser.Parse(csprojPath, deps.Options{})
if err != nil {
t.Fatalf("Parse() error = %v", err)
}

if result.Type != "csproj" {
t.Errorf("result.Type = %q, want %q", result.Type, "csproj")
}

if result.RootPackage != "MyApp" {
t.Errorf("result.RootPackage = %q, want %q", result.RootPackage, "MyApp")
}

// Check that graph has nodes
if result.Graph == nil {
t.Fatal("result.Graph is nil")
}

g := result.Graph.(*dag.DAG)
if g.NodeCount() != 3 { // root + 2 packages
t.Errorf("Graph has %d nodes, want 3", g.NodeCount())
}

// Check edges exist - verify root has 2 children
children := g.Children("__project__")
if len(children) != 2 {
t.Errorf("Root has %d children, want 2", len(children))
}
}
