package semver

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tag     string
		want    Version
		wantErr bool
	}{
		{
			name: "with lowercase v prefix",
			tag:  "v1.2.3",
			want: Version{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name: "without prefix",
			tag:  "1.2.3",
			want: Version{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name: "with uppercase V prefix",
			tag:  "V1.2.3",
			want: Version{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name: "zero version",
			tag:  "v0.0.0",
			want: Version{Major: 0, Minor: 0, Patch: 0},
		},
		{
			name: "double digit parts",
			tag:  "v10.20.30",
			want: Version{Major: 10, Minor: 20, Patch: 30},
		},
		{
			name:    "partial version",
			tag:     "v1.2",
			wantErr: true,
		},
		{
			name:    "prerelease",
			tag:     "v1.2.3-rc1",
			wantErr: true,
		},
		{
			name:    "build metadata",
			tag:     "v1.2.3+build",
			wantErr: true,
		},
		{
			name:    "non semver prefix",
			tag:     "release-1.2.3",
			wantErr: true,
		},
		{
			name:    "too many components",
			tag:     "v1.2.3.4",
			wantErr: true,
		},
		{
			name:    "excessive dots",
			tag:     "v1.2.3.4.5.6.7",
			wantErr: true,
		},
		{
			name:    "leading zero in major",
			tag:     "v01.2.3",
			wantErr: true,
		},
		{
			name:    "leading zero in minor",
			tag:     "v1.02.3",
			wantErr: true,
		},
		{
			name:    "leading zero in patch",
			tag:     "v1.2.03",
			wantErr: true,
		},
		{
			name:    "negative component",
			tag:     "v1.-2.3",
			wantErr: true,
		},
		{
			name:    "empty component",
			tag:     "v1..3",
			wantErr: true,
		},
		{
			name:    "empty string",
			tag:     "",
			wantErr: true,
		},
		{
			name:    "prefix only",
			tag:     "v",
			wantErr: true,
		},
		{
			name:    "nonnumeric",
			tag:     "abc",
			wantErr: true,
		},
		{
			name:    "nonnumeric component",
			tag:     "1.2.x",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Parse(tt.tag)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse(%q) error = %v, wantErr %v", tt.tag, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.tag, got, tt.want)
			}
		})
	}
}

