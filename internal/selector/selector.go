// Package selector provides interactive terminal prompts for user selection.
package selector

import (
	"errors"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
)

// ErrNotInteractive is returned when stdin is not a TTY.
var ErrNotInteractive = errors.New("not running in an interactive terminal")

// ErrCancelled is returned when the user aborts an interactive prompt.
var ErrCancelled = errors.New("user cancelled")

func checkTTY() error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return ErrNotInteractive
	}
	return nil
}

func mapErr(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}
	return err
}

// Select presents an interactive select list and returns the selected index (0-based).
func Select(items []string, prompt string) (int, error) {
	if err := checkTTY(); err != nil {
		return -1, err
	}

	opts := make([]huh.Option[int], 0, len(items))
	for i, it := range items {
		opts = append(opts, huh.NewOption(it, i))
	}

	var selected int
	err := huh.NewSelect[int]().
		Title(prompt).
		Options(opts...).
		Value(&selected).
		Run()
	if err != nil {
		return -1, mapErr(err)
	}
	return selected, nil
}

// Confirm displays a Y/n confirmation prompt and returns the chosen value.
func Confirm(prompt string, defaultYes bool) (bool, error) {
	if err := checkTTY(); err != nil {
		return false, err
	}

	result := defaultYes
	err := huh.NewConfirm().
		Title(prompt).
		Affirmative("Yes").
		Negative("No").
		Value(&result).
		Run()
	if err != nil {
		return false, mapErr(err)
	}
	return result, nil
}
