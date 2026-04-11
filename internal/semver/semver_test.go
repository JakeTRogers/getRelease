package semver

import "testing"

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
