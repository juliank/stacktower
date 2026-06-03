package nuget

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/integrations"
)

// TestClient_FetchPackage tests fetching a well-known NuGet package.
func TestClient_FetchPackage(t *testing.T) {
	tests := []struct {
		name        string
		pkg         string
		wantName    string
		wantVersion string
		wantDepsLen int
		wantErr     bool
		setupMock   func(*http.ServeMux, string)
	}{
		{
			name:        "valid package with dependencies",
			pkg:         "Newtonsoft.Json",
			wantName:    "Newtonsoft.Json",
			wantVersion: "13.0.3",
			wantDepsLen: 0, // Newtonsoft.Json has no dependencies
			wantErr:     false,
			setupMock: func(mux *http.ServeMux, serverURL string) {
				// Mock version index
				mux.HandleFunc("/newtonsoft.json/index.json", func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(versionIndexResponse{
						Versions: []string{"12.0.0", "13.0.1", "13.0.3"},
					})
				})
				// Mock registration - returns catalog entry URL
				mux.HandleFunc("/newtonsoft.json/13.0.3.json", func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(registrationResponse{
						CatalogEntry: serverURL + "/catalog/newtonsoft.json",
					})
				})
				// Mock catalog entry
				mux.HandleFunc("/catalog/newtonsoft.json", func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(catalogEntry{
						ID:               "Newtonsoft.Json",
						Version:          "13.0.3",
						Description:      "Json.NET is a popular high-performance JSON framework for .NET",
						ProjectURL:       "https://www.newtonsoft.com/json",
						LicenseURL:       "https://licenses.nuget.org/MIT",
						Authors:          "James Newton-King",
						DependencyGroups: []dependencyGroup{},
					})
				})
			},
		},
		{
			name:        "package with dependencies",
			pkg:         "Microsoft.Extensions.Logging",
			wantName:    "Microsoft.Extensions.Logging",
			wantVersion: "8.0.0",
			wantDepsLen: 2,
			wantErr:     false,
			setupMock: func(mux *http.ServeMux, serverURL string) {
				mux.HandleFunc("/microsoft.extensions.logging/index.json", func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(versionIndexResponse{
						Versions: []string{"7.0.0", "8.0.0"},
					})
				})
				mux.HandleFunc("/microsoft.extensions.logging/8.0.0.json", func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(registrationResponse{
						CatalogEntry: serverURL + "/catalog/logging.json",
					})
				})
				mux.HandleFunc("/catalog/logging.json", func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(catalogEntry{
						ID:      "Microsoft.Extensions.Logging",
						Version: "8.0.0",
						DependencyGroups: []dependencyGroup{
							{
								TargetFramework: ".NETStandard2.0",
								Dependencies: []dependency{
									{ID: "Microsoft.Extensions.DependencyInjection.Abstractions", Range: "[8.0.0, )"},
									{ID: "Microsoft.Extensions.Logging.Abstractions", Range: "[8.0.0, )"},
								},
							},
						},
					})
				})
			},
		},
		{
			name:    "package not found",
			pkg:     "NonExistent.Package",
			wantErr: true,
			setupMock: func(mux *http.ServeMux, serverURL string) {
				mux.HandleFunc("/nonexistent.package/index.json", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				})
			},
		},
		{
			name:    "empty package name",
			pkg:     "",
			wantErr: true,
			setupMock: func(mux *http.ServeMux, serverURL string) {
				// No mock needed - should fail before HTTP call
			},
		},
		{
			name:    "whitespace-only package name",
			pkg:     "   ",
			wantErr: true,
			setupMock: func(mux *http.ServeMux, serverURL string) {
				// No mock needed - should fail before HTTP call
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			defer server.Close()

			// Set up mocks with access to server URL
			tt.setupMock(mux, server.URL)

			client := testClient(t, server.URL, server.URL, server.URL)

			info, err := client.FetchPackage(context.Background(), tt.pkg, true)

			if tt.wantErr {
				if err == nil {
					t.Errorf("FetchPackage() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("FetchPackage() unexpected error: %v", err)
			}

			if info.Name != tt.wantName {
				t.Errorf("FetchPackage() Name = %v, want %v", info.Name, tt.wantName)
			}
			if info.Version != tt.wantVersion {
				t.Errorf("FetchPackage() Version = %v, want %v", info.Version, tt.wantVersion)
			}
			if len(info.Dependencies) != tt.wantDepsLen {
				t.Errorf("FetchPackage() Dependencies length = %v, want %v", len(info.Dependencies), tt.wantDepsLen)
			}
		})
	}
}

