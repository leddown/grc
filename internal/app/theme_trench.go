package app

// themeTrench is the Trench Crusade theme, after the tabletop setting: 1914,
// eight hundred years into a crusade against Hell that has settled into trench
// war. Brought across from wintermute, where its palette was read off the
// game's own site and its type follows the rulebook: blackletter for chapter
// heads, Garamond for everything read.
//
// The game's faces are proprietary, so open ones stand in: Grenze Gotisch and
// EB Garamond, both SIL OFL, bundled in themeassets with their licences. No
// mark, logo, art or lettering of the game's appears anywhere; every ornament
// in themeassets was drawn for wintermute.
const themeTrench = "trench"

// trenchThemeCSS is the costume. Red does two jobs and they are kept apart: the
// official red (--tc-blood) is too dark to read as text on these grounds, so
// it is the rubrication — fills, rules, ornaments — and never small type.
//
// Contested rules on the top bar and the sidebar go through the bar's id or
// carry !important, for the reason tronThemeCSS gives.
const trenchThemeCSS = `
@font-face {
  font-family: "EB Garamond";
  font-style: normal;
  font-weight: 400 800;
  font-display: swap;
  src: url("/theme-assets/EBGaramond-Roman.woff2") format("woff2");
}
@font-face {
  font-family: "EB Garamond";
  font-style: italic;
  font-weight: 400 800;
  font-display: swap;
  src: url("/theme-assets/EBGaramond-Italic.woff2") format("woff2");
}
@font-face {
  font-family: "Grenze Gotisch";
  font-style: normal;
  font-weight: 100 900;
  font-display: swap;
  src: url("/theme-assets/GrenzeGotisch.woff2") format("woff2");
}

/* Garamond's x-height is a fifth smaller than a system sans's, and the scale
   sets text in fixed pixels, so swapping the face alone would shrink every
   label. font-size-adjust brings the face to a common x-height instead.
   Code keeps its own proportions. Both carry !important because the scale sets
   text with the font shorthand, which resets font-size-adjust and
   font-variant-caps along with everything else. */
body, button, input, select, textarea, .tab, table {
  font-family: var(--tc-book) !important;
  font-size-adjust: 0.47 !important;
}
/* The Policy Studio's document is set in its own type, and its paper view is
   the deliverable: neither the adjustment nor the heads and table rules below
   reach it. */
code, pre, kbd, samp, .mono, .ps-prosemirror { font-size-adjust: none !important; }

/* The ground: warm black darkening to the corners, under film grain like the
   grain the setting's art is printed with. No man's land itself is drawn over
   it by noMansLandTag. */
html, body {
  background:
    radial-gradient(ellipse 120% 90% at 50% 40%, transparent 55%, rgba(0, 0, 0, 0.55)),
    url("/theme-assets/grain.svg"),
    var(--bg) !important;
  background-attachment: fixed !important;
}
html { scrollbar-color: var(--tc-iron) transparent; }
::selection { background: #8f1519; color: #f1e9da; }

/* The top bar is strung with wire along its foot: one strand, laid over the
   border so half of it hangs over whatever is below. */
.global-topbar {
  background: rgba(14, 11, 9, 0.9) !important;
  border-bottom-color: var(--tc-iron) !important;
}
.global-topbar::after {
  content: "";
  position: absolute;
  left: 0;
  right: 0;
  bottom: -5px;
  height: 10px;
  background: url("/theme-assets/wire.svg") repeat-x 0 50% / 40px 10px;
  pointer-events: none;
  z-index: 2;
}
/* The name of the machine is the one blackletter line at rest, after a cross. */
#globalTopbar .global-brand-link {
  font-family: var(--tc-blackletter) !important;
  font-size-adjust: none !important;
  font-size: 22px !important;
  font-weight: 700 !important;
  line-height: 1 !important;
  letter-spacing: 0.01em !important;
  color: var(--text) !important;
}
#globalTopbar .global-brand-link::before {
  content: "";
  display: inline-block;
  width: 12px;
  height: 12px;
  margin-right: 7px;
  vertical-align: 1px;
  background: var(--tc-blood);
  -webkit-mask: url("/theme-assets/cross.svg") center / contain no-repeat;
  mask: url("/theme-assets/cross.svg") center / contain no-repeat;
}

/* The section switcher is set the way the rulebook sets its section heads,
   in small capitals, with a red rule under the one you are on. */
#globalTopbar .global-view-btn {
  font-family: var(--tc-book) !important;
  font-variant-caps: all-small-caps !important;
  letter-spacing: 0.07em;
  font-size: 14px !important;
  font-weight: 600 !important;
  color: var(--muted) !important;
  border-radius: 0 !important;
}
#globalTopbar .global-view-btn:hover {
  color: var(--text) !important;
  background: rgba(233, 225, 209, 0.05) !important;
}
#globalTopbar .global-view-btn.active {
  color: var(--text) !important;
  background: linear-gradient(0deg, rgba(175, 31, 36, 0.2), transparent 75%) border-box !important;
  border-color: transparent !important;
  box-shadow: inset 0 -2px 0 var(--tc-blood);
}

/* The sidebar is the rulebook's margin: the entry you are reading is marked
   with a red rule down its edge. */
.global-sidenav {
  background: rgba(18, 14, 11, 0.88) !important;
  border-right-color: var(--tc-iron) !important;
}
/* Through the shell: the sidebar's own radius comes later in the page at the
   same specificity. A gradient on a control with a transparent border is
   painted from the border box, or it repeats into the border as a 1px line. */
.global-shell .global-sidenav .tab { border-radius: 0 !important; }
.global-sidenav .tab:hover { background: rgba(233, 225, 209, 0.05) !important; }
.global-sidenav .tab.active {
  color: var(--text) !important;
  background: linear-gradient(90deg, rgba(175, 31, 36, 0.18), transparent) border-box !important;
  border-color: transparent !important;
  box-shadow: inset 2px 0 0 var(--tc-blood);
}
.global-sidenav .tab-group-label, th:not(.ps-prosemirror *), .stat .k,
.panel-header, .list-header, .detail-header, .pane-head {
  font-family: var(--tc-book) !important;
  text-transform: none !important;
  font-variant-caps: all-small-caps !important;
  letter-spacing: 0.07em !important;
  font-size: 13px !important;
}

/* Chapter heads: blackletter in red, the way the rulebook opens a chapter. At
   20px and bold it is large text, which is what lets the red be read at all:
   3.8:1 on a panel, over the 3:1 floor for large text. Smaller heads keep the
   blackletter but not the red, which at their size would fall under it.
   Blackletter is set in lower case: a run of its capitals cannot be read, and
   several pages capitalise their heads. */
h1:not(.ps-prosemirror *) {
  font-family: var(--tc-blackletter) !important;
  text-transform: none !important;
  font-size-adjust: none !important;
  font-weight: 700 !important;
  letter-spacing: 0.01em !important;
  line-height: 1.1 !important;
  color: var(--tc-rubric) !important;
}
/* A page's h2 is often a caption at 12px, where blackletter cannot be read;
   17px is where it can, and Grenze is narrow enough to fit where the caption
   did. */
.global-content h2:not(.ps-prosemirror *) {
  font-family: var(--tc-blackletter) !important;
  text-transform: none !important;
  font-size-adjust: none !important;
  font-size: 17px !important;
  line-height: 1.15 !important;
  font-weight: 600 !important;
  letter-spacing: 0.01em !important;
}

/* Section labels: small capitals over a red rule, with a cross ahead of them in
   place of a pilcrow. */
.group-head {
  text-transform: none !important;
  font-variant-caps: all-small-caps !important;
  font-size: 14px !important;
  font-weight: 600 !important;
  letter-spacing: 0.08em !important;
  color: var(--text) !important;
  padding-bottom: 2px !important;
  border-bottom: 1px solid var(--tc-blood) !important;
}
.group-head::before {
  content: "";
  display: inline-block;
  width: 9px;
  height: 9px;
  margin-right: 6px;
  background: var(--tc-blood);
  -webkit-mask: url("/theme-assets/cross.svg") center / contain no-repeat;
  mask: url("/theme-assets/cross.svg") center / contain no-repeat;
}

/* Panes are framed twice, like a plate in an old book: the border, and a gilt
   hairline set inside it. The hairline is an outline drawn inward rather than
   an inset shadow, because a pane's header and body are opaque and would paint
   over a shadow; an outline is drawn above them. A focus ring replaces it. */
.panel, .pane, .detail, .list, .stat, .card, .summary-card, .status-card, .detail-card,
.report-card, main:has(#loginForm) {
  border-color: var(--border) !important;
  border-radius: 1px !important;
  outline: 1px solid rgba(184, 154, 94, 0.18);
  outline-offset: -4px;
  box-shadow: 0 10px 28px -14px rgba(0, 0, 0, 0.9);
}
/* The operator's words are rubricated. */
.msg.user { border-left-color: var(--tc-blood) !important; }

.global-content button, body:not(:has(.global-shell)) main button {
  border-radius: 1px !important;
  letter-spacing: 0.02em;
}
.global-content button:hover:not(:disabled),
body:not(:has(.global-shell)) main button:hover:not(:disabled) { filter: brightness(1.14); }
.global-content button.secondary, .global-content .ghost-btn { border-color: var(--tc-iron) !important; }
.global-content button.secondary:hover, .global-content .ghost-btn:hover {
  border-color: var(--accent) !important;
  color: var(--accent) !important;
}
.global-content a.btn, .global-content a.button, .global-content .button-link { border-radius: 1px !important; }
.pill, .chip, .tag, .badge { border-color: var(--tc-iron) !important; }

input, textarea, select { border-radius: 1px !important; }
input:focus, textarea:focus, select:focus {
  border-color: var(--accent) !important;
  box-shadow: 0 0 0 1px rgba(201, 168, 98, 0.35);
}
.global-content .tab.active { box-shadow: inset 0 -2px 0 var(--tc-blood); }

/* The sign-in card is an enlistment: the emblem over the name, then the form.
   The emblem, a faceless pilgrim under a halo of thorns, was drawn for
   wintermute. */
main:has(#loginForm)::before {
  content: "";
  display: block;
  width: 112px;
  height: 112px;
  margin: 0 auto 4px;
  background: url("/theme-assets/idol.svg") center / contain no-repeat;
}
main:has(#loginForm) h1 {
  text-align: center;
  font-size: 30px !important;
}`

