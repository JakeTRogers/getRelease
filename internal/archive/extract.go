// Package archive handles extraction of release archives and binary detection.
package archive

import (
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsArchive returns true when filename has an extension Extract can unpack:
// a tar or zip archive, or a single compressed file such as "tool.gz".
func IsArchive(filename string) bool {
	l := strings.ToLower(filename)
	return isTarOrZip(l) || compressedFileExt(l) != ""
}

func isTarOrZip(lower string) bool {
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz") ||
		strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2") ||
		strings.HasSuffix(lower, ".tar") || strings.HasSuffix(lower, ".zip")
}

// compressedFileExt returns the compression extension of a single compressed
// file (not a tarball), or "" when lower is not one.
func compressedFileExt(lower string) string {
	if isTarOrZip(lower) {
		return ""
	}
	for _, ext := range []string{".gz", ".bz2", ".xz"} {
		if strings.HasSuffix(lower, ext) {
			return ext
		}
	}
	return ""
}

// Extract extracts the given archive into destDir.
// Supports common tar formats (using system tar), zip (using archive/zip),
// and single compressed files, which are decompressed into destDir under
// their name without the compression extension.
func Extract(archivePath, destDir string) error {
	slog.Debug("extract: start", "archive", archivePath, "dest", destDir)

	lower := strings.ToLower(archivePath)

	// Ensure destination directory exists.
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create destination %s: %w", destDir, err)
	}

	if ext := compressedFileExt(lower); ext != "" {
		base := filepath.Base(archivePath)
		dest := filepath.Join(destDir, base[:len(base)-len(ext)])
		slog.Debug("extract: decompressing single file", "archive", archivePath, "dest", dest)
		if err := decompressFile(archivePath, dest, ext); err != nil {
			return fmt.Errorf("decompress %s: %w", archivePath, err)
		}
		return nil
	}

	// ZIP handled by stdlib
	if strings.HasSuffix(lower, ".zip") {
		slog.Debug("extract: using zip stdlib", "archive", archivePath)
		if err := extractZip(archivePath, destDir); err != nil {
			return fmt.Errorf("extract zip %s: %w", archivePath, err)
		}
		return nil
	}

	// Determine tar flags by extension.
	var flags string
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		flags = "-xzf"
	case strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz"):
		flags = "-xJf"
	case strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2"):
		flags = "-xjf"
	case strings.HasSuffix(lower, ".tar"):
		flags = "-xf"
	default:
		return fmt.Errorf("unsupported archive format: %s", archivePath)
	}

	slog.Debug("extract: using system tar", "flags", flags, "archive", archivePath)

	cmd := exec.Command("tar", flags, archivePath, "-C", destDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar extraction failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// decompressFile decompresses the single compressed file src into dest,
// marking it executable since only selected release assets are decompressed.
func decompressFile(src, dest, ext string) (err error) {
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create file %s: %w", dest, err)
	}
	defer func() {
		if closeErr := out.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close file %s: %w", dest, closeErr))
		}
	}()

	if ext == ".xz" {
		// The standard library has no xz decoder; use the xz tool, which
		// .tar.xz extraction already relies on through tar.
		var stderr bytes.Buffer
		cmd := exec.Command("xz", "--decompress", "--stdout", src)
		cmd.Stdout = out
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("xz decompression failed: %s: %w", strings.TrimSpace(stderr.String()), err)
		}
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() {
		if closeErr := in.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close %s: %w", src, closeErr))
		}
	}()

	var r io.Reader
	switch ext {
	case ".gz":
		gz, err := gzip.NewReader(in)
		if err != nil {
			return fmt.Errorf("open gzip stream: %w", err)
		}
		defer func() {
			if closeErr := gz.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close gzip stream: %w", closeErr))
			}
		}()
		r = gz
	case ".bz2":
		r = bzip2.NewReader(in)
	default:
		return fmt.Errorf("unsupported compression: %s", ext)
	}

	if _, err := io.Copy(out, r); err != nil {
		return fmt.Errorf("write file %s: %w", dest, err)
	}
	return nil
}

