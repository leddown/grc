# Risk & Crisis Exercises

A bank's exercise programme usually rehearses its parts separately and fails at
the joins. The SOC handles the intrusion beautifully and nobody says the word
"crisis". The crisis team runs for six hours before anyone starts the four-hour
regulatory clock. The board is briefed on a version of events three hours out of
date. Every part passed; the institution failed.

This module holds the whole arc as one artefact — red team, detection, incident,
classification, crisis, continuity, communications, board, authorities, recovery,
after-action — so that the seams are the thing being tested. It is built for a
financial entity in the Baltic states, which is an unusually demanding place to
run one: three small, highly digitised markets, sustained hybrid and hacktivist
pressure on the infrastructure a bank depends on, and a supervisory perimeter
where a national authority, the ECB, two CSIRTs and a data protection authority
can all have a claim on the same incident inside six hours.

Page: **/crisis-exercises**.

---

## What it does that a document does not

**It models the regulatory clock instead of narrating it.** A team fills in the
DORA materiality criteria and the module applies the combination rule from
Commission Delegated Regulation (EU) 2024/1772 — critical services affected,
*and* either the data-losses criterion or two or more of the rest. It then
derives every notification obligation the scenario started, on the right trigger
for each: the DORA initial notification at the earlier of four hours from
classification and twenty-four from awareness, the intermediate at seventy-two
hours, the final at one month; the national NIS2 early warning and notification
from awareness; the GDPR Article 33 clock from awareness of the *breach*, which
is a different and usually later moment; the ECB channel for a significant
institution; the scheme and customer obligations that are shorter than all of
them and are never in the playbook.

The team records when each notification actually went. The report then says
which were met and by how much the rest were missed. That is the artefact a
supervisor asks for, and no amount of discussion produces it.

**It links every part of the exercise to what that part tests.** An objective, a
phase, an inject, a decision, a notification clock and a finding can each cite
NIST 800-53 controls, this installation's Security NFRs, regulation clauses from
Regulation Coverage, policy clauses, risk register entries, and a seeded catalog
of frameworks and supervisory instruments. The **Coverage** tab inverts that:
which controls this exercise touched, what cited them, and which of them a
finding was raised against. "We tested incident response" becomes "we tested
IR-4, IR-6, NFR-INC-02 and DORA Article 17, and two of them failed."

**It separates the three documents an exercise actually needs.** The
after-action report as a PDF, for circulation. The controller's MSEL as CSV,
carrying the expected actions and evaluation notes, marked with the exercise's
TLP in the first column of every row. And the player handout, which is the same
exercise with the answers stripped out — because handing players the document
that says what they are supposed to do turns an assessment into a rehearsal, and
it has happened.

---

## The arc

Twelve phases, seeded on creation, each with a purpose, entry and exit criteria,
a lead role, a facilitator note and its own citations.

| Phase | What it tests |
|---|---|
| Threat intelligence | That the scenario is what this entity would actually face |
| Red team / adversary action | The attack path, played or narrated |
| Detection, triage and escalation | Whether noticing turns into telling someone |
| Security incident response | Scope, containment, evidence — and what each option costs the business |
| Classification and the regulatory clock | Classifying on partial information, on the record, and starting the clocks |
| Crisis declaration and CMT activation | Who can declare it, what changes when they do, and whether the team can be assembled |
| Business continuity and recovery | Degraded-mode capacity, and what breaks on day three |
| Crisis communications | What the institution will actually say, in every language its customers use |
| Board and management body | The reserved decisions, taken by people entitled to take them |
| Authority and law-enforcement interaction | The supervisor, the CSIRT, the DPA, the schemes, the ECB |
| Recovery and stand-down | Verified recovery rather than assumed, and a handover of what is still open |
| Hot debrief and after-action | Findings with severity, owners and dates, before the exercise becomes a story |

Drop phases at creation if you must. The ones people drop are reliably the ones
they are least ready for.

---

## The scenario library

Eight scenarios, each drawn from something that has happened to a European
financial institution or to Baltic critical infrastructure. Picking one fills in
the intelligence, the objectives and the citations; everything stays editable.

Each carries a **trap door** — the thing it is really designed to expose, shown
to the control team and withheld from players.

| Scenario | The trap door |
|---|---|
| Ransomware in the core banking platform | The backup catalogue was deleted before encryption, so the RTO everyone quotes is fiction |
| Sustained hacktivist DDoS against public channels | The breach claim is false, the bank cannot prove it quickly, and its instinct is to deny |
| Compromise at the outsourced core banking provider | Every step of the incident playbook instructs the bank to do something it has no ability to do |
| Hybrid attack — connectivity loss and an isolated data centre | Nobody can say whether it is an attack or an accident, and the honest public position is "we do not yet know" |
| Insider-enabled payment fraud mid-settlement | Every technical control worked; legal privilege, HR process, criminal investigation and the four-hour clock collide |
| Malicious update from a trusted vendor | Log retention is 30 days and the dwell time is 120 |
| TIBER-EU red team reaches the payment rail | Two alerts fired and were closed correctly by the process — a governance finding, not a tooling one |
| Disinformation and a digital deposit run | There is no incident, so the incident process never starts, so the crisis team is never convened |

