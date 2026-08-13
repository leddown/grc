# regmap — regulation → NIST 800-53 crosswalk

Ingest an arbitrary cybersecurity or resilience regulation, map its requirements
to **NIST SP 800-53 Rev 5** controls, and produce an auditable crosswalk.

The source framework is **not hardcoded**. DORA, PCI DSS, NIS2 and the EU Cyber
Resilience Act ship as starter profiles; adding another regulation means writing
a YAML file, not editing the pipeline.

Because source text is messy and the domain is regulatory, the tool is
**human-in-the-loop by design**. It pauses at every point where machine output
could be wrong, and never treats its own parsing or its LLM suggestions as
final. Nothing reaches `approved` without you.

```
ingest ──▶ [GATE 1: review segmentation] ──▶ map ──▶ [GATE 2: review mappings]
       ──▶ suggest ──▶ [GATE 3: review suggestions] ──▶ report
```

---

## Install

```sh
go build -o bin/regmap ./cmd/regmap
```

Note the `bin/` prefix: a `./regmap` binary at the repo root would collide with
the `regmap/` assets directory. `bin/` is gitignored.

For PDF input, `pdftotext` (poppler-utils) is used when available because its
layout mode preserves headings far better; without it the tool falls back to a
pure-Go PDF extractor and says so.

---

## The full loop

```sh
# 0. What can this thing read?
regmap frameworks

# 1. Ingest. The framework is auto-detected; --framework always overrides.
regmap ingest --in dora.pdf
regmap ingest --in dora.pdf --framework dora   # explicit

# 2. GATE 1 — confirm the framework, then fix the segmentation.
regmap review --gate 1

# 3. Resolve the framework's seed crosswalk. Only GATE 1-approved
#    requirements advance; anything with no seed hit becomes UNMAPPED.
regmap map

# 4. GATE 2 — validate every proposed mapping.
regmap review --gate 2

# 5. Optional: ask Claude about the requirements that are still UNMAPPED.
export ANTHROPIC_API_KEY=sk-ant-...
regmap suggest --with-llm

# 6. GATE 3 — accept, edit or reject each machine suggestion.
regmap review --gate 3

# 7. Report.
regmap report --format md --out crosswalk.md
regmap report --format csv --approved-only --out signed-off.csv

# At any point:
regmap status
```

### Review keystrokes

Every gate steps through one item at a time, shows progress (`item 4/37`), and
saves after each decision. `[q]` quits and saves; re-running the same command
resumes exactly where you left off.

| Key | GATE 1 (segmentation) | GATE 2 (mappings) | GATE 3 (suggestions) |
|---|---|---|---|
| `a` | accept | accept | accept (promotes the suggestion) |
| `e` | edit id / title / category / text | edit controls / rationale / confidence | edit before accepting |
| `s` | skip (stays draft) | skip | skip |
| `r` | reject as noise | reject | reject |
| `m` | merge into another requirement | — | — |
| `p` | split into two at a line | — | — |
| `u` | — | mark UNMAPPED | — |
| `v` | view full text | view full text | view full text |
| `q` | quit and save | quit and save | quit and save |
| `?` | help | help | help |

Editing free text (`text`, `rationale`) opens `$EDITOR` when one is set, and
falls back to a single-line prompt otherwise.

**Ordering is deliberate.** GATE 1 shows low-confidence segments first — front
matter, short bodies, duplicate section numbers, page-number noise — because
those are the ones most likely to be wrong. GATE 2 shows UNMAPPED requirements
first, because those are the ones needing your judgement.

**`--auto-approve` is per-gate, explicit and off by default.** If stdin is not a
terminal the tool refuses to run a gate and tells you to review it manually,
rather than silently approving anything.

---

## What lands on disk

Everything lives under `--data-dir` (default `regmap/data`), and every file is
written atomically and sorted by requirement id so git diffs stay clean.

| File | Contents |
|---|---|
| `state.yaml` | Which framework is in flight, whether you confirmed it, and per-gate progress |
| `requirements.yaml` | Every segmented requirement with its status |
| `mappings.yaml` | Every crosswalk entry with its status, source and reviewer |

The directory is created `0750` and reports written with `--out` are `0600`:
both carry regulatory source text and reviewer identities.

### The control catalog

The mapping target is grc's own embedded NIST dataset — the same
`internal/data` JSON that `internal/controlcatalog` serves — so the crosswalk
and the web application map against identical controls.

Base controls only by default (323 of 1193 entries). `--with-enhancements`
includes enhancements such as `AC-2(1)`; they are off normally because they
would bloat the control-ID list sent to the model on every suggestion without
improving a regulation-level mapping. `--catalog <file>` overrides with a YAML
catalog of your own.

Each command is resumable and idempotent. `map` re-run never clobbers an
approved or rejected mapping; `ingest` refuses to overwrite reviewed work unless
you pass `--force`.

### The audit trail

Every approved item records `reviewedBy` and `reviewedAt`. Every mapping records
where it came from:

- `source: curated` + `seedGroup` — resolved from the profile's seed crosswalk
- `source: llm-suggested` + `model` — proposed by a model, and never written as
  anything but `draft`

Reports separate the human-approved crosswalk from draft and machine-suggested
rows, and `--approved-only` emits just the signed-off set.

---

## Adding a new framework

Write `regmap/profiles/<id>.yaml`. No Go code changes. The pipeline, the gates,
the state store and the report engine contain no per-framework logic.

