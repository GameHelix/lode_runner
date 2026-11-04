# ⛏️ LODE RUNNER

A neon **Lode Runner**, Doug Smith's 1983 gold heist, with twelve original levels: you can't fight back, only dig. Run, climb and go hand over hand through each level, take every bar of gold while the guards close in, then climb out the top on the ladders the last bar brings out. Built with **Next.js 16**, **TypeScript** (strict) and **Tailwind CSS v4**, on a fixed-step engine where everything moves cell to cell: dig a hole at your lower left or right, let a guard fall in, run across its head, and be clear before the brick grows back. Every level is proven finishable by the built-in checker, so a board always has a way through.

## ✨ Features

| | |
| --- | --- |
| ⛏️ Dig to survive | Holes open at your lower left or right, stay open six seconds and grow back — crushing whatever is inside |
| 🗺️ Twelve levels | Hand-built boards on the classic 28 × 16 grid, from First Haul to The Last Heist, each built on one idea: bars, buried gold, trap floors, vaults, pits |
| ✅ Always finishable | A checker proves every level can be cleared — walking, climbing, falling and digging, in an order that works — and the tests run it on all twelve |
| 🧠 Guards that hunt | Each guard takes the shortest way to you that the board allows right now, falls included; they carry gold off, drop it later and climb out of holes |
| 🎚️ Three guard speeds | Calm, Classic and Relentless set how fast the guards move and how long a hole holds them |
| 🪜 Hidden ladders | The last bar of gold brings out the escape ladders, with a flare and a pulsing way out |
| 🧱 Trap bricks | False floors that look like any brick and hold nothing up — cracked once something has dropped through |
| 📱 Keys and touch | Arrows or WASD and Z / X on a keyboard; a thumb pad and two dig buttons on a phone, with a camera that follows you |
| ⏱️ Records | Levels open as you clear them; best time per level and best score per guard speed, saved locally |
| 🔊 Procedural audio | Web Audio crunches, chimes, clonks and fanfares — no asset files |
| 🌗 Polished | Glowing neon tiles and animated figures, reduced-motion aware, pauses itself when the tab is hidden |

## 🎮 How to play

1. Pick a guard speed and a level — the next one opens each time you clear one — and start.
2. Run along floors, climb **ladders**, and go hand over hand along **bars**; press down to let go. Step off an edge and you fall, and a bar on the way down catches you.
3. Dig the brick at your **lower left** or **lower right**. Stone never gives, and neither does a brick under a ladder, gold or a guard. Drop through your own hole to get down through a floor.
4. A guard that touches you costs a life. Trap one in a hole and walk over its head, or let the brick grow back over it.
5. Take every bar of **gold**, then climb any of the revealed ladders to the **top row**.

**Keys:** arrows / `WASD` move · `Z` (or `Q`) dig left · `X` (or `E`) dig right · `P` / `Esc` pause · `R` give up a life when stuck · `Enter` start or go on to the next level. **Touch:** slide your thumb around the pad and tap a **Dig** button.

## 🛠 Tech

- **Next.js 16** (App Router) + **React 19**
- **TypeScript 5**, strict mode
- **Tailwind CSS v4** via `@tailwindcss/postcss`
- **Vitest 3** for the core-logic unit tests
- Pure, framework-free game engine (movement rules, fixed-tick engine, digging and holes, guard path-finding, level parser and finishability checker) under `src/lib/game`

## Getting started

```bash
npm install
npm run dev      # http://localhost:3000
```

```bash
npm run build    # production build
npm test         # run the unit tests
npm run lint     # lint the project
```
