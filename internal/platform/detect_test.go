package platform

import (
	"runtime"
	"testing"
)

func TestDetect(t *testing.T) {
	t.Parallel()
	info := Detect()
	if info.OS != runtime.GOOS {
		t.Errorf("Detect().OS = %q, want %q", info.OS, runtime.GOOS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("Detect().Arch = %q, want %q", info.Arch, runtime.GOARCH)
	}
}

func TestOSKeywords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		os       string
		wantLen  int
		contains string
	}{
		{os: "linux", wantLen: 1, contains: "linux"},
		{os: "darwin", wantLen: 5, contains: "darwin"},
		{os: "windows", wantLen: 2, contains: "windows"},
		{os: "freebsd", wantLen: 1, contains: "freebsd"},
	}
	for _, tt := range tests {
		t.Run(tt.os, func(t *testing.T) {
			t.Parallel()
			kw := OSKeywords(tt.os)
			if len(kw) != tt.wantLen {
				t.Errorf("OSKeywords(%q) len = %d, want %d", tt.os, len(kw), tt.wantLen)
			}
			found := false
			for _, k := range kw {
				if k == tt.contains {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("OSKeywords(%q) = %v, want to contain %q", tt.os, kw, tt.contains)
			}
		})
	}
}

func TestArchKeywords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		arch     string
		wantLen  int
		contains string
	}{
		{arch: "amd64", wantLen: 3, contains: "x86_64"},
		{arch: "arm64", wantLen: 2, contains: "aarch64"},
		{arch: "386", wantLen: 3, contains: "i386"},
		{arch: "mips", wantLen: 1, contains: "mips"},
	}
	for _, tt := range tests {
		t.Run(tt.arch, func(t *testing.T) {
			t.Parallel()
			kw := ArchKeywords(tt.arch)
			if len(kw) != tt.wantLen {
				t.Errorf("ArchKeywords(%q) len = %d, want %d", tt.arch, len(kw), tt.wantLen)
			}
			found := false
			for _, k := range kw {
				if k == tt.contains {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("ArchKeywords(%q) = %v, want to contain %q", tt.arch, kw, tt.contains)
			}
		})
	}
}