// noMansLandTag is the Trench Crusade theme's backdrop: along the foot of the
// screen, a front at night. Two ridges of churned ground, grave crosses, stakes
// strung with wire, the odd helmet hung on a post, and the sky over the line
// dull red with whatever is burning past it. Ash falls across it and embers
// lift off it. Every so often a star shell goes up, hangs swaying under its
// parachute and lights the whole line from behind, then gutters out.
//
// The ground is generated from a fixed seed, so it is the same front on every
// load; a resize redraws it to the new width from the same seed.
//
// Same terms as the rain and the cycles: fixed behind the page, inert to the
// pointer, injected for this theme only, paused in a background tab, and a
// still frame (the front, no flare) for anyone who asked for less motion.
//
// The settings are per-browser, in localStorage, and can be changed from the
// browser console through window.GRCFront, which can also send up a flare:
//
//	grc-front-ash        : ash and ember particles (default 90, 0 to 400)
//	grc-front-brightness : percentage scaling of the fade (default 100, 10 to 115)
const noMansLandTag = `<canvas id="global-no-mans-land" aria-hidden="true"></canvas>
<style id="global-no-mans-land-style">
#global-no-mans-land {
  position: fixed;
  top: 0;
  left: 0;
  width: 100% !important;
  height: 100% !important;
  max-width: none !important;
  z-index: 0;
  pointer-events: none;
  opacity: 0.85;
}
/* The page rides above the front, as it does above the rain. */
main {
  position: relative !important;
  z-index: 1 !important;
}
</style>
<script id="global-no-mans-land-engine">
(() => {
  if (window.__globalFrontInit) return;
  window.__globalFrontInit = true;

  const canvas = document.getElementById("global-no-mans-land");
  if (!canvas || !canvas.getContext) return;

  const ASH_KEY = "grc-front-ash";
  const BRIGHTNESS_KEY = "grc-front-brightness";

  const DEFAULT_ASH = 90;
  const MIN_ASH = 0;
  const MAX_ASH = 400;

  // Higher than the rain's fade, because most of this layer is near-black on
  // near-black and only the sky and the ash show.
  const BASE_OPACITY = 0.85;
  const DEFAULT_BRIGHTNESS = 100;
  const MIN_BRIGHTNESS = 10;
  const MAX_BRIGHTNESS = 115;

  const FPS = 30;
  const SEED = 1914;

  // How often a flare goes up on average, and how long one burns. The wait is
  // randomised around the average: a barrage on a beat is a metronome.
  const FLARE_EVERY = 28;
  const FLARE_LIFE = 12;
  // Of the particles, how many are embers rising rather than ash falling.
  const EMBER_SHARE = 0.12;

  const ASH = "201, 191, 173";
  const EMBER = "236, 104, 52";
  const FLARE = "255, 240, 214";

  let ctx = null;
  // The ground and its sky, drawn once per size and copied every frame.
  let land = null;
  let width = 0;
  let height = 0;
  let horizon = 0;
  let particles = [];
  let flare = null;
  let nextFlare = 0;
  let clock = 0;
  let last = 0;
  let frame = null;

  function reduceMotion() {
    return window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  }

  function clampInt(value, min, max, fallback) {
    const n = parseInt(value, 10);
    if (!Number.isFinite(n)) return fallback;
    return Math.min(max, Math.max(min, n));
  }

  function ashCount() {
    return clampInt(localStorage.getItem(ASH_KEY), MIN_ASH, MAX_ASH, DEFAULT_ASH);
  }

  function brightness() {
    return clampInt(localStorage.getItem(BRIGHTNESS_KEY), MIN_BRIGHTNESS, MAX_BRIGHTNESS, DEFAULT_BRIGHTNESS);
  }

  function applyBrightness() {
    canvas.style.opacity = String(Math.min(1, BASE_OPACITY * (brightness() / 100)));
  }

  function rgba(colour, alpha) {
    return "rgba(" + colour + ", " + alpha + ")";
  }

  // mulberry32: a small seeded generator, so the front is the same each load.
  function seeded(seed) {
    let a = seed >>> 0;
    return () => {
      a = (a + 0x6d2b79f5) >>> 0;
      let t = a;
      t = Math.imul(t ^ (t >>> 15), t | 1);
      t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }

  // A ridge line: a few sine waves at unrelated lengths, which reads as ground
  // that has been shelled rather than as a wave.
  function ridge(rand, base, amp) {
    const p = [rand() * 6.3, rand() * 6.3, rand() * 6.3];
    return (x) => base + amp * (
      0.6 * Math.sin(x / 97 + p[0]) + 0.3 * Math.sin(x / 41 + p[1]) + 0.1 * Math.sin(x / 13 + p[2]));
  }

  function fillRidge(g, line, colour) {
    g.fillStyle = colour;
    g.beginPath();
    g.moveTo(-10, height + 10);
    for (let x = -10; x <= width + 10; x += 6) g.lineTo(x, line(x));
    g.lineTo(width + 10, height + 10);
    g.closePath();
    g.fill();
  }

  // A grave cross stood in the mud, leaning. Some have lost an arm.
  function cross(g, x, y, h, post, lean, broken) {
    g.save();
    g.translate(x, y);
    g.rotate(lean);
    g.fillRect(-post / 2, -h, post, h + 4);
    const arm = h * 0.6;
    const at = -h * 0.72;
    if (broken) g.fillRect(-post / 2, at, arm / 2, post * 0.9);
    else g.fillRect(-arm / 2, at, arm, post * 0.9);
    g.restore();
  }

  // A picket with a helmet hung on it: a dome and a brim.
  function helmet(g, x, top, lean) {
    g.save();
    g.translate(x, top);
    g.rotate(lean);
    g.beginPath();
    g.ellipse(0, 0, 6.5, 5, 0, Math.PI, 0);
    g.fill();
    g.fillRect(-8.5, -0.5, 17, 2);
    g.restore();
  }

  // Wire sagging between two pickets, with a barb every few pixels.
  function wire(g, a, b, sag) {
    const mx = (a.x + b.x) / 2;
    const my = (a.y + b.y) / 2 + sag;
    g.beginPath();
    g.moveTo(a.x, a.y);
    g.quadraticCurveTo(mx, my, b.x, b.y);
    g.stroke();
    const len = Math.hypot(b.x - a.x, b.y - a.y);
    for (let s = 6; s < len - 4; s += 9) {
      const t = s / len;
      const u = 1 - t;
      const px = u * u * a.x + 2 * u * t * mx + t * t * b.x;
      const py = u * u * a.y + 2 * u * t * my + t * t * b.y;
      g.beginPath();
      g.moveTo(px - 1.8, py - 1.8); g.lineTo(px + 1.8, py + 1.8);
      g.moveTo(px + 1.8, py - 1.8); g.lineTo(px - 1.8, py + 1.8);
      g.stroke();
    }
  }

  // buildLand draws the sky glow and the two ridges, with everything stood on
  // them, into an offscreen canvas. Called on every resize, never per frame.
  function buildLand(dpr) {
    land = document.createElement("canvas");
    land.width = Math.floor(width * dpr);
    land.height = Math.floor(height * dpr);
    const g = land.getContext("2d");
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    const rand = seeded(SEED);

    const band = Math.max(90, Math.min(190, height * 0.17));
    horizon = height - band;

    // Whatever is burning past the line, low on the sky.
    const sky = g.createLinearGradient(0, horizon - band * 1.6, 0, horizon + band * 0.4);
    sky.addColorStop(0, "rgba(150, 45, 28, 0)");
    sky.addColorStop(0.75, "rgba(150, 45, 28, 0.2)");
    sky.addColorStop(1, "rgba(175, 60, 30, 0.34)");
    g.fillStyle = sky;
    g.fillRect(0, horizon - band * 1.6, width, band * 2);

    // The far ridge, with its crosses small and close-set.
    const far = ridge(rand, horizon + band * 0.2, band * 0.08);
    g.fillStyle = "#1c1511";
    for (let x = 4 + rand() * 20; x < width; x += 14 + rand() * 34) {
      const h = 7 + rand() * 8;
      cross(g, x, far(x) + 2, h, 1.6 + rand() * 0.7, (rand() - 0.5) * 0.26, rand() < 0.12);
    }
    fillRidge(g, far, "#1c1511");

    // The near ridge, with the dead of the last push and the wire in front.
    const near = ridge(rand, horizon + band * 0.5, band * 0.12);
    const ink = "#090706";
    g.fillStyle = ink;
    g.strokeStyle = ink;
    g.lineWidth = 1.1;
    let picket = null;
    for (let x = -10 + rand() * 30; x < width + 20; x += 24 + rand() * 70) {
      const y = near(x) + 3;
      const lean = (rand() - 0.5) * 0.4;
      const roll = rand();
      if (roll < 0.58) {
        const h = 22 + rand() * 24;
        cross(g, x, y, h, 3 + rand() * 1.5, lean, rand() < 0.14);
        picket = null;
      } else {
        const h = 16 + rand() * 16;
        g.save();
        g.translate(x, y);
        g.rotate(lean * 0.5);
        g.fillRect(-1.5, -h, 3, h + 4);
        g.restore();
        const top = { x: x + Math.sin(lean * 0.5) * h, y: y - Math.cos(lean * 0.5) * h };
        if (rand() < 0.25) helmet(g, top.x, top.y - 2, lean * 0.4);
        if (picket && x - picket.x < 170) {
          wire(g, { x: picket.x, y: picket.y + 3 }, { x: top.x, y: top.y + 3 }, 6 + rand() * 8);
          wire(g, { x: picket.x, y: picket.y + picket.h * 0.55 }, { x: top.x, y: top.y + h * 0.55 }, 4 + rand() * 6);
        }
        picket = { x: top.x, y: top.y, h: h };
      }
    }
    fillRidge(g, near, ink);
  }

  function newAsh(scatter) {
    return {
      ember: false,
      x: Math.random() * width,
      y: scatter ? Math.random() * height : -4,
      vy: 8 + Math.random() * 14,
      r: 0.6 + Math.random() * 1.2,
      a: 0.18 + Math.random() * 0.4,
      ph: Math.random() * 6.3,
    };
  }

  function newEmber(scatter) {
    const life = 3 + Math.random() * 4;
    return {
      ember: true,
      x: Math.random() * width,
      y: horizon + Math.random() * (height - horizon) * 0.5,
      vy: -(12 + Math.random() * 16),
      r: 0.8 + Math.random() * 0.9,
      a: 0.5 + Math.random() * 0.4,
      ph: Math.random() * 6.3,
      born: clock - (scatter ? Math.random() * life : 0),
      life: life,
    };
  }

  function reset() {
    particles = [];
    const n = ashCount();
    for (let i = 0; i < n; i++) {
      particles.push(i < n * EMBER_SHARE ? newEmber(true) : newAsh(true));
    }
  }

  function scheduleFlare() {
    nextFlare = clock + FLARE_EVERY * (0.4 + Math.random() * 1.2);
  }

  function launch() {
    const x = width * (0.15 + Math.random() * 0.7);
    flare = {
      x0: x,
      y0: height * (0.06 + Math.random() * 0.16),
      from: x + (Math.random() - 0.5) * width * 0.25,
      born: clock,
      sway: Math.random() * 6.3,
      drift: (Math.random() - 0.5) * 12,
    };
    scheduleFlare();
  }

  // The flare's light, 0 to 1: lit almost at once, burning unevenly, guttering
  // out over its last few seconds.
  function flareLight(age) {
    const ignite = Math.min(1, age / 0.5);
    const fade = Math.min(1, (FLARE_LIFE - age) / 3);
    const flicker = 0.84 + 0.1 * Math.sin(age * 23.1) + 0.06 * Math.sin(age * 57.3);
    return Math.max(0, ignite * fade * flicker);
  }

  function advance(dt) {
    clock += dt;
    const wind = 5 + Math.sin(clock * 0.07) * 6;
    for (let i = 0; i < particles.length; i++) {
      const p = particles[i];
      if (p.ember) {
        p.x += (wind * 0.4 + Math.sin(clock * 1.7 + p.ph) * 5) * dt;
        p.y += p.vy * dt;
        if (clock - p.born > p.life) particles[i] = newEmber(false);
      } else {
        p.x += (wind + Math.sin(clock * 0.8 + p.ph) * 6) * dt;
        p.y += p.vy * dt;
        if (p.y > height + 4 || p.x < -6 || p.x > width + 6) particles[i] = newAsh(false);
      }
    }
    if (flare && clock - flare.born > FLARE_LIFE) flare = null;
    if (!flare && clock >= nextFlare) launch();
  }

  function draw() {
    ctx.globalCompositeOperation = "source-over";
    ctx.clearRect(0, 0, width, height);
    if (land) ctx.drawImage(land, 0, 0, width, height);

    if (flare) {
      const age = clock - flare.born;
      const light = flareLight(age);
      const fx = flare.x0 + Math.sin(age * 0.6 + flare.sway) * 18 + flare.drift * age;
      const fy = flare.y0 + height * 0.028 * age;

      // The light goes behind the ground already drawn, so the ridges and
      // everything on them stand black against it. 0.34 at the heart is the
      // ceiling: with the canvas fade on top, parchment text lying over the
      // flare still clears 5.8:1 against the lit sky.
      const reach = Math.max(width, height) * 0.75;
      const glow = ctx.createRadialGradient(fx, fy, 0, fx, fy, reach);
      glow.addColorStop(0, rgba(FLARE, 0.34 * light));
      glow.addColorStop(0.3, rgba(FLARE, 0.1 * light));
      glow.addColorStop(1, rgba(FLARE, 0));
      ctx.globalCompositeOperation = "destination-over";
      ctx.fillStyle = glow;
      ctx.fillRect(0, 0, width, height);
      ctx.globalCompositeOperation = "source-over";

      // The smoke of its climb, fading in the first seconds.
      if (age < 3.5) {
        ctx.strokeStyle = rgba(ASH, 0.22 * (1 - age / 3.5));
        ctx.lineWidth = 1.2;
        ctx.beginPath();
        ctx.moveTo(flare.from, horizon + 6);
        ctx.quadraticCurveTo((flare.from + flare.x0) / 2, flare.y0 - 30, flare.x0, flare.y0);
        ctx.stroke();
      }

      const core = ctx.createRadialGradient(fx, fy, 0, fx, fy, 30);
      core.addColorStop(0, "rgba(255, 255, 255, " + light + ")");
      core.addColorStop(0.12, "rgba(255, 252, 240, " + light + ")");
      core.addColorStop(0.3, rgba(FLARE, 0.35 * light));
      core.addColorStop(1, rgba(FLARE, 0));
      ctx.fillStyle = core;
      ctx.beginPath(); ctx.arc(fx, fy, 30, 0, Math.PI * 2); ctx.fill();
    }

    for (const p of particles) {
      let a = p.a;
      let colour = ASH;
      if (p.ember) {
        const t = (clock - p.born) / p.life;
        a *= Math.min(1, t * 4) * Math.min(1, (1 - t) * 2) * (0.7 + 0.3 * Math.sin(clock * 9 + p.ph));
        colour = EMBER;
      }
      if (a <= 0) continue;
      ctx.fillStyle = rgba(colour, a);
      ctx.fillRect(p.x, p.y, p.r * 2, p.r * 2);
    }
  }

  function loop(now) {
    frame = requestAnimationFrame(loop);
    const dt = (now - last) / 1000;
    if (dt < 1 / FPS) return;
    last = now;
    // A long gap, a dropped frame or a wake from sleep, is taken as one frame.
    advance(Math.min(dt, 0.1));
    draw();
  }

  function resize() {
    width = canvas.clientWidth;
    height = canvas.clientHeight;
    if (width <= 0 || height <= 0) return false;
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = Math.floor(width * dpr);
    canvas.height = Math.floor(height * dpr);
    ctx = canvas.getContext("2d");
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    buildLand(dpr);
    reset();
    return true;
  }

  // paintStill is what prefers-reduced-motion gets: the front by night, ash
  // hanging where it fell, no flare up.
  function paintStill() {
    if (!ctx) return;
    flare = null;
    reset();
    draw();
  }

  function startLoop() {
    stopLoop();
    last = performance.now();
    frame = requestAnimationFrame(loop);
  }

  function stopLoop() {
    if (frame) cancelAnimationFrame(frame);
    frame = null;
  }

  function paint() {
    if (reduceMotion()) {
      stopLoop();
      paintStill();
      return;
    }
    // The ground is there from the first paint rather than from the first tick.
    draw();
    startLoop();
  }

  function start() {
    if (!resize()) return;
    applyBrightness();
    // The first flare comes soon, so whoever just picked the theme sees one.
    nextFlare = clock + 4 + Math.random() * 4;
    paint();
    window.addEventListener("resize", () => {
      if (resize()) paint();
    });
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) {
        stopLoop();
      } else if (!reduceMotion()) {
        startLoop();
      }
    });
  }

  window.GRCFront = {
    config: () => ({ ash: ashCount(), brightness: brightness() }),
    setConfig: (next) => {
      localStorage.setItem(ASH_KEY, String(clampInt(next.ash, MIN_ASH, MAX_ASH, DEFAULT_ASH)));
      localStorage.setItem(BRIGHTNESS_KEY,
        String(clampInt(next.brightness, MIN_BRIGHTNESS, MAX_BRIGHTNESS, DEFAULT_BRIGHTNESS)));
      applyBrightness();
      if (!ctx) return;
      if (reduceMotion()) paintStill();
      else reset();
    },
    // A flare comes round every half minute, which is a long wait to judge the
    // brightness by. Nothing goes up when nothing is animating.
    flare: () => {
      if (!frame) return false;
      launch();
      return true;
    },
    MIN_ASH, MAX_ASH, MIN_BRIGHTNESS, MAX_BRIGHTNESS,
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
</script>`
