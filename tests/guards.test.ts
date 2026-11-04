import { describe, expect, it } from "vitest";
import { SCORE } from "@/lib/game/constants";
import { requestDig, step } from "@/lib/game/engine";
import { route } from "@/lib/game/guards";
import { at, cell, game, hold, row, span, until } from "./helpers";

describe("guards", () => {
  it("walk toward the runner", () => {
    const g = game({ 14: row({ 2: "&", 20: "0" }) });
    hold(g, 90);
    expect(g.guards[0].c).toBeLessThan(18);
  });

  it("climb a ladder to get at a runner on a ledge", () => {
    const g = game({
      9: row({ 8: "&" }),
      10: row({ ...span(5, 15, "#"), 13: "H" }),
      11: row({ 13: "H" }),
      12: row({ 13: "H" }),
      13: row({ 13: "H" }),
      14: row({ 13: "H", 20: "0" }),
    });
    until(g, () => g.guards[0].r === 9, [], 1000);
  });

  it("drop into a hole and stay stuck, so the runner can cross their heads", () => {
    const g = game({ 14: row({ 3: "&", 9: "0" }) });
    requestDig(g, 1);
    const guard = g.guards[0];
    until(g, () => guard.trapT > 0);
    expect(at(guard, 4, 15)).toBe(true);
    expect(g.trapped).toBe(1);
    expect(g.score).toBe(SCORE.trap);
    until(g, () => g.runner.c === 12, ["right"]);
    expect(g.status).toBe("play");
    // In time it climbs out and steps off the rim, toward the runner.
    until(g, () => guard.r === 14 && guard.c === 5);
    expect(guard.trapT).toBe(0);
  });

  it("die when the brick grows back around them, then come back at the top", () => {
    const g = game({ 14: row({ 3: "&", 9: "0" }) });
    requestDig(g, 1);
    const guard = g.guards[0];
    until(g, () => guard.trapT > 0);
    g.holes[0].phase = "closing";
    g.holes[0].t = 0;
    until(g, () => g.killed === 1);
    expect(guard.respawnT).toBeGreaterThan(0);
    expect(g.score).toBe(SCORE.trap + SCORE.kill);
    until(g, () => guard.respawnT === 0);
    expect(guard.r).toBeLessThan(2);
  });

  it("leave the gold they carry on the rim of the hole that holds them", () => {
    const g = game({ 14: row({ 3: "&", 7: "$", 9: "0" }) });
    requestDig(g, 1);
    until(g, () => g.guards[0].carrying);
    expect(g.gold[cell(7, 14)]).toBe(0);
    until(g, () => g.guards[0].trapT > 0);
    expect(g.guards[0].carrying).toBe(false);
    expect(g.gold[cell(4, 14)]).toBe(1);
    expect(g.goldLeft).toBe(1);
  });

  it("never share a cell", () => {
    const g = game({ 14: row({ 2: "&", 14: "0", 16: "0", 18: "0" }) });
    for (let t = 0; t < 240 && g.status === "play"; t++) {
      step(g, { dirs: [] });
      const cells = g.guards.map((o) => cell(o.c, o.r));
      expect(new Set(cells).size).toBe(cells.length);
    }
  });

  it("kill the runner they touch", () => {
    const g = game({ 14: row({ 3: "&", 8: "0" }) });
    until(g, () => g.status === "dying");
    expect(g.events.some((e) => e.kind === "death")).toBe(true);
  });

  it("find a way down a ladder or by a fall, and none to a runner they cannot reach", () => {
    const g = game({
      9: row({ 1: "0" }),
      10: row({ ...span(0, 6, "#"), 3: "H" }),
      11: row({ 3: "H" }),
      12: row({ 3: "H" }),
      13: row({ 3: "H" }),
      14: row({ 3: "H", 20: "&" }),
    });
    expect(route(g, cell(1, 9), cell(20, 14), ["right", "down", "up", "left"])).toBe("right");
    const island = game({ 4: row({ 20: "&" }), 5: row(span(18, 22, "@")), 14: row({ 1: "0" }) });
    expect(route(island, cell(1, 14), cell(20, 4), ["right", "up", "down", "left"])).toBeNull();
  });
});
