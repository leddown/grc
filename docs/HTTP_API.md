# HTTP routes

Every route the application registers, with who can call it. The table is taken
from the running router, not written by hand: a test instance was built with
`buildRouter` and each route was called without a session and with the session
of a non-admin user. 278 routes on 2026-10-10.

Read [SECURITY.md](SECURITY.md) with this file: it explains the gates the
Access column names, and lists the routes whose access is wider than their
neighbours'.

## Access levels

| Access | Meaning |
|---|---|
| public | Answers with no session. |
| signed in | Needs a session cookie (`grc_auth_session`). A non-admin user whose account lists `allowed_pages` also needs a grant covering the path; a user with no list reaches every signed-in route. |
| admin | Needs an admin session, or the break-glass `ADMIN_TOKEN` as `Authorization: Bearer` or `X-Admin-Token`. A browser request without one is redirected to `/login` (no session) or shown "Admin Access Required" (non-admin session); an API request gets 401 or 403. |
| knowledge token | `X-Knowledge-Token` or `Authorization: Bearer` with `KNOWLEDGE_TOKEN`. Separate from `ADMIN_TOKEN` and read-only. |
| share-link token | The unguessable token in a Policy Studio invitation link. |
| guest session | The cookie a guest receives on joining through a share link; bound to one document. |
| session + Origin check | The collaboration WebSocket: a session (or guest) cookie, and an `Origin` header matching the request host or `STUDIO_ALLOWED_ORIGINS`. |

In **local mode** (`LOCAL_MODE=true`) there are no accounts: `/login`,
`/auth/*` and `/admin/user-management` are not registered, every other route
is open, and Policy Studio share links are refused.

## Conventions

- Pages and their data are separate routes. A page handler returns an HTML
  shell; the page then fetches `<page>/data` or a sibling JSON route.
- The usual split is read open to signed-in users, write behind admin. Routes
  that spend model calls (analysis, AI proposals, chat inside a module) are
  behind admin as well. `/ai-chat/ask` is the exception: any signed-in user
  with the page grant can ask.
- Errors are JSON: `{"error": "..."}` with 400 for validation, 401, 403, 404,
  409 for a state conflict, 422 for a render failure, 500 otherwise.
- List routes that paginate take `page` and `per_page` and then answer
  `{items, total, page, per_page}`; without those parameters they return the
  whole array.
- Three routes exist only with `ALLOW_JSON_SAVE=true`; they are marked below.

The Handler column is the Go symbol, for finding the code.


## Shell, health and documentation

The home page, sign-in, build information and the in-app documentation.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/` | public | `app.homePage` |
| GET | `/changelog` | signed in | `app.changeLogPage` |
| GET | `/docs` | signed in | `app.openAPIPage` |
| GET | `/health` | public | `user.(*Handler).Health` |
| GET | `/help` | public | `app.helpPage` |
| GET | `/knowledge/:name` | public | `app.markdownKnowledgeDocPage` |
| GET | `/login` | public | `app.loginPage` |
| GET | `/openapi.json` | signed in | `app.openAPIJSON` |
| GET | `/version` | public | `app.versionHandler` |

## Authentication and accounts

`internal/authn`. Not registered in local mode. `/users` is the separate, older `internal/user` CRUD over the `users` table (names and e-mail addresses), unrelated to sign-in.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/admin/user-management` | admin | `app.userManagementPage` |
| POST | `/auth/bootstrap-admin` | setup token, first admin only | `authn.(*Handler).BootstrapAdmin` |
| POST | `/auth/login` | public | `authn.(*Handler).Login` |
| POST | `/auth/logout` | public | `authn.(*Handler).Logout` |
| GET | `/auth/me` | signed in | `authn.(*Handler).Me` |
| GET | `/auth/users` | admin | `authn.(*Handler).ListUsers` |
| POST | `/auth/users` | admin | `authn.(*Handler).CreateUser` |
| DELETE | `/auth/users/:username` | admin | `authn.(*Handler).DeleteUser` |
| PUT | `/auth/users/:username` | admin | `authn.(*Handler).UpdateUser` |
| GET | `/users` | public | `user.(*Handler).ListUsers` |
| POST | `/users` | admin | `user.(*Handler).CreateUser` |
| DELETE | `/users/:id` | admin | `user.(*Handler).DeleteUser` |
| GET | `/users/:id` | public | `user.(*Handler).GetUser` |
| PUT | `/users/:id` | admin | `user.(*Handler).UpdateUser` |

