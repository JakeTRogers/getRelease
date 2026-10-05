// Package platform provides OS and architecture detection and asset matching.
package platform

import (
	"runtime"
	"strings"
)

// Info holds normalized operating system and architecture strings.
type Info struct {
	OS   string
	Arch string
}

// Detect returns the current platform's OS and architecture.
func Detect() Info {
	return Info{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}
}

// osAliases maps other common names for an operating system to Go's GOOS.
var osAliases = map[string]string{
	"macos": "darwin",
	"mac":   "darwin",
	"osx":   "darwin",
	"win":   "windows",
}

// archAliases maps other common names for an architecture to Go's GOARCH.
// "x86" is deliberately absent: projects use it for both 32- and 64-bit.
var archAliases = map[string]string{
	"x86_64":  "amd64",
	"x64":     "amd64",
	"aarch64": "arm64",
	"i386":    "386",
	"i686":    "386",
}

// NormalizeOS returns Go's GOOS name for os, accepting common aliases such
// as "macos" and any capitalization. Unknown names are returned lowercased.
func NormalizeOS(os string) string {
	lower := strings.ToLower(strings.TrimSpace(os))
	if canonical, ok := osAliases[lower]; ok {
		return canonical
	}
	return lower
}

// NormalizeArch returns Go's GOARCH name for arch, accepting common aliases
// such as "x86_64" and any capitalization. Unknown names are returned
// lowercased.
func NormalizeArch(arch string) string {
	lower := strings.ToLower(strings.TrimSpace(arch))
	if canonical, ok := archAliases[lower]; ok {
		return canonical
	}
	return lower
}

// OSKeywords returns the set of asset-name keywords that match the given OS,
// which may be any name NormalizeOS accepts.
func OSKeywords(os string) []string {
	os = NormalizeOS(os)
	switch os {
	case "linux":
		return []string{"linux"}
	case "darwin":
		return []string{"darwin", "macos", "mac", "osx", "apple"}
	case "windows":
		return []string{"windows", "win"}
	default:
		return []string{os}
	}
}

// ArchKeywords returns the set of asset-name keywords that match the given
// architecture, which may be any name NormalizeArch accepts.
func ArchKeywords(arch string) []string {
	arch = NormalizeArch(arch)
	switch arch {
	case "amd64":
		return []string{"amd64", "x86_64", "x64"}
	case "arm64":
		return []string{"arm64", "aarch64"}
	case "386":
		return []string{"386", "i386", "i686"}
	default:
		return []string{arch}
	}
}
