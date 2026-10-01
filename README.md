# Swamp

Automate the collection, curation, and application of job postings from various job boards. i.e. Ashby.

## Your profile and canonical resume

The `apply-to-posting` skill drafts from two files you write and maintain
yourself. Both live under the documents directory (`SWAMP_DOCUMENTS_PATH`,
default `assets/`), which is gitignored, so they never get committed:

| File | What it is | Template |
| --- | --- | --- |
| `assets/canonical/profile.md` | Your background: experience, skills, framing options, and voice/style notes. The source every draft comes from. **Required**: the skill stops without it. | `docs/canonical/profile.md.example` |
| `assets/canonical/resume.md` | Your best-version resume in markdown. The skill tailors it for each posting (reorders, trims, rephrases) instead of writing a resume from scratch. **Optional**: without it, resumes are drafted from the profile alone. | `docs/canonical/resume.md.example` |

To set them up:

```sh
mkdir -p assets/canonical
cp docs/canonical/profile.md.example assets/canonical/profile.md
cp docs/canonical/resume.md.example assets/canonical/resume.md
```

Then replace the placeholder text with your own. If you set
`SWAMP_DOCUMENTS_PATH`, use `$SWAMP_DOCUMENTS_PATH/canonical/` instead of
`assets/canonical/`.

Agents read them through the `swamp` MCP server (`go tool task mcp-serve`)
with `read_profile` and `read_canonical_resume`, not from disk. Both
tools are read-only: edit the files yourself and the next draft picks up
the change. Neither tool needs a restart to see an edit.

Keep the resume complete rather than tuned for one role. Tailoring only
cuts and rephrases, and never adds claims that aren't in either file, so
anything left out of both won't appear in a draft.