## Settings

`internal/settings` and `internal/app/settings_page.go`. The Wintermute connection, the three agents, and the Policy Studio guest switch.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/api/settings/ai-credentials` | admin | `settings.(*Handler).list` |
| DELETE | `/api/settings/ai-credentials/:name` | admin | `settings.(*Handler).clear` |
| PUT | `/api/settings/ai-credentials/:name` | admin | `settings.(*Handler).set` |
| GET | `/api/settings/ai-providers` | admin | `settings.(*Handler).providers` |
| PUT | `/api/settings/ai-providers` | admin | `settings.(*Handler).setPreferences` |
| GET | `/api/settings/ai-providers/agents` | admin | `settings.(*Handler).listAgents` |
| GET | `/api/settings/ai-providers/catalog` | admin | `settings.(*Handler).listCatalog` |
| POST | `/api/settings/ai-providers/test` | admin | `settings.(*Handler).testProvider` |
| GET | `/api/settings/policy-studio` | admin | `settings.(*Handler).studioSettings` |
| PUT | `/api/settings/policy-studio` | admin | `settings.(*Handler).setStudioSettings` |
| GET | `/settings` | admin | `app.settingsPage` |

## NIST control catalog

`internal/controlcatalog`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/controls` | signed in | `controlcatalog.(*Handler).Page` |
| DELETE | `/controls/:controlID` | admin | `controlcatalog.(*Handler).DeleteControl` |
| PUT | `/controls/:controlID` | admin | `controlcatalog.(*Handler).UpdateControl` |
| GET | `/controls/data` | signed in | `controlcatalog.(*Handler).ListControls` |
| GET | `/controls/data/:controlID` | signed in | `controlcatalog.(*Handler).GetControl` |
| GET | `/controls/detail/:controlID` | signed in | `controlcatalog.(*Handler).DetailPage` |
| GET | `/controls/export` | signed in | `controlcatalog.(*Handler).ExportControls` |
| GET | `/controls/family-json/data` | signed in | `controlcatalog.(*Handler).FamilyJSONData` |
| GET | `/controls/family-visibility` | signed in | `controlcatalog.(*Handler).FamilyVisibilityPage` |
| PUT | `/controls/family-visibility/:family` | admin | `controlcatalog.(*Handler).UpdateFamilyVisibility` |
| GET | `/controls/family-visibility/data` | signed in | `controlcatalog.(*Handler).ListFamilyVisibility` |
| GET | `/controls/hierarchy` | signed in | `controlcatalog.(*Handler).HierarchyPage` |
| GET | `/controls/hierarchy/export` | signed in | `controlcatalog.(*Handler).ExportHierarchyRTF` |
| GET | `/controls/manage` | signed in | `controlcatalog.(*Handler).ManagePage` |
| POST | `/controls/save` | admin (only with `ALLOW_JSON_SAVE`) | `controlcatalog.(*Handler).SaveControlsToJSON` |

## Security NFR catalog and NFR-control links

