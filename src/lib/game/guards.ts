import {
  advance,
  DELTA,
  fallMove,
  falls,
  footing,
  groundAt,
  guardIn,
  holeAt,
  isBlock,
  nearestCell,
  spring,
  tryMove,
} from "./board";
import { CARRY_TICKS, COLS, FALL, GUARD_PACE, RESPAWN_TICKS, ROWS, SCORE } from "./constants";
import type { Game } from "./engine";
import { cellOf, colOf, rowOf } from "./level";
import type { Dir, Guard, Move, MoveKind } from "./types";

/*
 * The guards. Each one walks the same rules as the runner and chases it by
 * the shortest way the board allows right now — a breadth-first search over
 * cells, falls included as one-way steps — with ties broken toward the
 * runner, so they come at it the way a person would expect. They cannot
 * dig, they pick up gold they walk over and put it down again later, and a
 * hole with a floor holds one until it climbs out or the brick grows back
 * around it.
 */

const DIRS: readonly Dir[] = ["left", "right", "up", "down"];
const DIR_INDEX: Record<Dir, number> = { left: 0, right: 1, up: 2, down: 3 };

// Scratch space for the search, reused by every guard on every decision.
const stamp = new Int32Array(COLS * ROWS);
const first = new Int8Array(COLS * ROWS);
const queue = new Int16Array(COLS * ROWS);
const scratch: Dir[] = ["left", "left", "left", "left"];
let generation = 0;

function speedOf(g: Game): (kind: MoveKind) => number {
  const pace = GUARD_PACE[g.difficulty];
  return (kind) => (kind === "fall" ? FALL : kind === "climb" ? pace.climb : kind === "hang" ? pace.hang : pace.run);
}

export function updateGuard(g: Game, o: Guard): void {
  if (o.respawnT > 0) {
    if (--o.respawnT === 0) comeBack(g, o);
    return;
  }
  if (o.carrying && o.carryT > 0) o.carryT--;
  if (o.trapT > 0) {
    if (--o.trapT > 0) return;
    // Time to climb out — unless something sits on the way, then try again soon.
    if (isBlock(groundAt(g, o.c, o.r - 1)) || guardIn(g, o.c, o.r - 1, o)) {
      o.trapT = 10;
      return;
    }
    o.move = { dc: 0, dr: -1, kind: "climb", p: 0 };
    o.climbing = true;
  }
  advance(
    o,
    speedOf(g),
    () => decide(g, o),
    () => arrive(g, o)
  );
}

function decide(g: Game, o: Guard): Move | null {
  if (o.climbing) {
    // Out of a hole: step off its edge at once, or it would drop straight back in.
    o.climbing = false;
    const toward: Dir = g.runner.c < o.c ? "left" : "right";
    const step = tryMove(g, o, toward, o) ?? tryMove(g, o, toward === "left" ? "right" : "left", o);
    if (step) return step;
  }
  if (falls(g, o)) return fallMove();
  dropGold(g, o);
  return chase(g, o);
}

function arrive(g: Game, o: Guard): boolean {
  const cell = cellOf(o.c, o.r);
  if (g.tiles[cell] === "trap") spring(g, cell);
  const hole = holeAt(g, cell);
  if (hole && footing(g, o.c, o.r, o)) {
    trap(g, o, cell);
    return true;
  }
  // A guard climbing out does not take back the gold it left on the rim.
  if (g.gold[cell] && !o.carrying && !hole && !o.climbing) {
    g.gold[cell] = 0;
    o.carrying = true;
    o.carryT = g.rng.int(CARRY_TICKS[0], CARRY_TICKS[1]);
  }
  return false;
}

function trap(g: Game, o: Guard, cell: number): void {
  o.trapT = GUARD_PACE[g.difficulty].trap;
  g.trapped++;
  g.score += SCORE.trap;
  g.events.push({ kind: "trap", cell });
  if (!o.carrying) return;
  // Its gold stays on the rim of the hole, where the runner can take it.
  const above = cell - COLS;
  if (o.r > 0 && g.tiles[above] === "empty" && !g.gold[above] && !holeAt(g, above)) {
    g.gold[above] = 1;
    o.carrying = false;
  }
}

function dropGold(g: Game, o: Guard): void {
  if (!o.carrying || o.carryT > 0) return;
  const cell = cellOf(o.c, o.r);
  if (g.tiles[cell] !== "empty" || g.gold[cell] || holeAt(g, cell)) return;
  g.gold[cell] = 1;
  o.carrying = false;
}