func TestParseComponentRejectsSigns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		part string
	}{
		{name: "plus sign", part: "+1"},
		{name: "minus sign", part: "-1"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := parseComponent(tt.part, "v1.2.3"); err == nil {
				t.Fatalf("parseComponent(%q) error = nil, want non-nil", tt.part)
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	t.Parallel()

	v := Version{Major: 1, Minor: 2, Patch: 3}
	if got := v.String(); got != "v1.2.3" {
		t.Fatalf("Version.String() = %q, want %q", got, "v1.2.3")
	}
}

func TestVersionCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  Version
		right Version
		want  int
	}{
		{
			name:  "equal versions",
			left:  Version{Major: 1, Minor: 2, Patch: 3},
			right: Version{Major: 1, Minor: 2, Patch: 3},
			want:  0,
		},
		{
			name:  "patch boundary",
			left:  Version{Major: 1, Minor: 2, Patch: 3},
			right: Version{Major: 1, Minor: 2, Patch: 4},
			want:  -1,
		},
		{
			name:  "minor boundary",
			left:  Version{Major: 1, Minor: 2, Patch: 9},
			right: Version{Major: 1, Minor: 3, Patch: 0},
			want:  -1,
		},
		{
			name:  "major boundary",
			left:  Version{Major: 1, Minor: 9, Patch: 9},
			right: Version{Major: 2, Minor: 0, Patch: 0},
			want:  -1,
		},
		{
			name:  "greater patch",
			left:  Version{Major: 1, Minor: 2, Patch: 5},
			right: Version{Major: 1, Minor: 2, Patch: 4},
			want:  1,
		},
		{
			name:  "greater minor",
			left:  Version{Major: 1, Minor: 3, Patch: 0},
			right: Version{Major: 1, Minor: 2, Patch: 9},
			want:  1,
		},
		{
			name:  "greater major",
			left:  Version{Major: 2, Minor: 0, Patch: 0},
			right: Version{Major: 1, Minor: 9, Patch: 9},
			want:  1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.left.Compare(tt.right); got != tt.want {
				t.Fatalf("Version.Compare() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestVersionSameMajorAndSameMinor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		left          Version
		right         Version
		wantSameMajor bool
		wantSameMinor bool
	}{
		{
			name:          "same version",
			left:          Version{Major: 1, Minor: 2, Patch: 3},
			right:         Version{Major: 1, Minor: 2, Patch: 3},
			wantSameMajor: true,
			wantSameMinor: true,
		},
		{
			name:          "same minor different patch",
			left:          Version{Major: 1, Minor: 2, Patch: 3},
			right:         Version{Major: 1, Minor: 2, Patch: 4},
			wantSameMajor: true,
			wantSameMinor: true,
		},
		{
			name:          "same major different minor",
			left:          Version{Major: 1, Minor: 2, Patch: 3},
			right:         Version{Major: 1, Minor: 3, Patch: 0},
			wantSameMajor: true,
			wantSameMinor: false,
		},
		{
			name:          "different major",
			left:          Version{Major: 1, Minor: 9, Patch: 9},
			right:         Version{Major: 2, Minor: 0, Patch: 0},
			wantSameMajor: false,
			wantSameMinor: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.left.SameMajor(tt.right); got != tt.wantSameMajor {
				t.Fatalf("Version.SameMajor() = %v, want %v", got, tt.wantSameMajor)
			}
			if got := tt.left.SameMinor(tt.right); got != tt.wantSameMinor {
				t.Fatalf("Version.SameMinor() = %v, want %v", got, tt.wantSameMinor)
			}
		})
	}
}

func TestCompareTags(t *testing.T) {
	t.Parallel()

	// SemVer 2.0.0 §11 precedence example, lowest to highest.
	ordered := []string{
		"v1.0.0-alpha",
		"v1.0.0-alpha.1",
		"v1.0.0-alpha.beta",
		"v1.0.0-beta",
		"v1.0.0-beta.2",
		"v1.0.0-beta.11",
		"v1.0.0-rc.1",
		"v1.0.0",
		"v1.0.1",
	}
	for i := 0; i+1 < len(ordered); i++ {
		lo, hi := ordered[i], ordered[i+1]
		if got, ok := CompareTags(lo, hi); !ok || got != -1 {
			t.Errorf("CompareTags(%q, %q) = (%d, %v), want (-1, true)", lo, hi, got, ok)
		}
		if got, ok := CompareTags(hi, lo); !ok || got != 1 {
			t.Errorf("CompareTags(%q, %q) = (%d, %v), want (1, true)", hi, lo, got, ok)
		}
	}

	tests := []struct {
		a, b   string
		want   int
		wantOK bool
	}{
		{a: "v0.75.0-rc1", b: "v0.74.4", want: 1, wantOK: true},
		{a: "1.2.3", b: "v1.2.3", want: 0, wantOK: true},
		{a: "v1.0.0+build.5", b: "v1.0.0", want: 0, wantOK: true},
		{a: "v1.0.0-rc.1+build", b: "v1.0.0-rc.1", want: 0, wantOK: true},
		{a: "nightly", b: "v1.0.0", wantOK: false},
		{a: "v1.0.0", b: "2026-09-21", wantOK: false},
		{a: "v1.2", b: "v1.2.0", wantOK: false},
	}
	for _, tt := range tests {
		got, ok := CompareTags(tt.a, tt.b)
		if ok != tt.wantOK || (ok && got != tt.want) {
			t.Errorf("CompareTags(%q, %q) = (%d, %v), want (%d, %v)", tt.a, tt.b, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestCompareTags_LargeNumericPrerelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want int
	}{
		{
			name: "overflowing identifiers with different lengths",
			a:    "90000000000000000000", b: "100000000000000000000", want: -1,
		},
		{
			name: "overflowing identifiers with equal lengths",
			a:    "90000000000000000000", b: "90000000000000000001", want: -1,
		},
		{
			name: "uint64 boundary",
			a:    "18446744073709551615", b: "18446744073709551616", want: -1,
		},
		{
			name: "numeric before alphanumeric",
			a:    "90000000000000000000", b: "1alpha", want: -1,
		},
		{
			name: "equal large numeric identifier",
			a:    "90000000000000000000", b: "90000000000000000000",
		},
		{
			name: "equal large identifier compares following identifiers",
			a:    "90000000000000000000.2", b: "90000000000000000000.11", want: -1,
		},
		{
			name: "shorter list of equal identifiers",
			a:    "rc.90000000000000000000", b: "rc.90000000000000000000.1", want: -1,
		},
		{
			name: "identifiers longer than machine integers",
			a:    strings.Repeat("9", 128), b: "1" + strings.Repeat("0", 128), want: -1,
		},
		{
			name: "accepted leading zeros retain numeric equality",
			a:    "0002", b: "2",
		},
		{
			name: "accepted zero identifiers retain numeric equality",
			a:    "000", b: "0",
		},
		{
			name: "accepted leading zeros do not change numeric order",
			a:    strings.Repeat("0", 128) + "1", b: "2", want: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, b := "v1.0.0-"+tt.a, "v1.0.0-"+tt.b
			if got, ok := CompareTags(a, b); !ok || got != tt.want {
				t.Errorf("CompareTags(%q, %q) = (%d, %v), want (%d, true)", a, b, got, ok, tt.want)
			}
			if got, ok := CompareTags(b, a); !ok || got != -tt.want {
				t.Errorf("CompareTags(%q, %q) = (%d, %v), want (%d, true)", b, a, got, ok, -tt.want)
			}
		})
	}
}
