# Change Log

This file is the local rollback reference for changes made in this repository.
When a change introduces an error, review the latest entries here first and then inspect the related files before reverting.

## 2026-09-08 (Backing up all of it, and handing it to the agent)

Two separate holes in the same sentence — "back up all the data as JSON". The
JSON data-set export had never carried Regulation Coverage or the crisis
exercises at all, and there was no shape of the data an AI agent could be handed
as a file.

**The whole data set, this time.** `internal/dbsync/dbsync.go`: `syncOrder`
covered 19 tables; the schema has 40. The 21 it never mentioned are the entire
Regulation Coverage module (`reg_coverage_regulations`, `_sources`, `_sections`,
`_findings`, `_mappings`, `_versions`, `_chat`) and the entire Risk & Crisis
Exercise module (`crisis_ex_exercises` and its objectives, phases, injects,
responses, decisions, classification, clocks, findings, participants,
references, versions and chat). Both are now in the list, parent-before-child
and id-keyed so the foreign keys survive. The failure this fixes is quiet: an
export ran clean, an import reported success, and the destination came up with
an empty Regulation Coverage and no exercise history.

- `SnapshotVersion` 7 → 8. A v7-or-older backup still restores, with those
  tables empty, which is what the file actually contains.
- `app_state` and `app_secrets` stay out, now with the reason written down: the
  first is this deployment's own configuration (which AI provider, which
  Wintermute agent) and should not follow the data onto another server; the
  second is ciphertext whose key file never travels, so copying it would move an
  unreadable blob and hide that the credential has to be set again.
- `tableSpec.blobCols` and base64 in `internal/dbsync/transfer.go`:
  `reg_coverage_sources.content` is the original uploaded regulation, a BLOB.
  Export coerced every `[]byte` to a string, so `encoding/json` replaced each
  invalid UTF-8 byte with U+FFFD — a snapshot that imports cleanly and restores
  a corrupt PDF. Binary columns now travel as base64 and are decoded on import;
  `Sync` leaves them as bytes. `TestExportImport_CarriesRegulationCoverageAndCrisisExercises`
  fails with `[255 254 128]` → `[239 191 189 ...]` if that handling is removed.
- `internal/dbsync/coverage_test.go` (new) is the guard that would have caught
  this years earlier: every table in the live schema is either in `syncOrder` or
  in `skippedTables` with its reason, and every column of a managed table is
  listed (bar the `id` that natural-key tables omit on purpose). A new module
  can no longer ship without being in the backup.
- `internal/dbsync/modules_test.go` (new): both modules round-trip through
  JSON with their foreign keys resolving, the same via `Sync`, and a v7 snapshot
  still imports.

**The data as something an agent can read.** `internal/knowledge/export.go`
(new): `Service.Bundle` returns every record of every kind — NFRs, 800-53
controls, regulation clauses and their coverage findings, policy sections,
risks, crisis exercises and their findings — each with its full body, its `ref`,
its `related` records and a `url` back to its page here. It is the same `Item`
shape the four query endpoints already return, so an agent that knows one knows
the other.

- `GET /api/knowledge/export` (read-only knowledge token, like the rest) and
  **Utilities → Export for the AI agent** (`/utilities/knowledge-export`, admin
  session) serve it. The first is for an agent that can reach this server on a
  schedule; the second is for a person uploading the file into a Wintermute
  agent's library by hand.
- Corpora are read from the database, not from the 45-second query cache. That
  cache is the right trade for answering a question and the wrong one for an
  export: a backup taken ten seconds after an edit that silently predates it is
  the failure this is meant to prevent. `TestBundleIsNotServedFromTheCache`
  pins it.
- `generated_at` is stamped on the document and repeated in `note`, because an
  uploaded bundle goes on answering confidently after the catalog moves under
  it, and a model that can read the date can say how old its answer is.
- `internal/app/utilities.go`: the new panel, and the data-set panel's text
  corrected — it advertised CRM tables that no longer exist and did not mention
  the two modules that are now actually in the file.
- `AI_AGENT.md`: the fifth endpoint, a worked example of the document, and the
  staleness trade written down beside it. The kinds list also gained `exercise`
  and `exercise_finding`, which it had been missing.

`go fmt`, `go vet` and `go test ./...` clean apart from the pre-existing
`TestGovulncheck` failure (Go standard library advisories fixed in go1.25.13;
the toolchain here is go1.25.12).

## 2026-09-07 (A Help page: what this application does, as work)

There was no page that answered "what can this do, and how do I do it" — the
answer lived in the repository's markdown and in whoever had used the app
before. `/help` is that page, first in the Admin section.

- `internal/app/help.go` (new): the modules here are steps in a few long
  processes — the control catalog feeds the requirements catalog, which feeds
  policy, regulation coverage and the exercises that cite them — so it is
  organised as jobs rather than as a feature list: maintaining the two catalogs,
  growing them from a document, scoping by asset type, the risk register,
  exceptions, policy from draft to approved, regulation coverage, crisis
  exercises, detection rules, reporting and extracts, Jira, asking the AI,
  client-ready documents, and running the installation (settings, users, backup
  and transfer, reference surfaces). A table of every page and what it is for
  closes it, and a short orientation section covers the topbar sections, the
  Ctrl K page search, the Ask AI dock and the theme controls.
- Registered in `registerPublicPageRoutes` and deliberately left out of
  `protectedPrefixes`: it describes the application and carries none of its
  data, exactly like the home page, and a help page an operator cannot reach
  until they are already inside helps nobody. For the same reason it is not in
  `userManagementAllowedPages` — there is no gate to grant.
- `internal/pageui/nav.go`: **Help** is first in the Admin group, above
  Settings, API Docs and Utilities.
- `internal/app/navigation_test.go`: `/help` joins the pages asserted to render
  the shared navigation, palette and main spacing.

`go fmt`, `go vet` and `go test ./...` clean apart from the pre-existing
`TestGovulncheck` failure (Go standard library advisories fixed in go1.25.13;
the toolchain here is go1.25.12).

## 2026-09-07 (The agent is chosen per question, and AI Chat is a section)

Which agent answers decides which documents an answer is grounded in, and it
was an installation setting only: AI Chat sent every question under whatever
Settings was last set to, and a question asked with no agent is answered from
the model's training data while reading exactly like a grounded answer. It is
now a field on the page, beside the backend and the model.

- `internal/app/ai_chat.go`: an **Agent** select in the Wintermute fields,
  filled from the server and defaulting to the agent Settings configured — so a
  reader who changes nothing gets the answer they would have got anyway. The
  hint under it says what the choice means (the agent's description and
  sources, or that no agent means an ungrounded answer), an agent the server no
  longer has is shown as missing rather than silently dropped, and the
  "Documents in Wintermute" link now points at the chosen agent's library.
- `GET /ai-chat/wintermute/agents` (new, in `registerAIAuxRoutes`): the same
  shape as the backend/model catalog route — the page's URL override, the same
  `ValidateEndpoint` rules, and the client token taken from Settings and never
  from the request.
- `aiChatRequest.Agent` is a `*string`, because "no agent" and "not specified"
  are different instructions: the AI dock omits the field and gets the
  configured agent, while the AI Chat page always says which agent it means,
  including none. Changing the agent drops the Wintermute session, since a
  session is created with its agent.
- Tests: the new route against a stub server, its URL and token refusals, and
  the pointer semantics of the agent field.

### AI Chat is a top-level section

It was one entry among Reporting & Data, which is where reports live, not the
page most often reached from the middle of other work. It is its own section in
`internal/pageui/nav.go` now, and the topbar renders a section holding a single
page as a link straight to it — so AI Chat is one click from anywhere rather
than a section switch followed by a tab.

Checked headless on the AI Chat page under both providers and on Reports.
`go fmt`, `go vet` and `go test ./...` clean apart from the pre-existing
`TestGovulncheck` failure (Go standard library advisories fixed in go1.25.13;
the toolchain here is go1.25.12).

## 2026-09-07 (The top level of the navigation moved to a topbar)

The sidebar stacked all five groups — Catalog, Compliance & Risk, Reporting &
Data, Documents, Admin — and their thirty-odd items in one scrolling column, so
the section you were actually working in was a sixth of a list and the group
heads were doing the work of navigation while looking like captions. The top
level is now a switcher in a fixed topbar and the sidebar carries one section at
a time, which is wintermute's shape (`.topbar` holds the view buttons, the
sidebar holds the tabs of the view you are in).

- `internal/app/theme_middleware.go` (`sideNavTag`): a fixed `.global-topbar`
  holds the ☰ collapse toggle (moved in from where it floated over the page at
  that same corner), a **GRC** brand link — the Home tab, which was already
  being read as one — and a `.global-view-btn` per group. Admin sits apart at
  the right end of the bar, which is what the dashed rule in the sidebar used to
  say. The bar reserves the width the floating theme and brightness controls
  occupy, so they sit on it rather than over it.
- The sidebar and page content start below the bar (`--global-topbar-h`), and
  the sidebar's group padding lost the stacked-column spacing it no longer
  needs.
- Switching sections is a DOM swap, not a page load. Because the pages are
  separate documents, the bar re-derives the open section from the active tab on
  every load — a link followed from anywhere lands with its own section open,
  and Home, which marks no tab, opens on the first section.
- Phone widths keep the drawer: the bar stays, the switcher scrolls sideways
  under a smaller reserve for the theme controls, and the sidebar is still
  off-canvas behind the same ☰.

Checked headless at 1440x900 and 430x860 on Home, Controls, Risk Register and
Settings, including switching sections from the bar. `go fmt`, `go vet` and
`go test ./...` clean apart from the pre-existing `TestGovulncheck` failure (Go
standard library advisories fixed in go1.25.13; the toolchain here is
go1.25.12).

## 2026-09-07 (Anchors drawn as buttons were still white)

The dark themes restyle every `<button>` on a page, but a link drawn to look
like a button was never covered — so the control-ID pills under **NIST Mapping**
on `/security-nfrs`, the "Open Detail" / "Open Full Detail" links in the control
catalog, "Back to Exceptions", and the download links in Utilities all kept the
light parchment or plain white their own page CSS gives them, and read as white
bricks on a dark page.

- `internal/app/theme_design.go`: the shared theme layer now gives `a.btn`,
  `a.button`, `.button-link`, `.control-chip`, `.row-open-link` and
  `.detail-open-link` the same ghost treatment as a `<button>` — `--surface-2`
  fill, `--border` outline, accent border on hover — with the `.secondary` and
  `.danger` variants kept distinct.
- `internal/securitynfr/handler.go`: the NIST-mapping control-ID pills carried
  `background:#fff` as an inline style with no class, so nothing in the theme
  layer could reach them. They are now `class="control-chip"` and their inline
  fallback is `var(--surface-2, #efe6d6)`.

`go fmt`, `go vet` and `go test ./...` run clean apart from the pre-existing
`TestGovulncheck` failure (Go standard library advisories fixed in go1.25.13;
the toolchain here is go1.25.12), which this change does not touch.

## 2026-09-07 (Light theme removed; a UI review of what is left)

Four themes now: **Dark**, **Matrix** (green, the falling-glyph rain),
**Chaos** (the same green plus the per-character colour glitch) and **40K**
(brass and bone, the failing-CRT overlay). Light is gone — wintermute is
dark-only, and a fifth palette beside these four was a set of colours that had
to be re-checked on every change and was chosen by nobody. A browser still
carrying the retired `light` cookie falls through to Dark: that is the existing
default for an unrecognised value, so there is no migration and nothing to
clear.

### The themes were named as the one after them

The theme toggle is the only place a theme is ever named, and it named the theme
a **click switches to**. So the green palette with no glitch — Matrix — sat
under a button reading "Chaos theme", and every theme appeared to be called its
successor. The button now names the theme you are in (`▓ Matrix`), and what a
click does lives in the tooltip and the accessible name, where a control's
action belongs.

### Two regressions the review caught

- **Everything was one flat surface.** `main` was being painted `--surface`
  along with the panels and cards on it, so page, panel and card were the same
  colour and every boundary on screen came down to a 1px border. The page is
  `--bg` again, as wintermute's shell is; a card nested inside a panel takes
  `--surface-2` rather than repeating its parent's colour.
- **The sidebar links were underlined** on any page whose own CSS underlines
  links — the injected sidebar lives inside that page's `<main>`, so it
  inherited the rule. Cured once in the shared layer rather than in each page.

Checked across all four themes on the catalog, module and settings pages.

## 2026-09-07 (Why the agent did not reach the Ask AI box)

Three separate causes, all reproduced against a stand-in wintermuted that
records what each session is opened with. Only the first was a bug in the sense
of code doing the wrong thing; the other two are the same complaint from the
operator's chair.

**1. Until earlier today it genuinely never transferred.** The dock chose its
own provider — Claude whenever a key existed — and `aiChatProvider` set no agent
at all. Both are fixed above. A binary built before that still behaves the old
way, which is worth knowing if the box being tested is a deployed one.

**2. The agent only applies when questions go to Wintermute, and the default
provider is Claude.** The Settings page showed the whole Wintermute block —
server, backend, model, agent — at full strength whatever the provider was, so
an agent could be chosen, stored and displayed while every question went to
Anthropic, where agents do not exist. That block is now dimmed with a line
saying so: *"Not in use: questions are going to Claude, which has no agents.
These settings are kept, and apply as soon as the provider is Auto or
Wintermute."*

**3. A conversation keeps the agent it was opened with.** wintermuted owns the
transcript, so `aiprovider.Wintermute.Ask` sends the agent when it opens a
session and never again — a resumed one carries only its id. Changing the agent
in Settings therefore did nothing to a conversation already on screen, with
nothing to say why. Demonstrated: same conversation, still answered as `grc`
after the setting moved to `policy`; a fresh one used `policy`.

### The Ask AI panel now says what will answer

Its head carries `Wintermute · grc`, `Wintermute · no agent` (with a tooltip
explaining that answers then come from the model rather than this
installation's catalogs), or `Claude`. It is re-read every time the panel is
opened, because Settings is changed in another tab and that is the moment it
matters — and when what will answer has changed, the session id is dropped so
the next question opens a conversation against the new agent instead of
continuing one pinned to the old.

Checked end to end in a browser: a question answered as `grc`, the agent changed
to `policy` behind the page's back, the panel closed and re-opened, and the next
question answered as `policy` — a new session, opened with the new agent.

### Not this application's half

An agent that transfers can still answer ungrounded if it has no `grc` source,
or if that server has no `GRC_URL` / `GRC_KNOWLEDGE_TOKEN` — steps 1 to 3 in
`AI_AGENT.md`. From here that looks exactly like the agent not transferring; the
panel's new line is what separates the two.

## 2026-09-07 (The agent was being saved over, not not-saved)

"AI provider settings do not survive a restart, including the agent." They do —
`ai.provider`, the server URL, the backend, the model, the agent and both
encrypted credentials are written to SQLite and read back on the next start.
Verified rather than assumed: `TestAIProviderSettingsSurviveARestart` closes the
database, rebuilds the whole service including the keyring from disk, and reads
every one of them back.

What actually happened is worse than a value not being written, because it looks
identical from the outside.

### The agent select wrote its own emptiness back

The agent list is fetched from the Wintermute server. When that server is
unreachable — which it is on every restart until it comes up, and permanently if
it lives on a laptop that is off — the select fell back to its only option, "No
agent", and the *next Save stored exactly that*. One press of a button nobody
associated with the agent, and a setting that was never touched was gone.

Nothing announced it. Questions kept being answered, from the model's training
data rather than from this installation's catalogs, which is the failure
`AI_AGENT.md` exists to describe.

The backend and model selects had been given this guard already; the agent had
not. It now shows the stored value before any lookup returns, says
"Keeping the saved agent, grc." when the list cannot be fetched, and labels a
value the server no longer offers rather than dropping it.

### And the other reason settings "disappear"

The service defaults to `users.db`; `run_local.sh` defaults to `local.db`. They
are different databases, so anything configured under one is absent under the
other — which reads as "nothing was saved", the one explanation that is not
true. The Settings page now names the file it is writing to, beside the existing
line about where the encryption key came from.

### Tests

`TestAIProviderSettingsSurviveARestart` (every preference and both credentials
across a real restart, agent named explicitly), `TestClearedAgentStaysCleared`
(clearing is a decision that has to persist too), and
`TestStoredAgentOutranksTheEnvironmentAcrossARestart` (an install configured
through `WINTERMUTE_AGENT` keeps working, and a value stored in the page takes
over from it). On the page side, `TestSettingsPageKeepsStoredSelectionsItCannotList`
covers all three selects.

Checked end to end as well: with the Wintermute server stopped, the Settings
page shows `grc`, explains that it is keeping it, and pressing Save round-trips
it. Before this, the same sequence stored an empty agent.

## 2026-09-07 (One design system, shared with wintermute)

The two applications are used by the same people, often side by side, and they
did not look like they came from the same place. This ports wintermute's design
system — `internal/web/static/style.css` in that repository: its palettes, its
token names, its component vocabulary and its chat dock — onto every page here.

The mechanism is not ported and could not be. wintermute is a single-page app
that owns its markup, so it styles `.card` and `.pane` directly. Every page here
renders its own HTML with its own `<style>` block, so the same look has to be
imposed from outside — which is what the theme middleware already did. What
changed is what it paints, not how.

### `internal/app/theme_design.go`

New file holding the whole system: five palettes in wintermute's token names
(`--bg`, `--surface`, `--surface-2`, `--border`, `--text-base`, `--muted-base`,
`--accent`, `--error`, `--gain`, `--loss`), the alias block that maps this
application's existing `--ink` / `--line` / `--panel` / `--muted` onto them, and
one component layer addressed at both wintermute's class names and the ones the
pages here already use. About three hundred rules across the page handlers read
those older names; aliasing is what restyles thirty pages without editing
thirty handlers.

Dark, Matrix and Chaos are that stylesheet's palettes unchanged — Matrix already
matched to the hex. **40K is new here**: bone text and brass rule on a warm
near-black, Courier throughout, stamped uppercase headings, and the failing-CRT
overlay (static scanlines and vignette, a roll bar drifting down the glass, and
irregular tear bursts from a timer rather than a keyframe loop, because a
predictable glitch reads as decoration rather than as a fault). It is injected
only for that theme, the way the rain is injected only for Matrix and Chaos.
Light was carried over at first and then removed (below): wintermute is
dark-only, and a fifth palette maintained beside these four was re-checked on
every change and chosen by nobody.

**Text brightness**, also from wintermute: an `Aa` control beside the theme
toggle lifts the two text tokens towards white in four steps without touching
the backgrounds or the accents, so a theme survives being made legible on a
phone in daylight. Applied at the end of `<head>` from localStorage, because
after paint it is a visible flicker on every page load.

### Buttons: one deliberate divergence

wintermute fills every button with the accent and letters it in the palette's
near-black, because there a button is an action. Here it is also a filter chip,
a list row, a domain pill and a section toggle — most of the buttons on these
pages are one of those. Both were built and looked at: filled, a catalog page is
a column of accent bricks with no emphasis left for the control that actually
submits. So the default is wintermute's **ghost** button, and the fill is kept
for `[type=submit]` and `.primary`.

Everything else is literal: `.ghost-btn` / `.secondary` outlined, `.link-btn`
bare, `.danger` in the error colour, and the lettering on a filled button taken
from the palette (`--on-accent`) rather than hard-coded — which is how the
phosphor green and the brass get readable lettering on their own accent instead
of a dark that disappears into both. Two additions the port needed: whatever a
filled button holds is lettered on the accent too (a button there holds a word;
one here can hold a label the page paints in the muted tone, which on the fill
is grey on blue), and a selected row keeps an accent border, or the layer above
would flatten every state a page draws with a button into one surface.

### The AI panel comes from the right

It was a full-width strip along the bottom of every page. It is now wintermute's
chat dock: a vertical handle pinned to the right edge, and a `min(460px, 100%)`
panel that slides out over the page, full-screen below 720px. A question is
asked *while* working on a page, and the page has to stay legible beside it,
which a strip across the bottom does not allow.

It starts below the floating theme controls rather than at the ceiling — same
reason wintermute's starts below its topbar: the controls pinned above it have
to stay reachable with it open. Escape closes it. The body no longer reserves
84px of bottom padding and the sidebar runs the full height again.

### Tabs and menus

The sidebar takes wintermute's list vocabulary — 12px gutter, 8px radius rows,
muted until hovered, `surface-2` when active — and its group captions the
caption one. The command palette's rows were a flex line whose ragged group
caption ("ADMIN" to "COMPLIANCE & RISK") pushed every page name to a different
indent; they are a grid now, so the list can be read down its left edge.

### Checked

Rendered through headless Chrome at 1440x900 on Security NFRs, Controls and Risk
Register in all five themes, with the AI panel open and the command palette
open. New tests assert every theme defines the whole token set and the aliases,
that the brightness fallback precedes the `color-mix` derivation (a dropped
custom property does not fall back to the palette — it makes every colour that
uses it invalid), that the fritz overlay reaches 40K and nothing else, and that
the AI panel is a right-hand slide-out rather than a bottom strip.

## 2026-09-07 (The AI dock ignored the provider setting)

Setting the AI provider in Settings did not change what the **Ask AI** box
asked. It was not a stale value or a caching problem: the dock decided for
itself.

```js
if (data.claude_configured) return 'claude';
if (data.token_configured && data.configured) return 'wintermute';
```

It asked which credentials existed and picked Claude whenever an Anthropic key
was configured. `ai.provider` was never consulted — the status endpoint it
called did not even report it. An install pinned to **Wintermute only**, chosen
so questions do not leave the network, sent every docked question to Anthropic,
and the answer came back looking exactly like a local one.

### The routing

An unnamed provider on `POST /ai-chat/ask` now means "whatever Settings routes
to", resolved through the same `aiprovider.Router` every other AI field in the
app asks through — so it honours `auto`, and carries the stored backend, model
and agent. It used to mean Claude. The dock sends no provider at all now, which
is right for a box with no provider control: the decision belongs to the one
place that is configured.

The router's own usage logging is bypassed (`Selected()` returns the provider,
`Ask` is called on it directly), so a docked question is still counted once, in
the same place a page question is. Verified against a stand-in server: four
questions, four rows.

### The second half of it, which was quieter

A question routed to Wintermute carried **no agent**. `aiChatProvider` built its
config from the request only, and neither the dock nor the AI Chat page has an
agent field — so every question this app asked through that path ran against the
server's general assistant instead of the agent holding this installation's
catalogs. That is the failure `AI_AGENT.md` describes: an answer from the
model's training data, wearing the same confidence as a grounded one. The agent
now comes from Settings, as the backend and model already did for the dock.

### AI Chat page

Opens on the provider Settings routes to, rather than always on Claude —
including `auto`, which resolves the way the router resolves it. It remains the
one page where a provider is chosen per question, so the selection is only a
default, and a reader who changes it is not overridden by a status response
arriving late.

### Checked against a stand-in wintermuted and a stand-in Anthropic API

With both credentials configured, so the preference is the only thing that can
decide:

| `ai.provider` | docked question served by |
|---|---|
| `claude` | claude / claude-opus-5 |
| `auto` | wintermute / gemma3:12b |
| `wintermute` | wintermute / gemma3:12b |

The session the Wintermute server was asked to open carried
`backend=workshop, model=gemma3:12b, agent=grc` — the configured values, none of
which the dock can express. `TestUnnamedProviderFollowsTheSetting` covers all
three preferences and would have caught the original bug;
`TestWintermuteQuestionsCarryTheConfiguredAgent` covers the agent.

## 2026-09-07 (Build time)

A full build was taking 15+ minutes on a modest machine. Measured on a 12-core
box, a cold `go build ./...` was 57s wall — of which 42s was a single serial C
compile of the SQLite amalgamation in `mattn/go-sqlite3`, which cannot use more
than one core. A warm build is 4.3s. So there were two questions: why the build
was cold so often, and why cold cost so much. Both are answered below; the cold
build is now 31.4s and needs no C compiler.

### The build cache was being thrown away

Both release scripts defaulted `GOCACHE` to `/tmp/gocache-grc`, and `/tmp` is
cleared on reboot. Every cross-build after a restart therefore recompiled that
amalgamation and the standard library from scratch, once per target — three cold
cgo builds where there should have been none. It now defaults under
`${XDG_CACHE_HOME:-$HOME/.cache}`, which survives a reboot. `GOCACHE_DIR` still
overrides it.

Worth knowing: release builds use `-tags fts5` and local ones do not, and a
build tag is part of the cache key. The two never share the SQLite object, so
each pays its own cold compile the first time.

### The C driver is gone

`mattn/go-sqlite3` replaced by `modernc.org/sqlite` — SQLite translated to Go
rather than bound to it through cgo. The module now has no cgo at all, and
`Agents.md` says it must not gain any: that property is what the rest of this
entry rests on.

| | cgo driver | pure Go |
|---|---|---|
| driver compile, cold | 42s wall / 56s CPU, one serial C compile | 18.9s wall / 58s CPU, spread across cores |
| with `-tags fts5` | 47.6s | n/a — always compiled in |
| cold `go build ./...` | 57.3s | **31.4s** |
| cross-compiling | needs llvm-mingw / osxcross | `GOOS=windows go build`, no toolchain |

The CPU total barely moves; what changes is that the work can be split. On two
slow cores the difference is wider than these numbers suggest, because the C
compile is one translation unit and cannot use a second core at all.

`scripts/build-windows-cross.sh` lost its toolchain discovery and its hard
failure — it sets `CGO_ENABLED=0` and compiles. Verified by building a PE32+
binary on this Linux machine with no mingw installed. The goreleaser wrapper
still discovers toolchains and still exports `WINDOWS_CC` / `DARWIN_CC` for a
config that templates them, but a missing one is now a note rather than an
`exit 1`. **The `.goreleaser.yaml` is not in this repository** — whoever holds
it should set `CGO_ENABLED=0` and drop the `CC` entries to get the same benefit
there.

`setup.sh` and `update.sh` build with `CGO_ENABLED=0`, so a server no longer
needs a C compiler to deploy from source.

### `-tags fts5` retired

