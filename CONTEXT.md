# AI Context: NuGet/.NET Integration for Stacktower

## Project Overview

Stacktower is a Go-based CLI tool that visualizes package dependency graphs as "physical towers" where packages rest on their dependencies. Inspired by XKCD #2347. Primary workflow: parse dependencies from registries/manifests → render as SVG/PDF/PNG visualizations.

Repository: matzehuels/stacktower (forked to stacktower_juliank for development)
Current branch: `feature/nuget` (ready for PR to main)
Language: Go 1.x with standard library + minimal external dependencies

## Architecture Pattern

Stacktower uses a plugin-style architecture where each language ecosystem (Python, Rust, JavaScript, Ruby, PHP, Java, Go, **DotNet**) implements:

1. **Language Definition** (`pkg/deps/<language>/<language>.go`)
   - Implements `deps.Language` interface
   - Registers resolver and manifest parsers
   - Example: `pkg/deps/dotnet/dotnet.go`

2. **Registry Resolver** (`pkg/integrations/<registry>/client.go`)
   - Implements `deps.Resolver` interface
   - Fetches package metadata from registry API
   - Returns `deps.PackageInfo` with dependencies
   - Uses `integrations.Client` base for HTTP caching/retries
   - Example: `pkg/integrations/nuget/client.go`

3. **Manifest Parsers** (`pkg/deps/<language>/<parser>.go`)
   - Implements `deps.ManifestParser` interface
   - Methods: `Supports(filename)`, `Parse(ctx, path, opts)`
   - Returns `dag.Graph` with direct + transitive dependencies
   - Calls resolver for transitive dependencies
   - Example: `pkg/deps/dotnet/csproj.go`, `pkg/deps/dotnet/packages_config.go`

## NuGet/.NET Implementation Status

### Completed (27 commits, 2200+ lines, all tests passing)

#### Files Created:
- `pkg/integrations/nuget/client.go` (450+ lines) - NuGet API client
- `pkg/integrations/nuget/client_test.go` (420+ lines) - Unit tests with HTTP mocking
- `pkg/integrations/nuget/client_integration_test.go` (36 lines) - Real API tests
- `pkg/integrations/nuget/doc.go` (59 lines) - Package documentation
- `pkg/deps/dotnet/dotnet.go` (73 lines) - Language registration
- `pkg/deps/dotnet/packages_config.go` (122 lines) - Legacy XML parser
- `pkg/deps/dotnet/csproj.go` (284 lines) - Modern SDK-style parser
- `pkg/deps/dotnet/dotnet_test.go` (487 lines) - Parser unit tests
- `pkg/deps/dotnet/doc.go` (52 lines) - Package documentation
- `pkg/deps/dotnet/cpm.go` (62 lines) - Shared CPM parsing helpers
- `pkg/deps/dotnet/directory_packages_props.go` (89 lines) - CPM manifest parser
- `pkg/deps/dotnet/constants.go` (3 lines) - Shared root constant
- `pkg/deps/dotnet/resolve.go` (34 lines) - Shared transitive resolver
- `pkg/deps/dotnet/root_package.go` (8 lines) - Root package helper
- `internal/cli/parse.go` (2 lines modified) - CLI registration

#### Key Features:
1. **NuGet v3 API Integration**: Four-stage flow with .nuspec fallback
2. **Stable Version Preference**: Prefers stable releases over pre-releases
3. **Pre-release Package Support**: Full .nuspec parsing as fallback when catalog unavailable
4. **Framework Targeting**: Three-stage dependency resolution (framework-agnostic → modern .NET → fallback)
5. **packages.config Support**: Legacy XML format with transitive resolution
6. **SDK-style .csproj Support**: Modern format with PackageReference
7. **Central Package Management (CPM)**: Searches parent dirs for Directory.Packages.props
8. **ProjectReference Support**: Recursive parsing of referenced .csproj files with graph merging
9. **Input Validation**: Empty package name checks
10. **Future-proof Versioning**: Regex `(?i)net(standard|coreapp|\d+\.)` matches net5.0-net100+, excludes net47/net481
11. **Directory.Packages.props as Manifest**: Parse CPM file directly as an entry point
12. **Root Package Naming**: packages.config and Directory.Packages.props use containing folder name

