package app

// The design system in this file is ported from wintermute
// (internal/web/static/style.css in that repository): its palettes, its token
// names and its component vocabulary, so the two applications read as one
// system rather than as two products that happen to share an author.
//
// The mechanism is not ported. wintermute is a single-page app that owns its
// markup, so it can style .card and .pane directly. Every page here renders its
// own HTML with its own <style> block, so the same look has to be imposed from
// outside — which is what this layer does, with !important, over both
// wintermute's class names and the ones this application's pages already use.
// That is the existing arrangement in theme_middleware.go; this replaces what
// it painted, not how it paints.

// themeK40 is wintermute's 40K theme: a gothic-industrial cogitator terminal,
// bone text and brass rule on a warm near-black, with the failing-CRT overlay
// in fritzOverlayTag. It is the one theme here that carries an effect of its
// own rather than a palette alone.
const themeK40 = "40k"

// palettes are the per-theme token values, in wintermute's names.
//
// dark, matrix, chaos and 40k are that stylesheet's palettes unchanged. Light
// has no counterpart there — wintermute is dark-only — so this application's
// existing grey-and-maroon business palette is kept and re-expressed in the
// same names, because deleting the one theme that prints well on a projector
// is not what porting a design system means.
func palettes(theme string) string {
	switch theme {
	case themeLight:
		return `
  color-scheme: light !important;
  --bg: #f4f3f1 !important;
  --bg-image: linear-gradient(160deg, #f4f3f1, #ece9e5 52%, #e4e0da) !important;
  --surface: #ffffff !important;
  --surface-2: #f1efec !important;
  --border: #d4d2cf !important;
  --text-base: #2a2a2a !important;
  --muted-base: #6b6f76 !important;
  --accent: #7a1f2e !important;
  --on-accent: #ffffff !important;
  --error: #b3261e !important;
  --gain: #2f6b3f !important;
  --loss: #b3261e !important;`

	case themeMatrix, themeChaos:
		return `
  color-scheme: dark !important;
  --bg: #000000 !important;
  --bg-image: none !important;
  --surface: #001a00 !important;
  --surface-2: #002b00 !important;
  --border: #00611a !important;
  --text-base: #00ff41 !important;
  --muted-base: #00a82c !important;
  --accent: #00ff41 !important;
  --on-accent: #000000 !important;
  --error: #ff3b3b !important;
  --gain: #00ff41 !important;
  --loss: #ff3b3b !important;`

	case themeK40:
		// The muted tone is a failing indicator lamp rather than the brass it
		// reads as at first glance: a dark warm colour on a dark warm ground
		// separates only by lightness, and anyone reading a hint had to work at
		// it. Separating by hue as well makes it a different kind of text
		// rather than dimmer text — and it is the register this theme was
		// already speaking in.
		return `
  color-scheme: dark !important;
  --bg: #0a0806 !important;
  --bg-image: none !important;
  --surface: #12100c !important;
  --surface-2: #1c1811 !important;
  --border: #6b5528 !important;
  --text-base: #e4d3a6 !important;
  --muted-base: #b3c69a !important;
  --accent: #d8a730 !important;
  --on-accent: #0a0806 !important;
  --error: #ff5f56 !important;
  --gain: #a8bb63 !important;
  --loss: #ff5f56 !important;`

	default:
		return `
  color-scheme: dark !important;
  --bg: #0f1115 !important;
  --bg-image: none !important;
  --surface: #171a21 !important;
  --surface-2: #1f232c !important;
  --border: #2a2f3a !important;
  --text-base: #e6e8ec !important;
  --muted-base: #8b93a3 !important;
  --accent: #6ea8fe !important;
  --on-accent: #0b0d12 !important;
  --error: #ff8a8a !important;
  --gain: #7ddc9a !important;
  --loss: #ff8a8a !important;`
	}
}

