"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { BoardCanvas } from "./BoardCanvas";
import { Hud } from "./Hud";
import { clock } from "./MenuScreen";
import { TouchControls } from "./TouchControls";
import type { Difficulty, Dir } from "@/lib/game/types";
import { Run, type RunResult, type Voice } from "@/lib/run";

interface PlayScreenProps {
  difficulty: Difficulty;
  start: number;
  seed: number;
  soundOn: boolean;
  play: (voice: Voice) => void;
  /** Freeze the level: a dialog is open over it. */
  paused: boolean;
  onCleared: (level: number, ticks: number) => void;
  onOver: (result: RunResult) => void;
  onMenu: () => void;
  onRules: () => void;
  onToggleSound: () => void;
}

/** Arrows and WASD by their place on the keyboard, whatever the layout. */
const KEY_DIR: Record<string, Dir> = {
  ArrowLeft: "left",
  ArrowRight: "right",
  ArrowUp: "up",
  ArrowDown: "down",
  KeyA: "left",
  KeyD: "right",
  KeyW: "up",
  KeyS: "down",
};

/** Leave keys alone while the person is typing in a field. */
function isTyping(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return target.closest("input, textarea, select") !== null;
}

export function PlayScreen({
  difficulty,
  start,
  seed,
  soundOn,
  play,
  paused,
  onCleared,
  onOver,
  onMenu,
  onRules,
  onToggleSound,
}: PlayScreenProps) {
  // One run per mount; the parent remounts this screen for a new one.
  const [run] = useState(() => new Run(difficulty, start, seed));
  const view = useSyncExternalStore(run.subscribe, run.getView, run.getView);
  const [held, setHeld] = useState(false);
  const frozen = paused || held;
  const frozenRef = useRef(frozen);

  useEffect(() => {
    frozenRef.current = frozen;
    if (frozen) run.releaseAll();
  }, [frozen, run]);

  useEffect(() => {
    run.connect({ sound: play, cleared: onCleared, over: onOver });
  }, [run, play, onCleared, onOver]);

  // Leaving the tab mid-level pauses it, so nobody comes back to a lost life.
  useEffect(() => {
    const onVisibility = () => {
      if (document.hidden && run.phase === "play") setHeld(true);
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, [run]);

  useEffect(() => {
    const onDown = (e: KeyboardEvent) => {
      if (isTyping(e.target) || e.metaKey || e.ctrlKey || e.altKey) return;
      const dir = KEY_DIR[e.code];
      if (dir) {
        e.preventDefault();
        if (!frozenRef.current) run.press(dir);
        return;
      }
      switch (e.code) {
        case "KeyZ":
        case "KeyQ":
          e.preventDefault();
          if (!e.repeat && !frozenRef.current) run.dig(-1);
          break;
        case "KeyX":
        case "KeyE":
          e.preventDefault();
          if (!e.repeat && !frozenRef.current) run.dig(1);
          break;
        case "KeyP":
        case "Escape":
          if (paused) return; // a dialog has the key
          e.preventDefault();
          setHeld((h) => !h);
          break;
        case "KeyR":
          if (!e.repeat && !frozenRef.current) run.giveUp();
          break;
        case "Enter":
        case "Space":
          if (paused) return;
          e.preventDefault();
          if (frozenRef.current) setHeld(false);
          else run.proceed();
          break;
      }
    };
    const onUp = (e: KeyboardEvent) => {
      const dir = KEY_DIR[e.code];
      if (dir) run.release(dir);
    };
    const onBlur = () => run.releaseAll();
    window.addEventListener("keydown", onDown);
    window.addEventListener("keyup", onUp);
    window.addEventListener("blur", onBlur);
    return () => {
      window.removeEventListener("keydown", onDown);
      window.removeEventListener("keyup", onUp);
      window.removeEventListener("blur", onBlur);
    };
  }, [run, paused]);

  const over = view.phase === "over" || view.phase === "won";

  return (
    <div className="lr-play">
      <div className="lr-topbar">
        <button type="button" className="lr-ghost is-small" onClick={onMenu}>
          ← Menu
        </button>
        <div className="lr-topbar-actions">
          <button
            type="button"
            className="lr-ghost is-small"
            aria-pressed={held}
            disabled={over}
            onClick={() => setHeld((h) => !h)}
          >
            {held ? "▶ Resume" : "❚❚ Pause"}
          </button>
          <button
            type="button"
            className="lr-ghost is-small"
            aria-pressed={soundOn}
            aria-label={soundOn ? "Sound on" : "Sound off"}
            onClick={onToggleSound}
          >
            {soundOn ? "🔊" : "🔇"}
          </button>
          <button type="button" className="lr-ghost is-small" aria-label="How to play" onClick={onRules}>
            ?
          </button>
        </div>
      </div>

      <Hud view={view} difficulty={difficulty} />

      <BoardCanvas run={run} paused={frozen}>
        {view.phase === "ready" && !held ? (
          <div className="lr-banner">
            <span className="lr-banner-title">{`Level ${view.level + 1} · ${view.name}`}</span>
            <span className="lr-banner-detail">
              <span className="lr-only-keys">Press an arrow key to start · dig with Z and X</span>
              <span className="lr-only-touch">Touch the pad to start</span>
            </span>
          </div>
        ) : null}
        {view.phase === "cleared" && view.cleared ? (
          <div className="lr-banner is-good">
            <span className="lr-banner-title">Level clear!</span>
            <span className="lr-banner-detail">
              {`${clock(view.cleared.ticks)} · +${view.cleared.points.toLocaleString("en-US")} · ${view.cleared.trapped} trapped · +1 life`}
            </span>
            <button type="button" className="lr-primary is-small" onClick={() => run.proceed()}>
              Next level
            </button>
          </div>
        ) : null}
        {held && !over ? (
          <div className="lr-banner">
            <span className="lr-banner-title">Paused</span>
            <button type="button" className="lr-primary is-small" onClick={() => setHeld(false)} autoFocus>
              Resume
            </button>
          </div>
        ) : null}
      </BoardCanvas>

      <TouchControls run={run} />
      <p className="lr-keys">
        <kbd>←</kbd>
        <kbd>↑</kbd>
        <kbd>→</kbd>
        <kbd>↓</kbd> or <kbd>WASD</kbd> move · <kbd>Z</kbd> <kbd>X</kbd> dig · <kbd>P</kbd> pause · <kbd>R</kbd> give up a
        life
      </p>
    </div>
  );
}
