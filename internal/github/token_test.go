package github

import (
	"reflect"
	"testing"
)

func TestResolveToken(t *testing.T) {
	origGHAuthToken := ghAuthToken
	t.Cleanup(func() { ghAuthToken = origGHAuthToken })

	tests := []struct {
		name        string
		configToken string
		ghToken     string
		githubToken string
		ghCLIToken  string
		wantToken   string
		wantSource  string
	}{
		{
			name:        "config token wins over everything",
			configToken: "cfg-token",
			ghToken:     "gh-env-token",
			githubToken: "github-env-token",
			ghCLIToken:  "gh-cli-token",
			wantToken:   "cfg-token",
			wantSource:  "config",
		},
		{
			name:        "GH_TOKEN wins over GITHUB_TOKEN",
			ghToken:     "gh-env-token",
			githubToken: "github-env-token",
			ghCLIToken:  "gh-cli-token",
			wantToken:   "gh-env-token",
			wantSource:  "GH_TOKEN",
		},
		{
			name:        "GITHUB_TOKEN wins over gh CLI",
			githubToken: "github-env-token",
			ghCLIToken:  "gh-cli-token",
			wantToken:   "github-env-token",
			wantSource:  "GITHUB_TOKEN",
		},
		{
			name:       "gh CLI is the last resort",
			ghCLIToken: "gh-cli-token",
			wantToken:  "gh-cli-token",
			wantSource: "gh CLI",
		},
		{
			name:       "no token anywhere falls back to anonymous",
			wantToken:  "",
			wantSource: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GH_TOKEN", tt.ghToken)
			t.Setenv("GITHUB_TOKEN", tt.githubToken)
			ghAuthToken = func(host string) string { return tt.ghCLIToken }

			token, source := ResolveToken(tt.configToken, "")
			if token != tt.wantToken {
				t.Errorf("ResolveToken() token = %q, want %q", token, tt.wantToken)
			}
			if source != tt.wantSource {
				t.Errorf("ResolveToken() source = %q, want %q", source, tt.wantSource)
			}
		})
	}
}

// TestResolveToken_HostScoped verifies that gh CLI credentials are looked up
// for the exact target host: a machine authenticated only to a *.ghe.com host
// must fall back to anonymous access for github.com.
func TestResolveToken_HostScoped(t *testing.T) {
	origGHAuthToken := ghAuthToken
	t.Cleanup(func() { ghAuthToken = origGHAuthToken })

	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	ghAuthToken = func(host string) string {
		if host == "acme.ghe.com" {
			return "ghe-only-token"
		}
		return ""
	}

	tests := []struct {
		name       string
		host       string
		wantToken  string
		wantSource string
	}{
		{"github.com gets no token from ghe.com-only auth", "github.com", "", ""},
		{"ghe.com host gets its own token", "acme.ghe.com", "ghe-only-token", "gh CLI"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, source := ResolveToken("", tt.host)
			if token != tt.wantToken {
				t.Errorf("ResolveToken() token = %q, want %q", token, tt.wantToken)
			}
			if source != tt.wantSource {
				t.Errorf("ResolveToken() source = %q, want %q", source, tt.wantSource)
			}
		})
	}
}

func TestGhAuthTokenArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		want []string
	}{
		{"empty host defaults to github.com", "", []string{"auth", "token", "--hostname", "github.com"}},
		{"github.com passes hostname flag", "github.com", []string{"auth", "token", "--hostname", "github.com"}},
		{"enterprise host passes hostname flag", "acme.ghe.com", []string{"auth", "token", "--hostname", "acme.ghe.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ghAuthTokenArgs(tt.host)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ghAuthTokenArgs(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}