// aliasCSS maps this application's existing variable names onto the ported
// palette.
//
// Around three hundred rules across the page handlers read --ink, --line,
// --muted and --panel. Aliasing them is what lets one palette change restyle
// every page without thirty handlers being edited, and what keeps a page whose
// markup this layer does not recognise from ending up half-themed.
const aliasCSS = `
  --ink: var(--text) !important;
  --line: var(--border) !important;
  --panel: var(--surface) !important;
  --panel-text: var(--text) !important;
  --surface-strong: var(--surface-2) !important;
  --hover: var(--surface-2) !important;
  --accent-strong: var(--accent) !important;
  --accent-dark: var(--accent) !important;
  --accent-contrast: var(--on-accent) !important;
  --chip: var(--surface-2) !important;
  --chip-border: var(--border) !important;
  --good: var(--gain) !important;
  --success: var(--gain) !important;
  --bad: var(--loss) !important;
  --danger: var(--error) !important;
  --red: var(--error) !important;
  --green: var(--gain) !important;
  --radius: 10px !important;`

// textLiftCSS derives the two text colours from the palette's base values, so
// one control lifts all five themes and no rule below has to know the setting
// exists.
//
// The plain assignment comes first as the fallback. If color-mix is not
// understood the whole derived declaration is dropped, and a dropped custom
// property does not fall back to the palette — it makes every colour that uses
// it invalid, which on these backgrounds means an unreadable page.
const textLiftCSS = `
:root {
  --text: var(--text-base);
  --muted: var(--muted-base);
}
@supports (color: color-mix(in srgb, red, blue)) {
  :root {
    --text: color-mix(in srgb, var(--text-base), #fff var(--text-lift, 0%));
    --muted: color-mix(in srgb, var(--muted-base), #fff var(--text-lift, 0%));
  }
}`

