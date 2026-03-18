package cmd

import (
	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/install"
	"github.com/JakeTRogers/getRelease/internal/selector"
)

type releaseClient interface {
	GetLatestRelease(owner, repo string) (*github.Release, error)
	GetReleaseByTag(owner, repo, tag string) (*github.Release, error)
	ListReleases(owner, repo string, limit int) ([]github.Release, error)
	DownloadAsset(downloadURL, destPath string) (int64, error)
}

var newGitHubClient = func() releaseClient {
	return github.NewClient()
}

var selectItems = selector.Select

var confirmAction = selector.Confirm

var newBinaryInstaller = install.NewInstaller