`internal/securitynfr` and `internal/nfrlink`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/security-nfrs` | signed in | `securitynfr.(*Handler).Page` |
| DELETE | `/security-nfrs/:key` | admin | `securitynfr.(*Handler).DeleteNFR` |
| PUT | `/security-nfrs/:key` | admin | `securitynfr.(*Handler).UpdateNFR` |
| PUT | `/security-nfrs/:key/weight` | admin | `securitynfr.(*Handler).SetNFRWeight` |
| GET | `/security-nfrs/data` | signed in | `securitynfr.(*Handler).ListNFRs` |
| GET | `/security-nfrs/detail/:key` | signed in | `securitynfr.(*Handler).DetailPage` |
| GET | `/security-nfrs/domains` | signed in | `securitynfr.(*Handler).ListDomains` |
| GET | `/security-nfrs/json/data` | signed in | `securitynfr.(*Handler).NFRJSONData` |
| GET | `/security-nfrs/links` | signed in | `nfrlink.(*Handler).Page` |
| GET | `/security-nfrs/links/data` | signed in | `nfrlink.(*Handler).ListLinkRows` |
| GET | `/security-nfrs/links/overrides` | admin | `nfrlink.(*Handler).ListOverrides` |
| PUT | `/security-nfrs/links/overrides` | admin | `nfrlink.(*Handler).SetOverride` |
| DELETE | `/security-nfrs/links/overrides/:nfrKey/:mappingControlID` | admin | `nfrlink.(*Handler).DeleteOverride` |
| POST | `/security-nfrs/links/rebuild` | admin | `nfrlink.(*Handler).Rebuild` |
| GET | `/security-nfrs/manage` | signed in | `securitynfr.(*Handler).ManagePage` |
| POST | `/security-nfrs/save` | admin (only with `ALLOW_JSON_SAVE`) | `securitynfr.(*Handler).SaveNFRsToJSON` |

## NFR Enrichment

`internal/nfrenrich`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/nfr-enrichment` | signed in | `nfrenrich.(*Handler).Page` |
| GET | `/nfr-enrichment/documents` | signed in | `nfrenrich.(*Handler).ListDocuments` |
| POST | `/nfr-enrichment/documents` | admin | `nfrenrich.(*Handler).AddDocument` |
| DELETE | `/nfr-enrichment/documents/:id` | admin | `nfrenrich.(*Handler).DeleteDocument` |
| POST | `/nfr-enrichment/documents/:id/analyze` | admin | `nfrenrich.(*Handler).Analyze` |
| GET | `/nfr-enrichment/library` | admin | `nfrenrich.(*Handler).ListLibrary` |
| GET | `/nfr-enrichment/proposals` | signed in | `nfrenrich.(*Handler).ListProposals` |
| POST | `/nfr-enrichment/proposals/:id/accept` | admin | `nfrenrich.(*Handler).AcceptProposal` |
| POST | `/nfr-enrichment/proposals/:id/reject` | admin | `nfrenrich.(*Handler).RejectProposal` |

## Asset types, Exceptions, Wiz rules

Pages rendered by `internal/app` that work in the browser over the catalog endpoints; they have no endpoints of their own.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/asset-types` | signed in | `app.assetTypesPage` |
| GET | `/exceptions` | signed in | `app.exceptionsPage` |
| GET | `/exceptions/detail` | signed in | `app.exceptionsDetailPage` |
| GET | `/wiz-rules` | signed in | `app.wizRulesPage` |

## Risk register

`internal/riskregister`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/risk-register` | signed in | `riskregister.(*Handler).Page` |
| POST | `/risk-register` | admin | `riskregister.(*Handler).Create` |
| DELETE | `/risk-register/:id` | admin | `riskregister.(*Handler).Delete` |
| PUT | `/risk-register/:id` | admin | `riskregister.(*Handler).Update` |
| GET | `/risk-register/data` | signed in | `riskregister.(*Handler).List` |
| GET | `/risk-register/manage` | signed in | `riskregister.(*Handler).ManagePage` |

## Audit findings

