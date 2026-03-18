package platform

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

var installNameExtensions = []string{
	".tar.gz",
	".tar.xz",
	".tar.bz2",
	".tgz",
	".txz",
	".tbz2",
	".tar",
	".zip",
	".appimage",
	".exe",
}

var installNameExtraKeywords = []string{
	"gnu",
	"glibc",
	"musl",
	"musleabi",
	"musleabihf",
	"gnueabi",
	"gnueabihf",
	"gnuabi64",
	"eabi",
	"eabihf",
	"msvc",
	"android",
	"static",
	"dynamic",
	"universal",
	"universal2",
	"unknown",
	"pc",
	"apple",
}

// ResolveInstallNames returns the final on-disk basenames for the selected binaries.
// It strips recognized platform suffixes when that yields a safe command name and
// falls back to the original basename when the resolved names would collide.
func ResolveInstallNames(repo, assetName, osName, arch string, binaries []string) map[string]string {
	resolved := make(map[string]string, len(binaries))
	counts := make(map[string]int, len(binaries))

	for _, bin := range binaries {
		name := SuggestInstallName(repo, assetName, bin, osName, arch)
		resolved[bin] = name
		counts[name]++
	}

	for _, bin := range binaries {
		if counts[resolved[bin]] > 1 {
			resolved[bin] = filepath.Base(bin)
		}
	}

	return resolved
}

// SuggestInstallName returns the preferred on-disk basename for a binary.
// The original basename is returned when no confident normalization is found.
func SuggestInstallName(repo, assetName, binaryName, osName, arch string) string {
	originalBase := filepath.Base(binaryName)
	stem, ext := splitInstallName(originalBase)
	trimmedStem, removedPlatformSuffix := trimPlatformSuffixes(stem, osName, arch)
	if !removedPlatformSuffix || trimmedStem == "" {
		return originalBase
	}

	trimmedStem = maybeTrimVersionSuffix(trimmedStem, repo, assetName, osName, arch)
	if !looksLikeCommandName(trimmedStem) {
		return originalBase
	}

	return trimmedStem + ext
}

func splitInstallName(name string) (string, string) {
	lower := strings.ToLower(name)
	for _, ext := range installNameExtensions {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)], name[len(name)-len(ext):]
		}
	}

	ext := filepath.Ext(name)
	if strings.ContainsAny(ext, "-_") {
		return name, ""
	}
	return strings.TrimSuffix(name, ext), ext
}

func trimPlatformSuffixes(stem, osName, arch string) (string, bool) {
	keywords := append([]string(nil), OSKeywords(osName)...)
	keywords = append(keywords, ArchKeywords(arch)...)
	keywords = append(keywords, installNameExtraKeywords...)
	slices.SortFunc(keywords, func(a, b string) int {
		return len(b) - len(a)
	})

	trimmed := stem
	removedPlatformSuffix := false

	for {
		var (
			nextStem string
			matched  bool
		)

		for _, keyword := range keywords {
			candidate, ok := trimDelimitedSuffix(trimmed, keyword)
			if !ok {
				continue
			}

			nextStem = candidate
			matched = true
			if isPlatformKeyword(keyword, osName, arch) {
				removedPlatformSuffix = true
			}
			break
		}

		if !matched {
			break
		}

		trimmed = strings.TrimRight(nextStem, "-_.")
		if trimmed == "" {
			break
		}
	}

	return trimmed, removedPlatformSuffix
}

func trimDelimitedSuffix(stem, suffix string) (string, bool) {
	lower := strings.ToLower(stem)
	lowerSuffix := strings.ToLower(suffix)
	for _, sep := range []string{"-", "_", "."} {
		needle := sep + lowerSuffix
		if strings.HasSuffix(lower, needle) {
			return stem[:len(stem)-len(needle)], true
		}
	}
	return "", false
}

func isPlatformKeyword(keyword, osName, arch string) bool {
	for _, candidate := range OSKeywords(osName) {
		if strings.EqualFold(candidate, keyword) {
			return true
		}
	}
	for _, candidate := range ArchKeywords(arch) {
		if strings.EqualFold(candidate, keyword) {
			return true
		}
	}
	return false
}

func maybeTrimVersionSuffix(stem, repo, assetName, osName, arch string) string {
	trimmed, ok := trimTrailingVersion(stem)
	if !ok || trimmed == "" {
		return stem
	}

	refs := []string{normalizeName(repo)}
	assetStem, _ := splitInstallName(filepath.Base(assetName))
	assetStem, _ = trimPlatformSuffixes(assetStem, osName, arch)
	refs = append(refs, normalizeName(assetStem))

	trimmedNorm := normalizeName(trimmed)
	for _, ref := range refs {
		if ref != "" && trimmedNorm == ref {
			return trimmed
		}
	}

	return stem
}

func trimTrailingVersion(stem string) (string, bool) {
	for _, sep := range []string{"-", "_", "."} {
		idx := strings.LastIndex(stem, sep)
		if idx <= 0 || idx == len(stem)-1 {
			continue
		}
		suffix := stem[idx+1:]
		if isVersionToken(suffix) {
			return strings.TrimRight(stem[:idx], "-_."), true
		}
	}
	return "", false
}

func isVersionToken(token string) bool {
	if token == "" {
		return false
	}

	trimmed := strings.TrimPrefix(strings.ToLower(token), "v")
	if trimmed == "" {
		return false
	}

	hasDigit := false
	for _, r := range trimmed {
		switch {
		case unicode.IsDigit(r):
			hasDigit = true
		case r == '.', r == '-', r == '+':
		case unicode.IsLetter(r):
		default:
			return false
		}
	}

	return hasDigit
}

func looksLikeCommandName(name string) bool {
	if name == "" {
		return false
	}

	hasLetterOrDigit := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			hasLetterOrDigit = true
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}

	return hasLetterOrDigit
}

func normalizeName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
