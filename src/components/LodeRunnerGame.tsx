"use client";

import { useCallback, useMemo, useState } from "react";
import { MenuScreen } from "./MenuScreen";
import { PlayScreen } from "./PlayScreen";
import { ResultPanel } from "./ResultPanel";
import { RulesPanel } from "./RulesPanel";
import { cx } from "@/lib/cx";
import { PREFS_KEY, RECORDS_KEY } from "@/lib/game/constants";
import { LEVELS } from "@/lib/game/levels";
import { randomSeed } from "@/lib/game/rng";
import type { Difficulty } from "@/lib/game/types";
import { useLocalStorage } from "@/lib/hooks/useLocalStorage";
import { useSound } from "@/lib/hooks/useSound";
import type { RunResult } from "@/lib/run";

export interface Prefs {
  difficulty: Difficulty;
  /** The level the next run starts from. */
  level: number;
  sound: boolean;
}

export interface Records {
  /** The highest level that may be started; each clear opens the next. */
  unlocked: number;
  /** Fastest clear of each level, in ticks, by level index. */
  best: Record<string, number>;
  /** Best run score for each guard speed. */
  top: Record<Difficulty, number>;
}

const DEFAULT_PREFS: Prefs = { difficulty: "classic", level: 0, sound: true };
const EMPTY_RECORDS: Records = { unlocked: 0, best: {}, top: { calm: 0, classic: 0, relentless: 0 } };

interface Session {
  difficulty: Difficulty;
  start: number;
  seed: number;
  /** Bumped for every run so the play screen remounts clean. */
  nonce: number;
}

/**
 * Top-level orchestrator: routes between the menu and a run, and owns the
 * persisted choices and records, the sound switch, the rules dialog and the
 * result panel. The run's seed is drawn in the Start click, so nothing random
 * runs during server rendering and the first paint matches it.
 */
export function LodeRunnerGame() {
  const { value: prefs, store: storePrefs } = useLocalStorage<Prefs>(PREFS_KEY, DEFAULT_PREFS);
  const { value: stored, store: storeRecords } = useLocalStorage<Records>(RECORDS_KEY, EMPTY_RECORDS);
  // Records written by an older shape still read cleanly: missing fields fall back to empty.
  const records = useMemo<Records>(
    () => ({ ...EMPTY_RECORDS, ...stored, top: { ...EMPTY_RECORDS.top, ...stored.top } }),
    [stored]
  );
  const [session, setSession] = useState<Session | null>(null);
  const [rulesOpen, setRulesOpen] = useState(false);
  const [result, setResult] = useState<RunResult | null>(null);
  const play = useSound(prefs.sound);

  const begin = useCallback(
    (difficulty: Difficulty, start: number) => {
      setResult(null);
      setSession((prev) => ({ difficulty, start, seed: randomSeed(), nonce: (prev?.nonce ?? 0) + 1 }));
    },
    []
  );

  const startLevel = Math.min(Math.max(0, prefs.level), records.unlocked, LEVELS.length - 1);
  const handleStart = useCallback(() => begin(prefs.difficulty, startLevel), [begin, prefs.difficulty, startLevel]);
  const handleAgain = useCallback(() => {
    if (session) begin(session.difficulty, session.start);
  }, [begin, session]);

  const handleMenu = useCallback(() => {
    setResult(null);
    setSession(null);
  }, []);

  const handleCleared = useCallback(
    (level: number, ticks: number) => {
      const key = String(level);
      const prev = records.best[key];
      storeRecords({
        ...records,
        unlocked: Math.min(LEVELS.length - 1, Math.max(records.unlocked, level + 1)),
        best: { ...records.best, [key]: prev === undefined ? ticks : Math.min(prev, ticks) },
      });
    },
    [records, storeRecords]
  );

  const handleOver = useCallback(
    (r: RunResult) => {
      setResult(r);
      if (r.score > (records.top[r.difficulty] ?? 0)) {
        storeRecords({ ...records, top: { ...records.top, [r.difficulty]: r.score } });
      }
    },
    [records, storeRecords]
  );

  const toggleSound = useCallback(() => storePrefs({ ...prefs, sound: !prefs.sound }), [prefs, storePrefs]);

  return (
    <main className={cx("lr-app", session && "is-play")}>
      {session ? (
        <PlayScreen
          key={session.nonce}
          difficulty={session.difficulty}
          start={session.start}
          seed={session.seed}
          soundOn={prefs.sound}
          play={play}
          paused={rulesOpen || result !== null}
          onCleared={handleCleared}
          onOver={handleOver}
          onMenu={handleMenu}
          onRules={() => setRulesOpen(true)}
          onToggleSound={toggleSound}
        />
      ) : (
        <MenuScreen
          prefs={{ ...prefs, level: startLevel }}
          records={records}
          onPrefs={storePrefs}
          onStart={handleStart}
          onRules={() => setRulesOpen(true)}
        />
      )}

      {result ? (
        <ResultPanel result={result} top={records.top[result.difficulty] ?? 0} onAgain={handleAgain} onMenu={handleMenu} />
      ) : null}
      {rulesOpen ? <RulesPanel onClose={() => setRulesOpen(false)} /> : null}
    </main>
  );
}
