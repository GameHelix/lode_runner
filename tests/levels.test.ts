import { describe, expect, it } from "vitest";
import { COLS, ROWS } from "@/lib/game/constants";
import { parseLevel } from "@/lib/game/level";
import { LEVEL_DEFS, LEVELS } from "@/lib/game/levels";
import { checkLevel } from "@/lib/game/reach";

describe("the levels", () => {
  it("are twelve, with distinct names", () => {
    expect(LEVELS).toHaveLength(12);
    expect(new Set(LEVELS.map((l) => l.name)).size).toBe(LEVELS.length);
  });

  it.each(LEVEL_DEFS.map((d, i) => [i + 1, d.name, d] as const))("%i. %s is a full board", (_n, _name, def) => {
    expect(def.rows).toHaveLength(ROWS);
    for (const row of def.rows) expect(row).toHaveLength(COLS);
    const level = parseLevel(def);
    expect(level.gold.length).toBeGreaterThan(0);
    expect(level.guards.length).toBeLessThanOrEqual(6);
  });

  it.each(LEVELS.map((l, i) => [i + 1, l.name, l] as const))("%i. %s can be finished", (_n, _name, level) => {
    expect(checkLevel(level).problems).toEqual([]);
  });

  it("each end in a hidden ladder that reaches the top row", () => {
    for (const level of LEVELS) {
      expect(level.tiles.slice(0, COLS)).toContain("hidden");
    }
  });
});

describe("checkLevel", () => {
  /** A row of `fill` with the given characters at the given columns. */
  const row = (cells: Record<number, string> = {}, fill = " ") =>
    Array.from({ length: COLS }, (_, c) => cells[c] ?? fill).join("");
  /** A board of the given rows over a brick floor; missing rows are open air. */
  const board = (rows: Record<number, string>) =>
    parseLevel({
      name: "test",
      rows: Array.from({ length: ROWS }, (_, r) => rows[r] ?? (r === ROWS - 1 ? row({}, "#") : row())),
    });
  /** A hidden ladder at column 0 from row 0 down to row `to`. */
  const exit = (to: number, rows: Record<number, string>) => {
    const out = { ...rows };
    for (let r = 0; r <= to; r++) out[r] = (out[r] ?? row()).replace(/^./, "S");
    return out;
  };

  it("accepts a walk to the gold with a ladder out", () => {
    const level = board(exit(14, { 14: row({ 4: "&", 13: "$" }) }));
    expect(checkLevel(level)).toEqual({ ok: true, problems: [] });
  });

  it("finds gold that cannot be reached", () => {
    const level = board(exit(14, { 4: row({ 12: "$" }), 5: row({ 10: "@", 11: "@", 12: "@", 13: "@", 14: "@" }), 14: row({ 4: "&" }) }));
    const check = checkLevel(level);
    expect(check.ok).toBe(false);
    expect(check.problems[0]).toMatch(/cannot be reached/);
  });

  it("knows a dig drops the runner through a floor but never back up", () => {
    // The gold lies under a brick floor; digging reaches it, and nothing climbs back.
    const level = board(exit(3, { 3: row({ 3: "&" }), 4: row({}, "#"), 14: row({ 8: "$" }) }));
    const check = checkLevel(level);
    expect(check.ok).toBe(false);
    expect(check.problems[0]).toMatch(/no way to the top row/);
  });

  it("refuses gold that only an impossible order could take", () => {
    // Two pits with stone floors: whichever the runner drops into, it never leaves.
    const pit = (fill: string, mid: string) => row({ 4: mid, 22: mid }, fill);
    const level = board(
      exit(3, { 3: row({ 12: "&" }), 4: pit("#", " "), 5: pit("@", " "), 6: pit("@", "$"), 7: row({}, "@") })
    );
    const check = checkLevel(level);
    expect(check.ok).toBe(false);
    expect(check.problems[0]).toMatch(/cannot both be taken/);
  });
});