/** The brick grew back around a guard: it scores, and comes back at the top a moment later. */
export function killGuard(g: Game, o: Guard): void {
  g.killed++;
  g.score += SCORE.kill;
  g.events.push({ kind: "kill", cell: cellOf(o.c, o.r) });
  o.respawnT = RESPAWN_TICKS;
  o.trapT = 0;
  o.move = null;
  o.acc = 0;
  o.climbing = false;
}

/** Back on the board: open air in the top two rows, well away from the runner. */
function comeBack(g: Game, o: Guard): void {
  const options: number[] = [];
  for (let r = 0; r < 2; r++) {
    for (let c = 0; c < COLS; c++) {
      const cell = cellOf(c, r);
      if (g.tiles[cell] !== "empty" || g.gold[cell] || holeAt(g, cell) || guardIn(g, c, r, o)) continue;
      if (Math.abs(c - g.runner.c) < 5 && g.runner.r < 5) continue;
      options.push(cell);
    }
  }
  let cell = o.home;
  if (options.length > 0) cell = g.rng.pick(options);
  else if (Math.abs(colOf(cell) - g.runner.c) < 3 && Math.abs(rowOf(cell) - g.runner.r) < 3) {
    o.respawnT = 15; // nowhere safe yet; look again shortly
    return;
  }
  o.c = colOf(cell);
  o.r = rowOf(cell);
  o.facing = g.runner.c < o.c ? -1 : 1;
  g.events.push({ kind: "respawn", cell });
}

/** Directions to try, best first: toward the runner along its longer gap, then the shorter. */
function preference(g: Game, o: Guard): Dir[] {
  const dx = g.runner.c - o.c;
  const dy = g.runner.r - o.r;
  const h: Dir = dx < 0 ? "left" : "right";
  const v: Dir = dy < 0 ? "up" : "down";
  const hBack: Dir = h === "left" ? "right" : "left";
  const vBack: Dir = v === "up" ? "down" : "up";
  return Math.abs(dx) >= Math.abs(dy) ? [h, v, vBack, hBack] : [v, h, hBack, vBack];
}

/** The moves a guard could make from (c, r), other guards aside, in `order`. */
function exits(g: Game, c: number, r: number, order: readonly Dir[], out: Dir[]): number {
  const here = groundAt(g, c, r);
  const below = groundAt(g, c, r + 1);
  const floor = below === "brick" || below === "solid" || below === "ladder";
  if (here !== "ladder" && here !== "bar" && !floor) {
    out[0] = "down";
    return 1;
  }
  if (here === "hole") {
    // Stuck until it climbs out of the top.
    if (isBlock(groundAt(g, c, r - 1))) return 0;
    out[0] = "up";
    return 1;
  }
  let n = 0;
  for (const dir of order) {
    const [dc, dr] = DELTA[dir];
    const there = groundAt(g, c + dc, r + dr);
    if (isBlock(there)) continue;
    if (dr < 0 && here !== "ladder") continue;
    if (dr > 0 && here !== "ladder" && there !== "ladder" && here !== "bar") continue;
    out[n++] = dir;
  }
  return n;
}

/** The first step of a shortest way from `from` to `to`, or null if there is none. */
export function route(g: Game, from: number, to: number, order: readonly Dir[]): Dir | null {
  if (from === to) return null;
  generation++;
  stamp[from] = generation;
  queue[0] = from;
  let head = 0;
  let tail = 1;
  while (head < tail) {
    const u = queue[head++];
    const c = u % COLS;
    const r = (u - c) / COLS;
    const n = exits(g, c, r, order, scratch);
    for (let i = 0; i < n; i++) {
      const [dc, dr] = DELTA[scratch[i]];
      const v = cellOf(c + dc, r + dr);
      if (stamp[v] === generation) continue;
      stamp[v] = generation;
      first[v] = u === from ? DIR_INDEX[scratch[i]] : first[u];
      if (v === to) return DIRS[first[v]];
      queue[tail++] = v;
    }
  }
  return null;
}

/** Head for the runner; when no way leads there, drift toward it. */
function chase(g: Game, o: Guard): Move | null {
  const order = preference(g, o);
  const best = route(g, cellOf(o.c, o.r), nearestCell(g.runner), order);
  if (best) {
    const m = tryMove(g, o, best, o);
    if (m) return m;
  }
  // Blocked by another guard, or cut off: any opening, best first.
  for (const dir of order) {
    if (dir === best) continue;
    const m = tryMove(g, o, dir, o);
    if (m) return m;
  }
  return null;
}
