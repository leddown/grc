package app

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// themeCookieName stores the user's chosen UI theme. It is a plain
// (non-HttpOnly) cookie, set directly by client-side JS in themeToggleTag,
// since it only controls presentation and isn't sensitive.
const themeCookieName = "go_rcsa_theme"

const (
	themeDark   = "dark"
	themeMatrix = "matrix"
	themeChaos  = "chaos"
)

// The palettes and the component layer live in theme_design.go, ported from
// wintermute. What stays here is the machinery: which theme is current, and
// where its style and its background engines are spliced into a page.

const chaosEngineTag = `<script id="global-chaos-engine">
(() => {
  if (window.__globalChaosInit) return;
  window.__globalChaosInit = true;

  const INTERVAL_KEY = "grc-chaos-interval";
  const DENSITY_KEY = "grc-chaos-density";
  // Density is expressed per this many characters, so the effect scales with
  // however much text a page happens to render.
  const DENSITY_BASE = 300;
  const DEFAULT_INTERVAL = 60;
  const DEFAULT_DENSITY = 2;
  const MIN_INTERVAL = 1;
  const MAX_INTERVAL = 3600;

  // Elements whose text must not be split: either it is not rendered as text
  // nodes we can wrap, or wrapping would corrupt the control.
  const SKIP_TAGS = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEXTAREA", "INPUT", "SELECT", "OPTION"]);

  let timer = null;
  let applied = [];

  function clampInt(value, min, max, fallback) {
    const n = parseInt(value, 10);
    if (!Number.isFinite(n)) return fallback;
    return Math.min(max, Math.max(min, n));
  }

  function intervalSeconds() {
    return clampInt(localStorage.getItem(INTERVAL_KEY), MIN_INTERVAL, MAX_INTERVAL, DEFAULT_INTERVAL);
  }

  function density() {
    return clampInt(localStorage.getItem(DENSITY_KEY), 0, DENSITY_BASE, DEFAULT_DENSITY);
  }

  function randomColour() {
    // Full saturation at mid lightness stays legible on the Matrix black.
    return "hsl(" + Math.floor(Math.random() * 360) + ", 100%, 60%)";
  }

  function textNodes() {
    const root = document.body;
    if (!root) return [];
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
      acceptNode(node) {
        if (!node.nodeValue || !/\S/.test(node.nodeValue)) return NodeFilter.FILTER_REJECT;
        const parent = node.parentElement;
        if (!parent || SKIP_TAGS.has(parent.tagName)) return NodeFilter.FILTER_REJECT;
        if (parent.classList.contains("chaos-char")) return NodeFilter.FILTER_REJECT;
        // Bare text inside a flex or grid container is an anonymous item;
        // wrapping one character would promote it to an item of its own and
        // stack it on a separate line, shattering the layout.
        const display = getComputedStyle(parent).display;
        if (display.includes("flex") || display.includes("grid")) return NodeFilter.FILTER_REJECT;
        return NodeFilter.FILTER_ACCEPT;
      },
    });
    const out = [];
    for (let n = walker.nextNode(); n; n = walker.nextNode()) out.push(n);
    return out;
  }

  // candidates lists every recolourable character as a {node, offset} pair.
  // Whitespace is skipped (colouring a space shows nothing), as is either half
  // of a surrogate pair — splitting one apart would corrupt the glyph.
  function candidates(nodes) {
    const out = [];
    for (const node of nodes) {
      const text = node.nodeValue;
      for (let i = 0; i < text.length; i++) {
        const code = text.charCodeAt(i);
        if (code >= 0xd800 && code <= 0xdfff) continue;
        if (!/\S/.test(text[i])) continue;
        out.push({ node, offset: i });
      }
    }
    return out;
  }

  function pick(spots, count) {
    // Partial Fisher-Yates: correct even when count approaches spots.length,
    // unlike rejection sampling.
    const idx = spots.map((_, i) => i);
    for (let i = 0; i < count; i++) {
      const j = i + Math.floor(Math.random() * (idx.length - i));
      const tmp = idx[i];
      idx[i] = idx[j];
      idx[j] = tmp;
    }
    return idx.slice(0, count).map((i) => spots[i]);
  }

  function clear() {
    for (const span of applied) {
      const parent = span.parentNode;
      // A page that reloaded has already discarded its spans.
      if (!parent) continue;
      parent.replaceChild(document.createTextNode(span.textContent), span);
      parent.normalize();
    }
    applied = [];
  }

  function paint(picks) {
    // Offsets shift as soon as a node is split, so group by node and work from
    // the end backwards — the head keeps the offsets still to come.
    const byNode = new Map();
    for (const p of picks) {
      if (!byNode.has(p.node)) byNode.set(p.node, []);
      byNode.get(p.node).push(p.offset);
    }
    for (const [node, offsets] of byNode) {
      offsets.sort((a, b) => b - a);
      for (const offset of offsets) {
        if (!node.parentNode) break;
        const target = node.splitText(offset);
        target.splitText(1);
        const span = document.createElement("span");
        span.className = "chaos-char";
        span.style.color = randomColour();
        target.parentNode.insertBefore(span, target);
        span.appendChild(target);
        applied.push(span);
      }
    }
  }

  function tick() {
    clear();
    const n = density();
    if (n <= 0) return;
    const spots = candidates(textNodes());
    if (!spots.length) return;
    const count = Math.min(spots.length, Math.round((spots.length * n) / DENSITY_BASE));
    if (count < 1) return;
    paint(pick(spots, count));
  }

  // Several pages render their tables client-side after load, so a tick fired
  // the instant the engine starts can find an empty page and glitch nothing
  // until the next interval. Wait briefly for content before the opening
  // glitch; the interval itself runs on schedule regardless.
  function firstTick(attempts) {
    if (!timer) return;
    if (attempts > 0 && !candidates(textNodes()).length) {
      setTimeout(() => firstTick(attempts - 1), 250);
      return;
    }
    tick();
  }

  function start() {
    if (timer) clearInterval(timer);
    timer = setInterval(tick, intervalSeconds() * 1000);
    firstTick(20);
  }

  // Exposed so the cadence can be tuned from the console; there is no settings
  // UI for it. setConfig restarts the timer so a change applies immediately.
  window.GRCChaos = {
    config: () => ({ intervalSeconds: intervalSeconds(), density: density() }),
    setConfig: (next) => {
      localStorage.setItem(INTERVAL_KEY, String(clampInt(next.intervalSeconds, MIN_INTERVAL, MAX_INTERVAL, DEFAULT_INTERVAL)));
      localStorage.setItem(DENSITY_KEY, String(clampInt(next.density, 0, DENSITY_BASE, DEFAULT_DENSITY)));
      start();
    },
    glitch: tick,
    DENSITY_BASE,
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
</script>`

