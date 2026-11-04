"use client";

import { useCallback, useRef } from "react";
import type { Voice } from "@/lib/run";

/**
 * Procedural Web Audio effects — no asset files, so nothing to load or 404.
 * The context is created lazily on the first sound because browsers refuse to
 * start one before a user gesture. Digs crunch, gold chimes, and the guards'
 * fates get a clonk or a zap; footfalls stay silent so the board never nags.
 */
export function useSound(enabled: boolean) {
  const ctxRef = useRef<AudioContext | null>(null);
  const noiseRef = useRef<AudioBuffer | null>(null);

  const context = useCallback((): AudioContext | null => {
    if (typeof window === "undefined") return null;
    if (!ctxRef.current) {
      const Ctor =
        window.AudioContext ||
        (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
      if (!Ctor) return null;
      ctxRef.current = new Ctor();
    }
    if (ctxRef.current.state === "suspended") void ctxRef.current.resume();
    return ctxRef.current;
  }, []);

  /** Half a second of white noise, made once and reused by every burst. */
  const noise = useCallback((ctx: AudioContext): AudioBuffer => {
    if (!noiseRef.current || noiseRef.current.sampleRate !== ctx.sampleRate) {
      const buf = ctx.createBuffer(1, Math.floor(ctx.sampleRate * 0.5), ctx.sampleRate);
      const data = buf.getChannelData(0);
      let seed = 1;
      for (let i = 0; i < data.length; i++) {
        seed = (seed * 16807) % 2147483647;
        data[i] = (seed / 2147483647) * 2 - 1;
      }
      noiseRef.current = buf;
    }
    return noiseRef.current;
  }, []);

  const tone = useCallback(
    (ctx: AudioContext, freq: number, dur: number, type: OscillatorType, gain: number, slideTo?: number, delay = 0) => {
      const t = ctx.currentTime + delay;
      const osc = ctx.createOscillator();
      const amp = ctx.createGain();
      osc.type = type;
      osc.frequency.setValueAtTime(freq, t);
      if (slideTo !== undefined) osc.frequency.exponentialRampToValueAtTime(Math.max(1, slideTo), t + dur);
      amp.gain.setValueAtTime(Math.max(0.0001, gain), t);
      amp.gain.exponentialRampToValueAtTime(0.0001, t + dur);
      osc.connect(amp).connect(ctx.destination);
      osc.start(t);
      osc.stop(t + dur + 0.02);
      // Release the nodes as soon as they are silent so nothing accumulates.
      osc.onended = () => {
        osc.disconnect();
        amp.disconnect();
      };
    },
    []
  );

  const burst = useCallback(
    (ctx: AudioContext, freq: number, q: number, dur: number, gain: number, type: BiquadFilterType = "bandpass", delay = 0) => {
      const t = ctx.currentTime + delay;
      const src = ctx.createBufferSource();
      src.buffer = noise(ctx);
      const filter = ctx.createBiquadFilter();
      filter.type = type;
      filter.frequency.value = freq;
      filter.Q.value = q;
      const amp = ctx.createGain();
      amp.gain.setValueAtTime(Math.max(0.0001, gain), t);
      amp.gain.exponentialRampToValueAtTime(0.0001, t + dur);
      src.connect(filter).connect(amp).connect(ctx.destination);
      src.start(t, Math.random() * 0.3);
      src.stop(t + dur + 0.02);
      src.onended = () => {
        src.disconnect();
        filter.disconnect();
        amp.disconnect();
      };
    },
    [noise]
  );

  const play = useCallback(
    (voice: Voice) => {
      if (!enabled) return;
      const ctx = context();
      if (!ctx) return;

      switch (voice) {
        case "dig":
          // Brick giving way: a gritty crunch over a dull knock.
          burst(ctx, 900, 1.1, 0.16, 0.16);
          burst(ctx, 2400, 2.5, 0.06, 0.06, "bandpass", 0.05);
          tone(ctx, 150, 0.1, "sine", 0.12, 80);
          break;
        case "nodig":
          tone(ctx, 140, 0.09, "square", 0.04, 110);
          break;
        case "gold":
          tone(ctx, 988, 0.09, "triangle", 0.07);
          tone(ctx, 1480, 0.16, "triangle", 0.06, undefined, 0.06);
          break;
        case "reveal":
          [523, 659, 784, 1046, 1318].forEach((f, i) => tone(ctx, f, 0.18, "triangle", 0.055, undefined, i * 0.07));
          break;
        case "trap":
          // A guard drops into a hole: a hollow clonk.
          tone(ctx, 330, 0.16, "sine", 0.14, 150);
          burst(ctx, 600, 1.5, 0.08, 0.06, "lowpass");
          break;
        case "kill":
          tone(ctx, 620, 0.32, "sawtooth", 0.05, 70);
          burst(ctx, 400, 0.8, 0.22, 0.08, "lowpass", 0.05);
          break;
        case "fill":
          tone(ctx, 120, 0.12, "sine", 0.08, 60);
          break;
        case "land":
          burst(ctx, 300, 0.9, 0.05, 0.05, "lowpass");
          break;
        case "respawn":
          tone(ctx, 220, 0.14, "sine", 0.05, 440);
          break;
        case "start":
          tone(ctx, 660, 0.07, "triangle", 0.05);
          tone(ctx, 880, 0.09, "triangle", 0.05, undefined, 0.06);
          break;
        case "death":
          [494, 392, 330, 247].forEach((f, i) => tone(ctx, f, 0.22, "triangle", 0.06, f * 0.94, i * 0.11));
          break;
        case "clear":
          [523, 659, 784, 1046, 1318, 1568].forEach((f, i) => tone(ctx, f, 0.22, "triangle", 0.055, undefined, i * 0.08));
          break;
        case "over":
          [392, 330, 262, 196, 147].forEach((f, i) => tone(ctx, f, 0.34, "triangle", 0.055, undefined, i * 0.15));
          break;
        case "won":
          [523, 659, 784, 1046, 784, 1046, 1318, 1568].forEach((f, i) =>
            tone(ctx, f, 0.24, "triangle", 0.06, undefined, i * 0.1)
          );
          break;
      }
    },
    [burst, context, enabled, tone]
  );

  return play;
}
