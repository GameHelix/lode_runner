"use client";

import { useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { cx } from "@/lib/cx";
import type { Dir, Side } from "@/lib/game/types";
import type { Run } from "@/lib/run";

interface TouchControlsProps {
  run: Run;
}

const ARROW: Record<Dir, string> = { up: "▲", left: "◀", right: "▶", down: "▼" };
const PAD: readonly Dir[] = ["up", "left", "right", "down"];

/** Which way a point on the pad means: the axis it leans along more from the centre, with a dead zone in the middle. */
function dirAt(el: HTMLElement, x: number, y: number): Dir | null {
  const r = el.getBoundingClientRect();
  const dx = x - (r.left + r.width / 2);
  const dy = y - (r.top + r.height / 2);
  if (Math.hypot(dx, dy) < r.width * 0.12) return null;
  if (Math.abs(dx) > Math.abs(dy)) return dx < 0 ? "left" : "right";
  return dy < 0 ? "up" : "down";
}

/**
 * On-screen controls for touch screens: a pad the thumb can slide around
 * without lifting — the direction follows wherever it rests — and a dig
 * button for each side. Shown only where the pointer is coarse.
 */
export function TouchControls({ run }: TouchControlsProps) {
  const [active, setActive] = useState<Dir | null>(null);
  const current = useRef<Dir | null>(null);
  const pointer = useRef<number | null>(null);

  const aim = (dir: Dir | null) => {
    if (current.current === dir) return;
    if (current.current) run.release(current.current);
    current.current = dir;
    if (dir) run.press(dir);
    setActive(dir);
  };

  const onDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    pointer.current = e.pointerId;
    e.currentTarget.setPointerCapture(e.pointerId);
    aim(dirAt(e.currentTarget, e.clientX, e.clientY));
  };
  const onMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.pointerId === pointer.current) aim(dirAt(e.currentTarget, e.clientX, e.clientY));
  };
  const onUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.pointerId !== pointer.current) return;
    pointer.current = null;
    aim(null);
  };

  const dig = (side: Side) => (e: ReactPointerEvent<HTMLButtonElement>) => {
    e.preventDefault();
    run.dig(side);
  };

  return (
    <div className="lr-touch">
      <div
        className="lr-pad"
        role="group"
        aria-label="Move"
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onPointerCancel={onUp}
        onLostPointerCapture={onUp}
      >
        {PAD.map((d) => (
          <span key={d} className={cx("lr-pad-arrow", `is-${d}`, active === d && "is-on")} aria-hidden>
            {ARROW[d]}
          </span>
        ))}
      </div>
      <div className="lr-digs">
        <button type="button" className="lr-dig" onPointerDown={dig(-1)} aria-label="Dig to the lower left">
          <span aria-hidden>◣</span>
          Dig
        </button>
        <button type="button" className="lr-dig" onPointerDown={dig(1)} aria-label="Dig to the lower right">
          Dig
          <span aria-hidden>◢</span>
        </button>
      </div>
    </div>
  );
}
