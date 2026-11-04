import { position } from "@/lib/game/board";
import { COLS, DIG_TICKS, HOLE_CLOSE_TICKS, ROWS, SUB, TICK_HZ } from "@/lib/game/constants";
import type { Game } from "@/lib/game/engine";
import { cellOf, colOf, rowOf } from "@/lib/game/level";
import type { Actor, Guard, Tile } from "@/lib/game/types";
import type { Run } from "@/lib/run";
import { drawFigure, type FigureStyle, type Pose } from "./figures";

/*
 * Draws a run on a 2D canvas. The board's tiles go once into a cached layer,
 * rebuilt only when the terrain changes (a hole opens or closes, a trap
 * cracks, the hidden ladders show); every frame then lays the holes, the
 * gold, the guards, the runner and the passing flashes over it. Everything
 * is drawn in cell units through one transform. When the board is too wide
 * for the screen to show it at a playable size, a camera follows the runner.
 */

const C = {
  bg: "#070610",
  dot: "rgba(148, 163, 184, 0.07)",
  brick: "#2a1050",
  mortar: "rgba(192, 132, 252, 0.5)",
  brickEdge: "#e879f9",
  stone: "#172033",
  stoneHi: "rgba(148, 163, 184, 0.55)",
  stoneEdge: "#38bdf8",
  ladder: "#22d3ee",
  bar: "#f472b6",
  pit: "#020106",
  gold: "#fbbf24",
  goldHi: "#fef3c7",
  goldDeep: "#b45309",
  exit: "#34d399",
};

const RUNNER: FigureStyle = { stroke: "#e0f2fe", glow: "#22d3ee", visor: null };
const GUARD: FigureStyle = { stroke: "#fb7185", glow: "#f43f5e", visor: "#fde68a" };

/** Below this many CSS pixels a cell, the view zooms in and follows the runner. */
const MIN_TILE = 20;
/** Zoomed in, the view still shows at least this many columns. */
const MIN_VIEW_COLS = 18;

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

/** A small hash for deterministic per-cell wobble in the effects. */
function noise(n: number): number {
  const s = Math.sin(n * 12.9898) * 43758.5453;
  return s - Math.floor(s);
}

export class BoardRenderer {
  private readonly canvas: HTMLCanvasElement;
  private readonly ctx: CanvasRenderingContext2D;
  private readonly layer: HTMLCanvasElement;
  private layerKey = "";
  private dpr = 1;
  private cssW = 0;
  private cssH = 0;
  /** CSS pixels per cell. */
  private tile = 0;
  /** Cells visible across and down. */
  private viewW = COLS;
  private viewH = ROWS;
  /** The view's top-left in cells: negative when the whole board fits and sits centred. */
  private camX = 0;
  private camY = 0;
  private snap = true;
  private lastNow = -1;
  private readonly reducedMotion: boolean;

  constructor(canvas: HTMLCanvasElement) {
    this.canvas = canvas;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("2D canvas unavailable");
    this.ctx = ctx;
    this.layer = document.createElement("canvas");
    this.reducedMotion =
      typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;
  }

  resize(cssW: number, cssH: number, dpr: number): void {
    this.cssW = Math.max(1, cssW);
    this.cssH = Math.max(1, cssH);
    this.dpr = dpr;
    this.canvas.width = Math.round(this.cssW * dpr);
    this.canvas.height = Math.round(this.cssH * dpr);
    const fit = Math.min(this.cssW / COLS, this.cssH / ROWS);
    this.tile = fit >= MIN_TILE ? fit : Math.max(fit, Math.min(this.cssH / ROWS, this.cssW / MIN_VIEW_COLS));
    this.viewW = this.cssW / this.tile;
    this.viewH = this.cssH / this.tile;
    this.layerKey = "";
    this.snap = true;
  }

  /** Whether the board is wider or taller than the view, so the camera moves. */
  get scrolls(): boolean {
    return this.viewW < COLS - 0.01 || this.viewH < ROWS - 0.01;
  }