// designComponentCSS is wintermute's component layer, addressed at both its own
// class names and the ones the pages here already use.
//
// The pairing in each selector list is deliberate: .card and .stat are
// wintermute's, .summary-card and .status-card are this application's, and they
// are given one appearance rather than two that nearly match.
const designComponentCSS = `
html, body {
  background: var(--bg) !important;
  background-image: var(--bg-image) !important;
  color: var(--text) !important;
}
body {
  font: 15px/1.55 ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif !important;
}

/* ---- surfaces ---- */
main, .panel, .list, .detail, .table-wrap, pre, .msg, .auth-box, .panel-body,
.rows, .row, .control-item, .nfr-item, .pane, .cred {
  background: var(--surface) !important;
  color: var(--text) !important;
  border-color: var(--border) !important;
}
.card, .summary-card, .status-card, .detail-card, .report-card, .stat,
[class*="card"] {
  background: var(--surface) !important;
  color: var(--text) !important;
  border: 1px solid var(--border) !important;
  border-radius: var(--radius) !important;
}
/* A section head is a caption for what is under it, not a heading competing
   with the page title: small, spaced, and in the muted tone. */
.panel-header, .list-header, .detail-header, .pane-head, .group-head, .card-head h3 {
  background: var(--surface-2) !important;
  color: var(--muted) !important;
  border-color: var(--border) !important;
}
.group-head, .stat .k {
  background: none !important;
  font-size: 11px !important;
  text-transform: uppercase !important;
  letter-spacing: 0.07em !important;
}
.stat .n { font-size: 20px !important; font-weight: 600 !important; }

/* ---- tables ---- */
table { border-collapse: collapse !important; font-size: 13.5px !important; }
th, td {
  border-bottom: 1px solid var(--border) !important;
  border-color: var(--border) !important;
  vertical-align: top !important;
}
thead th, th {
  background: var(--surface-2) !important;
  color: var(--muted) !important;
  font-size: 11px !important;
  text-transform: uppercase !important;
  letter-spacing: 0.07em !important;
  font-weight: 600 !important;
}
tbody tr:hover { background: var(--surface-2) !important; }
td.num, th.num { text-align: right !important; font-variant-numeric: tabular-nums !important; }

/* ---- controls ---- */
input, select, textarea {
  background: var(--bg) !important;
  color: var(--text) !important;
  border: 1px solid var(--border) !important;
  border-radius: 8px !important;
}
input:focus, select:focus, textarea:focus {
  border-color: var(--accent) !important;
  outline: none !important;
}
/* wintermute fills every button with the accent and letters it in the palette's
   near-black, because there a button is an action. Here it is also a filter
   chip, a list row, a domain pill and a section toggle — most of the buttons on
   these pages are one of those. Built that way first and looked at: a catalog
   page came out as a column of accent-filled bricks with no emphasis left for
   the control that actually submits. So the default is wintermute's ghost
   button, and the fill is kept for the one control a form exists for.

   Everything else about the treatment is literal: .ghost-btn / .secondary
   outlined, .link-btn bare, .danger in the error colour, and the lettering on a
   filled button taken from the palette (--on-accent) rather than hard-coded, so
   the phosphor green and the brass are readable on their own accent.

   Scoped to page content: the sidebar, the AI panel, the command palette and
   the theme toggle draw their own controls, exactly as wintermute's own
   .view-btn, .link-btn and .session-del opt out of this rule there. */
.global-content button, body:not(:has(.global-shell)) main button {
  background: var(--surface-2) !important;
  color: var(--text) !important;
  border: 1px solid var(--border) !important;
  border-radius: 8px !important;
  font-weight: 500 !important;
  cursor: pointer !important;
}
.global-content button:hover, body:not(:has(.global-shell)) main button:hover {
  border-color: var(--accent) !important;
}
.global-content button:disabled, body:not(:has(.global-shell)) main button:disabled {
  opacity: 0.5 !important;
  cursor: default !important;
}
/* The one action a screen is for. */
.global-content button[type="submit"], .global-content button.primary,
body:not(:has(.global-shell)) main button[type="submit"],
body:not(:has(.global-shell)) main button.primary {
  background: var(--accent) !important;
  color: var(--on-accent) !important;
  border-color: transparent !important;
  font-weight: 600 !important;
}
/* Whatever a filled button holds is lettered on the accent too. A button in
   wintermute holds a word; one here can hold a label the page paints in the
   muted tone, which on the fill would be grey on blue. */
.global-content button[type="submit"] *, .global-content button.primary *,
body:not(:has(.global-shell)) main button[type="submit"] * {
  color: inherit !important;
}
/* A selected row keeps its selection visible: the layer above would otherwise
   flatten every state a page draws with a button into one surface. */
.global-content button.active, .global-content button.selected,
.global-content button[aria-pressed="true"], .global-content button[aria-selected="true"] {
  border-color: var(--accent) !important;
  color: var(--text) !important;
}
/* Outlined, for everything that is not the action a screen is for. */
.global-content button.secondary, .global-content .ghost-btn, .global-content .icon-btn,
body:not(:has(.global-shell)) main button.secondary {
  background: transparent !important;
  color: var(--text) !important;
  border: 1px solid var(--border) !important;
  font-weight: 500 !important;
}
.global-content .link-btn {
  background: none !important;
  border: none !important;
  color: var(--muted) !important;
  padding: 0 !important;
  font-size: 12px !important;
  font-weight: 500 !important;
}
.global-content .link-btn:hover { color: var(--text) !important; }
/* A destructive control keeps the error colour under every palette: it is the
   one button on a page that should never blend in. */
.global-content button.danger, .global-content .danger-btn,
body:not(:has(.global-shell)) main button.danger {
  background: transparent !important;
  color: var(--error) !important;
  border: 1px solid var(--error) !important;
}
.global-content button.danger:hover, .global-content .danger-btn:hover {
  background: var(--error) !important;
  color: var(--bg) !important;
}

/* ---- pills, chips, tags ---- */
.pill, .chip, .tag, .badge {
  display: inline-block;
  padding: 1px 8px !important;
  border-radius: 999px !important;
  font-size: 11px !important;
  background: var(--surface-2) !important;
  color: var(--muted) !important;
  border: 1px solid var(--border) !important;
}
.pill.on, .pill.good, .chip.on { color: var(--gain) !important; border-color: var(--gain) !important; }
.pill.off, .pill.high, .pill.bad { color: var(--error) !important; border-color: var(--error) !important; }

/* ---- the page-tab strip, where a page still renders one ---- */
.tab {
  background: transparent !important;
  color: var(--muted) !important;
  border: 1px solid transparent !important;
  border-radius: 8px !important;
  font-weight: 500 !important;
}
.tab:hover { background: var(--surface-2) !important; color: var(--text) !important; }
.tab.active {
  background: var(--surface-2) !important;
  color: var(--text) !important;
  border-color: var(--border) !important;
}

/* ---- text ---- */
a { color: var(--accent) !important; }
code, .mono, pre code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace !important;
  color: var(--text) !important;
}
code, .mono { background: var(--surface-2) !important; border-color: var(--border) !important; }
.status, .meta, .hint, .row-sub, .nfr-meta, .muted, .keyring, .note {
  color: var(--muted) !important;
}
.error, .status.error, .warn { color: var(--error) !important; }
.pos, .up { color: var(--gain) !important; }
.neg, .down { color: var(--loss) !important; }
h1, h2, h3, h4 { color: var(--text) !important; }`

