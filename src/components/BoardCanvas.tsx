"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { BoardRenderer } from "@/lib/render/renderer";
import type { Run } from "@/lib/run";

interface BoardCanvasProps {
  run: Run;
  /** Freeze the level (paused, or a dialog is open over it). */
  paused: boolean;
  /** Overlays laid over the board: the start prompt, the level-clear banner, the pause card. */
  children?: ReactNode;
}

/**
 * The board canvas. One animation loop advances the run and repaints it;
 * while paused it keeps painting but stops the clock, and on resume the run
 * forgets the time that passed so nothing jumps.
 */
export function BoardCanvas({ run, paused, children }: BoardCanvasProps) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const pausedRef = useRef(paused);

  useEffect(() => {
    pausedRef.current = paused;
  }, [paused]);

  useEffect(() => {
    const canvas = canvasRef.current;
    const wrap = wrapRef.current;
    if (!canvas || !wrap) return;
    const renderer = new BoardRenderer(canvas);

    const resize = () => {
      const r = wrap.getBoundingClientRect();
      renderer.resize(r.width, r.height, Math.min(window.devicePixelRatio || 1, 2.5));
    };
    resize();
    const ro = new ResizeObserver(resize);
    ro.observe(wrap);

    let raf = 0;
    let wasPaused = false;
    const loop = (t: number) => {
      if (pausedRef.current) {
        wasPaused = true;
      } else {
        if (wasPaused) run.resume();
        wasPaused = false;
        run.frame(t);
      }
      renderer.draw(run, t);
      raf = requestAnimationFrame(loop);
    };
    raf = requestAnimationFrame(loop);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
    };
  }, [run]);

  return (
    <div ref={wrapRef} className="lr-board">
      <canvas ref={canvasRef} className="lr-canvas" role="img" aria-label="The board" />
      {children}
    </div>
  );
}
