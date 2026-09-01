package tui

import "testing"

func TestEditorCommand_EditorSet_ReturnsEditorAndPath(t *testing.T) {
	cmd, args, err := editorCommand("vim", "/tmp/cover_letter.md")
	if err != nil {
		t.Fatalf("editorCommand: %v", err)
	}
	if cmd != "vim" {
		t.Fatalf("cmd = %q, want %q", cmd, "vim")
	}
	if len(args) != 1 || args[0] != "/tmp/cover_letter.md" {
		t.Fatalf("args = %v, want [/tmp/cover_letter.md]", args)
	}
}

func TestEditorCommand_EditorUnset_ReturnsError(t *testing.T) {
	_, _, err := editorCommand("", "/tmp/cover_letter.md")
	if err == nil {
		t.Fatal("editorCommand: expected error when $EDITOR is unset, got nil")
	}
}
