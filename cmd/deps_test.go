package cmd

import "testing"

func TestNewGitHubClientRejectsUnsupportedHost(t *testing.T) {
	useTestCommandDeps(t, nil)

	if _, err := newGitHubClient("github.acme.internal"); err == nil {
		t.Fatal("newGitHubClient() error = nil, want error for unsupported host")
	}
}
