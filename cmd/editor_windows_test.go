package cmd

import (
	"reflect"
	"strings"
	"testing"
)

func TestEditorCommandWindows(t *testing.T) {
	t.Parallel()

	target := `C:\Users\Jake Rogers\config & history\config.yaml`
	tests := []struct {
		name   string
		editor string
		want   []string
	}{
		{
			name:   "quoted executable path",
			editor: `"C:\Program Files\Microsoft VS Code\Code.exe" --wait`,
			want:   []string{`C:\Program Files\Microsoft VS Code\Code.exe`, "--wait", target},
		},
		{
			name:   "quoted argument",
			editor: `notepad.exe --profile "my profile"`,
			want:   []string{"notepad.exe", "--profile", "my profile", target},
		},
		{
			name:   "escaped quote and trailing backslash",
			editor: `notepad.exe "say \"hello\"" "C:\my profile\\"`,
			want:   []string{"notepad.exe", `say "hello"`, `C:\my profile\`, target},
		},
		{
			name:   "empty argument",
			editor: `notepad.exe ""`,
			want:   []string{"notepad.exe", "", target},
		},
		{
			name:   "bare executable",
			editor: "notepad.exe",
			want:   []string{"notepad.exe", target},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			command, err := editorCommand(tt.editor, target)
			if err != nil {
				t.Fatalf("editorCommand() error: %v", err)
			}
			if !reflect.DeepEqual(command.Args, tt.want) {
				t.Errorf("editorCommand() args = %q, want %q", command.Args, tt.want)
			}
		})
	}
}

func TestEditorCommandWindowsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		editor  string
		wantErr string
	}{
		{name: "empty executable", editor: `"" --wait`, wantErr: "no executable"},
		{name: "empty command", wantErr: "no executable"},
		{name: "embedded NUL", editor: "notepad.exe\x00 --wait", wantErr: "parsing editor command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			command, err := editorCommand(tt.editor, "config.yaml")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("editorCommand() error = %v, want %q", err, tt.wantErr)
			}
			if command != nil {
				t.Errorf("editorCommand() = %v, want nil for an invalid command", command)
			}
		})
	}
}
