# Asking this installation questions

Ask the AI dock *"how many of the Security NFRs are focused on network
segmentation?"* and, until this existed, you got a polite essay about what the
model would need in order to answer, and an offer to go through the list if you
pasted it in. It could not know the list was one query away.

This document is how the list gets to it: a read-only **knowledge API** over
this installation's own data, and an **agent** on your Wintermute server that
uses it.

## Where the agent lives

Not here. `wintermuted` already owns an agent loop, a tool registry and a
transcript store; a second one in this application would mean two of each and
two places to look when a model does something surprising. So this application
publishes its data and routes its questions through that server.

```
  GRC (this app)                        wintermuted
 ┌───────────────────────┐            ┌──────────────────────────┐
 │ AI dock / AI Chat     │──question─▶│ agent loop               │
 │                       │◀──answer───│  ├─ grc_search ──────────┼──┐
 │ /api/knowledge/*      │◀───────────┼──┘  grc_list_nfrs, …     │  │
 │  (read-only, tokened) │            │  ├─ search_documents     │  │
 └───────────────────────┘            │  └─ web_search           │  │
                                      └──────────────────────────┘  │
                                        the agent's own library ◀────┘
```

The consequence to be deliberate about: **grc's AI must be pointed at
Wintermute** for any of this to apply. Wintermute can forward the turn to
Claude, so you keep whichever model you want and gain the tools. Pointed
straight at Claude, grc's AI has no access to this data and will answer as it
did before.

## Setting it up

1. **Give this installation a knowledge token.** Set `KNOWLEDGE_TOKEN` (or
   `-knowledge-token`). Without one the API is registered only in local mode —
   refusing to serve it otherwise is deliberate, because it reads the whole
   catalog, the policy library and the risk register, and "we will set the
   token later" is how that ends up on a network.
2. **Tell wintermuted about this installation.** On that server, set `GRC_URL`
   to this application's base URL and `GRC_KNOWLEDGE_TOKEN` to the same token.
