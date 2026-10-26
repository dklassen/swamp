---
title: "[RFC 0013 step 6] ErrMsg and BannerMsg become Error and Notify events; the banner routes them"
labels: [rfc-0013, rfc0013-step-6]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Decisions: Payloads (Error, How an error is carried, How an error reaches the user)", "Adopting: Already in tui/message", "Open questions: 7"]
depends_on: [1, 4]
---

RFC 0013, step 4 (the banner's messages). See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

`message.ErrMsg` and `message.BannerMsg` carry a Go `error`. `App.handle` (`tui/app.go:1008`) takes them before `Dispatch`, so nothing else can hear a failure. Later steps fold every result's `Err` field into `Event[Error]`, which needs the error codes, the domain-to-code mapping and a banner that hears `Error` first.

## Change

- `ErrorCode` constants for the codes the routed messages need: `not_found`, `sync_in_progress`, `other`.
- `errorOf(err) message.Error` outside `tui/message` (it can't import the errors it names), mapping with `errors.Is`: `store.ErrNotFound` to `not_found`, `sync.ErrSyncInProgress` to `sync_in_progress`, anything else to `other`.
- `message.Fail(src, err)` sends `Event[Error]` with `Code` `other`. `ReportErr` sends `Event[Error]`. `ReportStatus` sends `Event[Notify]`.
- `App` routes both events to the banner instead of switching on them in `handle`. The banner shows the line for a code it has text for. A code it has no text for (`other`, a fault) it logs to `swamp.log` and shows "internal error, see swamp.log", as `ErrMsg` does today.
- Remove `ErrMsg` and `BannerMsg`, and take them off the allow-list.

## Decide before starting

- **Open question 7: how a load's error stays up.** `ReportLoad` lets a failed load's error stay until a later load succeeds, since loads finish in any order. With `Event[Error]`, the banner hears only failures. The RFC's default: the banner clears its error on its next `Error` or `Notify`. That changes the behaviour of `setLoadErr`, which unconverted screens still use through `a.err`. Recommendation: take the default for routed messages only, leave `setLoadErr` for `App`'s old paths, and record it in the RFC.
- **What text the banner shows for `other`.** Today `ReportErr(err)` shows `err.Error()` for any error, for example a company name that's taken. If every unmapped error is `other` and logged, those messages stop reaching the user. Either every user-facing failure gets a code, or `other` shows `Text` and only `Fail` (a fault) logs. Recommendation: a separate `fault` code for `Fail`, and show `Text` for `other`.

## Acceptance criteria

- [ ] A failed save the user can act on still shows its message in the banner (existing banner tests).
- [ ] A fault from a mistyped route is logged and shows "internal error, see swamp.log" (`TestApp_ErrMsg_ShowsTheInternalErrorBanner`, renamed).
- [ ] `errorOf` table test: each mapped error, a wrapped one, and an unmapped one.
- [ ] The allow-list is down to 14.
- [ ] Open question 7's decision recorded in RFC 0013.
- [ ] Verification checklist clean.
