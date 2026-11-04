import { describe, expect, it } from "vitest";
import { groundAt, position } from "@/lib/game/board";
import { DEATH_TICKS, DIG_TICKS, HOLE_CLOSE_TICKS, HOLE_OPEN_TICKS, SCORE } from "@/lib/game/constants";
import { canDig, giveUp, requestDig } from "@/lib/game/engine";
import { at, cell, game, hold, row, span, until } from "./helpers";

describe("moving", () => {
  it("runs one whole cell for a tap, then stands", () => {
    const g = game({ 14: row({ 3: "&" }) });
    hold(g, 1, "right");
    expect(until(g, () => at(g.runner, 4, 14))).toBeLessThan(16);
    hold(g, 30);
    expect(at(g.runner, 4, 14)).toBe(true);
  });

  it("runs four and a half cells a second", () => {
    const g = game({ 14: row({ 3: "&" }) });
    const ticks = until(g, () => g.runner.c >= 13, ["right"]);
    expect(ticks).toBeGreaterThanOrEqual(130);
    expect(ticks).toBeLessThanOrEqual(138);
  });

  it("stops at brick and stone", () => {
    const g = game({ 14: row({ 1: "@", 3: "&", 6: "#" }) });
    hold(g, 120, "right");
    expect(at(g.runner, 5, 14)).toBe(true);
    hold(g, 200, "left");
    expect(at(g.runner, 2, 14)).toBe(true);
  });

  it("climbs a ladder and stands on its top", () => {
    const g = game({ 11: row({ 5: "H" }), 12: row({ 5: "H" }), 13: row({ 5: "H" }), 14: row({ 3: "&", 5: "H" }) });
    hold(g, 1, "right");
    until(g, () => at(g.runner, 4, 14));
    hold(g, 1, "right");
    until(g, () => at(g.runner, 5, 14));
    until(g, () => at(g.runner, 5, 10), ["up"]);
    hold(g, 30, "up");
    expect(at(g.runner, 5, 10)).toBe(true);
  });

  it("steps off a ledge, falls, and lands with a thud", () => {
    const g = game({ 9: row({ 3: "&" }), 10: row(span(0, 4, "#")) });
    until(g, () => g.runner.c === 5, ["right"]);
    until(g, () => at(g.runner, 5, 14));
    expect(g.events.some((e) => e.kind === "land")).toBe(true);
  });

  it("catches a bar on the way down, goes hand over hand and lets go with down", () => {
    const g = game({ 9: row({ 3: "&" }), 10: row(span(0, 4, "#")), 12: row(span(5, 8, "-")) });
    until(g, () => g.runner.c === 5, ["right"]);
    until(g, () => at(g.runner, 5, 12));
    hold(g, 30);
    expect(at(g.runner, 5, 12)).toBe(true);
    hold(g, 1, "right");
    until(g, () => at(g.runner, 6, 12));
    expect(g.runner.r).toBe(12);
    hold(g, 1, "down");
    until(g, () => at(g.runner, 6, 14));
  });

  it("turns back at once mid-step", () => {
    const g = game({ 14: row({ 5: "&" }) });
    hold(g, 6, "right");
    expect(position(g.runner).x).toBeGreaterThan(5.3);
    hold(g, 6, "left");
    expect(position(g.runner).x).toBeLessThan(5.05);
  });

  it("drops through a trap brick, which then shows cracked", () => {
    const g = game({ 9: row({ 3: "&" }), 10: row({ 5: "X" }, "#") });
    until(g, () => g.runner.c === 5, ["right"]);
    until(g, () => at(g.runner, 5, 14));
    expect(g.sprung.has(cell(5, 10))).toBe(true);
  });
});

