# OSCAL test fixtures

Cut from NIST's official OSCAL content, which is in the public domain
(<https://github.com/usnistgov/oscal-content>, `nist.gov/SP800-53/rev5/json/`).
Catalog version 5.2.0, `oscal-version` 1.2.3, last modified
2026-10-22T11:30:00-00:00. Cut on 2026-11-09. Nothing in the repository
downloads these files; the full catalog is not committed.

| Fixture | Source file (SHA-256 of the full file) | What was kept |
|---|---|---|
| `NIST_SP-800-53_rev5_catalog_cut.json` | `NIST_SP-800-53_rev5_catalog.json` (`64ca6b4161de51ab6af098a7d101d326f14c95e1abf721396cd8a049e8115eeb`) | The catalog's `uuid`; its metadata `title`, `last-modified`, `version` and `oscal-version` (revisions, roles and parties dropped); the AC and SA groups with `AC-1`, `AC-2` (with only its enhancement `AC-2(1)`), the withdrawn `AC-13`, and `SA-24`, each control unchanged; the 11 back-matter resources those controls cite. |
| `NIST_SP-800-53_rev5_LOW-baseline_profile_cut.json` | `NIST_SP-800-53_rev5_LOW-baseline_profile.json` (`cfd4f5d9a192eb42dc04b745d82f95a85926a2a85a45418034a380a42bda9424`) | The profile with its `with-ids` cut to the fixture's controls (`ac-1`, `ac-2`) and its metadata cut as above. |
| `NIST_SP-800-53_rev5_MODERATE-baseline_profile_cut.json` | `NIST_SP-800-53_rev5_MODERATE-baseline_profile.json` (`e3a0f11237cfaac24dfcf1586e07683f17423757fd84208deb33196b12e8f590`) | The same, with `ac-1`, `ac-2` and `ac-2.1`. |

What the fixture exercises: `AC-1` has Assignment and Selection parameters and
an aggregating parameter (`ac-1_prm_1` aggregates `ac-01_odp.01` and
`ac-01_odp.02`), three statement levels (`a.` / `1.` / `(a)`), and NIST's
space before punctuation after an insertion (`}} ; and`). `AC-2(1)` is an
enhancement in the MODERATE baseline but not the LOW one. `AC-13` is withdrawn
(a `status` prop, no statement, `incorporated-into` links). `SA-24` is in the
NIST catalog but not in the catalog file grc ships.

The tests check the fixture against the OSCAL 1.2.1 JSON schema, so a cut that
broke the document would fail there.