The C driver left FTS5 out unless asked for it, which is why the tag existed and
why `/version` reported it: a binary built without it failed only when the
policy-document corpus search first ran. The pure-Go driver compiles FTS5 in
unconditionally, so `internal/app/fts5_enabled.go` and `fts5_disabled.go` are
one plain constant now.

`FTS5Enabled` stays, and so does the `verify-install.sh` check, because a
deployed binary predating this change can still answer false and that is worth
seeing. What it claims is now proved rather than asserted: `TestFTS5IsCompiledIn`
in `internal/db` creates a virtual table and runs a `MATCH` against a real
database, which the old build-tag test could not do.

### What was checked before this landed

- The whole suite passes with `CGO_ENABLED=0` and no build tags.
- govulncheck reports no new advisories — the same seven standard-library ones
  from the go1.25.12 toolchain, and nothing from the six modules the driver
  adds (`libc`, `mathutil`, `memory`, `go-humanize`, `go-strftime`, `bigfft`).
- **A database written by the cgo build opens unchanged under the new one.** A
  binary built from the previous commit created `legacy.db`, wrote preferences
  and an encrypted credential; the pure-Go binary read both back, decrypted the
  credential, and served the pages. The file format was never the risk, but a
  live `users.db` is not the place to find that out.
- WAL is still the journal mode after the swap (`TestOpenSQLiteUsesWAL`), which
  the app depends on for readers not to block behind a writer.
- Settings write, restart, read back; a Windows binary cross-builds with no C
  toolchain present.

One honest trade: a Go translation of SQLite is slower than the C original —
commonly cited around 2x on write-heavy work. This application's queries run
against a few thousand rows of catalog, so it is not expected to show, but it is
a real cost paid for build time and portability. `PRAGMA busy_timeout` and
`foreign_keys` are still applied per connection by `OpenSQLite` exactly as
before, unreliably across a pooled `*sql.DB` — unchanged here on purpose, since
fixing it would start enforcing foreign keys that are not enforced today.

### `go test -short` for the inner loop

`TestGosec` and `TestGovulncheck` skip under `-short`. They analyse every
package and govulncheck fetches its database over the network — 16s of a 21s
suite here, and far more on a slow machine or link. Plain `go test ./...` runs
them exactly as before, so the security gate this repository documents is
unchanged: -short has to be asked for.

## 2026-09-07 (Every model and backend is chosen from a list, not typed)

The agent was already picked from a list fetched off the Wintermute server; the
backend and the model beside it were still free text. That asymmetry was the
wrong way round — a mistyped agent id answers ungrounded, but a mistyped backend
name is a question that fails at ask time, in a feature nobody is watching, with
an error that reads like the model is broken.

Both are dropdowns now, populated from the server itself.

### `internal/settings`

New `GET /api/settings/ai-providers/catalog`, proxying that server's
`/api/v1/backends` and `/api/v1/models` for the same reason the agents route
exists: the client token lives here and must not reach a browser, and the server
is often on a network the browser cannot reach. It returns only what the page
renders — name, kind, health, and each model's backend, size and whether it is
resident. The server's backend records carry base URLs and the names of the
environment variables holding vendor keys, and a test asserts none of that
survives into the response.

The two lists are fetched separately and only the backends are fatal. A server
that cannot produce a model list (an older API, an unreachable backend) still
lets an operator choose a backend, which answers on its own default; the failure
is reported next to the dropdown rather than swallowed, so an empty list is
never mistaken for a complete one.

### Settings page

The backend select filters the model select, so the models offered are the ones
that backend reported. Changing backends drops a model the new one does not
serve rather than carrying it over into a pin that cannot be honoured. A stored
value the server no longer has is shown as "not on this server" instead of
disappearing — the same rule the agent list already followed — and stored values
are rendered before the lookup returns, so pressing Save while the server is
unreachable cannot quietly clear a working configuration.

### AI Chat

The same two dropdowns on that page's own per-question fields, against whichever
server it is pointed at: `GET /ai-chat/wintermute/catalog`, with the page's
server-URL override as an optional `url` parameter. That parameter reaches no
further than `/ai-chat/ask` already does — the same `ValidateEndpoint` rules
apply, and the client token comes from Settings rather than from the request.

The lists are fetched when the Wintermute fields are first shown rather than on
page load, since the page opens on Claude, and refetched when the server URL
changes: a different server has different backends, and leaving the old ones on
screen is how a name that exists nowhere gets picked.

### Shared, rather than a second copy

`Wintermute.Catalog` and `Wintermute.Agents` in `internal/aiprovider`, where the
protocol, the endpoint validation and the HTTP client already live. Both pages
call the same lookup through their own thin proxy handler — Settings' over the
stored configuration, AI Chat's over the per-question one — instead of the two
of them growing separate clients. `request` now decodes into a typed value, and
a non-2xx reply carries its status, so a 404 can be reported as an older server
rather than as a wrong URL.

### Claude's model, too

`Claude.Models` over the Anthropic Models API (`GET /v1/models`), behind
`GET /ai-chat/claude/models`, so that page's Claude model field is a dropdown of
what this key can actually address. A model id typed by hand ages badly: the id
that was right last quarter answers with a 404 today, and the failure surfaces
on the question rather than on the field that caused it. It is a metadata call —
nothing here is billed as tokens — the page's endpoint override is carried
through and stays restricted to Anthropic's own host, and the list is fetched
only once the status call says a key exists, so an install without one makes no
request at all. A model the key is not offered is labelled rather than dropped,
since it is what the next question would be asked with.

### Not changed

Settings still has no Claude model field: every AI field in the app answers on
`aiprovider.DefaultClaudeModel`, and the per-question override lives on the AI
Chat page.

## 2026-08-20 (Risk & Crisis Exercises)

A bank's exercise programme usually rehearses its parts separately and fails at
the joins. The SOC handles the intrusion and nobody says the word "crisis". The
crisis team runs for six hours before anyone starts the four-hour regulatory
clock. The board is briefed on a version of events three hours old. Every part
passed; the institution failed.

New module holding the whole arc as one artefact — red team, detection,
incident, classification, crisis, continuity, communications, board,
authorities, recovery, after-action — so the seams are what gets tested. Built
for a financial entity in the Baltics. Page `/crisis-exercises`. Full
documentation in `CRISIS_EXERCISE.md`.

### `internal/crisisexercise`

Twelve-phase arc seeded on creation, each phase with a purpose, entry and exit
criteria, a lead role, a facilitator note and its own citations. A Master
Scenario Events List of injects on a T+ clock, each with the behaviour it should
provoke — ISACA's rule that expected and actual behaviour are tracked at every
step, because an inject with no expected action is entertainment. Inject types
include `stress` (removes what the plan assumed) and `ambiguous` (conflicting or
simply wrong information), which is what the first hours of a real incident are
made of and what most exercises omit.

A response log per inject, a decision log that records what else was considered
and who was entitled to decide, a roster, findings with severity and owners, and
immutable versions: a report circulated to a board cannot silently change when
somebody edits a finding.

Eight seeded scenarios, each drawn from something that has happened to a
European financial institution or to Baltic critical infrastructure — ransomware
in core banking, a hacktivist DDoS wave, compromise at the outsourced core
provider, hybrid connectivity loss, insider payment fraud, a malicious vendor
update, a TIBER-EU red team reaching the payment rail, and a disinformation-driven
digital deposit run. Each carries a trap door: the thing it is really designed to
expose, shown to the control team and withheld from players.

### The regulatory clock, modelled rather than narrated

`clocks.go` applies the DORA major-incident combination rule from Commission
Delegated Regulation (EU) 2024/1772 — critical services affected, *and* either
the data-losses criterion or two or more of the rest — and derives every
notification obligation the scenario started, on the right trigger for each.

The DORA initial notification is due at the **earlier** of four hours from
classification and twenty-four from awareness. Teams read the four hours as a
budget and the twenty-four as the real deadline; it is the other way round, and
the whichever-is-earlier arithmetic makes that visible. A team that classifies
late has not bought time, it has spent it.

Alongside it: the intermediate at 72 hours and the final at one month; the
national NIS2 early warning and notification from awareness; GDPR Article 33
from awareness of the *breach*, which is a different and usually later moment
than awareness of the incident; the ECB channel for a significant institution;
and the scheme and customer obligations that are shorter than all of them and
never in the playbook. Obligations that do not apply are returned marked
not-applicable rather than omitted, so the report can distinguish "we considered
GDPR and it did not apply" from "we never thought about GDPR".

The module applies the combination rule; it does not decide whether a criterion
is met. The real thresholds are numeric, sector-specific and revised, and
hard-coding one would produce an authoritative-looking answer that is wrong for
some entities and out of date for the rest.

Reclassifying rebuilds the clock set and **preserves the evidence already
recorded** against a regime that survives the change. Losing "we notified at
T+3h10" because someone corrected a downtime figure would destroy the
exercise's only record of the notification.

### Everything is referenceable

Objectives, phases, injects, decisions, notification clocks and findings can
each cite NIST 800-53 controls, this installation's Security NFRs, Regulation
Coverage clauses, policy clauses, risk register entries, and a seeded catalog of
frameworks and supervisory instruments (DORA articles and RTS, TIBER-EU and its
purple-teaming guidance, the ECB CROE and SSM reporting, ESRB/EU-SCICF, NIS2,
GDPR, ISO 22301/22361/22398/27035, NIST SP 800-84 and 800-61r3, CSF 2.0,
ATT&CK, ISACA's exercise guidance, FSB, CPMI-IOSCO, G7, ENISA, and the Baltic
national authorities).

Catalog references resolve through `internal/knowledge` rather than through a
second implementation, so a citation in an exercise report and a citation in an
agent's answer name the same record and link to the same page. The authority
catalog is seeded rather than stored because an entity must be able to cite DORA
Article 18 on a fresh installation without first having uploaded DORA.

The Coverage tab inverts the citations: which controls this exercise touched,
what cited them, and which had a finding raised against them. "We tested
incident response" becomes "we tested IR-4, IR-6, NFR-INC-02 and DORA Article
17, and two of them failed."

### Three documents, not one

The after-action report as a paginated PDF, for circulation. The controller's
MSEL as CSV, carrying the expected actions and evaluation notes, with the
exercise's TLP marking in the first column of every row so a printed copy left
on a table says what it is. And the player handout, which is the same exercise
with the answers stripped out — because handing players the document that says
what they are supposed to do turns an assessment into a rehearsal, and it has
happened. A test asserts the handout leaks neither the expected actions nor the
scenario's trap door.

### Generation and the expert seat

Everything works without a model: design by hand, run, record, report. An
exercise programme that stops when the model is unavailable is not a programme.

Generation is one call per phase, never one call for the whole exercise, because
a single answer covering twelve phases degrades badly in the last four. Each
call gets the institution, its jurisdiction and that jurisdiction's authorities,
the scenario, the objectives, the phase's own criteria, and a shortlist of
candidate references retrieved lexically. The model judges the shortlist rather
than browsing the catalog, which keeps the prompt small enough to reason over
and every citation traceable to why the candidate was offered.

Every generated inject records the model, the prompt hash and its own
confidence. Every generated citation resolves against the catalog before it is
stored, so a model that invents `XX-99` produces a reference marked
**unresolved** rather than one sitting in a client's report beside the real
ones. Regeneration replaces the model's own work and never touches a
hand-written inject — a designer who edited something made a decision.

Twelve expert personas: facilitator, board trainer, CISO, supervisory examiner,
communications lead, legal/DPO, red team lead, BCM lead, journalist, threat
intelligence analyst, evaluator, and the adversary. Two ways to use them —
Advisers, a conversation about the exercise, and Hot seat, which hands a persona
one inject and what the team actually did with it and lets them push back in
character. A team that has just written a holding statement learns more from
thirty seconds of a journalist's follow-up than from an hour on communications
principles.

The evaluator persona also drafts after-action findings from the record. Every
draft is marked as the model's proposal for a human to accept, edit or delete: a
finding is an accusation about an organisation, and nobody should be able to say
it was the software's.

Standing constraints on every persona prompt: say when you do not know, never
invent a control identifier or a threshold or a deadline, distinguish what the
regulation requires from what good practice suggests, and produce narrative
rather than attack capability. The adversary persona is asked by design what an
attacker does next, and its prompt refuses exploit code, malware and working
commands outright; a test asserts that constraint is still there.

### `internal/knowledge`

Two new read-only kinds, `exercise` and `exercise_finding`, so a Wintermute
agent can answer questions no other corpus can: *have we ever exercised our
major-incident classification, and how did it go?*, *which controls have we
actually tested rather than documented?*, *what have our exercises found about
escalation?* The control catalog describes an intention; the exercise record
describes an outcome, and without the second an agent answers the first question
from the first corpus and sounds confident. Exercises are in the compact index
for the same reason NFRs are: an installation holds a handful per year and
counting questions are answered by reading all of them.

The knowledge service is now built once in `app.Run` and shared by the API and
this module's reference resolver, rather than constructed inside
`registerKnowledgeRoutes` — which declines to serve the API without a token,
while the resolver needs the service either way.

### Elsewhere

- `internal/db` — thirteen `crisis_ex_*` tables on both backends, all cascading
  from the exercise.
- `internal/pageui` — "Crisis Exercises" under Compliance & Risk.
- `internal/app` — `/crisis-exercises` added to the protected prefixes and to
  the grantable page list. The whole write surface is behind the admin gate,
  including recording what happened during delivery: an exercise record is
  evidence a supervisor may read.
- Cloning an exercise carries the design and the citations and leaves the
  observations, findings and objective ratings behind, which is what makes a
  year-on-year comparison possible — ISACA's campaign approach rather than a
  fresh exercise each year.

### Known, not introduced here

`go test ./...` currently fails `TestGovulncheck` on seven Go standard-library
advisories against toolchain `go1.25.12`, all fixed in `go1.25.13`. Verified
present on `main` before this change with the same count. Per this repository's
policy the toolchain bump belongs on a `security-patches` branch and has not
been folded into this feature.

## 2026-08-14 (An agent that can read this installation)

Asked "how many of the Security NFRs are focused on network segmentation?", the
AI dock used to explain what it would need in order to answer and offer to work
through the list if someone pasted it in. It could not know the list was one
query away. Now it is: a read-only knowledge API over this installation's own
data, and an agent on the Wintermute server that uses it. Full documentation in
`AI_AGENT.md`.

Against the live catalog that question now answers with 26 NFRs matching either
word, 3 matching both, and the three named.

### `internal/knowledge`

Four GET endpoints under `/api/knowledge` — overview, index, search, item —
over Security NFRs, 800-53 controls and their links, Regulation Coverage
clauses and their findings, the policy library with its control claims, and the
risk register. A small tool surface on purpose: a model uses a short vocabulary
well and a long one badly.

Search returns two counts, `total_matches` (any term) and `total_all_terms`
(every term), because for a two-word question those differ — 26 against 3 here
— and quoting the first as the second turns a precise question into an inflated
answer. Each hit reports which terms it actually matched, so the borderline
records are visible rather than silently counted. The response also says that
matching is unstemmed, since "segmented" not matching "segmentation" is the
trap this data walks into; a test asserts the caveat is actionable by checking
the other form does find it.

For counting questions the honest primitive is the whole catalog: `index/nfr`
returns all ~109 entries compactly. It is refused for regulation clauses and
the like, where dumping everything is not an index but a denial of service
against the answer.

Everything is read-only — the package has no method that writes — behind a
`KNOWLEDGE_TOKEN` separate from `ADMIN_TOKEN`, because this credential lives in
another service's configuration and must never be able to change the catalog.
Without a token the API is registered only in local mode: it reads the whole
catalog, the policies and the risk register, and "we will set the token later"
is how that ends up exposed on a network.

### Choosing an agent

`aiprovider.WintermuteConfig` gained an `Agent`, sent when a session is opened,
so every question this application asks runs against a named agent on that
server — with its documents and its sources — rather than against a general
assistant. Settings → AI providers lists the agents fetched from the server
(proxied through `/api/settings/ai-providers/agents`, since the client token
must not reach a browser) and stores the choice in `ai.wintermute.agent`. An
agent the server no longer has is shown as such rather than silently dropped.

The Settings and AI Chat pages link to that agent's page on Wintermute for
document upload: that server owns the library, the extraction and the search,
and a second upload page here would be a second copy of all three.

### The consequence worth knowing

This works when grc's AI provider is Wintermute. Wintermute can forward the
turn to Claude, so the model is still whichever you choose — but pointed
straight at Claude, this application's AI has no access to this data and
answers as it did before.

## 2026-08-14 (Regulation Coverage: upload an EU regulation, get a mapped report)

New module `internal/regcoverage`, at `/regulation-coverage` in the Compliance
& Risk group beside Policy Coverage. Upload an EU regulation as a PDF and every
article is analysed against this installation's Security NFR catalog and NIST
SP 800-53: what the article requires, whether it imposes a security obligation
at all, which NFRs and controls satisfy it, what the catalog does not cover,
and practitioner commentary on meeting it well. The result is a versioned
report — an in-app page, a PDF, or JSON — that can then be interrogated in
conversation and corrected. Full documentation in `REGULATION_COVERAGE.md`.

This is the web counterpart to the `regmap` CLI and reuses its machinery rather
than growing a second copy: PDF/DOCX extraction, article and annex
segmentation, the framework profiles, and the embedded 800-53 catalog. What it
adds is the half a crosswalk does not answer — NFR mapping, relevance,
commentary, and a report someone can argue with.

### How a mapping is produced

Retrieval first, then judgement. A BM25 ranker (the scorer `internal/nfrenrich`
already ships, reused rather than reimplemented) shortlists ten candidate
controls and ten candidate NFRs per section out of the ~1200-item catalog, and
only that shortlist enters the prompt. The model judges the shortlist and is
told not to map to anything outside it — it never browses the catalog. Where
the upload is recognised as a framework with a curated regmap crosswalk (DORA,
NIS2, CRA, PCI-DSS), that crosswalk is supplied as a prior to confirm against
the text, and a mapping both proposed and confirmed is marked `curated`.

Three checks sit on the output, because none of it is trustworthy by
construction: every reference is resolved against the catalog, so an invented
control ID is tagged `unknown` rather than rendered into a client-facing report
as though it were real; every finding must quote its section verbatim, and one
that cannot is tagged `unverified quote` (whitespace, case and punctuation are
normalised first, so a re-wrapped quote passes and an invented one does not);
and every finding records the model and the SHA-256 of its prompt.

### Versions, chat and revisions

Report versions are immutable. `Revise` re-analyses one section with the
reviewer's correction and appends the next version; the previous version keeps
its JSON snapshot and stays downloadable exactly as it was, because these are
artifacts that get sent to clients. The chat is grounded in the report rather
than the raw regulation and carries its own history through the `aiprovider`
harness — including resuming a Wintermute server-side session rather than
resending the transcript, the same rule the AI Chat dock follows.

### The original document

The uploaded file is stored as it arrived, in its own table so a listing never
drags a multi-megabyte blob it does not read, and served back byte-identical at
`/regulation-coverage/:id/source` — inline for PDFs and text, `nosniff` and a
`sandbox` CSP on the response, displayed in an iframe beside the report so the
analysis can be read against the regulation as published.

### Supporting changes

- `internal/aiprovider` gained nothing here; this module uses the `History` /
  `SessionID` plumbing added earlier today.
- `internal/regmap/profile`: `Parse` (YAML from memory) and `LoadFS`, so the
  profiles can be embedded. `regmap/profiles/embed.go` embeds them; the CLI
  still reads the same files from disk.
- `internal/regmap/ingest`: `ExtractBytes`, for a document that arrived as an
  upload rather than a path.
- New `eu-generic` profile: article and annex segmentation with no curated
  knowledge, the fallback for an instrument with no profile. It carries no
  detect patterns, so it never wins detection — a report built on it says
  "generic segmentation" on its face, because it means nothing curated went
  into the analysis.
- `registerReportingRoutes` now returns its renderer, so Regulation Coverage
  shares the one pooled headless browser instead of starting a second.
- Six `reg_coverage_*` tables in both the SQLite and Postgres schemas.

### Cost and failure behaviour

One model call per section plus one for the summary, run synchronously behind a
45-minute bound and cancelled if the reader navigates away. Uploading is
separate from analysing, so a document that segmented badly costs nothing. A
section that fails is recorded and the run continues — one bad section should
not lose the other ninety-nine. Sections with almost no body (an inline "…
pursuant to Article 20" cross-reference that the segmenter reads as a heading)
are skipped rather than analysed: they can only produce an ungrounded finding,
and they appear in the report as unanalysed.

### UI note

The report body is rendered once and used for both the page and the PDF, so the
two cannot drift. On the page it needs a handful of narrow `!important` rules:
the global theme layer paints every `<p>` with `--muted`, which is right for
incidental page prose and wrong here, where the prose is the deliverable — the
findings were rendering as grey secondary text in the dark themes.

## 2026-08-14 (Ask AI dock answers in place)

The global "Ask AI" dock at the bottom of every page used to throw the question
away and open `/ai-chat?q=...` in a new tab, so the answer arrived somewhere
other than where it was asked. It now expands in place: the dock grows a
transcript panel above the input (`#global-ai-dock-panel`), posts the question
to `POST /ai-chat/ask` from the current page, and appends the answer under the
question. The reader keeps their page, their scroll position and their context.

Provider selection stays out of the dock — it asks `GET
/ai-chat/wintermute/status` once, on the first question, and uses Claude when a
key is configured, otherwise a configured Wintermute server. When neither is
set, the panel says "No AI provider is configured. Set a key in Settings."
instead of failing silently. Errors and the pending "Thinking…" placeholder
render as notes in the same transcript.

The panel is capped at `min(46vh, 420px)` (40vh under 700px wide) and scrolls
internally, so a long answer never buries the page behind it. Header controls:
"Full chat ↗" (opens `/ai-chat` for provider/model/system-prompt control),
"Clear", and "Close" to collapse back to the bare input. The dock starts
collapsed and is unchanged in height until the first question. Messages are
built with `createTextNode`, not innerHTML, so model output cannot inject markup
into the host page.

### The dock carries conversation context

A follow-up in the dock now means what it says: "and the second one?" reaches
the model with the question and answer above it. `aiprovider.Request` grew a
`History []Message` (roles `user`/`assistant`, oldest first, excluding the
prompt) and a `SessionID`, and `Response` returns the `SessionID` a turn
belongs to. The harness itself stays stateless — it holds no transcript.

The two providers carry context the way each one works:

- **Claude** replays the transcript as prior messages. `claudeMessages` joins
  consecutive same-role turns and drops a leading assistant turn, because the
  Messages API requires alternating roles starting with a user turn and a
  transcript with a failed turn in it does not always satisfy that — that would
  otherwise surface as an HTTP 400.
- **Wintermute** posts to the session the previous answer came from, so the
  server keeps owning the transcript and nothing is resent. A `History` with no
  `SessionID` (a caller holding its own transcript, or an expired session) is
  folded into the message text instead, since that server takes text only.

`POST /ai-chat/ask` accepts `history` (`[{role, content}]`) and `session_id`,
and returns `session_id`. The transcript is client-held and therefore bounded
server-side by `boundedHistory`: blank turns and unknown roles are dropped, and
only the newest 20 turns / 24000 characters are forwarded — an unbounded one
would grow every request until the model rejected it, at the operator's
expense. The body itself is capped at 512 KiB (`http.MaxBytesReader`), which is
new: the body used to be one typed question.

In the dock, the transcript is recorded only when a turn succeeds, so a
question that errored does not sit in the model's context for every later turn,
and **Clear** drops the history and the session id along with the visible
messages. **Close** only collapses the panel — the conversation survives it.

### /ai-chat carries context too

The full page follows the same rules as the dock: the transcript is held by the
page and sent with each question, a Wintermute answer's `session_id` is carried
back so the server keeps owning that transcript, and a turn is recorded only
when it succeeds. **Clear** drops the conversation and the session along with
the visible messages.

Two things are specific to the page, because it can change per question what
the dock cannot. A Wintermute session is pinned to the server URL, backend and
model it was opened with, so changing the provider or any of those fields drops
the session id — the next question opens a new session and sends the transcript
with it, rather than continuing against a pin that is no longer what the form
says. And since the transcript is resent, the Conversation header now states
what is being carried ("3 turns of context"), so the cost of a long thread is
visible rather than inferred from the bill.

## 2026-08-13 (AI Chat keys move to Settings; chat-first layout)

Credentials are now set in **one** place. The AI Chat Gateway no longer has an
API key or client token field, and `POST /ai-chat/ask` no longer accepts an
`api_key` — `aiChatRequest` has no such field, so a key cannot ride through the
browser to this endpoint at all. `aiChatProvider` resolves both credentials
from `storedAICredential`, which reads the Settings store with the existing
environment fallback (`ANTHROPIC_API_KEY`, `WINTERMUTE_TOKEN`), so an install
configured either way is unaffected.

What stays per-question is what was never a secret: the **provider dropdown**
(Anthropic Claude API, or Wintermute routing to a self-hosted backend), the
model, the Claude endpoint override, and the Wintermute server URL, backend and
model. A reviewer can still put one question to a different model or backend
without touching the install-wide setting.

Error text follows the move: "no Anthropic API key: set one in Settings" rather
than "paste one above". The page reports readiness up front instead — two chips
next to the title read "configured" / "not configured" per provider from
`GET /ai-chat/wintermute/status` (which still returns only whether a credential
exists, never the value), beside a link to Settings.

That status endpoint now sources its Wintermute defaults from the Settings
preferences rather than reading `WINTERMUTE_URL`/`_BACKEND`/`_MODEL` directly.
`Preference` falls back to those same variables, so an env-configured install
prefills exactly as before, and one configured in the Settings page — which
previously left the chat page blank — now prefills too.

### Layout

The AI Chat page was a form column beside a chat column capped at `62vh`, with
the composer buried at the bottom of the form. It is now chat-first: a fixed
sidebar panel (provider, model/endpoint or URL/backend/model, system prompt,
usage) beside a conversation panel that fills the viewport height, with the
question composer docked under the transcript. Enter sends, Shift+Enter adds a
line. Messages are bubbles — user right, assistant left, capped at 80ch for
readability — and the whole layout collapses to one column under 980px.

Colour fixes for the global themes, which repaint `--ink`/`--muted` with
`!important`: the composer strip and the Wintermute notice are transparent
rather than parchment-tinted (the strip read as a grey band on the dark themes,
and the notice put a theme-dark `<code>` chip on a cream box), and the status
chips carry literal text colours on their own opaque pills.

### Settings page

The credential cards sit in an auto-fit grid, so the two short forms are side by
side on a wide screen instead of stacked full-width. The page also rendered
`pageui.Nav` **outside** `<main>` — the only page that did — which meant the
shared sidebar script skipped it and it showed a row of unstyled links across
the top. The nav moved inside `<main>`, so Settings now gets the same sidebar
shell and full-width content area as every other page.

Verified by rendering both pages in a headless browser at 1600px, 1400px and
820px with each provider selected: no credential field remains on the chat page
(no `type="password"` input, no `api_key` in the payload), the chips report the
Settings state, the composer stays docked under the transcript, and the layout
stacks cleanly on the narrow viewport. `go fmt`, `go vet` and `go test ./...`
(including `TestGosec` and `TestGovulncheck`) are clean.

## 2026-08-13 (AI Chat routed through the harness; SSRF fix in the harness)

The AI Chat gateway now asks through `internal/aiprovider` rather than carrying
its own copy of both protocols. Every AI surface in the app is on the harness.

### SECURITY: the harness allowed plaintext to a public host

`aiprovider.ValidateEndpoint`, added in the previous entry, accepted `http://`
to **any** host. The client token rides on every Wintermute request, so a
Settings entry of `http://wintermute.example.com` would have sent it in
plaintext across the internet.

The AI Chat code being replaced already had the correct rule — `http` only for
a loopback, private or link-local literal, or the name `localhost` — and
porting the transport is what surfaced the gap. `ValidateEndpoint` now enforces
the same rule, with the same reasoning as the original: a hostname is **not**
resolved to decide this, because a DNS lookup here would be both a TOCTOU race
and a request to an attacker-chosen name.

The practical consequence, unchanged from the old AI Chat behaviour: a
Wintermute server reached by hostname needs `https`, or its IP address.
`http://192.168.1.50:8080` works; `http://nas.local:8080` does not.

### AI Chat on the harness

`askClaude`, `askWintermute` and their JSON plumbing are gone — about 170 lines
of duplicated protocol, along with the now-dead `claudeEndpointURL`,
`claudeAPIVersion`, `claudeDefaultModel`, `wintermuteDefaultTitle`,
`aiChatTokenUsage`, `wintermuteAnswer` and the app package's `isPrivateHost`
(the harness has its own). `postJSON`, `stringField`, `validatedEndpoint` and
`hostAllowed` stay: other modules use them.

The page keeps choosing a provider per question rather than per install, which
is why it does **not** use the Settings router. `aiChatProvider` builds a
provider from the request and falls back field by field to the stored
configuration, so a reviewer can still put one question to a different model or
try a key before saving it — while the transport underneath is the shared
harness. Verified: a typed credential still wins, and a blank form resolves
entirely from Settings.

The Claude endpoint override keeps its `api.anthropic.com` allowlist, now
converted to an SDK base URL by `validatedClaudeBaseURL`. That field can
retarget the path, not the server.

`Response.Refused` is honoured here too, so a refusal reads as a refusal rather
than as an empty answer.

### Tests

The nine tests in `ai_chat_test.go` all covered the deleted implementation.
Their behaviour now lives in `internal/aiprovider`'s tests (turn flow, endpoint
rules including the plaintext-host cases, usage extraction, and the failure
modes). They are replaced by tests for what this page adds on top: the
per-request/stored precedence for both providers, the error messages pointing at
Settings, and the Claude host allowlist.

