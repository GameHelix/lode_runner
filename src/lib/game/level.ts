import { COLS, ROWS } from "./constants";
import type { Level, LevelDef, Tile } from "./types";

/*
 * The level alphabet, one character per cell:
 *
 *   ' ' empty      '#' brick     '@' stone     'H' ladder     '-' bar
 *   'X' trap       'S' a ladder that appears once the gold is gone
 *   '$' gold       '0' a guard   '&' the runner
 *
 * Gold, guards and the runner all stand on empty cells.
 */
const TILE_OF: Record<string, Tile> = {
  " ": "empty",
  "#": "brick",
  "@": "solid",
  H: "ladder",
  "-": "bar",
  X: "trap",
  S: "hidden",
  $: "empty",
  "0": "empty",
  "&": "empty",
};

export const cellOf = (c: number, r: number): number => r * COLS + c;
export const colOf = (cell: number): number => cell % COLS;
export const rowOf = (cell: number): number => Math.floor(cell / COLS);

/** Turn a level's rows into tiles, gold, guards and a start, refusing anything malformed. */
export function parseLevel(def: LevelDef): Level {
  if (def.rows.length !== ROWS) throw new Error(`${def.name}: ${def.rows.length} rows, not ${ROWS}`);
  const tiles: Tile[] = [];
  const gold: number[] = [];
  const guards: number[] = [];
  let start = -1;

  def.rows.forEach((row, r) => {
    if (row.length !== COLS) throw new Error(`${def.name}: row ${r} has ${row.length} cells, not ${COLS}`);
    for (let c = 0; c < COLS; c++) {
      const ch = row[c];
      const tile = TILE_OF[ch];
      if (tile === undefined) throw new Error(`${def.name}: unknown cell "${ch}" at column ${c}, row ${r}`);
      tiles.push(tile);
      const cell = cellOf(c, r);
      if (ch === "$") gold.push(cell);
      else if (ch === "0") guards.push(cell);
      else if (ch === "&") {
        if (start !== -1) throw new Error(`${def.name}: more than one runner`);
        start = cell;
      }
    }
  });

  if (start === -1) throw new Error(`${def.name}: no runner`);
  if (gold.length === 0) throw new Error(`${def.name}: no gold`);
  return { name: def.name, tiles, gold, guards, start };
}
