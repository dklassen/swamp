package message_test

import (
	"go/build"
	"strings"
	"testing"
)

func TestPackage_ImportsOnlyTheStandardLibraryAndBubbleTea(t *testing.T) {
	t.Parallel()

	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("ImportDir: %v", err)
	}
	for _, path := range pkg.Imports {
		// A standard library path has no dot in its first element.
		if !strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
			continue
		}
		if path == "charm.land/bubbletea/v2" {
			continue
		}
		t.Errorf("tui/message imports %q; events are plain data, so it may import only the standard library and Bubble Tea", path)
	}
}