`internal/auditfinding`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/audit-findings` | signed in | `auditfinding.(*Handler).Page` |
| POST | `/audit-findings` | admin | `auditfinding.(*Handler).Create` |
| DELETE | `/audit-findings/:id` | admin | `auditfinding.(*Handler).Delete` |
| PUT | `/audit-findings/:id` | admin | `auditfinding.(*Handler).Update` |
| GET | `/audit-findings/data` | signed in | `auditfinding.(*Handler).List` |
| GET | `/audit-findings/data/:id` | signed in | `auditfinding.(*Handler).GetOne` |
| GET | `/audit-findings/summary` | signed in | `auditfinding.(*Handler).SummaryData` |
| GET | `/audit-findings/vocabulary` | signed in | `auditfinding.(*Handler).VocabularyData` |

## Policy library (documents, sections, mappings, coverage)

`internal/policydocs`; `compare.pdf` is wired in `internal/app/app.go` over `internal/doctemplate`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/policies` | signed in | `policydocs.(*Handler).ListPage` |
| POST | `/policies` | admin | `policydocs.(*Handler).CreateDocument` |
| DELETE | `/policies/:id` | admin | `policydocs.(*Handler).DeleteDocument` |
| PUT | `/policies/:id` | admin | `policydocs.(*Handler).UpdateDocument` |
| POST | `/policies/:id/approve` | admin | `policydocs.(*Handler).Approve` |
| GET | `/policies/:id/compare` | signed in | `policydocs.(*Handler).CompareData` |
| GET | `/policies/:id/compare.pdf` | signed in | `app.registerDocTemplateRoutes (inline)` |
| GET | `/policies/:id/controls` | signed in | `policydocs.(*Handler).DocumentControlRefs` |
| GET | `/policies/:id/data` | signed in | `policydocs.(*Handler).DocumentData` |
| GET | `/policies/:id/export.html` | signed in | `policydocs.(*Handler).ExportHTML` |
| GET | `/policies/:id/export.json` | signed in | `policydocs.(*Handler).ExportTemplateJSON` |
| GET | `/policies/:id/export.md` | signed in | `policydocs.(*Handler).ExportMarkdown` |
| GET | `/policies/:id/lint` | signed in | `policydocs.(*Handler).LintData` |
| POST | `/policies/:id/reopen` | admin | `policydocs.(*Handler).ReturnToDraft` |
| POST | `/policies/:id/retire` | admin | `policydocs.(*Handler).Retire` |
| GET | `/policies/:id/sections` | signed in | `policydocs.(*Handler).SectionsData` |
| POST | `/policies/:id/sections` | admin | `policydocs.(*Handler).CreateSection` |
| DELETE | `/policies/:id/sections/:sectionID` | admin | `policydocs.(*Handler).DeleteSection` |
| PUT | `/policies/:id/sections/:sectionID` | admin | `policydocs.(*Handler).UpdateSection` |
| GET | `/policies/:id/sections/:sectionID/controls` | signed in | `policydocs.(*Handler).SectionControlRefs` |
| POST | `/policies/:id/sections/:sectionID/controls` | admin | `policydocs.(*Handler).AttachControl` |
| DELETE | `/policies/:id/sections/:sectionID/controls/:refID` | admin | `policydocs.(*Handler).DetachControl` |
| POST | `/policies/:id/sections/reorder` | admin | `policydocs.(*Handler).ReorderSections` |
| POST | `/policies/:id/submit` | admin | `policydocs.(*Handler).SubmitForReview` |
| GET | `/policies/:id/versions` | signed in | `policydocs.(*Handler).VersionsData` |
| GET | `/policies/:id/view` | signed in | `policydocs.(*Handler).ViewPage` |
| GET | `/policies/control-search` | signed in | `policydocs.(*Handler).ControlSearch` |
| GET | `/policies/coverage` | signed in | `policydocs.(*Handler).CoveragePage` |
| GET | `/policies/coverage/data` | signed in | `policydocs.(*Handler).CoverageData` |
| GET | `/policies/data` | signed in | `policydocs.(*Handler).DocumentsData` |
| GET | `/policies/manage` | signed in | `policydocs.(*Handler).ManagePage` |
| GET | `/policies/meta` | signed in | `policydocs.(*Handler).Meta` |

## Policy Studio (collaborative editor, review, AI proposals)

