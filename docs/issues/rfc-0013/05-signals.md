---
title: "[RFC 0013 step 5] ContentSizeMsg and FocusChangedMsg become Signal events"
labels: [rfc-0013, rfc0013-step-5]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: [Primitives, "Adopting: Already in tui/message", Template]
depends_on: [1, 4]
---

RFC 0013, step 4 (the size and focus messages). See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

`message.FocusChangedMsg{Focused tea.Model}` puts a model inside a message, so it can't be serialised and breaks the plain-data rule. Every routed model registers it and `message.ContentSizeMsg`: the company list and delete confirmation's `resize` and `focusChanged`.

## Change

- `ContentSize` and `FocusChanged{Focused ScreenInstance}` bodies with `TypeContentSize` and `TypeFocusChanged`.
- `App` dispatches `Event[Signal[ContentSize]]` where it sends `ContentSizeMsg` today, and `Event[Signal[FocusChanged]]` with the focused model's instance from step 4 (`tui/app.go:1048`, `:1052`). It still compares sizes to decide whether to resend: compare the body, not the event, since each event has its own `ID`.
- The company list's and delete confirmation's `resize` and `focusChanged` take the events. `focusChanged` compares the instance with its own.
- Remove `ContentSizeMsg` and `FocusChangedMsg`.

## Acceptance criteria

- [ ] `TestApp_Screens_FitTheTerminalUnderTheBanner` and the focus tests pass unchanged in what they assert.
- [ ] The size is resent only when the width or rows change, not on every message (test: two messages with the banner unchanged dispatch one size).
- [ ] Verification checklist clean.
