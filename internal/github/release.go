// Package github provides a client for the GitHub Releases API.
package github

import "time"

// Release represents a GitHub release.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

// DisplayName returns the best human-readable name for the release.
func (r Release) DisplayName() string {
	if r.Name != "" {
		return r.Name
	}
	return r.TagName
}

// Asset represents a downloadable file attached to a GitHub release.
type Asset struct {
	Name          string `json:"name"`
	DownloadURL   string `json:"browser_download_url"`
	Size          int64  `json:"size"`
	ContentType   string `json:"content_type"`
	DownloadCount int    `json:"download_count"`
}
