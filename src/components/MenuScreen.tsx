"use client";

import type { Prefs, Records } from "./LodeRunnerGame";
import { cx } from "@/lib/cx";
import { DIFFICULTIES, DIFFICULTY_LABEL, DIFFICULTY_NOTE, TICK_HZ } from "@/lib/game/constants";
import { LEVELS } from "@/lib/game/levels";

interface MenuScreenProps {
  prefs: Prefs;
  records: Records;
  onPrefs: (prefs: Prefs) => void;
  onStart: () => void;
  onRules: () => void;
}

/** Ticks as m:ss. */
export function clock(ticks: number): string {
  const s = Math.floor(ticks / TICK_HZ);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

export function MenuScreen({ prefs, records, onPrefs, onStart, onRules }: MenuScreenProps) {
  const chosen = LEVELS[prefs.level];
  return (
    <>
      <h1 className="lr-title">{"⛏️ LODE RUNNER"}</h1>
      <p className="lr-subtitle">Dig traps, dodge the guards, grab every bar of gold — then climb out the top.</p>

      <div className="lr-card">
        <span className="lr-field-label">Guards</span>
        <div className="lr-choice-row">
          {DIFFICULTIES.map((d) => (
            <button
              key={d}
              type="button"
              className={cx("lr-choice", prefs.difficulty === d && "is-on")}
              aria-pressed={prefs.difficulty === d}
              onClick={() => onPrefs({ ...prefs, difficulty: d })}
            >
              {DIFFICULTY_LABEL[d]}
            </button>
          ))}
        </div>
        <p className="lr-menu-note">{DIFFICULTY_NOTE[prefs.difficulty]}</p>

        <span className="lr-field-label">Level</span>
        <div className="lr-levels">
          {LEVELS.map((level, i) => {
            const locked = i > records.unlocked;
            const best = records.best[String(i)];
            return (
              <button
                key={level.name}
                type="button"
                className={cx("lr-level", prefs.level === i && "is-on", best !== undefined && "is-done")}
                aria-pressed={prefs.level === i}
                aria-label={locked ? `Level ${i + 1}, locked` : `Level ${i + 1}, ${level.name}`}
                disabled={locked}
                title={locked ? "Clear the level before it to open this one" : level.name}
                onClick={() => onPrefs({ ...prefs, level: i })}
              >
                <span className="lr-level-num">{locked ? "🔒" : i + 1}</span>
                <span className="lr-level-sub">{best !== undefined ? clock(best) : locked ? "" : "—"}</span>
              </button>
            );
          })}
        </div>
        <p className="lr-menu-note">
          {`${prefs.level + 1}. ${chosen.name} · ${chosen.gold.length} gold · ${chosen.guards.length} guard${
            chosen.guards.length === 1 ? "" : "s"
          }`}
        </p>

        <button type="button" className="lr-primary" onClick={onStart}>
          {`Run level ${prefs.level + 1}`}
        </button>

        <div className="lr-record" aria-label="Best scores">
          {DIFFICULTIES.map((d) => (
            <div key={d} className={cx("lr-record-cell", prefs.difficulty === d && "is-on")}>
              <span className="lr-record-tier">{DIFFICULTY_LABEL[d]}</span>
              <span className="lr-record-score">{(records.top[d] ?? 0).toLocaleString("en-US")}</span>
              <span className="lr-record-sub">best score</span>
            </div>
          ))}
        </div>

        <div className="lr-menu-actions">
          <button type="button" className="lr-ghost" onClick={onRules}>
            How to play
          </button>
          <button
            type="button"
            className="lr-ghost"
            aria-pressed={prefs.sound}
            onClick={() => onPrefs({ ...prefs, sound: !prefs.sound })}
          >
            {prefs.sound ? "🔊 Sound on" : "🔇 Sound off"}
          </button>
        </div>
      </div>
    </>
  );
}
