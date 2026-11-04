import type { Difficulty } from "./types";

/** The classic screen: 28 columns by 16 rows. Below row 15 is solid ground. */
export const COLS = 28;
export const ROWS = 16;

/** Sub-steps from one cell centre to the next; every move is SUB of them. */
export const SUB = 12;

/** Simulation ticks per second; the view runs a fixed-step loop at this rate. */
export const TICK_HZ = 60;

/*
 * Speeds, in sub-steps per second (SUB of them make a cell). The runner
 * covers 4.5 cells a second on the flat; everyone falls at five.
 */
export const RUNNER_RUN = 54;
export const RUNNER_CLIMB = 48;
export const RUNNER_HANG = 46;
export const FALL = 60;

export interface GuardPace {
  run: number;
  climb: number;
  hang: number;
  /** Ticks a guard stays stuck in a hole before it climbs out. */
  trap: number;
}

/** The guards set the difficulty: how fast they move, how soon they climb out. */
export const GUARD_PACE: Record<Difficulty, GuardPace> = {
  calm: { run: 32, climb: 28, hang: 28, trap: 200 },
  classic: { run: 40, climb: 35, hang: 34, trap: 160 },
  relentless: { run: 47, climb: 42, hang: 40, trap: 125 },
};

/** Ticks to dig a hole, during which the runner stands still. */
export const DIG_TICKS = 18;
/** Ticks a hole stays open, then ticks it takes to grow back. */
export const HOLE_OPEN_TICKS = 360;
export const HOLE_CLOSE_TICKS = 36;
/** A dig asked for in mid-move waits this long for the runner to arrive. */
export const DIG_WAIT_TICKS = 14;

/** Ticks before a killed guard comes back at the top of the board. */
export const RESPAWN_TICKS = 70;
/** A guard holds a bar of gold for this many ticks (inclusive range) before it may drop it. */
export const CARRY_TICKS: readonly [number, number] = [150, 540];

/** Length of the runner's death, before the level starts over. */
export const DEATH_TICKS = 80;

/**
 * A guard touches the runner when their centres are closer than this, in
 * cells, along both axes — so standing on a guard's head is safe.
 */
export const TOUCH = 0.7;

export const SCORE = { gold: 250, trap: 75, kill: 75, clear: 1500 } as const;
export const START_LIVES = 5;
export const MAX_LIVES = 9;

/** Menu order, gentlest first. */
export const DIFFICULTIES: readonly Difficulty[] = ["calm", "classic", "relentless"];

export const DIFFICULTY_LABEL: Record<Difficulty, string> = {
  calm: "Calm",
  classic: "Classic",
  relentless: "Relentless",
};

export const DIFFICULTY_NOTE: Record<Difficulty, string> = {
  calm: "Slow guards that sit in holes a while.",
  classic: "Guards at the arcade pace.",
  relentless: "Guards nearly as quick as you.",
};

/** localStorage key for the menu choices and the sound switch. */
export const PREFS_KEY = "lode_runner.prefs.v1";
/** localStorage key for cleared levels, best times and best scores. */
export const RECORDS_KEY = "lode_runner.records.v1";
