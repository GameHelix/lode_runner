import { COLS, ROWS, SUB, TICK_HZ } from "./constants";
import { cellOf } from "./level";
import type { Actor, Dir, Ground, Guard, Hole, Move, MoveKind, Tile } from "./types";

/*
 * The movement rules every actor shares, on a live board: what a cell is
 * made of right now (holes included), what holds an actor up, and which
 * moves a cell allows. The runner, the guards and their path-finding all
 * ask these same questions, so nobody walks by different laws.
 */

/** The parts of a game the rules read. */
export interface BoardState {
  tiles: Tile[];
  holes: Hole[];
  guards: Guard[];
}

export const DELTA: Record<Dir, readonly [-1 | 0 | 1, -1 | 0 | 1]> = {
  left: [-1, 0],
  right: [1, 0],
  up: [0, -1],
  down: [0, 1],
};

export function holeAt(b: BoardState, cell: number): Hole | undefined {
  for (const h of b.holes) if (h.cell === cell) return h;
  return undefined;
}

/**
 * What an actor meets at (c, r). Beside, below and above the board is stone,
 * so nothing walks off the edge or climbs past row 0. A hole counts once it
 * is dug; a hidden ladder is empty air until it shows.
 */
export function groundAt(b: BoardState, c: number, r: number): Ground {
  if (c < 0 || c >= COLS || r < 0 || r >= ROWS) return "solid";
  const cell = cellOf(c, r);
  const hole = holeAt(b, cell);
  if (hole && hole.phase !== "digging") return "hole";
  const t = b.tiles[cell];
  return t === "hidden" ? "empty" : t;
}

export const isBlock = (g: Ground): boolean => g === "brick" || g === "solid";

/** The cell an actor counts as standing on: the one it is leaving, mid-move. */
export function sameCell(a: Actor, c: number, r: number): boolean {
  return a.c === c && a.r === r;
}

/** The actor stands on (c, r) or is on its way into it. */
export function occupies(a: Actor, c: number, r: number): boolean {
  if (a.c === c && a.r === r) return true;
  return a.move !== null && a.c + a.move.dc === c && a.r + a.move.dr === r;
}

/** A guard other than `self` is on (c, r) or moving into it. */
export function guardIn(b: BoardState, c: number, r: number, self: Actor | null): boolean {
  for (const o of b.guards) if (o !== self && o.respawnT === 0 && occupies(o, c, r)) return true;
  return false;
}

/** Something under (c, r) holds an actor up: brick, stone, a ladder, the bottom of the board, or a guard's head. */
export function footing(b: BoardState, c: number, r: number, self: Actor): boolean {
  const below = groundAt(b, c, r + 1);
  if (below === "brick" || below === "solid" || below === "ladder") return true;
  for (const o of b.guards) if (o !== self && o.respawnT === 0 && sameCell(o, c, r + 1)) return true;
  return false;
}

/** Off a ladder and a bar, with nothing underfoot, an actor falls. */
export function falls(b: BoardState, a: Actor): boolean {
  const here = groundAt(b, a.c, a.r);
  if (here === "ladder" || here === "bar") return false;
  return !footing(b, a.c, a.r, a);
}

export const fallMove = (): Move => ({ dc: 0, dr: 1, kind: "fall", p: 0 });

/** Something dropped through a trap brick: from now on it is drawn cracked. */
export function spring(b: { sprung: Set<number>; terrain: number }, cell: number): void {
  if (b.sprung.has(cell)) return;
  b.sprung.add(cell);
  b.terrain++;
}

/**
 * The move `dir` from where `a` stands, or null if the board forbids it.
 * Sideways needs an open cell (hand over hand on a bar); up needs a ladder
 * here; down needs a ladder here or below, or a bar to let go of. A guard
 * also never steps where another guard is.
 */
export function tryMove(b: BoardState, a: Actor, dir: Dir, guard: Guard | null): Move | null {
  const { c, r } = a;
  const here = groundAt(b, c, r);
  const [dc, dr] = DELTA[dir];
  const there = groundAt(b, c + dc, r + dr);
  if (isBlock(there)) return null;
  if (guard && guardIn(b, c + dc, r + dr, guard)) return null;
  if (dr === 0) return { dc, dr, kind: here === "bar" ? "hang" : "run", p: 0 };
  if (dr < 0) return here === "ladder" ? { dc, dr, kind: "climb", p: 0 } : null;
  if (here === "ladder" || there === "ladder") return { dc, dr, kind: "climb", p: 0 };
  return here === "bar" ? { dc, dr, kind: "fall", p: 0 } : null;
}

/**
 * Walk an actor forward one tick. Its speed (sub-steps a second, by the kind
 * of move) is banked until it makes whole sub-steps; whenever a move ends,
 * `arrive` runs for the new cell (true stops the actor there) and `decide`
 * picks the next move, so a fast actor can finish one move and start the
 * next in the same tick without losing time.
 */
export function advance(
  a: Actor,
  speed: (kind: MoveKind) => number,
  decide: () => Move | null,
  arrive: (finished: MoveKind) => boolean
): void {
  let m = a.move ?? decide();
  if (!m) {
    a.acc = 0;
    return;
  }
  if (m.dc !== 0) a.facing = m.dc;
  a.move = m;
  a.acc += speed(m.kind);
  while (a.acc >= TICK_HZ) {
    a.acc -= TICK_HZ;
    m.p++;
    a.steps++;
    if (m.p < SUB) continue;
    a.c += m.dc;
    a.r += m.dr;
    a.move = null;
    if (arrive(m.kind)) {
      a.acc = 0;
      return;
    }
    const next = decide();
    if (!next) {
      a.acc = 0;
      return;
    }
    if (next.dc !== 0) a.facing = next.dc;
    a.move = m = next;
  }
}

/** Where an actor is drawn and touches others, in cells. */
export function position(a: Actor): { x: number; y: number } {
  if (!a.move) return { x: a.c, y: a.r };
  const f = Math.min(1, a.move.p / SUB);
  return { x: a.c + a.move.dc * f, y: a.r + a.move.dr * f };
}

/** The cell an actor is closest to, mid-move or not. */
export function nearestCell(a: Actor): number {
  if (a.move && a.move.p * 2 > SUB) return cellOf(a.c + a.move.dc, a.r + a.move.dr);
  return cellOf(a.c, a.r);
}