// matrixRainTag ports morpheus's matrix-rain.js: a full-viewport canvas of
// falling katakana behind the page, injected for Matrix and Chaos only.
//
// It exists for a layout reason, not only a decorative one. A control catalog
// or a risk-register detail pane is a finite amount of content, so on a 21:9 or
// 32:9 display there is always screen left over below and beside it. The rain
// gives that space something to be, so the void reads as deliberate rather than
// as a page that ran out.
//
// It stays strictly behind the content, whose panes are opaque under this
// palette: the glyphs are only ever visible where there is nothing to read, and
// nothing has to be dimmed or made translucent to accommodate them.
//
// Adapted for this app's server-rendered multi-page architecture. morpheus is a
// single-page app whose theme.js starts and stops the rain as the theme changes
// at runtime; here a theme switch is a cookie write plus a reload, so the tag is
// simply absent on Dark and Light and the engine starts unconditionally when it
// is present — there is no stop path and no hidden canvas to keep repainting.
// Sizing also comes from the canvas's own box rather than window.innerWidth,
// which includes the scrollbar and would push a fixed, left-anchored canvas past
// the viewport. As in chaosEngineTag, template literals are rewritten as
// concatenation because this source lives inside a Go raw string literal, which
// cannot contain a backtick.
//
// Brightness is read from localStorage on every apply, so it can be changed from
// the browser console and takes effect on the frame already on screen:
//
//	grc-rain-brightness : percentage scaling of the fade (default 100)
const matrixRainTag = `<canvas id="global-matrix-rain" aria-hidden="true"></canvas>
<style id="global-matrix-rain-style">
/* Fixed rather than scrolled: it is the room the app sits in, not part of the
   page. The starting opacity is BASE_OPACITY below, which the engine re-applies
   inline as soon as it runs; change it there, not here.

   width/height are spelled out rather than left to inset: 0, and they carry
   !important. A canvas is a replaced element, so an out-of-flow box with
   auto width and both insets at 0 is not stretched — the insets are treated as
   over-constrained and the box keeps its intrinsic 300x150, which is what this
   rule looked like it was avoiding. Percentages resolve against the viewport
   minus its scrollbars, so the canvas covers exactly the visible area and does
   not itself become something to scroll. The !important is for the responsive
   layer's "canvas { height: auto }", which is meant for inline media. */
#global-matrix-rain {
  position: fixed;
  top: 0;
  left: 0;
  width: 100% !important;
  height: 100% !important;
  max-width: none !important;
  z-index: 0;
  pointer-events: none;
  opacity: 0.18;
}
/* The page rides above the rain. Every page in this app wraps its content in a
   single <main>, including the ones with no sidebar, so raising that one element
   covers all of them. Positioning is required: the canvas is a positioned
   z-index 0 element, and an unpositioned <main> would paint underneath it,
   taking every pane's background with it.

   !important because the sidebar shell resets this same element with
   "main:has(> .global-shell) { all: revert }", which is more specific and
   injected later in the cascade. The fixed sidebar and the fixed AI dock are
   unaffected: a relatively positioned ancestor is not a containing block for
   fixed descendants, and the dock and toggle sit outside <main> entirely, so
   they keep painting above it exactly as before. */
main {
  position: relative !important;
  z-index: 1 !important;
}
</style>
<script id="global-matrix-rain-engine">
(() => {
  if (window.__globalMatrixRainInit) return;
  window.__globalMatrixRainInit = true;

  const canvas = document.getElementById("global-matrix-rain");
  if (!canvas || !canvas.getContext) return;

  // A glyph cell. Coarse on purpose — this is texture at low opacity, and a
  // finer grid would cost real work every frame to render detail nobody can
  // resolve.
  const CELL = 16;

  const BRIGHTNESS_KEY = "grc-rain-brightness";

  // The opacity the rain is faded to at 100%: faint enough to sit behind the
  // panes without competing with them.
  const BASE_OPACITY = 0.18;

  // Brightness is a percentage scaling that fade, so 100 is the weight the
  // theme was designed at and 10 is nearly invisible. The ceiling is 500
  // because that is where the fade reaches 0.9 — beyond it the glyphs are
  // effectively opaque and the control would have nothing left to move.
  const DEFAULT_BRIGHTNESS = 100;
  const MIN_BRIGHTNESS = 10;
  const MAX_BRIGHTNESS = 500;

  // Redraws per second. The rain reads as rain at 12; at 60 it costs five times
  // as much to look almost identical, on a machine whose actual job is rendering
  // catalog tables and talking to an API.
  const FPS = 12;

  // How much of the previous frame is painted over each tick. Lower leaves
  // longer, brighter trails.
  const TRAIL_FADE = 0.08;

  // Katakana, the glyphs the film uses, plus digits — a set with enough shapes
  // that a column never looks like it is repeating.
  const GLYPHS = "アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン0123456789";

  let ctx = null;
  let columns = [];
  let viewWidth = 0;
  let viewHeight = 0;
  let timer = null;

  function reduceMotion() {
    return window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  }

  function brightness() {
    const n = parseInt(localStorage.getItem(BRIGHTNESS_KEY), 10);
    if (!Number.isFinite(n)) return DEFAULT_BRIGHTNESS;
    return Math.min(MAX_BRIGHTNESS, Math.max(MIN_BRIGHTNESS, n));
  }

  // applyBrightness sets the canvas's opacity from the current setting.
  //
  // The opacity is what the brightness control has to move. The glyphs are
  // painted at the palette's own weights and then the whole canvas is faded to
  // BASE_OPACITY so it stays behind the panes — which means the alpha a glyph is
  // drawn at is multiplied by 0.18 before it reaches the screen, and no amount of
  // scaling inside the canvas can get past that. Scaling the fade itself is the
  // only lever with real range. Clamped at 1 because opacity saturates there;
  // MAX_BRIGHTNESS is set so the top of the range lands just under it rather than
  // in a dead zone.
  function applyBrightness() {
    canvas.style.opacity = String(Math.min(1, BASE_OPACITY * (brightness() / 100)));
  }

  function glyph() {
    return GLYPHS[Math.floor(Math.random() * GLYPHS.length)];
  }

  // resize rebuilds the column heads for the current window. Each column starts
  // at a random height so the rain is already falling when it appears rather
  // than beginning as one flat line across the top.
  //
  // The size comes from the canvas's own laid-out box, not window.innerWidth:
  // innerWidth counts the scrollbar, and a fixed element sized to it would sit
  // partly off the right edge of the viewport and add horizontal scroll.
  function resize() {
    viewWidth = canvas.clientWidth;
    viewHeight = canvas.clientHeight;
    if (viewWidth <= 0 || viewHeight <= 0) return false;

    const dpr = Math.min(window.devicePixelRatio || 1, 2); // 2 is plenty for glyphs this size
    canvas.width = Math.floor(viewWidth * dpr);
    canvas.height = Math.floor(viewHeight * dpr);

    ctx = canvas.getContext("2d");
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.font = CELL + 'px "Courier New", Courier, monospace';
    ctx.textBaseline = "top";

    const count = Math.ceil(viewWidth / CELL);
    columns = new Array(count);
    for (let i = 0; i < count; i++) {
      columns[i] = Math.random() * (viewHeight / CELL);
    }
    ctx.fillStyle = "#000";
    ctx.fillRect(0, 0, viewWidth, viewHeight);
    return true;
  }

  function tick() {
    if (!ctx) return;

    // Fade rather than clear: what is left of the previous frames is the trail
    // behind each falling head.
    ctx.fillStyle = "rgba(0, 0, 0, " + TRAIL_FADE + ")";
    ctx.fillRect(0, 0, viewWidth, viewHeight);

    for (let i = 0; i < columns.length; i++) {
      const x = i * CELL;
      const y = columns[i] * CELL;

      // The head is brighter than its trail, which is what makes a column read
      // as falling rather than as a static string of characters.
      ctx.fillStyle = "rgba(140, 255, 170, 0.85)";
      ctx.fillText(glyph(), x, y);
      ctx.fillStyle = "rgba(0, 255, 65, 0.55)";
      ctx.fillText(glyph(), x, y - CELL);

      // Past the bottom, restart high up — but only sometimes, so the columns
      // drift out of step instead of marching in rows.
      if (y > viewHeight && Math.random() > 0.975) {
        columns[i] = 0;
      } else {
        columns[i] += 1;
      }
    }
  }

  // paintStill draws a single frame of scattered glyph runs. This is what
  // prefers-reduced-motion gets: the same texture at the same weight, holding
  // still. The preference is about movement, not contrast — dimming it to
  // nothing would take the theme away rather than just the animation.
  function paintStill() {
    if (!ctx) return;
    ctx.fillStyle = "#000";
    ctx.fillRect(0, 0, viewWidth, viewHeight);
    const rows = viewHeight / CELL;
    for (let i = 0; i < columns.length; i++) {
      // Runs of varying length at varying heights, so the field reads as rain
      // caught mid-fall rather than as an even wash of characters.
      const runLength = 4 + Math.floor(Math.random() * 11);
      const top = Math.random() * rows;
      for (let j = 0; j < runLength; j++) {
        // Each run fades along its length, the way a trail does.
        ctx.fillStyle = "rgba(0, 255, 65, " + (0.75 - (j / runLength) * 0.5) + ")";
        ctx.fillText(glyph(), i * CELL, (top + j) * CELL);
      }
    }
  }

  function startTimer() {
    stopTimer();
    timer = setInterval(tick, 1000 / FPS);
  }

  function stopTimer() {
    if (timer) clearInterval(timer);
    timer = null;
  }

  function paint() {
    if (reduceMotion()) {
      stopTimer();
      paintStill();
      return;
    }
    startTimer();
  }

  function start() {
    if (!resize()) return;
    applyBrightness();
    paint();

    window.addEventListener("resize", () => {
      if (resize()) paint();
    });

    // A backgrounded tab has nothing to animate for, and browsers throttle the
    // timer unevenly, which makes the rain lurch on return.
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) {
        stopTimer();
      } else if (!reduceMotion()) {
        startTimer();
      }
    });
  }

  // Exposed so the brightness can be tuned from the console; there is no
  // settings UI for it, matching GRCChaos. The fade is a property of the
  // canvas element rather than of the pixels in it, so a change takes effect on
  // the frame already on screen — there is nothing to repaint and no wait for
  // the trails to turn over.
  window.GRCRain = {
    config: () => ({ brightness: brightness() }),
    setConfig: (next) => {
      const n = parseInt(next.brightness, 10);
      localStorage.setItem(
        BRIGHTNESS_KEY,
        String(Number.isFinite(n)
          ? Math.min(MAX_BRIGHTNESS, Math.max(MIN_BRIGHTNESS, n))
          : DEFAULT_BRIGHTNESS),
      );
      applyBrightness();
    },
    MIN_BRIGHTNESS,
    MAX_BRIGHTNESS,
    DEFAULT_BRIGHTNESS,
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
</script>`