## Critical Implementation Details

### NuGet API Client (`pkg/integrations/nuget/client.go`)

**Base URL**: `https://api.nuget.org/v3-flatcontainer/`

**Four-stage fetch pattern**:
```go
// 1. Version index: /{package_lower}/index.json → latest STABLE version (prefers stable)
// 2. Registration: /registration5-semver1/{package_lower}/{version}.json → catalog entry URL
// 3. Catalog entry: /catalog0/data/.../package.json → package metadata (if available)
// 4. .nuspec XML fallback: /{package_lower}/{version}/{package_lower}.nuspec → full metadata
//    (Step 4 used when catalog unavailable, e.g., for pre-release versions)
```

**Version Selection** (`findLatestStableVersion`):
- Searches backwards through version list for first stable version (no hyphen)
- Stable versions preferred: `10.0.3` over `11.0.0-preview.1`
- Falls back to latest pre-release if no stable versions exist
- Rationale: Pre-release versions have catalog API limitations (404 on registration endpoint)

**Package name normalization**: Always lowercase for API calls (`strings.ToLower`)

**Framework targeting algorithm** (`extractDependencies`):
```
Stage 1: Framework-agnostic (empty or "any" targetFramework) - HIGHEST PRIORITY
Stage 2: Newest available framework - Sophisticated comparison logic
Stage 3: First available group - FALLBACK

Framework Comparison Logic:
1. Parse framework string into family + version (parseTargetFramework)
2. Assign priority scores to framework families:
   - Modern .NET (net6.0+): 300
   - .NET Core (netcoreapp3.1): 200
   - .NET Standard (netstandard2.1): 100
   - Legacy .NET Framework (net481): 10
3. Compare families by priority; if same family, compare versions numerically
4. Select dependencies from newest framework group

Framework Detection:
- Uses regex: frameworkRegex = regexp.MustCompile(`(?i)^(net)(standard|coreapp)?(\d+(?:\.\d+)*)`)
- Distinguishes modern .NET (net6.0) from legacy Framework (net481) by period presence
- Supports future versions automatically (net9.0, net100.0, etc.)
```

**Result**: Accurate dependency resolution for modern .NET applications. Example: Newtonsoft.Json correctly shows 0 dependencies for net6.0 instead of showing legacy dependencies from netstandard1.3.

**Repository URL extraction** (`fetchRepositoryURL`):
- Fetches .nuspec XML from flatcontainer API
- Parses `<repository url="..." type="git">` element
- Returns empty string if .nuspec unavailable or no repository element
- Used by GitHub enrichment to associate packages with correct source repos
- Example: EPPlus correctly maps to github.com/EPPlusSoftware/EPPlus

**.nuspec Fallback for Pre-releases** (`fetchNuspecMetadata`):
- Pre-release versions return 404 from NuGet's registration/catalog APIs
- Fetches complete .nuspec XML and parses all metadata fields:
  - `<description>`, `<authors>`, `<projectUrl>`, `<licenseUrl>`
  - `<repository url="...">` for GitHub URL
  - `<dependencies><group>` for framework-specific dependencies
- Converts .nuspec dependencies to catalog-style dependency groups
- Maintains full enrichment capability for pre-release packages
- Example: Microsoft.Extensions.Options pre-release versions get full metadata

**Error handling**:
- Returns `integrations.ErrNotFound` for 404
- Returns `integrations.ErrNetwork` for network failures
- Uses `fmt.Errorf` with context for other errors

**Testing**: HTTP mocking via `httptest.NewServer` in `client_test.go`

### .csproj Parser (`pkg/deps/dotnet/csproj.go`)

**XML structure**:
```xml
<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
    <ProjectReference Include="..\OtherProject\OtherProject.csproj" />
  </ItemGroup>
</Project>
```

**CPM (Central Package Management)**:
- Searches for `Directory.Packages.props` in current dir → parent dirs → repository root
- Format: `<PackageVersion Include="PackageName" Version="1.2.3" />`
- When found: Versions from PackageReference override CPM versions (direct wins)
- When not found: Logs error via `opts.Logger()` but continues parsing
- Implementation: `loadCPMVersions()` walks filesystem with `filepath.Dir()` loop

