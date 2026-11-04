/*
 * The runner and the guards, drawn as glowing stick figures in cell units:
 * (x, y) is the top-left corner of the cell the figure fills. Each pose is a
 * handful of joints; walking and climbing step through frames by distance
 * travelled, so the legs keep time with the ground covered.
 */

export type Pose = "stand" | "run" | "climb" | "hang" | "fall" | "dig" | "trapped";

export interface FigureStyle {
  stroke: string;
  glow: string;
  /** Guards wear a visor; the runner does not. */
  visor: string | null;
}

type Pt = readonly [number, number];

interface Joints {
  /** Hands, then feet; elbows and knees are implied by the bends. */
  handL: Pt;
  handR: Pt;
  footL: Pt;
  footR: Pt;
  kneeL?: Pt;
  kneeR?: Pt;
  /** Lean of the torso, in cells at the shoulders. */
  lean?: number;
}

const HEAD: Pt = [0.5, 0.2];
const NECK: Pt = [0.5, 0.33];
const HIP: Pt = [0.5, 0.62];

function joints(pose: Pose, frame: number): Joints {
  const f = frame & 3;
  switch (pose) {
    case "run": {
      const stride = [0.22, 0.06, -0.22, -0.06][f];
      return {
        handL: [0.5 - stride * 0.9, 0.55],
        handR: [0.5 + stride * 0.9, 0.55],
        footL: [0.5 + stride, 0.97],
        footR: [0.5 - stride, f % 2 === 1 ? 0.88 : 0.97],
        kneeL: [0.5 + stride * 0.6, 0.8],
        kneeR: [0.5 - stride * 0.4, 0.78],
        lean: 0.05,
      };
    }
    case "climb": {
      const up = f < 2;
      return {
        handL: [0.3, up ? 0.08 : 0.3],
        handR: [0.7, up ? 0.3 : 0.08],
        footL: [0.34, up ? 0.92 : 0.98],
        footR: [0.66, up ? 0.98 : 0.92],
        kneeL: [0.32, up ? 0.74 : 0.8],
        kneeR: [0.68, up ? 0.8 : 0.74],
      };
    }
    case "hang": {
      const swing = f < 2 ? 0.05 : -0.05;
      return {
        handL: [0.36 + swing, 0.08],
        handR: [0.64 + swing, 0.08],
        footL: [0.44 - swing, 0.96],
        footR: [0.56 - swing, 0.96],
      };
    }
    case "fall":
      return {
        handL: [0.18, 0.16],
        handR: [0.82, 0.16],
        footL: [0.3, 0.95],
        footR: [0.7, 0.95],
        kneeL: [0.36, 0.8],
        kneeR: [0.64, 0.8],
      };
    case "dig":
      return {
        handL: [0.96, 0.86],
        handR: [0.86, 0.62],
        footL: [0.32, 0.97],
        footR: [0.62, 0.97],
        kneeL: [0.38, 0.8],
        kneeR: [0.6, 0.78],
        lean: 0.14,
      };
    case "trapped":
      return {
        handL: [0.26, 0.04],
        handR: [0.74, 0.04],
        footL: [0.4, 0.96],
        footR: [0.6, 0.96],
      };
    default:
      return {
        handL: [0.32, 0.58],
        handR: [0.68, 0.58],
        footL: [0.38, 0.97],
        footR: [0.62, 0.97],
      };
  }
}

/** Draw one figure filling the cell at (x, y), facing left (-1) or right (1). */
export function drawFigure(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  pose: Pose,
  frame: number,
  facing: -1 | 1,
  style: FigureStyle,
  glowPx: number
): void {
  const j = joints(pose, frame);
  // Mirror around the cell's centre line for the facing.
  const px = (p: Pt) => x + 0.5 + (p[0] - 0.5) * facing;
  const py = (p: Pt) => y + p[1];
  const lean = (j.lean ?? 0) * facing;

  ctx.save();
  ctx.lineCap = "round";
  ctx.lineJoin = "round";
  ctx.strokeStyle = style.stroke;
  ctx.fillStyle = style.stroke;
  ctx.shadowColor = style.glow;
  ctx.shadowBlur = glowPx;
  ctx.lineWidth = 0.1;

  const neck: Pt = [NECK[0] + lean, NECK[1]];
  ctx.beginPath();
  // Torso.
  ctx.moveTo(px(neck), py(neck));
  ctx.lineTo(px(HIP), py(HIP));
  // Arms from the shoulders.
  const shoulder: Pt = [neck[0], neck[1] + 0.05];
  ctx.moveTo(px(j.handL), py(j.handL));
  ctx.lineTo(px(shoulder), py(shoulder));
  ctx.lineTo(px(j.handR), py(j.handR));
  // Legs, through the knees when the pose bends them.
  ctx.moveTo(px(j.footL), py(j.footL));
  if (j.kneeL) ctx.lineTo(px(j.kneeL), py(j.kneeL));
  ctx.lineTo(px(HIP), py(HIP));
  if (j.kneeR) ctx.lineTo(px(j.kneeR), py(j.kneeR));
  ctx.lineTo(px(j.footR), py(j.footR));
  ctx.stroke();

  // Head.
  const head: Pt = [HEAD[0] + lean, HEAD[1]];
  ctx.beginPath();
  ctx.arc(px(head), py(head), 0.13, 0, Math.PI * 2);
  ctx.fill();
  if (style.visor) {
    ctx.shadowBlur = 0;
    ctx.strokeStyle = style.visor;
    ctx.lineWidth = 0.06;
    ctx.beginPath();
    ctx.moveTo(px([head[0] - 0.02, head[1]]), py([0, head[1] - 0.01]));
    ctx.lineTo(px([head[0] + 0.12, head[1]]), py([0, head[1] - 0.01]));
    ctx.stroke();
  }
  ctx.restore();
}