// mobileStyleTag is the shared responsive/accessibility layer. It is injected
// after the palette so it can override the themes' !important declarations, and
// it is deliberately palette-agnostic: everything here is layout, sizing, touch
// and focus behaviour that every theme needs identically.
//
// Targeted at iOS Safari specifically (per product decision — Android is not a
// supported client), which drives three choices that look odd otherwise: the
// 16px form-control floor, env(safe-area-inset-*) padding, and dvh units.
const mobileStyleTag = `<style id="global-mobile-style">
html {
  /* iOS inflates text in landscape unless this is pinned. */
  -webkit-text-size-adjust: 100%;
  text-size-adjust: 100%;
}
body {
  /* The AI panel slides in from the right rather than sitting along the
     bottom, so the page keeps its full height; only the home-indicator inset
     is reserved. */
  padding-bottom: env(safe-area-inset-bottom) !important;
  padding-left: env(safe-area-inset-left);
  padding-right: env(safe-area-inset-right);
  overflow-x: hidden;
}
/* iOS zooms the viewport whenever a focused control renders below 16px, and
   never zooms back out. This is the single biggest phone usability bug here. */
input, select, textarea {
  font-size: max(16px, 1em);
}
a, button, [role="button"], summary, label {
  /* Drops the legacy 300ms double-tap-to-zoom delay on taps. */
  touch-action: manipulation;
}
a, button, [role="button"] {
  -webkit-tap-highlight-color: rgba(127, 127, 127, 0.25);
}
/* One visible, palette-independent focus ring for keyboard and switch-control
   users. Several pages build clickable rows out of divs, so [role=button] and
   [tabindex] have to be covered as well as the real interactive elements. */
a:focus-visible,
button:focus-visible,
input:focus-visible,
select:focus-visible,
textarea:focus-visible,
[role="button"]:focus-visible,
[tabindex]:focus-visible {
  outline: 3px solid currentColor;
  outline-offset: 2px;
  border-radius: 6px;
}
/* Touch scrolling momentum + no scroll chaining out of inner panes. */
.rows, .table-wrap, pre, .list, .detail-body, .panel-body {
  -webkit-overflow-scrolling: touch;
  overscroll-behavior: contain;
}
.table-wrap {
  max-width: 100%;
  overflow-x: auto;
}
img, svg, video, canvas {
  max-width: 100%;
  height: auto;
}
pre, code {
  overflow-wrap: anywhere;
}
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
    scroll-behavior: auto !important;
  }
}
@media (max-width: 900px) {
  main {
    max-width: 100% !important;
    margin: 10px !important;
    padding: 14px !important;
    border-radius: 14px !important;
  }
  h1 { overflow-wrap: break-word; }
  /* Filter/toolbar rows are laid out as fixed multi-column grids on nearly
     every page; on a phone they have to stack or they overflow the viewport.
     Deliberately not !important: this is a fallback, and a page that knows a
     better phone layout for its own toolbar should be able to say so with a
     more specific selector. */
  .toolbar, .filters, .controls-bar {
    grid-template-columns: 1fr;
  }
  .tab, .button, button[type="submit"] {
    min-height: 40px;
  }
}
@media (pointer: coarse) {
  /* WCAG 2.2 target size, applied to the small inline actions that would
     otherwise be a few pixels tall. */
  .row-open-link, .detail-open-link, .chip, .subchip, .mono-link {
    min-height: 32px;
    display: inline-flex;
    align-items: center;
  }
}
</style>`

