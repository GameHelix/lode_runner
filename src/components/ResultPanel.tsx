"use client";

import { DIFFICULTY_LABEL } from "@/lib/game/constants";
import { LEVELS } from "@/lib/game/levels";
import type { RunResult } from "@/lib/run";

interface ResultPanelProps {
  result: RunResult;
  /** Best score at this guard speed, this run included. */
  top: number;
  onAgain: () => void;
  onMenu: () => void;
}

/** End of a run: out of lives, or every level cleared. */
export function ResultPanel({ result, top, onAgain, onMenu }: ResultPanelProps) {
  const best = result.score > 0 && result.score >= top;
  return (
    <div className="lr-overlay" role="dialog" aria-modal aria-label={result.won ? "Every level cleared" : "Game over"}>
      <div className="lr-dialog">
        <h2 className="lr-dialog-title">{result.won ? "Heist complete" : "Game over"}</h2>
        <p className={result.won ? "lr-result is-win" : "lr-result is-loss"}>
          {result.won ? "🏆 All twelve cleared!" : "💀 Out of lives"}
        </p>

        <table className="lr-score">
          <tbody>
            <tr>
              <th scope="row">Score</th>
              <td>{result.score.toLocaleString("en-US")}</td>
            </tr>
            <tr>
              <th scope="row">Levels cleared</th>
              <td>{result.cleared}</td>
            </tr>
            <tr>
              <th scope="row">{result.won ? "Last level" : "Caught on"}</th>
              <td>{`${result.level + 1}. ${LEVELS[result.level].name}`}</td>
            </tr>
            <tr>
              <th scope="row">Guards</th>
              <td>{DIFFICULTY_LABEL[result.difficulty]}</td>
            </tr>
          </tbody>
        </table>

        <p className="lr-result-note">
          {best ? <b>New best score!</b> : `Best at ${DIFFICULTY_LABEL[result.difficulty]}: ${top.toLocaleString("en-US")}`}
        </p>

        <button type="button" className="lr-primary" onClick={onAgain} autoFocus>
          {`Play again from level ${result.start + 1}`}
        </button>
        <button type="button" className="lr-ghost" onClick={onMenu}>
          Back to menu
        </button>
      </div>
    </div>
  );
}