**Directory.Packages.props Manifest**:
- Can be parsed directly as a manifest (type `cpm`)
- Treats PackageVersion entries as direct dependencies
- Root package defaults to containing folder name

**ProjectReference handling**:
- Extracts relative paths from `Include` attribute
- Normalizes Windows backslashes: `strings.ReplaceAll(path, "\\", "/")`
- Uses `filepath.Join()` with current .csproj directory for absolute paths
- Recursively calls `Parse()` on referenced .csproj files
- Merges graphs: `currentGraph.Merge(referencedGraph)`
- Graph merging naturally handles duplicates (no manual dedup needed)

**opts.WithDefaults() pattern**:
- MUST call `opts = opts.WithDefaults()` at start of `Parse()` function
- Ensures `opts.Logger()` is never nil (prevents panics in tests)
- Used in both `csproj.go` and `packages_config.go`

**Transitive dependencies**:
- After parsing direct PackageReference nodes, calls resolver for each package
- Adds transitive deps to graph: `g.AddNode()` and `g.AddEdge()`

### packages.config Parser (`pkg/deps/dotnet/packages_config.go`)

**XML structure**:
```xml
<?xml version="1.0" encoding="utf-8"?>
<packages>
  <package id="Newtonsoft.Json" version="13.0.3" targetFramework="net472" />
</packages>
```

**Simpler than .csproj**: No CPM, no ProjectReference support
**Transitive resolution**: Same pattern as .csproj - calls resolver for each package
**Root package**: Defaults to containing folder name (since file has no project name)

### Language Registration (`pkg/deps/dotnet/dotnet.go`)

Registers language in CLI via `internal/cli/parse.go`:
```go
var languages = []deps.Language{
    // ... other languages
    dotnet.Language,
}
```

**Manifest filename matching** (`newManifest`):
- Case-insensitive matching for Windows compatibility
- Handles variable .csproj names (MyProject.csproj, App.csproj, etc.)
- Pattern matching:
  - `case "csproj"` → canonical name from ManifestAliases
  - `strings.HasSuffix(nameLower, ".csproj")` → any .csproj file
  - `nameLower == "packages.config"` → case-insensitive
  - `nameLower == "directory.packages.props"` → case-insensitive

CLI command: `stacktower parse dotnet <package-or-file> [flags]`

### Testing Patterns

**Unit tests with HTTP mocking**:
```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Mock responses based on r.URL.Path
}))
defer server.Close()

client := testClient(t, server.URL, server.URL, server.URL) // configurable URLs for testing
// Production: client := nuget.NewClient(cache.NewNullCache(), 24*time.Hour)
```

**Test naming**: `Test<Type>_<Method>[_<Scenario>]`
- Example: `TestClient_FetchPackage`, `TestClient_FetchPackage_NotFound`

**Table-driven tests**: Used in `dotnet_test.go` for `Supports()` method testing

**Integration tests**: Suffix `_integration_test.go`, requires build tag (not run by default)

## Code Patterns and Conventions

### Error Handling
- Use `integrations.ErrNotFound` for missing packages (404)
- Use `integrations.ErrNetwork` for network issues
- Wrap errors with context: `fmt.Errorf("failed to parse version index: %w", err)`

### Logging
- Access via `opts.Logger()` (after `opts.WithDefaults()`)
- Error-level for failures that don't break parsing: `opts.Logger().Error("failed to load CPM", "err", err)`

### Documentation
- Every exported function/type needs doc comment
- Package-level `doc.go` with overview, usage example, feature list
- Inline comments for complex logic (e.g., 3-stage framework targeting)

