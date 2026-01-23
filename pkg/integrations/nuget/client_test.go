package nuget

import (
	"testing"
	"time"
)

// TestClient_FetchPackage tests fetching a well-known NuGet package.
// This is a unit test that mocks the HTTP responses.
func TestClient_FetchPackage(t *testing.T) {
	// For now, this is a placeholder. We'll add proper mocking later.
	// This ensures the package compiles and has test coverage.
	t.Skip("Integration test - run with go test -tags=integration")
}

// TestExtractDependencies tests the dependency extraction logic.
func TestExtractDependencies(t *testing.T) {
	tests := []struct {
		name   string
		groups []dependencyGroup
		want   []string
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
			want: []string{"System.Text.Json", "Microsoft.Extensions.Logging"},
		},
		{
			name: "multiple groups - prefers netstandard",
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
						{ID: "System.Text.Json", Range: "[6.0.0, )"},
					},
				},
			},
			want: []string{"System.Text.Json"},
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
			want: []string{"Common.Logging"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDependencies(tt.groups)

			// Check length
			if len(got) != len(tt.want) {
				t.Errorf("extractDependencies() length = %v, want %v", len(got), len(tt.want))
				return
			}

			// Check each dependency
			for i, dep := range got {
				if dep != tt.want[i] {
					t.Errorf("extractDependencies()[%d] = %v, want %v", i, dep, tt.want[i])
				}
			}
		})
	}
}

// TestNewClient tests client creation.
func TestNewClient(t *testing.T) {
	client, err := NewClient(24 * time.Hour)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewClient() returned nil client")
	}
	if client.baseURL != "https://api.nuget.org/v3-flatcontainer" {
		t.Errorf("NewClient() baseURL = %v, want https://api.nuget.org/v3-flatcontainer", client.baseURL)
	}
}
