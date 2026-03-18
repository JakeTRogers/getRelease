package platform

import (
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JakeTRogers/getRelease/internal/github"
)

// skipExtensions are file extensions always excluded from asset candidates.
var skipExtensions = map[string]bool{
	".sha256": true,
	".sha512": true,
	".md5":    true,
	".sig":    true,
	".asc":    true,
	".sbom":   true,
	".pem":    true,
	".txt":    true,
}

// ShouldSkipAsset returns true if the asset name matches a known non-installable extension.
func ShouldSkipAsset(name string) bool {
	lower := strings.ToLower(name)
	ext := filepath.Ext(lower)
	if skipExtensions[ext] {
		return true
	}
	// Handle .json files (often metadata, not installable)
	if ext == ".json" {
		return true
	}
	return false
}

// MatchesExcludePattern returns true if the asset name matches any of the provided glob patterns.
func MatchesExcludePattern(name string, patterns []string) bool {
	lower := strings.ToLower(name)
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(strings.ToLower(pattern), lower); matched {
			return true
		}
	}
	return false
}

// ContainsKeyword checks if the asset name (lowered) contains any of the keywords.
// Keywords are matched as delimited substrings: they must be bounded by
// start/end of string or a non-alphanumeric character to avoid false matches
// (e.g. "win" should not match "darwin").
func ContainsKeyword(name string, keywords []string) bool {
	lower := strings.ToLower(name)
	for _, kw := range keywords {
		idx := strings.Index(lower, strings.ToLower(kw))
		if idx < 0 {
			continue
		}
		// Check that the match is at a word boundary
		before := idx == 0 || !isAlphaNum(lower[idx-1])
		after := idx+len(kw) >= len(lower) || !isAlphaNum(lower[idx+len(kw)])
		if before && after {
			return true
		}
	}
	return false
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// FormatScore returns the priority score for an asset based on the preferred formats list.
// Lower index in the formats list = higher score. Unknown formats return 0.
func FormatScore(name string, formats []string) int {
	lower := strings.ToLower(name)
	for i, fmt := range formats {
		ext := "." + strings.ToLower(fmt)
		if strings.HasSuffix(lower, ext) {
			return len(formats) - i
		}
	}
	return 0
}

func libcScore(name, osName string) int {
	if osName != "linux" {
		return 0
	}

	switch {
	case ContainsKeyword(name, []string{"gnu", "glibc"}):
		return 2
	case ContainsKeyword(name, []string{"musl"}):
		return 1
	default:
		return 0
	}
}

func assetScore(name, osName string, formats []string) int {
	return FormatScore(name, formats)*10 + libcScore(name, osName)
}

// MatchAssets filters and ranks release assets based on OS, arch, and format preferences.
// It returns matching assets sorted by format score (highest first).
func MatchAssets(assets []github.Asset, osName, arch string, formats, excludePatterns []string) []github.Asset {
	osKw := OSKeywords(osName)
	archKw := ArchKeywords(arch)

	slog.Debug("matching assets", "total", len(assets), "os", osName, "arch", arch,
		"osKeywords", osKw, "archKeywords", archKw)

	var candidates []github.Asset
	for _, a := range assets {
		name := a.Name

		// Step 1: skip checksums/signatures/metadata
		if ShouldSkipAsset(name) {
			slog.Debug("skipping asset (extension)", "name", name)
			continue
		}

		// Step 2: exclude user-configured patterns
		if MatchesExcludePattern(name, excludePatterns) {
			slog.Debug("skipping asset (exclude pattern)", "name", name)
			continue
		}

		// Step 3: must match OS keyword
		if len(osKw) > 0 && !ContainsKeyword(name, osKw) {
			slog.Debug("skipping asset (OS mismatch)", "name", name)
			continue
		}

		// Step 4: must match arch keyword
		if len(archKw) > 0 && !ContainsKeyword(name, archKw) {
			slog.Debug("skipping asset (arch mismatch)", "name", name)
			continue
		}

		candidates = append(candidates, a)
	}

	// Step 5: sort by overall preference score (highest first)
	sort.SliceStable(candidates, func(i, j int) bool {
		return assetScore(candidates[i].Name, osName, formats) > assetScore(candidates[j].Name, osName, formats)
	})

	slog.Debug("matched assets", "count", len(candidates))
	return candidates
}

// BestAsset returns the highest-ranked asset and whether that choice is unambiguous.
func BestAsset(assets []github.Asset, osName string, formats []string) (github.Asset, bool) {
	if len(assets) == 0 {
		return github.Asset{}, false
	}

	best := assets[0]
	bestScore := assetScore(best.Name, osName, formats)
	unique := true

	for _, asset := range assets[1:] {
		score := assetScore(asset.Name, osName, formats)
		if score > bestScore {
			best = asset
			bestScore = score
			unique = true
			continue
		}
		if score == bestScore {
			unique = false
		}
	}

	return best, unique
}
