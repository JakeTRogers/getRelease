package history

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// PinLevel represents the version pinning policy for an installed target.
type PinLevel string

const (
	// PinNone means upgrades track the latest release.
	PinNone PinLevel = ""
	// PinPatch locks upgrades to the current exact release.
	PinPatch PinLevel = "patch"
	// PinMinor allows patch updates within the current minor release.
	PinMinor PinLevel = "minor"
	// PinMajor allows minor and patch updates within the current major release.
	PinMajor PinLevel = "major"
)

// Record represents a single install/upgrade entry in history.
type Record struct {
	ID          string    `json:"id"`
	Owner       string    `json:"owner"`
	Repo        string    `json:"repo"`
	Tag         string    `json:"tag"`
	PinLevel    PinLevel  `json:"pinLevel,omitempty"`
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

// ParsePinLevel parses a user-provided pin level.
func ParsePinLevel(s string) (PinLevel, error) {
	p := PinLevel(strings.ToLower(strings.TrimSpace(s)))
	if p == PinNone || !isValidPinLevel(p) {
		return PinNone, fmt.Errorf("invalid pin level %q", s)
	}
	return p, nil
}

func isValidPinLevel(p PinLevel) bool {
	switch p {
	case PinNone, PinPatch, PinMinor, PinMajor:
		return true
	default:
		return false
	}
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