`internal/policystudio` and `internal/policyai`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/assets/policy-studio/:file` | public | `policystudio.serveAsset` |
| GET | `/collab/policies/:id` | session + Origin check | `policystudio.(*Handler).Collab` |
| DELETE | `/policies/:id/guests/:sessionID` | admin | `policystudio.(*Handler).KickGuest` |
| GET | `/policies/:id/share-links` | admin | `policystudio.(*Handler).ListSharing` |
| POST | `/policies/:id/share-links` | admin | `policystudio.(*Handler).CreateShareLink` |
| DELETE | `/policies/:id/share-links/:linkID` | admin | `policystudio.(*Handler).RevokeShareLink` |
| GET | `/policies/:id/studio` | signed in | `policystudio.(*Handler).Page` |
| GET | `/policies/:id/studio/ai/edits` | signed in | `policyai.(*Handler).PlacedEdits` |
| POST | `/policies/:id/studio/ai/proposals` | admin | `policyai.(*Handler).Propose` |
| POST | `/policies/:id/studio/ai/proposals/:proposalID/placements` | admin | `policyai.(*Handler).Placements` |
| GET | `/policies/:id/studio/ai/status` | signed in | `policyai.(*Handler).Status` |
| GET | `/policies/:id/studio/comments` | signed in | `policystudio.(*Handler).ListThreads` |
| POST | `/policies/:id/studio/comments` | admin | `policystudio.(*Handler).CreateThread` |
| PATCH | `/policies/:id/studio/comments/:threadID` | admin | `policystudio.(*Handler).SetThreadStatus` |
| POST | `/policies/:id/studio/comments/:threadID/replies` | admin | `policystudio.(*Handler).Reply` |
| POST | `/policies/:id/studio/decisions` | admin | `policystudio.(*Handler).Decide` |
| POST | `/policies/:id/studio/migrate` | admin | `policystudio.(*Handler).Migrate` |
| GET | `/policies/:id/studio/provenance` | signed in | `policystudio.(*Handler).Provenance` |
| POST | `/policies/:id/studio/reorder` | admin | `policystudio.(*Handler).Reorder` |
| POST | `/policies/:id/studio/sections` | admin | `policystudio.(*Handler).AddSection` |
| DELETE | `/policies/:id/studio/sections/:uid` | admin | `policystudio.(*Handler).DeleteSection` |
| PATCH | `/policies/:id/studio/sections/:uid` | admin | `policystudio.(*Handler).SetKind` |
| POST | `/policies/:id/studio/sections/:uid/restore` | admin | `policystudio.(*Handler).Restore` |
| GET | `/policies/:id/studio/state` | signed in | `policystudio.(*Handler).State` |
| POST | `/policies/from-template` | admin | `policystudio.(*Handler).FromTemplate` |
| GET | `/policies/library` | admin | `policyai.(*Handler).Library` |

## Policy templates and client profiles

`internal/policystudio` (templates), `internal/policyai` (drafting a template from a library document) and `internal/clientprofile`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/policies/app-templates` | admin | `policystudio.(*Handler).ListAppTemplates` |
| DELETE | `/policies/app-templates/:tid` | admin | `policystudio.(*Handler).DeleteAppTemplate` |
| GET | `/policies/app-templates/:tid` | admin | `policystudio.(*Handler).GetAppTemplate` |
| PUT | `/policies/app-templates/:tid` | admin | `policystudio.(*Handler).SaveAppTemplate` |
| POST | `/policies/app-templates/:tid/publish` | admin | `policystudio.(*Handler).PublishAppTemplate` |
| POST | `/policies/app-templates/:tid/restore` | admin | `policystudio.(*Handler).RestoreAppTemplate` |
| POST | `/policies/app-templates/:tid/retire` | admin | `policystudio.(*Handler).RetireAppTemplate` |
| GET | `/policies/app-templates/:tid/template.json` | admin | `policystudio.(*Handler).ExportAppTemplate` |
| POST | `/policies/app-templates/copy` | admin | `policystudio.(*Handler).CopyAppTemplate` |
| POST | `/policies/app-templates/draft` | admin | `policyai.(*Handler).DraftTemplate` |
| GET | `/policies/app-templates/draft/status` | admin | `policyai.(*Handler).TemplateDraftStatus` |
| POST | `/policies/app-templates/import` | admin | `policystudio.(*Handler).ImportAppTemplate` |
| GET | `/policies/app-templates/meta` | admin | `policystudio.(*Handler).TemplateMeta` |
| GET | `/policies/clients` | signed in | `clientprofile.(*Handler).list` |
| POST | `/policies/clients` | admin | `clientprofile.(*Handler).create` |
| DELETE | `/policies/clients/:clientID` | admin | `clientprofile.(*Handler).delete` |
| PUT | `/policies/clients/:clientID` | admin | `clientprofile.(*Handler).update` |
| GET | `/policies/clients/:clientID/facts` | signed in | `clientprofile.(*Handler).facts` |
| DELETE | `/policies/clients/:clientID/facts/:key` | admin | `clientprofile.(*Handler).deleteFact` |
| PUT | `/policies/clients/:clientID/facts/:key` | admin | `clientprofile.(*Handler).setFact` |
| GET | `/policies/templates` | signed in | `policystudio.(*Handler).ListTemplates` |
| GET | `/policies/templates/manage` | admin | `policystudio.(*Handler).TemplatesPage` |

## Policy Studio guests

`internal/policystudio`. Reached with a share link and then a guest session cookie; never with an account. Refused in local mode and until guest links are switched on in Settings.