// sideNavTag renders the one shared sidebar shell every page gets: a fixed,
// independently-scrolling left sidebar on desktop (collapsible, state
// persisted in localStorage) that becomes an off-canvas drawer on phone
// widths (opened by the same toggle button, never persisted — a drawer left
// open across a full page navigation reads as broken, not as a preference).
//
// This replaced a horizontal top bar that just wrapped nav.tabs onto three
// rows of pills above the page content, and a separate hand-built sidebar
// that only internal/app/home.go had (see homePage) — every other page got
// the top bar. Consolidating onto one implementation here means every page,
// including Home, now shares the same nav chrome and the same mobile
// handling instead of home.go duplicating and diverging from it.
//
// The sidebar carries one section at a time. Its top level — Catalog,
// Compliance & Risk, Reporting & Data, Documents, Admin — is a switcher in a
// fixed topbar, which is wintermute's shape: .topbar holds the view buttons,
// the sidebar holds the tabs of the view you are in. Stacking all five groups
// and their thirty-odd items in one column meant the section you were working
// in was a sixth of a scrolling list, and the group heads were doing the work
// of navigation while looking like captions. Switching a section is a DOM
// swap, not a page load: the pages are separate documents here, so the bar
// re-derives which section you are in from the active tab on every load.
const sideNavTag = `<style id="global-side-nav-style">
/* Every page's own <main> still carries whatever box-model CSS it defined
   before this sidebar existed: a max-width, centering auto-margins, its own
   padding, border and shadow — styling that assumed main filled the near-
   full viewport width. The script below nests .global-shell inside that
   same <main> rather than replacing it, so that old box model kept applying
   around the new shell too, producing a stray, off-center dead zone next to
   the fixed sidebar on any page whose max-width didn't happen to match the
   viewport. Resetting it once here, for any <main> we've actually wrapped,
   is simpler and more reliable than hunting down and editing the same rule
   across every page handler — the visible box now comes entirely from
   .global-shell's own children.
   :has() requires Chrome 105+/Safari 15.4+/Firefox 121+; this app doesn't
   support older browsers elsewhere (backdrop-filter, dvh units, env()). */
main:has(> .global-shell) {
  all: revert;
  display: block !important;
}
.global-shell {
  min-height: 100vh;
  --global-topbar-h: 52px;
}
/* The topbar is fixed to the window rather than placed in the shell: the
   sidebar and the AI dock are already fixed, and a bar that scrolls away
   takes the section switcher with it. Its right padding is the room the
   floating theme and brightness controls occupy — they sit on the bar rather
   than over it. */
.global-topbar {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 52px;
  z-index: 9700;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 max(210px, calc(12px + env(safe-area-inset-right))) 0 12px;
  background: var(--panel);
  border-bottom: 1px solid var(--line);
}
.global-brand-link {
  flex: none;
  font: 700 15px ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif !important;
  color: var(--ink) !important;
  text-decoration: none !important;
  padding: 6px 8px !important;
  text-transform: none !important;
  letter-spacing: 0 !important;
  background: none !important;
  border: none !important;
}
.global-views {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none;
}
.global-views::-webkit-scrollbar { display: none; }
.global-view-btn {
  flex: none;
  padding: 6px 10px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--muted);
  font: 500 13px ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  cursor: pointer;
  white-space: nowrap;
}
.global-view-btn:hover { background: var(--hover); color: var(--ink); }
/* A single-page section is a link, and the palette layer colours every link
   with the accent — which would make one entry in the bar blue and the rest
   not. It is a section entry first. */
a.global-view-btn {
  display: inline-flex;
  align-items: center;
  text-decoration: none;
  color: var(--muted) !important;
}
a.global-view-btn:hover, a.global-view-btn.active { color: var(--ink) !important; }
.global-view-btn.active {
  background: var(--surface-strong);
  border-color: var(--line);
  color: var(--ink);
}
/* Admin is operational rather than day-to-day, so it sits apart at the end of
   the bar instead of competing at the same weight — what the dashed rule in
   the sidebar used to say when every group was stacked in one column. */
.global-view-btn.global-view-admin { margin-left: auto; opacity: 0.75; }
.global-view-btn.global-view-admin:hover { opacity: 1; }
.global-sidenav {
  position: fixed;
  top: var(--global-topbar-h, 52px);
  left: 0;
  /* The AI panel is on the right now, so the sidebar runs the full height. */
  bottom: 0;
  width: 260px;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior: contain;
  background: var(--panel);
  border-right: 1px solid var(--line);
  padding: 14px 12px 20px;
  z-index: 9500;
  transform: translateX(0);
  transition: transform 0.2s ease;
}
.global-content {
  margin-left: 260px;
  padding: calc(var(--global-topbar-h, 52px) + 20px) 24px 20px;
  min-width: 0;
  transition: margin-left 0.2s ease;
}
.global-content > * {
  min-width: 0;
}
.global-content .table-wrap {
  max-width: 100%;
  overflow: auto;
}
.global-content table {
  max-width: 100%;
}
.global-sidenav-backdrop {
  display: none;
}
/* Desktop collapse: user-toggled via #global-nav-toggle, persisted. */
.global-shell.nav-collapsed .global-sidenav { transform: translateX(-100%); }
.global-shell.nav-collapsed .global-content { margin-left: 0; }

.global-sidenav nav.tabs {
  display: flex !important;
  flex-direction: column !important;
  gap: 4px !important;
  margin: 0 !important;
}
.global-sidenav .tab {
  display: block !important;
  padding: 8px 10px !important;
  border-radius: 8px !important;
  font-size: 13px !important;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-transform: none !important;
  letter-spacing: 0 !important;
}
.global-sidenav .tab-home {
  font-weight: 700 !important;
  margin-bottom: 12px !important;
}
.global-nav-search-trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  margin-bottom: 14px;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid var(--line);
  background: var(--surface-strong);
  color: var(--muted);
  font: 13px Arial, sans-serif;
  cursor: pointer;
  text-align: left;
}
.global-nav-search-trigger:hover {
  background: var(--hover);
  color: var(--ink);
}
.global-nav-search-trigger kbd {
  font: 11px Arial, sans-serif;
  padding: 2px 6px;
  border-radius: 5px;
  border: 1px solid var(--line);
  background: var(--panel);
  color: var(--muted);
}
/* One group is shown at a time; the topbar says which. The hidden ones stay
   in the DOM so switching sections is a class toggle rather than a fetch. */
.global-sidenav .tab-group { margin-top: 0; }
.global-sidenav .tab-group[hidden] { display: none; }
.global-sidenav .tab-group-label {
  margin: 0 0 8px;
  padding: 0 10px;
  font: 600 11px ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  text-transform: uppercase;
  letter-spacing: 0.07em;
  color: var(--muted);
}

/* Lives in the topbar now, where wintermute's ☰ is, rather than floating over
   the page at the same corner the bar occupies. */
#global-nav-toggle {
  flex: none;
  min-height: 36px;
  min-width: 36px;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid var(--line);
  background: var(--surface-strong);
  color: var(--ink);
  font: 15px Arial, sans-serif;
  cursor: pointer;
  touch-action: manipulation;
}
#global-nav-toggle:hover {
  background: var(--hover);
}

@media (max-width: 900px) {
  /* Off-canvas by default; two dozen-plus items as a horizontal-scroll wall
     of pills ate the whole screen before, so this is a drawer instead. */
  .global-sidenav {
    transform: translateX(-100%);
    width: min(82vw, 300px);
    box-shadow: 0 0 40px rgba(0, 0, 0, 0.4);
  }
  .global-shell.nav-collapsed .global-sidenav {
    transform: translateX(-100%);
  }
  .global-shell.nav-open .global-sidenav {
    transform: translateX(0);
  }
  .global-shell.nav-open .global-sidenav-backdrop {
    display: block;
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    z-index: 9400;
  }
  .global-content {
    margin-left: 0 !important;
    padding: calc(var(--global-topbar-h, 52px) + 14px) 14px 20px;
  }
  .global-sidenav .tab {
    min-height: 40px;
    display: flex !important;
    align-items: center;
  }
  /* Less room reserved for the floating theme controls than on desktop —
     they shrink to icons-and-a-word at this width — and the switcher scrolls
     sideways under what is left. */
  .global-topbar {
    padding-right: max(165px, calc(8px + env(safe-area-inset-right)));
    gap: 6px;
  }
  .global-brand-link { font-size: 14px !important; padding: 6px 4px !important; }
}
</style>
<script>
(() => {
  if (window.__globalSideNavInit) return;
  window.__globalSideNavInit = true;

  const navs = Array.from(document.querySelectorAll("nav.tabs"));
  for (const nav of navs) {
    if (!nav || nav.closest(".global-sidenav")) continue;
    const main = nav.closest("main");
    if (!main || main.dataset.globalSideReady === "1") continue;

    const existingNodes = Array.from(main.childNodes);
    const pageShell = document.createElement("div");
    pageShell.className = "global-shell";
    pageShell.id = "globalShell";

    const backdrop = document.createElement("div");
    backdrop.className = "global-sidenav-backdrop";

    const aside = document.createElement("aside");
    aside.className = "global-sidenav";
    const homeTab = nav.querySelector("a.tab:first-child");
    if (homeTab) homeTab.classList.add("tab-home");

    const searchTrigger = document.createElement("button");
    searchTrigger.type = "button";
    searchTrigger.className = "global-nav-search-trigger";
    searchTrigger.innerHTML = '<span>Search pages…</span><kbd>Ctrl K</kbd>';
    searchTrigger.addEventListener("click", () => window.__globalOpenCommandPalette && window.__globalOpenCommandPalette());
    aside.appendChild(searchTrigger);

    aside.appendChild(nav);

    // The top level of the sidebar becomes the topbar switcher: one button per
    // group, and the sidebar then shows that group alone. Home is the brand
    // link at the left rather than a tab, which is where it was being read as
    // anyway.
    const topbar = document.createElement("header");
    topbar.className = "global-topbar";
    topbar.id = "globalTopbar";

    if (homeTab) {
      homeTab.classList.add("global-brand-link");
      homeTab.textContent = "GRC";
      homeTab.title = "Home";
      topbar.appendChild(homeTab);
    }

    const views = document.createElement("nav");
    views.className = "global-views";
    views.setAttribute("aria-label", "Sections");
    const groups = Array.from(nav.querySelectorAll(".tab-group"));
    const viewButtons = groups.map((group, index) => {
      const labelEl = group.querySelector(".tab-group-label");
      const tabs = Array.from(group.querySelectorAll("a.tab"));
      // A section holding one page is that page: switching to it and then
      // clicking its only tab is two clicks for one destination, so the bar
      // links straight there. AI Chat is the case this exists for.
      const single = tabs.length === 1 ? tabs[0] : null;
      const button = document.createElement(single ? "a" : "button");
      if (single) {
        button.href = single.getAttribute("href");
        button.classList.add("global-view-link");
      } else {
        button.type = "button";
        button.setAttribute("aria-pressed", "false");
        button.addEventListener("click", () => showGroup(index));
      }
      button.classList.add("global-view-btn");
      if (group.classList.contains("tab-group-admin")) {
        button.classList.add("global-view-admin");
      }
      button.textContent = labelEl ? labelEl.textContent.trim() : "Section " + (index + 1);
      views.appendChild(button);
      return button;
    });
    topbar.appendChild(views);
    document.body.appendChild(topbar);

    function showGroup(index) {
      groups.forEach((group, i) => { group.hidden = i !== index; });
      viewButtons.forEach((button, i) => {
        button.classList.toggle("active", i === index);
        if (button.tagName === "BUTTON") {
          button.setAttribute("aria-pressed", i === index ? "true" : "false");
        } else if (i === index) {
          button.setAttribute("aria-current", "page");
        } else {
          button.removeAttribute("aria-current");
        }
      });
    }

    // Which section a page belongs to is answered by the tab it marks active,
    // so a link followed from anywhere lands with its own section open. Home
    // marks no tab; it opens on the first section rather than on nothing.
    const activeIndex = groups.findIndex((group) => group.querySelector("a.tab.active"));
    showGroup(activeIndex >= 0 ? activeIndex : 0);

    const content = document.createElement("section");
    content.className = "global-content";
    for (const node of existingNodes) {
      if (node === nav) continue;
      content.appendChild(node);
    }

    pageShell.appendChild(backdrop);
    pageShell.appendChild(aside);
    pageShell.appendChild(content);

    main.innerHTML = "";
    main.appendChild(pageShell);
    main.dataset.globalSideReady = "1";
  }

  // Wide tables are the main cause of horizontal page scroll on a phone.
  // Pages that already opted into .table-wrap are left alone.
  for (const table of Array.from(document.querySelectorAll("main table"))) {
    if (table.closest(".table-wrap")) continue;
    const wrap = document.createElement("div");
    wrap.className = "table-wrap";
    table.parentNode.insertBefore(wrap, table);
    wrap.appendChild(table);
  }

  const shell = document.getElementById("globalShell");
  if (!shell) return;

  const COLLAPSE_KEY = "grc-nav-collapsed";
  const isMobile = () => window.matchMedia("(max-width: 900px)").matches;

  if (!isMobile() && localStorage.getItem(COLLAPSE_KEY) === "1") {
    shell.classList.add("nav-collapsed");
  }

  const toggle = document.createElement("button");
  toggle.type = "button";
  toggle.id = "global-nav-toggle";
  toggle.setAttribute("aria-label", "Toggle navigation");
  toggle.title = "Toggle navigation";
  toggle.textContent = "☰";
  const topbar = document.getElementById("globalTopbar");
  if (topbar) {
    topbar.insertBefore(toggle, topbar.firstChild);
  } else {
    document.body.appendChild(toggle);
  }

  toggle.addEventListener("click", () => {
    if (isMobile()) {
      shell.classList.toggle("nav-open");
    } else {
      const collapsed = shell.classList.toggle("nav-collapsed");
      localStorage.setItem(COLLAPSE_KEY, collapsed ? "1" : "0");
    }
  });

  document.querySelector(".global-sidenav-backdrop").addEventListener("click", () => {
    shell.classList.remove("nav-open");
  });

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") shell.classList.remove("nav-open");
  });

  // A drawer left open across a resize to desktop width would otherwise
  // still be occupying its off-canvas transform once the media query stops
  // applying, which reads as a stuck sidebar rather than a closed one.
  window.addEventListener("resize", () => {
    if (!isMobile()) shell.classList.remove("nav-open");
  });
})();
</script>`