  draw(run: Run, now: number): void {
    if (this.tile <= 0) return;
    const g = run.game;
    const dt = this.lastNow < 0 ? 0 : Math.min(0.1, (now - this.lastNow) / 1000);
    this.lastNow = now;
    this.follow(g, dt);

    const { ctx, dpr, tile } = this;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.fillStyle = C.bg;
    ctx.fillRect(0, 0, this.canvas.width, this.canvas.height);

    this.ensureLayer(run);
    const scale = dpr * tile;
    // Whole device pixels, so the cached tiles stay crisp as the camera glides.
    const tx = Math.round(-this.camX * scale);
    const ty = Math.round(-this.camY * scale);
    ctx.drawImage(this.layer, tx, ty);
    ctx.setTransform(scale, 0, 0, scale, tx, ty);

    const clock = run.ticks;
    this.drawHoles(g);
    this.drawRevealGlow(g);
    this.drawGold(g, now);
    for (const o of g.guards) this.drawGuard(g, o, clock);
    this.drawRunner(run, g, clock);
    this.drawEffects(run);
  }

  /* ----------------------------------------------------------- camera */

  private follow(g: Game, dt: number): void {
    const p = position(g.runner);
    const tx = this.viewW >= COLS ? (COLS - this.viewW) / 2 : clamp(p.x + 0.5 - this.viewW / 2, 0, COLS - this.viewW);
    const ty = this.viewH >= ROWS ? (ROWS - this.viewH) / 2 : clamp(p.y + 0.5 - this.viewH / 2, 0, ROWS - this.viewH);
    if (this.snap || this.reducedMotion) {
      this.camX = tx;
      this.camY = ty;
      this.snap = false;
      return;
    }
    const k = 1 - Math.exp(-dt * 7);
    this.camX += (tx - this.camX) * k;
    this.camY += (ty - this.camY) * k;
  }

  /* ------------------------------------------------------ cached tiles */

