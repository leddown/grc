package app

// themeTron is inside the machine: a black-blue void ruled into a grid,
// everything drawn in light rather than ink, and light cycles racing along the
// lines behind the panes (lightCyclesTag). Cyan is ours. Orange is the other
// side, so it is spent on errors, losses and destructive controls.
//
// Brought across from wintermute, onto this application's shell: its top bar
// is .global-topbar here, its view switcher .global-view-btn, its sidebar rows
// .global-sidenav .tab, and its panes the .panel/.card family.
const themeTron = "tron"

// tronThemeCSS is the costume. Every glow is a drop-shadow filter, a
// text-shadow or a gradient; none of it positions anything.
//
// The top bar and the sidebar declare their own backgrounds and fonts after
// this layer in the cascade, so what is contested there is addressed through
// the bar's id or carries !important, as the 40K rules do.
const tronThemeCSS = `
/* The grid, fixed to the viewport so the cycles' canvas, also fixed, rides on
   its lines: 40px is CELL in lightCyclesTag. A horizon glow rises from below,
   and a faint orange sky hangs above. */
html, body {
  background:
    radial-gradient(ellipse 85% 45% at 50% 112%, rgba(24, 230, 255, 0.2), transparent 70%),
    radial-gradient(ellipse 70% 35% at 50% -12%, rgba(255, 122, 26, 0.09), transparent 70%),
    linear-gradient(rgba(24, 230, 255, 0.065) 1px, transparent 1px),
    linear-gradient(90deg, rgba(24, 230, 255, 0.065) 1px, transparent 1px),
    var(--bg) !important;
  background-size: auto, auto, 40px 40px, 40px 40px, auto !important;
  background-attachment: fixed !important;
}
html { scrollbar-color: rgba(24, 230, 255, 0.45) transparent; }
::selection { background: rgba(255, 122, 26, 0.55); color: #ffffff; }

/* The top bar: a light rail along its foot, with a pulse running down it. */
.global-topbar {
  border-bottom-color: var(--accent) !important;
  box-shadow: 0 1px 16px rgba(24, 230, 255, 0.3);
  background:
    linear-gradient(90deg, transparent, rgba(220, 254, 255, 0.95), transparent) no-repeat,
    linear-gradient(180deg, rgba(24, 230, 255, 0.07), transparent),
    rgba(3, 9, 18, 0.92) !important;
  background-size: 240px 2px, auto, auto !important;
  background-position: -240px 100%, 0 0, 0 0 !important;
  animation: tron-pulse 7s linear infinite;
}
@keyframes tron-pulse {
  to { background-position: calc(100% + 240px) 100%, 0 0, 0 0; }
}
#globalTopbar .global-brand-link {
  font-family: var(--tron-display) !important;
  text-transform: uppercase !important;
  letter-spacing: 0.34em !important;
  background: linear-gradient(90deg, #8ff7ff, #ffffff 45%, var(--accent)) !important;
  -webkit-background-clip: text !important;
  background-clip: text !important;
  color: transparent !important;
  filter: drop-shadow(0 0 6px var(--tron-glow));
}

/* The section switcher: no boxes, just a lit rail under the one you are on. */
#globalTopbar .global-view-btn {
  font-family: var(--tron-display) !important;
  font-size: 11px !important;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  border-radius: 0 !important;
}
#globalTopbar .global-view-btn:hover {
  color: var(--text) !important;
  background: rgba(24, 230, 255, 0.08) !important;
}
#globalTopbar .global-view-btn.active {
  color: var(--accent) !important;
  background: linear-gradient(0deg, rgba(24, 230, 255, 0.16), transparent 70%) border-box !important;
  border-color: transparent !important;
  box-shadow: inset 0 -2px 0 var(--accent);
  text-shadow: 0 0 8px var(--tron-glow);
}

.global-sidenav {
  background: rgba(3, 9, 18, 0.9) !important;
  border-right-color: var(--tron-faint) !important;
  box-shadow: 1px 0 18px rgba(24, 230, 255, 0.12);
}
/* Through the shell: the sidebar's own radius comes later in the page at the
   same specificity. A gradient on a control with a transparent border is
   painted from the border box, or it repeats into the border as a 1px line. */
.global-shell .global-sidenav .tab { border-radius: 0 !important; }
.global-sidenav .tab:hover {
  background: linear-gradient(90deg, rgba(24, 230, 255, 0.1), transparent) border-box !important;
}
.global-sidenav .tab.active {
  color: var(--accent) !important;
  background: linear-gradient(90deg, rgba(24, 230, 255, 0.2), transparent) border-box !important;
  border-color: transparent !important;
  box-shadow: inset 2px 0 0 var(--accent);
  text-shadow: 0 0 8px var(--tron-glow);
}

/* Section labels trail a circuit line that fades out to the right. */
.global-sidenav .tab-group-label, .group-head {
  font-family: var(--tron-display) !important;
  letter-spacing: 0.18em !important;
  color: #5fe9ff !important;
  padding-bottom: 3px !important;
  border-bottom: 1px solid;
  border-image: linear-gradient(90deg, rgba(24, 230, 255, 0.7), transparent 70%) 1;
}

/* Neither the heads nor the table rules reach the Policy Studio's document,
   which is set in its own type and has a paper view the colour of a page. */
h1:not(.ps-prosemirror *), .global-content h2:not(.ps-prosemirror *) {
  font-family: var(--tron-display) !important;
  text-transform: uppercase !important;
  letter-spacing: 0.12em !important;
  color: var(--accent) !important;
  text-shadow: 0 0 10px var(--tron-glow);
}
th:not(.ps-prosemirror *), .stat .k, .panel-header, .list-header, .detail-header, .pane-head {
  color: #5fe9ff !important;
  letter-spacing: 0.12em !important;
}
.panel-header, .list-header, .detail-header, .pane-head { border-color: var(--tron-faint) !important; }
tbody tr:not(.ps-prosemirror *):hover { background: rgba(24, 230, 255, 0.06) !important; }

/* Panes are lit at two corners, like the edge of a display in the dark. The
   border is translucent, and the brackets are painted on the border box, so
   they show through it. A card inside a pane keeps the plainer surface the
   layer above gives it, with the more specific rule. */
.panel, .pane, .detail, .list, .stat, .card, .summary-card, .status-card, .detail-card,
.report-card, main:has(#loginForm) {
  --tron-corner: var(--accent);
  border-color: var(--tron-faint) !important;
  border-radius: 2px !important;
  background:
    linear-gradient(var(--tron-corner), var(--tron-corner)) top left / 16px 2px no-repeat border-box,
    linear-gradient(var(--tron-corner), var(--tron-corner)) top left / 2px 16px no-repeat border-box,
    linear-gradient(var(--tron-corner), var(--tron-corner)) bottom right / 16px 2px no-repeat border-box,
    linear-gradient(var(--tron-corner), var(--tron-corner)) bottom right / 2px 16px no-repeat border-box,
    linear-gradient(160deg, rgba(24, 230, 255, 0.05), transparent 40%),
    var(--surface) !important;
  box-shadow: 0 0 22px -6px rgba(24, 230, 255, 0.35), inset 0 0 26px rgba(24, 230, 255, 0.035);
}
/* The operator is the program from outside, and speaks in orange. */
.msg.user { border-left-color: var(--tron-orange) !important; }

/* Buttons glow in whatever shape they are painted — a fill, an outline or bare
   text — because the glow is a filter rather than a box. */
.global-content button, body:not(:has(.global-shell)) main button {
  border-radius: 2px !important;
  letter-spacing: 0.04em;
  filter: drop-shadow(0 0 3px rgba(24, 230, 255, 0.35));
  transition: filter 120ms ease;
}
.global-content button:hover:not(:disabled),
body:not(:has(.global-shell)) main button:hover:not(:disabled) {
  filter: drop-shadow(0 0 8px rgba(24, 230, 255, 0.85));
}
.global-content button:disabled { filter: none; }
.global-content button.secondary, .global-content .ghost-btn { border-color: var(--tron-faint) !important; }
.global-content button.secondary:hover, .global-content .ghost-btn:hover {
  border-color: var(--accent) !important;
  color: var(--accent) !important;
}
.global-content button.danger, .global-content .danger-btn {
  filter: drop-shadow(0 0 4px rgba(255, 122, 26, 0.5));
}
.global-content button.danger:hover:not(:disabled), .global-content .danger-btn:hover:not(:disabled) {
  filter: drop-shadow(0 0 10px rgba(255, 122, 26, 0.95));
}
.global-content a.btn, .global-content a.button, .global-content .button-link { border-radius: 2px !important; }

input, textarea, select { border-radius: 2px !important; }
input:focus, textarea:focus, select:focus {
  border-color: var(--accent) !important;
  box-shadow: 0 0 0 1px var(--accent), 0 0 14px var(--tron-glow);
}

.global-content .tab.active {
  color: var(--accent) !important;
  border-color: var(--tron-faint) !important;
  box-shadow: inset 0 2px 0 var(--accent);
  text-shadow: 0 0 8px var(--tron-glow);
}
.pill, .chip, .tag, .badge { border-color: var(--tron-faint) !important; }

@media (prefers-reduced-motion: reduce) {
  .global-topbar { animation: none; }
  .global-content button { transition: none; }
}`

