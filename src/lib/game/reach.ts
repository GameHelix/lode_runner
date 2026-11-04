import { COLS, ROWS } from "./constants";
import { cellOf, colOf, rowOf } from "./level";
import type { Ground, Level, Tile } from "./types";

/*
 * Whether a level can be finished at all, guards aside. The runner's moves
 * form a graph over cells — walking, climbing, hanging, falling, and digging
 * a brick to drop through it — that is not symmetric, since falls and digs
 * only go down. Gold in one strongly connected part can be taken in any
 * order, so the gold is collectable exactly when its parts, with the start's,
 * lie along one chain of the graph's condensation; and the level ends when,
 * from the last of them, the top row is in reach once the hidden ladders show.
 */

export interface LevelCheck {
  ok: boolean;
  problems: string[];
}

const N = COLS * ROWS;

/** A tile as the runner meets it, before or after the hidden ladders show; `dug` is a brick standing open. */
function ground(tiles: readonly Tile[], revealed: boolean, c: number, r: number, dug: number): Ground {
  if (c < 0 || c >= COLS || r < 0 || r >= ROWS) return "solid";
  const cell = cellOf(c, r);
  if (cell === dug) return "hole";
  const t = tiles[cell];
  if (t === "hidden") return revealed ? "ladder" : "empty";
  return t;
}

/** Where the runner can get to in one move (a dig counts as one) from `u`. */
export function runnerMoves(tiles: readonly Tile[], revealed: boolean, u: number): number[] {
  const c = colOf(u);
  const r = rowOf(u);
  // The runner is only ever on a brick cell when it dug its way in.
  const dug = tiles[u] === "brick" ? u : -1;
  const at = (cc: number, rr: number) => ground(tiles, revealed, cc, rr, dug);
  const here = at(c, r);
  const below = at(c, r + 1);
  const out: number[] = [];
  if (here !== "ladder" && here !== "bar" && below !== "brick" && below !== "solid" && below !== "ladder") {
    out.push(cellOf(c, r + 1));
    return out;
  }
  for (const dc of [-1, 1]) {
    const side = at(c + dc, r);
    if (side !== "brick" && side !== "solid") out.push(cellOf(c + dc, r));
  }
  const above = at(c, r - 1);
  if (here === "ladder" && above !== "brick" && above !== "solid") out.push(cellOf(c, r - 1));
  if (below !== "brick" && below !== "solid" && (here === "ladder" || below === "ladder" || here === "bar")) {
    out.push(cellOf(c, r + 1));
  }
  // Dig a lower side, step over and drop into the hole.
  for (const dc of [-1, 1]) {
    const bc = c + dc;
    if (bc < 0 || bc >= COLS || r + 1 >= ROWS) continue;
    if (tiles[cellOf(bc, r + 1)] !== "brick") continue;
    const beside = at(bc, r);
    if (beside === "empty" || beside === "bar") out.push(cellOf(bc, r + 1));
  }
  return out;
}

/** Every cell reachable from `from`. */
function reachable(tiles: readonly Tile[], revealed: boolean, from: number): Uint8Array {
  const seen = new Uint8Array(N);
  const stack = [from];
  seen[from] = 1;
  while (stack.length > 0) {
    const u = stack.pop()!;
    for (const v of runnerMoves(tiles, revealed, u)) {
      if (!seen[v]) {
        seen[v] = 1;
        stack.push(v);
      }
    }
  }
  return seen;
}

/** Strongly connected components of the cells `inside`, by Kosaraju's two passes. */
function components(tiles: readonly Tile[], inside: Uint8Array): Int32Array {
  const adj: number[][] = [];
  const radj: number[][] = Array.from({ length: N }, () => []);
  for (let u = 0; u < N; u++) {
    adj[u] = inside[u] ? runnerMoves(tiles, false, u).filter((v) => inside[v]) : [];
    for (const v of adj[u]) radj[v].push(u);
  }
  const order: number[] = [];
  const seen = new Uint8Array(N);
  for (let s = 0; s < N; s++) {
    if (!inside[s] || seen[s]) continue;
    // Iterative depth-first search, recording each cell as it finishes.
    const stack: [number, number][] = [[s, 0]];
    seen[s] = 1;
    while (stack.length > 0) {
      const top = stack[stack.length - 1];
      const [u, i] = top;
      if (i < adj[u].length) {
        top[1]++;
        const v = adj[u][i];
        if (!seen[v]) {
          seen[v] = 1;
          stack.push([v, 0]);
        }
      } else {
        order.push(u);
        stack.pop();
      }
    }
  }
  const comp = new Int32Array(N).fill(-1);
  let count = 0;
  for (let k = order.length - 1; k >= 0; k--) {
    const s = order[k];
    if (comp[s] !== -1) continue;
    const stack = [s];
    comp[s] = count;
    while (stack.length > 0) {
      const u = stack.pop()!;
      for (const v of radj[u]) {
        if (comp[v] === -1) {
          comp[v] = count;
          stack.push(v);
        }
      }
    }
    count++;
  }
  return comp;
}

export function checkLevel(level: Level): LevelCheck {
  const problems: string[] = [];
  const { tiles, start } = level;
  const where = (cell: number) => `column ${colOf(cell)}, row ${rowOf(cell)}`;

  const fromStart = reachable(tiles, false, start);
  for (const cell of level.gold) if (!fromStart[cell]) problems.push(`gold at ${where(cell)} cannot be reached`);
  if (problems.length > 0) return { ok: false, problems };

  const comp = components(tiles, fromStart);
  // One cell per part that holds gold, plus the start's part.
  const reps = new Map<number, number>([[comp[start], start]]);
  for (const cell of level.gold) if (!reps.has(comp[cell])) reps.set(comp[cell], cell);
  const parts = [...reps.keys()];
  const reaches = new Map<number, Set<number>>();
  for (const [part, cell] of reps) {
    const seen = reachable(tiles, false, cell);
    reaches.set(part, new Set(parts.filter((other) => seen[reps.get(other)!])));
  }
  // The parts must form a chain: of any two, one leads to the other.
  for (let i = 0; i < parts.length; i++) {
    for (let j = i + 1; j < parts.length; j++) {
      const a = parts[i];
      const b = parts[j];
      if (!reaches.get(a)!.has(b) && !reaches.get(b)!.has(a)) {
        problems.push(`gold at ${where(reps.get(a)!)} and at ${where(reps.get(b)!)} cannot both be taken`);
      }
    }
  }
  if (problems.length > 0) return { ok: false, problems };

  // The last part is the one every other part leads to.
  const last = parts.reduce((best, p) => (reaches.get(p)!.size < reaches.get(best)!.size ? p : best));
  for (const cell of level.gold) {
    if (comp[cell] !== last) continue;
    const seen = reachable(tiles, true, cell);
    let out = false;
    for (let c = 0; c < COLS && !out; c++) out = seen[cellOf(c, 0)] === 1;
    if (!out) problems.push(`no way to the top row after the gold at ${where(cell)}`);
  }
  return { ok: problems.length === 0, problems };
}