// commandPaletteTag is a Ctrl+K / Cmd+K fuzzy-jump palette over the same
// destinations the sidebar renders. It deliberately does not carry its own
// copy of the nav data — it reads the <a class="tab"> elements already
// rendered inside .global-sidenav at open time, so it can never drift out of
// sync with pageui.Nav the way the four separate legacy tab lists once did.
const commandPaletteTag = `<div id="global-command-palette" role="dialog" aria-modal="true" aria-label="Search pages" hidden>
  <div id="global-command-palette-backdrop"></div>
  <div id="global-command-palette-box">
    <input id="global-command-palette-input" type="text" placeholder="Search pages…" autocomplete="off" aria-label="Search pages">
    <ul id="global-command-palette-results"></ul>
  </div>
</div>
<style id="global-command-palette-style">
#global-command-palette[hidden] { display: none; }
#global-command-palette {
  position: fixed;
  inset: 0;
  z-index: 20000;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 12vh 16px 16px;
}
#global-command-palette-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
}
#global-command-palette-box {
  position: relative;
  width: 100%;
  max-width: 560px;
  max-height: 70vh;
  display: flex;
  flex-direction: column;
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: var(--radius, 10px);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.35);
  overflow: hidden;
}
#global-command-palette-input {
  border: 0;
  border-bottom: 1px solid var(--line);
  background: transparent;
  color: var(--ink);
  padding: 16px;
  font: 16px Arial, sans-serif;
  outline: none;
}
#global-command-palette-results {
  list-style: none;
  margin: 0;
  padding: 6px;
  overflow-y: auto;
}
#global-command-palette-results li {
  display: block;
}
/* A grid rather than a flex row: the group caption varies from "ADMIN" to
   "COMPLIANCE & RISK", and inline that ragged width pushed every page name to
   a different indent, so the list could not be read down its left edge. The
   caption gets a fixed column and the names line up. */
#global-command-palette-results a {
  display: grid;
  grid-template-columns: 156px minmax(0, 1fr);
  align-items: baseline;
  gap: 10px;
  padding: 9px 12px;
  border-radius: 8px;
  text-decoration: none;
  color: var(--ink) !important;
  font-size: 14px;
}
#global-command-palette-results a .cp-group {
  font: 600 11px ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  text-transform: uppercase;
  letter-spacing: 0.07em;
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
#global-command-palette-results li.cp-active a,
#global-command-palette-results a:hover {
  background: var(--hover);
}
#global-command-palette-empty {
  padding: 16px;
  color: var(--muted);
  font: 14px Arial, sans-serif;
}
</style>
<script>
(() => {
  if (window.__globalCommandPaletteInit) return;
  window.__globalCommandPaletteInit = true;

  const palette = document.getElementById("global-command-palette");
  const input = document.getElementById("global-command-palette-input");
  const results = document.getElementById("global-command-palette-results");
  const backdrop = document.getElementById("global-command-palette-backdrop");
  if (!palette || !input || !results || !backdrop) return;

  let items = [];
  let activeIndex = -1;
  let lastFocused = null;

  function collectItems() {
    return Array.from(document.querySelectorAll(".global-sidenav nav.tabs a.tab")).map((a) => ({
      href: a.getAttribute("href") || "",
      label: (a.textContent || "").trim(),
      group: (a.closest(".tab-group")?.querySelector(".tab-group-label")?.textContent || "").trim(),
    }));
  }

  function score(item, query) {
    const label = item.label.toLowerCase();
    if (label === query) return 0;
    if (label.startsWith(query)) return 1;
    if (label.includes(query)) return 2;
    if (item.href.toLowerCase().includes(query)) return 3;
    return -1;
  }

  function render(query) {
    const q = query.trim().toLowerCase();
    const matches = (q === "" ? items.map((item) => ({ item, s: 0 })) : items
      .map((item) => ({ item, s: score(item, q) }))
      .filter((m) => m.s >= 0))
      .sort((a, b) => a.s - b.s || a.item.label.localeCompare(b.item.label))
      .slice(0, 30)
      .map((m) => m.item);

    results.innerHTML = "";
    if (!matches.length) {
      const empty = document.createElement("div");
      empty.id = "global-command-palette-empty";
      empty.textContent = "No matching page.";
      results.appendChild(empty);
      activeIndex = -1;
      return;
    }

    matches.forEach((item, i) => {
      const li = document.createElement("li");
      const a = document.createElement("a");
      a.href = item.href;
      const group = document.createElement("span");
      group.className = "cp-group";
      group.textContent = item.group;
      const label = document.createElement("span");
      label.textContent = item.label;
      a.appendChild(group);
      a.appendChild(label);
      li.appendChild(a);
      li.addEventListener("mouseenter", () => setActive(i));
      results.appendChild(li);
    });
    activeIndex = 0;
    highlight();
  }

  function highlight() {
    Array.from(results.children).forEach((li, i) => li.classList.toggle("cp-active", i === activeIndex));
    const active = results.children[activeIndex];
    if (active) active.scrollIntoView({ block: "nearest" });
  }

  function setActive(i) {
    activeIndex = i;
    highlight();
  }

  function open() {
    items = collectItems();
    lastFocused = document.activeElement;
    palette.hidden = false;
    input.value = "";
    render("");
    input.focus();
  }

  function close() {
    palette.hidden = true;
    if (lastFocused && lastFocused.focus) lastFocused.focus();
  }

  window.__globalOpenCommandPalette = open;

  document.addEventListener("keydown", (event) => {
    const key = event.key.toLowerCase();
    if ((event.ctrlKey || event.metaKey) && key === "k") {
      event.preventDefault();
      if (palette.hidden) open(); else close();
      return;
    }
    if (palette.hidden) return;
    if (key === "escape") {
      event.preventDefault();
      close();
    } else if (key === "arrowdown") {
      event.preventDefault();
      if (results.children.length) setActive((activeIndex + 1) % results.children.length);
    } else if (key === "arrowup") {
      event.preventDefault();
      if (results.children.length) setActive((activeIndex - 1 + results.children.length) % results.children.length);
    } else if (key === "enter") {
      const link = results.children[activeIndex]?.querySelector("a");
      if (link) {
        event.preventDefault();
        window.location.href = link.getAttribute("href");
      }
    }
  });

  backdrop.addEventListener("click", close);
  input.addEventListener("input", () => render(input.value));
})();
</script>`

