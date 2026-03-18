package github

import (
	"testing"
)

func TestParseRepoURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "full HTTPS URL",
			url:       "https://github.com/derailed/k9s",
			wantOwner: "derailed",
			wantRepo:  "k9s",
		},
		{
			name:      "URL with trailing slash",
			url:       "https://github.com/derailed/k9s/",
			wantOwner: "derailed",
			wantRepo:  "k9s",
		},
		{
			name:      "URL with .git suffix",
			url:       "https://github.com/derailed/k9s.git",
			wantOwner: "derailed",
			wantRepo:  "k9s",
		},
		{
			name:      "URL with extra path segments",
			url:       "https://github.com/derailed/k9s/releases/tag/v1.0",
			wantOwner: "derailed",
			wantRepo:  "k9s",
		},
		{
			name:      "without scheme",
			url:       "github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTP scheme",
			url:       "http://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "www prefix",
			url:       "https://www.github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
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
			owner, repo, err := ParseRepoURL(tt.url)
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
		})
	}
}
