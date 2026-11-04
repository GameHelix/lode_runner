import { MAX_LIVES, START_LIVES, TICK_HZ } from "./game/constants";
import { createGame, giveUp, requestDig, step, type Game } from "./game/engine";
import { LEVELS } from "./game/levels";
import type { Difficulty, Dir, GameEvent, Side } from "./game/types";

/*
 * One run: the levels played in order from the one the person picked, on five
 * lives. It owns the live level, turns held keys into the engine's input,
 * steps the engine at a fixed 60 Hz from the view's animation frames, and
 * tells the React side what changed through a tiny external store. It has no
 * React and no DOM, and time only moves while the view calls `frame` — a
 * hidden tab or an open dialog simply stops calling it.
 */

export type Phase = "ready" | "play" | "dying" | "cleared" | "over" | "won";

export type Voice = GameEvent["kind"] | "start" | "over" | "won";

/** A flash on the board — dust, sparkle, a burst — drawn for a moment where it happened. */
export interface Effect {
  kind: "dig" | "gold" | "trap" | "kill" | "fill" | "respawn";
  cell: number;
  /** The run's tick it happened on. */
  t0: number;
}

export interface Cleared {
  level: number;
  ticks: number;
  points: number;
  trapped: number;
  killed: number;
}

/** What the React side renders; replaced, never mutated, when it changes. */
export interface RunView {
  phase: Phase;
  level: number;
  name: string;
  score: number;
  lives: number;
  goldLeft: number;
  goldTotal: number;
  /** Whole seconds into the level. */
  seconds: number;
  cleared: Cleared | null;
}

export interface RunResult {
  won: boolean;
  score: number;
  difficulty: Difficulty;
  start: number;
  /** The level the run ended on. */
  level: number;
  /** Levels cleared during the run. */
  cleared: number;
}

export interface RunHandlers {
  sound: (voice: Voice) => void;
  /** A level was cleared in this many ticks. */
  cleared: (level: number, ticks: number) => void;
  over: (result: RunResult) => void;
}

/** How long the level-clear banner stays before the next level, unless skipped. */
const BANNER_TICKS = 3 * TICK_HZ;
/** Effects older than this are dropped. */
const EFFECT_TICKS = 50;
/** A long gap between frames (a stalled tab) never fast-forwards more than this. */
const MAX_TICKS_PER_FRAME = 8;

export class Run {
  readonly difficulty: Difficulty;
  readonly start: number;
  level: number;
  game: Game;
  lives = START_LIVES;
  phase: Phase = "ready";
  /** Points from levels finished or lost so far; the live level's add to them. */
  private banked = 0;
  private clearedCount = 0;
  private cleared: Cleared | null = null;
  /** Bumped for every new level or retry, so the renderer knows its cached board is stale. */
  serial = 0;
  /** Ticks of the run so far, the clock effects and animations run on. */
  ticks = 0;
  effects: Effect[] = [];

  private readonly seed: number;
  private held: Dir[] = [];
  private bannerT = 0;
  private last = -1;
  private acc = 0;
  private view: RunView;
  private listeners = new Set<() => void>();
  private handlers: RunHandlers = { sound: () => {}, cleared: () => {}, over: () => {} };

  constructor(difficulty: Difficulty, start: number, seed: number) {
    this.difficulty = difficulty;
    this.start = start;
    this.level = start;
    this.seed = seed;
    this.game = this.newGame();
    this.view = this.makeView();
  }

  connect(handlers: RunHandlers): void {
    this.handlers = handlers;
  }

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  getView = (): RunView => this.view;

  /* ------------------------------------------------------------ input */

  /** A direction pressed: it goes to the front of the held keys, and a waiting level starts. */
  press(dir: Dir): void {
    this.held = [dir, ...this.held.filter((d) => d !== dir)];
    if (this.phase === "ready") this.begin();
  }

  release(dir: Dir): void {
    this.held = this.held.filter((d) => d !== dir);
  }

  releaseAll(): void {
    this.held = [];
  }

  dig(side: Side): void {
    if (this.phase === "ready") this.begin();
    if (this.phase === "play") requestDig(this.game, side);
  }

  /** The way out of a spot with no way out: lose this life. */
  giveUp(): void {
    if (this.phase === "play") giveUp(this.game);
  }

  /** Enter, Space or a tap: starts a waiting level, or moves on from the level-clear banner. */
  proceed(): void {
    if (this.phase === "ready") this.begin();
    else if (this.phase === "cleared") this.next();
  }

