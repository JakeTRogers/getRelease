package cmd

import (
	"errors"
	"fmt"
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

// confirmDestructive asks the user to confirm a destructive action, defaulting
// to no, unless force is set. Without a terminal it fails with a hint to use
// --force rather than assuming an answer.
func confirmDestructive(prompt string, force bool) (bool, error) {
	if force {
		return true, nil
	}
	ok, err := confirmAction(prompt, false)
	if errors.Is(err, selector.ErrNotInteractive) {
		return false, fmt.Errorf("cannot ask for confirmation: %w; use --force to proceed without the prompt", err)
	}
	return ok, err
}

var newBinaryInstaller = install.NewInstaller