// monoThemeCSS is the typography for the three themes that are meant to look
// like output from a machine rather than a document.
const monoThemeCSS = `
body, button, input, select, textarea, .tab, table {
  font-family: "Courier New", Courier, monospace !important;
}`

// k40ThemeCSS is what makes 40K a costume rather than a palette swap: stamped
// lettering on the headings, and the name of the machine carrying the gold.
//
// No mark, badge or lettering belonging to anyone else appears in it. The whole
// effect is a colour scheme, a typeface and the drawn-from-scratch overlay in
// fritzOverlayTag.
const k40ThemeCSS = monoThemeCSS + `
body, button, input, select, textarea { letter-spacing: 0.02em !important; }
h1, h2, .group-head, .panel-header, .list-header, thead th, th {
  text-transform: uppercase !important;
  letter-spacing: 0.12em !important;
}
h1 { color: var(--accent) !important; }
.global-sidenav .tab-home { color: var(--accent) !important; }`

// chaosCharCSS eases the per-character colour the glitch engine sets, so the
// effect is a shimmer rather than a strobe. Chaos only: the other themes must
// not pay for a transition on every span on the page.
const chaosCharCSS = `
.chaos-char {
  transition: color 140ms linear;
}`

// themeStyleTag is the whole palette layer for one theme, in the order the
// cascade needs it: palette tokens, the aliases the pages read, the text-lift
// derivation, then the components.
func themeStyleTag(theme string) string {
	css := `<style id="global-theme-style">
:root {` + palettes(theme) + aliasCSS + `
}` + textLiftCSS + designComponentCSS

	switch theme {
	case themeMatrix:
		css += monoThemeCSS
	case themeChaos:
		css += monoThemeCSS + chaosCharCSS
	case themeK40:
		css += k40ThemeCSS
	}
	return css + `
</style>` + textLiftInitTag
}

// textLiftInitTag applies the saved brightness before first paint.
//
// Every palette here except Light is dark and was tuned on one screen; the same
// colours on a phone in daylight, or on a panel with the contrast wound down,
// can be genuinely hard to read. This lifts the two text colours towards white
// without touching the backgrounds or the accents, so a theme survives being
// made legible.
//
// It is a per-browser choice in localStorage rather than server state: it is a
// property of the screen being looked at, and the same install is read from a
// phone and from a 32:9 monitor. Applied here, at the end of <head>, for the
// same reason wintermute loads its own copy there — after paint it is a visible
// flicker on every page load.
const textLiftInitTag = `<script id="global-text-lift-init">
(() => {
  const lift = parseInt(localStorage.getItem('grc-text-lift'), 10);
  if (Number.isFinite(lift) && lift > 100) {
    document.documentElement.style.setProperty(
      '--text-lift', Math.min(175, lift) - 100 + '%');
  }
})();
</script>`