const aiQuickPromptDockTag = `<button id="global-ai-dock-toggle" type="button" aria-controls="global-ai-dock" aria-expanded="false" aria-label="Open the AI panel">
  <span aria-hidden="true">&#10022;</span><span id="global-ai-dock-toggle-label">Ask AI</span>
</button>
<aside id="global-ai-dock" class="dock" aria-label="Ask AI" hidden>
  <div id="global-ai-dock-head">
    <strong>Ask AI</strong>
    <span id="global-ai-dock-who"></span>
    <a href="/ai-chat" target="_blank" rel="noopener noreferrer">Full chat &#8599;</a>
    <button id="global-ai-dock-clear" type="button">Clear</button>
    <button id="global-ai-dock-close" type="button" aria-label="Close the AI panel">Close</button>
  </div>
  <div id="global-ai-dock-log" role="log" aria-live="polite"></div>
  <form id="global-ai-dock-form">
    <input id="global-ai-dock-input" type="text" placeholder="Ask AI" aria-label="Ask AI" autocomplete="off" autocapitalize="sentences" enterkeyhint="send">
    <button type="submit">Ask</button>
  </form>
</aside>
<style id="global-ai-dock-style">
/* The panel slides in from the right edge rather than sitting along the bottom.
   A question asked here is asked *while* working on a page — the page has to
   stay legible beside it, which a full-width strip across the bottom of the
   viewport does not allow. Ported from wintermute's chat dock.

   Below the 40K theme's fritz overlay (z-index 40) on purpose, so the
   scanlines run across it like everything else. */
#global-ai-dock-toggle {
  position: fixed; right: 0; top: 50%; transform: translateY(-50%);
  z-index: 9600;
  display: flex; flex-direction: column; align-items: center; gap: 6px;
  padding: 12px 10px;
  border: 1px solid var(--line); border-right: none;
  border-radius: 10px 0 0 10px;
  background: var(--surface-strong); color: var(--ink);
  font: 12px Arial, sans-serif;
  cursor: pointer;
  touch-action: manipulation;
}
#global-ai-dock-toggle:hover { border-color: var(--accent); color: var(--accent); }
#global-ai-dock-toggle-label { writing-mode: vertical-rl; letter-spacing: 0.08em; }
/* Open, the handle would sit under the panel; it is the one control the panel
   already carries its own version of. */
#global-ai-dock-toggle[aria-expanded="true"] { display: none; }

#global-ai-dock[hidden] { display: none; }
/* Starts below the floating theme and brightness controls rather than at the
   ceiling: the point of the panel is to ask something *while* working on a
   page, which means the controls pinned above it have to stay reachable with
   it open. Same reason wintermute's dock starts below its topbar. */
#global-ai-dock {
  position: fixed; top: calc(56px + env(safe-area-inset-top)); right: 0; bottom: 0;
  z-index: 9601;
  width: min(460px, 100%);
  display: flex;
  flex-direction: column;
  background: var(--panel);
  border-left: 1px solid var(--line);
  box-shadow: -18px 0 40px rgba(0, 0, 0, 0.45);
  transform: translateX(100%);
  transition: transform 0.18s ease;
}
#global-ai-dock.open { transform: none; }
@media (prefers-reduced-motion: reduce) {
  #global-ai-dock { transition: none; }
}
#global-ai-dock-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--line);
  font: 12px Arial, sans-serif;
  color: var(--muted);
}
#global-ai-dock-head strong { font-size: 13px; color: var(--ink); }
/* What is actually answering takes the slack, so Close stays pinned right. */
#global-ai-dock-who {
  flex: 1;
  min-width: 0;
  font-size: 12px;
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
#global-ai-dock-head a,
#global-ai-dock-head button {
  font: 12px Arial, sans-serif;
  color: var(--muted);
  background: none;
  border: none;
  padding: 0;
  text-decoration: none;
  cursor: pointer;
}
#global-ai-dock-head a:hover,
#global-ai-dock-head button:hover { color: var(--ink); }
#global-ai-dock-log {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  font: 14px/1.55 ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
}
.global-ai-dock-msg {
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 8px 12px;
  max-width: 92%;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--ink);
  background: var(--surface-strong);
}
.global-ai-dock-msg.user { align-self: flex-end; border-left: 3px solid var(--accent); }
.global-ai-dock-msg.ai { align-self: flex-start; border-left: 3px solid var(--good); }
.global-ai-dock-msg.note { align-self: stretch; border-style: dashed; color: var(--muted); }
.global-ai-dock-msg .role {
  display: block;
  margin-bottom: 4px;
  font-size: 11px;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--muted);
}
#global-ai-dock-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  padding: 12px 14px calc(12px + env(safe-area-inset-bottom));
  border-top: 1px solid var(--line);
}
#global-ai-dock-input {
  width: 100%;
  padding: 10px 12px;
  border-radius: 8px;
  border: 1px solid var(--line);
  background: var(--bg);
  color: var(--ink);
  /* 16px, not 14px: iOS Safari zooms the whole page when a focused field is
     smaller than that, and the panel is fixed so the zoom never scrolls back. */
  font: 16px Arial, sans-serif;
}
#global-ai-dock-input:focus { border-color: var(--accent); outline: none; }
#global-ai-dock-form button {
  min-height: 40px;
  padding: 10px 14px;
  border-radius: 8px;
  border: 1px solid transparent;
  background: var(--accent);
  color: var(--accent-contrast);
  font: 600 14px Arial, sans-serif;
  cursor: pointer;
}
#global-ai-dock-form button[disabled] {
  opacity: 0.6;
  cursor: progress;
}
/* At the width where the sidebar goes off-canvas, the panel takes the whole
   screen: a 460px panel over a 600px window is a worse version of the page it
   covers. Close is in the head, so nothing is unreachable. */
@media (max-width: 720px) {
  #global-ai-dock { width: 100%; border-left: none; }
}
</style>
<script>
(() => {
  if (window.__globalAIDockInit) return;
  window.__globalAIDockInit = true;
  const dock = document.getElementById('global-ai-dock');
  const form = document.getElementById('global-ai-dock-form');
  const input = document.getElementById('global-ai-dock-input');
  const log = document.getElementById('global-ai-dock-log');
  const toggleBtn = document.getElementById('global-ai-dock-toggle');
  const whoEl = document.getElementById('global-ai-dock-who');
  const clearBtn = document.getElementById('global-ai-dock-clear');
  const closeBtn = document.getElementById('global-ai-dock-close');
  const sendBtn = form ? form.querySelector('button[type="submit"]') : null;
  if (!dock || !form || !input || !log) return;

  // The dock holds the conversation so a follow-up question means what it says.
  // /ai-chat/ask is stateless, so the transcript goes back with each turn —
  // except on Wintermute, which keeps it server-side and only needs the session
  // id the previous answer carried. The server bounds both.
  let history = [];
  let sessionID = '';

  // The dock names no provider. Which one answers is an install-wide setting,
  // and the server reads it per question — including "auto", the stored
  // backend, model and agent, all of which this box has no way to express.
  //
  // It used to ask /ai-chat/wintermute/status and pick Claude whenever a key
  // existed, which quietly ignored Settings: an install pinned to Wintermute
  // still sent every docked question to Anthropic.

  // hidden is removed first and .open set on the next frame, or the panel is
  // laid out already-open and the transform has nothing to animate from.
  // Which provider and agent a question will go to. It is the one thing this
  // box could not previously be asked: an agent chosen in Settings and a
  // conversation answering without one look identical from here.
  //
  // The session id is dropped when the agent changes, because wintermuted pins
  // a session to the agent it was opened with and never re-reads it — so a
  // conversation already in progress would keep answering as the old one, with
  // nothing on screen to say why the new choice had no effect.
  let answeringAs = null;
  async function refreshWho() {
    try {
      const resp = await fetch('/ai-chat/wintermute/status');
      const data = await resp.json();
      if (!resp.ok) return;
      const provider = String(data.provider || 'claude');
      const wintermute = provider === 'wintermute'
        || (provider === 'auto' && data.configured && data.token_configured);
      const agent = wintermute ? String(data.default_agent || '') : '';
      const label = wintermute
        ? 'Wintermute' + (agent ? ' · ' + agent : ' · no agent')
        : 'Claude';
      whoEl.textContent = label;
      whoEl.title = wintermute && !agent
        ? 'No agent is set, so answers come from the model rather than from this installation\'s catalogs.'
        : 'Set in Settings → AI providers';
      if (answeringAs !== null && answeringAs !== label) sessionID = '';
      answeringAs = label;
    } catch (_) {
      // The panel still works; it just cannot say what will answer.
    }
  }

  function open() {
    if (dock.classList.contains('open')) return;
    dock.hidden = false;
    requestAnimationFrame(() => dock.classList.add('open'));
    if (toggleBtn) toggleBtn.setAttribute('aria-expanded', 'true');
    input.focus();
    // Asked each time it is opened rather than once per page: Settings is
    // changed in another tab, and this is the moment the answer matters.
    refreshWho();
  }

  function close() {
    dock.classList.remove('open');
    if (toggleBtn) toggleBtn.setAttribute('aria-expanded', 'false');
    // Hidden only once it has finished sliding out, so the panel is not
    // removed from the page mid-animation.
    setTimeout(() => { if (!dock.classList.contains('open')) dock.hidden = true; }, 200);
    if (toggleBtn) toggleBtn.focus();
  }

  function expand() {
    open();
  }

  function addMessage(role, text, kind) {
    expand();
    const row = document.createElement('div');
    row.className = 'global-ai-dock-msg ' + (kind || '');
    const label = document.createElement('span');
    label.className = 'role';
    label.textContent = role;
    row.appendChild(label);
    row.appendChild(document.createTextNode(String(text || '')));
    log.appendChild(row);
    log.scrollTop = log.scrollHeight;
    return row;
  }

  if (toggleBtn) {
    toggleBtn.addEventListener('click', open);
  }
  if (clearBtn) {
    clearBtn.addEventListener('click', () => {
      log.replaceChildren();
      history = [];
      sessionID = '';
      input.focus();
    });
  }
  if (closeBtn) {
    closeBtn.addEventListener('click', close);
  }
  // Escape closes the panel, as it closes the command palette.
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && dock.classList.contains('open')) close();
  });

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const value = String(input.value || '').trim();
    if (!value) return;
    input.value = '';
    addMessage('You', value, 'user');
    const pending = addMessage('AI', 'Thinking…', 'note');
    if (sendBtn) sendBtn.disabled = true;
    input.disabled = true;
    try {
      const resp = await fetch('/ai-chat/ask', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          question: value,
          // A resumed Wintermute session already holds the transcript; sending
          // it again would replay every earlier turn into the same session.
          history: sessionID ? [] : history,
          session_id: sessionID
        })
      });
      const data = await resp.json();
      if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
      pending.remove();
      const answer = data.answer || '(empty answer)';
      // Recorded only on success: a question that never got an answer would
      // otherwise sit in the transcript as context for every later turn.
      history.push({ role: 'user', content: value });
      history.push({ role: 'assistant', content: answer });
      sessionID = data.session_id || '';
      addMessage(data.model ? 'AI · ' + data.model : 'AI', answer, 'ai');
    } catch (err) {
      pending.remove();
      addMessage('Error', (err && err.message) || 'request failed', 'note');
    } finally {
      if (sendBtn) sendBtn.disabled = false;
      input.disabled = false;
      input.focus();
      log.scrollTop = log.scrollHeight;
    }
  });
})();
</script>`

