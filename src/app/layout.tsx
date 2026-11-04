import type { Metadata, Viewport } from "next";
import "./globals.css";

/** A bar of gold, inline, so the tab has an icon without any asset file. */
const ICON =
  "data:image/svg+xml," +
  encodeURIComponent(
    "<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'>" +
      "<defs><linearGradient id='g' x1='0' y1='0' x2='0' y2='1'><stop offset='0' stop-color='#fef3c7'/>" +
      "<stop offset='0.4' stop-color='#fbbf24'/><stop offset='1' stop-color='#b45309'/></linearGradient></defs>" +
      "<rect x='3' y='3' width='58' height='58' rx='14' fill='#070610' stroke='#22d3ee' stroke-width='3'/>" +
      "<path d='M22 22 H42 L51 45 H13 Z' fill='url(#g)'/>" +
      "</svg>"
  );

export const metadata: Metadata = {
  title: "Lode Runner — a neon gold heist",
  icons: { icon: ICON },
  description:
    "Neon Lode Runner built with Next.js and TypeScript. Dig holes to trap the guards, take every bar of gold and climb out the top, across twelve hand-built levels with three guard speeds, keyboard and touch controls.",
};

export const viewport: Viewport = {
  themeColor: "#070610",
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