  private ensureLayer(run: Run): void {
    const g = run.game;
    const key = `${run.serial}:${g.terrain}:${this.tile}:${this.dpr}`;
    if (key === this.layerKey) return;
    this.layerKey = key;
    const scale = this.dpr * this.tile;
    this.layer.width = Math.ceil(COLS * scale);
    this.layer.height = Math.ceil(ROWS * scale);
    const ctx = this.layer.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(scale, 0, 0, scale, 0, 0);
    ctx.clearRect(0, 0, COLS, ROWS);

    // Board edge and a faint dot at every cell corner.
    ctx.fillStyle = "rgba(15, 12, 32, 0.9)";
    ctx.fillRect(0, 0, COLS, ROWS);
    ctx.fillStyle = C.dot;
    for (let r = 1; r < ROWS; r++) for (let c = 1; c < COLS; c++) ctx.fillRect(c - 0.03, r - 0.03, 0.06, 0.06);

    const open = new Set(g.holes.filter((h) => h.phase !== "digging").map((h) => h.cell));
    const glow = Math.max(2, this.tile * this.dpr * 0.35);
    for (let r = 0; r < ROWS; r++) {
      for (let c = 0; c < COLS; c++) {
        const cell = cellOf(c, r);
        if (open.has(cell)) continue;
        const t = g.tiles[cell];
        if (t === "brick" || (t === "trap" && !g.sprung.has(cell))) this.brick(ctx, c, r);
        else if (t === "trap") this.sprungTrap(ctx, c, r);
        else if (t === "solid") this.stone(ctx, c, r);
      }
    }
    // Lit edges where brick and stone meet open air: one glowing pass.
    ctx.save();
    ctx.lineCap = "round";
    ctx.lineWidth = 0.07;
    for (const [kind, color] of [
      ["brick", C.brickEdge],
      ["solid", C.stoneEdge],
    ] as const) {
      ctx.strokeStyle = color;
      ctx.shadowColor = color;
      ctx.shadowBlur = glow;
      ctx.beginPath();
      for (let r = 0; r < ROWS; r++) {
        for (let c = 0; c < COLS; c++) {
          const cell = cellOf(c, r);
          if (open.has(cell) || !this.isKind(g, cell, kind)) continue;
          if (r > 0 && this.isAir(g, open, c, r - 1)) {
            ctx.moveTo(c + 0.04, r + 0.04);
            ctx.lineTo(c + 0.96, r + 0.04);
          }
          if (c > 0 && this.isAir(g, open, c - 1, r)) {
            ctx.moveTo(c + 0.04, r + 0.04);
            ctx.lineTo(c + 0.04, r + 0.96);
          }
          if (c < COLS - 1 && this.isAir(g, open, c + 1, r)) {
            ctx.moveTo(c + 0.96, r + 0.04);
            ctx.lineTo(c + 0.96, r + 0.96);
          }
        }
      }
      ctx.stroke();
    }
    ctx.restore();

    // Ladders and bars glow on top.
    ctx.save();
    ctx.lineCap = "round";
    ctx.shadowBlur = glow;
    ctx.strokeStyle = C.ladder;
    ctx.shadowColor = C.ladder;
    ctx.lineWidth = 0.08;
    ctx.beginPath();
    for (let cell = 0; cell < g.tiles.length; cell++) {
      if (g.tiles[cell] !== "ladder") continue;
      const c = colOf(cell);
      const r = rowOf(cell);
      ctx.moveTo(c + 0.22, r);
      ctx.lineTo(c + 0.22, r + 1);
      ctx.moveTo(c + 0.78, r);
      ctx.lineTo(c + 0.78, r + 1);
      ctx.moveTo(c + 0.22, r + 0.3);
      ctx.lineTo(c + 0.78, r + 0.3);
      ctx.moveTo(c + 0.22, r + 0.78);
      ctx.lineTo(c + 0.78, r + 0.78);
    }
    ctx.stroke();
    ctx.strokeStyle = C.bar;
    ctx.shadowColor = C.bar;
    ctx.lineWidth = 0.07;
    ctx.beginPath();
    for (let cell = 0; cell < g.tiles.length; cell++) {
      if (g.tiles[cell] !== "bar") continue;
      const c = colOf(cell);
      const r = rowOf(cell);
      ctx.moveTo(c, r + 0.1);
      ctx.lineTo(c + 1, r + 0.1);
    }
    ctx.stroke();
    ctx.restore();
  }

  private isKind(g: Game, cell: number, kind: "brick" | "solid"): boolean {
    const t = g.tiles[cell];
    if (kind === "solid") return t === "solid";
    return t === "brick" || (t === "trap" && !g.sprung.has(cell));
  }

  /** Open air for the lit edges: anything but brick, stone and an unsprung trap. */
  private isAir(g: Game, open: Set<number>, c: number, r: number): boolean {
    const cell = cellOf(c, r);
    if (open.has(cell)) return true;
    const t: Tile = g.tiles[cell];
    return !(t === "brick" || t === "solid" || (t === "trap" && !g.sprung.has(cell)));
  }

  private brick(ctx: CanvasRenderingContext2D, c: number, r: number): void {
    ctx.fillStyle = C.brick;
    ctx.fillRect(c + 0.03, r + 0.03, 0.94, 0.94);
    ctx.strokeStyle = C.mortar;
    ctx.lineWidth = 0.05;
    ctx.beginPath();
    ctx.moveTo(c + 0.03, r + 0.5);
    ctx.lineTo(c + 0.97, r + 0.5);
    ctx.moveTo(c + 0.5, r + 0.03);
    ctx.lineTo(c + 0.5, r + 0.5);
    ctx.moveTo(c + 0.22, r + 0.5);
    ctx.lineTo(c + 0.22, r + 0.97);
    ctx.moveTo(c + 0.78, r + 0.5);
    ctx.lineTo(c + 0.78, r + 0.97);
    ctx.stroke();
  }

