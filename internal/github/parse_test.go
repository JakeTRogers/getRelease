package github

import (
	"strings"
	"testing"
)

func TestParseRepoURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantHost  string
		wantErr   bool
	}{
		{
			name:      "full HTTPS URL",
			url:       "https://github.com/derailed/k9s",
			wantOwner: "derailed",
			wantRepo:  "k9s",
			wantHost:  "github.com",
		},
		{
			name:      "URL with trailing slash",
			url:       "https://github.com/derailed/k9s/",
			wantOwner: "derailed",
			wantRepo:  "k9s",
			wantHost:  "github.com",
		},
		{
			name:      "URL with .git suffix",
			url:       "https://github.com/derailed/k9s.git",
			wantOwner: "derailed",
			wantRepo:  "k9s",
			wantHost:  "github.com",
		},
		{
			name:      "URL with extra path segments",
			url:       "https://github.com/derailed/k9s/releases/tag/v1.0",
			wantOwner: "derailed",
			wantRepo:  "k9s",
			wantHost:  "github.com",
		},
		{
			name:      "without scheme",
			url:       "github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
		},
		{
			name:      "HTTP scheme",
			url:       "http://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
		},
		{
			name:      "www prefix normalized",
			url:       "https://www.github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
		},
		{
			name:      "enterprise host taken from URL",
			url:       "https://acme.ghe.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "acme.ghe.com",
		},
		{
			name:      "enterprise host without scheme",
			url:       "acme.ghe.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "acme.ghe.com",
		},
		{
			name:      "scp-like SSH URL",
			url:       "git@github.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
		},
		{
			name:      "scp-like SSH URL without .git",
			url:       "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
		},
		{
			name:      "scp-like SSH URL for enterprise host",
			url:       "git@acme.ghe.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "acme.ghe.com",
		},
		{
			name:      "ssh scheme URL",
			url:       "ssh://git@github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
		},
		{
			name:      "host with port is not scp-like",
			url:       "github.com:443/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
		},
		{
			name:    "scp-like SSH URL for another host",
			url:     "git@gitlab.com:owner/repo.git",
			wantErr: true,
		},
		{
			name:    "scp-like SSH URL without repo",
			url:     "git@github.com:owner",
			wantErr: true,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
		{
			name:    "not a GitHub URL",
			url:     "https://gitlab.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "self-hosted GHES URL rejected",
			url:     "https://github.acme.internal/owner/repo",
			wantErr: true,
		},
		{
			name:    "only owner, no repo",
			url:     "https://github.com/owner",
			wantErr: true,
		},
		{
			name:    "root URL only",
			url:     "https://github.com/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			owner, repo, host, err := ParseRepoURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRepoURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("ParseRepoURL(%q) owner = %q, want %q", tt.url, owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("ParseRepoURL(%q) repo = %q, want %q", tt.url, repo, tt.wantRepo)
			}
			if host != tt.wantHost {
				t.Errorf("ParseRepoURL(%q) host = %q, want %q", tt.url, host, tt.wantHost)
			}
		})
	}
}

func TestParseRepoRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ref     string
		want    RepoRef
		wantErr string
	}{
		{name: "owner/repo", ref: "sharkdp/bat", want: RepoRef{Owner: "sharkdp", Repo: "bat"}},
		{name: "owner/repo with tag", ref: "junegunn/fzf@v0.66.0", want: RepoRef{Owner: "junegunn", Repo: "fzf", Tag: "v0.66.0"}},
		{name: "tag containing @", ref: "owner/repo@pkg@1.2.0", want: RepoRef{Owner: "owner", Repo: "repo", Tag: "pkg@1.2.0"}},
		{name: "tag containing /", ref: "owner/repo@release/1.0", want: RepoRef{Owner: "owner", Repo: "repo", Tag: "release/1.0"}},
		{name: "enterprise host", ref: "acme.ghe.com/org/repo", want: RepoRef{Owner: "org", Repo: "repo", Host: "acme.ghe.com"}},
		{name: "enterprise host with tag", ref: "acme.ghe.com/org/repo@v1.0.0", want: RepoRef{Owner: "org", Repo: "repo", Host: "acme.ghe.com", Tag: "v1.0.0"}},
		{name: "github.com host", ref: "github.com/owner/repo", want: RepoRef{Owner: "owner", Repo: "repo", Host: "github.com"}},
		{name: "HTTPS URL", ref: "https://github.com/owner/repo", want: RepoRef{Owner: "owner", Repo: "repo", Host: "github.com"}},
		{name: "enterprise HTTPS URL", ref: "https://acme.ghe.com/org/repo", want: RepoRef{Owner: "org", Repo: "repo", Host: "acme.ghe.com"}},
		{name: "scp-like SSH URL", ref: "git@acme.ghe.com:org/repo.git", want: RepoRef{Owner: "org", Repo: "repo", Host: "acme.ghe.com"}},
		{name: "ssh scheme URL", ref: "ssh://git@github.com/owner/repo.git", want: RepoRef{Owner: "owner", Repo: "repo", Host: "github.com"}},
		{name: "owner only", ref: "owner", wantErr: "use [host/]owner/repo[@tag]"},
		{name: "owner only with tag", ref: "owner@v1.0.0", wantErr: "use [host/]owner/repo[@tag]"},
		{name: "missing owner", ref: "/repo", wantErr: "use [host/]owner/repo[@tag]"},
		{name: "missing repo", ref: "owner/", wantErr: "use [host/]owner/repo[@tag]"},
		{name: "empty tag", ref: "owner/repo@", wantErr: "missing tag after @"},
		{name: "empty tag after host", ref: "acme.ghe.com/org/repo@", wantErr: "missing tag after @"},
		{name: "tag after URL with scheme", ref: "https://github.com/owner/repo@v1.0.0", wantErr: "use --tag"},
		{name: "unsupported URL host", ref: "https://gitlab.com/owner/repo", wantErr: "not a supported GitHub URL"},
		{name: "unsupported bare host", ref: "gitlab.com/owner/repo", wantErr: "not a supported GitHub URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRepoRef(tt.ref)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseRepoRef(%q) error = %v, want substring %q", tt.ref, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRepoRef(%q) error = %v", tt.ref, err)
			}
			if got != tt.want {
				t.Errorf("ParseRepoRef(%q) = %+v, want %+v", tt.ref, got, tt.want)
			}
		})
	}
}
