package cmd

import (
	"errors"
	"fmt"
	"os/exec"

	"golang.org/x/sys/windows"
)

func editorCommand(editor, path string) (*exec.Cmd, error) {
	args, err := windows.DecomposeCommandLine(editor)
	if err != nil {
		return nil, fmt.Errorf("parsing editor command: %w", err)
	}
	if len(args) == 0 || args[0] == "" {
		return nil, errors.New("editor command has no executable")
	}
	return exec.Command(args[0], append(args[1:], path)...), nil
}