| Method | Path | Access | Handler |
|---|---|---|---|
| POST | `/shared/api/ai/proposals` | guest session | `policyai.(*Handler).GuestPropose` |
| POST | `/shared/api/ai/proposals/:proposalID/placements` | guest session | `policyai.(*Handler).GuestPlacements` |
| GET | `/shared/api/ai/status` | guest session | `policyai.(*Handler).GuestStatus` |
| GET | `/shared/api/comments` | guest session | `policystudio.(*Handler).GuestThreads` |
| POST | `/shared/api/comments` | guest session | `policystudio.(*Handler).GuestCreateThread` |
| POST | `/shared/api/comments/:threadID/replies` | guest session | `policystudio.(*Handler).GuestReply` |
| POST | `/shared/api/leave` | guest session | `policystudio.(*Handler).GuestLeave` |
| GET | `/shared/api/state` | guest session | `policystudio.(*Handler).GuestState` |
| GET | `/shared/p/:token` | share-link token | `policystudio.(*Handler).JoinPage` |
| POST | `/shared/p/:token` | share-link token | `policystudio.(*Handler).Join` |
| GET | `/shared/studio` | guest session | `policystudio.(*Handler).GuestStudio` |

## Document templates and rendering

`internal/doctemplate`: Typst, LaTeX and Pandoc renders of a policy or a sample payload.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/templates` | signed in | `doctemplate.(*Handler).GalleryPage` |
| GET | `/templates/brand` | signed in | `doctemplate.(*Handler).BrandData` |
| PUT | `/templates/brand` | admin | `doctemplate.(*Handler).SaveBrand` |
| POST | `/templates/brand/reset` | admin | `doctemplate.(*Handler).ResetBrand` |
| GET | `/templates/data` | signed in | `doctemplate.(*Handler).CatalogData` |
| POST | `/templates/engines/refresh` | admin | `doctemplate.(*Handler).RefreshEngines` |
| GET | `/templates/manage` | signed in | `doctemplate.(*Handler).BrandPage` |
| GET | `/templates/render` | signed in | `doctemplate.(*Handler).RenderEndpoint` |

## Regulation Coverage

`internal/regcoverage`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/regulation-coverage` | signed in | `regcoverage.(*Handler).IndexPage` |
| DELETE | `/regulation-coverage/:id` | admin | `regcoverage.(*Handler).Delete` |
| GET | `/regulation-coverage/:id` | signed in | `regcoverage.(*Handler).ReportPage` |
| POST | `/regulation-coverage/:id/analyze` | admin | `regcoverage.(*Handler).Analyze` |
| DELETE | `/regulation-coverage/:id/chat` | admin | `regcoverage.(*Handler).ClearChat` |
| GET | `/regulation-coverage/:id/chat` | signed in | `regcoverage.(*Handler).ListChat` |
| POST | `/regulation-coverage/:id/chat` | admin | `regcoverage.(*Handler).Ask` |
| GET | `/regulation-coverage/:id/report.json` | signed in | `regcoverage.(*Handler).ReportJSON` |
| GET | `/regulation-coverage/:id/report.pdf` | signed in | `regcoverage.(*Handler).ReportPDF` |
| POST | `/regulation-coverage/:id/revise` | admin | `regcoverage.(*Handler).Revise` |
| GET | `/regulation-coverage/:id/versions` | signed in | `regcoverage.(*Handler).ListVersions` |
| GET | `/regulation-coverage/library` | admin | `regcoverage.(*Handler).ListLibrary` |
| GET | `/regulation-coverage/regulations` | signed in | `regcoverage.(*Handler).ListRegulations` |
| POST | `/regulation-coverage/regulations` | admin | `regcoverage.(*Handler).Import` |

## Risk & Crisis Exercises

