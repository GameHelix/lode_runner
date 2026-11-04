import { COLS, ROWS } from "@/lib/game/constants";
import { createGame, step, type Game } from "@/lib/game/engine";
import { cellOf, parseLevel } from "@/lib/game/level";
import type { Difficulty, Dir } from "@/lib/game/types";

/** A row of `fill` with the given characters at the given columns. */
export function row(cells: Record<number, string> = {}, fill = " "): string {
  return Array.from({ length: COLS }, (_, c) => cells[c] ?? fill).join("");
}

/** `ch` in every column from `from` to `to`, inclusive, for `row`. */
export function span(from: number, to: number, ch: string): Record<number, string> {
  const out: Record<number, string> = {};
  for (let c = from; c <= to; c++) out[c] = ch;
  return out;
}

/**
 * A game on a board of the given rows. Missing rows are open air over a
 * brick bottom row, and a bar of gold waits in the far top corner unless the
 * rows bring their own, so a level always parses.
 */
export function game(rows: Record<number, string>, difficulty: Difficulty = "classic"): Game {
  const all = Array.from({ length: ROWS }, (_, r) => rows[r] ?? (r === ROWS - 1 ? row({}, "#") : row()));
  if (!all.some((r) => r.includes("$"))) all[0] = row({ [COLS - 1]: "$" });
  return createGame(parseLevel({ name: "test", rows: all }), difficulty, "test");
}

/** Step `ticks` times holding `dirs`, most recent first. */
export function hold(g: Game, ticks: number, ...dirs: Dir[]): void {
  for (let i = 0; i < ticks; i++) step(g, { dirs });
}

/** Step holding `dirs` until `done` holds; the ticks it took. Fails after `limit`. */
export function until(g: Game, done: () => boolean, dirs: Dir[] = [], limit = 3000): number {
  for (let t = 0; t < limit; t++) {
    if (done()) return t;
    step(g, { dirs });
  }
  throw new Error(`not done after ${limit} ticks`);
}

/** The actor stands still exactly on (c, r). */
export const at = (a: { c: number; r: number; move: unknown }, c: number, r: number): boolean =>
  a.move === null && a.c === c && a.r === r;

export const cell = cellOf;
