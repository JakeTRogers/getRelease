package history

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Record represents a single install/upgrade entry in history.
type Record struct {
	ID          string    `json:"id"`
	Owner       string    `json:"owner"`
	Repo        string    `json:"repo"`
	Tag         string    `json:"tag"`
	Asset       AssetInfo `json:"asset"`
	Binaries    []Binary  `json:"binaries"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	InstalledAt time.Time `json:"installedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AssetInfo records which asset was downloaded.
type AssetInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Binary records a single installed binary from an asset.
type Binary struct {
	Name        string `json:"name"`        // original name in archive
	InstalledAs string `json:"installedAs"` // name on disk (may differ if renamed)
	InstallPath string `json:"installPath"` // absolute path where installed
}

// GenerateID returns an 8-character random hex string using crypto/rand.
func GenerateID() string {
	b := make([]byte, 4) // 4 bytes -> 8 hex characters
	if _, err := rand.Read(b); err != nil {
		// fallback to time-based value if crypto/rand fails
		return fmt.Sprintf("%08x", uint32(time.Now().UnixNano()))
	}
	return hex.EncodeToString(b)
}
