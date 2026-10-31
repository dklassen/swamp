---
title: "[App simplification 2] Which screens are overlays is said once"
labels: [app-simplification]
depends_on: []
---

## Problem

`App.View` and `App.beneath` each list `ScreenCompanyDelete` and `ScreenApplicationDelete` as the screens drawn over the one below. A new confirmation added to one list but not the other would get either the overlay or the header of the screen beneath it, but not both.

RFC 0012 section 8 names a `Dialog` interface for this. It was never added.

## Change

`View` reads `beneath` rather than checking for overlays itself, so `beneath` is the only place that knows which screens are overlays.

Before, the two screens are listed in each:

```go
func (a *App) View() tea.View {
	path := a.path()
	top := path[len(path)-1]
	body := a.screenOf(top).View().Content
	if top == message.ScreenCompanyDelete || top == message.ScreenApplicationDelete {
		body = overlay(a.screenOf(path[len(path)-2]).View().Content, body, a.width)
	}
	…
}

func (a *App) beneath() []message.Screen {
	path := a.path()
	switch path[len(path)-1] {
	case message.ScreenCompanyDelete, message.ScreenApplicationDelete:
		return path[:len(path)-1]
	}
	return path
}
```

After, `View` draws the screen at the end of `beneath`'s path, and boxes the top over it when the two paths differ:

```go
func (a *App) View() tea.View {
	path, under := a.path(), a.beneath()
	body := a.screenOf(under[len(under)-1]).View().Content
	if len(under) < len(path) {
		body = overlay(body, a.screenOf(path[len(path)-1]).View().Content, a.width)
	}
	…
}
```

`beneath` then asks whether the top screen is an overlay in one of two ways.

A list in `tui`:

```go
// overlays are drawn as a box over the screen beneath them, and keep its header.
var overlays = []message.Screen{message.ScreenCompanyDelete, message.ScreenApplicationDelete}

func (a *App) beneath() []message.Screen {
	path := a.path()
	if slices.Contains(overlays, path[len(path)-1]) {
		return path[:len(path)-1]
	}
	return path
}
```

A method the screen reports (RFC 0012's `Dialog`):

```go
// tui/model
// Dialog is a screen drawn as a box over the screen beneath it.
type Dialog interface {
	Dialog()
}

// tui
func (m *companyDeleteModel) Dialog()     {}
func (m *applicationDeleteModel) Dialog() {}

func (a *App) beneath() []message.Screen {
	path := a.path()
	if _, ok := a.screenOf(path[len(path)-1]).(model.Dialog); ok {
		return path[:len(path)-1]
	}
	return path
}
```

## Decide before starting

- **A list or a method.** The list is less code while there are two overlays, and keeps the question in `App`, beside the header that uses it. The method follows RFC 0012 (open question 2: overlays are stacked screens that report they are one), and a new confirmation can't be forgotten in a list. But it's a marker interface checked by type assertion, which is earned only once a third overlay arrives or a screen needs to know it is one.

## Acceptance criteria

- [ ] The overlay screens are named in one place, read only by `beneath`; `View` uses `beneath`.
- [ ] Existing overlay and header tests pass unchanged.
- [ ] RFC 0012 updated if the `Dialog` decision changes it.
- [ ] Verification checklist clean.