`internal/crisisexercise`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/crisis-exercises` | signed in | `crisisexercise.(*Handler).IndexPage` |
| DELETE | `/crisis-exercises/:id` | admin | `crisisexercise.(*Handler).Delete` |
| GET | `/crisis-exercises/:id` | signed in | `crisisexercise.(*Handler).ExercisePage` |
| PATCH | `/crisis-exercises/:id` | admin | `crisisexercise.(*Handler).Update` |
| POST | `/crisis-exercises/:id/advise` | admin | `crisisexercise.(*Handler).Advise` |
| POST | `/crisis-exercises/:id/after-action` | admin | `crisisexercise.(*Handler).DraftAfterAction` |
| POST | `/crisis-exercises/:id/agent` | admin | `crisisexercise.(*Handler).AskAgent` |
| DELETE | `/crisis-exercises/:id/chat` | admin | `crisisexercise.(*Handler).ClearChat` |
| GET | `/crisis-exercises/:id/chat` | signed in | `crisisexercise.(*Handler).ListChat` |
| PUT | `/crisis-exercises/:id/classification` | admin | `crisisexercise.(*Handler).SaveClassification` |
| POST | `/crisis-exercises/:id/clocks/:childID` | admin | `crisisexercise.(*Handler).RecordNotification` |
| POST | `/crisis-exercises/:id/clone` | admin | `crisisexercise.(*Handler).Clone` |
| GET | `/crisis-exercises/:id/coverage` | signed in | `crisisexercise.(*Handler).Coverage` |
| POST | `/crisis-exercises/:id/decisions` | admin | `crisisexercise.(*Handler).SaveDecision` |
| DELETE | `/crisis-exercises/:id/decisions/:childID` | admin | `crisisexercise.(*Handler).DeleteDecision` |
| POST | `/crisis-exercises/:id/design` | admin | `crisisexercise.(*Handler).Design` |
| POST | `/crisis-exercises/:id/findings` | admin | `crisisexercise.(*Handler).SaveFinding` |
| DELETE | `/crisis-exercises/:id/findings/:childID` | admin | `crisisexercise.(*Handler).DeleteFinding` |
| GET | `/crisis-exercises/:id/handout.md` | signed in | `crisisexercise.(*Handler).Handout` |
| POST | `/crisis-exercises/:id/injects` | admin | `crisisexercise.(*Handler).SaveInject` |
| DELETE | `/crisis-exercises/:id/injects/:childID` | admin | `crisisexercise.(*Handler).DeleteInject` |
| POST | `/crisis-exercises/:id/injects/:childID/probe` | admin | `crisisexercise.(*Handler).ProbeInject` |
| POST | `/crisis-exercises/:id/injects/:childID/response` | admin | `crisisexercise.(*Handler).RecordResponse` |
| GET | `/crisis-exercises/:id/msel.csv` | signed in | `crisisexercise.(*Handler).MSEL` |
| POST | `/crisis-exercises/:id/objectives` | admin | `crisisexercise.(*Handler).SaveObjective` |
| POST | `/crisis-exercises/:id/participants` | admin | `crisisexercise.(*Handler).SaveParticipants` |
| POST | `/crisis-exercises/:id/phases` | admin | `crisisexercise.(*Handler).SavePhase` |
| POST | `/crisis-exercises/:id/references` | admin | `crisisexercise.(*Handler).AddReference` |
| DELETE | `/crisis-exercises/:id/references/:childID` | admin | `crisisexercise.(*Handler).DeleteReference` |
| GET | `/crisis-exercises/:id/report.json` | signed in | `crisisexercise.(*Handler).ReportJSON` |
| GET | `/crisis-exercises/:id/report.pdf` | signed in | `crisisexercise.(*Handler).ReportPDF` |
| GET | `/crisis-exercises/:id/versions` | signed in | `crisisexercise.(*Handler).ListVersions` |
| POST | `/crisis-exercises/:id/versions` | admin | `crisisexercise.(*Handler).CutVersion` |
| GET | `/crisis-exercises/catalog` | signed in | `crisisexercise.(*Handler).Catalog` |
| GET | `/crisis-exercises/exercises` | signed in | `crisisexercise.(*Handler).ListExercises` |
| POST | `/crisis-exercises/exercises` | admin | `crisisexercise.(*Handler).Create` |
| GET | `/crisis-exercises/suggest` | signed in | `crisisexercise.(*Handler).SuggestReferences` |

## Reports and the Exceptions Workbook

`internal/reports` (the linked/unlinked controls report), `internal/reporting` (two sample PDFs through headless Chrome) and `internal/exceptionbook`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/reports` | signed in | `reports.(*Handler).Page` |
| GET | `/reports/control-assessment.pdf` | signed in | `reporting.(*Handler).SampleControlAssessmentPDF` |
| GET | `/reports/data/unlinked-controls` | signed in | `reports.(*Handler).UnlinkedControlsData` |
| GET | `/reports/exceptions-workbook` | signed in | `app.exceptionsWorkbookPage` |
| GET | `/reports/exceptions-workbook/download` | signed in | `app.exceptionsWorkbookDownload` |
| POST | `/reports/exceptions-workbook/download` | signed in | `app.exceptionsWorkbookDownload` |
| GET | `/reports/exceptions-report` | signed in | `app.exceptionsReportPage` |
| GET | `/reports/risk-assessment.pdf` | signed in | `reporting.(*Handler).SampleRiskAssessmentPDF` |

