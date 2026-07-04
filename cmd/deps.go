package cmd

import (
	"log/slog"

	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/install"
	"github.com/JakeTRogers/getRelease/internal/selector"
)

type releaseClient interface {
	GetLatestRelease(owner, repo string) (*github.Release, error)
	GetReleaseByTag(owner, repo, tag string) (*github.Release, error)
	ListReleases(owner, repo string, limit int) ([]github.Release, error)
	DownloadAsset(asset github.Asset, destPath string) (int64, error)
}

var newGitHubClient = func(host string) (releaseClient, error) {
	normalized, err := github.NormalizeHost(host)
	if err != nil {
		return nil, err
	}
	if normalized != github.DefaultHost {
		slog.Debug("targeting GitHub Enterprise host", "host", normalized)
	}

	token, source := github.ResolveToken(cfgViper.GetString("token"), normalized)
	if token != "" {
		slog.Debug("authenticating GitHub API requests", "source", source)
	} else {
		slog.Debug("no GitHub token found, using anonymous API requests")
	}
	return github.NewClientForHost(normalized).WithToken(token), nil
}

var selectItems = selector.Select

var confirmAction = selector.Confirm

var newBinaryInstaller = install.NewInstaller
