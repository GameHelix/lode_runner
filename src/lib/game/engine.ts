import { advance, DELTA, fallMove, falls, groundAt, guardIn, holeAt, occupies, position, spring, tryMove } from "./board";
import {
  COLS,
  DEATH_TICKS,
  DIG_TICKS,
  DIG_WAIT_TICKS,
  FALL,
  HOLE_CLOSE_TICKS,
  HOLE_OPEN_TICKS,
  ROWS,
  RUNNER_CLIMB,
  RUNNER_HANG,
  RUNNER_RUN,
  SCORE,
  SUB,
  TOUCH,
} from "./constants";
import { killGuard, updateGuard } from "./guards";
import { cellOf, colOf, rowOf } from "./level";
import { makeRng, type Rng } from "./rng";
import type {
  Actor,
  Difficulty,
  GameEvent,
  GameStatus,
  Guard,
  Hole,
  Input,
  Level,
  Move,
  MoveKind,
  Runner,
  Side,
  Tile,
} from "./types";

/*
 * One level in play: the live board, the runner, the guards and the holes,
 * moved on by `step` one fixed tick at a time. It is plain mutable data with
 * no clock of its own — the view drives it, draws it and drains `events` for
 * sounds — and it is seeded, so the same inputs always play out the same
 * way, which is what the tests lean on.
 */

export interface Game {
  level: Level;
  difficulty: Difficulty;
  /** The live tiles: hidden ladders become ladders once the gold is gone. */
  tiles: Tile[];
  /** 1 where a bar of gold lies. */
  gold: Uint8Array;
  /** Bars the runner has still to take, those the guards carry included. */
  goldLeft: number;
  runner: Runner;
  guards: Guard[];
  holes: Hole[];
  /** A dig asked for between cells, waiting for the runner to arrive. */
  pendingDig: { side: Side; wait: number } | null;
  /** Trap bricks something has dropped through; drawn cracked from then on. */
  sprung: Set<number>;
  /** The tick the hidden ladders showed, or -1. */
  revealedAt: number;
  status: GameStatus;
  /** Ticks of play so far. */
  time: number;
  /** Ticks into the runner's death. */
  deathT: number;
  /** Bumped whenever the board's look changes, so the view can redraw its cached tiles. */
  terrain: number;
  /** Points scored on this level, the clear bonus included. */
  score: number;
  trapped: number;
  killed: number;
  events: GameEvent[];
  rng: Rng;
}

function standing(cell: number): Actor {
  return { c: colOf(cell), r: rowOf(cell), move: null, facing: 1, acc: 0, steps: 0 };
}

export function createGame(level: Level, difficulty: Difficulty, seed: number | string = 1): Game {
  const gold = new Uint8Array(level.tiles.length);
  for (const cell of level.gold) gold[cell] = 1;
  return {
    level,
    difficulty,
    tiles: level.tiles.slice(),
    gold,
    goldLeft: level.gold.length,
    runner: { ...standing(level.start), dig: null },
    guards: level.guards.map((cell, id) => ({
      ...standing(cell),
      id,
      home: cell,
      carrying: false,
      carryT: 0,
      trapT: 0,
      respawnT: 0,
      climbing: false,
    })),
    holes: [],
    pendingDig: null,
    sprung: new Set(),
    revealedAt: -1,
    status: "play",
    time: 0,
    deathT: 0,
    terrain: 0,
    score: 0,
    trapped: 0,
    killed: 0,
    events: [],
    rng: makeRng(seed),
  };
}

const runnerSpeed = (kind: MoveKind): number =>
  kind === "fall" ? FALL : kind === "climb" ? RUNNER_CLIMB : kind === "hang" ? RUNNER_HANG : RUNNER_RUN;

/** Advance the level one tick. */
export function step(g: Game, input: Input): void {
  if (g.status === "dying") {
    if (++g.deathT >= DEATH_TICKS) g.status = "dead";
    return;
  }
  if (g.status !== "play") return;
  g.time++;
  updateHoles(g);
  if (g.status !== "play") return;
  updateRunner(g, input);
  if (g.status !== "play") return;
  for (const o of g.guards) updateGuard(g, o);
  // Gold a trapped guard left on the rim lands in the runner's hands if it stands there.
  if (!g.runner.move && collect(g)) return;
  if (caught(g)) die(g);
}

/** Ask for a dig to one side: at once if the runner stands still, else when it reaches the next cell. */
export function requestDig(g: Game, side: Side): void {
  const a = g.runner;
  if (g.status !== "play" || a.dig) return;
  if (a.move) {
    g.pendingDig = { side, wait: DIG_WAIT_TICKS };
    return;
  }
  startDig(g, side);
}

/** Give up this life: the way out of a spot with no way out. */
export function giveUp(g: Game): void {
  if (g.status === "play") die(g);
}

/**
 * Whether the runner, standing still, could dig the brick at its lower left
 * or right now: plain brick (not stone, not a trap) with open air or a bar
 * above it — no ladder, gold or guard in the way.
 */
