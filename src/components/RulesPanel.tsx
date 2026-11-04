"use client";

import { useEffect } from "react";

interface RulesPanelProps {
  onClose: () => void;
}

export function RulesPanel({ onClose }: RulesPanelProps) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="lr-overlay" role="dialog" aria-modal aria-label="How to play">
      <div className="lr-dialog is-wide">
        <h2 className="lr-dialog-title">How to play</h2>
        <div className="lr-rules">
          <p>
            <b>The heist.</b> Take every bar of <b>gold</b>. The last one brings out hidden ladders; climb any of them to
            the <b>top row</b> to finish the level.
          </p>
          <p>
            <b>Moving.</b> Run along floors, climb <b>ladders</b>, and go hand over hand along <b>bars</b> — press down to
            let go. Step off any edge and you fall; a bar on the way down catches you.
          </p>
          <p>
            <b>Digging.</b> Dig the brick at your lower left or lower right. Stone never gives, and neither does a brick
            under a ladder, a bar of gold or a guard. A hole stays open a few seconds, then grows back — and crushes
            whatever is inside, you included. Dig a hole and drop through it to get down through a floor.
          </p>
          <p>
            <b>Guards.</b> Touch one and you lose a life. They fall into holes and stay stuck a while: walk across their
            heads, or let the brick grow back over them. They pick up gold and put it down later; one stuck in a hole
            leaves its gold on the rim.
          </p>
          <p>
            <b>Trap bricks</b> look like any other brick but hold nothing up. Once something has dropped through one, it
            shows cracked.
          </p>
          <p>
            <b>Lives.</b> Five to start, one more for each level cleared. Stuck with no way out? Give up the life and
            start the level again.
          </p>
          <p>
            <b>Keys.</b> Arrows or WASD move, Z or Q digs left, X or E digs right, P pauses, R gives up a life.{" "}
            <b>Touch.</b> Slide your thumb around the pad to move, and tap a Dig button.
          </p>
        </div>
        <button type="button" className="lr-primary" onClick={onClose} autoFocus>
          Got it
        </button>
      </div>
    </div>
  );
}