  private sprungTrap(ctx: CanvasRenderingContext2D, c: number, r: number): void {
    ctx.save();
    ctx.globalAlpha = 0.45;
    this.brick(ctx, c, r);
    ctx.restore();
    ctx.strokeStyle = C.brickEdge;
    ctx.lineWidth = 0.05;
    ctx.setLineDash([0.12, 0.1]);
    ctx.strokeRect(c + 0.06, r + 0.06, 0.88, 0.88);
    ctx.setLineDash([]);
    ctx.beginPath();
    ctx.moveTo(c + 0.3, r + 0.1);
    ctx.lineTo(c + 0.46, r + 0.42);
    ctx.lineTo(c + 0.38, r + 0.62);
    ctx.lineTo(c + 0.58, r + 0.92);
    ctx.stroke();
  }

  private stone(ctx: CanvasRenderingContext2D, c: number, r: number): void {
    ctx.fillStyle = C.stone;
    ctx.fillRect(c + 0.02, r + 0.02, 0.96, 0.96);
    ctx.strokeStyle = C.stoneHi;
    ctx.lineWidth = 0.05;
    ctx.beginPath();
    ctx.moveTo(c + 0.08, r + 0.92);
    ctx.lineTo(c + 0.08, r + 0.08);
    ctx.lineTo(c + 0.92, r + 0.08);
    ctx.stroke();
    ctx.fillStyle = "rgba(148, 163, 184, 0.16)";
    ctx.fillRect(c + 0.3, r + 0.3, 0.4, 0.4);
  }

  /* ------------------------------------------------------ every frame */

  private drawHoles(g: Game): void {
    const ctx = this.ctx;
    for (const h of g.holes) {
      const c = colOf(h.cell);
      const r = rowOf(h.cell);
      if (h.phase === "digging") {
        // The brick crumbles from the top down as the dig goes on.
        const f = Math.min(1, h.t / DIG_TICKS);
        ctx.fillStyle = C.pit;
        ctx.fillRect(c + 0.03, r + 0.03, 0.94, 0.94 * f);
        continue;
      }
      ctx.fillStyle = C.pit;
      ctx.fillRect(c, r, 1, 1);
      ctx.strokeStyle = "rgba(192, 132, 252, 0.35)";
      ctx.lineWidth = 0.05;
      ctx.strokeRect(c + 0.05, r + 0.05, 0.9, 0.9);
      if (h.phase === "closing") {
        // The brick grows back from the bottom.
        const f = Math.min(1, h.t / HOLE_CLOSE_TICKS);
        ctx.save();
        ctx.beginPath();
        ctx.rect(c, r + 1 - f, 1, f);
        ctx.clip();
        this.brick(ctx, c, r);
        ctx.restore();
      }
    }
  }

  private drawRevealGlow(g: Game): void {
    if (g.revealedAt < 0) return;
    const ctx = this.ctx;
    const since = g.time - g.revealedAt;
    const pulse = 0.55 + 0.45 * Math.sin(since / 7);
    ctx.save();
    ctx.lineCap = "round";
    ctx.shadowColor = C.exit;
    ctx.shadowBlur = Math.max(4, this.tile * this.dpr * 0.6);
    // The new ladders flare when they appear, and the way out keeps pulsing.
    if (since < TICK_HZ * 1.5) {
      ctx.globalAlpha = 1 - since / (TICK_HZ * 1.5);
      ctx.strokeStyle = "#ffffff";
      ctx.lineWidth = 0.12;
      ctx.beginPath();
      g.level.tiles.forEach((t, cell) => {
        if (t !== "hidden") return;
        const c = colOf(cell);
        const r = rowOf(cell);
        ctx.moveTo(c + 0.22, r);
        ctx.lineTo(c + 0.22, r + 1);
        ctx.moveTo(c + 0.78, r);
        ctx.lineTo(c + 0.78, r + 1);
      });
      ctx.stroke();
    }
    ctx.globalAlpha = this.reducedMotion ? 0.9 : pulse;
    ctx.fillStyle = C.exit;
    for (let c = 0; c < COLS; c++) {
      if (g.level.tiles[c] !== "hidden") continue;
      ctx.beginPath();
      ctx.moveTo(c + 0.5, 0.08);
      ctx.lineTo(c + 0.78, 0.4);
      ctx.lineTo(c + 0.22, 0.4);
      ctx.closePath();
      ctx.fill();
    }
    ctx.restore();
  }