// fritzOverlayTag is the 40K theme's failing-CRT layer: scanlines and a
// vignette that never move, a roll bar drifting down the glass, and irregular
// bursts of tearing.
//
// It is fixed over the whole shell and inert to the pointer, so it can never
// intercept a click or shift anything: every effect here is painted. The bursts
// come from a timer rather than a keyframe loop because a predictable glitch
// stops reading as a fault and starts reading as a decoration.
const fritzOverlayTag = `<div id="global-fritz" class="fritz" aria-hidden="true">
  <div class="fritz-scan"></div>
  <div class="fritz-roll"></div>
  <div class="fritz-vignette"></div>
</div>
<style id="global-fritz-style">
.fritz {
  --fritz-intensity: 1;
  position: fixed; inset: 0; z-index: 40;
  pointer-events: none;
  overflow: hidden;
}
.fritz-scan {
  position: absolute; inset: 0;
  background: repeating-linear-gradient(
    to bottom,
    rgba(0, 0, 0, 0) 0px,
    rgba(0, 0, 0, 0) 2px,
    rgba(0, 0, 0, calc(0.22 * var(--fritz-intensity))) 3px,
    rgba(0, 0, 0, calc(0.22 * var(--fritz-intensity))) 3px);
  background-size: 100% 3px;
}
.fritz-roll {
  position: absolute; left: 0; right: 0;
  height: 22vh;
  background: linear-gradient(
    to bottom,
    rgba(216, 167, 48, 0) 0%,
    rgba(216, 167, 48, calc(0.05 * var(--fritz-intensity))) 45%,
    rgba(255, 226, 160, calc(0.08 * var(--fritz-intensity))) 50%,
    rgba(216, 167, 48, calc(0.05 * var(--fritz-intensity))) 55%,
    rgba(216, 167, 48, 0) 100%);
  animation: fritz-roll 9s linear infinite;
}
@keyframes fritz-roll {
  from { transform: translateY(-25vh); }
  to   { transform: translateY(105vh); }
}
.fritz-vignette {
  position: absolute; inset: 0;
  background: radial-gradient(
    ellipse at center,
    rgba(0, 0, 0, 0) 55%,
    rgba(0, 0, 0, calc(0.45 * var(--fritz-intensity))) 100%);
}
.fritz.fritz-flicker {
  background: rgba(216, 167, 48, calc(0.06 * var(--fritz-intensity)));
  animation: fritz-flicker 0.16s steps(2, end) 2;
}
@keyframes fritz-flicker {
  0%   { opacity: 1; }
  50%  { opacity: 0.72; }
  100% { opacity: 1; }
}
.fritz-band {
  position: absolute; left: 0; right: 0;
  background: linear-gradient(
    to right,
    rgba(193, 39, 45, 0.5) 0%,
    rgba(228, 211, 166, 0.75) 12%,
    rgba(228, 211, 166, 0.75) 88%,
    rgba(110, 168, 254, 0.45) 100%);
  transform: translateX(var(--shift, 0));
  mix-blend-mode: screen;
}
/* Asked for less motion: the scanlines and the vignette stay, because neither
   moves; the roll bar stops and the bursts are skipped entirely. */
@media (prefers-reduced-motion: reduce) {
  .fritz-roll { animation: none; opacity: 0.35; }
  .fritz.fritz-flicker { animation: none; }
}
</style>
<script id="global-fritz-engine">
(() => {
  if (window.__globalFritzInit) return;
  window.__globalFritzInit = true;
  const box = document.getElementById('global-fritz');
  if (!box) return;

  const reduced = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // A burst is a handful of tear bands plus a whole-panel flicker. The bands
  // are removed on a timer rather than on animationend: an interrupted or
  // never-fired animation would otherwise leave one on screen permanently.
  function burst() {
    box.classList.add('fritz-flicker');
    setTimeout(() => box.classList.remove('fritz-flicker'), 90 + Math.random() * 120);
    const bands = 1 + Math.round(Math.random() * 3);
    for (let i = 0; i < bands; i++) {
      const band = document.createElement('div');
      band.className = 'fritz-band';
      band.style.top = (Math.random() * 100) + '%';
      band.style.height = (2 + Math.random() * 26) + 'px';
      // The fringe is the giveaway that a picture has torn rather than simply
      // dimmed: the band carries a sliver of colour off to one side.
      band.style.setProperty('--shift', (Math.random() * 12 - 6) + 'px');
      band.style.opacity = String(Math.min(1, 0.25 + Math.random() * 0.5));
      box.append(band);
      setTimeout(() => band.remove(), 120 + Math.random() * 260);
    }
  }

  // The wait is randomised around the interval so the fault never falls into a
  // rhythm, and a backgrounded tab is not worth glitching for.
  (function schedule() {
    setTimeout(() => {
      if (!document.hidden && !reduced()) burst();
      schedule();
    }, 7000 * (0.45 + Math.random() * 1.1));
  })();
})();
</script>`