### Verified end to end

Against a stand-in wintermuted server: an AI Chat question with an empty key and
empty endpoint field was answered by `local-8b` / `llama-3.1-8b` entirely from
Settings; a deliberately wrong token typed into the form produced a 401 from the
server, confirming the typed value still takes precedence; and the usage row was
logged against the serving provider and model.

### Also

Two comments in `nfrenrich/analyze.go` referenced `internal/assistant`, a
package that no longer exists in this repository. Corrected. There is no
separate assistant module left to route.

## 2026-08-13 (AI provider harness: local network models, not just the cloud)

AI questions can now be answered by a model on your own network. The new
`internal/aiprovider` harness sits in front of every AI field and routes each
question to Claude or to a Wintermute server, which in turn dispatches to a
self-hosted model (llama.cpp, Ollama, vLLM) or on to Claude.

grc does not implement local-model clients: wintermuted already abstracts them
behind one token, so the harness holds a server URL and a client token and
never sees a model endpoint or a vendor key for that path.

### Added

| Path | Purpose |
|---|---|
| `internal/aiprovider/provider.go` | `Provider`, `Request`, `Response`, `Probe` |
| `internal/aiprovider/claude.go` | Claude over the Anthropic Messages API |
| `internal/aiprovider/wintermute.go` | Session/turn flow, endpoint validation, discovery |
| `internal/aiprovider/router.go` | Provider selection, usage logging, status |
| `internal/settings/preferences.go` | Non-secret settings, in the existing `app_state` table |

### Choosing a provider

The Settings page gains a provider selector and the Wintermute server URL,
backend and model, alongside a **Test connection** button.

- **auto** prefers Wintermute when it is configured and falls back to Claude.
  A local model is cheaper and more private, and an operator who configured one
  meant to use it; the fallback means selecting auto cannot leave the app unable
  to answer.
- **claude** and **wintermute** are explicit and do **not** fall back. Someone
  who selects Wintermute may be doing so because questions must not leave the
  network, and quietly reaching for the cloud would break exactly that
  expectation. An unusable explicit choice is an error, not a silent reroute.

The default is `claude`, so this release changes no existing install's
behaviour until someone opts in. `WINTERMUTE_URL`/`_BACKEND`/`_MODEL` still
work as fallbacks for an install already configured through the environment.

### Discovery

**Test connection** calls the server's `GET /api/v1/me`, so one request checks
the URL, the token and discovery together. It reports the backends the server
advertises along with its default and fallback, so an operator can see which
local models are reachable and copy a name into the backend field rather than
guessing. A backend pinned in Settings that the server does not have is
reported then, rather than failing later at ask time with a vaguer message.

`ValidateEndpoint` rejects a non-http(s) scheme, a missing host, and a URL
carrying embedded credentials — those would be sent on every request and
logged, and the client token belongs in Settings.

### NFR Enrichment routed through the harness

`ClaudeAnalyzer` is replaced by `RoutedAnalyzer`, which asks through the
harness. The module no longer constructs an Anthropic client, holds a key, or
knows that Claude exists; the credential, model and provider all resolve per
request. `NFR_ENRICHMENT_MODEL` still overrides the model. Refusals survive the
indirection: `aiprovider.Response.Refused` carries `stop_reason: refusal`, which
is a successful response with empty or partial content and would otherwise read
as a malformed answer.

Usage is logged by the router against **what actually served the turn**, which
for Wintermute is not always what was asked for — a failed backend is retried
against the server's fallback.

### Verified end to end

Against a throwaway instance and a stand-in wintermuted server: configuring the
URL, token and backend, then running an NFR enrichment analysis over an uploaded
document, produced **42 model calls, every one logged as
`wintermute` / `llama-3.1-8b`, with no Anthropic API key configured at all.**
That is a real AI field in this app answered entirely by a local model. The
connection test reported all three advertised backends, and a deliberately
wrong backend name was caught by the test rather than at ask time.

The unit tests cover the turn flow against a stub server — including the system
prompt being folded into the message text, since wintermuted derives its own
system prompt and takes message text only — plus the failure modes (bad token,
server error, a turn that ends waiting on tool calls, an empty reply), endpoint
validation, and every routing and fallback combination.

### Not changed

The AI Chat page keeps its own per-conversation provider picker. Choosing a
provider per question is a legitimately different interaction from the
install-wide default this harness sets, and folding one into the other would
lose that.

## 2026-08-13 (Settings module: one place for AI credentials)

AI credentials were handled two incompatible ways. AI Chat took a key pasted
into a browser form on every visit and sent it in each request body; NFR
Enrichment read `ANTHROPIC_API_KEY` once at startup and told the operator to
"set `ANTHROPIC_API_KEY` and restart". Neither could share with the other, and
a third AI feature would have invented a third mechanism.

There is now a Settings page under Admin. A key set there is used by every AI
field in the app and takes effect immediately, with no restart.

### Added

| Path | Purpose |
|---|---|
| `internal/secrets` | Master key resolution and AES-256-GCM seal/open |
| `internal/settings` | Credential store, resolution, and the admin API |
| `internal/app/settings_page.go` | The `/settings` page |
| `app_secrets` table | On both SQLite and PostgreSQL |

### Where the master key comes from

`$GRC_SECRET_KEY` (base64, 32 bytes) first; then a key file, at
`$GRC_SECRET_KEY_FILE`, else `$STATE_DIRECTORY/secret.key` — which systemd sets
from `StateDirectory=grc` in the unit — else beside the SQLite database; and
failing all of that a fresh key is generated at that location with mode `0600`.

**It is never placed in the working directory of a checkout.** The
2026-08-07 entry records an encryption key that lived beside the repository and
was swept into a commit, which made the encryption it protected worthless. A
key file readable by other accounts is rejected with the `chmod` that fixes it,
and the page reports which source won so this is visible rather than assumed.

A missing master key disables storing credentials but is deliberately not
fatal: the environment fallback still resolves, so an existing deployment keeps
working and the page explains why saving is unavailable.

### Storage and resolution

Values are AES-256-GCM sealed and base64-encoded, so both backends store TEXT
rather than diverging over BLOB/BYTEA. The credential's name is passed as the
AEAD additional data, so a ciphertext moved to another row will not decrypt
there. Each row carries `updated_at`/`updated_by`.

Resolution prefers a stored credential and falls back to the environment
variable, so an install that has only ever used `ANTHROPIC_API_KEY` is
unaffected until someone saves a key in the page. Clearing a stored credential
falls back rather than disabling the feature, and the page says so before the
button is pressed. A row that cannot be decrypted — after a rotated or replaced
master key — is treated as absent and falls back too, rather than taking every
AI feature down.

Credentials are write-only in the UI: set, replace and clear. A stored value is
never sent to a browser. The page shows whether one is configured, which source
is active, and the last four characters so two keys can be told apart.

### Consumers

`nfrenrich.NewClaudeAnalyzer` now takes a `KeyFunc` consulted per request
rather than reading the environment once at construction — this is what removes
the restart. The SDK client is rebuilt only when the resolved key changes, so
the common path does not construct one per request. A nil `KeyFunc` falls back
to `ANTHROPIC_API_KEY`, which keeps existing callers and tests working.

The AI Chat gateway falls back to the stored credential when a request omits
one. An explicit key in the request still wins, so the page stays usable for
trying a different key without changing the install-wide one.

### Security

`go test ./...` covers the new packages, so `TestGosec` applies to them. One
`#nosec G101` remains, line-scoped: gosec matches the Go identifiers
`AnthropicAPIKey` and `WintermuteToken`, but those constants are `app_secrets`
row names, not credentials.

### Verified

Against a throwaway `LOCAL_MODE` instance on a spare port and database: the
master key was generated in `STATE_DIRECTORY`; a saved key was confirmed
non-plaintext in `app_secrets`; NFR Enrichment reported `configured: true`
moments after the save **in the same process**, and `false` again after a clear;
and AI Chat's "no key" error changed to an upstream authentication error once a
key was stored, confirming the stored value is what gets sent.

### Follow-up: the AI Chat page ignored stored credentials

The server-side fallback landed working, but the AI Chat page still refused to
use it. Two client-visible gaps, both fixed here:

- The page's submit handler returned early with "Anthropic API key is required"
  whenever its key field was empty, so the request was never sent and the
  server-side fallback never ran. It now blocks only when the server has no
  credential either — the same shape the Wintermute branch beside it already
  used.
- `GET /ai-chat/wintermute/status` reported `token_configured` from
  `os.Getenv("WINTERMUTE_TOKEN")` alone, so a token stored in Settings was
  invisible to the page. Availability now comes from the same resolver the
  gateway uses, covering stored and environment credentials, and the payload
  adds `claude_configured` for the Anthropic side. Neither credential is
  returned — only whether one exists.

The key fields are now optional rather than required: when a credential is
configured server-side their placeholder reads "Using the key from Settings —
paste one only to override it", and an explicitly pasted key still wins.

Verified on a throwaway instance: `claude_configured` and `token_configured`
flipped to true immediately after saving each credential, and a request with an
empty `api_key` — exactly what the browser sends with a blank field — reached
Anthropic and returned an upstream authentication error for the test value,
confirming the stored key was the one sent.

### Also restored: `.gitignore`

The repository had no `.gitignore` — the one the 2026-08-07 entry added was
lost when history was squashed to `init`/`rebrand`, and both `*.aichat.key`
files are tracked in `HEAD` again along with `users.db`. The ignore block is
restored here (`*.aichat.key`, `*.gcal.key`, `/ai_chat.key`, the databases and
their `-shm`/`-wal` sidecars).

**Still outstanding, and an operator decision:** ignore rules do not apply to
already-tracked files. Untracking them needs
`git rm --cached users.db.aichat.key local.db.aichat.key users.db`, with
`--cached` deliberate so the files stay on disk and existing encrypted sessions
remain readable. The keys are in history regardless; treat them as compromised.

## 2026-08-13 (manage-user.sh now uses the service's own database)

Fixes a silent failure: resetting a password on a deployed host, restarting the
service, and finding the account still could not log in.

### The cause

The service reads `SQLITE_PATH` from its `EnvironmentFile` (`/etc/grc/grc.env`),
normally `/var/lib/grc/users.db`. `scripts/manage-user.sh` did not look at that
file. With no `--db` flag, no `config/db.env`, and no exported `SQLITE_PATH`,
the chain fell through to `userctl`'s built-in default — the *relative* path
`users.db` — which, because the script has already `cd`'d to the repo root,
resolved to `<checkout>/users.db`.

The new hash was written to a database the service never opens. Nothing failed;
the reset reported success. `sudo` made it more likely, since it strips
`SQLITE_PATH` from the environment.

### The fix

`/etc/grc/grc.env` is now a step in the backend precedence chain, below an
explicit `--db` or an exported `DATABASE_URL`/`SQLITE_PATH` and above the local
`config/db.env` dev profile — on a host with the service installed, the
service's database is the one a password reset has to touch. `$GRC_ENV_FILE`
overrides the location, matching `update.sh`, `setup.sh` and `verify-install.sh`.

Values are read through the same `sudo -n` subshell helper `verify-install.sh`
uses: the file is root-owned `0600`, `-n` means a non-interactive run degrades
to empty rather than hanging on a password prompt, and reading in a subshell
keeps the rest of the file (`ADMIN_TOKEN` and friends) out of the caller's
environment.

When the service's database is known but the run is pointed elsewhere — an
explicit `--db`, an exported variable, or the dev profile — the script now
prints a warning naming both paths and the `--db` argument that would correct
it. The warning mirrors `userctl`'s own resolution order, so when an
environment variable wins it names the database that variable selects rather
than the built-in default.

Behaviour on a machine with no service installed is unchanged.

## 2026-08-13 (added regmap: regulation → NIST 800-53 crosswalk tool)

New operator CLI, `cmd/regmap`, that ingests a cybersecurity or resilience
regulation, maps its requirements to NIST SP 800-53 Rev 5 controls, and emits an
auditable crosswalk. It is a terminal tool, separate from the web application —
no routes, handlers or DB tables were touched — but it shares this module's
embedded NIST control catalog so both surfaces map against the same controls.

The tool is human-in-the-loop by design: a three-gate state machine where
nothing reaches `approved` without a named reviewer.

    ingest → [GATE 1 segmentation] → map → [GATE 2 mappings]
           → suggest → [GATE 3 suggestions] → report

### Added

| Path | Purpose |
|---|---|
| `cmd/regmap` | Entry point, following the existing `cmd/userctl` pattern |
| `internal/regmap/cli` | Cobra command tree: `frameworks`, `ingest`, `review`, `map`, `suggest`, `report`, `status` |
| `internal/regmap/profile` | Framework profile loading, validation, section matchers, framework auto-detection |
| `internal/regmap/ingest` | PDF/DOCX/TXT extraction and the framework-agnostic segmenter |
| `internal/regmap/{requirement,mapping,nist}` | Domain types, the seed-crosswalk resolver, the control catalog |
| `internal/regmap/review` | The three interactive gates — the only package permitted to approve anything |
| `internal/regmap/{report,state,suggest}` | Markdown/JSON/CSV output, the YAML state store, the Anthropic suggestion client |
| `regmap/profiles/*.yaml` | Starter profiles: DORA, PCI DSS v4.0, NIS2, EU Cyber Resilience Act |
| `regmap/testdata/*.txt` | Regulation excerpts used to exercise the loop |

The source framework is not hardcoded. Segmentation strategies (`article`,
`numbered`, `annex`, `regex`, composable), classification rules and the seed
crosswalk all live in the profile, so adding a regulation means adding a YAML
file rather than editing the pipeline. All four starter profiles run through one
code path.

### Reuses the existing NIST catalog

`internal/regmap/nist` reads `seeddata.ControlCatalogJSON()` — the same embedded
dataset `internal/controlcatalog` serves — rather than carrying its own copy.
Base controls only by default (323 of the 1193 entries); `--with-enhancements`
opts into control enhancements such as `AC-2(1)`, which are excluded normally
because they would bloat the control-ID list sent to the model on every
suggestion without improving a regulation-level mapping. `--catalog <file>`
still accepts a YAML override.

The embedded dataset decodes from a JSON map, so the loader imposes a sort;
a test asserts repeated loads produce identical ordering, since report output
has to be reproducible. Another test asserts every control ID referenced by
every shipped seed crosswalk resolves against that catalog.

### New dependencies

`github.com/spf13/cobra`, `gopkg.in/yaml.v3` and `github.com/ledongthuc/pdf` are
now direct requirements. PDF extraction prefers the `pdftotext` binary when it
is on PATH (its layout mode preserves headings) and falls back to the Go
library, reporting which was used.

### Security

`go test ./...` covers the new packages, so `TestGosec` applies to them. Twelve
findings were resolved before this landed:

- `state.Store.write` creates the data directory `0750`, not `0755` — pipeline
  state holds regulatory source text and review decisions.
- `report --out` writes `0600` via `os.OpenFile` for the same reason.
- `pdftotext` is invoked with an absolute input path, closing the
  argument-injection hole a source path such as `-v` would otherwise open.
- `$EDITOR` is validated before use: bare command name only, resolved through
  `exec.LookPath`, rejected if it carries arguments or shell metacharacters.
- Deferred `os.Remove` / `Close` calls have explicit error discards.

The remaining `#nosec` annotations are line-scoped with a stated reason, and
cover only paths inherent to a CLI: reading the document named by `--in`, the
profiles under `--profiles-dir`, an explicit `--catalog`, the `--out`
destination, and the tool's own temp file.

### Reviewer guarantees, enforced by tests

`internal/regmap/cli/approval_guard_test.go` walks the regmap tree with the Go
AST and fails if any package outside `internal/regmap/review` writes
`StatusApproved`. Reading the constant is allowed — reports compare against it —
but no code path may set it. A companion test plants a violation to confirm the
guard actually bites. Review sessions save after every decision and resume where
they left off; `--auto-approve` is per-gate and explicit; a gate refuses to run
at all when stdin is not a terminal.

### Running it

    go build -o bin/regmap ./cmd/regmap
    ./bin/regmap frameworks
    ./bin/regmap ingest --in dora.pdf
    ./bin/regmap review --gate 1

The `bin/` prefix matters: a `./regmap` binary at the repo root would collide
with the `regmap/` assets directory. `bin/` and `regmap/data/` are gitignored —
this change also adds a `.gitignore`, which the repository did not previously
have (existing ignores live in `.git/info/exclude`).

Pipeline state defaults to `regmap/data/` (`state.yaml`, `requirements.yaml`,
`mappings.yaml`), sorted by requirement id for clean diffs and written
atomically. `ANTHROPIC_API_KEY` is read from the environment for
`suggest --with-llm`; that path has not yet been exercised against the live API.

## 2026-08-12 (renamed the project from carelockconsulting to grc)

Every reference to the old `carelockconsulting` / `CareLock Consulting` name is
gone; the project is now `grc` in code and `GRC` in anything a user reads. This
was a full de-brand, not just a module rename, so it touches deployment,
persisted client state and the historical entries in this file.

### Renamed

| Was | Now |
|---|---|
| Go module `carelockconsulting`, all `carelockconsulting/internal/...` imports | `grc`, `grc/internal/...` |
| Binary, systemd unit, service user, `/etc/carelockconsulting/`, `/var/lib/carelockconsulting`, `/usr/local/share/carelockconsulting` | the same paths under `grc` |
| `deploy/carelockconsulting.{service,nginx,env.example}` | `deploy/grc.{service,nginx,env.example}` |
| `CARELOCK_*` script env overrides (`CARELOCK_ENV_FILE`, `_BIN_PATH`, `_SERVICE_NAME`, `_SERVICE_USER`, `_DOC_DIR`) | `GRC_*` |
| Page titles `<Page> · CareLock Consulting`, the home `<h1>`, the footer, `CareLock AI Chat` | `GRC` |
| Auth cookie `carelockconsulting_auth_session` | `grc_auth_session` |
| `localStorage` key `carelockconsulting_jira_connection`, `carelock-chaos-{interval,density}` | `grc_jira_connection`, `grc-chaos-*` |
| Response headers `X-Carelock-{Bundled,Reason,Render-Log}` | `X-GRC-*` |
| JS globals `window.CarelockChaos`, `window.CarelockRain` | `window.GRCChaos`, `window.GRCRain` |
| NFR enrichment `User-Agent: CareLockConsulting-NFR-Enrichment/1.0` | `GRC-NFR-Enrichment/1.0` |
| `templates/latex/carelock.sty` and its `\usepackage{carelock}` | `grc.sty`, `\usepackage{grc}` |
| Report branding `OrgName` / sample assessor `Care Lock Consulting` | `GRC` |
| Postgres role/database `carelockconsulting` (docs only) | `grc` |

Historical entries in this file were rewritten too, so the old name no longer
appears anywhere in the repository. Earlier entries therefore describe past
work using the current name — including the entry below that records the
original `go_rcsa` → (now) `grc` module rename.

### Deploying this

The rename breaks state that outlives a restart, deliberately:

- **Everyone is logged out.** The session cookie changed name, so existing
  cookies are ignored and every user re-authenticates once.
- **Saved Jira connections are lost.** They live in `localStorage` under a key
  that changed; users re-enter the connection details once.
- **The existing install needs manual migration** — the service, service user,
  env file and data directory all changed name. Stop and disable the old unit,
  move `/var/lib/carelockconsulting` to `/var/lib/grc` and
  `/etc/carelockconsulting/carelockconsulting.env` to `/etc/grc/grc.env`, then
  run `scripts/setup.sh`. `update.sh` alone will not do this; it now points at
  the new paths and would treat the deploy as a fresh one.

`templates/out/*.pdf` are checked-in build artifacts that still contain the old
name inside the PDFs. They are regenerated by `templates/build.sh`, which needs
`typst` / `tectonic`; neither was available when this rename was made.

Verified with `go build ./...`, `go fmt ./...`, `go vet ./...` and
`go test ./...` (including the `TestGosec` / `TestGovulncheck` gates) — all
clean. The two `internal/reporting` golden files were regenerated with
`go test -run Golden -update`; their only diff is the brand string and the
base64 logo that embeds it.

## 2026-08-11 (CRM, Company and Productivity moved to wintermute)

The CRM, Company and Productivity tabs are gone from this application. Their
functionality now lives in wintermute (`/home/l3d/go/wintermute`), which keeps
this app to what it is actually for — the control catalog, security NFRs, risk
register, policy authoring and the reporting over them — and puts the
practice-management side in the tool that is already a personal server.

### Moved

| Was here | Now |
|---|---|
| `/crm/*` (8 pages) | wintermute CRM view, `/api/v1/crm/*` |
| `/company` | wintermute Company view, `/api/v1/company` |
| `/todo` | wintermute Tasks view, `/api/v1/todo/*` |
| `/assistant` | wintermute's own chat, with the task tools on its agent |
| Google Calendar sync | **not moved — see below** |

Deleted here: `internal/crm`, `internal/company`, `internal/todo`,
`internal/gcal`, `internal/assistant`, their routes and page-permission
entries, their tables in both the SQLite and PostgreSQL schemas, their `dbsync`
rows, and `CRM_FRAMEWORK.md` / `COMPANY_INFO.md` / `TASKS_AND_ASSISTANT.md`
(now `docs/crm.md`, `docs/company.md`, `docs/tasks.md` in wintermute).

### Stayed

**Templates** (`/templates`, `/templates/manage`) was under the Company tab but
exists to render policy documents authored by `internal/policydocs`, which is
not moving. Splitting the renderer from the thing it renders would have coupled
two applications at runtime for no gain. The Company nav group is now
**Documents** and holds the two template pages.

**AI Chat** (`/ai-chat`) was under Productivity and stayed for a related
reason: it was rebuilt earlier today with a Wintermute provider, so moving it
into wintermute would produce a chat page whose "Wintermute" option points at
its own server, sitting next to wintermute's own chat. It has joined the
Reporting & Data group.

### The one real coupling, and what it cost

Policy documents carry a `client_id` into the CRM, and the cover page prints
the client's name — resolved live through `crm_clients` in `documentColumns`.
With the CRM gone, that subquery had nothing to resolve against.

`client_name` is now **stored on the policy document** rather than resolved.
That is not a workaround; it is the more correct record. A policy is an issued
artifact, and the client it was issued to is a fact about that document, not a
live foreign key — under the old shape, renaming a client in the CRM silently
rewrote the cover page of every policy already approved for them. A migration
(`ensureColumn`) adds the column, and `client_id` is kept so existing rows
retain the link.