```yaml
id: my-framework                 # required; used as --framework <id>
displayName: My Framework v1.0   # shown in reports
sourceRef: ISO/IEC 12345:2026    # named in every report header
idPrefix: MYF                    # requirement ids become MYF-<section>

# Optional. Used to guess the framework from the filename and the first
# ~20k characters of text. You always confirm the guess at GATE 1.
detect:
  filenamePatterns: [myframework, "12345"]
  contentPatterns: ["ISO/IEC 12345", "the Standard applies to"]

# Required. How to split the extracted text into requirements. Strategies
# compose — declare several and their boundaries merge in document order.
segmentation:
  - type: article        # "Article 5", "Art. 5"
    label: Article       # the heading word; defaults to "Article"
    minBodyChars: 120    # shorter bodies are flagged low-confidence

  - type: numbered       # hierarchical numbers: 3.4, 3.4.1
    minDepth: 2          # 2 => "3.4" is the shallowest boundary
    maxDepth: 3          # 3 => "3.4.1" is the deepest

  - type: annex          # "Annex I", "Annex 2"
    label: Annex
    idPrefix: MYF-ANNEX  # overrides the profile idPrefix for these items

  - type: regex          # anything else
    pattern: '(?m)^[ \t]*Section[ \t]+(\d+)'   # group 1 is the section key
    label: Section
    idPrefix: MYF-SEC

# Optional. Category/pillar labels for this framework. Anything that matches
# no rule is left "unclassified" — the tool never guesses silently.
classification:
  categories: [Governance, Incident Response]   # vocabulary; validates rules
  rules:
    - category: Governance
      match: { articles: "1-8" }
    - category: Incident Response
      match: { keywords: [incident, notification] }

# Optional. Known requirement-group -> control mappings. Seed hits are written
# with confidence=high and status=draft; GATE 2 is where you validate them.
seedCrosswalk:
  - group: Governance duties (Arts 1-8)
    match: { articles: "1-8" }
    controls: [PM-9, RA-3]
    rationale: Why these controls satisfy this group of requirements.
```

### The `match` selector

One selector type, shared by classification rules and seed crosswalk entries.
Fields are OR-ed — any hit matches. Rules are evaluated in declaration order and
the first match wins, so ordering is deterministic.

| Field | Matches | Example |
|---|---|---|
| `articles` | the section key's leading number, as values and inclusive ranges | `"5-16"`, `"45"`, `"5,7,9"`, `"5-16,45"` |
| `sections` | section keys by prefix | `["3."]` matches `3.4.1` but not `31.4` |
| `annexes` | annex keys exactly, case-insensitively | `["I", "II"]` |
| `keywords` | title + body text, case-insensitively | `["threat intelligence"]` |

### Checklist for a new profile

1. Write the YAML. `regmap frameworks` validates it on load — unknown
   strategies, regexes without a capture group, seed entries with no controls or
   no selector, and classification rules using an undeclared category are all
   rejected with a specific message.
2. Run `ingest` against a real document and read the segmentation summary.
3. Walk GATE 1. If you find yourself rejecting the same *kind* of noise
   repeatedly, that is a signal to tighten the profile rather than keep fixing
   it by hand.
4. Add seed crosswalk entries for the requirement groups you already know. It is
   fine to ship a profile with none — everything simply starts UNMAPPED, and
   `suggest` plus GATE 3 fills the gaps under review.

---

## Machine suggestions

`suggest --with-llm` sends only requirements that are **approved at GATE 1** and
resolved to **UNMAPPED**. Each call injects the requirement text plus the full
list of valid 800-53 control IDs and requires a strict JSON object back
(`controlIDs`, `rationale`, `confidence`).

- The response is parsed defensively: markdown fences, leading prose, malformed
  JSON and implausible control counts are all handled without trusting the
  payload.
- Control IDs that are not in the catalog are **kept and flagged**, not silently
  dropped, so the reviewer can see what the model invented.
- Requirement text is wrapped in tags and the system prompt tells the model to
  treat it as untrusted data, never as instructions.
- Results are written `source: llm-suggested`, `status: draft`. Nothing is
  auto-approved. A failed run saves what it gathered and is resumable.

`ANTHROPIC_API_KEY` comes from the environment and is never hardcoded. The
model defaults to `claude-sonnet-4-6` and is overridable with `--model`.

---

## Development

```sh
go build ./...
go vet ./...
go test ./...
```

The test suite covers each segmentation strategy against deliberately messy
sample text, the mapping resolver (including idempotency and the
never-clobber-a-human-decision rule), the profile matcher and validator, and the
LLM response parser.

`internal/regmap/cli/approval_guard_test.go` is the one that matters most: it walks the source tree
with the Go AST and fails if any package outside `internal/regmap/review` writes
`StatusApproved`. Reading the constant is fine — reports have to compare against
it — but no code path may set it. If that test ever fails, the human-in-the-loop
guarantee has been broken. A companion test checks the guard itself catches a
planted violation, so a passing run means something.

### Layout

```
cmd/regmap                   entry point
internal/regmap/cli          CLI wiring (cobra); no framework-specific logic
internal/regmap/profile      profile loading, validation, matchers, detection
internal/regmap/ingest       PDF/DOCX/TXT extraction and the generic segmenter
internal/regmap/requirement  the Requirement type and the status vocabulary
internal/regmap/nist         the 800-53 catalog, over grc's embedded dataset
internal/regmap/mapping      crosswalk entries and the seed resolver
internal/regmap/review       the three interactive gates (the only approver)
internal/regmap/suggest      the Anthropic client and response parsing
internal/regmap/report       Markdown / JSON / CSV rendering
internal/regmap/state        state.yaml, requirements.yaml, mappings.yaml
regmap/profiles/             one YAML file per supported framework
regmap/data/                 pipeline output (gitignored)
regmap/testdata/             sample regulation excerpts for the walkthrough
```