---

## The Baltic specifics

Jurisdiction is not decoration here. It drives which authority is named on which
clock, and it drives the prompt the model writes injects from.

| | Lithuania | Latvia | Estonia |
|---|---|---|---|
| Competent authority | Lietuvos bankas | Latvijas Banka | Finantsinspektsioon |
| National CSIRT | NKSC / CERT-LT | CERT.LV | CERT-EE (RIA) |
| Data protection | VDAI | DVI | AKI |
| Deposit guarantee | Indėlių ir investicijų draudimas | Noguldījumu garantiju fonds | Tagatisfond |
| Statement languages | LT, EN | LV, EN, **RU** | ET, EN, **RU** |

There is a fifth option, **Baltic cross-border group**, which is the case worth
exercising and the one that does not work by itself: one core-banking outage
becomes three national notifications on three triggers, a lead-authority
determination nobody made in advance, and a consolidated message that has to be
true in three markets at once.

A statement released only in the state language will be translated by someone
else, with their framing. The module says so, and the customer-communication
clock records which languages the statement existed in when it went out.

---

## The AI: generation and the expert seat

Everything works without a model. Design an exercise by hand, run it, record it,
report on it — an exercise programme that stops when the model is unavailable is
not a programme. What the model adds is two things.

### Generating the MSEL

One model call per phase, never one call for the whole exercise, because a
single answer covering twelve phases degrades badly in the last four. Each call
gets the institution, the jurisdiction and its authorities, the scenario, the
objectives, the phase's own purpose and criteria, and a **shortlist of candidate
references** retrieved lexically from this installation's catalogs. The model
judges the shortlist; it does not browse the catalog. That keeps the prompt small
enough to reason over and every citation traceable to why the candidate was
offered.

Every generated inject records the model, the hash of the prompt it answered and
its own confidence. Every generated citation is resolved against the catalog
before it is stored, so a model that invents `XX-99` produces a reference marked
**unresolved** rather than one sitting in a client's report looking like the real
ones.

Regeneration replaces the model's previous work and never touches a
hand-written inject. A designer who edited something made a decision.

### The expert seat

Twelve personas, each a system prompt written in the voice of someone who has
sat in the seat you are trying to exercise:

`facilitator` · `board_trainer` · `ciso` · `supervisor` · `comms` · `legal` ·
`red_team` · `bcm` · `journalist` · `threat_intel` · `evaluator` · `adversary`

Two ways to use them. **Advisers** is a conversation about the exercise —
ask the board trainer what the chair will actually ask at T+4h, ask the
supervisory examiner whether a draft initial notification is adequate. **Hot
seat**, on any inject, hands the persona that inject and what the team actually
did with it and lets them push back in character. It is the single most useful
thing here during delivery: a team that has just written a holding statement
learns more from thirty seconds of a journalist's follow-up than from an hour on
communications principles.

The evaluator persona also drafts after-action findings from the record — the
observations, the decisions, the clocks. Every draft is marked as the model's
proposal for a human to accept, edit or delete. A finding is an accusation about
an organisation, and nobody should be able to say it was the software's.

Every persona prompt carries standing constraints: say when you do not know,
never invent a control identifier or a threshold or a deadline, distinguish what
the regulation requires from what good practice suggests, and produce narrative
rather than attack capability.

### Pointed at Wintermute

Routed through a wintermuted agent with the `grc` source (see
[AI_AGENT.md](AI_AGENT.md)), these personas answer from this installation's own
catalogs rather than from general knowledge — and the knowledge API now exposes
the exercises themselves. Two new kinds, `exercise` and `exercise_finding`, let
an agent answer questions no other corpus can:

- *Have we ever exercised our major-incident classification, and how did it go?*
- *Which controls have we actually tested, as opposed to documented?*
- *What have our exercises found about escalation?*

The control catalog describes an intention. The exercise record describes an
outcome. Without the second, an agent asked the first question answers from the
first corpus and sounds confident.

---

## Running one

1. **Create.** Pick a scenario or write your own; set the entity, jurisdiction
   and supervision. The arc, the objectives and the citations are seeded. Nothing
   has been sent to a model.
2. **Design.** Generate the MSEL phase by phase, or write injects by hand. Cite
   what each one tests. Add the roster.
3. **Brief.** Download the player handout and the controller MSEL. Cut a design
   version so what was circulated stays fixed.
4. **Run.** Move the status to *in progress*. Deliver injects on the clock and
   record what happened against each. Log decisions as they are taken — what
   else was considered, and who was entitled to decide. Use the hot seat when
   the room needs pressure.
5. **Classify.** In the classification phase, make the team fill in the
   materiality criteria on partial information. The clocks start. Record each
   notification as it goes.
