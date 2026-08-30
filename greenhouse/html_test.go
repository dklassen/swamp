package greenhouse

import "testing"

func TestStripHTML_SimpleParagraphs_NewlineSeparated(t *testing.T) {
	t.Parallel()

	got := stripHTML("<h1>About the role</h1><p>Join our security team.</p>")
	want := "About the role\nJoin our security team."
	if got != want {
		t.Fatalf("stripHTML() = %q, want %q", got, want)
	}
}

func TestStripHTML_NestedTags_TextConcatenatedWithinBlock(t *testing.T) {
	t.Parallel()

	got := stripHTML("<p>We need someone who knows <strong>Go</strong> and <em>Kubernetes</em>.</p>")
	want := "We need someone who knows Go and Kubernetes."
	if got != want {
		t.Fatalf("stripHTML() = %q, want %q", got, want)
	}
}

func TestStripHTML_List_OneItemPerLine(t *testing.T) {
	t.Parallel()

	got := stripHTML("<ul><li>Own the roadmap</li><li>Ship things</li></ul>")
	want := "Own the roadmap\nShip things"
	if got != want {
		t.Fatalf("stripHTML() = %q, want %q", got, want)
	}
}

func TestStripHTML_ScriptAndStyleContent_Dropped(t *testing.T) {
	t.Parallel()

	got := stripHTML("<p>Visible text.</p><script>alert('nope')</script><style>.x{color:red}</style>")
	want := "Visible text."
	if got != want {
		t.Fatalf("stripHTML() = %q, want %q (script/style content should never appear in plain text)", got, want)
	}
}

func TestStripHTML_HTMLEntities_Decoded(t *testing.T) {
	t.Parallel()

	got := stripHTML("<p>Engineering &amp; Product teams work closely together.</p>")
	want := "Engineering & Product teams work closely together."
	if got != want {
		t.Fatalf("stripHTML() = %q, want %q", got, want)
	}
}

func TestStripHTML_EmptyInput_ReturnsEmptyString(t *testing.T) {
	t.Parallel()

	if got := stripHTML(""); got != "" {
		t.Fatalf("stripHTML(\"\") = %q, want empty string", got)
	}
}

func TestStripHTML_PlainTextNoTags_ReturnedAsIs(t *testing.T) {
	t.Parallel()

	got := stripHTML("Just plain text, no markup at all.")
	want := "Just plain text, no markup at all."
	if got != want {
		t.Fatalf("stripHTML() = %q, want %q", got, want)
	}
}
