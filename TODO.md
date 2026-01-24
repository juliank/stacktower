# TODO

This document tracks future improvements and enhancements for the stacktower project, with a focus on the .NET/NuGet integration.

## Ideas

New features and significant improvements.

### 1. Solution File (.sln) Support

For .NET applications, it's typical that an application consists of multiple projects, all referenced from a solution file (.sln). Currently, we support both legacy `packages.config` files and modern `.csproj` files with dependencies, but we require targeting individual project files.

**Proposal**: Add support for targeting `.sln` files as the application root. This would:
- Parse the solution file to discover all referenced projects
- Loop through all the referenced project files
- Build up the dependency graph for the whole solution
- Provide a complete view of all dependencies across all projects

**Implementation considerations**:
- Solution file format is text-based with project references
- Need to handle relative paths from solution to projects
- Should support both old-style and new-style .sln formats
- Consider how to merge graphs from multiple projects
- Determine root node representation (solution vs individual projects)

### 2. Support for Additional Manifest Formats

- **project.json**: Deprecated but might still exist in legacy codebases
- **nuget.config**: Support for custom feeds and package sources

### 3. Enhanced Metadata from NuGet.org

- Download counts for popularity metrics
- License validation and compatibility checking
- Security advisories integration
- Deprecation warnings

## Improvements and Fixes

Minor technical improvements and issue fixes.

### Framework Targeting Accuracy

- [ ] **Prefer newest framework version with minimal dependencies**
  
  **Issue**: The current three-stage dependency resolution (1. framework-agnostic, 2. modern .NET, 3. fallback) can select older framework targets that have more dependencies than newer versions support.
  
  **Example**: `Newtonsoft.Json` has different dependency sets per target framework:
  - `.NETFramework 2.0-4.5`: No dependencies
  - `.NETStandard 1.0`: 4 dependencies (Microsoft.CSharp, NETStandard.Library, etc.)
  - `.NETStandard 1.3`: 6 dependencies
  - `.NETStandard 2.0`: No dependencies
  - `net6.0`: No dependencies
  
  Current algorithm picks `.NETStandard 1.0` or `1.3` (stage 1: framework-agnostic) when it should prefer `.NETStandard 2.0` or `net6.0` for modern applications. This makes modern packages appear to have outdated dependencies they don't actually need.
  
  **Proposed solutions**:
  1. **Simple approach**: Check the newest supported .NET version first (e.g., `net6.0`). If it has no dependencies, use that regardless of older framework-agnostic targets.
  2. **Advanced approach**: Derive the target framework from the root application being analyzed. Match dependency framework versions to the application's framework (or closest/newest match). This would require detecting the application's target framework from the manifest file.
  
  **Impact**: More accurate dependency graphs for modern .NET applications, avoiding false positives for legacy dependencies.

### Pre-PR Testing

- [ ] **End-to-end testing with diverse real-world projects**
  - Test with various .NET project structures
  - Verify CPM scenarios with complex configurations
  - Test deep ProjectReference chains
  - Validate framework targeting logic with edge cases

- [ ] **Edge case validation**
  - Packages with no dependencies
  - Packages with only legacy .NET Framework targets (net47, net48, etc.)
  - Circular ProjectReference scenarios (if possible in .NET)
  - Invalid or malformed CPM files
  - Network failures and timeout scenarios
  - Very large projects with 100+ dependencies

- [ ] **Code review self-check**
  - Verify against CONTRIBUTING.md guidelines
  - Cross-reference with npm/pypi implementations for consistency
  - Search for any remaining TODO/FIXME comments in code
  - Check for proper error handling in all code paths

- [ ] **Documentation review**
  - Verify all public APIs have complete documentation
  - Check for typos and grammar in comments
  - Ensure code examples are accurate and runnable
  - Validate that doc.go examples match actual API

### Performance Optimizations

- [ ] **Parallel ProjectReference parsing**: Currently processes references sequentially; could parse multiple project files concurrently

- [ ] **Batch API requests**: Group multiple NuGet API calls where possible to reduce latency

- [ ] **Caching improvements**: Consider caching parsed .csproj files to avoid re-parsing in complex ProjectReference chains

### Code Quality

- [ ] **Add more test coverage for error paths**: Ensure all error returns are tested

- [ ] **Refactor extractDependencies**: Consider breaking down the three-stage logic into separate functions for better testability

- [ ] **Add benchmarks**: Performance benchmarks for parsing large .csproj files and complex dependency graphs

### Documentation

- [ ] **Add troubleshooting section**: Common issues and solutions (CPM not loading, framework targeting, etc.)

- [ ] **Add architecture diagram**: Visual representation of the three-endpoint NuGet API flow

- [ ] **Add examples directory**: Real-world example .csproj and packages.config files with expected outputs

### Minor Technical Debt

- [ ] **Consistent error wrapping**: Review all error returns for consistent use of fmt.Errorf with context

- [ ] **Logging consistency**: Ensure all significant operations have appropriate log levels

- [ ] **Configuration options**: Consider exposing more configuration options (API URLs, timeouts, retry behavior)
