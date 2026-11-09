// Draws The Investor Project's tree-ring logo (variant B, chosen by Rob on
// 9 November 2026) and writes the master SVGs, static/favicon.svg and an
// icons.html page that render-icons.ps1 screenshots into the PNG icons.
//
//   node docs/brand/logo.mjs
//   pwsh docs/brand/render-icons.ps1
//
// The rings are off-centre on purpose: centred rings read as a bullseye
// (and look like Target's logo). Each ring is interpolated between the
// pith and the log's outer edge, so rings sit wider apart on the side away
// from the pith, like a real cross-section.
import { writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repo = join(here, '..', '..');
const f = (v) => (Math.round(v * 10) / 10).toString();

export const colours = {
  navy: '#002664', // Consortium Private Wealth's navy
  blue: '#199ce4', // Consortium's blue
  ochre: '#D9A064', // the warm accent (the pith)
  ink: '#0A0E17', // the site's dark background
  white: '#ffffff',
};

// Outer edge of the log at angle th: a circle with gentle lobes.
function outer(th, seed) {
  const r = 205 * (1 + 0.018 * Math.sin(3 * th + seed) + 0.012 * Math.sin(5 * th + 2 * seed) + 0.03 * Math.sin(2 * th - seed));
  return [256 + r * Math.cos(th), 256 + r * Math.sin(th)];
}

// A closed, smooth ring t of the way from the pith (px, py) to the edge.
function ring(px, py, t, seed, wobble) {
  const n = 60, pts = [];
  for (let i = 0; i < n; i++) {
    const th = (2 * Math.PI * i) / n;
    const [ox, oy] = outer(th, seed);
    const w = 1 + wobble * Math.sin(4 * th + 7 * t + seed);
    pts.push([px + t * w * (ox - px), py + t * w * (oy - py)]);
  }
  let d = `M${f(pts[0][0])} ${f(pts[0][1])}`;
  for (let i = 0; i < n; i++) {
    const p0 = pts[(i - 1 + n) % n], p1 = pts[i], p2 = pts[(i + 1) % n], p3 = pts[(i + 2) % n];
    d += ` C${f(p1[0] + (p2[0] - p0[0]) / 6)} ${f(p1[1] + (p2[1] - p0[1]) / 6)} ${f(p2[0] - (p3[0] - p1[0]) / 6)} ${f(p2[1] - (p3[1] - p1[1]) / 6)} ${f(p2[0])} ${f(p2[1])}`;
  }
  return d + 'Z';
}

// full: variant B, three inner rings. small: one inner ring and heavier
// lines, for 16 and 32 px, where three rings run together.
const shapes = {
  full: { px: 196, py: 314, rings: [0.32, 0.57, 0.80], outerW: 30, innerW: 22, dot: 22, seed: 0.6 },
  small: { px: 206, py: 304, rings: [0.52], outerW: 48, innerW: 44, dot: 46, seed: 0.6 },
};
const themes = {
  light: { outer: colours.navy, inner: colours.blue, dot: colours.ochre }, // on white
  dark: { outer: colours.white, inner: colours.blue, dot: colours.ochre }, // on #0A0E17
};

// The mark's paths, in a 512 x 512 box.
function markBody(shape, theme) {
  const s = shapes[shape], c = themes[theme];
  let g = `<path d="${ring(s.px, s.py, 1, s.seed, 0)}" stroke="${c.outer}" stroke-width="${s.outerW}"/>`;
  for (const t of s.rings) g += `<path d="${ring(s.px, s.py, t, s.seed, 0.03)}" stroke="${c.inner}" stroke-width="${s.innerW}"/>`;
  return g + `<circle cx="${s.px}" cy="${s.py}" r="${s.dot}" fill="${c.dot}" stroke="none"/>`;
}

const svgOpen = (vb = '0 0 512 512') => `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${vb}" fill="none" stroke-linejoin="round">`;

// Mark alone, transparent background.
function mark(shape, theme) {
  return `${svgOpen()}${markBody(shape, theme)}</svg>\n`;
}

// Mark on a dark tile: rx 0 for a full-bleed square (iOS and Android round
// the corners themselves), scale = share of the tile the mark fills.
function tile(shape, rx, scale) {
  const off = (512 * (1 - scale)) / 2;
  return `${svgOpen()}<rect width="512" height="512" rx="${rx}" fill="${colours.ink}"/>` +
    `<g transform="translate(${f(off)} ${f(off)}) scale(${scale})">${markBody(shape, 'dark')}</g></svg>\n`;
}

const files = {
  'mark-light.svg': mark('full', 'light'),
  'mark-dark.svg': mark('full', 'dark'),
  'mark-small-light.svg': mark('small', 'light'),
  'mark-small-dark.svg': mark('small', 'dark'),
  'tile-rounded.svg': tile('full', 96, 0.86),
  'tile-small-rounded.svg': tile('small', 96, 0.9),
  'tile-square.svg': tile('full', 0, 0.74),
};
for (const [name, svg] of Object.entries(files)) writeFileSync(join(here, name), svg);

// The browser tab icon and the nav's mark: only ever 16 to 32 px, so the
// small cut.
writeFileSync(join(repo, 'static', 'favicon.svg'), files['tile-small-rounded.svg']);

// icons.html: every PNG icon side by side at its real size, top-aligned, for
// render-icons.ps1 to screenshot and crop. Keep in step with $icons there.
export const icons = [
  { file: 'favicon-16x16.png', size: 16, svg: 'tile-small-rounded.svg' },
  { file: 'favicon-32x32.png', size: 32, svg: 'tile-small-rounded.svg' },
  { file: 'favicon-48x48.png', size: 48, svg: 'tile-rounded.svg' },
  { file: 'apple-touch-icon.png', size: 180, svg: 'tile-square.svg' },
  { file: 'android-chrome-192x192.png', size: 192, svg: 'tile-square.svg' },
  { file: 'android-chrome-512x512.png', size: 512, svg: 'tile-square.svg' },
];
let x = 0, imgs = '';
for (const i of icons) {
  imgs += `<img src="${i.svg}" width="${i.size}" height="${i.size}" style="position:absolute;left:${x}px;top:0">`;
  i.x = x;
  x += i.size + 8;
}
writeFileSync(join(here, 'icons.html'), `<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:transparent}</style></head><body>${imgs}</body></html>\n`);
writeFileSync(join(here, 'icons.json'), JSON.stringify(icons, null, 2) + '\n');