// extractZip extracts a zip archive into destDir with zip-slip protection.
func extractZip(zipPath, destDir string) (err error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", zipPath, err)
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close zip reader %s: %w", zipPath, closeErr))
		}
	}()

	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolve destination %s: %w", destDir, err)
	}
	destAbs = filepath.Clean(destAbs)

	for _, f := range r.File {
		if err := extractZipFile(f, destAbs); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, destAbs string) (err error) {
	name := f.Name

	// Reject absolute paths
	if filepath.IsAbs(name) {
		return fmt.Errorf("zip contains absolute path: %s", name)
	}

	// Clean the entry name and reject any parent traversal
	// Note: filepath.Clean preserves leading .. segments, so verify output path.
	if strings.Contains(name, "..") {
		return fmt.Errorf("zip contains invalid path: %s", name)
	}

	// Construct destination path and verify it stays inside destDir
	// Use FromSlash to support zip entries with forward slashes.
	outPath := filepath.Join(destAbs, filepath.FromSlash(name))
	outPath = filepath.Clean(outPath)

	// Ensure outPath is within destAbs
	prefix := destAbs
	if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
		prefix = prefix + string(os.PathSeparator)
	}
	if outPath != destAbs && !strings.HasPrefix(outPath, prefix) {
		return fmt.Errorf("zip entry would extract outside destination: %s", name)
	}

	if f.FileInfo().IsDir() {
		if err := os.MkdirAll(outPath, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", outPath, err)
		}
		return nil
	}

	// Make sure parent directory exists
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", filepath.Dir(outPath), err)
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zipped file %s: %w", name, err)
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close zipped file %s: %w", name, closeErr))
		}
	}()

	// Create destination file with mode from zip
	outFile, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return fmt.Errorf("create file %s: %w", outPath, err)
	}
	defer func() {
		if closeErr := outFile.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close file %s: %w", outPath, closeErr))
		}
	}()

	if _, err := io.Copy(outFile, rc); err != nil {
		return fmt.Errorf("write file %s: %w", outPath, err)
	}

	// Preserve mode bits
	if err := os.Chmod(outPath, f.Mode()); err != nil {
		return fmt.Errorf("chmod %s: %w", outPath, err)
	}
	return nil
}

// FindBinaries walks dir and returns paths (relative to dir) that look like executables.
// It skips common documentation/config files and detects binaries by magic bytes or exec bit.
func FindBinaries(dir string) ([]string, error) {
	var bins []string

	skipExts := map[string]struct{}{
		".md":   {},
		".txt":  {},
		".rst":  {},
		".html": {},
		".json": {},
		".yaml": {},
		".yml":  {},
		".toml": {},
		".ini":  {},
		".cfg":  {},
		".conf": {},
		".css":  {},
	}

	skipPrefixes := []string{"license", "readme", "changelog", "authors", "contributing", "notice"}

	slog.Debug("find binaries: start", "dir", dir)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip directories and symlinks
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		name := d.Name()
		lname := strings.ToLower(name)

		// Skip known prefixes
		for _, p := range skipPrefixes {
			if strings.HasPrefix(lname, p) {
				return nil
			}
		}

		// Skip known text/config extensions
		if ext := strings.ToLower(filepath.Ext(name)); ext != "" {
			if _, ok := skipExts[ext]; ok {
				return nil
			}
		}

		// Read file header for magic bytes
		isBin, err := LooksExecutable(path)
		if err != nil {
			// Best effort: skip files we can't open
			return nil
		}

		if !isBin {
			// Fallback: executable permission bit
			info, err := d.Info()
			if err == nil {
				if info.Mode()&0o111 != 0 {
					isBin = true
				}
			}
		}

		if isBin {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return fmt.Errorf("rel path %s: %w", path, err)
			}
			bins = append(bins, rel)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	return bins, nil
}

// executableMagics are file headers of native executables and scripts:
// ELF, Mach-O (32/64-bit in both byte orders, and universal), PE, and "#!".
var executableMagics = [][]byte{
	{0x7f, 'E', 'L', 'F'},
	{0xfe, 0xed, 0xfa, 0xce},
	{0xfe, 0xed, 0xfa, 0xcf},
	{0xce, 0xfa, 0xed, 0xfe},
	{0xcf, 0xfa, 0xed, 0xfe},
	{0xca, 0xfe, 0xba, 0xbe},
	{'M', 'Z'},
	{'#', '!'},
}

// LooksExecutable reports whether the file at path starts with the header of
// a native executable or a script.
func LooksExecutable(path string) (bool, error) {
	buf := make([]byte, 4)
	n, err := readFileHeader(path, buf)
	if err != nil {
		return false, err
	}
	for _, magic := range executableMagics {
		if bytes.HasPrefix(buf[:n], magic) {
			return true, nil
		}
	}
	return false, nil
}

func readFileHeader(path string, buf []byte) (n int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Warn("closing file header reader failed", "path", path, "err", closeErr)
		}
	}()

	n, err = f.Read(buf)
	if errors.Is(err, io.EOF) {
		return n, nil
	}
	return n, err
}