  /* ------------------------------------------------------------- time */

  /** Called every animation frame with the frame's timestamp, in milliseconds. */
  frame(now: number): void {
    if (this.last < 0) this.last = now;
    this.acc += Math.min(0.25, Math.max(0, (now - this.last) / 1000));
    this.last = now;
    const tick = 1 / TICK_HZ;
    let n = 0;
    while (this.acc >= tick && n < MAX_TICKS_PER_FRAME) {
      this.acc -= tick;
      this.tick();
      n++;
    }
    if (n === MAX_TICKS_PER_FRAME) this.acc = 0;
    this.publish();
  }

  /** After a pause, forget the time that passed while it lasted. */
  resume(): void {
    this.last = -1;
    this.acc = 0;
  }

  private tick(): void {
    if (this.phase === "ready" || this.phase === "over" || this.phase === "won") return;
    this.ticks++;
    if (this.effects.length > 0 && this.ticks - this.effects[0].t0 > EFFECT_TICKS) {
      this.effects = this.effects.filter((e) => this.ticks - e.t0 <= EFFECT_TICKS);
    }
    if (this.phase === "cleared") {
      if (++this.bannerT >= BANNER_TICKS) this.next();
      return;
    }
    step(this.game, { dirs: this.held });
    this.drain();
    const status = this.game.status;
    if (status === "dying") this.phase = "dying";
    else if (status === "dead") this.lose();
    else if (status === "clear") this.win();
  }

  private drain(): void {
    for (const e of this.game.events) {
      this.handlers.sound(e.kind);
      if (e.kind === "dig" || e.kind === "gold" || e.kind === "trap" || e.kind === "kill" || e.kind === "fill" || e.kind === "respawn") {
        this.effects.push({ kind: e.kind, cell: e.cell, t0: this.ticks });
      }
    }
    this.game.events.length = 0;
  }

  /* ----------------------------------------------------------- levels */

  private newGame(): Game {
    this.serial++;
    return createGame(LEVELS[this.level], this.difficulty, `${this.seed}:${this.level}:${this.serial}`);
  }

  private begin(): void {
    this.phase = "play";
    this.handlers.sound("start");
  }

  private lose(): void {
    this.banked += this.game.score;
    this.lives--;
    if (this.lives <= 0) {
      this.finish(false);
      return;
    }
    this.game = this.newGame();
    this.effects = [];
    this.phase = "ready";
  }

  private win(): void {
    this.banked += this.game.score;
    this.clearedCount++;
    this.lives = Math.min(MAX_LIVES, this.lives + 1);
    this.cleared = {
      level: this.level,
      ticks: this.game.time,
      points: this.game.score,
      trapped: this.game.trapped,
      killed: this.game.killed,
    };
    this.phase = "cleared";
    this.bannerT = 0;
    this.handlers.cleared(this.level, this.game.time);
  }

  private next(): void {
    if (this.level + 1 >= LEVELS.length) {
      this.finish(true);
      return;
    }
    this.level++;
    this.game = this.newGame();
    this.effects = [];
    this.cleared = null;
    this.phase = "ready";
  }

  private finish(won: boolean): void {
    this.phase = won ? "won" : "over";
    this.handlers.sound(won ? "won" : "over");
    this.publish();
    this.handlers.over({
      won,
      score: this.banked,
      difficulty: this.difficulty,
      start: this.start,
      level: this.level,
      cleared: this.clearedCount,
    });
  }

  /* ------------------------------------------------------------- view */

  private makeView(): RunView {
    const g = this.game;
    // The live level's points join the bank when it ends, so count them only while it runs.
    const live = this.phase === "ready" || this.phase === "play" || this.phase === "dying";
    return {
      phase: this.phase,
      level: this.level,
      name: LEVELS[this.level].name,
      score: this.banked + (live ? g.score : 0),
      lives: this.lives,
      goldLeft: g.goldLeft,
      goldTotal: g.level.gold.length,
      seconds: Math.floor(g.time / TICK_HZ),
      cleared: this.phase === "cleared" ? this.cleared : null,
    };
  }

  private publish(): void {
    const next = this.makeView();
    const v = this.view;
    if (
      next.phase === v.phase &&
      next.level === v.level &&
      next.score === v.score &&
      next.lives === v.lives &&
      next.goldLeft === v.goldLeft &&
      next.seconds === v.seconds &&
      next.cleared === v.cleared
    ) {
      return;
    }
    this.view = next;
    this.listeners.forEach((fn) => fn());
  }
}
