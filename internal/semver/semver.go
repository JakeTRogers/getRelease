// Package semver provides minimal semantic version parsing and comparison.
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version represents a stable semantic version with major, minor, and patch parts.
type Version struct {
	Major int
	Minor int
	Patch int
}

// Parse parses a stable semantic version tag in vMAJOR.MINOR.PATCH form.
func Parse(tag string) (Version, error) {
	s := tag
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		s = s[1:]
	}
	if s == "" {
		return Version{}, fmt.Errorf("empty version string")
	}
	if strings.ContainsAny(s, "-+") {
		return Version{}, fmt.Errorf("prerelease or build metadata not supported: %s", tag)
	}

	majorStr, rest, ok := strings.Cut(s, ".")
	if !ok {
		return Version{}, fmt.Errorf("invalid version format: %s", tag)
	}
	minorStr, patchStr, ok := strings.Cut(rest, ".")
	if !ok {
		return Version{}, fmt.Errorf("invalid version format: %s", tag)
	}
	if strings.Contains(patchStr, ".") {
		return Version{}, fmt.Errorf("too many components: %s", tag)
	}

	major, err := parseComponent(majorStr, tag)
	if err != nil {
		return Version{}, err
	}
	minor, err := parseComponent(minorStr, tag)
	if err != nil {
		return Version{}, err
	}
	patch, err := parseComponent(patchStr, tag)
	if err != nil {
		return Version{}, err
	}

	return Version{Major: major, Minor: minor, Patch: patch}, nil
}

func parseComponent(s, tag string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty component in version: %s", tag)
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("leading zero in component %q: %s", s, tag)
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("non-numeric component %q: %s", s, tag)
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("non-numeric component %q: %s", s, tag)
	}
	return n, nil
}

// String formats a version as a v-prefixed semantic version.
func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare compares v with other and returns -1, 0, or 1.
func (v Version) Compare(other Version) int {
	switch {
	case v.Major < other.Major:
		return -1
	case v.Major > other.Major:
		return 1
	case v.Minor < other.Minor:
		return -1
	case v.Minor > other.Minor:
		return 1
	case v.Patch < other.Patch:
		return -1
	case v.Patch > other.Patch:
		return 1
	default:
		return 0
	}
}

// SameMajor reports whether v and other share the same major version.
func (v Version) SameMajor(other Version) bool {
	return v.Major == other.Major
}

// SameMinor reports whether v and other share the same major and minor versions.
func (v Version) SameMinor(other Version) bool {
	return v.Major == other.Major && v.Minor == other.Minor
}
