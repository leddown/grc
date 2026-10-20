# Letters

Formal correspondence written on a client's behalf, first of all letters from
a significant institution to its ECB Joint Supervisory Team (JST). A letter is
a document of type `letter` in the policy library: it is written in the Policy
Studio with everything a policy has there (co-editing, suggestions, comments,
AI proposals, client facts, approval with a hashed snapshot, compare and
redline), and typeset as a letter rather than as a controlled document.

Code: `internal/policydocs` (the type, its lint and its approval record),
`internal/policystudio/templates/letter-*.json` (the two built-in templates),
`internal/policyai` (the letter rules for AI proposals),
`internal/doctemplate/letter.go` and `templates/typst/letter.typ` (the Word
and PDF output).

---

## Writing a letter

1. **New document** on the Policies page, from one of the built-in templates,
   for the client the letter is sent for:
   - *Letter to the ECB Joint Supervisory Team*: any letter to a JST, with an
     annex that lists every commitment with its accountable function, due date
     and the evidence the JST will receive.
   - *Action plan addressing AI-enabled cybersecurity threats: response to ECB
     letter SSM-2026-0301*: the covering letter for the action plan the ECB
     asked significant institutions to send their JST by **31 October 2026**,
     with an annex that follows the six focus areas of that letter's Annex 1.
2. **Client facts** fill what belongs to the client: its legal name and
   address, the JST Coordinator and how to address them, two signatories and
   their functions, the contact for the JST, the national authority for the
   copy. Set them once on the client profile; every letter for that client uses
   them, and a missing one blocks approval.
3. **Document control.** The title is the **subject line**: name what the
   letter answers, with its reference. The reference is *our reference*. The
   **effective date is the letter's date** (the Studio's panel still labels it
   *Effective date*; the policy page says *Letter date*). Classification
   defaults to Confidential. A letter has no frameworks and no review cycle.
4. **Write it.** What only a person can supply for this letter (the date of
   the supervisory letter being answered, what the board decided, a due date)
   is a `[TO DO: …]` placeholder in the template. Each one is highlighted in a
   draft PDF and blocks approval until it is replaced.
5. **Review and approve** like any document. Approval needs the letter's date,
   an owner role and no blocking finding, and records the approved text with
   its SHA-256.
6. **Render PDF** for the letter as it will be sent, or **Word** to finish it
   on the institution's letterhead. Sign and send it outside the application.

### The parts of a letter

A letter's sections are its parts, by kind. Only an annex prints its heading;
the other headings are for finding your way in the Studio.

| Kind | Printed as | Required |
|---|---|---|
| Letter: Sender | Lines under the client's name at the head of the first page (the address). Delete it for a letter printed on the institution's own letterhead | |
| Letter: Addressee | The address block, one line per line | yes |
| Letter: Body | Everything from the salutation to the closing formula. `###` sub-headings are allowed | yes |
| Letter: Signatories | A signature line per signatory: a table of name and function (a header row is skipped), or one paragraph each | yes |
| Letter: Enclosures | Listed under *Enclosures* | |
| Letter: Copy To | Listed under *Copy to* | |
| Letter: Annex | Each from a new page, under its own heading | |