### Imports
- Standard library first, blank line, external packages, blank line, internal packages
- Group related stdlib imports (net/*, encoding/*)

### Naming
- Package names: lowercase, no underscores (nuget, dotnet, not nuget_api)
- Exported types: PascalCase (Client, PackageInfo)
- Unexported: camelCase (extractDependencies, modernNetRegex)

## Dependencies

**External**:
- None for core nuget/dotnet packages (only stdlib)

**Internal**:
- `pkg/integrations`: Base Client interface, HTTP caching, retry logic, error types
- `pkg/deps`: Resolver, ManifestParser, Language interfaces, PackageInfo struct
- `pkg/dag`: Graph data structure (Node, Edge, AddNode, AddEdge, Merge methods)
- `pkg/httputil`: HTTP caching implementation (used by integrations.Client)

**HTTP Client**:
- Uses `cache.Cache` backend passed to `NewClient(backend, cacheTTL)`
- Wrapped with `integrations.Client` for caching/retries
- Cache backend typically provided by language infrastructure

## Environment Variables

- `GITHUB_TOKEN`: Required for `--enrich` flag (metadata enrichment)
- `GITLAB_TOKEN`: GitLab equivalent
- No special env vars for NuGet (API is public, no auth)

## Build and Test Commands

```bash
go build -o bin/stacktower .                     # Build binary
go test ./pkg/integrations/nuget                 # Test NuGet client
go test ./pkg/deps/dotnet                        # Test .NET parsers
go test ./...                                    # All tests
make check                                       # CI checks (fmt, lint, test, vuln)
```

## Known Issues and Limitations

1. **No .sln Support Yet** (idea in TODO.md):
   - Must target individual .csproj files, not solution files
   - Multi-project solutions require running parse on each project separately

2. **Framework Targeting** (completed, see TODO.md):
   - ✅ Sophisticated framework comparison implemented (January 25, 2026)
   - Prioritizes modern .NET frameworks over legacy ones
   - Future consideration: derive target framework from root app's .csproj

3. **No project.json or nuget.config Support**:
   - Only handles packages.config and SDK-style .csproj

4. **Circular ProjectReference**:
   - Not tested (unclear if .NET allows circular project references)

5. **Performance**:
   - ProjectReference parsing is sequential (not parallel)
   - No caching of parsed .csproj files (re-parses if referenced multiple times)

## File Locations Map

```
pkg/
├── integrations/
│   └── nuget/
│       ├── client.go                    # API client, FetchPackage, extractDependencies
│       ├── client_test.go               # HTTP mocking tests
│       ├── client_integration_test.go   # Real API tests
│       └── doc.go                       # Package documentation
├── deps/
│   └── dotnet/
│       ├── dotnet.go                    # Language definition, registration
│       ├── packages_config.go           # Legacy parser
│       ├── csproj.go                    # Modern parser, CPM, ProjectReference
│       ├── directory_packages_props.go  # CPM manifest parser
│       ├── cpm.go                        # CPM XML helpers
│       ├── constants.go                  # Shared root constant
│       ├── resolve.go                    # Shared transitive resolver
│       ├── root_package.go               # Root package helper
│       ├── dotnet_test.go               # Parser tests
│       └── doc.go                       # Package documentation
├── dag/
│   └── dag.go                           # Graph data structure
├── httputil/
│   └── cache.go                         # HTTP caching
└── io/
    └── ...                              # File system utilities

internal/cli/
└── parse.go                             # CLI command registration (line ~50: languages array)

examples/
├── manifest/
│   ├── example-app.csproj               # Example .csproj (not real project)
│   └── ...
└── real/
    └── ...                              # Pre-parsed JSON graphs

SUMMARY.md                               # Human-readable implementation summary
TODO.md                                  # Future work, ideas, improvements
CONTEXT.md                               # This file (AI context)
```

## Regex Reference

**modernNetRegex**: `(?i)net(standard|coreapp|\d+\.)`
- `(?i)`: Case-insensitive
- `net`: Literal prefix
- `(standard|coreapp|\d+\.)`: Alternation group
  - `standard`: Matches "netstandard", "netstandard1.3", etc.
  - `coreapp`: Matches "netcoreapp3.1", etc.
  - `\d+\.`: One or more digits followed by period
    - `\d+`: Matches 5, 6, 10, 100 (any length)
    - `\.`: Literal period (KEY: distinguishes modern from legacy)

**Why period is critical**:
- Modern .NET: `net6.0`, `net7.0`, `net8.0` (has period)
- Legacy .NET Framework: `net47`, `net48`, `net481` (no period)

## Testing Checklist Before PR

- [ ] All unit tests pass: `go test ./...`
- [ ] Build succeeds: `go build -o bin/stacktower .`
- [ ] CI checks pass locally: `make check`
- [ ] Test with real .NET projects (various structures)
- [ ] Test CPM scenarios
- [ ] Test ProjectReference chains
- [ ] Validate framework targeting with packages that have multiple targets
- [ ] Test with `--enrich` flag (requires GITHUB_TOKEN)
- [ ] Verify no TODO/FIXME comments remain in code
- [ ] Run `make fmt` before commit

## Commit Message Format

Follows Conventional Commits:
```
<type>(<scope>): <description>

[optional body]
```

Types: feat, fix, docs, test, refactor, perf, chore
Scopes: nuget, dotnet, deps, integrations
Examples:
- `feat(nuget): add input validation for empty package names`
- `test(dotnet): add unit tests for CPM loading`
- `refactor(nuget): use regex for future-proof .NET version matching`

## Reference Implementations

When unsure about patterns:
- **npm integration**: `pkg/integrations/npm/` and `pkg/deps/javascript/`
- **pypi integration**: `pkg/integrations/pypi/` and `pkg/deps/python/`

These follow the same architecture and testing patterns.

## Next Steps (from TODO.md)

**Highest priority** (before PR):
1. Fix framework targeting accuracy issue (prefer net6.0 over netstandard1.0 when appropriate)
2. End-to-end testing with real projects
3. Self-review against CONTRIBUTING.md

**Future ideas**:
1. Solution file (.sln) support
2. Parallel ProjectReference parsing
3. Additional manifest formats (project.json, nuget.config)
4. Enhanced metadata (download counts, security advisories)

## Gotchas and Quirks

1. **Always lowercase package names for NuGet API**: `pkgLower := strings.ToLower(pkg)`
2. **opts.WithDefaults() is mandatory**: Call at start of Parse() or Logger() will panic
3. **Windows paths in .csproj**: Must replace backslashes with forward slashes
4. **Graph.Merge() handles duplicates**: No manual deduplication needed for ProjectReference
5. **CPM is optional**: Don't fail parsing if Directory.Packages.props not found, just log
6. **Transitive deps are resolver's job**: Manifest parsers call resolver for transitive deps, don't traverse themselves
7. **HTTP mocking requires configurable URL**: Pass server.URL to NewClient() in tests
8. **Integration tests are separate**: Build tag prevents running without network access

## API Endpoints Reference

**NuGet v3 API**:
- Base: `https://api.nuget.org/v3-flatcontainer/`
- Version index: `GET /{id_lower}/index.json`
  - Returns: `{"versions": ["1.0.0", "2.0.0", ...]}`
- Registration: `GET /v3/registration5-gz-semver2/{id_lower}/{version_lower}.json`
  - Returns: JSON with `catalogEntry` URL
- Catalog entry: `GET {catalogEntry_url}`
  - Returns: Full metadata with `dependencyGroups` array

**Catalog Entry Structure**:
```json
{
  "catalogEntry": "url",
  "listed": true,
  "packageContent": "url",
  "dependencyGroups": [
    {
      "targetFramework": "net6.0",
      "dependencies": [
        {
          "id": "PackageName",
          "range": "[1.0.0, )"
        }
      ]
    }
  ]
}
```

## Quick Start for New AI Agent

1. Read this file completely
2. Review SUMMARY.md for human-readable overview
3. Check TODO.md for next tasks
4. Run `go test ./pkg/integrations/nuget ./pkg/deps/dotnet` to verify tests pass
5. Review `pkg/integrations/nuget/client.go` for core logic
6. Review `pkg/deps/dotnet/csproj.go` for parser logic
7. Check `internal/cli/parse.go` for CLI integration
8. Compare with `pkg/integrations/npm/` or `pkg/integrations/pypi/` for reference patterns

## Current Branch Status

- Branch: `feature/nuget`
- Base: `main`
- Status: All features implemented, all tests passing, pre-release support complete, ready for PR
- Commits: 27 (all follow conventional commits format)
- Files changed: 15 files (14 new, 1 modified)
- Lines added: 2,200+
- Last commits: 
  - "fix(nuget): use .nuspec as fallback for pre-release package metadata"
  - "feat(nuget): prefer stable versions over pre-releases"

---

*Updated: February 15, 2026*
*Purpose: AI agent context for continuing NuGet/.NET integration work*
*Not intended for human consumption - see SUMMARY.md and TODO.md instead*