## Jira and stored JSON

`internal/app/jira_pages.go` over `internal/jira`; `internal/app/stored_json.go`. Jira credentials arrive in each request body and are not stored.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/JSON_view` | signed in | `app.jsonViewPage` |
| GET | `/jira/json` | signed in | `app.jiraJSONPage` |
| POST | `/jira/json/data` | signed in | `app.jiraJSONData` |
| POST | `/jira/json/export` | admin (only with `ALLOW_JSON_SAVE`) | `app.jiraJSONExport` |
| POST | `/jira/json/test-auth` | signed in | `app.jiraJSONTestAuth` |
| GET | `/jira/reports` | signed in | `app.jiraReportsPage` |
| POST | `/jira/reports/data` | signed in | `app.jiraReportsData` |
| GET | `/jira/reports/presets` | signed in | `app.jiraReportPresetsData` |
| GET | `/json-view` | signed in | `app.jsonViewPage` |
| GET | `/stored-json` | public | `app.listStoredJSONDocuments` |
| POST | `/stored-json` | public | `app.saveStoredJSONDocument` |
| GET | `/stored-json/:id` | public | `app.getStoredJSONDocument` |

## AI Chat and the AI dock

`internal/app/ai_chat.go`. Every question goes to the configured Wintermute server.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/ai-chat` | signed in | `app.aiChatPage` |
| POST | `/ai-chat/ask` | signed in | `app.aiChatAsk` |
| POST | `/ai-chat/stop` | signed in | `app.aiChatStop` |
| GET | `/ai-chat/usage` | signed in | `app.aiChatUsageHandler` |
| GET | `/ai-chat/wintermute/agents` | signed in | `app.aiChatWintermuteAgents` |
| GET | `/ai-chat/wintermute/catalog` | signed in | `app.aiChatWintermuteCatalog` |
| GET | `/ai-chat/wintermute/status` | signed in | `app.aiChatWintermuteStatus` |

## Knowledge API

`internal/knowledge`. Read-only. Registered only when `KNOWLEDGE_TOKEN` is set, or in local mode.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/api/knowledge/export` | knowledge token | `knowledge.(*Handler).Export` |
| GET | `/api/knowledge/index/:kind` | knowledge token | `knowledge.(*Handler).Index` |
| GET | `/api/knowledge/item` | knowledge token | `knowledge.(*Handler).Get` |
| GET | `/api/knowledge/overview` | knowledge token | `knowledge.(*Handler).Overview` |
| GET | `/api/knowledge/search` | knowledge token | `knowledge.(*Handler).Search` |

## Utilities

`internal/app/utilities.go` over `internal/dbsync`.

| Method | Path | Access | Handler |
|---|---|---|---|
| GET | `/utilities` | admin | `app.utilitiesPage` |
| GET | `/utilities/backup` | admin | `app.utilitiesBackup` |
| GET | `/utilities/export` | admin | `app.utilitiesExport` |
| POST | `/utilities/import` | admin | `app.utilitiesImport` |
| GET | `/utilities/integrity-check` | admin | `app.utilitiesIntegrityCheck` |
| GET | `/utilities/knowledge-export` | admin | `app.utilitiesKnowledgeExport` |

## grc-render (a separate binary)

`cmd/grc-render`, served on the render host, not by GRC; GRC calls it when
`RENDER_URL` is set (`internal/doctemplate/remote.go`). Every route needs
`Authorization: Bearer <GRC_RENDER_TOKEN>`.

| Method | Path | Access | Handler |
|---|---|---|---|
| POST | `/v1/render` | render token | `doctemplate.(*RemoteServer).render` |
| GET | `/v1/engines` | render token | `doctemplate.(*RemoteServer).engines` |