A section of any other kind is printed as part of the body, under its
heading, so nothing written in a letter is left off the page. The closing
formula (the body's last paragraph) is kept on the page with the signatures.

### What lint checks in a letter

- the addressee, body and signatories are there (blocks approval);
- no client fact is unresolved and no `[TO DO]` is left (blocks approval; the
  `[TO DO]` rule applies to every document type);
- no suggestion is still open (blocks approval);
- the letter has a date (advisory here; approval refuses an undated letter);
- no commitment is hedged: *endeavour to*, *strive to*, *where possible*,
  *where practical*, *as appropriate*, *best effort*, *in due course*, *as soon
  as possible* (advisory). A letter says *will*: the policy rules on
  must/shall/should/may do not apply to it.

A letter makes no coverage claims. A control cannot be mapped to it, the
coverage matrix leaves out any mapping a re-typed document still carries (and
lint says so), and the *Suggest control mappings* AI action is refused. Cite a
control in the text with a control chip instead.

### AI on a letter

The AI is told it is writing a letter, not a policy (`letterRules` in
`internal/policyai/prompt.go`): formal and factual register, commitments with
what, who and when, what was done kept apart from what will be done, the same
hedges forbidden, and **nothing invented**. Names, figures, dates, deadlines,
references of supervisory letters and findings, and what a body decided come
from client facts or stay a `[TO DO: …]` for a person to fill. The inline
actions read the letter's way: *Make testable* makes each commitment specific,
*Review section* reads the text as the supervisor will, and *Draft this
section* drafts the part of the letter the section is. Every edit is still a
suggestion a person accepts or rejects, and each proposal and decision is
recorded.

Supervisory correspondence is confidential. Set the document's **AI** setting
to *local only* unless the institution has decided that the text may go to a
cloud model. A letter is also readable through the knowledge API
(`/api/knowledge`, kind `policy`) by the Wintermute agent that holds its
token, as every document in the policy library is. The proposal record is the
answer to "how was AI used in preparing this letter".

### PDF or Word

| | PDF (`letter-typst`) | Word (`letter-docx`) |
|---|---|---|
| Layout | Finished: client's name and address, address block, reference and long-form date, subject, body, signature lines, enclosures, copies, annexes on new pages, page *n of m* | Text and structure in Pandoc's default styles |
| Marking | Classification on every page; **DRAFT — NOT APPROVED** on every page until approved; *Date not set* in red | The same, as the first line |
| Archival | PDF/A-2b by default, PDF/UA-1 on request | — |
| Use it for | The letter as approved, for the file and for sending as PDF | Setting the letter on the institution's Word letterhead template, footnotes, a last legal review with tracked changes |

Both are rendered from the same payload (`GET /policies/:id/export.json`, the
contract in `DOCUMENT_TEMPLATES.md`). The policy page's and the Studio's
**Render PDF** and **Word** links name the policy templates; a letter is given
the letter template for the same engine.

**What stays outside the application, deliberately:** the institution's
letterhead artwork (it belongs to the institution, and the app accepts no
uploads), signing (wet or a qualified electronic signature, under the
institution's signing authorities), and submission (the SSM Portal or e-mail,
as the JST asked). The application's part ends with the approved text and its
hash.

### Known limits

- The editor has no footnotes. Put the reference in the text or an annex, or
  add the footnote in the Word file.
- A fact token carries no bold or italic. The letterhead prints the client's
  name in bold for that reason; style a fact in Word if it matters.
- A long annex table does not repeat its header row on the next page.
- The Studio's document-control panel labels the letter's date *Effective
  date* and offers a review cadence, which a letter ignores. Changing that
  means rebuilding the editor bundle.

---

## What a letter to a JST is: the research behind the templates

Checked on 2026-10-20 against the sources at the end.

- **Language.** A supervised entity may write to the ECB in any official EU
  language, and the ECB answers in the same language (Article 24 of the SSM
  Framework Regulation, implementing Council Regulation No 1/1958). An entity
  may agree with the ECB to use one language only; in 2017 about 70% of
  significant institutions had agreed to English, and the agreement can be
  revoked. The templates are in English: write in the language the institution
  agreed with the ECB.
- **Channel.** The SSM Portal (formerly the IMAS Portal) is where supervised
  entities submit information on supervisory processes, follow their status
  and exchange information with supervisors; submissions reach the ECB and
  the national authority together. Whether a letter goes through the portal
  or by e-mail to the JST is what the JST asked for. No public ECB source sets
  a signature format for such a letter; follow the institution's signing
  authorities.
- **Form.** The ECB's own letters to banks are laid out as sender, addressee,
  reference, place and date, subject, salutation, body, *Yours sincerely*,
  signatory, *Encl.* and annexes, with footnotes for sources. The letter
  template follows that order. The JST Coordinator heads the institution's
  JST; the ECB's postal address is Sonnemannstrasse 20, 60314 Frankfurt am
  Main.
- **Content.** The ECB expects remediation to address the root cause with a
  final deadline and interim deadlines, assesses whether solutions are durable
  rather than whether findings can be closed, and escalates (up to business
  restrictions and periodic penalty payments) when material weaknesses are not
  remediated in time, taking the bank's responsiveness into account. Hence the
  rule that every commitment in a letter has an owner and a date, and the
  annex where the JST can track them.
- **Letter SSM-2026-0301** (7 July 2026, from the Chair of the Supervisory
  Board to the CEOs of significant institutions): assess the effect of
  AI-enabled cyber threats without delay; send the JST, by 31 October 2026, an
  action plan with concrete measures, resources, roles and responsibilities
  and timelines, building on the bank's cyber-risk strategy; address open ICT
  and security findings from on-site inspections, targeted reviews and the
  2024 cyber resilience stress test without delay. Short-term focus:
  vulnerability and patch management at scale; monitoring, detection and
  AI-enabled defence; third-party risk management, with perimeter and
  internet-facing assets first. Structural: defence in depth, cyber hygiene
  and replacing legacy technology; response, recovery, crisis management and
  information sharing. AI-based tools are acceptable with a prior assessment
  of benefits and risks, safeguards and human oversight. The JST will discuss
  and monitor each plan, and the ECB will analyse all plans horizontally. The
  IT Risk Questionnaire moved from September 2026 to February 2027.
- **AI in drafting.** No ECB rule on using AI to draft supervisory
  correspondence was found. The ECB's 2026–2028 priorities include how banks
  govern generative AI, and its supervisors have said they will ask banks to
  show they govern AI as a core risk topic. The design here (the AI proposes,
  a person decides, every proposal and decision recorded, local-only routing
  per document, nothing invented) is meant to be shown, not explained away.

### Sources

- ECB, letter SSM-2026-0301, *Addressing AI-enabled cybersecurity threats*, 7 July 2026:
  <https://www.bankingsupervision.europa.eu/press/letterstobanks/shared/pdf/2026/ssm.2026_letter_on_AI_enabled_cybersecurity_threats.en.pdf>
- ECB, *Effective supervision through timely remediation*, speech, 6 October 2026:
  <https://www.ecb.europa.eu/press/key/date/2026/html/ecb.sp261006_1~0df91e4e44.en.html>
- ECB Banking Supervision, SSM Portal:
  <https://www.bankingsupervision.europa.eu/framework/portal/ssm/html/index.en.html>
- ECB, letter to M. Ferber MEP on the SSM language regime, 9 January 2017:
  <https://www.bankingsupervision.europa.eu/ecb/pub/pdf/170109letter_ferber.en.pdf>
- ECB Banking Supervision, *Supervisory Manual*, 2024:
  <https://www.bankingsupervision.europa.eu/ecb/pub/pdf/ssm.supervisory_guides202401_manual.en.pdf>
- ECB Banking Supervision blog, 22 January 2026, on AI in banking supervision:
  <https://www.bankingsupervision.europa.eu/press/blog/2026/html/ssm.blog20260122~7fdd534c3c.en.html>
- Paperjam, *AI in banking is a core risk, warns ECB's Machado*, February 2026:
  <https://en.paperjam.lu/article/ai-in-banking-is-a-core-risk-warns-ecbs-machado>
