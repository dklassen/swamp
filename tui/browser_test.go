package tui

import "testing"

func TestBrowserCommand(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		wantCmd string
		wantArg []string
		wantErr bool
	}{
		{
			name:    "darwin uses open",
			goos:    "darwin",
			wantCmd: "open",
			wantArg: []string{"https://example.com"},
		},
		{
			name:    "linux uses xdg-open",
			goos:    "linux",
			wantCmd: "xdg-open",
			wantArg: []string{"https://example.com"},
		},
		{
			name:    "windows uses rundll32",
			goos:    "windows",
			wantCmd: "rundll32",
			wantArg: []string{"url.dll,FileProtocolHandler", "https://example.com"},
		},
		{
			name:    "unsupported OS returns error",
			goos:    "plan9",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args, err := browserCommand(tt.goos, "https://example.com")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("browserCommand(%q, ...): expected error, got nil", tt.goos)
				}
				return
			}
			if err != nil {
				t.Fatalf("browserCommand(%q, ...): %v", tt.goos, err)
			}
			if cmd != tt.wantCmd {
				t.Fatalf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if len(args) != len(tt.wantArg) {
				t.Fatalf("args = %v, want %v", args, tt.wantArg)
			}
			for i, want := range tt.wantArg {
				if args[i] != want {
					t.Fatalf("args = %v, want %v", args, tt.wantArg)
				}
			}
		})
	}
}