// TestClient_FetchPackage_EmptyInput tests that empty package names are properly rejected.
func TestClient_FetchPackage_EmptyInput(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	tests := []struct {
		name string
		pkg  string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"tabs and spaces", "\t  \n  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.FetchPackage(context.Background(), tt.pkg, true)
			if err == nil {
				t.Errorf("FetchPackage(%q) expected error for empty package name, got nil", tt.pkg)
			} else if !strings.Contains(err.Error(), "empty") {
				t.Errorf("FetchPackage(%q) expected error mentioning 'empty', got: %v", tt.pkg, err)
			}
		})
	}
}

// TestExtractDependencies tests the dependency extraction logic.
func TestExtractDependencies(t *testing.T) {
	tests := []struct {
		name   string
		groups []dependencyGroup
		want   []PackageDependency
	}{
		{
			name:   "empty groups",
			groups: []dependencyGroup{},
			want:   nil,
		},
		{
			name: "single group with dependencies",
			groups: []dependencyGroup{
				{
					TargetFramework: ".NETStandard2.0",
					Dependencies: []dependency{
						{ID: "System.Text.Json", Range: "[6.0.0, )"},
						{ID: "Microsoft.Extensions.Logging", Range: "[6.0.0, )"},
					},
				},
			},
			want: []PackageDependency{
				{Name: "System.Text.Json", Constraint: "[6.0.0, )"},
				{Name: "Microsoft.Extensions.Logging", Constraint: "[6.0.0, )"},
			},
		},
		{
			name: "multiple groups - prefers newest framework",
			groups: []dependencyGroup{
				{
					TargetFramework: ".NETFramework4.6.1",
					Dependencies: []dependency{
						{ID: "Newtonsoft.Json", Range: "[13.0.1, )"},
					},
				},
				{
					TargetFramework: ".NETStandard2.0",
					Dependencies: []dependency{
						{ID: "Newtonsoft.Json", Range: "[13.0.1, )"},
					},
				},
				{
					TargetFramework: "net6.0",
					Dependencies: []dependency{
						{ID: "System.Text.Json", Range: "[6.0.0, )"},
					},
				},
			},
			want: []PackageDependency{
				{Name: "System.Text.Json", Constraint: "[6.0.0, )"},
			},
		},
		{
			name: "framework-agnostic dependencies",
			groups: []dependencyGroup{
				{
					TargetFramework: "",
					Dependencies: []dependency{
						{ID: "Common.Logging", Range: "[3.4.1, )"},
					},
				},
			},
			want: []PackageDependency{
				{Name: "Common.Logging", Constraint: "[3.4.1, )"},
			},
		},
		{
			name: "prefers net8.0 over net6.0",
			groups: []dependencyGroup{
				{
					TargetFramework: "net6.0",
					Dependencies: []dependency{
						{ID: "OldDep", Range: "[1.0.0, )"},
					},
				},
				{
					TargetFramework: "net8.0",
					Dependencies: []dependency{
						{ID: "NewDep", Range: "[2.0.0, )"},
					},
				},
			},
			want: []PackageDependency{
				{Name: "NewDep", Constraint: "[2.0.0, )"},
			},
		},
		{
			name: "prefers netcoreapp over netstandard",
			groups: []dependencyGroup{
				{
					TargetFramework: "netstandard2.0",
					Dependencies: []dependency{
						{ID: "StandardDep", Range: "[1.0.0, )"},
					},
				},
				{
					TargetFramework: "netcoreapp3.1",
					Dependencies: []dependency{
						{ID: "CoreDep", Range: "[2.0.0, )"},
					},
				},
			},
			want: []PackageDependency{
				{Name: "CoreDep", Constraint: "[2.0.0, )"},
			},
		},
		{
			name: "prefers netstandard2.1 over netstandard2.0",
			groups: []dependencyGroup{
				{
					TargetFramework: "netstandard2.0",
					Dependencies: []dependency{
						{ID: "Standard20Dep", Range: "[1.0.0, )"},
					},
				},
				{
					TargetFramework: "netstandard2.1",
					Dependencies: []dependency{
						{ID: "Standard21Dep", Range: "[2.0.0, )"},
					},
				},
			},
			want: []PackageDependency{
				{Name: "Standard21Dep", Constraint: "[2.0.0, )"},
			},
		},
		{
			name: "ignores legacy .NET Framework when modern available",
			groups: []dependencyGroup{
				{
					TargetFramework: "net481",
					Dependencies: []dependency{
						{ID: "LegacyDep", Range: "[1.0.0, )"},
					},
				},
				{
					TargetFramework: "net6.0",
					Dependencies: []dependency{
						{ID: "ModernDep", Range: "[2.0.0, )"},
					},
				},
			},
			want: []PackageDependency{
				{Name: "ModernDep", Constraint: "[2.0.0, )"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDependencies(tt.groups)

			if len(got) != len(tt.want) {
				t.Errorf("extractDependencies() length = %v, want %v", len(got), len(tt.want))
				return
			}

			for i, dep := range got {
				if dep.Name != tt.want[i].Name {
					t.Errorf("extractDependencies()[%d].Name = %v, want %v", i, dep.Name, tt.want[i].Name)
				}
				if dep.Constraint != tt.want[i].Constraint {
					t.Errorf("extractDependencies()[%d].Constraint = %v, want %v", i, dep.Constraint, tt.want[i].Constraint)
				}
			}
		})
	}
}

// TestFindLatestStableVersion tests version selection logic.
func TestFindLatestStableVersion(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{
			name:     "all stable versions",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			want:     "3.0.0",
		},
		{
			name:     "stable and pre-release versions",
			versions: []string{"9.0.0", "10.0.0", "10.0.3", "11.0.0-preview.1.26104.118"},
			want:     "10.0.3",
		},
		{
			name:     "all pre-release versions",
			versions: []string{"1.0.0-alpha", "1.0.0-beta", "1.0.0-rc1"},
			want:     "1.0.0-rc1",
		},
		{
			name:     "single stable version",
			versions: []string{"1.0.0"},
			want:     "1.0.0",
		},
		{
			name:     "single pre-release version",
			versions: []string{"1.0.0-preview"},
			want:     "1.0.0-preview",
		},
		{
			name:     "pre-release then stable",
			versions: []string{"1.0.0-alpha", "1.0.0"},
			want:     "1.0.0",
		},
		{
			name:     "multiple pre-releases after stable",
			versions: []string{"8.0.0", "9.0.0", "10.0.0-preview.1", "10.0.0-preview.2", "10.0.0-rc1"},
			want:     "9.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findLatestStableVersion(tt.versions)
			if got != tt.want {
				t.Errorf("findLatestStableVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNewClient tests client creation.
func TestNewClient(t *testing.T) {
	client := NewClient(cache.NewNullCache(), 24*time.Hour)
	if client == nil {
		t.Fatal("NewClient() returned nil client")
	}
	if client.baseURL != "https://api.nuget.org/v3-flatcontainer" {
		t.Errorf("NewClient() baseURL = %v, want https://api.nuget.org/v3-flatcontainer", client.baseURL)
	}
	if client.registrationURL != "https://api.nuget.org/v3/registration5-semver1" {
		t.Errorf("NewClient() registrationURL = %v, want https://api.nuget.org/v3/registration5-semver1", client.registrationURL)
	}
}

// testClient creates a test client with mock URLs for testing.
func testClient(t *testing.T, baseURL, registrationURL, catalogURL string) *Client {
	t.Helper()
	backend := cache.NewNullCache()
	return &Client{
		Client:          integrations.NewClient(backend, "nuget:", time.Hour, nil),
		baseURL:         baseURL,
		registrationURL: registrationURL,
	}
}
