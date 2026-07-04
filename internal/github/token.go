package github

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

const ghTokenTimeout = 3 * time.Second

// ghAuthTokenArgs builds the `gh auth token` argument list for host,
// passing --hostname only for non-default hosts since gh treats an omitted
// hostname as github.com.
func ghAuthTokenArgs(host string) []string {
	args := []string{"auth", "token"}
	if host != "" && host != DefaultHost {
		args = append(args, "--hostname", host)
	}
	return args
}

// ghAuthToken asks the gh CLI for its stored token for the given host
// ("github.com" or a *.ghe.com host), returning "" if gh is not installed,
// not authenticated for that host, or slow to respond. Package variable so
// tests can stub it.
var ghAuthToken = func(host string) string {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), ghTokenTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, ghPath, ghAuthTokenArgs(host)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ResolveToken returns the GitHub API token to use and the source it came
// from, checking in order: the configured token (config file or
// GETRELEASE_TOKEN), the GH_TOKEN and GITHUB_TOKEN environment variables, and
// finally the gh CLI's stored credentials for host (which gh treats the same
// way for github.com and *.ghe.com). It returns empty strings when no token
// is available, in which case API requests are made anonymously.
func ResolveToken(configToken, host string) (token, source string) {
	if configToken != "" {
		return configToken, "config"
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t, "GH_TOKEN"
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t, "GITHUB_TOKEN"
	}
	if t := ghAuthToken(host); t != "" {
		return t, "gh CLI"
	}
	return "", ""
}
