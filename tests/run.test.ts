import { describe, expect, it } from "vitest";
import { DEATH_TICKS, MAX_LIVES, SCORE, START_LIVES, TICK_HZ } from "@/lib/game/constants";
import { cellOf } from "@/lib/game/level";
import { LEVELS } from "@/lib/game/levels";
import { Run, type RunResult, type Voice } from "@/lib/run";

/** Feed the run animation frames for `seconds` of wall time. */
function frames(run: Run, seconds: number, from = { t: 0 }): void {
  const step = 1000 / TICK_HZ;
  for (let i = 0; i < seconds * TICK_HZ; i++) {
    from.t += step;
    run.frame(from.t);
  }
}

function connect(run: Run) {
  const voices: Voice[] = [];
  const cleared: [number, number][] = [];
  const results: RunResult[] = [];
  run.connect({
    sound: (v) => voices.push(v),
    cleared: (level, ticks) => cleared.push([level, ticks]),
    over: (r) => results.push(r),
  });
  return { voices, cleared, results };
}

/** Put the runner one rung below the top of a revealed ladder with no gold left. */
function readyToEscape(run: Run): void {
  const g = run.game;
  g.gold.fill(0);
  g.goldLeft = 0;
  const col = g.tiles.findIndex((t) => t === "hidden");
  for (let i = 0; i < g.tiles.length; i++) if (g.tiles[i] === "hidden") g.tiles[i] = "ladder";
  g.runner.c = col;
  g.runner.r = 1;
  g.runner.move = null;
  for (const o of g.guards) o.respawnT = 10_000;
  expect(g.tiles[cellOf(col, 1)]).toBe("ladder");
}

describe("a run", () => {
  it("waits on each level until the person moves", () => {
    const run = new Run("classic", 0, 1);
    const { voices } = connect(run);
    const clock = { t: 0 };
    frames(run, 1, clock);
    expect(run.getView().phase).toBe("ready");
    expect(run.game.time).toBe(0);
    run.press("right");
    frames(run, 1, clock);
    expect(run.getView().phase).toBe("play");
    expect(run.game.time).toBeGreaterThan(50);
    expect(voices).toContain("start");
  });

  it("banks a cleared level, adds a life, shows the banner, then moves on", () => {
    const run = new Run("classic", 0, 1);
    const { cleared } = connect(run);
    const clock = { t: 0 };
    run.press("up");
    readyToEscape(run);
    frames(run, 1, clock);
    const view = run.getView();
    expect(view.phase).toBe("cleared");
    expect(view.cleared?.level).toBe(0);
    expect(view.score).toBe(SCORE.clear);
    expect(view.lives).toBe(START_LIVES + 1);
    expect(cleared).toHaveLength(1);
    run.release("up");
    run.proceed();
    frames(run, 0.1, clock);
    expect(run.getView().phase).toBe("ready");
    expect(run.getView().level).toBe(1);
    expect(run.getView().name).toBe(LEVELS[1].name);
    expect(run.getView().score).toBe(SCORE.clear);
  });

  it("moves on by itself once the banner has shown long enough", () => {
    const run = new Run("calm", 0, 1);
    connect(run);
    const clock = { t: 0 };
    run.press("up");
    readyToEscape(run);
    frames(run, 1, clock);
    run.release("up");
    expect(run.getView().phase).toBe("cleared");
    frames(run, 3.2, clock);
    expect(run.getView().phase).toBe("ready");
    expect(run.level).toBe(1);
  });

  it("never holds more than the most lives", () => {
    const run = new Run("classic", 0, 1);
    connect(run);
    run.lives = MAX_LIVES;
    run.press("up");
    readyToEscape(run);
    frames(run, 1);
    expect(run.getView().lives).toBe(MAX_LIVES);
  });

  it("restarts the level on a lost life, and ends the run on the last one", () => {
    const run = new Run("classic", 3, 1);
    const { results } = connect(run);
    const clock = { t: 0 };
    for (let life = START_LIVES; life > 0; life--) {
      run.press("left");
      run.release("left");
      run.giveUp();
      frames(run, (DEATH_TICKS + 5) / TICK_HZ, clock);
      if (life > 1) {
        expect(run.getView().phase).toBe("ready");
        expect(run.getView().lives).toBe(life - 1);
        expect(run.getView().goldLeft).toBe(LEVELS[3].gold.length);
      }
    }
    expect(run.getView().phase).toBe("over");
    expect(results).toEqual([{ won: false, score: 0, difficulty: "classic", start: 3, level: 3, cleared: 0 }]);
  });

  it("wins after the last level", () => {
    const run = new Run("relentless", LEVELS.length - 1, 1);
    const { results } = connect(run);
    run.press("up");
    readyToEscape(run);
    frames(run, 1);
    run.proceed();
    frames(run, 0.1);
    expect(run.getView().phase).toBe("won");
    expect(results[0]).toMatchObject({ won: true, cleared: 1, score: SCORE.clear, level: LEVELS.length - 1 });
  });

  it("forgets the time a pause took", () => {
    const run = new Run("classic", 0, 1);
    connect(run);
    run.press("right");
    run.frame(0);
    run.frame(16);
    const before = run.game.time;
    run.resume();
    run.frame(60_000);
    expect(run.game.time).toBe(before);
  });
});