  private drawGold(g: Game, now: number): void {
    const ctx = this.ctx;
    ctx.save();
    ctx.shadowColor = C.gold;
    ctx.shadowBlur = Math.max(3, this.tile * this.dpr * 0.45);
    for (let cell = 0; cell < g.gold.length; cell++) {
      if (!g.gold[cell]) continue;
      this.ingot(ctx, colOf(cell), rowOf(cell), now, cell);
    }
    ctx.restore();
  }

  private ingot(ctx: CanvasRenderingContext2D, c: number, r: number, now: number, seed: number): void {
    const grad = ctx.createLinearGradient(c, r + 0.6, c, r + 0.95);
    grad.addColorStop(0, C.goldHi);
    grad.addColorStop(0.35, C.gold);
    grad.addColorStop(1, C.goldDeep);
    ctx.fillStyle = grad;
    ctx.beginPath();
    ctx.moveTo(c + 0.3, r + 0.6);
    ctx.lineTo(c + 0.7, r + 0.6);
    ctx.lineTo(c + 0.86, r + 0.94);
    ctx.lineTo(c + 0.14, r + 0.94);
    ctx.closePath();
    ctx.fill();
    if (this.reducedMotion) return;
    // A glint that sweeps across now and then, out of step from bar to bar.
    const t = ((now / 1000 + noise(seed) * 3) % 3) / 3;
    if (t < 0.25) {
      const x = c + 0.18 + (t / 0.25) * 0.64;
      ctx.save();
      ctx.shadowBlur = 0;
      ctx.strokeStyle = "rgba(255, 255, 255, 0.85)";
      ctx.lineWidth = 0.05;
      ctx.beginPath();
      ctx.moveTo(x, r + 0.92);
      ctx.lineTo(x + 0.1, r + 0.62);
      ctx.stroke();
      ctx.restore();
    }
  }

  private pose(g: Game, a: Actor, digging: boolean): Pose {
    if (digging) return "dig";
    const m = a.move;
    if (m) return m.kind === "run" ? "run" : m.kind;
    const t = g.tiles[cellOf(a.c, a.r)];
    if (t === "bar") return "hang";
    if (t === "ladder") return "climb";
    return "stand";
  }

  private glowPx(): number {
    return Math.max(3, this.tile * this.dpr * 0.4);
  }

  private drawGuard(g: Game, o: Guard, clock: number): void {
    if (o.respawnT > 0) return;
    const p = position(o);
    const ctx = this.ctx;
    let x = p.x;
    let pose = this.pose(g, o, false);
    if (o.trapT > 0) {
      pose = "trapped";
      // Struggling in the hole, harder as it gets ready to climb out.
      if (!this.reducedMotion) x += Math.sin(clock * (o.trapT < 40 ? 1.4 : 0.6)) * 0.05;
    }
    drawFigure(ctx, x, p.y, pose, Math.floor(o.steps / (SUB / 2)), o.facing, GUARD, this.glowPx());
    if (o.carrying) {
      ctx.save();
      ctx.shadowColor = C.gold;
      ctx.shadowBlur = this.glowPx();
      ctx.fillStyle = C.gold;
      ctx.fillRect(x + 0.42, p.y + 0.42, 0.16, 0.12);
      ctx.restore();
    }
  }