// lightCyclesTag is the Tron theme's backdrop: a handful of cycles racing
// along the grid tronThemeCSS draws, turning only at right angles and laying a
// wall of light that fades after a few seconds. A cycle that runs into a wall,
// its own or anyone's, derezzes into fragments and comes back in from an edge.
// Most of the time they steer round; sometimes they do not, because a grid
// where nobody ever crashes is a screensaver.
//
// Same terms as the rain: fixed behind the page, inert to the pointer, faded
// so text always wins, injected for this theme only, paused in a background
// tab, and a still frame of the race for anyone who asked for less motion.
// Sized from the canvas's own box, and written without template literals,
// for the reasons matrixRainTag gives.
//
// The settings are per-browser, in localStorage, and can be changed from the
// browser console through window.GRCCycles:
//
//	grc-cycles-count      : how many cycles race (default 4, 0 to 12)
//	grc-cycles-brightness : percentage scaling of the fade (default 100)
const lightCyclesTag = `<canvas id="global-light-cycles" aria-hidden="true"></canvas>
<style id="global-light-cycles-style">
#global-light-cycles {
  position: fixed;
  top: 0;
  left: 0;
  width: 100% !important;
  height: 100% !important;
  max-width: none !important;
  z-index: 0;
  pointer-events: none;
  opacity: 0.42;
}
/* The page rides above the cycles, as it does above the rain. */
main {
  position: relative !important;
  z-index: 1 !important;
}
</style>
<script id="global-light-cycles-engine">
(() => {
  if (window.__globalCyclesInit) return;
  window.__globalCyclesInit = true;

  const canvas = document.getElementById("global-light-cycles");
  if (!canvas || !canvas.getContext) return;

  // One grid cell, in CSS pixels. Has to match the grid's background-size, or
  // the cycles ride between the lines instead of on them.
  const CELL = 40;

  const COUNT_KEY = "grc-cycles-count";
  const BRIGHTNESS_KEY = "grc-cycles-brightness";

  const DEFAULT_COUNT = 4;
  const MIN_COUNT = 0;
  const MAX_COUNT = 12;

  // Brighter than the rain's fade, because a few thin lines cover far less of
  // the screen than a field of glyphs.
  const BASE_OPACITY = 0.42;
  const DEFAULT_BRIGHTNESS = 100;
  const MIN_BRIGHTNESS = 10;
  const MAX_BRIGHTNESS = 230;

  // Cells per second, and how long a wall of light stands.
  const SPEED = 7;
  const TRAIL_LIFE = 5.5;
  // How often a cycle turns of its own accord at a junction, and how often it
  // simply fails to see the wall in front of it.
  const TURN_CHANCE = 0.16;
  const BLIND_CHANCE = 0.25;
  const RESPAWN_DELAY = 1.4;
  const FPS = 30;

  // Mostly the two sides, cyan and orange, and the odd stray in gold.
  const COLOURS = ["24, 230, 255", "255, 122, 26", "24, 230, 255", "255, 122, 26", "255, 214, 74"];
  const DIRS = [[1, 0], [0, 1], [-1, 0], [0, -1]];

  let ctx = null;
  let width = 0;
  let height = 0;
  let cols = 0;
  let rows = 0;
  let cycles = [];
  let shards = [];
  // The walls of cycles that have derezzed and come back: still fading, no
  // longer anybody's.
  let ghosts = [];
  // Cell to the moment its wall goes dark. Read for steering and collisions.
  let walls = new Map();
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

  function count() {
    return clampInt(localStorage.getItem(COUNT_KEY), MIN_COUNT, MAX_COUNT, DEFAULT_COUNT);
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

  const key = (x, y) => x + "," + y;
  // The outermost lines are the arena's wall rather than track: a cycle riding
  // the very edge of the window would have half its light cut off.
  const inside = (x, y) => x >= 1 && y >= 1 && x < cols && y < rows;
  const blocked = (x, y) => !inside(x, y) || (walls.get(key(x, y)) || 0) > clock;

  // spawn brings a cycle in through a random edge, heading inwards.
  function spawn(c, colour) {
    const side = Math.floor(Math.random() * 4);
    const across = (n) => 1 + Math.floor(Math.random() * Math.max(1, n - 1));
    const x = side === 0 ? 0 : side === 2 ? cols : across(cols);
    const y = side === 1 ? 0 : side === 3 ? rows : across(rows);
    const dir = side;
    if (c.trail && c.trail.length > 1) ghosts.push({ colour: c.colour, trail: c.trail });
    Object.assign(c, {
      colour: colour || c.colour, x: x, y: y, dir: dir, step: 0, alive: true, back: 0,
      trail: [{ x: x, y: y, t: clock }],
    });
    return c;
  }

  // derezz breaks a cycle into fragments where it stopped.
  function derezz(c) {
    c.alive = false;
    c.back = clock + RESPAWN_DELAY;
    const px = c.x * CELL;
    const py = c.y * CELL;
    for (let i = 0; i < 26; i++) {
      const a = Math.random() * Math.PI * 2;
      const v = 40 + Math.random() * 160;
      shards.push({
        x: px, y: py, vx: Math.cos(a) * v, vy: Math.sin(a) * v,
        s: 2 + Math.random() * 4, born: clock, life: 0.6 + Math.random() * 0.7, colour: c.colour,
      });
    }
  }

  // arrive runs at each junction: lay the wall, then decide where to go next.
  function arrive(c) {
    walls.set(key(c.x, c.y), clock + TRAIL_LIFE);
    c.trail.push({ x: c.x, y: c.y, t: clock });

    const ahead = (d) => blocked(c.x + DIRS[d][0], c.y + DIRS[d][1]);
    const left = (c.dir + 3) % 4;
    const right = (c.dir + 1) % 4;
    let dir = c.dir;
    if (Math.random() < TURN_CHANCE) dir = Math.random() < 0.5 ? left : right;
    if (ahead(dir)) {
      const open = [c.dir, left, right].filter((d) => !ahead(d));
      // Outside the grid is not a wall anyone can miss; only light is.
      const edge = !inside(c.x + DIRS[dir][0], c.y + DIRS[dir][1]);
      if (open.length && (edge || Math.random() > BLIND_CHANCE)) {
        dir = open[Math.floor(Math.random() * open.length)];
      } else {
        derezz(c);
        return;
      }
    }
    c.dir = dir;
  }

  function advance(dt) {
    clock += dt;
    for (const c of cycles) {
      if (!c.alive && clock >= c.back) arrive(spawn(c));
      c.step += c.alive ? dt * SPEED : 0;
      while (c.alive && c.step >= 1) {
        c.step -= 1;
        c.x += DIRS[c.dir][0];
        c.y += DIRS[c.dir][1];
        arrive(c);
      }
    }
    // Walls that have gone dark are dropped from the back of each trail; the
    // newest point always stays, so a live cycle never loses its tail end.
    for (const t of cycles.concat(ghosts)) {
      while (t.trail.length > 1 && clock - t.trail[0].t > TRAIL_LIFE) t.trail.shift();
    }
    ghosts = ghosts.filter((g) => g.trail.length > 1);
    shards = shards.filter((s) => clock - s.born < s.life);
    // Swept now and then rather than every frame: a stale entry is harmless,
    // because its expiry is checked on every read.
    if (walls.size > 4000) {
      for (const [k, until] of walls) if (until <= clock) walls.delete(k);
    }
  }

  // Each segment is drawn twice, wide and faint then thin and bright, which is
  // the glow without paying for shadowBlur on every stroke.
  function drawTrail(colour, pts) {
    for (let i = 1; i < pts.length; i++) {
      const fade = Math.max(0, 1 - (clock - pts[i].t) / TRAIL_LIFE);
      const a = pts[i - 1];
      const b = pts[i];
      ctx.strokeStyle = rgba(colour, 0.18 * fade);
      ctx.lineWidth = 7;
      ctx.beginPath(); ctx.moveTo(a.x * CELL, a.y * CELL); ctx.lineTo(b.x * CELL, b.y * CELL); ctx.stroke();
      ctx.strokeStyle = rgba(colour, 0.95 * fade);
      ctx.lineWidth = 2;
      ctx.stroke();
    }
  }

  function draw() {
    ctx.clearRect(0, 0, width, height);
    // Butt caps: each segment is stroked on its own, and round or square caps
    // overlap at every junction, beading the wall with brighter dots.
    ctx.lineCap = "butt";

    for (const g of ghosts) drawTrail(g.colour, g.trail);
    for (const c of cycles) {
      const pts = c.trail;
      drawTrail(c.colour, pts);
      if (!c.alive || !pts.length) continue;

      // The stretch from the last junction to the cycle itself, and the cycle.
      const tail = pts[pts.length - 1];
      const hx = (c.x + DIRS[c.dir][0] * c.step) * CELL;
      const hy = (c.y + DIRS[c.dir][1] * c.step) * CELL;
      ctx.strokeStyle = rgba(c.colour, 0.95);
      ctx.lineWidth = 2;
      ctx.beginPath(); ctx.moveTo(tail.x * CELL, tail.y * CELL); ctx.lineTo(hx, hy); ctx.stroke();

      const glow = ctx.createRadialGradient(hx, hy, 0, hx, hy, 16);
      glow.addColorStop(0, "rgba(255, 255, 255, 0.95)");
      glow.addColorStop(0.25, rgba(c.colour, 0.8));
      glow.addColorStop(1, rgba(c.colour, 0));
      ctx.fillStyle = glow;
      ctx.beginPath(); ctx.arc(hx, hy, 16, 0, Math.PI * 2); ctx.fill();
    }

    for (const s of shards) {
      const age = clock - s.born;
      ctx.fillStyle = rgba(s.colour, 1 - age / s.life);
      ctx.fillRect(s.x + s.vx * age, s.y + s.vy * age, s.s, s.s);
    }
  }

  function loop(now) {
    frame = requestAnimationFrame(loop);
    const dt = (now - last) / 1000;
    if (dt < 1 / FPS) return;
    last = now;
    // A long gap, a dropped frame or a wake from sleep, is taken as one frame
    // rather than a jump that would carry every cycle through several walls.
    advance(Math.min(dt, 0.1));
    draw();
  }

  function reset() {
    clock = 0;
    walls = new Map();
    shards = [];
    ghosts = [];
    cycles = [];
    for (let i = 0; i < count(); i++) {
      const c = spawn({}, COLOURS[i % COLOURS.length]);
      // Staggered, so they do not all come in on the same frame.
      c.alive = false;
      c.back = i * 0.6;
      cycles.push(c);
    }
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
    cols = Math.ceil(width / CELL);
    rows = Math.ceil(height / CELL);
    reset();
    return true;
  }

  // paintStill is what prefers-reduced-motion gets: the grid a few seconds
  // into a race, held. Run forward without drawing, then drawn once.
  function paintStill() {
    if (!ctx) return;
    reset();
    for (let i = 0; i < 160; i++) advance(1 / FPS);
    shards = [];
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
    startLoop();
  }

  function start() {
    if (!resize()) return;
    applyBrightness();
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

  // A new count starts a new race rather than adding or removing riders
  // mid-flight.
  window.GRCCycles = {
    config: () => ({ count: count(), brightness: brightness() }),
    setConfig: (next) => {
      localStorage.setItem(COUNT_KEY, String(clampInt(next.count, MIN_COUNT, MAX_COUNT, DEFAULT_COUNT)));
      localStorage.setItem(BRIGHTNESS_KEY,
        String(clampInt(next.brightness, MIN_BRIGHTNESS, MAX_BRIGHTNESS, DEFAULT_BRIGHTNESS)));
      applyBrightness();
      if (!ctx) return;
      if (reduceMotion()) paintStill();
      else reset();
    },
    MIN_COUNT, MAX_COUNT, MIN_BRIGHTNESS, MAX_BRIGHTNESS,
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
</script>`
