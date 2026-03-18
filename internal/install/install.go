// Package install provides binary installation via configurable commands.
package install

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"log/slog"
)

// Installer installs a binary from source to target.
type Installer interface {
	Install(source, target string) error
}

// CommandInstaller executes a configured command template to perform installation.
// The template may include the placeholders {source} and {target} which will be
// substituted with the validated, absolute paths.
type CommandInstaller struct {
	// Command is the command template to run, for example:
	//   "sudo install -m 755 {source} {target}"
	Command string
}

// Install runs the configured command with {source} and {target} substituted.
// It performs strict path validation and uses os/exec without shell interpretation.
func (ci *CommandInstaller) Install(source, target string) error {
	slog.Debug("CommandInstaller.Install starting", "command", ci.Command, "source", source, "target", target)
	if strings.TrimSpace(ci.Command) == "" {
		return fmt.Errorf("command installer: no command configured")
	}

	src, err := validatePath(source)
	if err != nil {
		return fmt.Errorf("invalid source: %w", err)
	}
	tgt, err := validatePath(target)
	if err != nil {
		return fmt.Errorf("invalid target: %w", err)
	}

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("source is a directory")
	}

	tokens := strings.Fields(ci.Command)
	if len(tokens) == 0 {
		return fmt.Errorf("command installer: empty command template")
	}

	for i, tok := range tokens {
		tok = strings.ReplaceAll(tok, "{source}", src)
		tok = strings.ReplaceAll(tok, "{target}", tgt)
		tokens[i] = tok
	}

	slog.Debug("CommandInstaller executing", "argv", strings.Join(tokens, " "))
	cmd := exec.Command(tokens[0], tokens[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		return fmt.Errorf("execute command %q: %w (output: %s)", strings.Join(tokens, " "), err, outStr)
	}

	slog.Debug("CommandInstaller completed", "output", strings.TrimSpace(string(out)))
	return nil
}

// CopyInstaller copies the file from source to target and sets mode 0755.
// This can be used as a fallback when a command-based installer is not desired.
type CopyInstaller struct{}

// Install copies the source file to target and ensures executable mode.
func (ci *CopyInstaller) Install(source, target string) error {
	slog.Debug("CopyInstaller.Install starting", "source", source, "target", target)

	src, err := validatePath(source)
	if err != nil {
		return fmt.Errorf("invalid source: %w", err)
	}
	tgt, err := validatePath(target)
	if err != nil {
		return fmt.Errorf("invalid target: %w", err)
	}

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("source is a directory")
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() {
		if cerr := in.Close(); cerr != nil {
			slog.Warn("closing source file failed", "source", src, "err", cerr)
		}
	}()

	out, err := os.OpenFile(tgt, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create target: %w", err)
	}
	defer func() {
		if cerr := out.Close(); cerr != nil {
			slog.Warn("closing target file failed", "target", tgt, "err", cerr)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}

	if err := os.Chmod(tgt, 0o755); err != nil {
		return fmt.Errorf("chmod target: %w", err)
	}

	slog.Debug("CopyInstaller completed", "target", tgt)
	return nil
}

// NewInstaller returns an Installer. If command is empty it returns a CopyInstaller,
// otherwise it returns a CommandInstaller configured with the provided template.
func NewInstaller(command string) Installer {
	if strings.TrimSpace(command) == "" {
		return &CopyInstaller{}
	}
	return &CommandInstaller{Command: command}
}

// validatePath ensures the provided path is absolute, contains no control
// characters or null bytes, and does not contain any ".." elements after
// cleaning. It returns the cleaned path on success.
func validatePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.IndexByte(p, 0) != -1 {
		return "", fmt.Errorf("path contains null byte")
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("path contains control character")
		}
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("path must be absolute")
	}
	clean := filepath.Clean(p)
	parts := strings.Split(clean, string(os.PathSeparator))
	for _, part := range parts {
		if part == ".." {
			return "", fmt.Errorf("path must not contain .. elements")
		}
	}
	return clean, nil
}
