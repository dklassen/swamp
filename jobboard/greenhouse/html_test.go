package greenhouse

import "testing"

func TestStripHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "simple paragraphs newline separated",
			in:   "<h1>About the role</h1><p>Join our security team.</p>",
			want: "About the role\nJoin our security team.",
		},
		{
			name: "nested tags text concatenated within block",
			in:   "<p>We need someone who knows <strong>Go</strong> and <em>Kubernetes</em>.</p>",
			want: "We need someone who knows Go and Kubernetes.",
		},
		{
			name: "list one item per line",
			in:   "<ul><li>Own the roadmap</li><li>Ship things</li></ul>",
			want: "Own the roadmap\nShip things",
		},
		{
			name: "script and style content dropped",
			in:   "<p>Visible text.</p><script>alert('nope')</script><style>.x{color:red}</style>",
			want: "Visible text.",
		},
		{
			name: "HTML entities decoded",
			in:   "<p>Engineering &amp; Product teams work closely together.</p>",
			want: "Engineering & Product teams work closely together.",
		},
		{
			name: "empty input returns empty string",
			in:   "",
			want: "",
		},
		{
			name: "plain text no tags returned as is",
			in:   "Just plain text, no markup at all.",
			want: "Just plain text, no markup at all.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := stripHTML(tt.in); got != tt.want {
				t.Fatalf("stripHTML(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
