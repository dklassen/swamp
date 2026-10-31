---
title: "[App simplification 2] A screen owns its delete confirmation"
labels: [app-simplification]
depends_on: []
---

## Problem

A delete confirmation is pushed onto the stack like any screen, so `App` can't tell a screen from a box drawn over one. It undoes that by recognising screen kinds: `App.View` and `App.beneath` each list `ScreenCompanyDelete` and `ScreenApplicationDelete`, to draw the screen beneath and keep its header. A new confirmation missing from either list is drawn full screen, or under the wrong header.

Being a separate screen costs elsewhere too. Application detail can't leave when its application is deleted from its own confirmation, because the confirmation is on top. So it sets `deleted`, and pops itself when focus comes back after the confirmation's pop: two pops and a flag for one delete.

## Change

The screen that opens a confirmation owns it, as a field, and draws it over itself. A confirmation is no longer a screen: not on the stack, not registered with the router, not in `App`'s nav handlers or step 4's table. `App` loses the overlay check in `View`, `beneath`, and both kinds from `message.Screen`.

| Confirmation | Owned by |
|---|---|
| `companyDeleteModel` | company list |
| `applicationDeleteModel` | active applications, application detail |

Before, `App` draws whichever screen is beneath the box:

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

// company list
case "d":
	return message.Push(src, message.ScreenCompanyDelete, about, message.CompanyRow{…})
```

After, `App` draws the top screen, and the screen draws its own box:

```go
func (a *App) View() tea.View {
	body := a.screenOf(a.onScreen()).View().Content
	…
}

type companyListModel struct {
	…
	// confirm is the delete being confirmed, or nil.
	confirm *companyDeleteModel
}

func (m *companyListModel) keyPressed(key tea.KeyPressMsg) tea.Cmd {
	if m.confirm != nil {
		return m.confirmKey(key)
	}
	switch key.String() {
	case "d":
		m.confirm = newCompanyDeleteModel(m.companies, current, m.openPostings[current.ID])
		return nil
	…
	}
}

func (m *companyListModel) View() tea.View {
	body := m.table()
	if m.confirm != nil {
		body = overlay(body, m.confirm.View(m.width), m.width)
	}
	return tea.NewView(body)
}
```

The confirmation keeps its text, its `y` that deletes once, and its own instance as the delete's source. It drops `Routes`, `Focused`, `focusChanged`, `resize`, `Init` and `Update`: it gets keys and width from its owner and is told the result. Its key handler says whether to close rather than returning `Pop`.

### What the owners already hear

Checked against the code:

- **The delete.** The company list's `companyChanged` and active applications' `applicationChanged` hear every write. Application detail's `applicationChanged` hears writes to its own application. Each closes its confirmation on the delete of the confirmation's subject. The confirmation still decides what the banner says ("Deleted the application for …" when it made the delete, "… was deleted elsewhere" when it didn't), so the owner asks it.
- **The error.** Not heard today. Each owner's `readFailed` is `From` its own instance, and the confirmation sends its delete from its own. The owner adds a second `Event[Error]` route (the router allows more than one per owner) and acts only while `confirm` is set and the error is from `confirm`'s instance. An application `ErrNotFound` there means deleted elsewhere, as today.
- **The width.** The company list and active applications hear `ContentSize` already. Application detail doesn't; it passes its own width or starts hearing it.

Application detail's own delete becomes one step: on its application's delete from its confirmation, it closes the confirmation and pops itself. Its focus-gained pop goes. `deleted` stays for a load that comes back `ErrNotFound` after the delete.

## Decide before starting

- **Keys typed just after closing.** Today `n`, `esc` or `enter` return a `Pop`, so a second key typed before it lands still goes to the confirmation and is dropped. With the owner closing it at once, the second key reaches the owner. `n` is bound on none of the three, but `enter` opens the company or application, and `esc` clears a search or, on application detail, leaves. Accept this (it's what a key after any close does), or have the confirmation's close come back as a message the owner applies.
- **Pastes and other focused routes.** The company list's and active applications' `paste` would type into the search under the box. Each `WhenFocused` route on an owner checks `confirm` first.
- **Glue in three owners.** The `confirm` field, key forwarding, drawing and closing happen in three places, twice for the application confirmation. Three is enough to share a small helper; decide once the second one is written.

## Rejected

- **A list of overlay kinds in `tui`, or a `Dialog()` marker method (RFC 0012 section 8).** Either says once which screens are overlays, but keeps the confirmations on the stack, keeps `beneath`, and keeps application detail's two-pop delete.
- **A dialog slot on `App`.** `App` could tell screens from dialogs by where they are, but it needs a new `Nav` op for opening one, plus a dialog case in focus, `onTop` and pop.

## Acceptance criteria

- [ ] `App` has no overlay check and no `beneath`; `ScreenCompanyDelete` and `ScreenApplicationDelete` are gone.
- [ ] Neither confirmation is a `Routable`.
- [ ] Each owner's tests cover: `d` (or `D`) opens it, `n`/`esc`/`enter` close it, `y` deletes once, a delete elsewhere closes it with its status, a failed delete closes it, and a paste with it open changes nothing.
- [ ] Deleting from application detail returns to the screen beneath it, with "Deleted the application for …".
- [ ] The App-level delete tests (`TestApp_CompanyDeleteConfirmation_*` and the like) move to the owners or go.
- [ ] RFC 0012 open question 2 and section 8's `Dialog` updated: a confirmation belongs to the screen that opens it.
- [ ] Manual check against real data: deleting a company, and an application from both owners.
- [ ] Verification checklist clean.