// themeToggleTag renders a small fixed-position control that flips the
// go_rcsa_theme cookie and reloads the page.
//
// The button names the theme you are *in*, not the one a click switches to.
// It used to do the opposite, and since this control is the only place a theme
// is ever named, that made every theme appear to be called the next one along:
// the green palette with no glitch — Matrix — sat under a button reading
// "Chaos theme". What a click does is in the tooltip and the accessible name,
// where a control's action belongs.
func themeToggleTag(theme string) string {
	name := map[string]string{
		themeDark:   "Dark",
		themeMatrix: "Matrix",
		themeChaos:  "Chaos",
		themeK40:    "40K",
	}
	icon := map[string]string{
		themeDark:   "🌙",
		themeMatrix: "▓",
		themeChaos:  "⚡",
		themeK40:    "⚙",
	}
	next := map[string]string{
		themeDark:   themeMatrix,
		themeMatrix: themeChaos,
		themeChaos:  themeK40,
		themeK40:    themeDark,
	}
	current, ok := name[theme]
	if !ok {
		theme, current = themeDark, name[themeDark]
	}
	action := "Switch to " + name[next[theme]]
	return `<button id="global-theme-toggle" type="button" aria-label="Theme: ` + current + `. ` + action + `" title="` + action + `">` + icon[theme] + ` ` + current + `</button>
<button id="global-text-lift" type="button" aria-label="Text brightness" title="Text brightness">Aa</button>
<style id="global-theme-toggle-style">
#global-theme-toggle {
  position: fixed;
  top: calc(12px + env(safe-area-inset-top));
  right: calc(60px + env(safe-area-inset-right));
  z-index: 10000;
  min-height: 36px;
  padding: 8px 14px;
  border-radius: 999px;
  border: 1px solid var(--line);
  background: var(--surface-strong);
  color: var(--ink);
  font: 13px Arial, sans-serif;
  cursor: pointer;
  touch-action: manipulation;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.18);
}
#global-text-lift {
  position: fixed;
  top: calc(12px + env(safe-area-inset-top));
  right: calc(12px + env(safe-area-inset-right));
  z-index: 10000;
  min-height: 36px;
  min-width: 36px;
  padding: 8px 10px;
  border-radius: 999px;
  border: 1px solid var(--line);
  background: var(--surface-strong);
  color: var(--ink);
  font: 13px Arial, sans-serif;
  cursor: pointer;
  touch-action: manipulation;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.18);
}
#global-theme-toggle:hover,
#global-text-lift:hover {
  background: var(--hover);
}
@media (max-width: 700px) {
  #global-theme-toggle,
  #global-text-lift {
    top: calc(8px + env(safe-area-inset-top));
    min-height: 44px;
    min-width: 44px;
    padding: 8px 12px;
    font-size: 12px;
  }
  #global-theme-toggle { right: calc(60px + env(safe-area-inset-right)); }
  #global-text-lift { right: calc(8px + env(safe-area-inset-right)); }
}
</style>
<script>
(() => {
  if (window.__globalThemeToggleInit) return;
  window.__globalThemeToggleInit = true;

  // Text brightness, per browser. Every palette but Light is dark and was
  // tuned on one screen; on a phone in daylight, or a panel with the contrast
  // wound down, the same colours are genuinely hard to read. It lifts the two
  // text tokens towards white and leaves the backgrounds and accents alone, so
  // a theme survives being made legible. 100 is the palette as designed.
  const lift = document.getElementById('global-text-lift');
  if (lift) {
    const STEPS = [100, 125, 150, 175];
    const apply = (value) => {
      document.documentElement.style.setProperty('--text-lift', (value - 100) + '%');
      lift.title = 'Text brightness ' + value + '%';
      lift.setAttribute('aria-label', 'Text brightness ' + value + '%, click to change');
    };
    const saved = parseInt(localStorage.getItem('grc-text-lift'), 10);
    apply(STEPS.includes(saved) ? saved : 100);
    lift.addEventListener('click', () => {
      const current = parseInt(localStorage.getItem('grc-text-lift'), 10);
      const next = STEPS[(STEPS.indexOf(STEPS.includes(current) ? current : 100) + 1) % STEPS.length];
      localStorage.setItem('grc-text-lift', String(next));
      apply(next);
    });
  }

  const btn = document.getElementById('global-theme-toggle');
  if (!btn) return;
  btn.addEventListener('click', () => {
    const cycle = ['` + themeDark + `', '` + themeMatrix + `', '` + themeChaos + `', '` + themeK40 + `'];
    const match = document.cookie.split('; ').find((row) => row.startsWith('` + themeCookieName + `='));
    const current = match ? match.split('=')[1] : '` + themeDark + `';
    const idx = cycle.indexOf(current);
    const next = cycle[(idx + 1) % cycle.length];
    document.cookie = '` + themeCookieName + `=' + next + '; Max-Age=31536000; Path=/; SameSite=Lax';
    window.location.reload();
  });
})();
</script>`
}

type htmlCaptureWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *htmlCaptureWriter) Write(data []byte) (int, error) {
	return w.body.Write(data)
}

func (w *htmlCaptureWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

func themeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		capture := &htmlCaptureWriter{ResponseWriter: c.Writer}
		c.Writer = capture

		theme := currentTheme(c)

		c.Next()

		contentType := strings.ToLower(capture.Header().Get("Content-Type"))
		payload := capture.body.Bytes()

		if !strings.Contains(contentType, "text/html") {
			capture.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = capture.ResponseWriter.Write(payload)
			return
		}

		modified := injectGlobalUI(payload, theme)
		capture.Header().Set("Content-Length", strconv.Itoa(len(modified)))
		_, _ = capture.ResponseWriter.Write(modified)
	}
}

// currentTheme reads the user's theme preference cookie, defaulting to dark
// when it is absent or unrecognised.
//
// That default is also the migration: a browser still carrying the retired
// "light" cookie falls through to dark rather than to a palette that no longer
// exists, with no cookie to clear and nothing to explain.
func currentTheme(c *gin.Context) string {
	value, err := c.Cookie(themeCookieName)
	if err != nil {
		return themeDark
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case themeMatrix:
		return themeMatrix
	case themeChaos:
		return themeChaos
	case themeK40:
		return themeK40
	default:
		return themeDark
	}
}

// injectGlobalUI splices the theme palette and the responsive layer into the
// end of <head>, and the nav, toggle, AI dock and (for Chaos) the glitch engine
// into the end of <body>.
//
// It does this in one pass. The previous shape chained six functions, each of
// which converted the payload to a string, lowercased the *entire document* just
// to locate a tag case-insensitively, and concatenated — so a 120 KB page cost
// ~2 MB of garbage and ~5 ms before the handler's own work. Here the two
// insertion points are found once with an allocation-free case-insensitive
// scan, and the result is assembled with a single sized allocation.
//
// Ordering is load-bearing and must not change: the palette goes in before the
// responsive layer, because the responsive layer overrides the palette's
// !important declarations and can only do that from later in the cascade.
func injectGlobalUI(payload []byte, theme string) []byte {
	headTag := headInsertTag(theme)
	bodyTag := bodyInsertTag(theme)

	headAt := indexFoldASCII(payload, []byte("</head>"))
	bodyAt := lastIndexFoldASCII(payload, []byte("</body>"))

	// A fragment with neither tag is passed through untouched, which is what
	// kept partial responses working before.
	if headAt < 0 && bodyAt < 0 {
		return payload
	}
	// Defensive: if </body> somehow precedes </head>, splicing both would
	// interleave the insertions. Fall back to the head insertion alone.
	if headAt >= 0 && bodyAt >= 0 && bodyAt < headAt {
		bodyAt = -1
	}

	size := len(payload)
	if headAt >= 0 {
		size += len(headTag)
	}
	if bodyAt >= 0 {
		size += len(bodyTag)
	}

	out := make([]byte, 0, size)
	cursor := 0
	if headAt >= 0 {
		out = append(out, payload[:headAt]...)
		out = append(out, headTag...)
		cursor = headAt
	}
	if bodyAt >= 0 {
		out = append(out, payload[cursor:bodyAt]...)
		out = append(out, bodyTag...)
		cursor = bodyAt
	}
	return append(out, payload[cursor:]...)
}

// headInsertTag is everything that belongs at the end of <head>: the palette
// followed by the responsive layer.
func headInsertTag(theme string) string {
	return themeStyleTag(theme) + mobileStyleTag
}

// bodyInsertTag is everything that belongs at the end of <body>. The falling-
// glyph backdrop goes to Matrix and Chaos, which share a palette built around
// it; Dark and Light must not pay for a canvas repainting behind a page that
// would never show it. The glitch engine is narrower still — Chaos only, since
// the other three themes must not pay for a timer that walks the whole DOM.
func bodyInsertTag(theme string) string {
	tags := sideNavTag + commandPaletteTag + themeToggleTag(theme) + aiQuickPromptDockTag
	if theme == themeMatrix || theme == themeChaos {
		tags += matrixRainTag
	}
	if theme == themeChaos {
		tags += chaosEngineTag
	}
	if theme == themeK40 {
		tags += fritzOverlayTag
	}
	return tags
}

// indexFoldASCII is strings.Index with ASCII case folding, without allocating a
// lowercased copy of the haystack. HTML tag names are ASCII, so folding only
// A–Z is correct here and avoids the Unicode machinery entirely.
func indexFoldASCII(haystack, needle []byte) int {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return -1
	}
	last := len(haystack) - len(needle)
	for i := 0; i <= last; i++ {
		if matchFoldASCII(haystack[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

// lastIndexFoldASCII is the trailing-match counterpart, used for </body> so a
// nested or commented-out tag earlier in the document does not win.
func lastIndexFoldASCII(haystack, needle []byte) int {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return -1
	}
	for i := len(haystack) - len(needle); i >= 0; i-- {
		if matchFoldASCII(haystack[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

func matchFoldASCII(a, b []byte) bool {
	for i := range b {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
