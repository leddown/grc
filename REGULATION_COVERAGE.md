# Regulation Coverage

Upload an EU regulation, have every article analysed against this
installation's **Security NFR catalog** and **NIST SP 800-53**, and get a
versioned report you can download as a PDF, interrogate in conversation, and
correct.

It lives at `/regulation-coverage`, in the Compliance & Risk group beside
Policy Coverage. The module is `internal/regcoverage`.

## What it is for

Policy Coverage answers "what do our policies claim to satisfy?". This answers
the question from the other direction: **"a regulation landed on my desk —
what does it require, what do we already have, and where are we exposed?"**

For each article it records:

- whether the article imposes a security or resilience obligation at all
  (definitions, scope, supervisory machinery and entry-into-force provisions do
  not, and saying so is a useful answer);
- what it requires, in plain terms;
- which Security NFRs and 800-53 controls satisfy it, each with a rationale and
  a confidence;
- what the catalog does **not** cover for it;
- practitioner commentary — what good looks like, what evidence an auditor
  asks for, where implementations usually fall short.

## The loop

```
upload PDF  →  extract + segment  →  analyse  →  report v1
                                                    ↓
                          chat about it  ←→  revise a section  →  v2, v3 …
```

1. **Upload** (`POST /regulation-coverage/regulations`, multipart). Extracts
   the text, identifies the framework, segments into articles and annexes.
   Nothing is sent to a model at this stage, so a document that segmented badly
   can be deleted before any spend. The original file is kept and served back
   unmodified at `/regulation-coverage/:id/source`.
2. **Analyse** (`POST /regulation-coverage/:id/analyze`). One model call per
   section, plus one for the executive summary. Writes version 1.
3. **Read** the report at `/regulation-coverage/:id`, download it at
   `/regulation-coverage/:id/report.pdf`, or take the JSON at
   `/regulation-coverage/:id/report.json`.
4. **Ask** (`POST /regulation-coverage/:id/chat`) — a conversation grounded in
   the report, carrying its own history.
5. **Revise** (`POST /regulation-coverage/:id/revise`) — apply a correction to
   one section. This re-analyses that section with your instruction and writes
   the **next version**. Earlier versions keep their snapshot and stay
   downloadable exactly as they were.

Reading is open to any signed-in user; upload, analysis, chat and revision sit
behind the admin gate, because the last three spend model calls.

## How a mapping is produced

**Retrieval first, then judgement.** For each section, a BM25 ranker (the same
scorer `internal/nfrenrich` uses) shortlists ten candidate controls and ten
candidate NFRs from the ~1200-item catalog. Only that shortlist goes into the
prompt. The model judges the shortlist; it never browses the catalog, and it is
told not to map to anything that is not on it.

**Curated priors where they exist.** When the upload is recognised as a
framework that has a regmap profile (DORA, NIS2, CRA, PCI-DSS), that profile's
reviewed seed crosswalk is supplied as a strong prior for the section, and the
model is asked to confirm each entry against the text. A mapping the crosswalk
proposed and the model confirmed is marked `curated` in the report.

**Three checks on the output**, because none of this is trustworthy by
construction:

| Check | What it catches | Where it shows |
|---|---|---|
| Catalog lookup | An invented control ID or NFR key | `unknown` tag on the row |
| Grounding | A confident analysis of text that is not in the section — every finding must quote the section verbatim | `unverified quote` tag |
| Provenance | Which model produced it, and the SHA-256 of the prompt it answered | Under each section |

Whitespace, case and punctuation are normalised before the grounding check, so
a re-wrapped quote still passes; an invented one does not.

## Framework profiles

Segmentation comes from the `regmap` framework profiles in `regmap/profiles/`,
embedded into the binary (`regmap/profiles/embed.go`) so no directory has to
ship beside it. The CLI still reads them from disk via `--profiles-dir`; they
are the same files.

Detection runs against the filename and the first 20k characters. Anything not
recognised falls back to **`eu-generic`** — article and annex segmentation with
no classification rules and no crosswalk, which is what EU legislative drafting
gives you for free. The report says which happened: an upload segmented by the
generic profile is labelled *"generic segmentation (no framework profile
matched this document)"*, because it means no curated knowledge went into it.

To add a framework, add a YAML profile — see `REGMAP.md`. No code changes.

## Extraction

`pdftotext -layout` when poppler-utils is installed, falling back to a pure-Go
reader. Which one ran is recorded on the regulation and shown in the report
footnote — it is the first thing to check when segmentation looks wrong. A
scanned PDF with no text layer is refused rather than analysed as an empty
document; it needs OCR first.

Sections with almost no body are skipped rather than analysed. These are
usually artifacts: an inline cross-reference ("…designated pursuant to Article
20") reads as a heading to the segmenter. They appear in the report as
unanalysed instead of costing a model call each.

Supported: `.pdf`, `.docx`, `.txt`, `.md`, up to 25 MiB.

## Cost

One model call per section, one for the summary, one per question, one per
revision. A hundred-article regulation is about a hundred calls — the analysis
button confirms before it starts, and the run continues past a section that
fails rather than losing the other ninety-nine.

The provider is whatever Settings selects (Claude, or a Wintermute server
routing to a self-hosted model), through the same `aiprovider` router the rest
of the app uses. Spend lands in the same `ai_usage_log`. Section analysis and
retrieval are exactly the bulk work worth pointing at a local model; keep the
frontier model for the summary if you are watching cost.

## Storage

Six tables, all prefixed `reg_coverage_`: `regulations`, `sources` (the
original upload's bytes, kept out of the regulation row so listings do not drag
a multi-megabyte blob), `sections`, `findings` (one row per section per
revision), `mappings`, `versions` (an immutable JSON snapshot per version) and
`chat`.

Deleting a regulation deletes all of it, including the stored original.

## Limits worth knowing

- **The analysis is synchronous.** A long regulation holds the request for
  minutes; the handler bounds it at 45 minutes and cancels the run if the
  reader navigates away.
- **A revision targets one section.** There is no "re-run everything with this
  instruction" — re-analysing is the button for that.
- **The chat sees a condensed report**, capped at 60 security-relevant
  sections, not the full regulation text. Ask about a section by its reference
  to get at it specifically.
- **Nothing here is a compliance determination.** It is analysis to be
  reviewed, and the report says so in its footnote.
