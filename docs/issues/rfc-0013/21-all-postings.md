---
title: "[RFC 0013 step 21] All postings registers its routes and owns its search"
labels: [rfc-0013, rfc0013-step-21]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui"]
depends_on: [17, 19]
---

RFC 0013, step 5 (the all-postings tab).

## Problem

All postings is a value field on `App`. Its search is debounced with `allPostingsSearchMsg`, its results arrive as `allPostingsLoadedMsg`, opening a posting loads it first (`allPostingsDetailLoadedMsg`), and `o` asks `App` to open the browser (`openJobURLMsg`). `App` reloads it on a markup write.

## Change

- A pointer `Routable`, registered in `New` with the tabs. Keys and pastes are `WhenFocused`.
- The debounce is `Signal[SearchDebounce]` filtered `From` itself; results are `Read[PostingSearch]`.
- Enter pushes posting detail (step 19) with the posting it already has, so `allPostingsDetailLoadedMsg` goes unless detail needs a field the search row lacks.
- `o` opens the browser itself and gets `Ran[OpenBrowser]` or `Error`. Remove `openJobURLMsg` and `browserOpenedMsg`.
- It routes `Changed[Markup]` and reloads; `App`'s reload goes.

## Acceptance criteria

- [ ] Typing a search shows results once the typing stops; a late result for an earlier query is dropped.
- [ ] Archiving a posting from detail updates all postings.
- [ ] Manual check against real data, including `o`.
- [ ] Verification checklist clean.
