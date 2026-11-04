/**
 * Core Lode Runner types — pure data, no React, no DOM.
 *
 * The board is a 28 × 16 grid of cells, row 0 at the top. Everything moves
 * from cell centre to cell centre: an actor either stands on a cell or is on
 * a move to a neighbour, `p` sub-steps of the way there. Until a move ends
 * the actor still counts as being on the cell it left, which keeps support,
 * collisions, digging and the guards' path-finding all on whole cells.
 */

export type Difficulty = "calm" | "classic" | "relentless";

/** What a cell is built of. */
export type Tile =
  | "empty"
  | "brick" // a floor the runner can dig
  | "solid" // stone: never dug
  | "ladder"
  | "bar" // a hand-over-hand rope
  | "trap" // looks like brick and holds nothing up
  | "hidden"; // empty until the last gold is taken, then a ladder

/** A cell as the moving actors meet it: its tile, or an open hole. */
export type Ground = Exclude<Tile, "hidden"> | "hole";

export type Dir = "left" | "right" | "up" | "down";

/** A dig to the runner's lower left (-1) or lower right (1). */
export type Side = -1 | 1;

export interface LevelDef {
  name: string;
  /** Sixteen rows of 28 characters; see `parseLevel` for the alphabet. */
  rows: readonly string[];
}

export interface Level {
  name: string;
  /** Row-major tiles, `r * COLS + c`. */
  tiles: Tile[];
  /** Cells that start with a bar of gold. */
  gold: number[];
  /** Where each guard starts. */
  guards: number[];
  /** Where the runner starts. */
  start: number;
}

export type MoveKind = "run" | "hang" | "climb" | "fall";

export interface Move {
  dc: -1 | 0 | 1;
  dr: -1 | 0 | 1;
  kind: MoveKind;
  /** Sub-steps taken, 0 until SUB. */
  p: number;
}

export interface Actor {
  /** The cell the actor stands on, or leaves on its current move. */
  c: number;
  r: number;
  move: Move | null;
  facing: -1 | 1;
  /** Speed banked between ticks, in sub-steps × TICK_HZ. */
  acc: number;
  /** Sub-steps taken in all, for the walk and climb cycles. */
  steps: number;
}

export interface Runner extends Actor {
  /** The dig in progress: which side, ticks into it. */
  dig: { side: Side; t: number } | null;
}

export interface Guard extends Actor {
  id: number;
  /** The cell it first stood on. */
  home: number;
  carrying: boolean;
  /** Ticks before a carried bar may be put down. */
  carryT: number;
  /** Ticks left stuck in a hole; 0 when free. */
  trapT: number;
  /** Ticks until a killed guard is back; 0 while it is on the board. */
  respawnT: number;
  /** Climbed out of a hole and has not stepped aside yet. */
  climbing: boolean;
}

export type HolePhase = "digging" | "open" | "closing";

export interface Hole {
  cell: number;
  phase: HolePhase;
  /** Ticks into the phase. */
  t: number;
}

export type GameStatus = "play" | "dying" | "dead" | "clear";

/** Something worth a sound or a flash, drained by the view every frame. */
export type GameEvent =
  | { kind: "gold"; cell: number }
  | { kind: "reveal" }
  | { kind: "dig"; cell: number }
  | { kind: "nodig" }
  | { kind: "trap"; cell: number }
  | { kind: "kill"; cell: number }
  | { kind: "respawn"; cell: number }
  | { kind: "fill"; cell: number }
  | { kind: "land" }
  | { kind: "death" }
  | { kind: "clear" };

/** What the person is asking for this tick. */
export interface Input {
  /** Held directions, the most recently pressed first. */
  dirs: readonly Dir[];
}
