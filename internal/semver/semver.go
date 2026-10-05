// Package semver provides minimal semantic version parsing and comparison.
package semver

import (
	"cmp"
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

// CompareTags compares two release tags by semantic version precedence,
// including prerelease identifiers, so v1.0.0-rc.1 sorts before v1.0.0.
// Numeric prerelease identifiers have no fixed-width size limit.
// Build metadata is ignored. ok is false when either tag is not a
// MAJOR.MINOR.PATCH version with an optional prerelease suffix.
func CompareTags(a, b string) (result int, ok bool) {
	av, aPre, err := parseWithPrerelease(a)
	if err != nil {
		return 0, false
	}
	bv, bPre, err := parseWithPrerelease(b)
	if err != nil {
		return 0, false
	}
	if c := av.Compare(bv); c != 0 {
		return c, true
	}
	return comparePrerelease(aPre, bPre), true
}

func parseWithPrerelease(tag string) (Version, string, error) {
	core, _, _ := strings.Cut(tag, "+")
	core, prerelease, _ := strings.Cut(core, "-")
	v, err := Parse(core)
	return v, prerelease, err
}

// comparePrerelease orders prerelease strings per SemVer 2.0.0: a version
// without a prerelease ranks higher, and dot-separated identifiers are
// compared left to right, numeric ones numerically and below alphanumeric
// ones, with a shorter list of otherwise equal identifiers ranking lower.
func comparePrerelease(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	aIDs, bIDs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(aIDs) && i < len(bIDs); i++ {
		if c := comparePrereleaseIdentifier(aIDs[i], bIDs[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(aIDs), len(bIDs))
}

func comparePrereleaseIdentifier(a, b string) int {
	aNumeric := isNumericPrereleaseIdentifier(a)
	bNumeric := isNumericPrereleaseIdentifier(b)
	switch {
	case aNumeric && bNumeric:
		// Preserve numeric equality for leading-zero identifiers accepted by the parser.
		a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
		if c := cmp.Compare(len(a), len(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	case aNumeric:
		return -1
	case bNumeric:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func isNumericPrereleaseIdentifier(identifier string) bool {
	return identifier != "" && strings.Trim(identifier, "0123456789") == ""
}

// SameMajor reports whether v and other share the same major version.
func (v Version) SameMajor(other Version) bool {
	return v.Major == other.Major
}

// SameMinor reports whether v and other share the same major and minor versions.
func (v Version) SameMinor(other Version) bool {
	return v.Major == other.Major && v.Minor == other.Minor
}