export function canDig(g: Game, side: Side): boolean {
  const a = g.runner;
  if (a.move || a.dig || falls(g, a)) return false;
  const c = a.c + side;
  const r = a.r;
  if (c < 0 || c >= COLS || r + 1 >= ROWS) return false;
  const target = cellOf(c, r + 1);
  if (g.tiles[target] !== "brick" || holeAt(g, target)) return false;
  const beside = groundAt(g, c, r);
  if (beside !== "empty" && beside !== "bar") return false;
  return !g.gold[cellOf(c, r)] && !guardIn(g, c, r, null);
}

function startDig(g: Game, side: Side): boolean {
  if (!canDig(g, side)) {
    g.events.push({ kind: "nodig" });
    return false;
  }
  const a = g.runner;
  const cell = cellOf(a.c + side, a.r + 1);
  g.holes.push({ cell, phase: "digging", t: 0 });
  a.dig = { side, t: 0 };
  a.facing = side;
  a.acc = 0;
  g.events.push({ kind: "dig", cell });
  return true;
}

function updateHoles(g: Game): void {
  for (let i = g.holes.length - 1; i >= 0; i--) {
    const h = g.holes[i];
    h.t++;
    if (h.phase === "digging") {
      // A guard stepping onto the brick being dug spoils the dig.
      if (guardIn(g, colOf(h.cell), rowOf(h.cell) - 1, null)) {
        g.holes.splice(i, 1);
        g.runner.dig = null;
        continue;
      }
      if (g.runner.dig) g.runner.dig.t = h.t;
      if (h.t >= DIG_TICKS) {
        h.phase = "open";
        h.t = 0;
        g.runner.dig = null;
        g.terrain++;
      }
    } else if (h.phase === "open") {
      if (h.t >= HOLE_OPEN_TICKS) {
        h.phase = "closing";
        h.t = 0;
      }
    } else if (h.t >= HOLE_CLOSE_TICKS) {
      // The brick is whole again, and whatever was inside is crushed.
      g.holes.splice(i, 1);
      g.terrain++;
      g.events.push({ kind: "fill", cell: h.cell });
      const c = colOf(h.cell);
      const r = rowOf(h.cell);
      if (occupies(g.runner, c, r)) die(g);
      for (const o of g.guards) if (o.respawnT === 0 && occupies(o, c, r)) killGuard(g, o);
    }
  }
}

function updateRunner(g: Game, input: Input): void {
  const a = g.runner;
  if (g.pendingDig && --g.pendingDig.wait < 0) g.pendingDig = null;
  if (a.dig) return;
  turnBack(a, input);
  advance(
    a,
    runnerSpeed,
    () => decideRunner(g, input),
    (finished) => arriveRunner(g, finished)
  );
}

/** Pressing the opposite way mid-move turns back at once instead of finishing the step. */
function turnBack(a: Runner, input: Input): void {
  const m = a.move;
  if (!m || m.kind === "fall" || input.dirs.length === 0) return;
  const [dc, dr] = DELTA[input.dirs[0]];
  if (dc !== -m.dc || dr !== -m.dr) return;
  a.c += m.dc;
  a.r += m.dr;
  m.dc = dc;
  m.dr = dr;
  m.p = SUB - m.p;
  if (dc !== 0) a.facing = dc;
}

function decideRunner(g: Game, input: Input): Move | null {
  const a = g.runner;
  if (a.dig) return null;
  if (falls(g, a)) return fallMove();
  if (g.pendingDig) {
    const { side } = g.pendingDig;
    g.pendingDig = null;
    if (startDig(g, side)) return null;
  }
  for (const dir of input.dirs) {
    const m = tryMove(g, a, dir, null);
    if (m) return m;
  }
  return null;
}

function arriveRunner(g: Game, finished: MoveKind): boolean {
  const a = g.runner;
  const cell = cellOf(a.c, a.r);
  if (g.tiles[cell] === "trap") spring(g, cell);
  if (collect(g)) return true;
  if (finished === "fall" && !falls(g, a)) g.events.push({ kind: "land" });
  return false;
}

/** Take any gold where the runner stands; with none left, the top row is the way out. */
function collect(g: Game): boolean {
  const a = g.runner;
  const cell = cellOf(a.c, a.r);
  if (g.gold[cell]) {
    g.gold[cell] = 0;
    g.goldLeft--;
    g.score += SCORE.gold;
    g.events.push({ kind: "gold", cell });
    if (g.goldLeft === 0) reveal(g);
  }
  if (g.goldLeft > 0 || a.r !== 0) return false;
  g.status = "clear";
  g.score += SCORE.clear;
  g.events.push({ kind: "clear" });
  return true;
}

function reveal(g: Game): void {
  for (let i = 0; i < g.tiles.length; i++) if (g.tiles[i] === "hidden") g.tiles[i] = "ladder";
  g.revealedAt = g.time;
  g.terrain++;
  g.events.push({ kind: "reveal" });
}

function caught(g: Game): boolean {
  const me = position(g.runner);
  for (const o of g.guards) {
    if (o.respawnT > 0) continue;
    const p = position(o);
    if (Math.abs(p.x - me.x) < TOUCH && Math.abs(p.y - me.y) < TOUCH) return true;
  }
  return false;
}

function die(g: Game): void {
  g.status = "dying";
  g.deathT = 0;
  g.pendingDig = null;
  if (g.runner.dig) {
    g.runner.dig = null;
    g.holes = g.holes.filter((h) => h.phase !== "digging");
  }
  g.events.push({ kind: "death" });
}