### Single-user, by decision

Every moved table carried an `owner` column scoping rows to a signed-in user.
Wintermute has no user accounts — it authenticates registered *clients* by
bearer token — so the scoping is gone rather than stubbed: a parameter nothing
scopes on reads as a guarantee that is not being made. This was a deliberate
choice, and it means the moved modules are single-operator.

### Google Calendar sync did not move

`internal/gcal` (1,584 lines excluding tests) is deleted here and **not ported**.
It is an OAuth flow with its own AES-256-GCM token store, a key file beside the
database, and a `google.golang.org/api` dependency, and it is a bigger piece of
work than the three modules it hung off. Tasks moved without it, so due dates no
longer mirror into a Google Calendar. Flagging rather than silently dropping it:
if that sync matters, it is a follow-up, and the deleted code is in this repo's
history.

### Also

- `TestSync_CarriesTheCompanyProfile` and the dbsync FK-chain fixtures used
  `company_profile` and `crm_*` as their representative tables. They now use
  `doc_template_brand` (the other single-row table) and the
  `nfr_source_documents` -> `nfr_source_chunks` -> `nfr_enrichment_proposals`
  chain, which exercise the same two behaviours.
- `RUNTIME_ARGS.md` loses its Assistant and Google Calendar sections;
  `ANTHROPIC_API_KEY` is still read here, now only by AI Chat and NFR
  enrichment.
- `go mod tidy` after the deletions.

## 2026-08-11 (Security NFR AI enrichment module — internal/nfrenrich)

A new module that reads uploaded or fetched security documents and proposes
enrichments to the Security NFR catalog. The worked example is the one that
motivated it: a document describing how TLS is actually terminated at the edge,
against an NFR whose `implementation` field says "TBD".

Everything it produces is a **proposal**. `AcceptProposal` is the only function
in the package that writes to a security NFR, and it is reachable only from a
review action. Analysing a hundred documents changes no catalog row.

### Why it is shaped this way

`POLICY_MODULE_FRAMEWORK.md` §2.1–2.5 already committed this repo to rules for
AI-assisted work on the catalog. They are enforced structurally here rather
than left as advice, because a hallucinated control mapping that survives to a
client audit is a billable-work failure with the firm's name on the cover:

- **§2.2, the model may draft language but not invent facts.** Every proposal
  must cite the excerpts it was given *and* supply a verbatim quote from each.
  `ValidateReply` rejects the proposal if any quote is not actually present in
  the excerpt it is attributed to. This is the load-bearing check: the model can
  compose any prose it likes, but it cannot produce a verbatim span of a
  document that does not contain it. Whitespace and case are normalised so an
  honest quote across a line wrap still passes; the words and their order must
  be there.
- **§2.3, provenance.** Each proposal stores the exact model string and a
  SHA-256 of the rendered prompt, so everything one model-and-prompt pairing
  wrote can be found again after a model change.
- **§2.4, retrieval.** Section-aware chunking (Markdown ATX, setext, and the
  numbered headings a Word export produces), with the full heading path on each
  chunk, then lexical BM25 — no embeddings, no vector store, no new service.

### Retrieval qualifies on *what* matched, not how highly

A bare BM25 score floor did not survive its own test. "Physical badge access to
the data centre" scored 3.28 against an unrelated "Access Reviews" section on
the strength of the single word *access* — comfortably above any floor low
enough to admit real matches. Scores are not comparable across corpora, so
`ScoredChunk.Qualifies` asks what matched instead: at least two distinct query
terms, at least one drawn from the requirement's summary or NIST mapping. A
single term qualifies only when it is a control identifier (`SC-8`, `8.2.1` —
a digit plus a separator), because those are self-identifying and ordinary
words are not. This is the filter that makes one document against a 700-entry
catalog affordable: unrelated pairings never reach the model.

### Ingestion

Plain text, Markdown and HTML, by upload or URL. HTML headings are converted to
ATX before tags are stripped, so a web page keeps the structure chunking
depends on. Binary formats are refused with a clear error rather than guessed
at — PDF/DOCX extraction needs a parsing dependency, and a silently mangled
extraction feeds the model garbage that reads like prose. Duplicate documents
are rejected on the SHA-256 of their *extracted text*, so the same standard
exported twice is recognised.

The URL path is the module's one SSRF surface and is guarded at the dialer's
`Control` hook, not just on the parsed hostname: a name that validates as
public can resolve to `169.254.169.254` before the connect, and only the dial
sees the address actually used. Loopback, RFC1918, link-local, CGNAT,
multicast and IPv4-mapped IPv6 are all refused, and every redirect hop is
re-validated.

### Review

Pending proposals are listed strongest-first with the citation shown beside the
suggestion rather than behind a disclosure — a proposal accepted without
reading its evidence is the failure this module exists to prevent. The reviewer
can edit the text before accepting; what gets stored as the proposal's text is
what was actually applied, so provenance matches the catalog. Accepting one
proposal supersedes the other pending ones for the same NFR field, which were
written against text that no longer exists. Rejections carry a note.

### Schema, routes, wiring

- New tables `nfr_source_documents`, `nfr_source_chunks` and
  `nfr_enrichment_proposals` in both the SQLite and PostgreSQL schemas, with
  `dbsync` rows (documents first — the other two reference a document id).
- `db.Tx` gained an `Insert` method mirroring `Conn.Insert`, so a document and
  its chunks are written in one transaction without the caller reimplementing
  the SQLite/Postgres difference in reading back a generated key.
- `securitynfr.Handler` gained a `Service()` accessor. `buildHandlers` already
  returns eight values and a ninth would not have made it clearer.
- `GET /nfr-enrichment`, `/documents` and `/proposals` are open to any
  signed-in user; the ingest, analyse, accept and reject routes sit behind the
  admin middleware. Accepting writes to a client deliverable, and running an
  analysis spends money.
- Token spend is logged through the app's existing `logAIUsage`, so it appears
  in the AI Chat usage dashboard under `claude` — one place for spend, per
  §2.5.

### An error-mapping fix the smoke test forced

The first end-to-end run returned `{"error":"no such column: n.key"}` with a
**400**. Two faults: the proposal list joined `security_nfrs.key` when the
column is `record_key`, and the handler's default branch reported any
unrecognised error as a bad request carrying its own message. A storage failure
is not the caller's fault and its message is not theirs to see. There is now an
`ErrInvalid` sentinel for caller-fixable failures; anything else is logged and
returned as a generic 500.

### Verification

23 unit tests covering chunking, heading detection, HTML extraction, retrieval
ranking and qualification, the citation gate (including that a fabricated quote
is rejected), reply parsing, prompt construction, the SSRF guard, and the
review workflow — with direct assertions that analysis never writes to the
catalog, that a discarded proposal is never stored, that an unrelated NFR never
reaches the model, and that one failed NFR does not discard a run's other
proposals.

Also exercised end to end against a throwaway database: page load, upload,
duplicate rejection, the analyse-without-a-key path, SSRF refusals for the
cloud metadata endpoint and loopback, and delete.

## 2026-08-11 (AI Chat is Anthropic-only, plus a Wintermute provider)

The AI Chat Gateway had three providers: OpenAI (key-based), Azure OpenAI
(Microsoft Entra SSO) and Anthropic Claude. The vendor set is now Anthropic
only, and the second provider is **Wintermute** — a self-hosted `wintermuted`
server on your own network that decides for itself whether a local model or
Claude answers the question. So the two options are no longer two vendors;
they are "call Anthropic directly" and "let your own server decide", which is
the actual choice being made.

### Removed

- `askOpenAI`, `askAzureOpenAIWithMicrosoftSSO`, `extractOpenAIUsage`,
  `extractChatCompletionText`, `buildMessages`, `validatedOpenAIEndpoint` and
  `validatedAzureOpenAIEndpoint` from `internal/app/ai_chat.go`, with the
  matching UI fields and provider-dropdown entries.
- The entire Microsoft Entra SSO subsystem, which existed only to obtain an
  Azure OpenAI token: `loadMicrosoftAuthConfig`, `microsoftTokenRequest`,
  `microsoftAccessTokenForSession`, `emailFromIDToken`, the four
  `/ai-chat/auth/*` routes, the `ai_chat_session` cookie, and
  `internal/app/ai_chat_sessions.go` — the AES-256-GCM encrypted session store
  built to hold those OAuth tokens — along with its key file, its startup
  wiring in `app.go`, and `cleanupStaleAIChatSessions`.
- The `ai_chat_sessions` table from the SQLite and Postgres schemas, its two
  `ensureColumn` migrations, its `dbsync` row, and its entry in the
  `sqlite_test` table list. **No `DROP TABLE` is issued**: an existing
  deployment keeps an orphan table that nothing reads or writes, which is
  preferable to a destructive migration running unattended on upgrade. Drop it
  by hand if you want it gone.
- `AZURE_OPENAI_ENDPOINT`, `AZURE_OPENAI_DEPLOYMENT`, `AZURE_OPENAI_API_VERSION`
  and `AZURE_TENANT_ID` from `deploy/grc.env.example` and
  `scripts/setup.sh`.

### Added — the Wintermute provider

`askWintermute` opens a conversation on a `wintermuted` server
(`POST /api/v1/sessions`), posts the question (`POST .../messages`) and reads
the reply. That server owns the transcript and routes the turn, so this app
never holds a model endpoint or a vendor API key for that path — only a client
token. The response reports which backend and model actually served the turn,
because wintermuted retries a failed local backend against its configured
fallback and the answer may not come from where it was asked for. A turn that
ends `awaiting_client` is surfaced as an error rather than an empty answer:
this app declares no client-side tools, so that status means the model asked
for something only a harness can run.

Configuration comes from the request or, when a field is blank, from
`WINTERMUTE_URL`, `WINTERMUTE_TOKEN`, `WINTERMUTE_BACKEND` and
`WINTERMUTE_MODEL`. `GET /ai-chat/wintermute/status` reports those defaults so
the page can prefill them; it returns whether a token is configured, never the
token.

`validatedWintermuteEndpoint` is deliberately looser than the vendor
validator and deliberately not arbitrary. A wintermuted server is normally a
host on the operator's own LAN with no TLS, so plain HTTP is accepted — but
only to a loopback, private or link-local literal address, or the name
`localhost`. Other hostnames are rejected rather than resolved: resolving one
here would be both a TOCTOU race and a lookup of an attacker-chosen name, and
allowing cleartext to any host would turn this handler into an open proxy.
HTTPS is accepted anywhere.

### Note on `github.com/openai/openai-go`

It is still in `go.mod` as an indirect requirement and is **not** an AI backend
for this app. It arrives via `github.com/securego/gosec/v2/cmd/gosec`, whose
`autofix` package imports it; gosec is what `TestGosec` runs, so removing the
dependency means removing the security gate. Left in place deliberately.

### Default Claude model moved to `claude-opus-5`

`claudeDefaultModel` was `claude-sonnet-4-6`, a generation behind; the current
Claude line is `claude-opus-5` / `claude-sonnet-5` / `claude-fable-5` plus
Haiku 4.5. Both the constant and the model field's prefilled value on the page
now say `claude-opus-5`.

This raises the per-token cost of a request that does not override the model,
which is the intended trade for this app's work — control and policy drafting
is reasoning-heavy and low-volume. The field is still per request, so a
cheaper model can be named for anything that is not. `POLICY_MODULE_FRAMEWORK.md`
had flagged the stale default as a pending separate change; that note is now
the current state instead, with the cost guidance kept.

### Also

- `ai_usage_store.go` provider ordering is now `claude`, `wintermute`. Rows
  written by the old providers still render, under their raw provider name.
- `home.go` and `POLICY_MODULE_FRAMEWORK.md` updated to describe the new
  provider set.
- `internal/app/ai_chat_test.go` drops the OpenAI/Azure endpoint tests and the
  session-cookie test. It gains coverage for the endpoint validator (accepted
  private/TLS forms; rejected cleartext-public, bare hostname, user-info,
  non-HTTP scheme and relative URL), for usage extraction, and — against an
  `httptest` stub standing in for wintermuted, which listens on `127.0.0.1`
  and so passes the validator for real — for the two-call session/message
  sequence, the bearer header, the system-prompt prefix, the served-by
  reporting, the `awaiting_client` error path and the environment-variable
  fallbacks.

## 2026-08-10 (Matrix/Chaos themes gained the cascading-glyph backdrop from morpheus)

`internal/app/theme_middleware.go` gained `matrixRainTag`, a port of morpheus's
`internal/app/static/matrix-rain.js`: a fixed, full-viewport canvas of falling
katakana painted behind the page. `bodyInsertTag` injects it for Matrix and
Chaos only — the two themes already sharing that palette — so Dark and Light do
not pay for a canvas repainting behind a page that would never show it. This
completes the morpheus Matrix theme in this app, which previously had the
palette and the Chaos glitch engine but not the backdrop they were designed
around.

Carried over unchanged from morpheus: the 16px cell, 12 FPS redraw, 0.08 trail
fade, the brighter head over a dimmer trail, the random per-column restart that
keeps columns out of step, the 0.18 base opacity, and the 10–500% brightness
scaling of that opacity.

Adapted for this app, which is server-rendered multi-page rather than a SPA:

- No start/stop pair and no hidden canvas. morpheus's `theme.js` toggles the
  rain as the theme changes at runtime; here a theme switch writes the
  `go_rcsa_theme` cookie and reloads, so the tag is simply absent on the other
  two themes and the engine starts unconditionally when present.
- `main { position: relative !important; z-index: 1 !important }` ships with the
  rain. The canvas is a positioned `z-index: 0` element, so an unpositioned
  `<main>` would paint underneath it and take every pane's background with it.
  Every page here wraps its content in exactly one `<main>`, including the
  sidebar-less ones, so lifting that one element covers all of them. The
  `!important` is required because `sideNavTag` resets the same element with
  `main:has(> .global-shell) { all: revert }`, which is more specific and is
  injected later in the cascade. Fixed descendants are unaffected — a relatively
  positioned ancestor is not a containing block for them — and the AI dock and
  theme toggle sit outside `<main>`, so they keep painting above it.
- The canvas is sized by explicit `width/height: 100% !important` rather than
  morpheus's `window.innerWidth`/`innerHeight`. Two separate reasons: a canvas
  is a *replaced* element, so `inset: 0` alone leaves it at its intrinsic
  300x150 (confirmed in-browser before the fix), and `innerWidth` counts the
  scrollbar, which on a fixed left-anchored element adds horizontal scroll. The
  engine then reads `clientWidth`/`clientHeight`, which resolve against the
  viewport minus scrollbars. `max-width`/`height` also need `!important` to beat
  `mobileStyleTag`'s `canvas { max-width: 100%; height: auto }`, meant for
  inline media.
- Brightness is console-only via `window.GRCRain.setConfig`
  (`grc-rain-brightness` in localStorage), matching the existing
  `GRCChaos` precedent, rather than morpheus's Settings-page control —
  there is no equivalent settings UI here.
- Template literals are rewritten as concatenation, the same constraint
  `chaosEngineTag` already lives under: the source sits in a Go raw string
  literal, which cannot contain a backtick. Pinned by a test.

`prefers-reduced-motion: reduce` gets a single still frame of scattered glyph
runs rather than a blank canvas — the preference is about movement, and dropping
the backdrop entirely would take the theme away with it. The animation is also
stopped while the tab is hidden, since browsers throttle the timer unevenly and
the rain otherwise lurches on return.

Five tests added to `internal/app/theme_middleware_test.go` covering
per-theme injection, the no-backtick constraint, the `<main>` lift (including a
guard that fails if `sideNavTag` stops reverting `main`, which would make the
`!important` stale), and the reduced-motion path.

Verified with `go build ./...`, `go fmt ./...`, `go vet ./...`, `go test ./...`
(clean, including the gosec and govulncheck gates) and in headless Chrome
against a throwaway `LOCAL_MODE` instance on a 1900px viewport: the canvas
measures the full viewport, `document.scrollWidth` gains no horizontal overflow,
`elementFromPoint` at the page centre returns page content rather than the
canvas, and the canvas has lit pixels on both a dense page (`/controls`) and a
sparse one (`/`) as well as under emulated `prefers-reduced-motion`. No new
dependencies were added.

## 2026-08-08 (Edit button on the /security-nfrs detail panel opens the selected NFR in the editor)

The detail ("Viewing key ...") panel header on `/security-nfrs` gained an `Edit`
pill beside the `Hide` toggle. It navigates to
`/security-nfrs/manage?key=<key>`, and `ManagePage` now honours that `key` query
parameter by preselecting the record and filling the editor form, so the round
trip lands on the NFR the user was reading instead of the top of the list.
Both changes are in `internal/securitynfr/handler.go`.

- `Edit` and `Hide` are wrapped in a `.header-actions` flex box that owns the
  `margin-left: auto`, which moved off `.panel-toggle`. Two siblings each
  carrying `margin-left: auto` would have split the free space between them and
  pushed the buttons apart.
- The button is `disabled` (`.panel-toggle:disabled`, 45% opacity) whenever no
  row is selected, kept in sync by `renderDetail`. Because the panel header
  survives collapsing, `Edit` stays reachable with the detail body hidden.
- Deliberately a `<button>` doing `window.location.assign`, not an `<a>`: the
  global dark theme in `internal/app/theme_middleware.go` restyles `button` and
  `a` differently and with `!important` (buttons get a dark fill, links get
  `#93c5fd` text), so an anchor would have rendered as pale blue text on the
  cream pill next to a dark `Hide` button — mismatched and low contrast. The
  cost is losing middle-click/open-in-new-tab, which an anchor would have given.
- `pendingKey` on the editor page is consumed on the first load only, so
  `Reload DB` can't drag the selection back to the deep-linked key after the
  user has moved on. A key that isn't in the database reports so via
  `formMessage` and falls back to the normal full list rather than failing
  silently.
- The deep-linked row is scrolled into view (`block: 'center'`) on that first
  load only; doing it on every render would fight the user on ordinary clicks.
- No permission filtering on the button, matching existing behaviour:
  `pageui.Nav` already shows the `/security-nfrs/manage` tab to every user and
  lets `pageAccessMiddleware` reject the request, so a user without that grant
  sees the same access-denied path they already would from the side nav.

Verified with `go build ./...`, `go fmt ./...`, `go vet ./...`,
`go test ./...` (clean) and by serving both pages from a throwaway `LOCAL_MODE`
instance: the list page emits the button and the `?key=` navigation, the editor
page emits the `pendingKey` handling, and `GET /security-nfrs/manage?key=7`
returns 200. The click-through itself was not exercised in a browser.

## 2026-08-08 (/security-nfrs/manage row list matched to the /security-nfrs row list)

`ManagePage` in `internal/securitynfr/handler.go` now carries the same
`--panel-text: 13px` token as `Page`, applied to `.rows` and `.row`, so the NFR
row lists on the two pages render identically.

No family change was needed here, and none was made: unlike the list page, this
page's stylesheet has a bare `button { ...; font: inherit; }` rule, and `.row`
declares no font properties of its own, so the rows have always inherited the
page's Georgia body font. That element rule is also why only `font-size` is
pinned on `.row` here, where the list page needed `font-family: inherit` as
well.

Accepted trade-off, chosen deliberately: the rows are now 13px while the editor
form beside them stays at 16px, so this page is no longer internally uniform.
Cross-page consistency of the two row lists was preferred over it. Shrinking
the form was not on the table — its inputs are kept at 16px because anything
smaller makes iOS zoom the viewport on focus, which is what the global
`input, select, textarea { font-size: max(16px, 1em) }` rule in
`internal/app/theme_middleware.go` exists to prevent.

A stray detail worth remembering: the CSS comment first written on `.row`
quoted the button rule in backticks, which closed the Go raw string literal
holding the page HTML and broke the build (`expected ';', found button`). Page
CSS/JS comments in these handlers must not contain a backtick.

Verified with `go build ./...`, `go fmt ./...`, `go vet ./...`,
`go test ./...` (clean) and by serving both `/security-nfrs` and
`/security-nfrs/manage` from a throwaway `LOCAL_MODE` instance to confirm each
emits the token and the rules that consume it. Not visually confirmed in a
browser.

## 2026-08-08 (Collapsible detail panel and matched panel text size on /security-nfrs)

Two follow-ups to the collapsible-filters change below, both in `Page` in
`internal/securitynfr/handler.go`.

**Panel text sizes now match.** The detail ("Viewing key ...") panel rendered
its card text noticeably larger than the NFR list beside it. Cause: the list
rows are `<button class="row">` elements, and buttons do not inherit the page
font, so the rows had always been drawn at the user agent's ~13px control
default while `.detail-body` inherited the 16px Georgia body font. Neither
panel was pinned to an explicit size, so the mismatch was whatever the browser
chose. A new `--panel-text: 13px` token is now applied to `.rows`, `.row` and
`.detail-body`, making the two panels equal by construction rather than by
coincidence. `.rows` is included so the "No Security NFRs match" empty state
matches the rows it replaces. The `.card h3` labels stay at 12px uppercase, and
the NIST chips were already 13px.

`.row` also carries `font-family: inherit`, so the rows use the page's Georgia
body font instead of the UA control font and the two panels match in family as
well as size. (This was a follow-up request: the first pass pinned only the
size, on the grounds that changing the family would restyle the list panel that
was being used as the size reference.) `font-family: inherit` rather than the
`font: inherit` used by `.chip`/`.button` on this page, so the explicit
`--panel-text` size survives.

**The detail panel collapses.** Its header gained a `Hide`/`Show` pill toggle
(`.panel-toggle`, with the caret rotating like the filters header).
`.layout.detail-collapsed` drops the grid to one column so the NFR list
stretches the full page width, and hides `.detail-body` while keeping the
detail header as a bar under the list so the panel can be brought back.

- The collapsed layout also sets `align-content: start`. `.layout` has
  `min-height: 70vh`, and the default `stretch` would have inflated the
  now header-only detail row to fill roughly half of it.
- `border-bottom: 0` when collapsed is scoped to `.detail-header`, not
  `.panel-header`, so it doesn't also strip the list panel's header rule.
- State persists under `grc-nfr-detail-collapsed`, alongside the filters
  flag. Clicking a row while the panel is collapsed reopens it (and clears the
  stored flag, so the panel really is expanded again): selecting an NFR is a
  request to see it, and leaving the click with no visible result reads as a
  broken list. This reverses the first cut of this change, which suppressed the
  auto-expand on the theory that an explicit collapse shouldn't be undone by
  browsing — that was the wrong call in practice. Only real row clicks expand;
  the implicit re-selection `applyFilters` does when the current key filters
  out does not.
- The two duplicated `localStorage` `try`/`catch` blocks are now shared
  `readFlag`/`writeFlag` helpers, and the caret class was unified from
  `.filters-caret` to `.caret` for use by both toggles.

Verified with `go fmt ./...`, `go vet ./...`, `go test ./...` (all clean, so the
gosec and govulncheck gates pass) and by serving the page from a throwaway
`LOCAL_MODE` instance to confirm the new CSS, markup and script render. As with
the change below, the collapsed and expanded layouts were not visually
confirmed in a browser.

## 2026-08-08 (Collapsible search/domain filters on /security-nfrs)

The search box, domain chips and Reset button at the top of `/security-nfrs`
(`ListPage` in `internal/securitynfr/handler.go`) are now inside a collapsible
`.filters` section with a pill-shaped toggle header. The domain chip list grows
with the catalog and pushed the NFR rows and detail panel well down the page;
collapsing it reclaims that vertical space.

- The existing `.toolbar` grid is unchanged apart from its top margin, which
  moved to the wrapping `.filters` section; `.filters.collapsed .toolbar`
  hides it.
- Collapsed state persists per browser in `localStorage` under
  `grc-nfr-filters-collapsed`, matching the pattern the global side-nav
  collapse already uses (`grc-nav-collapsed` in
  `internal/app/theme_middleware.go`). Both the read and the write are wrapped
  in `try`/`catch` so blocked storage degrades to a working
  non-persisted toggle rather than a dead button.
