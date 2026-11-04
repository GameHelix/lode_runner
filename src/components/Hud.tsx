"use client";

import { DIFFICULTY_LABEL } from "@/lib/game/constants";
import { LEVELS } from "@/lib/game/levels";
import type { Difficulty } from "@/lib/game/types";
import type { RunView } from "@/lib/run";

interface HudProps {
  view: RunView;
  difficulty: Difficulty;
}

function mmss(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

/** Level, score, gold taken, lives and the clock, in one strip above the board. */
export function Hud({ view, difficulty }: HudProps) {
  const taken = view.goldTotal - view.goldLeft;
  return (
    <div className="lr-hud" role="status" aria-live="off">
      <div className="lr-stat is-level">
        <span className="lr-stat-label">{`Level ${view.level + 1}/${LEVELS.length} · ${DIFFICULTY_LABEL[difficulty]}`}</span>
        <span className="lr-stat-value">{view.name}</span>
      </div>
      <div className="lr-stat">
        <span className="lr-stat-label">Score</span>
        <span className="lr-stat-value">{view.score.toLocaleString("en-US")}</span>
      </div>
      <div className="lr-stat is-gold">
        <span className="lr-stat-label">Gold</span>
        <span className="lr-stat-value">{view.goldLeft === 0 ? "Climb out!" : `${taken}/${view.goldTotal}`}</span>
      </div>
      <div className="lr-stat">
        <span className="lr-stat-label">Lives</span>
        <span className="lr-stat-value" aria-label={`${view.lives} lives`}>
          {"♥".repeat(Math.min(view.lives, 9))}
        </span>
      </div>
      <div className="lr-stat">
        <span className="lr-stat-label">Time</span>
        <span className="lr-stat-value">{mmss(view.seconds)}</span>
      </div>
    </div>
  );
}