describe("digging", () => {
  it("opens the brick at the runner's lower side, keeps it open, then grows it back", () => {
    const g = game({ 9: row({ 3: "&" }), 10: row({}, "#") });
    expect(canDig(g, 1)).toBe(true);
    requestDig(g, 1);
    expect(g.runner.dig).not.toBeNull();
    hold(g, DIG_TICKS);
    expect(groundAt(g, 4, 10)).toBe("hole");
    expect(g.runner.dig).toBeNull();
    hold(g, HOLE_OPEN_TICKS + HOLE_CLOSE_TICKS - 1);
    expect(groundAt(g, 4, 10)).toBe("hole");
    hold(g, 1);
    expect(groundAt(g, 4, 10)).toBe("brick");
    expect(g.events.some((e) => e.kind === "fill")).toBe(true);
  });

  it("lets the runner drop through the floor it dug", () => {
    const g = game({ 9: row({ 3: "&" }), 10: row({}, "#") });
    requestDig(g, 1);
    hold(g, DIG_TICKS);
    hold(g, 1, "right");
    until(g, () => at(g.runner, 4, 14));
  });

  it("refuses stone, traps, and bricks under gold, a ladder or a guard", () => {
    const cases = [
      game({ 14: row({ 3: "&" }), 15: row({ 4: "@" }, "#") }),
      game({ 14: row({ 3: "&" }), 15: row({ 4: "X" }, "#") }),
      game({ 14: row({ 3: "&", 4: "$" }) }),
      game({ 14: row({ 3: "&", 4: "H" }) }),
      game({ 14: row({ 3: "&", 4: "0" }) }),
    ];
    for (const g of cases) {
      expect(canDig(g, 1)).toBe(false);
      requestDig(g, 1);
      expect(g.events.at(-1)?.kind).toBe("nodig");
      expect(g.holes).toHaveLength(0);
    }
  });

  it("refuses in mid-air", () => {
    const g = game({ 5: row({ 3: "&" }) });
    expect(canDig(g, 1)).toBe(false);
  });

  it("crushes the runner if the brick grows back around it", () => {
    const g = game({ 14: row({ 3: "&" }) });
    requestDig(g, 1);
    hold(g, DIG_TICKS);
    hold(g, 1, "right");
    until(g, () => at(g.runner, 4, 15));
    until(g, () => g.status === "dying");
    expect(g.events.some((e) => e.kind === "death")).toBe(true);
    hold(g, DEATH_TICKS);
    expect(g.status).toBe("dead");
  });

  it("waits for the runner to reach the next cell when asked mid-step", () => {
    const g = game({ 14: row({ 3: "&" }) });
    hold(g, 4, "right");
    requestDig(g, 1);
    expect(g.runner.dig).toBeNull();
    until(g, () => g.runner.dig !== null);
    expect(at(g.runner, 4, 14)).toBe(true);
    expect(g.holes[0].cell).toBe(cell(5, 15));
  });
});

describe("gold and the way out", () => {
  it("shows the hidden ladders with the last bar of gold, and ends the level on the top row", () => {
    const rows: Record<number, string> = {};
    for (let r = 0; r < 14; r++) rows[r] = row({ 1: "S" });
    rows[14] = row({ 1: "S", 3: "&", 5: "$" });
    const g = game(rows);
    until(g, () => g.goldLeft === 0, ["right"]);
    expect(g.score).toBe(SCORE.gold);
    expect(g.tiles[cell(1, 0)]).toBe("ladder");
    expect(g.events.some((e) => e.kind === "reveal")).toBe(true);
    until(g, () => g.status === "clear", ["up", "left"]);
    expect(g.runner.r).toBe(0);
    expect(g.score).toBe(SCORE.gold + SCORE.clear);
  });

  it("keeps the level going on the top row while gold is left", () => {
    const g = game({ 0: row({ 3: "&" }), 1: row({}, "#"), 14: row({ 9: "$" }) });
    hold(g, 60, "right");
    expect(g.status).toBe("play");
  });

  it("costs a life at once when the runner gives up", () => {
    const g = game({ 14: row({ 3: "&" }) });
    giveUp(g);
    expect(g.status).toBe("dying");
  });
});