3. **Create an agent there** with the `grc` source (its Agents view, see that
   repository's `docs/agents.md`).
4. **Point this application at it.** Settings → AI providers → Wintermute:
   server URL, client token, then pick the agent from the list. The list is
   fetched from the server, so a mistyped id cannot silently produce a
   confident, ungrounded answer. The backend and model dropdowns beside it are
   fetched the same way; leaving both on their default hands the routing back
   to the server.
5. **Optionally, give Crisis Exercises an agent of its own.** Create a second
   agent the same way, with the `grc` source and the exercise material in its
   library, and pick it as the **Crisis Exercise agent** below the first. That
   module's questions, its Agent conversation and Ask AI on its pages then go
   to it. If that agent cannot reach this server, tick **Send the open exercise
   with each question**. See [CRISIS_EXERCISE.md](CRISIS_EXERCISE.md).

The Settings page and the AI Chat page then link to that agent's page on
Wintermute, which is where documents are uploaded — that server owns the
library, the extraction and the search over it.

## Documents go one way: to the library

This application accepts no document uploads and reads no PDFs. It used to do
both, badly: its own PDF reader, its own DOCX reader, no OCR, its own size
limit, and its own copy of every file. All of that is on the Wintermute server
already, and better — a text layer where there is one, `ocrmypdf` and Tesseract
for a scan, LibreOffice for the office formats, and a fleet node to run them on
if the server is small.

So a regulation or a security document is uploaded **there**, to one agent's
library, and the two modules that work on documents read the extracted text
back:

```
GET /api/v1/agents/{id}/documents            what the library holds
GET /api/v1/agents/{id}/documents/{n}/text   one document's passages, paged
```

`internal/aiprovider/library.go` is the client, using the same server URL,
client token and agent already configured in Settings. **Regulation Coverage**
imports a regulation and segments it into articles; **NFR Enrichment** imports a
security document and proposes catalog entries from its passages. Both copy the
passages in, so a report and its citations survive that server being
unreachable — but the document itself has one home, and re-reading it with a
better tool happens there and is re-imported here.

The practical consequence: a document that has not finished being read on that
server cannot be imported, and both pickers say so rather than importing a
fraction of it. A document that server could not read at all cannot be imported
either, which is the honest outcome — it was never readable here.

## The API

Five endpoints under `/api/knowledge`, all GET, all read-only. There is no
write path here to secure, forget to secure, or be talked into using.

| Endpoint | Answers |
|---|---|
| `GET /overview` | How much of each kind exists, the NFR domains and control families with counts, the regulations analysed |
| `GET /index/:kind` | An entire small catalog, compactly — the only honest way to answer "how many" |
| `GET /search?kind=&q=&limit=` | Lexical search within one kind, with the counts below |
| `GET /item?kind=&ref=` | One full record |
| `GET /export` | Everything, in one document — see below |

Kinds: `nfr`, `control`, `regulation_clause`, `regulation`, `policy_clause`,
`policy`, `risk`, `exercise`, `exercise_finding`. Exceptions are absent because
that page is a saved view over the NFR catalog rather than a record set of its
own.

Authenticate with `X-Knowledge-Token` or `Authorization: Bearer`.

## Handing the agent the whole thing

The four query endpoints answer a question at the moment it is asked, which
needs this application to be reachable from wherever the agent runs. That is the
better arrangement when it holds. When it does not — the agent is on a network
that cannot reach this server, or you would rather it read the data than call
for it — `GET /api/knowledge/export` returns the same data as one file:

```json
{
  "format_version": 1,
  "generated_at": "2026-09-08T09:12:44Z",
  "note": "Full export of one GRC installation's compliance data, taken at ...",
  "overview": { "counts": { "nfr": 104, "control": 1189, ... } },
  "corpora": [
    { "kind": "nfr", "count": 104, "items": [ { "kind": "nfr", "ref": "56",
      "title": "...", "summary": "...", "body": "...", "group": "Data Security",
      "related": ["SC-7"], "fields": {...}, "url": "/security-nfrs?key=56" } ] },
    { "kind": "control", ... }
  ]
}
```

Every record of every kind, with its **full text** rather than the snippet a
search result carries — the same `Item` shape the query endpoints return, so an
agent that already knows one knows the other. Upload it into the agent's library
on Wintermute and it can answer about this installation without a round trip.

An admin can download the same file from **Utilities → Export for the AI
agent** (`/utilities/knowledge-export`), which is behind the session rather than
the knowledge token — for the case where a person, not a process, is doing the
uploading.

The thing to be deliberate about is staleness. A bundle is a point in time, and
an uploaded one goes on answering confidently after the catalog moves under it.
So `generated_at` is stamped on the document and repeated in `note`, where a
model will read it: an answer from an uploaded bundle should say how old the
bundle is. An agent that *can* reach this server should fetch `/export` on a
schedule, or use the four query endpoints and skip the problem entirely.

### Counting honestly

Search returns two numbers:

- `total_matches` — records matching **any** term.
- `total_all_terms` — records matching **every** term.

For "network segmentation" against the live catalog those are 26 and 3. Quoting
the first as though it were the second is how a precise question becomes an
inflated answer, so both are returned, along with the terms they refer to and,
per record, which terms it actually matched. The tool prompt tells the model to
say which one it is quoting.

Neither number is authoritative. The search is lexical and does not stem, so a
requirement saying "segmented" does not match "segmentation" — the response
says so, and the fix is to search the other forms. For the NFR catalog, which
runs to about a hundred entries, the reliable answer to a counting question is
`index/nfr`: read all of it and count.

## What it exposes

Everything below is readable by anything holding the token, which is why the
token is read-only and separate from `ADMIN_TOKEN`.

- **Security NFRs** — key, summary, domain, description, NIST mapping, and the
  controls that mapping resolved to.
- **800-53 controls** — id, name, family, requirements, discussion, baselines.
- **Regulation coverage** — each analysed article with what it requires, what
  it mapped to, the gap, and the commentary (see
  [REGULATION_COVERAGE.md](REGULATION_COVERAGE.md)).
- **Policies** — documents and their sections, with the controls each section
  claims to satisfy.
- **Risk register** — open risks with scores, owners and current controls.

Corpora are cached for 45 seconds, so an agent working through a question does
not re-read every table per tool call.

## Checking it works

```sh
curl -H "X-Knowledge-Token: $KNOWLEDGE_TOKEN" localhost:8080/api/knowledge/overview
curl -H "X-Knowledge-Token: $KNOWLEDGE_TOKEN" \
  'localhost:8080/api/knowledge/search?kind=nfr&q=network+segmentation'
curl -H "X-Knowledge-Token: $KNOWLEDGE_TOKEN" \
  -o grc-knowledge.json localhost:8080/api/knowledge/export
```

On the Wintermute side, **Admin → Backends → Send a test question** confirms a
backend answers at all, and asking the agent a catalog question confirms the
tools reach this application.
