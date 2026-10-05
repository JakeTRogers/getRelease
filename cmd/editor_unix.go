//go:build !windows

package cmd

import "os/exec"

func editorCommand(editor, path string) (*exec.Cmd, error) {
	return exec.Command("sh", "-c", editor+` "$@"`, editor, path), nil
}
