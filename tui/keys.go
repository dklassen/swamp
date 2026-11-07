package tui

import "charm.land/bubbles/v2/key"

// sharedKeyMap is the bindings more than one screen shares; a screen's own
// letters are in its own keymap.
type sharedKeyMap struct {
	Up, Down, Next, Previous key.Binding
	// Left and Right leave h and l free to type.
	Left, Right          key.Binding
	SearchUp, SearchDown key.Binding
	Search, Backspace    key.Binding
	Back, Esc, Quit      key.Binding
	ForceQuit            key.Binding
	Enter, Tab, Save     key.Binding
	Yes, No              key.Binding
}

var keys = sharedKeyMap{
	Up:         key.NewBinding(key.WithKeys("up", "k")),
	Down:       key.NewBinding(key.WithKeys("down", "j")),
	Next:       key.NewBinding(key.WithKeys("right", "l")),
	Previous:   key.NewBinding(key.WithKeys("left", "h")),
	Left:       key.NewBinding(key.WithKeys("left")),
	Right:      key.NewBinding(key.WithKeys("right")),
	SearchUp:   key.NewBinding(key.WithKeys("up", "ctrl+p")),
	SearchDown: key.NewBinding(key.WithKeys("down", "ctrl+n")),
	Search:     key.NewBinding(key.WithKeys("/")),
	Backspace:  key.NewBinding(key.WithKeys("backspace")),
	Back:       key.NewBinding(key.WithKeys("esc", "b")),
	Esc:        key.NewBinding(key.WithKeys("esc")),
	Quit:       key.NewBinding(key.WithKeys("q")),
	ForceQuit:  key.NewBinding(key.WithKeys("ctrl+c")),
	Enter:      key.NewBinding(key.WithKeys("enter")),
	Tab:        key.NewBinding(key.WithKeys("tab")),
	Save:       key.NewBinding(key.WithKeys("ctrl+s")),
	Yes:        key.NewBinding(key.WithKeys("y")),
	No:         key.NewBinding(key.WithKeys("n", "esc")),
}

// listingKeyMap acts on the posting under the cursor of a posting list.
type listingKeyMap struct {
	Sort, Interested, Archive, Open key.Binding
}

var listingKeys = listingKeyMap{
	Sort:       key.NewBinding(key.WithKeys("s")),
	Interested: key.NewBinding(key.WithKeys("i")),
	Archive:    key.NewBinding(key.WithKeys("x")),
	Open:       key.NewBinding(key.WithKeys("o")),
}