- While collapsed, the header shows a summary of the active filters ("No
  filters", or the domain and quoted search term) so a filtered-down row count
  can't be mistaken for a small catalog. It is hidden when expanded, where the
  controls speak for themselves.
- `aria-expanded` on the toggle tracks the state and `aria-controls` points at
  the toolbar.

Note: `.filters` is also matched by the global mobile fallback in
`theme_middleware.go` (`.toolbar, .filters, .controls-bar { grid-template-columns:
1fr }`); harmless here because this new `.filters` element is a block container,
not a grid.

Verified with `go build ./...`, `go vet ./...`, `go test ./...` (clean, so the
gosec and govulncheck gates pass) and by serving the page from a throwaway
`LOCAL_MODE` instance to confirm the new markup, CSS and script render.

## 2026-08-07 (SECURITY: untracked two committed AI-chat encryption keys)

`users.db.aichat.key` and `local.db.aichat.key` were tracked in git, committed
in `fff1c99` ("chore: auto-commit 2026-06-28"), and pushed to `origin/main`.
Each is the 32-byte AES-256 key that encrypts stored AI chat sessions —
`aiChatKeyPath` in `internal/app/app.go` derives it as
`<sqlite-path>.aichat.key`, and `internal/app/ai_chat_sessions.go` uses it for
AES-GCM seal/open. Anyone with repository access could therefore decrypt AI
chat session content from any copy of the database.

Root cause was a gap in `.gitignore`, not a bad manual `git add`: `*.gcal.key`
was already ignored — with a comment reading "they must never be committed" —
but the analogous `*.aichat.key` was never added, so an `git add -A` in an
auto-commit swept both files in. `.gitignore` now covers `*.aichat.key` and
`/ai_chat.key` (the PostgreSQL-mode path) in that same block, and both files
are untracked via `git rm --cached`.

`--cached` is deliberate: the files remain on disk, because deleting them
would leave every already-encrypted AI chat session permanently unreadable.
Verified both files are byte-identical after untracking, are now matched by
`git check-ignore`, and are no longer picked up by `git add -A`.

**Not done here, and still outstanding:** the keys remain in git history and on
the GitHub remote, so this commit alone does not un-expose them. Purging
history (e.g. `git filter-repo`) force-pushes a shared branch, and rotating the
keys makes existing encrypted sessions undecryptable — both are operator
decisions with real consequences, deliberately left to a human. Treat both keys
as compromised if the repository is or ever was public.

A sweep of the rest of the index found no other tracked secrets: no `.key`,
`.pem`, `.env`, or database files, and only `config/db.env.example` under
`config/`.

## 2026-08-07 (userctl set-admin — recover admin without hand-editing SQLite)

Closes a real lockout reachable through the documented tooling. `userctl
create` defaults `-admin` to false, so a first account created with
`scripts/manage-user.sh create -u admin` (no `--admin`) is a non-admin. In
single-user mode — the shipped default, since `SetSingleUserMode(false)` is
never called from `Run()` — the single-user guard then refuses a second
account, and every in-app route to admin (`/admin/user-management`, the
`/auth/users` API) is itself behind an admin session. `userctl` had no
promote command, so the only escape was hand-editing `is_admin` in SQLite.
This was hit on a live deployment.

New `userctl set-admin -u USER [-remove]`, wired through
`scripts/manage-user.sh set-admin -u USER [--remove]`. It is a thin wrapper
over `authn.Service.UpdateUser`'s existing `isAdmin *bool` parameter, so it
inherits that method's `ErrLastAdmin` guard: `-remove` refuses to strand a
deployment with zero admins, and therefore cannot be used to re-create the
lockout it exists to undo.

Two deliberate choices. The flag is `-remove`, not `-revoke`, because
`manage-user.sh` already uses `--revoke` on `passwd` to mean "delete that
user's sessions" — reusing it here would read as revoking sessions. And
`set-admin` is routed before the wrapper's password block, so it never
prompts for a password: it reads no stdin and leaves credentials untouched,
which matters because it is the command an operator reaches for when they are
already locked out. No sessions are revoked either — `GetSessionUser` joins
`auth_users` on every request, so the change takes effect on the next page
load with no restart and no re-login (verified against a running server: the
same session cookie went 403 → 200 on `/utilities`).

Also fixes a latent bug in `manage-user.sh`'s `usage()`, which printed a
hardcoded `sed -n '2,26p'` line range. Adding usage lines would have silently
truncated the help text, and the range already overran the comment block by
one line, leaking `set -euo pipefail` into the output. It now prints from line
2 until the first non-comment line, so editing the header cannot desync it.

New `cmd/userctl/main_test.go` (the package had no tests): covers the
non-admin-sole-account recovery path including the single-user refusal that
creates it, the last-admin guard on `-remove` plus that the flag is unchanged
after a refusal, `-remove` succeeding when another admin remains, that the
password still authenticates afterwards, and the unknown-user and missing-`-u`
errors.

## 2026-08-07 (fixed the gosec gate broken by the backup/integrity-check work)

`TestGosec` (and therefore `go test ./...`, which is this repo's security
gate) had been failing since the full-database-backup and integrity-check
endpoints landed: `internal/app/utilities.go` had five G104 "errors
unhandled" findings, all on bare `Close()` calls. This was a real regression
in the security gate, not a pre-existing failure.

`utilitiesIntegrityCheck` was closing its `*sql.Rows` by hand at four points
because it runs two pragma queries in sequence in one function body. Each
pragma is now its own helper — `runIntegrityCheck` and `runForeignKeyCheck` —
which lets both use the plain `defer rows.Close()` idiom used everywhere else
in this repo, and picks up a previously missing `rows.Err()` check on each
loop so a truncated pragma result is reported instead of silently read as a
clean bill of health.

`utilitiesBackup` no longer does the `CreateTemp` → `Close` → `Remove` dance
it used to need to hand `VACUUM INTO` a path that does not yet exist. It
creates a temp *directory* via `os.MkdirTemp` and targets `backup.db` inside
it, removed with a single `defer os.RemoveAll`. Same behaviour, one less
failure mode, and no unhandled `Close`.

Verified end to end against a scratch instance beyond the unit tests:
`GET /utilities/backup` returns a `PRAGMA integrity_check`-clean SQLite file
carrying all 31 live tables with no temp files left behind,
`GET /utilities/integrity-check` returns `{"ok":true}`, an `ai_usage_log` row
round-trips through export → import (confirming the v7 snapshot gap fix), and
the admin gate returns 302 to `/login` for a browser request with no session
versus 401 JSON for an API request.

## 2026-08-07 (Ctrl+K / Cmd+K command palette for jumping to any page)

Added a fuzzy-jump command palette, opened by Ctrl+K (or Cmd+K), covering
every destination the sidebar renders — `commandPaletteTag` in
`internal/app/theme_middleware.go`, wired into `bodyInsertTag` alongside the
sidebar and theme toggle. A "Search pages… Ctrl K" trigger button is also
inserted at the top of the sidebar (above Home) for discoverability without
the shortcut.

Deliberately does not carry its own copy of the nav data: at open time it
reads the `<a class="tab">` elements already rendered inside
`.global-sidenav`, so it can never drift out of sync the way the four
separate legacy tab lists did before Phase 1 of the nav redesign. Matches
are scored (exact label match, then starts-with, then contains, then a
match against the href) and capped at 30 results; arrow keys move the
selection, Enter navigates, Escape or a backdrop click closes it.

## 2026-08-07 (fixed page-chrome inconsistencies introduced by the sidebar rollout)

Several pages looked visibly different from the rest after the sidebar nav
shipped: `reports`, `crm`, `utilities`, `ai-chat`, `controlcatalog`, and
`riskregister` each define their own boxed `main { max-width: …; margin: 24px
auto; border: …; }` from before the sidebar existed. The sidebar script nests
`.global-shell` *inside* that same `<main>` rather than replacing it, so the
old box model kept applying around the new shell — on any page whose
hardcoded max-width didn't happen to match the viewport width, this produced
a stray, off-center dead zone next to the fixed sidebar. Pages without that
legacy box style (e.g. `controls`, `todo`, `policies`) looked fine, which is
what made this read as "inconsistent" rather than "broken everywhere."

Fixed centrally in `theme_middleware.go` with one rule —
`main:has(> .global-shell) { all: revert; display: block !important; }` —
rather than editing the max-width numbers in every affected handler; the
visible box now comes entirely from `.global-shell`'s own children.

Also fixed: `ai_chat.go` and `wiz_rules.go` used a `.panel-head` class where
every other page uses `.panel-header`, so their panel headers never picked
up the shared theme's `.panel-header` styling and rendered in a mismatched
tan color instead of the theme's navy — renamed to `.panel-header`. And on
`/security-nfrs`, the toolbar's `align-items: center` left the search box and
Reset button floating in the middle of the much-taller wrapped domain-chip
row instead of sitting at the top with it — changed to `align-items: start`.

## 2026-08-07 (fixed repo-wide CRLF corruption from an earlier commit)

`bece084` ("new", not authored via this session) had converted line endings
to CRLF across most of the repository, apparently as an unintended side
effect of an IDE action. On Linux/WSL this silently breaks anything that
treats `\r` as meaningful: reported symptom was `bash update.sh` failing with
a garbled `invalid option name` error, because `set -euo pipefail\r` reads
`pipefail\r` as the option name (the embedded `\r` is also why the error text
itself looked corrupted on screen — it's a literal carriage return moving the
cursor back to column 0 mid-message).

Converted CRLF back to LF (content-neutral — verified with `git diff -w`
showing zero remaining differences after the fix) in every affected tracked
file except `.idea/*` (IDE-managed, no functional impact): all 14 shell
scripts (`update.sh`, `admin_setup_login.sh`, `run_local.sh`,
`scripts/*.sh`, `templates/build.sh`), `.gitignore` (CRLF in a gitignore
pattern can silently defeat the match), the systemd unit and nginx config
under `deploy/`, both `.env.example` files, `go.mod`/`go.sum`,
`.goreleaser.yaml(.bak)`, the two large embedded catalog JSON files, the
reporting templates and golden HTML fixtures, and the LaTeX/Typst document
templates.

Fixing `templates/typst/brand.typ`'s CRLF also incidentally fixed four
`internal/doctemplate` test failures (`TestApplyToTypstBrandFindsBlock` and
three others erroring with "brand dictionary in brand.typ is not closed by a
parenthesis at column zero") that an earlier session had logged as
"pre-existing, unrelated" — they were actually caused by this same
corruption, not a separate bug.

## 2026-08-07 (global navigation replaced with one grouped, collapsible sidebar)

Every page's navigation used to come from one of four separate tab lists in
`internal/pageui/nav.go` (`primaryTabs`, `homeTabs`, `riskRegisterTabs`,
`crmTabs`) that had drifted apart over time: a page using `primaryTabs` (used
by almost every module page) had no link at all to Risk Register, CRM, or API
Docs — those only existed in `homeTabs`, reachable solely from the home page.
On top of that, the home page rendered its own bespoke sidebar
(`internal/app/home.go`) while every other page got a flat top bar of ~25
pill buttons wrapping three rows before any page content.

Replaced with one shared, grouped nav dataset (`pageui.Nav`, replacing
`PrimaryTabs`/`HomeTabs`/`RiskRegisterTabs`/`CRMTabs`/`PrimaryTabsWithExtra`)
rendered identically on every page — Catalog, Compliance & Risk, Reporting &
Data, CRM, Productivity, and Company groups, plus an Admin group
(API Docs, Utilities) visually deprioritized at the bottom. This closes the
missing-links gap by construction: every page can now reach every section.

The chrome itself moved from `theme_middleware.go`'s horizontal top-bar wrap
to a fixed, independently-scrolling left sidebar on desktop (collapsible,
state persisted in `localStorage`) that becomes an off-canvas drawer on phone
widths, opened by a new `#global-nav-toggle` hamburger button — replacing the
previous mobile behavior of a single horizontally-scrolling row of two dozen
pills. `home.go`'s bespoke `.sidenav`/`.shell` implementation (~150 lines of
CSS/JS, and the only page with a mobile-responsive bug where nav items were
clipped off-screen at phone widths) was removed in favor of the shared one.

Detail pages (e.g. `/controls/detail/AC-1`) no longer need a synthetic
per-page nav tab to show something active — `pageui.Nav` highlights the
nearest ancestor via longest-prefix matching on the active path instead
(`/controls` lights up for any `/controls/detail/*` route), which is also
simpler than the `PrimaryTabsWithExtra` mechanism it replaces.

`TestDetailPagesRenderFullNavigation` and `TestPrimaryPagesRenderFullNavigation`
in `internal/app/navigation_test.go` were strengthened to assert the full
nav link set (including the previously Home-only and CRM-only links) appears
on every page, and to check ancestor-active highlighting instead of the
removed synthetic tab.

This is phases 1–4 of a larger nav/UI review; a command palette, breadcrumbs
on detail pages, and de-duplicating the per-page inline CSS across ~19
handler files are tracked as follow-up work, not included here.

## 2026-08-06 (full database backup, integrity check, and a full-export gap fix)

`/utilities` gained two new panels backed by two new admin-only endpoints.
`GET /utilities/backup` streams a raw, engine-native copy of the live SQLite
database via `VACUUM INTO` to a temp file (deleted after streaming); it is
not table-aware, so unlike the JSON export it cannot silently miss a table
the way `ai_usage_log` had been missing (see below). It returns 400 with a
`pg_dump` pointer when the deployment is running PostgreSQL, since
`VACUUM INTO` is SQLite-only. `GET /utilities/integrity-check` runs
`PRAGMA integrity_check` and `PRAGMA foreign_key_check` read-only, so a
backup can be verified before it is relied on.

Separately, `ai_usage_log` (the AI Chat per-call token/cost history) existed
in the schema but was never added to `dbsync.syncOrder`, so the existing
"Export full data set" JSON snapshot was silently missing it. Added it to
`syncOrder` in `internal/dbsync/dbsync.go` (id-keyed) and bumped
`SnapshotVersion` 6 → 7 in `internal/dbsync/transfer.go`; older (v6) backups
still import fine, they just restore `ai_usage_log` empty rather than losing
anything that was actually captured.

## 2026-08-06 (admin-only pages no longer bounce signed-in users back to login)

`adminTokenMiddleware` (used by `/utilities`, user management, CRM/company/
policy/doc-template admin routes) previously redirected *any* request that
failed the admin check to `/login`, including a request from a user who was
already validly signed in but just not an admin. That is indistinguishable
from being logged out, which is what made `/utilities` look broken: the page
itself (unlike most other pages, which are read-open/write-admin) is gated
entirely behind admin, so a non-admin session landed straight back at the
login form with no explanation.

The middleware now tracks whether the request carried a valid session
separately from whether that session is an admin. A signed-in non-admin
browsing an admin-only page gets a 403 "Admin Access Required" page (with a
link home and a "sign in as someone else" action) instead of a redirect to
`/login`; a request with no session at all still redirects to login as
before. Non-browser (fetch/API) callers get the same distinction as JSON:
401 for no session, 403 for a session that isn't admin. New test:
`TestAdminTokenMiddleware_SignedInNonAdminGetsExplanationNotLoginRedirect` in
`internal/app/auth_test.go`.

## 2026-08-06 (control detail page: dark "Linked Security NFRs" card)

On `/controls/detail/:controlID`, the "Linked Security NFRs" card now uses a
black background with light text, matching the dark NFR row styling already
used on `/exceptions`. Previously it used the same light card styling as the
rest of the page, which made it visually indistinguishable from neighboring
cards despite holding cross-linked content.

## 2026-08-06 (exceptions page links selected Security NFRs to their detail page)

On `/exceptions`, the summary text for each selected Security NFR in the
"Selected Security NFRs" panel is now a link to `/security-nfrs/detail/<key>`
(opens in a new tab), matching the linked Controls beneath it which already
pointed to `/controls/detail/<id>`. Previously only the linked Controls were
clickable — the NFR itself had no way to reach its detail page from the
selection panel without going through `/exceptions/detail`, which already had
this NFR link.

## 2026-08-05 (tasks mirror to a Google Calendar)

New `internal/gcal` and a Google Calendar panel on `/todo`: sign in with a
Google account, and tasks that carry a due date are pushed to a calendar the app
creates for itself, kept in step as they are edited, and shown back on the month
grid. Routes live under `/todo/google/*`, so they inherit the `/todo` entry
already in `protectedPrefixes` rather than needing a new one — a top-level
prefix would have had to be added there by hand, and forgetting would have left
an OAuth flow reachable without a session.

- **The sync is deliberately one-way.** Tasks become events; events never become
  tasks. This app is the system of record and Google is a view of it, so there
  are no conflict rules, no sync tokens and no tombstones to get wrong. Events
  are read back only for the overlay, which under this scope is a reconciliation
  view — it shows what actually reached Google, and anything the user added to
  that calendar themselves.
- **The OAuth scope is `calendar.app.created`, not `calendar`.** The credential
  can only touch the calendar it made, so it cannot read or modify the user's
  existing meetings and a leaked token exposes the task mirror rather than a
  diary. `TestAuthCodeURLRequestsOfflineAccessAndPKCE` fails if the consent URL
  ever starts asking for `calendar`, `calendar.events` or `calendar.readonly`.
- **Authorization code flow with PKCE (S256)**, `access_type=offline` and
  `prompt=consent` — without the latter two Google issues no refresh token and
  the link would die an hour after it was made. A connection that comes back
  without one is refused at the point of connecting rather than presented as
  working and then quietly stopping.
- **State is single-use and bound to the owner that started the flow.** The
  callback's owner is checked against the one recorded when the consent URL was
  built, not read from the request, so a callback delivered to another session
  cannot file its tokens under that session's name. Pending authorizations are
  held in memory for ten minutes, not persisted — a table whose only content is
  short-lived secrets is a liability, and a restart mid-login costs one click.
- **Tokens are encrypted at rest** in `google_calendar_accounts` with AES-256-GCM,
  keyed from a file beside the database (`<sqlite-path>.gcal.key`), the same
  split the AI chat sessions use. `Account.Token` is `json:"-"` so an account
  cannot be serialised into a response by accident, and there is no plaintext
  fallback on decrypt: a value that will not decrypt is a wrong key, which is
  reported as "reconnect the account" rather than fed to the token endpoint.
  If the key cannot be created the feature stays off — it never degrades to
  storing credentials in the clear.
- **A signature column, so a sync is not a rewrite.** `google_calendar_events`
  stores a hash of the fields that make up each event; unchanged tasks are
  skipped rather than patched, which is the difference between a sync that costs
  one API call and one that costs a call per task per run. `updated_at` is
  deliberately not part of it — it moves when an ordinal changes, which the
  calendar does not show.
- **Failures the user causes are handled, not reported.** An event deleted in
  Google answers the next patch with 404/410 and is recreated; a deleted
  calendar drops the link rows and rebuilds the mirror; a repeat delete answering
  410 counts as success. Per-task errors are collected (capped at five — a
  revoked token fails every task and five copies of one message is no more
  informative than a thousand) and stamped on the account, so a failure from a
  previous pass is still visible.
- **Disconnect revokes the grant at Google but leaves the events.** They sit on
  a calendar the user owns and can delete in one action; erasing a month of
  somebody's calendar because they unlinked an integration is not a decision
  this code should make. Revocation is best-effort — a network failure must not
  leave a user unable to disconnect.
- **Not given to the assistant.** The tool registry in `registerTaskRoutes` is
  unchanged: pushing a task list to an external service is not an action a
  language model should take unprompted.
- **Dates stay dates.** Tasks are written as all-day events with the API's
  exclusive end date (due date + 1) — `internal/todo` stores `YYYY-MM-DD` text
  precisely so "due Friday" does not move for a reader in another timezone, and
  giving the event a clock time would invent precision nobody entered.
- **`google_calendar_accounts` and `google_calendar_events` join the dbsync
  table set**; `SnapshotVersion` is now 6 and a v5 backup restores with both
  empty, which reads as "not connected". The key file does not travel in a
  snapshot, so a restore onto another host yields an account that must be
  reconnected rather than one that silently keeps writing to a calendar.
- **Tests** (`internal/gcal/gcal_test.go`, 29 cases) cover the sync against a
  stub Calendar API: create/update/skip/delete, recovery from an event deleted
  in Google, token refresh persisting the original refresh token when the
  response omits one, expiry headroom, PKCE and scope on the consent URL,
  single-use owner-bound state, encryption round-trip and wrong-key rejection,
  that the refresh token is not readable in the database, and owner isolation on
  both tables — two users' link rows for the same task id stay distinct.
- Two annotated gosec suppressions: G101 on the OAuth endpoint URLs (addresses,
  not credentials) and G117 on marshalling the token (the payload exists only to
  be encrypted on the next line).

## 2026-08-04 (a page for the firm's own details)

New `internal/company` and a `/company` page: the consultancy's own record —
legal name, trading name, registration and tax identifiers, registered address,
contact details and a short description. One row for the install, in a new
`company_profile` table. Full write-up in the new `COMPANY_INFO.md`, served at
`/knowledge/company` like the other module docs.

- **Its own module rather than more fields on the template brand.** The brand is
  how a document *looks* and is consumed by a typesetter; a CRM client describes
  somebody else; this is the firm's identity as fact. A rebrand is not a change
  of registered office, and correcting a VAT number should not mean opening the
  typography settings.
- **Read-open, write-admin**, the same split as the CRM, policy and template
  modules — `registerCompanyRoutes` in `app.go`. The registered address is what a
  consultant puts on a deliverable, not a secret; the identity every deliverable
  is issued under is not something any account should be able to rewrite.
  `updated_at`/`updated_by` are stamped from the session, never from the body.
- **The website field only accepts http or https.** That is a security boundary,
  not a typo guard: the card renders it as a link, so an unconstrained URL means
  the page offers whatever scheme was stored — `javascript:`, `data:` and
  `file:` all reach an href. A value carrying another scheme is rejected with a
  message naming it; a bare domain gets `https://`; a host with a port
  (`example.com:8443`) is left alone, since that colon is not a scheme.
  Covered both ways in `company_test.go`.
- **Single-line fields reject control characters** rather than stripping them —
  a newline in a city name means the input is not what it claims to be. The
  description is the one multi-line field, with CRLF normalised so a pasted
  value equals a typed one.
- **No default profile.** A fresh install returns an empty one and the page says
  so, with a `complete` flag for whether it carries what a letterhead needs.
  Unlike a brand, a legal name has no sensible stand-in, and a placeholder
  company number would be assumed to have been checked.
- **`company_profile` joins the dbsync table set**, id-keyed upsert like
  `doc_template_brand` — it is authored content, and an install that lost it
  would keep working while quietly having no letterhead. `SnapshotVersion` is
  now 5; a v4 backup restores with the table empty, which is the correct reading
  of a backup taken before the feature existed.
- **Nav, page access and per-user page grants all know about `/company`** —
  `pageui` (primary and home tabs), `protectedPrefixes`, and
  `userManagementAllowedPages`.
- **`TestPageScriptHasNoDoubledBackslashes`** pins a bug found while verifying
  this page: the page JS lives in a Go raw string literal, where `\\n` and `\\/`
  reach the browser doubled. It broke the website-display regex and the address
  line break, and the page looked fine until something rendered.

## 2026-08-04 (the assistant can read the lists it creates)

- **Added `list_todo_lists` to the assistant's tool registry**, the follow-up
  left open when the task modules shipped yesterday. Without a read tool the
  model could not see what already existed, so asking for the same list twice
  produced two lists; the tool's description tells it to check before creating.
  It returns title, description, archived flag, task count and done count per
  list, with an optional `include_archived` (default false).
- **The tool returns a projection, not the stored rows.** `todo.List` carries
  the owner, and the owner is session state the model has no business reading
  back — it is what the loop passes *in* to authorise the call.
- **An argument-free tool call can arrive with an empty input rather than
  `{}`**, which is not valid JSON to unmarshal, so the handler treats empty as
  defaults instead of erroring on a well-formed call.
- **The action surface is still exactly two tools, both confined to the
  signed-in user's own to-do lists.** New `TestToolSurface` pins the full list
  by name and fails when a tool is added, so widening what a language model may
  do in this application stays a decision someone makes on purpose. Also covered:
  owner scoping on the read path (another user sees none of your lists), the
  archive flag, and rejection of malformed input. `internal/assistant` is
  unchanged apart from a stale comment.

Still open: none of this has been exercised against the live Anthropic API —
there is still no `ANTHROPIC_API_KEY` in the build environment. Whether the model
actually *calls* these tools rather than describing them is a prompt property
that the test suite cannot pin.

## 2026-08-03 (personal tasks, and a Claude assistant that can act on them)

Two new modules: `internal/todo` (personal lists, tasks, due dates, agenda and a
month calendar at `/todo`) and `internal/assistant` (a Claude chat window at
`/assistant` that can take actions in the app through a tool registry). Shape
and interaction follow the calendar/todo views in the `morpheus` repo; the
tool-registry pattern is ported from its `internal/assistant`. Full write-up in
the new `TASKS_AND_ASSISTANT.md`.

- **The assistant's action surface is exactly one tool: `create_todo_list`.**
  The registry is assembled in `registerTaskRoutes` and nowhere else —
  `internal/assistant` never imports a feature module, and a feature module
  never registers itself — so what a language model may change in this
  application is readable in one function. That matters here more than in a
  general-purpose app: the modules alongside it hold an approved control
  catalogue and a policy set with an audit trail and a separation-of-duties
  check, and widening the surface should be a deliberate edit rather than a side
  effect of adding code somewhere else. The page shows the tool list rather than
  hiding it — a chat box that can change your data should say which changes it
  can make.
- **Tool handlers are thin adapters over the same `Service` methods the HTTP
  layer calls**, so a list Claude creates goes through the same validation,
  owner scoping and timestamps as one typed into the page. There is one code
  path whether a person or the model triggers it.
- **`owner` is passed to every tool handler by the loop, from the session.** A
  tool cannot be told by the model which user to act as; that is the whole basis
  on which a handler is allowed to write. `TestSendMessageRunsToolsAndFeedsResultsBack`
  asserts the handler sees the session user, and `TestOwnerIsolation` covers the
  storage side: a second user cannot read, write to or delete another's lists
  even holding a valid id, which is the case that matters because ids are
  guessable.
- **Every tool call writes an audit row** (`assistant_tool_calls`) with its
  arguments and result, as each call completes rather than at the end of the
  turn — a cancelled request must still record what already happened. "What did
  the AI change" is answerable without replaying a conversation.
- **Uses the official `anthropic-sdk-go`, which was already in `go.mod`** as an
  indirect dependency — this promotes it to direct rather than adding anything
  to the build graph. Hand-rolling tool-use over the existing `postJSON` helper
  was the alternative and was rejected: the content-block unions are exactly
  where a hand-rolled implementation goes subtly wrong, and the failure mode is
  a 400 at runtime rather than a compile error.
- **A failing tool is reported to the model, not fatal to the turn.** The error
  comes back as an `is_error` tool result so the model can correct itself — a
  bad due date should not fail the conversation. The iteration ceiling (6) is
  required rather than defensive: a model that calls a failing tool, reads the
  error and retries has no natural stopping point.
- **`stop_reason` is checked before the response content.** A refusal returns a
  successful HTTP 200 with empty or partial content, so code that reads
  `content[0]` first breaks on it.
- **Stored content blocks are this package's own form, not the SDK's.** The SDK
  type is shaped for building a request and changes with the SDK; these rows
  have to be readable by whatever binary runs next year. Conversion sits in one
  file, so an SDK upgrade breaks at compile time there rather than silently
  failing to decode a year of conversations. `TestHistoryRoundTripsToolBlocks`
  pins that a reloaded `tool_use` keeps its id, name and arguments and stays
  paired to its result — the API rejects the turn otherwise.
- **The model is behind a `Messenger` interface**, so the tool loop is tested
  against a scripted model: dispatch, result feedback, the audit trail, error
  recovery, the iteration cap, owner isolation and the history round trip all
  run with no API key and no network. `toSDKTools` / `toSDKMessages` are tested
  directly, since a mistake at the SDK boundary is otherwise invisible until a
  live call 400s.
- **Task rules that are decisions, not defaults.** Status drives `completed_at`
  rather than the client (a completion time the browser can set is one that
  lies); an edit cannot move a task between lists (that is a separate operation
  with its own ordinal bookkeeping); the agenda excludes done tasks while the
  calendar keeps them (a to-do list should not be mostly things already done,
  but a calendar is a record of when work landed); and due dates are stored as
  `YYYY-MM-DD` text, because a due date is a calendar day and "due Friday"
  should not move with the reader's timezone.
- **Booleans are INTEGER in both dialects**, matching `crm_time_entries.billable`
  rather than introducing a `BOOLEAN` column that would make `archived = 0`
  non-portable. Five tables added to `internal/db` for both backends.
- **Added to `dbsync`, `SnapshotVersion` bumped to 4**, parent-first and
  id-keyed so the list→task and conversation→message relationships survive a
  copy. Without it a data-set export would silently omit a user's task lists —
  the same gap fixed for the template brand in the previous entry.
- **Calendar padding cells were showing the grid's own rule colour as a solid
  block**, which read as a rendering fault rather than an empty week; caught by
  screenshotting the page, not by a test. They now take the day background, and
  the last week is padded so the grid ends on a row boundary.
- Both pages follow the theming lessons from the template module: panels take
  their background from the theme variables, and pills, chips and chat bubbles
  set background *and* text explicitly, since anything taking only one of the
  two inverts when the theme flips.
- New `ANTHROPIC_API_KEY` (shared with the existing `/ai-chat` page — the
  assistant deliberately does not keep a second copy of the same secret) and
  optional `ASSISTANT_MODEL`, documented in `RUNTIME_ARGS.md`. With no key the
  page loads, says it is not configured and disables the input rather than
  accepting a question it cannot answer.
- **Not verified end to end against the live API**: no `ANTHROPIC_API_KEY` was
  available in the build environment. The loop, the storage round trip and the
  SDK conversion are covered by tests; what remains unexercised is a real call
  and the model's actual willingness to call the tool rather than describe it.

## 2026-08-03 (web UI for the document templates — Typst and LaTeX rendering)

Phase 5 of `POLICY_MODULE_FRAMEWORK.md`. The templates in `templates/` were
buildable only from the command line; this puts them behind two pages and a
render endpoint, so authoring a policy and producing the client deliverable no
longer involves a shell.

- **New `internal/doctemplate`**: a registry of three templates (Typst policy,
  Typst business report, LaTeX policy), engine detection, brand settings, and
  the render pipeline. It deliberately does not import `internal/policydocs` —
  the render payload arrives as JSON matching the contract in
  `DOCUMENT_TEMPLATES.md`, and `registerDocTemplateRoutes` in `app.go` is the
  only place the two modules meet. That keeps the dependency pointing outward
  and lets the same pipeline render a payload from the corpus later.
- **`/templates`** — gallery: engine status with versions, a render form over
  the existing policy documents, and a card per template with a
  render-the-sample action. **`/templates/manage`** — the brand editor.
  **`GET /templates/render`** — the render endpoint (`doc=` or `sample=1`,
  `template=`, `standard=`, `bundle=1`, `inline=1`). Both pages added to
  `pageui`, `access_control.go` and the grantable list in `user_management.go`;
  the policy editor gained a **Render PDF** action.
- **A missing typesetter returns the sources, not an error.** The app ships as
  one Go binary and typst/tectonic are separate installs, so "no engine" is an
  ordinary state rather than a failure. Where one is absent — and on demand via
  `bundle=1` — the endpoint returns a zip of the template, its dependencies,
  the brand already applied, the payload, a `build.sh` and a README. Verified
  both ways: with no engine the endpoint returns a 6-entry zip and an
  `X-GRC-Reason` header, and with typst 0.15.1 on `TYPST` the same request
  returns a 4-page PDF/A.
- **Renders are sandboxed to a temporary workspace.** `templates/build.sh`
  roots the Typst compile at the repository, which is right for a developer and
  wrong for a server: each render now assembles a throwaway directory holding
  only the files it needs and passes `--root` pointing at it, so a template
  cannot read anything the render did not put there. Subprocesses get a minimal
  environment rather than the app's — `ADMIN_TOKEN`, the Jira token and the AI
  key have no business being visible to a PDF renderer — plus a 3-minute
  timeout, capped output, no stdin (latexmk prompts on error and would
  otherwise block until the deadline) and `defer os.RemoveAll`.
- **Brand settings are stored per install** in a new one-row
  `doc_template_brand` table (both dialects), as a JSON blob rather than a
  column per knob: the row is never queried by any of them and a column each
  would mean a migration every time the templates gain a setting. Loading
  decodes over `DefaultBrand()`, so a blob written before a field existed comes
  back with the shipped default rather than a zero value the validator would
  reject. Reset deletes the row rather than writing a copy of the defaults, so
  a later change to the shipped palette reaches an install that never
  customised anything.
- **Validation on brand input is a security boundary, not a nicety.** Every
  field is interpolated into template source that a typesetter then *executes* —
  Typst markup can read files and LaTeX can do worse. Colours must match
  `^#[0-9a-f]{6}$`, font names a conservative family-name pattern, sizes and
  margins are range- and order-checked, paper comes from an allowlist, and the
  free-text identity fields reject quotes and backslashes outright rather than
  relying on the generators' escaping. `TestBrandValidateRejectsInjection`
  covers fourteen concrete payloads, including
  `Acme", evil: read("/etc/passwd"), x: "`.
- **Applying the brand splices `brand.typ`'s dictionary** rather than
  regenerating the file. `brand.typ` also defines `classification-colour`,
  `wordmark`, `label-text`, `field` and `pill`, all of which close over `brand`
  at their definition site — appending a redefinition would not reach them, and
  regenerating wholesale would mean this package owning code that belongs to
  whoever designs the templates. `TestApplyToTypstBrandFindsBlock` fails loudly
  if a reformat of the file breaks the splice, and
  `TestDefaultBrandMatchesBrandTyp` guards the one duplication accepted here:
  `DefaultBrand()` restating the shipped dictionary.
- **The LaTeX path now generates its `.tex` from the payload.** The header of
  `templates/latex/policy-document.tex` says LaTeX has no native data loading
  and points at "a small script"; `latexgen.go` is that script, in Go so it is
  covered by the same tests and escaping rules as the rest of the app. The
  committed `.tex` stays as the hand-filled reference the output mirrors.
  Section bodies are escaped and blank-line-separated blocks become paragraphs,
  with all-bullet or all-numbered blocks becoming `itemize`/`enumerate`; a
  block mixing markers stays prose rather than being silently restructured.
  This is one step ahead of the Typst template, which passes the body through
  as content — closing that gap means parsing markdown there, and the obvious
  way to do it (`eval()` on author-supplied text) hands the typesetter
  arbitrary code.
- **The engine log is filtered by diagnostic, not by line.** The first version
  dropped `unknown font family` warning lines the way `build.sh` does and left
  their framed source excerpts behind, pointing at `brand.typ` with nothing
  saying why — caught on a real render, not in a test. It now drops the warning
  together with its excerpt and keeps real errors' excerpts, which are the
  whole diagnostic value of the log when a template breaks.
- **Both pages had to be fixed for the global theme.** `theme_middleware.go`
  overrides `--ink`/`--muted` with `!important`, so the light backgrounds
  copied from `internal/policydocs` rendered light text on light panels —
  caught by screenshotting the pages, not by any test. Panels and cards now
  take their background from the theme variables, badges set both background
  and text explicitly (following `.pill` in `policydocs`, since anything taking
  only one of the two inverts when the theme flips), the template grid was
  renamed off `card-grid` because the theme's `[class*="card"]` rule was
  painting the gaps between cards, and the brand preview's body text is a
  `div` because the theme colours every `p` with `!important`.
- **New `-templates-dir` / `TEMPLATES_DIR`**, resolved the same way as
  `-docs-dir` and for the same reason: a systemd service runs from `/`, where a
  relative `templates/` finds nothing. Documented in `RUNTIME_ARGS.md`
  alongside `TYPST`, `TECTONIC` and `LATEXMK`.
- Two `#nosec G703` annotations, both justified in place: the workspace write
  takes `filepath.Base` of a compile-time registry entry into a directory this
  process just created, and the engine `os.Stat` reads an operator-set
  environment variable. No request input reaches either path.
- **`doc_template_brand` added to `dbsync`, `SnapshotVersion` bumped to 3.**
  The brand is authored content, not machine-local state, and without this a
  server migration or a data-set restore would silently drop it — noticed only
  when the next deliverable came out in the shipped default maroon. It is
  id-keyed so a repeated sync upserts rather than violating the `CHECK (id = 1)`
  constraint on the second pass, which is the shape a periodic sync takes. A v2
  backup restores with the table empty and the install falls back to the shipped
  brand; both directions are covered by tests.
- `internal/doctemplate/doctemplate_test.go` covers the brand/`brand.typ`
  round trip, the injection table, LaTeX escaping and list handling against the
  committed sample, the payload contract, bundle contents for both engines,
  filename sanitising, header flattening, the log filter, persistence including
  the upgrade path, and the routes. The end-to-end compile test skips where no
  typst is installed rather than failing.

## 2026-08-02 (the Change Log page was empty on every deployed server)

- **`/changelog` rendered "No change log entries found." on every server, and every `/knowledge/*` page 404'd.** Both read markdown files from the repository root, and the path they used was the bare file name — resolved against the process working directory. `scripts/setup.sh` installs only the binary to `/usr/local/bin` and the systemd unit sets no `WorkingDirectory`, so the service ran from `/` and `os.ReadFile("CHANGELOG.md")` failed on every request. It worked in development purely because `go run` and `./grc` are launched from the checkout. Reproduced by running the built binary from an empty directory before changing anything, and confirmed fixed the same way.
- New `internal/app/docs.go` resolves the documentation directory explicitly: `DOCS_DIR` / `-docs-dir` if set, otherwise the working directory, the binary's own directory, `<bindir>/../share/grc`, `/usr/local/share/grc` and `/usr/share/grc`, identified by the presence of `CHANGELOG.md`. Resolved once per process — the answer cannot change while the binary runs, and doing it per request would stat five directories on every page load.
- **`go:embed` was the obvious fix and is not available here.** It only reaches files inside the importing package's own directory, and the docs are at the repository root, where `Agents.md`, `CLAUDE.md` and the framework documents are meant to stay. Embedding from the root `main.go` would fix goreleaser builds only: `.goreleaser.yaml` builds `.` while `setup.sh` and `update.sh` build `./cmd/api`, so half the deploy paths would have kept the bug.
- **An explicit `DOCS_DIR` is used as-is with no fallback search.** An operator who names a directory wants a wrong path reported, not a different copy of the docs silently served in its place.
- `setup.sh` and `update.sh` now install the root `*.md` files to `<bindir>/../share/grc`, which is one of the searched locations, so no configuration is needed. `DOCS_DIR` is documented in `deploy/grc.env.example` and `RUNTIME_ARGS.md`.
- **`scripts/verify-install.sh` gained a Documentation section**, because this is exactly the failure shape the script exists for: the service is healthy, `/health` returns 200, and the defect only appears when somebody clicks a tab. It checks that `CHANGELOG.md` is present in the resolved directory, that the installed copy is **not stale relative to the checkout** (a stale copy renders the wrong history as convincingly as a stale binary serves the wrong code), and that the eight other `/knowledge/*` sources are there. `DOCS_DIR` is now also passed to the temporary instance the script may start — without it that process inherits the script's working directory, finds the docs in the checkout, and reports a healthy change log on a host where the real service cannot see them. The HTTP probe of `/changelog` reports **"not exercised"** rather than a pass when auth turns it away, since `/changelog` is in `protectedPrefixes`; the on-disk check is the one that runs unconditionally.
- Both failure paths now say what went wrong. The change log page names the full path it tried and points at `DOCS_DIR`; `/knowledge/*` distinguishes "no such document name" from "that document's file is not installed at `<path>`" — previously both were a bare 404, which is what made a deployment problem read as a routing one.
- Making the path a variable tripped `TestGosec` with G304 (file inclusion via variable). Annotated `#nosec G304` with the reason rather than worked around: the file name is a package constant and only the directory varies, from a flag/env or the fixed search list — no request data reaches it. `/knowledge/*` was already restricted to a fixed map of names, so no path from a URL parameter is ever joined.
- New `internal/app/docs_test.go`: candidate ordering (a checkout beats a system install), the fallback when nothing is found, a directory named `CHANGELOG.md` not counting as the marker, both handlers serving from a resolved directory, both error messages, and that every entry in `markdownKnowledgeDocs` names a file that actually exists at the repository root — a map entry pointing at a deleted file would otherwise only surface as a 404 in production.

## 2026-08-01 (template render payload, approval separation of duties, FTS5 build tag)

Closes the three follow-ups left open after the previous batch.

- **`GET /policies/:id/export.json`** emits the render payload the Typst templates consume, so the document pipeline is end-to-end: create a document in the editor, export the JSON, `./build.sh policy ../../doc.json`, get a PDF/A. Verified by doing exactly that against a running instance — an Encryption Standard authored over the API rendered correctly through `policy-document.typ`. Exposed as "Export JSON" in the editor.
- The payload is a dedicated `TemplateExport` type rather than the internal structs serialised directly. Row ids, ordinals and provenance detail have no business in a client deliverable's data file, and a separate type means renaming an internal JSON tag cannot silently produce PDFs with blank fields. Collections are always non-nil: Typst distinguishes `[]` from `null` and faults looping over the latter, which `TestTemplateExportEmitsEmptyArraysNotNulls` pins.
- **Separation of duties on approval.** Documents now record an `author`, taken from the session and never from the request body — an author a caller can choose is no basis for refusing a self-approval. `Service.SetRequireSeparateApprover` refuses approval by the document's own author, case- and whitespace-insensitively. It is wired on only when multi-user authentication is active: in single-user mode the author is necessarily the approver, so enforcing it unconditionally would make every document permanently unapprovable. Documents with no recorded author (created before the column, or under local mode) are not blocked by a check they could never satisfy.
- Under authentication the approver is now the **signed-in identity**, not the free-text `approved_by` field. Without that the control was defeatable by typing somebody else's name, which would have left both the separation check and the audit trail worthless. The author is also pinned across edits, since an edit that could rewrite it would be another way round the control.
- New `author` column with migrations for **both** backends — `ensureColumn` for SQLite and `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` for PostgreSQL. `CREATE TABLE IF NOT EXISTS` does not add a column to an existing table, so an upgraded server would otherwise fail on every policy read. Added to the `dbsync` column list too.
- **`-tags fts5` added to `.goreleaser.yaml` and to the `setup.sh`/`update.sh` builds**, ahead of the phase-3 corpus search that needs it. Verified rather than assumed: without the tag `CREATE VIRTUAL TABLE ... USING fts5` fails with `no such module: fts5`; with it, `MATCH` works. The goreleaser builds also gained the same `BuildVersion` stamp the source builds use, and the duplicated `CC`/`CXX` entries in the darwin build were removed.
- `/version` now reports `fts5`, backed by a build-tag constant, and `scripts/verify-install.sh` checks it. A binary built without the tag would otherwise look healthy and fail only when corpus search first ran — the same deferred-failure shape as the stale-binary problem the version check already covers. Confirmed both ways: tagged build reports the check green, untagged build warns.

## 2026-08-01 (deploy verification — setup/update scripts prove a remote server is current)

- **New `scripts/verify-install.sh`**, run automatically at the end of `scripts/setup.sh` and `update.sh` and safe to run standalone. It checks the deployed build revision, that the core and policy-module routes answer without faulting, that the schema carries all seven required tables (including the four policy tables), and that the optional external tooling each feature needs is present. Read-only: every request is a GET.
- **New `/version` endpoint and `-ldflags` build stamping**, because nothing else can detect a stale deploy. The first version of the verification script probed for the new routes and **failed to catch a deliberately stale binary** — `pageAccessMiddleware` guards whole path prefixes and aborts *before* gin routes the request, so an unregistered path under `/policies` returns exactly the same 401 as a registered one. Route probing cannot distinguish "missing" from "protected". `setup.sh` and `update.sh` now build with `-X grc/internal/app.BuildVersion=$(git rev-parse --short HEAD)` and the script compares the running binary's revision against the checkout. Verified against three cases: a correctly stamped binary passes, a binary stamped with an older revision fails with `STALE BINARY: serving abc1234, checkout is at 3cbef0d`, and a binary predating the endpoint entirely fails too.
- `/version` is deliberately unauthenticated (like `/health`) so verification works without credentials, and returns only a short commit hash and the Go version. `TestVersionPathIsNotProtected` pins that it stays outside `protectedPrefixes` — putting it behind auth would silently disable deploy verification.
- The pagination-overflow probe reports **"not exercised"** rather than a pass when auth turns it away before the handler. A check that always succeeds is worse than no check; the fix itself is covered by `internal/apiutil/pagination_test.go`. A 5xx there is still a hard failure.
- `setup.sh` gained opt-in `INSTALL_TYPST=1` (fetches the static musl Typst binary for `templates/build.sh`) and `INSTALL_CHROME=1` (chromium for the reporting module), both off by default because installing software on a server should be deliberate rather than a side effect of running setup. It now also starts the service and prints the `manage-user.sh` invocation for granting the new `/policies` pages — admins bypass page access, everyone else does not.
- `sudo -n` throughout the verification script: it runs from deploy hooks and non-interactive shells, where a password prompt would hang a deploy rather than fail it. Reads that need credentials degrade to warnings.
- `deploy/grc.env.example` documents `REPORTING_CHROME_PATH` and `REPORTING_CHROME_NO_SANDBOX`, which the reporting module has always read but which were never listed.

## 2026-08-01 (code review remediation — six findings, all fixed)

- **`internal/apiutil` — integer overflow panicked six endpoints.** `(page-1)*perPage` overflowed for a large enough `page`, so `?page=9223372036854775807&per_page=500` produced a negative slice bound and panicked; gin's Recovery turned it into a 500, reachable by anyone on controls, reports, nfrlink, riskregister, securitynfr and the risk register. `PaginateSlice` now rejects pages beyond the data *before* computing the offset, which bounds the multiplication by `total` so it cannot overflow at all. Checking the offset afterwards is not sufficient and the first attempt at this fix proved it: `1<<62 * 500` wraps to exactly **0**, silently returning the first page instead of an empty one, which a sign check does not catch. `ParsePagination` also clamps page and per-page. New `pagination_test.go` — the package had no tests.
- **`internal/app/access_control.go` — no grant made through the UI produced a working page.** Every page renders a shell then fetches `<page>/data`, and exact-match-only grants meant `/controls` returned the page and 403'd its data, so the page loaded and immediately showed a load error. The mirror image was equally broken: `/controls/*` granted the sub-paths but 403'd `/controls` itself. New `grantCovers` makes an exact grant cover its own supporting endpoints while still refusing *sibling* pages that are separately grantable (so `/controls` does not silently confer `/controls/manage`), and the `/*` form now includes its base path. Dormant in production because single-user mode is on, which is why it had gone unnoticed.
- **`internal/app/utilities.go` — the phase-1 `SnapshotVersion` bump made every existing backup unrestorable.** The import handler compared versions for equality, so raising the constant to 2 rejected every snapshot from an earlier build with a 400. It now accepts 1..current and rejects only *newer*. The claim in the previous changelog entry that "a v1 snapshot still imports cleanly" was true of `Import()` and false of the only path a user has to it.
- **`internal/dbsync/transfer.go` — `importTable` wrote explicit NULL for absent columns**, which defeats the schema DEFAULT and fails NOT NULL. That made *any* future column addition silently break restore of every older snapshot — the same class of bug as the version gate, found while writing the test for it. Columns a row does not carry are now omitted from the INSERT so the DEFAULT applies; statements are cached per column-set, which prepares once for a homogeneous snapshot.
- **`internal/app/theme_middleware.go` — the global-UI splice ran on every HTML response and cost ~17× the page size in garbage.** Six chained passes each converted the payload to a string and lowercased the whole document just to locate a tag. Replaced with one allocation-free case-insensitive scan and a single sized splice. Measured on a 120 KB page: **4931 µs → 48 µs, 2,025,219 B → 149,248 B, 16 allocs → 3, ~25 MB/s → ~2550 MB/s.** New `theme_middleware_bench_test.go` records those numbers and exists so a reintroduced whole-document copy shows up as a two-order-of-magnitude throughput collapse. Ordering is load-bearing and preserved: palette before responsive layer, since the latter overrides the former's `!important` rules from later in the cascade.
- **`/controls` shipped 1.87 MB per keystroke.** The list endpoint returned all 1193 controls including `requirements`, `discussion` and the NIST appendix — none of which the list pane renders — and the search box fired an unfiltered fetch on every `input` event. Added an opt-in `?slim=1` that drops the long-form fields (1,914,299 → 542,534 bytes) plus `GET /controls/data/:controlID` for the one control the detail pane is showing, and debounced the search at 250ms. Existing API consumers are unaffected: `slim` is opt-in and the Control Editor still fetches the full payload. Attempting to also drop `mapping_baselines` was reverted after measuring — the zero value marshals four nil slices as `null`, which is *longer* than the mostly-empty `[]` they carry, and made the response ~3 KB bigger.
- **`internal/jira/client.go` — unbounded `io.ReadAll` on responses from a caller-supplied base URL.** `safeJiraDialContext` constrains where a connection may go but not how much comes back. Now bounded at 32 MiB with an explicit error, matching the `io.LimitReader` pattern `ai_chat.go` already used. The utilities import body is likewise capped at 128 MiB via `http.MaxBytesReader`, applied before `FormFile` so it bounds both the multipart and raw-body paths.
- Minor: `err == sql.ErrNoRows` → `errors.Is` in four places; `policydocs.Coverage` no longer runs a second full scan of `rcsa_controls` unless a claim actually failed to match the in-scope set.

## 2026-08-01 (business and policy document templates)

- New `templates/` and `DOCUMENT_TEMPLATES.md`: typeset templates for policy documents (ISMS / ISO 27001 / NIST / FedRAMP / PCI DSS) and consulting reports (assessments, gap analyses), with the research behind the typography, build instructions and a rebranding guide. Implements the template half of phase 5 in `POLICY_MODULE_FRAMEWORK.md`; wiring a render endpoint into the app is still outstanding.
- **Typst is the primary path, LaTeX is supplied alongside.** The decisive factor is native JSON loading: `policy-document.typ` reads the record shape `internal/policydocs` already models, so rendering is "write the document as JSON, invoke the template" with no intermediate transformation. Typst is also a single Apache-2.0 static binary against a >1 GB TeX distribution, which matters given this repo ships self-contained goreleaser artifacts. `templates/latex/` mirrors the typography for clients whose house style is already LaTeX, but does **not** read the JSON — LaTeX has no native data loading and the alternatives are the error-prone path.
- Typography decisions are recorded with their reasons rather than left as taste: serif body against sans headings so the hierarchy survives a greyscale print; 11pt on a 150mm measure because that lands near 86 characters, the top of the 45–90 comfortable band; one accent colour used only for rules and table headers; font *stacks* rather than single names, with Liberation Serif/Sans metric-compatible with Times New Roman/Arial so pagination survives a rebuild on a different OS.
- Compliance-specific behaviour that is load-bearing rather than decorative: classification marked in both header and footer of **every** page (a page separated from its document must carry its own handling instruction), "Page X of Y" rather than "Page X", numbered sections (auditors and exception records cite by number), a revision-history table, and real signature lines — a policy that cannot be wet-signed is one a client cannot put in an audit file.
- `build.sh` supports `PDF_STD`, defaulting to **PDF/A-2b** for archival output. `PDF_STD=a-2b,ua-1` additionally enforces PDF/UA-1, under which Typst checks heading order, document title and alt text and fails the build if the document is not genuinely accessible. Both templates pass; verified in the emitted XMP (`pdfaid:part 2`, conformance `B`, plus `pdfuaid`).
- Everything was compiled and visually inspected rather than written blind, which caught four defects that would otherwise have shipped: `set heading(numbering:)` inside an `if` block is scoped to that block, so section numbering silently never applied; `\\` after `\vfill` in the LaTeX cover is vertical mode and errors; `\rowcolors` needs `\usepackage[table]{xcolor}`, not plain xcolor plus colortbl; and `tabularx` cannot be split across a custom environment's begin/end because it scans for its own `\end`, which fails as a misleading "Missing } inserted". A fifth was found in the build script itself — the LaTeX output collided with the Typst policy PDF and destroyed it, now fixed by giving LaTeX its own output directory.

## 2026-08-01 (policy authoring module — phase 2: control mapping and coverage matrix)

Implements phase 2 of `POLICY_MODULE_FRAMEWORK.md`: the policy module now joins the existing control database, which is the point at which it is worth more than a word processor.

- **New `policy_section_controls` table** (sqlite + postgres + `dbsync`) mapping a document *section* to a catalog control, with a coverage level and optional framework and note. Mapping lives on the section rather than the document so the coverage report can point at the text that makes the claim, which is what an assessor asks for. Unique index on `(section_id, control_id)`; section deletes cascade, so removing text removes the claims it backed.
- **No foreign key to `rcsa_controls`, deliberately.** The catalog is reseeded from JSON and controls can be deleted from the Control Editor; a cascade there would silently erase coverage claims, which are evidence. Instead a ref whose control has left the catalog survives, is flagged `known: false`, is listed under "stale mappings" on the coverage page, and produces a lint warning. `TestCoverageReportsOrphanedMappings` pins all three. Attaching still validates against the catalog at creation time — a ref that never resolved is a typo, not a forward reference.
- **The coverage report splits covered into three states**, which is the substance of the feature: `approved` (an approved document asserts it), `draft_only` (the only claims come from unapproved documents) and `supporting_only` (every claim is "supporting", so nothing actually asserts it). A single covered/uncovered count would hide exactly the gap the report exists to find — both of the latter read as covered in a spreadsheet and neither survives an assessor asking which approved document says so. Retired documents stop counting entirely.
- Coverage respects `control_family_visibility` via the same `familyVisibleSQL` pattern `internal/reports` uses — a family switched off in Family Filters is out of scope everywhere, and counting it here would report gaps the practice has already declared irrelevant. Control enhancements are excluded by default (at Moderate they outnumber the base controls and drown the report) behind an "Include enhancements" toggle. Filters for baseline, family, client and gaps-only; the counts always describe the full scope rather than the filtered rows, so "gaps only" cannot report "2 of 2 uncovered" for a set that is half covered.
- **Mapping is a draft-only edit**, matching section text: a coverage claim is a compliance assertion, so changing what a policy claims to satisfy costs a revision cycle. The approval snapshot now records the control mappings alongside the text, which is what makes that coherent rather than merely strict (`TestApprovalSnapshotRecordsMappings`).
- Markdown and HTML exports gained an inline `Satisfies: AC-2 (partial)` line under each mapped section plus a full Control Mapping table. An unqualified control id means the section satisfies it on its own, which is the reading an assessor will take.
- Two new lint warnings, both advisory rather than blocking: a document that declares a framework but maps no controls (it will not count in the coverage matrix), and a mapping pointing at a control no longer in the catalog. Neither blocks approval — mapping is a separate pass, a guideline may legitimately map to nothing, and a stale ref is a fact about the catalog rather than a defect in the text. Making either an error would make the linter something authors route around.
- UI: `/policies/coverage` (nav tab, allowed pages) with counter cards, a stale-mapping banner, per-control claim links and CSV export. The editor gained a debounced per-section control picker backed by a new `/policies/control-search` endpoint capped at 50 results — a dedicated lightweight endpoint rather than reusing `/controls/data`, so the editor is not coupled to another module's payload shape and does not pull ~1200 rows to fill a type-ahead.
- Two more instances of the theme bug from phase 1: the coverage table's `<th>` sits in an implicit `tbody`, so the injected dark theme's `thead th` rule never reached it and its light background stranded inherited light text; and the coverage-specific status pills had no colour coding. Both fixed with explicit colour pairs, and the new `.ctlchip` mapping chips carry no fill at all so they read in every theme.
- Tests: 14 new tests covering catalog validation and canonicalisation on attach, duplicate refusal, draft-only enforcement for both attach and detach, section-scoped detach, cascade on section delete, the approved/draft-only/supporting-only/uncovered split, family visibility, orphan reporting, enhancement exclusion, gaps-only with full-scope counts, retired documents dropping out, exports and snapshots carrying mappings, the unmapped-framework warning, and control search by id and name. Verified end-to-end against the real 1,193-control catalog: 177 controls in the Moderate AC-family scope, mapping three of them produced 3 approved / 174 uncovered.

## 2026-08-01 (policy authoring module — phase 1: document model, editor, approval)

Implements phase 1 of `POLICY_MODULE_FRAMEWORK.md`. No AI, no document upload, no template rendering — those are phases 3–5 and are deliberately absent.

- **New `internal/policydocs`** (`model.go`, `lint.go`, `repository.go`, `service.go`, `render.go`, `handler.go`, `pages.go`), following the CRM module's model/repository/service/handler split and its read-open / write-admin route pattern. New `policy_documents`, `policy_sections` and `policy_versions` tables in both `internal/db/sqlite.go` and `postgres.go`.
- **Document hierarchy with a tier rule.** Documents link to a parent (policy → standard → procedure → work instruction) and `validateDocument` refuses a parent that does not sit strictly higher: a policy hanging off a work instruction is how document sets become unnavigable. `checkNoCycle` walks the parent chain as a backstop, because the tier rule alone is not sufficient once a document can be re-typed after it was linked.
- **Approval is gated, and the gate is the point of the module.** `Approve` refuses unless the document is in review, has an approver, effective date and owner role, and passes `Lint` with no blocking findings. Refusals return `ApprovalError` carrying the findings, which the handler maps to HTTP 422 with the list attached — the editor renders what to fix rather than a bare "invalid". There is deliberately no `draft -> approved` edge: review is not skippable, and `CreateDocument` overwrites any caller-supplied status so a client cannot post `status: "approved"` past the gate.
- **The linter encodes two research findings.** Missing mandatory sections block approval, using a per-document-type set derived from FedRAMP's requirement that a family policy address purpose, scope, roles, responsibilities, management commitment, coordination and compliance — the lower tiers inherit only what is meaningful for them, since a management-commitment section on a work instruction is boilerplate nobody reads. Non-testable wording ("endeavour to", "where possible", "as appropriate", a statements section with no must/shall/should/may) warns but does not block: an assessor cannot test those, but it is the author's call to overrule.
- **`[[UNRESOLVED: …]]` placeholders block approval.** Nothing emits them yet — phase 4's AI drafting against a client profile is what will — but the gate ships now so it is already in place when that lands. A policy reaching a client with a placeholder in it is worse than one that was never generated.
- **Approval snapshots the text.** `policy_versions` stores the rendered Markdown as approved, including the document control block and not just the prose, so the snapshot is a self-contained record. Sections are then locked: editing an approved document is refused with a message pointing at "return it to draft", which preserves the snapshot — this is what ISO 27001 clause 7.5 version control exists to enforce. Re-approving increments `v1.0 → v2.0`.
- Review dates are computed from the effective date plus cadence (not from creation), and recomputed at approval, since approval is what starts the clock. A `due_review` filter surfaces documents at or past their next review — PCI DSS requires annual review where other frameworks accept 1–3 years, so cadence is per document rather than global.
- Deletion is refused for a document that is some other document's parent (`parent_document_id` carries no foreign key, because 0 is a legitimate "no parent", so SQLite would not catch the orphan) and for one with approved versions (retire it instead). Section routes are nested under their document and the service checks the section actually belongs to it, so guessing an id in the URL cannot reach another document's section.
- **`internal/dbsync`**: the three new tables are id-keyed in `syncOrder` — sections and versions hold a foreign key to the document and `parent_document_id` points back into the same table, so a renumbering sync would silently detach both. `SnapshotVersion` 1 → 2; a v1 snapshot still imports (missing tables yield nil and `importTable` returns early), pinned by `TestImport_ToleratesSnapshotWithoutPolicyTables`.
- UI: `/policies` (library, read-only) and `/policies/manage` (editor) added to `pageui` nav, `access_control.go` protected prefixes and `userManagementAllowedPages`. Markdown/HTML export and a printable `/policies/:id/view` preview. Section bodies render preformatted rather than through a Markdown parser — the rendering pipeline belongs to the template module, and a partial parser here would be thrown away when it lands.
- Three page elements (`.pill`, `.section`, `.finding`) initially set light backgrounds without setting a text colour, which left them invisible under the injected dark theme — it recolours the containing panel but not those classes, so the inherited light text stranded on a light fill. The pills now carry explicit colour pairs (the background *is* the status signal); `.section` and `.finding` dropped their fills entirely and carry the grouping/severity on the border, which reads correctly in all four themes.
- Tests: 16 new tests in `internal/policydocs` covering defaults and review-date computation, caller-supplied status being ignored, the approval gate (review-first, missing sections, unresolved placeholders), snapshot content and post-approval locking, version increments, illegal transitions, the parent tier rule and self-parenting, delete guards, reorder completeness, cross-document section scoping, the linter's warn-vs-block split, exports, HTML escaping of section bodies and titles, filters and validation. Plus two in `internal/dbsync`. Verified end-to-end against a throwaway server: the gate returns 422 with findings, the full happy path approves to v1.0, and editing an approved document returns 400.

## 2026-08-01 (policy authoring / document template module — research and design)

- New `POLICY_MODULE_FRAMEWORK.md`, following the `CRM_FRAMEWORK.md` / `RISK_REGISTER_FRAMEWORK.md` convention. **Design proposal only — no code.** Covers policy-document writing best practices (the policy/standard/procedure/work-instruction hierarchy, required section set, normative language), the framework-specific documentation demands of ISO 27001 clause 7.5, NIST 800-53 `-1` controls and ODPs, FedRAMP Rev 5/CR26, and PCI DSS v4.0.1 Requirement 12 + Targeted Risk Analyses; AI-drafting guardrails; templating engine comparison; and a proposed schema, route set and phasing for `internal/policydocs`, `internal/policycorpus`, `internal/clientprofile` and `internal/doctemplate`.
- Three findings drive the design and are worth reading even if the module is never built: FedRAMP RFC-0024 makes machine-readable (OSCAL) authorization data mandatory from **30 September 2026**, which argues for authoring structured content and treating documents as a render target rather than authoring prose; NIST ODPs and client scope/role facts must come from a client-profile store with unresolved values *blocking* approval, since that is where AI hallucination in compliance documents actually does damage; and SQLite FTS5 (`-tags fts5`, the driver already builds with `CGO_ENABLED=1`) is the right first retrieval layer over uploaded documents, ahead of any embedding store.
- Flags a deployment decision to settle before renderer code exists: Typst/Pandoc are external binaries and the repo currently ships self-contained goreleaser artifacts with no Docker path, so PDF/DOCX export should be runtime-discovered and degrade to in-process HTML/Markdown when absent.

## 2026-08-01 (iOS/mobile responsive layer; /controls Control Record density pass)

- **New global responsive layer**, `mobileStyleTag` in `internal/app/theme_middleware.go`, injected by a new `injectMobileStyle` step in `injectGlobalUI`. It is emitted at the end of `<head>`, *after* both the page's own `<style>` block and the palette, which is the only position from which it can override the themes' `!important` declarations (notably `body { padding-bottom }`). `TestMobileStyleFollowsThemeStyleInEveryTheme` pins that ordering for all four themes — get it wrong and the rules silently stop applying.
- The layer targets **iOS Safari specifically** (Android is explicitly out of scope), which drives three fixes that look arbitrary otherwise:
  - `input, select, textarea { font-size: max(16px, 1em) }` — iOS zooms the viewport whenever a focused control renders below 16px and never zooms back out. This was the single worst phone bug on the app; the AI dock input in particular was fixed-position at 14px, so focusing it left the page zoomed with no way to scroll back. The dock input is now 16px at source too.
  - `env(safe-area-inset-*)` on the body, the AI dock and the theme toggle, so the dock clears the iPhone home indicator and the toggle clears the notch/Dynamic Island in both orientations.
  - `dvh` units (behind `@supports`, with `vh` fallback) for the `/controls` scroll panes — iOS sizes `vh` against the *largest* viewport, so a `vh`-capped pane runs underneath the dynamic toolbar.
  - Also: `-webkit-text-size-adjust: 100%` (iOS inflates text in landscape without it) and `-webkit-backdrop-filter` alongside the unprefixed property on the dock and the `/controls` `main`.
- Accessibility, applied globally rather than per page: a single `:focus-visible` outline covering `[role="button"]` and `[tabindex]` as well as real interactive elements (several pages build clickable rows out of `div`s, which previously had *no* focus indicator at all); `touch-action: manipulation` to drop the legacy 300ms tap delay; a `prefers-reduced-motion` block; and WCAG 2.2 target sizes on coarse pointers — 44px for the theme toggle and dock button, 32px for small inline actions such as `.row-open-link`. The AI dock input gained an `aria-label` (it had only a placeholder).
- Horizontal overflow: `body { overflow-x: hidden }`, `max-width: 100%` on media, and the global nav script now wraps any bare `main table` in a `.table-wrap` scroll container. Wide tables were the main cause of whole-page sideways scroll on a phone.
- The top nav's 24 tabs used to wrap into a wall that consumed an entire phone screen. Below 900px it is now a single swipeable, snap-scrolling row, and `.global-top-shell` gains 44px of top padding so the fixed theme toggle no longer floats over the tabs — on desktop the nav's 150px right padding handles that, but that padding is dropped once the nav goes full-bleed.
- **`/controls` Control Record density pass** (`internal/controlcatalog/handler.go`). The right-hand pane rendered at body-copy size throughout, so a single control ran several screens. Detail body 20/18/24px padding → 12/12/16 at 13px/1.45; grid gap 16 → 10; cards 14px/16r → 10-12px/12r with 14px → 11px headings; metrics 12px/14r → 8-10px/10r with `strong` 24px → 15px and the metric grid floor 120px → 92px; `.mono` chips 13px → 11.5px; detail tags 12px → 11px; the `Control Record` header 16/18px → 10/14px. Roughly twice as much of a record is now visible without scrolling, and `Open Full Detail` remains for long reads.
- The detail pane is now capped at `72vh`/`72dvh` with its own scroll, matching `.rows`, so the two panes end level. Previously a control with a long NIST discussion stretched the page to many screens and left the control list scrolled off the top.
- `/controls` phone layout: `section.toolbar` (element+class, so it outranks the global layer's plain `.toolbar` fallback — which is deliberately *not* `!important` for exactly this reason) puts the three action buttons on one row instead of stacking all six children. Stacked, the filter toolbar cost a full screen before the first control was visible. Also caps the list at `52vh`/`52dvh` when stacked, and lowers `minVisibleControlRows` from 10 to 3 below 700px — ten rows of min-height is taller than a phone screen and pushed the detail pane out of reach.
- Tests: `TestMobileStyleFollowsThemeStyleInEveryTheme` (injection order and in-`<head>` placement across all four themes) and `TestMobileStyleCarriesIOSFixes` (pins the six iOS/accessibility declarations the layer exists for, since dropping one regresses the phone experience without breaking any page). Verified visually against a throwaway server at 390x844 and 1440x1000 in headless Chrome.

## 2026-07-31 (Chaos theme; Matrix palette realigned to morpheus)

- Added a **Chaos** theme, ported from the morpheus app (`internal/app/static/chaos.js` + `style.css` there). The theme toggle cycle is now dark → light → matrix → **chaos** → dark; `themeChaos` is accepted by `currentTheme` and persisted in the same `go_rcsa_theme` cookie as the others.
- **Matrix already existed here** and was not re-created from scratch, but its palette was slightly off from morpheus's. Realigned to morpheus's exact values — border `#00611a` (was `#004d00`), muted `#00a82c` (was `#00b32c`), surfaces `#001a00`/`#002b00`/`#003d00` (was `#000800`/`#001a00`/`#002a00`) — and added the `--danger`/`--success` variables it was missing. The `#00ff41` text/accent and Courier New stack were already correct.
- Extracted `matrixPaletteCSS` as the shared body of both themes, mirroring morpheus's own split: Chaos is Matrix plus the glitch engine, so it reuses the entire palette rather than duplicating it. `chaosThemeStyleTag` adds only a `.chaos-char` transition rule, since the glitch colour itself is set inline per character by the engine.
- New `chaosEngineTag` ports morpheus's glitch engine: on a timer it recolours a random sample of the characters on screen, clearing the previous sample first. Preserved from the original are the partial Fisher-Yates sampling (correct as the count approaches the candidate pool), the reverse-order `splitText` walk (offsets shift once a node is split), skipping surrogate-pair halves (splitting one corrupts the glyph), and skipping bare text in flex/grid parents (wrapping a character would promote it to its own grid item and break the layout).
- Adaptations for this app's server-rendered multi-page architecture: the sample root is `document.body` rather than morpheus's SPA `#app`, and there is no `repaint()` hook because there are no client-side view swaps — a navigation is a full page load, the interval re-samples on its own, and `clear()` tolerates spans a reload already discarded. morpheus's template literal for the glitch colour became string concatenation, because the source lives in a Go raw string literal which cannot contain a backtick (pinned by `TestChaosEngineSourceHasNoBacktick`).
- The engine is injected **only** when Chaos is the active theme, via a new `injectChaosEngine` step in `injectGlobalUI`, so the other three themes never pay for a timer that walks the whole DOM.
- Cadence knobs are read from `localStorage` on every tick and exposed as `window.GRCChaos` for console tuning (`grc-chaos-interval`, default 60s; `grc-chaos-density`, default 2 characters per 300 on screen). There is no settings UI — morpheus drives these from its Settings view, which this app has no equivalent of.
- Tests: new `internal/app/theme_middleware_test.go` — the file had **no** theme coverage before — covering theme-cookie recognition for all four themes (including case/whitespace normalization and the unknown-value fallback to dark), per-theme palette selection, engine-injected-only-for-chaos, the four-entry toggle cycle and its forward-looking labels, and an end-to-end `injectGlobalUI` assertion. Verified manually against a throwaway server: chaos serves palette + `.chaos-char` + engine, matrix serves the palette with no engine, dark serves neither, and the extracted engine passes `node --check`.

## 2026-07-31 (user-management backend diagnostics)

- `scripts/manage-user.sh` and `cmd/userctl` now print the repo directory and the **resolved database** to stderr on every run, before doing any work. This diagnoses the recurring "auth user not found" confusion, which is always the tool opening a different database than intended — most often `sudo` resetting the environment and dropping `DATABASE_URL`/`SQLITE_PATH`, so the precedence chain falls through to the built-in `users.db` default. The script also names *which* link in that chain won (`--db` flag, `$DATABASE_URL`, `$SQLITE_PATH`, `config/db.env` profile, or built-in default).
- New `describeSpec` in `cmd/userctl/main.go` renders the spec safely: a PostgreSQL password is replaced with `****` so the line can be pasted into a bug report, and a SQLite path is resolved to an absolute path and annotated `(did not exist — created empty)` when the file is missing. That annotation is the actual tell — `db.Open` creates and schema-initializes a SQLite file on demand, so pointing at a wrong path yields a silently empty database rather than an error.
- `set-password` now maps `authn.ErrNotFound` to an actionable message naming the user and pointing at `list` and the database line, instead of the bare `update password: auth user not found`.
- Documented the existing `list` command in the script's usage header (it was implemented but undocumented) and noted the stderr diagnostics there. Output goes to stderr so it never pollutes parsed `list` output.

## 2026-07-31 (single-user mode, 5-character password minimum)

- **Single-user mode.** `authn.Service` now refuses to create a second account: `createUser` checks `HasUsers()` before hashing and returns the new `ErrSingleUserMode` if any account already exists. The first account is unaffected, so `POST /auth/bootstrap-admin` and `admin_setup_login.sh` still work on an empty database. `authn.Handler.CreateUser` maps the new error to HTTP 409. Password changes on the existing account are **not** blocked — `UpdateUser` is untouched, so `scripts/manage-user.sh passwd -u USER` and the user-management page still reset the password normally.
- The restriction is a toggle, not a rewrite: `NewService` defaults `singleUser` to true, and `SetSingleUserMode(false)` restores full multi-user behaviour. None of the multi-user code (allowed-pages enforcement, last-admin guards, `ListUsers`, `DeleteUser`) was removed, so this reverts to a one-line change when the app needs more than one account.
- **Last-admin guard relaxed under single-user mode.** `DeleteUser` previously refused to remove the last admin (`ErrLastAdmin`), which made the sole account undeletable through the supported tooling once single-user mode was on — the only way out was hand-written SQL. It now skips that guard when single-user mode is on **and** the target is the only account, since deleting it empties `auth_users` and bootstrap can create a fresh admin. The guard still applies when other accounts remain: bootstrap refuses to run on a non-empty table and single-user mode blocks `CreateUser`, so deleting the last admin alongside surviving non-admin accounts would be unrecoverable. New `isSoleAccount()` helper.
- Removed the `/admin/user-management` link from the home page navigation (`internal/app/home.go`). The route itself is still registered and still admin-gated — only the link is hidden, since managing a single account from that page is mostly redundant.
- **Password minimum lowered from 12 to 5 characters.** Introduced `minPasswordLength = 5` in `internal/authn/service.go` and replaced both hardcoded `12` checks (create and update) with it, so the error text is now generated from the constant. Matching updates: `MIN_PASSWORD_LENGTH=5` in `scripts/manage-user.sh` (used by both the prompt and the length check) and the `5+ chars` placeholder in `internal/app/user_management.go`. Intended for internal single-user development only — a 5-character bcrypt hash is trivially brute-forced offline if the database leaks, so raise `minPasswordLength` before any shared or exposed deployment. Both constants carry a comment saying so.
- Fixed a stray `/re` prefix on line 1 of `security_test.go` (`/repackage main` → `package main`) that broke compilation of the root package — the same IDE-inserted-garbage failure mode `CLAUDE.md` documents for `main.go`.
- Tests: new `TestSingleUserModeRefusesSecondAccount` asserts the second account is refused with `ErrSingleUserMode` and that the surviving account can still have its password reset and authenticate with it. `TestSingleUserModeAllowsDeletingSoleAdmin` covers delete-then-rebootstrap, and `TestSingleUserModeStillRefusesLastAdminWithOtherAccounts` pins the unrecoverable case that must stay refused. Five existing tests that legitimately create multiple users (`TestGetSessionUserPopulatesAllowedPages`, `TestCreateAndUpdateAuthUser`, `TestCanDeleteAdminWhenAnotherAdminExists`, `TestCannotDeleteLastAdmin`, `TestPageAccessMiddlewareEnforcesAllowedPages`) now call `SetSingleUserMode(false)` with an inline comment, keeping the multi-user coverage intact rather than deleting it.

## 2026-07-29 (security patches — x/text, quic-go, crypto/tls)

- Remediated the three vulnerabilities `TestGovulncheck` was reporting against this module. Patched on `security-patches` and merged to `main`; `go test ./...` (including `TestGosec` and `TestGovulncheck`) is clean again — govulncheck reports no vulnerabilities in called code.
  - **GO-2026-5970** — infinite loop on invalid input in `golang.org/x/text`. Reached via `db.OpenPostgres` → `sql.Open` → `norm.Form.*`. Fixed: `golang.org/x/text` v0.37.0 → v0.39.0.
  - **GO-2026-5856** — Encrypted Client Hello privacy leak in `crypto/tls` (standard library). Reached from every outbound TLS client and the HTTPS server path. Fixed: `toolchain` directive go1.25.11 → go1.25.12 in `go.mod`. Builds now require Go 1.25.12; with the default `GOTOOLCHAIN=auto` the Go command downloads it automatically, so update hosts that pin `GOTOOLCHAIN` or install Go from a distro package.
  - **GO-2026-5676** — HTTP/3 QPACK trailer expansion memory exhaustion in `github.com/quic-go/quic-go`. Fixed: v0.59.0 → v0.59.1.
- `go mod tidy` pulled the rest of the `golang.org/x` set forward with x/text: `crypto` v0.52.0 → v0.53.0, `net` v0.55.0 → v0.56.0, `sync` v0.20.0 → v0.21.0, `sys` v0.45.0 → v0.46.0, `mod` v0.36.0 → v0.37.0, `tools` v0.45.0 → v0.47.0, `telemetry` to 2026-06-25. No source changes were needed — `bcrypt` and the rest of the app compile unchanged against the new versions.

## 2026-07-29 (offline user/password management script)

- Added `scripts/manage-user.sh` — create an auth user, reset a password, list users, or delete a user directly against the database, with no running server and no admin session. Covers the gap between `admin_setup_login.sh` (first admin only) and the `/admin/user-management` page (needs a working login): use it to add users on a server or to recover when the only admin password is lost.
- Commands: `list`, `create -u USER [--admin] [--pages /a,/b]`, `passwd -u USER [--revoke]`, `delete -u USER`. Passwords are prompted with no echo by default, or supplied with `--generate` (random 32-char, printed once) or `--password-stdin`; they are never passed as command-line arguments. `--revoke` also deletes that user's rows in `auth_sessions`, forcing a re-login after a reset.
- Backend selection: `--db SPEC` (postgres:// URL or SQLite path), else `$DATABASE_URL`/`$SQLITE_PATH`, else the active `config/db.env` profile from `scripts/db-switch.sh`, else `users.db`.
- New `cmd/userctl` does the actual work (bcrypt hashing cannot be done from bash): it opens the backend with `db.Open` and calls the existing `authn.Service` methods, so password rules (12+ chars), username normalization, and the last-admin guards on delete are identical to the HTTP API. The shell script is a thin front end; `USERCTL_BIN` points it at a prebuilt binary instead of `go run`.
- Documented in `RUNTIME_ARGS.md` under user management.

## 2026-06-29 (Utilities page — full data set export/import)

- Added a **Utilities** page (`/utilities`) for moving an entire data set between two disconnected deployments. The page offers two actions: **Export** downloads a single JSON snapshot of every managed table, and **Import** uploads such a snapshot to **overwrite** the database with its contents.
- New `internal/dbsync/transfer.go` adds `Export(*db.Conn) (*Snapshot, error)` and `Import(*db.Conn, *Snapshot) (Report, error)`, reusing the existing `syncOrder` table specs so the snapshot covers exactly the same tables/columns as the `-sync-to`/`-sync-from` modes. `Export` reads each table into JSON-serializable maps (TEXT `[]byte` normalized to string); `Import` is a full-replace (not a merge): in one transaction it deletes rows children-first and inserts the snapshot parents-first, then realigns PostgreSQL identity sequences for id-keyed tables. A failed import rolls back, leaving the database unchanged.
- New `internal/app/utilities.go` renders the page and wires `GET /utilities/export` (streams the snapshot as a `grc-data-export-<timestamp>.json` download) and `POST /utilities/import` (accepts a multipart file upload, validates the snapshot version, runs the overwrite, and returns a per-table row-count report). All three routes sit behind the admin middleware (open only in `-local-mode`) because the snapshot includes password hashes and import is destructive.
- Wired `registerUtilitiesRoutes` in `app.go`, added a **Utilities** tab to the primary and home navigation (`internal/pageui/nav.go`), and added `/utilities` to `protectedPrefixes` (`internal/app/access_control.go`).
- Note: importing clears `auth_sessions` (cascade from `auth_users` overwrite), so administrators may need to sign in again after an import.
- Tests: `internal/dbsync/transfer_test.go` covers a JSON round-trip export→import, overwrite semantics (destination-only rows removed), integer survival across the JSON float round-trip, FK-chain id preservation, and idempotent re-import.

## 2026-06-28 (top nav)

- Replaced side-panel navigation with a top navigation bar. The `nav.tabs` element is now injected above page content instead of in a collapsible left column. CSS changed from a two-column grid (`global-side-shell`) to a vertical flex stack (`global-top-shell`); tabs display as a horizontal wrapping flex row inside a `<header class="global-top-nav">`. The collapse toggle is removed. Right padding on the nav bar (150 px, collapsed to 12 px on narrow viewports) keeps tabs clear of the fixed theme-toggle button.

## 2026-06-28 (update script)

- Added `update.sh` — redeploy script mirroring the morpheus pattern: `git pull --ff-only`, rebuild `./cmd/api` into a temp binary and `sudo install` to `/usr/local/bin/grc`, then `sudo systemctl restart grc`. Env overrides: `GRC_ENV_FILE`, `GRC_BIN_PATH`, `GRC_SERVICE_NAME`. No migration step needed — schema changes apply automatically on startup.

## 2026-06-28 (PostgreSQL migration)

- Switched the Morpheus deployment from SQLite to a local PostgreSQL backend (`grc` role/database on `localhost:5432`). Existing SQLite data synced to PostgreSQL via `scripts/db-sync.sh push` before cutover. Service env updated with `DATABASE_URL`; `SQLITE_PATH` removed.

## 2026-06-28 (nginx / service deployment)

- Added `deploy/grc.nginx` — nginx reverse-proxy site config routing `grc.l3d.local` → `127.0.0.1:8081`.
- Updated `deploy/grc.env.example`: default `LISTEN_ADDR` changed to `127.0.0.1:8081` (loopback, behind nginx) and `TRUST_PROXY=true` added.
- App deployed as a systemd service (`grc.service`) on Morpheus, co-hosted with the morpheus app; nginx routes by `server_name` (`grc.l3d.local` → :8081, `morpheus.l3d.local` → :8080).

## 2026-06-28

- Added **Matrix theme** as a third UI theme option alongside Dark and Light. Black background, bright-green (`#00ff41`) text with a glow on headings, monospace font (`Courier New`) throughout, and dark-green borders. The theme toggle button now cycles Dark → Light → Matrix → Dark (icon: ☀ / 🌙 / ▓). Implementation: new `themeMatrix` constant and `matrixThemeStyleTag` CSS block in `theme_middleware.go`; `currentTheme()` and `injectThemeStyle()` updated to handle the third value; `themeToggleTag()` JS updated to cycle through all three via `indexOf`.

- Added **AI Chat usage tracking** on every AI Chat page visit. A "Usage ▶" toggle button (next to Ask / Clear Chat) fetches `GET /ai-chat/usage` and shows token counts (today + all-time, requests + input/output tokens) broken down by provider (Claude, OpenAI, Azure OpenAI). Backend: new `ai_usage_log` table in both SQLite and Postgres schemas; `ai_usage_store.go` implements a `aiUsageStore` interface with a DB-backed store (writes a row per successful call) and an in-memory fallback for local/no-DB mode. Token counts are extracted from the raw API response (`usage.prompt_tokens`/`completion_tokens` for OpenAI format, `usage.input_tokens`/`output_tokens` for Claude). `askOpenAI`, `askClaude`, and `askAzureOpenAIWithMicrosoftSSO` now return a `(string, aiChatTokenUsage, error)` triple so the caller can log usage without touching the response extraction helpers.

- Added **Anthropic Claude API** as a third AI provider in the AI Chat Gateway (`/ai-chat`). Backend: `askClaude` posts to `https://api.anthropic.com/v1/messages` with `x-api-key` and `anthropic-version: 2023-06-01` headers, passes `system` as a top-level field (not a chat message role), extracts text from the `content[].text` response, and defaults the model to `claude-sonnet-4-6`. `validatedClaudeEndpoint` restricts custom endpoints to `api.anthropic.com` over HTTPS. UI: new "Anthropic Claude API" option in the provider dropdown shows a dedicated API key field, model field (default `claude-sonnet-4-6`), and optional endpoint override; the Azure notice is now hidden when a non-Azure provider is selected. Tests added for endpoint validation (default URL, official URL, rejection of HTTP/wrong-host/user-info URLs). The `github.com/anthropics/anthropic-sdk-go` module was already present as an indirect dep; the integration uses direct HTTP for consistency with the existing OpenAI path.

## 2026-06-27

- Fixed AES-256-GCM key path for AI chat session encryption when using the PostgreSQL backend: `configureAIChatSessionEncryption` now takes an explicit key path; `app.go` derives it as `<sqlite-path>.aichat.key` for SQLite and `ai_chat.key` (relative to CWD) for Postgres, preventing a new key from being generated on every Postgres restart when the SQLite path doesn't exist.
- Fixed `decryptAIChatSessionPayload` to return an explicit error instead of `([]byte(raw), nil)` when `gcm.Open` fails, so ciphertext tampering or key-rotation failures surface as crypto errors rather than being silently swallowed and misreported as JSON parse errors.
- Fixed `constantTimeEquals` in `internal/authn/handler.go` and `internal/app/auth.go` to remove the `len(a) != len(b)` early-return guard before `subtle.ConstantTimeCompare`, which leaked the expected token's byte-length via response timing; `subtle.ConstantTimeCompare` already handles differing lengths safely.
- Fixed SSRF error message in `safeJiraDialContext` (`internal/app/jira_pages.go`) to use a generic "connections to private or reserved addresses are not allowed" string instead of embedding the resolved private IP, preventing internal network topology disclosure in HTTP responses.
- Fixed `authn.Handler.Login` to return a generic "too many failed login attempts, please try again later" message on 429, instead of forwarding the full `ErrTooManyAttempts` error string which included the exact remaining lockout duration.
- Fixed `authn.Service.Authenticate` to perform the dummy bcrypt comparison even when the account is locked out, eliminating the timing oracle that distinguished locked accounts (microseconds) from non-existent ones (bcrypt time).
- Fixed unbounded growth of the `loginAttempts` map in `authn.Service`: `recordFailedLogin` now evicts fully-expired entries for other keys on each call, preventing indefinite accumulation under credential-stuffing with many unique usernames.
- Fixed `normalizeAndValidate` in `internal/riskregister/service.go` to include `RiskID` in the `maxRiskFieldLength` cap; it was the only stored field not bounded by the 10,000-character limit.
- Fixed risk register embedded JavaScript `load()` to handle both the plain array and the paginated `{items, total, page, per_page}` response shapes; previously any request with `?page=` or `?per_page=` params caused the table to silently render empty.
- Added RFC 6598 CGNAT range `100.64.0.0/10` to `isDisallowedJiraTargetIP` in `internal/app/jira_pages.go`; Go's `net.IP.IsPrivate()` omits this range, leaving it unblocked on cloud/ISP environments that route it to internal management interfaces.

## 2026-06-26

- Added a built-in, one-shot **database sync mode** to the application binary
  for moving data between the two backends in either direction
  (SQLite ↔ PostgreSQL). New `internal/dbsync` copies the managed tables with
  upsert/merge by each table's natural key (so destination-only rows survive),
  preserves ids on the FK-linked CRM/`stored_json_documents` tables and
  realigns PostgreSQL identity sequences afterwards, rebuilds the derived
  `security_nfr_control_links` table, and skips ephemeral `auth_sessions`.
  Wired into `app.Run` via new `-sync-to` / `-sync-from` flags (`SYNC_TO` /
  `SYNC_FROM` env), each taking a `postgres://` URL or SQLite path that
  `db.Open` engine-detects; the binary copies and exits without serving and
  logs a per-table row count. Covered by an `internal/dbsync` SQLite↔SQLite
  test (copy, merge-preserve, idempotent re-sync/update, derived-table
  replace).
- Added dev/run helper scripts and a how-to for switching backends and syncing:
  `scripts/db-switch.sh` (flip the active `local`/`remote` profile in a
  gitignored `config/db.env`, password-masked status), `scripts/run-app.sh`
  (build + run the active profile — SQLite/local-mode or PostgreSQL/auth, one
  binary), and `scripts/db-sync.sh push|pull` (export local → remote / pull
  remote → local with a confirmation prompt). Added `config/db.env.example`,
  gitignored `config/db.env`, and `docs/DATABASE.md` documenting backend
  selection, the two run profiles, the local→remote export workflow, and
  researched alternatives (`pgloader`, `pg_dump`+transform) and why the in-app
  sync is preferred. `RUNTIME_ARGS.md` documents the new flags/env.
- Added an optional external **PostgreSQL** storage backend alongside the
  existing SQLite one, selected at runtime by `DATABASE_URL` /
  `-database-url` (empty = SQLite, unchanged default). New `internal/db`
  pieces: `Conn`/`Tx` wrappers (embedding `*sql.DB`/`*sql.Tx`) that
  transparently rebind `?` placeholders to `$N`, an `Insert` helper that uses
  `RETURNING id` on Postgres and `LastInsertId` on SQLite, a
  `GroupConcatDistinct` dialect shim, and `OpenPostgres` (via
  `jackc/pgx/v5/stdlib`) which creates a Postgres-native schema
  (`BIGSERIAL`/`BIGINT` FKs, `DOUBLE PRECISION`). Repositories/services
  (`user`, `authn`, `riskregister`, `crm`, `controlcatalog`, `securitynfr`,
  `nfrlink`, `reports`, plus the app stores and `app.go` wiring) now take
  `*db.Conn` instead of `*sql.DB`; the seven `LastInsertId` inserts were moved
  to `db.Insert`. Rewrote the few SQLite-only SQL constructs for cross-engine
  compatibility: `GROUP_CONCAT`→`STRING_AGG`, `COLLATE NOCASE`→`LOWER(...)`,
  and `HAVING <alias>`→`HAVING <full aggregate>` (Postgres rejects output
  aliases in `HAVING`). `ON CONFLICT ... DO UPDATE SET ... excluded.*` upserts
  were already valid on both engines. Tests still run on SQLite, so
  `go test ./...` (including `TestGosec`/`TestGovulncheck`) remains the
  security gate and passes; added `github.com/jackc/pgx/v5` to `go.mod`.
- Taught `scripts/setup.sh` and the deploy assets about the Postgres backend:
  when `DATABASE_URL` is set the env file/bootstrap loopback run use Postgres
  instead of SQLite, and when `PG_ADMIN_URL` is supplied the script provisions
  the role/database idempotently (`CREATE ROLE`/`CREATE DATABASE`,
  auto-generating `PG_PASSWORD`) and assembles `DATABASE_URL` from
  `PG_DB`/`PG_USER`/`PG_HOST`/`PG_PORT`/`PG_SSLMODE`. `deploy/*.service` now
  waits on `network-online.target` (remote DB reachability) and
  `deploy/*.env.example` + `RUNTIME_ARGS.md` document `DATABASE_URL` and the
  `PG_*` provisioning knobs.
- Added a bare-metal install script `scripts/setup.sh` (modeled on the
  morpheus `setup.sh`, adapted for this app's SQLite + token-bootstrap model):
  checks prerequisites (`go`/`curl`/`openssl`), builds `./cmd/api` and installs
  it to `/usr/local/bin/grc`, creates the `grc`
  system user and the `/var/lib/grc` SQLite data directory,
  writes a mode-0600 env file with generated `ADMIN_TOKEN`/`SETUP_TOKEN`,
  provisions the first admin login by briefly running the binary on a loopback
  port and POSTing `/auth/bootstrap-admin` (201 created / 409 already exists),
  installs+enables the systemd unit, and prints generated credentials once.
  Idempotent: existing env files, users, databases, and admins are left alone.
- Added the systemd unit `deploy/grc.service` (StateDirectory
  for the SQLite dir, `ProtectSystem=strict`, `CAP_NET_BIND_SERVICE` for low
  ports) and `deploy/grc.env.example` documenting the runtime
  env vars. Verified the binary starts from outside the repo (embedded seed
  fallback) and that the bootstrap flow returns 200/201/409 as expected.
- Branded every browser-tab page title with the `<Page> · GRC`
  suffix across all HTML handlers (`internal/app`, `controlcatalog`,
  `securitynfr`, `nfrlink`, `reports`, `riskregister`, `crm`), including the
  dynamic risk-register and CRM-grid titles; updated the home-page title to the
  same suffix form and the `TestJSONViewPageSmoke` title assertion to match.
- Added a branded footer to the home page (`/`, `internal/app/home.go`):
  `© <year> GRC · Risk & Control Self-Assessment platform`,
  with the year rendered dynamically via `time.Now().Year()`. Styled for the
  light page background (it sits outside the dark content panel).
- Added a top-level `README.md` branded `grc`: quick start
  (build/run/local-mode), the local fmt/vet/test gate, an architecture overview
  of the `internal/*` packages, the stack summary, and links to the existing
  doc set (`Agents.md`, `RUNTIME_ARGS.md`, the framework docs, `FAQ.md`,
  `CHANGELOG.md`, and `internal/reporting/README.md`).
- Renamed the Go module and output binary from `go_rcsa` to `grc`:
  updated the `module` directive in `go.mod`, every `grc/internal/...`
  import path across the `.go` sources, the `binary:` names in `.goreleaser.yaml`,
  the `BINARY`/path references in `run_local.sh`/`admin_setup_login.sh` and the
  `scripts/*` helpers, the `/grc` entries in `.gitignore`, and the
  docs (`Agents.md`, `RUNTIME_ARGS.md`). Updated the two human-facing UI titles in
  `internal/app/home.go` to "GRC …". `go fmt` re-grouped imports in
  a few files since the new path sorts differently; `go vet`, `go build`, and
  `go test ./...` (incl. the `TestGosec`/`TestGovulncheck` gates) all pass. The
  standalone domain term "RCSA" (Risk and Control Self-Assessment) was left intact.
- Finalized the control-assessment + CRM work: ran `go fmt`, which restored the
  missing trailing newline in `internal/app/login.go` (a stray IDE edit) so the
  `gofmt` gate is clean; verified `go vet ./...` and `go test ./...` (including
  the `TestGosec`/`TestGovulncheck` security gates) all pass.

## 2026-06-25 (later)

- Added a second report type to `internal/reporting`: the Security Control Assessment Report (`RenderControlAssessment` / `ControlAssessmentHTML`, template `control_assessment.tmpl`, `GET /reports/control-assessment.pdf`). It documents NIST SP 800-53A-style assessment results with a satisfied/partial/not-satisfied/N-A donut, a controls-by-family bar chart, a paginating results table with result badges, and keep-together deficiency blocks. Added a golden HTML test and extended the `pdf_smoke` test to render both report types.
- Added a new `internal/crm` consulting CRM module: clients → engagements → billable time entries → billing, following the loop common to open-source small-business/consulting tools (Dolibarr, Invoice Ninja, SolidInvoice, Solidtime). New SQLite tables `crm_clients`, `crm_engagements`, `crm_time_entries` (FK cascade) with repository → service → handler layering matching the risk register.
- CRM features: a dashboard (`/crm`) with practice KPIs, top clients, and recent entries; config-driven CRUD grids for clients/engagements/time (`/crm/clients`, `/crm/engagements`, `/crm/time` plus `…/manage` editors) with relation dropdowns; a billing rollup (`/crm/billing`) of billable/unbilled/invoiced totals with one-click "mark invoiced" (`POST /crm/engagements/:id/invoice`). Billing rates are snapshotted on each time entry so later rate changes don't rewrite logged work; non-billable time contributes hours but $0.
- Wired CRM read routes plus admin-guarded mutation routes into `internal/app`, added CRM tabs to `internal/pageui`, home-page links, and `CRM_FRAMEWORK.md` (exposed at `/knowledge/crm`). Added CRM service tests covering the client/engagement/time flow, rate snapshotting, billing rollups, validation, not-found, and FK cascade delete.

## 2026-06-25

- Added a new `internal/reporting` package: an HTML/CSS-to-PDF reporting module that renders Go data structs to HTML via stdlib `html/template` (auto-escaped for untrusted GRC data) using layout composition (base layout + cover/table/header-footer partials + a Risk Assessment report template), then converts to PDF behind a pluggable `Renderer` interface.
- Implemented the primary `ChromeRenderer` (in-process headless Chrome via chromedp `Page.printToPDF`, pinned `github.com/chromedp/chromedp v0.12.1`): pooled browser process, per-render context timeouts honoring caller cancellation, JavaScript disabled by default, all `http(s)`/`file`/`ftp`/`ws(s)` loads blocked, and content injected via `setDocumentContent` (no navigation) to prevent SSRF/local-file exfiltration; assets are inlined as `data:` URIs.
- Pinned chromedp to `v0.12.1` deliberately: newer chromedp/cdproto releases pull in `github.com/go-json-experiment/json`, whose named-slice variadic generic crashes the `golang.org/x/tools` SSA builder used by `govulncheck` (the `TestGovulncheck` security gate). v0.12.1 (cdproto Jan 2025) is the last release before that dependency, keeps the security gate green, and exposes the same `printToPDF`/allocator API the renderer uses.
- Added a `GotenbergRenderer` stub (interface + docs only) documenting the chromedp-vs-Gotenberg tradeoff, including Gotenberg's PDF/A archival support, as a clean extension point.
- Reports support a cover page (title/logo/classification/timestamp/author/owner/version), running header/footer with `Page X of Y`, a classification banner, an optional diagonal DRAFT watermark, multi-page tables that repeat headers and avoid row splits, keep-together findings, and server-rendered inline SVG charts (`charts.go`) for deterministic output. Branding colors/fonts are validated (`theme.go`).
- Added an example endpoint `GET /reports/risk-assessment.pdf` (query params `draft=false`, `download=1`) wired in `internal/app/app.go` via a self-registering handler; Chrome can be tuned with `REPORTING_CHROME_PATH` and `REPORTING_CHROME_NO_SANDBOX`.
- Added structured `log/slog` timing logs for render operations, a package `README.md` (how to add a report type, chromedp-vs-Gotenberg + PDF/A, Chrome runtime dependency, security model, limitations), golden-file HTML tests (fast, browser-free) plus security/escaping unit tests, and a build-tagged (`pdf_smoke`) smoke test that renders a real PDF and is skipped when no browser is present so `go test ./...` stays browser-free.

## 2026-06-24

- Added a light, grey-and-maroon business theme as an alternative to the existing dark theme. `internal/app/theme_middleware.go`'s former `darkThemeMiddleware` is now `themeMiddleware`, which reads a `go_rcsa_theme` cookie (default `dark`, preserving current behavior) and injects either `darkThemeStyleTag` or the new `lightThemeStyleTag` before `</head>`. A fixed top-right toggle button (`themeToggleTag`, injected on every HTML page) flips the cookie client-side and reloads; both themes share the same selector coverage so the switch is consistent across every page. Refactored the side-nav panel and AI quick-prompt dock CSS to consume the same `--ink`/`--muted`/`--line`/`--panel`/`--surface-strong`/`--hover` custom properties instead of hardcoded dark hex values, so they follow whichever theme is active. Added mobile breakpoints for the new toggle control (existing side-nav/AI-dock breakpoints were already responsive).
- Fixed a stored-XSS gap in `internal/app/asset_types.go` where admin-editable control IDs/names were written into `innerHTML` without escaping (every other interpolation in the file already used the page's `esc()` helper); added the same `esc()` helper to `internal/app/wiz_rules.go` as a defensive baseline since it had none.
- Hardened outbound Jira API calls against SSRF: `internal/app/jira_pages.go`'s default HTTP client now resolves the configured `base_url` host and refuses to dial loopback/private/link-local/multicast addresses (e.g. cloud metadata endpoints or internal services) at actual connection time, so self-hosted Jira on any public domain still works while internal targets are blocked. Added `TestIsDisallowedJiraTargetIP`.
- Added login rate-limiting/lockout to `internal/authn/service.go` (5 failed attempts locks a username out for 15 minutes, in-memory), switched the `BootstrapAdmin` setup-token comparison to constant-time (`internal/authn/handler.go`), and capped password length at 256 bytes before hashing (bcrypt truncates at 72 bytes regardless, so longer inputs only cost allocation time).
- Added a `-trust-proxy`/`TRUST_PROXY` option (default off, documented in `RUNTIME_ARGS.md`) gating whether `X-Forwarded-Proto` is honored when marking auth cookies `Secure`; previously this client-supplied header was trusted unconditionally in `internal/app/ai_chat.go` and `internal/authn/handler.go`.
- Encrypted Microsoft OAuth tokens at rest: `internal/app/ai_chat_sessions.go` now AES-256-GCM encrypts the `ai_chat_sessions.data_json` payload using a key generated on first run and stored in a sibling file next to the SQLite database (kept out of the DB itself), with backward-compatible reads of pre-existing plaintext rows. Added a startup cleanup pass that deletes AI chat sessions untouched for 30+ days.
- Stopped persisting the Jira API token to browser `localStorage` in `internal/app/jira_pages.go` (base URL, email, and other form state are still persisted for convenience; the token is not).
- Added opt-in pagination (`page`/`per_page` query params, matching the existing `nfrlink` pattern) to `riskregister.Handler.List`, and capped risk-register free-text fields at 10,000 characters.
- Deduplicated session lookups: `pageAccessMiddleware` now stashes the resolved session user on the Gin context so `adminTokenMiddleware` can reuse it instead of querying the session table a second time for the same request.
- Batched `controlcatalog` and `securitynfr` catalog reseeds into a single transaction each (`Repository.UpsertMany`) instead of one commit per row.
- Investigated `db.SetMaxOpenConns(1)` as a proposed SQLite hardening change; reverted it after it deadlocked `internal/nfrlink.List` (which holds an open `*sql.Rows` while issuing a nested query for override data) — flagging that nested-query-while-rows-open pattern as a known constraint on this DB layer rather than fixing every call site in this pass.
- Replaced a `fmt.Sprintf` call in the hot-path `controlid.Normalize` with plain string concatenation.

## 2026-05-02

- Added production-oriented runtime/setup documentation in `RUNTIME_ARGS.md` covering first-boot admin bootstrap (`/auth/bootstrap-admin`), login/session auth (`/auth/login`, `/auth/me`, `/auth/logout`), and admin auth-user management endpoints (`/auth/users`).
- Updated `Agents.md` security policy to require: pre-finalization internal library patch checks, daily CI security patch checks, weekly full security audits, remediation updates on the `security-patches` branch, and mandatory `CHANGELOG.md` entries both when vulnerabilities requiring patches are identified and when patch remediation is applied.
- Added `FAQ.md` with instructions for merging a fully tested `security-patches` branch back into `master`, including local merge, test verification, push, and optional branch cleanup steps.
- Updated `RUNTIME_ARGS.md` with a how-to section for merging a fully tested `security-patches` branch back into `master`, including optional branch cleanup commands.
- Updated the home page (`/`) to include direct links to all repository markdown knowledge/how-to files and added read-only routes under `/knowledge/:name` for `Agents.md`, `CHANGELOG.md`, `RUNTIME_ARGS.md`, and `FAQ.md`.
- Added `internal/jira` Jira Cloud REST connector (get/search/create issue + get project, API-token auth, 429/503 retry support), added `JIRA_CONNECTOR.md` with Atlassian-based integration methodology and source links, and exposed it in the home page knowledge links via `/knowledge/jira`.
- Added a new editable security risk register framework with persistent SQLite storage and endpoints (`/risk-register`, `/risk-register/manage`, `/risk-register/data`, plus admin write APIs), and added `RISK_REGISTER_FRAMEWORK.md` documenting the NIST-based methodology and field model.
- Updated the home page to use an enterprise-style side navigation panel (persistent on desktop, responsive wrap on smaller screens) while preserving the full navigation link set.
- Replaced the previously injected global navigation dropdown with a global side-navigation transform in HTML middleware so pages with `nav.tabs` now render as collapsible left side panels with persistent collapse state.
- Added an admin-only User Management page (`/admin/user-management`) for auth-user create/update/delete, added per-user `allowed_pages` storage in `auth_users`, and enforced page-level access control for authenticated non-admin users based on allowed page paths.
- Added `TestGovulncheck` to `security_test.go` so vulnerability scanning is part of normal `go test ./...` execution, and updated `Agents.md` test/CI instructions accordingly.

## 2026-04-19

- `/reports` was restyled to use the shared dark table/card treatment, smaller typography, visible filter legends, and an `All Controls` filter mode; `/security-nfrs/links` was aligned to the same table palette.
- Navigation and layout coverage was expanded across primary and detail pages, page-shell widths were standardized to `1320px`, and `/changelog` was aligned visually with `/reports`.
- Slow app navigation tests were refactored to use lightweight fixtures instead of heavy seeded bootstrap, which removed the earlier `go test ./...` stall and kept the full suite passing.
- AI chat and Microsoft SSO were hardened by rejecting unsafe outbound endpoints, disallowing Azure endpoint override, tightening session-cookie behavior, clearing the session cookie on logout, tightening JSON file permissions to `0600`, and fixing the ignored XML escaping error path.
- `gosec` was installed, the reported issues were resolved or explicitly documented where safe, and a root-level `TestGosec` regression test now enforces a clean security scan.
- Windows build scripts now auto-discover an installed llvm-mingw toolchain under `.toolchains/`, respect `WINDOWS_TOOLCHAIN_DIR`, and fall back to `x86_64-w64-mingw32-gcc/g++` on `PATH` instead of requiring one hardcoded dated directory name.
- Goreleaser now uses the real `grc` binary name in release artifacts, and the release build script resolves Darwin cross-compilers from `.toolchains/`, `DARWIN_TOOLCHAIN_BIN_DIR`, or `PATH` instead of a machine-specific osxcross path.
- A new release helper script can upload the built `dist/` artifacts to a configurable Google Drive destination through `rclone`, with optional build-first behavior and passthrough `rclone` flags.
- Generated GoLand module files are no longer committed; `.idea/modules.xml` and `*.iml` are now ignored so the IDE can regenerate project metadata locally without repo-level conflicts.

## 2026-04-18

- `/changelog` was added as an in-app page, the primary navigation was updated to expose it, and shared navigation/palette regression coverage was introduced for the main UI pages.
- `/reports` gained CIA/threat summaries, radio-based filters, refined filter styling, and lower-overhead aggregation with supporting handler and service regression tests.
- `nfrlink.Rebuild()` was optimized to resolve controls from memory instead of querying SQLite per mapping token.
- Goreleaser build and smoke-test scripts were added and then extended so release artifacts include the required seed JSON files and use the correct Windows cross-compiler with preflight checks.