6. **Debrief.** Draft findings from the record, edit them, assign owners and
   dates, rate the objectives, and cite what each finding relates to. Push the
   ones that represent standing risk into the risk register.
7. **Issue.** Cut an after-action version and download the PDF. It is immutable;
   later edits append a new version rather than changing the circulated one.
8. **Next year.** Clone it. The design and the citations come across; the
   observations, the findings and the objective ratings do not. That is what
   makes a year-on-year comparison possible, and it is why ISACA argues for a
   campaign of progressively harder exercises rather than a fresh one each year.

---

## Where the thinking came from

- **DORA** (Regulation (EU) 2022/2554) Articles 5, 11–14, 17–20, 24–27, with
  the RTS on incident classification ((EU) 2024/1772) and on threat-led
  penetration testing ((EU) 2025/1190).
- **ECB / Eurosystem** — the TIBER-EU framework and its purple-teaming best
  practices; the Cyber Resilience Oversight Expectations; SSM cyber incident
  reporting.
- **EU crisis machinery** — the ESRB recommendation on a pan-European systemic
  cyber incident coordination framework, and the ESAs' EU-SCICF.
- **NIS2** (Directive (EU) 2022/2555) Article 23 and the Baltic transpositions;
  **GDPR** Articles 33 and 34.
- **ISACA** — *Cybersecurity Incident Response Exercise Guidance* and *Five key
  considerations for developing crisis management and incident response tabletop
  exercises*, which are where the stress injects, the ambiguous injects, the
  expected-versus-actual tracking and the campaign approach come from.
- **Standards** — ISO 22301, ISO 22361, ISO 22398, ISO/IEC 27035.
- **NIST** — SP 800-84 for the exercise ladder, SP 800-61r3 and CSF 2.0 for
  incident response, and ATT&CK for attack paths.
- **Sector** — FSB cyber incident response and recovery, CPMI-IOSCO cyber
  resilience for FMIs, the G7 fundamental elements, ENISA's finance threat
  landscape, and the Nordic-Baltic financial crisis simulation exercises.

Every one of these is in the seeded citation catalog and can be attached to any
part of an exercise. `GET /crisis-exercises/catalog` returns the whole
vocabulary.

---

## API

Read (behind page access):

```
GET  /crisis-exercises                     the index page
GET  /crisis-exercises/catalog             formats, audiences, jurisdictions,
                                           scenarios, phases, roles, personas,
                                           authorities
GET  /crisis-exercises/exercises           list
GET  /crisis-exercises/suggest?q=          candidate references for a phrase
GET  /crisis-exercises/:id                 the exercise page
GET  /crisis-exercises/:id/report.json     the full dossier (?version=N)
GET  /crisis-exercises/:id/report.pdf      the report
GET  /crisis-exercises/:id/msel.csv        the controller's copy — carries the answers
GET  /crisis-exercises/:id/handout.md      the player brief — does not
GET  /crisis-exercises/:id/coverage        what this exercise tested
GET  /crisis-exercises/:id/versions
GET  /crisis-exercises/:id/chat
```

Write and spend (admin):

```
POST   /crisis-exercises/exercises
PATCH  /crisis-exercises/:id
DELETE /crisis-exercises/:id
POST   /crisis-exercises/:id/clone
POST   /crisis-exercises/:id/objectives | /phases | /participants
POST   /crisis-exercises/:id/injects
DELETE /crisis-exercises/:id/injects/:injectID
POST   /crisis-exercises/:id/injects/:injectID/response
POST   /crisis-exercises/:id/injects/:injectID/probe      (AI — the hot seat)
POST   /crisis-exercises/:id/decisions
PUT    /crisis-exercises/:id/classification               (recomputes the clocks)
POST   /crisis-exercises/:id/clocks/:clockID              (record a notification)
POST   /crisis-exercises/:id/findings
POST   /crisis-exercises/:id/references
POST   /crisis-exercises/:id/design                       (AI — generate the MSEL)
POST   /crisis-exercises/:id/advise                       (AI — ask a persona)
POST   /crisis-exercises/:id/after-action                 (AI — draft findings)
POST   /crisis-exercises/:id/versions
```

The whole write surface is behind the admin gate, including recording what
happened during delivery. An exercise record is evidence a supervisor may read,
and "anyone with the page open could edit the observations" is not a property it
should have.

---

## Two cautions

**The ECB clock's due time is a placeholder.** It is aligned to the DORA initial
notification and says so in the record. The real expectation is set by your Joint
Supervisory Team. Confirm it and correct the entry before you run the exercise —
a rehearsed deadline that turns out to be wrong is worse than no deadline.

**The materiality thresholds are yours, not the module's.** It applies the
combination rule; it does not decide whether a criterion is met. The real
thresholds are numeric, sector-specific and revised, and hard-coding a number
would give an authoritative-looking answer that is wrong for some entities and
out of date for the rest. Assessing each criterion is the judgement the
classification phase exists to rehearse.
