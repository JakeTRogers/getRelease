// Package platform provides OS and architecture detection and asset matching.
package platform

import "runtime"

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

// OSKeywords returns the set of asset-name keywords that match the given OS.
func OSKeywords(os string) []string {
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

// ArchKeywords returns the set of asset-name keywords that match the given architecture.
func ArchKeywords(arch string) []string {
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