  private drawRunner(run: Run, g: Game, clock: number): void {
    const a = g.runner;
    const p = position(a);
    const ctx = this.ctx;
    if (g.status === "dying" || g.status === "dead") {
      // A flash, then the figure fades as a ring spreads.
      const f = Math.min(1, g.deathT / 60);
      ctx.save();
      ctx.globalAlpha = Math.max(0, 1 - f);
      drawFigure(ctx, p.x, p.y, "fall", 0, a.facing, { ...RUNNER, stroke: f < 0.2 ? "#ffffff" : RUNNER.stroke }, this.glowPx() * (1 + f * 3));
      ctx.globalAlpha = Math.max(0, 0.8 - f * 0.8);
      ctx.strokeStyle = "#fb7185";
      ctx.lineWidth = 0.06;
      ctx.beginPath();
      ctx.arc(p.x + 0.5, p.y + 0.5, 0.3 + f * 1.6, 0, Math.PI * 2);
      ctx.stroke();
      ctx.restore();
      return;
    }
    // While a level waits to start, the runner breathes so it is easy to find.
    if (run.phase === "ready" && !this.reducedMotion) {
      ctx.save();
      ctx.globalAlpha = 0.35 + 0.25 * Math.sin(clock / 6 + performance.now() / 300);
      ctx.strokeStyle = RUNNER.glow;
      ctx.lineWidth = 0.06;
      ctx.beginPath();
      ctx.arc(p.x + 0.5, p.y + 0.5, 0.75, 0, Math.PI * 2);
      ctx.stroke();
      ctx.restore();
    }
    const frame = Math.floor(a.steps / (SUB / 2));
    drawFigure(ctx, p.x, p.y, this.pose(g, a, a.dig !== null), frame, a.facing, RUNNER, this.glowPx());
  }

  private drawEffects(run: Run): void {
    const ctx = this.ctx;
    for (const e of run.effects) {
      const age = run.ticks - e.t0;
      const c = colOf(e.cell);
      const r = rowOf(e.cell);
      const f = age / 50;
      if (f >= 1) continue;
      ctx.save();
      ctx.globalAlpha = 1 - f;
      switch (e.kind) {
        case "dig":
        case "fill": {
          // Dust kicked up from the brick.
          ctx.fillStyle = e.kind === "dig" ? "#c084fc" : "#a78bfa";
          for (let i = 0; i < 6; i++) {
            const n = noise(e.cell * 7 + i + e.t0);
            const dx = (n - 0.5) * 1.2 * f;
            const dy = -0.6 * f + f * f * 0.9;
            ctx.fillRect(c + 0.5 + dx + (i - 3) * 0.08, r + 0.1 + dy, 0.07, 0.07);
          }
          break;
        }
        case "gold":
        case "trap":
        case "kill": {
          const color = e.kind === "gold" ? C.gold : e.kind === "trap" ? "#f472b6" : "#fb7185";
          const text = e.kind === "gold" ? "+250" : "+75";
          ctx.fillStyle = color;
          ctx.shadowColor = color;
          ctx.shadowBlur = this.glowPx();
          ctx.font = "bold 0.42px 'Share Tech Mono', ui-monospace, monospace";
          ctx.textAlign = "center";
          ctx.fillText(text, c + 0.5, r + 0.2 - f * 0.9);
          if (e.kind !== "trap") {
            ctx.strokeStyle = color;
            ctx.lineWidth = 0.05;
            ctx.beginPath();
            ctx.arc(c + 0.5, r + 0.7, 0.2 + f * 0.7, 0, Math.PI * 2);
            ctx.stroke();
          }
          break;
        }
        case "respawn": {
          ctx.strokeStyle = "#fb7185";
          ctx.lineWidth = 0.06;
          ctx.beginPath();
          ctx.arc(c + 0.5, r + 0.5, 1.1 - f, 0, Math.PI * 2);
          ctx.stroke();
          break;
        }
      }
      ctx.restore();
    }
  }
}
