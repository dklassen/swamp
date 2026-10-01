# RFC 0004: Making a new document type a one-line change

- **Status:** Implemented 2026-10-01 (#180: PRs #194, #195, #196 and the step 4 PR; see "What shipped")
- **Date:** 2026-09-28
- **Related:** RFC 0002 (phase 2 proposes an `answers.md` document); `documents/documents.go` package comment; `store/document_review.go`; `decisions.log` 2026-09-04 (#94 follow-up, line ~3668) and 2026-09-17 (#45 review pass, line ~4115)

## Summary

Swamp has exactly two document types, cover letter and resume. The code assumes that in about 20 places across 8 packages and the agent skill. RFC 0002 proposes a third, `answers.md`, for application-form questions. Adding it today means finding every one of those places by hand, and almost every place that's missed **does the wrong thing silently** instead of failing: it treats the new document as the cover letter.

This RFC proposes making the set of document types a single list, so that adding a type is one entry plus its filename and label, and a test fails if anything is missing. The main design choice is where `DocumentType` lives. We recommend moving it from `store` into `documents`, rather than making `documents` import `store` (the earlier draft's proposal).

An earlier draft of this RFC framed the problem as duplicated code (6 copies of one block, 3 of one loop). That's true, but it's the smaller half. The draft also said its fix would make missed cases fail at build time, and described `store` as a "pure enum-and-struct package". Neither is true; see "Corrections to the earlier draft".

## Problem

### A missed site doesn't fail; it uses the cover letter

The common block picks a document like this:

```go
doc := status.CoverLetter
if documentType == store.DocumentTypeResume {
    doc = status.Resume
}
```

Any type that isn't the resume falls through to the cover letter. With an `answers` type added and this block left alone:

- the "current reviews" check hashes `cover_letter.md` against the answers review, gets a mismatch, and **drops the review**, so a flagged answers document shows as "not reviewed";
- export writes the cover letter out a second time under the answers name;
- the TUI can't start a review of it at all, since the `L`/`R` keys and the review picker offer only the two existing types.

None of these produce an error. `mcpserver.documentPath` (`mcpserver/mcpserver.go:172`) is the one site with a `default:` error.

### Every place that assumes two types

Measured on 2026-09-29, non-test code:

| Kind | Where | Fails how if missed |
|---|---|---|
| "Cover letter unless resume" dispatch | `stage/stage.go:84`, `tui/app.go:619`, `tui/application_detail.go:88`, `tui/application_export.go:112`, `cmd/swamp/main.go:352`, `:383` | Silently uses the cover letter |
| Same, as a switch with an error | `mcpserver/mcpserver.go:172` (`documentPath`) | Tool error |
| Same, on a `bool` | `tui/application_detail.go:72` (`openDocument(resume bool)`) | Can't express a third type |
| Hard-coded list of the two types | `cmd/swamp/main.go:351`, `tui/application_export.go:111`, `store/document_review.go:269` (`LatestDocumentReviews`) | New type silently skipped: not exported, its reviews never loaded |
| Screens that show exactly two documents | `tui/app.go:224` (`reviewGlyphSummary`), `tui/app.go:292`, `tui/application_detail.go:42`/`:44` (`L`/`R` keys) and `:115`–`:118`, `tui/document_review_select.go:42` | New type never shown or reviewable in the TUI |
| Label switch | `tui/document_review_form.go:104` | Falls back to the raw name (`answers`), which is acceptable |
| "Drafting is done" rule | `stage/stage.go:172` (`CoverLetter.Exists && Resume.Exists`) | A posting with no answers drafted drops out of `list_postings` as done |
| Path convention | `documents/documents.go` (`Paths`, `Status`, `ForApplication`: two named fields) | Needs a new field; the compiler doesn't point to the sites above |
| MCP output | `stage.Prepared` (`stage/stage.go:129`): `CoverLetter` and `Resume` fields | The agent never learns the new document's path or whether it exists |
| Agent skill | `.agents/skills/apply-to-posting/SKILL.md`: "write a cover letter and a resume" | The agent never drafts it |

Some places already handle any number of types and need no change:
- the MCP tools' `DocumentType` enum, which comes from `store.DocumentTypes()` (`mcpserver/mcpserver.go:191`);
- `export.FileName`, which uses `documentType.String()`;
- `stage.Candidate`/`Prepared`'s `LatestReviews` map, keyed by type.

### The "current reviews" check has three copies

`stage.currentReviews` (`stage/stage.go:81`), `tui.currentDocumentReviews` (`tui/app.go:615`) and `main.currentDocumentReviews` (`cmd/swamp/main.go:~381`) all do the same thing:

1. pick the document for the review's type;
2. skip it if the file doesn't exist;
3. read it, failing on a read error;
4. keep the review only if `IsCurrent`.

They behave identically today; only their comments differ. Each copy contains the dispatch block, so each one has the silent fallback above. The reason there are three is recorded in `decisions.log` (2026-09-04, line ~3668):
- `store` has no filesystem access;
- `documents` "never reads content";
- the project doesn't want a generic shared-utilities package.

So neither existing package could host the check.

### Why now

At two types, the duplication has cost comments and three decisions.log entries, but no bugs. It becomes a real risk the moment a third type is added, and RFC 0002 proposes one. This RFC should land before that work, or as its first PR.

## Goals

1. **One list of document types.** Everything that needs "all types" loops over it.
2. **One mapping from a type to its file, filename and label,** with no fallback: an unknown type is an error, not the cover letter.
3. **One "current reviews" check,** replacing the three copies.
4. **A test that fails** when a type in the list has no filename or label.
5. **No hard-coded pair** of document fields in the MCP output or in the TUI's document screens.

### Success criteria

- Adding a type is: one entry in the list, plus its filename and label, plus any content-specific work (like the skill's instructions). No other Go file needs editing to make it appear in the TUI, export, the review check and the MCP tools.
- A test fails if a listed type has no filename or label.
- `grep` finds no remaining `status.CoverLetter`/`status.Resume` dispatch outside `documents`.

## Where things stand

- **`store`** owns `DocumentType`: the constants, `DocumentTypes()` (already derived from the name table), `String`, `ParseDocumentType`, and the JSON/text methods. It also owns `DocumentReview`, `IsCurrent(content)` (a pure hash comparison) and all database access.
  - It imports goose, the migrations, sqlc's `store/db` and `database/sql`; it's the whole database layer.
  - It has no filesystem access.
- **`documents`** owns the path convention (`ForApplication`), `Doc{Path, Exists}` and `Status{CoverLetter, Resume}`.
  - It imports only `os`, `path/filepath` and `strconv`.
  - Its package comment says "the content itself is never read by this package."
- **Imports:** `stage`, `tui`, `mcpserver` and `cmd/swamp` import both packages. From this repo, `store` imports only `store/db` and `db/migrations`.
- **Type names match file names.** The database name for each type (`cover_letter`, `resume`) is already the file's base name (`cover_letter.md`, `resume.md`), by convention rather than by code.

## Options

### Option A (recommended): move `DocumentType` into `documents`; `store` imports `documents`

`DocumentType` describes a kind of document, which is what `documents` is about. `store` needs the type only to save and load it as a string. Moving the type makes `documents` the one owner of everything about document types:
- the list;
- the name, used both as the database value and as the file's base name;
- the label;
- the mapping to a path.

```go
// package documents

type Type int // was store.DocumentType

const (
    CoverLetter Type = iota
    Resume
)

// types is the single list: name (DB value and file base name) and label.
var types = [...]struct{ name, label string }{
    CoverLetter: {"cover_letter", "Cover letter"},
    Resume:      {"resume", "Resume"},
}

func Types() []Type
func (t Type) String() string // the name
func (t Type) Label() string
func ParseType(s string) (Type, error)
// plus MarshalJSON/MarshalText/UnmarshalText, moved as they are

type Status struct{ /* one Doc per Type, e.g. map[Type]Doc or [len(types)]Doc */ }
func (s Status) Doc(t Type) (Doc, error) // error for an unknown type, never a fallback
func (s *Store) Path(applicationID int64, t Type) (string, error)
```

- **`store`** imports `documents` for the type. It's a small package with no dependencies of its own, and `store` still does no file I/O itself.
- **`documents` doesn't import `store`,** so it doesn't pull in the database layer. There's no cycle.
- **The "current reviews" check** lives in `documents` and takes a callback, so `documents` still doesn't need `store.DocumentReview`:

  ```go
  // CurrentReviews keeps the reviews whose document exists and whose
  // content still matches. isCurrent is store.DocumentReview.IsCurrent.
  func CurrentReviews[R any](s Status, reviews map[Type]R, isCurrent func(R, string) bool) (map[Type]R, error)
  ```

  Alternatively, it can live in `stage`, with `tui` and `cmd/swamp` calling it there (see open questions).

**Costs:**
- Renaming `store.DocumentType` to `documents.Type` touches 11 non-test files and 9 test files. It's mechanical, and the compiler finds every site.
- `documents` starts reading content, for the hash comparison only. Its package comment changes from "never read" to "read only to check whether a review is still current". This is the second reason the 2026-09-04 entry gave, and it's relaxed on purpose here.
- The database string for each type now lives in `documents`. That fits, since it's the same string as the file's base name. But renaming a type's file would now also mean migrating `document_reviews.document_type`. That's a real coupling, and it's written into the name table's comment.

### Option B (the earlier draft's recommendation): `documents` imports `store`

`documents` gains `Status.Doc(store.DocumentType)`, `Paths.Path(store.DocumentType)` and `Status.CurrentReviews(...)`, and imports `store` for the type.

- **Pro:** smallest diff; no rename.
- **Con:** the small filesystem package would depend on the whole database layer (goose, migrations, sqlc).
- **Con:** the type list stays in `store`, while the filename and label mapping sits in `documents`. The two packages share one list, and a type added in `store` but not mapped in `documents` still only fails at runtime. Goal 4 needs a test that spans both packages.
- **Con:** it's the exact change the 2026-09-17 entry declined ("documents deliberately has no dependency on store"). That's allowed, but Option A gets the same result without it.

### Option C: a new leaf package for `DocumentType` (e.g. `doctype`)

Both `store` and `documents` import it.

- **Pro:** neither core package depends on the other.
- **Con:** a third package for one type, which is close to the "shared utilities" package the 2026-09-04 entry rejected. The filename mapping would still live in `documents`, split from the list.

### Option D: status quo, plus a checklist

Keep the copies. Replace each `if … Resume` fallback with a switch that errors on unknown types, and switch the hard-coded lists to `DocumentTypes()`.

- **Pro:** smallest change; removes the silent fallback, which is the worst part of the problem.
- **Con:** adding a type still means editing six dispatch sites and three loops, with nothing pointing to them but grep.
- **Worth doing anyway, as the first PR under any option:** it's cheap and removes the silent failure right away.

## Proposal

Each step is its own issue and PR.

1. **Fail loudly, not silently (S).** Replace the six fallbacks with an error on unknown types, and replace the three hard-coded lists with `DocumentTypes()`. No design change. This removes the silent failures even if the rest waits.
2. **Move `DocumentType` into `documents` (S–M).** Rename, move the name table, add labels and `Types()`. Update the `documents` package comment. Add a test that every entry in `Types()` has a name, a label and a path.
3. **One "current reviews" check (S).** Replace the three copies. Update the doc comments that cross-reference them, and add a decisions.log entry recording that the 2026-09-04 and 2026-09-17 decisions were revisited, and why.
4. **Screens and MCP output driven by the list (M).**
   - Show review glyphs, the detail screen's document rows and the review picker for every entry in `Types()`.
   - Replace `openDocument(resume bool)`.
   - Decide the `L`/`R` keys (see open questions).
   - Replace `Prepared.CoverLetter`/`Resume` with a map keyed by type, and update the skill.
   - Generalize `stage.go:172` to "every required document exists" (see open questions: RFC 0002 makes `answers` optional per posting).

After step 4, RFC 0002's answers document is: one entry in the list, plus the skill's drafting instructions.

## What shipped

Option A, in the four steps above, with the open questions decided by the user on 2026-10-01 (see `decisions.log`):

1. **Fail loudly** (#194). The six fallbacks errored on unknown types and the hard-coded pairs used the type list. Three more pairs had appeared since this RFC was measured (#164, #166, #188).
2. **`documents.Type`** (#195). One table in `documents` holds each type's name (file base name and stored value) and label; `store` imports `documents`. `Status.Doc(Type)` and `Store.Path(applicationID, Type)` replace the dispatch.
3. **`documents.Current`** (#196). One generic check replaces the three copies. It's named `Current`, not `CurrentReviews`, because the home screen uses it for exports too.
4. **Screens and MCP output driven by the list.**
   - Each type carries its TUI key (`l`/`r`; uppercase reviews).
   - Application detail's keys, rows and help line, posting detail's rows, the review picker and the home screen's review column all loop over `documents.Types()`.
   - Row titles and the review column's abbreviations are derived from the label.
   - `stage.Prepared` has `Documents`, keyed by type name, and SKILL.md says so.
   - "Drafting is done" means every type exists.
   - `Status` and `EnsureDir` hold no per-type fields; `Paths` and `ForApplication` are gone.

Answers to the open questions: (1) `documents`, generic over the record; (2) a key per type in the list; (3) every type in the list, until RFC 0002 phase 2 adds per-posting requirements; (4) one PR changing both the MCP output and the skill.

Adding a type is now one entry in `documents`' table (name, label, key), plus the skill's drafting instructions. `TestTypes_EachHasANameLabelAndDocument` and `TestTypes_KeysAreDistinctLowercaseLetters` fail if the entry is incomplete, and `TestApplicationDetailModel_EveryDocumentType` checks the new type's keys don't clash with the screen's own.

## Corrections to the earlier draft

| Earlier draft said | Actually |
|---|---|
| 7 sites to change for a new type | About 20, plus the skill. The earlier count covered only the dispatch block. |
| Missed cases would "fail loudly at build time" | Go switches aren't exhaustive, and the configured linters (`default: standard`) don't include `exhaustive`. A test over `Types()` is what catches a missing mapping. |
| `store` is "a pure enum-and-struct package" | It's the whole database layer: goose, migrations, sqlc, `database/sql`. |
| The copies "have already drifted" | They behave identically. Only the comments differ. |
| A missed site fails as "document missing or a wrong-field read" | It silently uses the cover letter (see Problem). |
| Quoted 2026-09-04 as "the only shared thing is this two-line dispatch" and "cheaper to duplicate that than to create a new shared-utility package" | Neither phrase is in `decisions.log`. The entry (line ~3668) gives two reasons: `store` has no filesystem access, and `documents` never reads content. Option A answers the first and relaxes the second explicitly. |
| `cmd/swamp/main.go:322`/`:353`, loop at `:350` | Now `:352`/`:383`, loop at `~:381`, after #145. |

## Open questions

1. **Where does the "current reviews" check live?** `documents`, with a generic callback, as in Option A? Or `stage`, which already imports both packages, with `tui` and `cmd/swamp` calling `stage`? The second avoids generics and keeps `documents` from reading content, but makes the TUI and CLI depend on `stage` for it.
2. **Keys for more than two documents.** Keep `L`/`R` and give each type a key in the list? Or replace them with the existing two-step review picker (`document_review_select.go`)?
3. **Required vs optional documents.** RFC 0002 makes a cover letter or answers document optional depending on the posting. Should "drafting is done" (`stage.go:172`) read a per-application requirement, or is "every type in the list" enough until RFC 0002 phase 2 lands?
4. **MCP output compatibility.** Replacing `Prepared.CoverLetter`/`Resume` with a map changes the tool's output. The skill is the only consumer and ships in this repo, so change both in one PR rather than keeping the old fields?

## Out of scope

- `store` gaining filesystem access.
- Adding the answers document itself (RFC 0002, phase 2).
- Reading document content for anything other than the currentness hash comparison.
