// When can I stop working? (/tools/stop-working): the page. Ported on 18
// November 2026 from the prototype's published artifact. Everything runs in
// the browser: nothing typed is sent anywhere or saved.
import { DEFAULTS, yearsToStop, savingsCurve, planWithSuper, project } from "./stop-working-calc.js";
const $ = (id) => document.getElementById(id);
const SVG = "http://www.w3.org/2000/svg";

const state = {
  rate: 0.5,
  r: DEFAULTS.realReturn,
  rs: DEFAULTS.superReturn,
  w: DEFAULTS.withdrawalRate,
  sg: DEFAULTS.superGuarantee,
  ct: DEFAULTS.contributionsTax,
};

// ---- Formatting -----------------------------------------------------------
const aud = new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD", maximumFractionDigits: 0 });
const int = new Intl.NumberFormat("en-AU", { maximumFractionDigits: 0 });
const money = (n) => (n < 0 ? "−" + aud.format(-Math.round(n)) : aud.format(Math.round(n)));
const pct = (x) => `${+(x * 100).toFixed(1)}%`;
const trim = (x, d = 1) => String(+x.toFixed(d));
function compactMoney(n) {
  if (Math.abs(n) >= 1e6) return `$${trim(n / 1e6, 2)}m`;
  if (Math.abs(n) >= 1e3) return `$${trim(n / 1e3, 0)}k`;
  return `$${Math.round(n)}`;
}
function niceStep(max, ticks) {
  const raw = max / ticks;
  const mag = 10 ** Math.floor(Math.log10(raw));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * mag >= raw) return m * mag;
  return 10 * mag;
}
function el(name, attrs = {}, text) {
  const node = document.createElementNS(SVG, name);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  if (text != null) node.textContent = text;
  return node;
}
function yearsPhrase(n) {
  if (!Number.isFinite(n) || n > 100) return { html: `More than <span class="big">100</span> years`, small: true };
  const y = Math.round(n);
  if (y === 0) return { html: `<span class="big">Right away</span>`, small: false };
  return { html: `About <span class="big">${y}</span> ${y === 1 ? "year" : "years"}`, small: false };
}

// ---- Simple mode ----------------------------------------------------------
const TABLE_RATES = [0.1, 0.25, 0.5, 0.75];

function renderSimple() {
  const slider = $("rate");
  state.rate = Number(slider.value) / 100;
  slider.style.setProperty("--pct", `${((slider.value - slider.min) / (slider.max - slider.min)) * 100}%`);
  $("rate-out").textContent = `${slider.value}%`;

  const n = yearsToStop(state.rate, state.r, state.w);
  const phrase = yearsPhrase(n);
  const answer = $("simple-answer");
  answer.innerHTML = phrase.html;
  answer.classList.toggle("small", phrase.small);
  const multiple = trim(1 / state.w, 1);
  $("simple-sub").innerHTML = Number.isFinite(n)
    ? `At a ${slider.value}% savings rate, it takes <strong>${trim(n, 1)} years</strong> for your savings to reach ${multiple} times a year's spending.`
    : `With these assumptions, savings at a ${slider.value}% rate never reach ${multiple} times a year's spending.`;

  const rows = savingsCurve(TABLE_RATES, state.r, state.w);
  $("rate-table").innerHTML = rows
    .map(({ rate, years }) => {
      const current = Math.round(rate * 100) === Number(slider.value);
      const shown = Number.isFinite(years) && years <= 100 ? trim(years, 1) : "100+";
      return `<tr class="${current ? "current" : ""}"><td>${Math.round(rate * 100)}%</td><td>${shown}</td></tr>`;
    })
    .join("");

  $("simple-sum").textContent = `${pct(state.r)} real return, ${pct(state.w)} withdrawal rate`;
  drawCurve();
}

function drawCurve() {
  const svg = $("curve");
  const W = svg.getBoundingClientRect().width;
  if (!W) return;
  const H = Math.round(Math.min(260, Math.max(200, W * 0.55)));
  const m = { l: 36, r: 12, t: 16, b: 30 };
  const pw = W - m.l - m.r;
  const ph = H - m.t - m.b;
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
  svg.setAttribute("height", H);
  svg.replaceChildren();

  const pts = [];
  for (let p = 5; p <= 90; p++) pts.push({ p, y: yearsToStop(p / 100, state.r, state.w) });
  const finite = pts.map((d) => d.y).filter((y) => Number.isFinite(y));
  const top = Math.min(100, Math.max(10, ...finite));
  const step = niceStep(top, 4);
  const yMax = Math.ceil(top / step) * step;
  const x = (p) => m.l + ((p - 5) / 85) * pw;
  const y = (v) => m.t + ph - (Math.min(v, yMax) / yMax) * ph;

  for (let v = 0; v <= yMax + 1e-9; v += step) {
    svg.append(el("line", { class: "grid", x1: m.l, x2: W - m.r, y1: y(v), y2: y(v) }));
    svg.append(el("text", { x: m.l - 6, y: y(v) + 4, "text-anchor": "end" }, trim(v, 1)));
  }
  for (const p of [10, 30, 50, 70, 90]) {
    svg.append(el("text", { x: x(p), y: H - 8, "text-anchor": p === 90 ? "end" : "middle" }, `${p}%`));
  }
  svg.append(el("text", { x: m.l, y: m.t - 4 }, "years"));

  const d = pts
    .filter((pt) => Number.isFinite(pt.y))
    .map((pt, i) => `${i ? "L" : "M"}${x(pt.p).toFixed(1)},${y(pt.y).toFixed(1)}`)
    .join("");
  if (d) {
    const first = pts.find((pt) => Number.isFinite(pt.y));
    const area = `${d}L${x(90)},${y(0)}L${x(first.p)},${y(0)}Z`;
    svg.append(el("path", { d: area, fill: "var(--primary)", "fill-opacity": "0.08", stroke: "none" }));
    svg.append(el("path", { d, fill: "none", stroke: "var(--primary)", "stroke-width": 2.5, "stroke-linejoin": "round" }));
  }

  const cur = Math.round(state.rate * 100);
  const cy = yearsToStop(state.rate, state.r, state.w);
  if (Number.isFinite(cy)) {
    const cx = x(cur);
    svg.append(el("line", { x1: cx, x2: cx, y1: y(cy), y2: y(0), stroke: "var(--text)", "stroke-opacity": 0.35, "stroke-dasharray": "3 3" }));
    svg.append(el("circle", { cx, cy: y(cy), r: 6, fill: "var(--text)", stroke: "var(--primary)", "stroke-width": 3 }));
    const label = el("text", { x: cx + (cur > 70 ? -10 : 10), y: y(cy) - 10, "text-anchor": cur > 70 ? "end" : "start", style: "fill: var(--text); font-weight: 600" }, `${trim(cy, 1)} years`);
    svg.append(label);
  }
}

// ---- Australian mode ------------------------------------------------------
const parseMoney = (s) => {
  const n = Number(String(s).replace(/[^0-9.]/g, ""));
  return Number.isFinite(n) ? n : 0;
};

function readInputs() {
  const ageInput = $("age");
  let age = Math.round(Number(ageInput.value));
  const ageOk = Number.isFinite(age) && age >= 16 && age <= 80;
  ageInput.setAttribute("aria-invalid", String(!ageOk));
  if (!ageOk) age = Math.min(80, Math.max(16, Number.isFinite(age) ? age : 35));
  return {
    age,
    salary: parseMoney($("salary").value),
    takeHome: parseMoney($("takehome").value),
    spending: parseMoney($("spending").value),
    outside: parseMoney($("outside").value),
    superBalance: parseMoney($("super").value),
    realReturn: state.r,
    superReturn: state.rs,
    withdrawalRate: state.w,
    superGuarantee: state.sg,
    contributionsTax: state.ct,
  };
}

function renderSuper() {
  const input = readInputs();
  const plan = planWithSuper(input);
  const c = plan.check;
  const pres = DEFAULTS.preservationAge;
  const maxAge = Math.max(input.age, DEFAULTS.maxAge);

  const answer = $("super-answer");
  if (plan.stopAge === null) {
    $("super-lead").textContent = `With these numbers, no age up to ${maxAge} works`;
    answer.innerHTML = `Not by age <span class="big">${maxAge}</span>`;
    answer.classList.add("small");
    $("super-sub").textContent = `The details below and the chart show stopping at ${maxAge}.`;
  } else {
    $("super-lead").textContent = "The earliest age where both parts work";
    answer.innerHTML = `At age <span class="big">${plan.stopAge}</span>`;
    answer.classList.remove("small");
    $("super-sub").innerHTML =
      plan.yearsFromNow === 0
        ? "That's your age now: your savings already cover both parts."
        : `That's <strong>${plan.yearsFromNow} ${plan.yearsFromNow === 1 ? "year" : "years"}</strong> from now, at age ${input.age} today.`;
  }

  // Part 1: bridge years
  const stopAge = c.stopAge;
  let bridgeHtml;
  if (c.bridgeYears === 0) {
    bridgeHtml = `<div class="part-head"><span class="eyebrow">Part 1 · Bridge years</span><span class="chip na">None</span></div>
      <h3>No bridge years</h3>
      <p>Stopping at ${stopAge}, super is already unlocked.</p>`;
  } else {
    const chip = c.bridgeOk
      ? `<span class="chip ok">Covered</span>`
      : `<span class="chip bad">Runs short at ${c.bridgeShortAge}</span>`;
    bridgeHtml = `<div class="part-head"><span class="eyebrow">Part 1 · Bridge years</span>${chip}</div>
      <h3>Age ${stopAge} to ${pres}</h3>
      <p><strong class="num">${money(c.outsideAtStop)}</strong> outside super at ${stopAge} has to cover <strong>${c.bridgeYears} ${c.bridgeYears === 1 ? "year" : "years"}</strong> of spending at ${money(input.spending)} a year.</p>`;
  }
  $("part-bridge").innerHTML = bridgeHtml;

  // Part 2: from 60
  const startAge = c.startAge;
  const gap = c.needed - c.pool;
  const chip2 = c.retiredOk
    ? `<span class="chip ok">Enough</span>`
    : `<span class="chip bad">Short by ${money(gap)}</span>`;
  const outsideLeft = Math.max(0, c.outsideAtStart);
  $("part-retired").innerHTML = `<div class="part-head"><span class="eyebrow">Part 2 · From ${startAge}</span>${chip2}</div>
    <h3><span class="num">${money(c.pool)}</span> at ${startAge}</h3>
    <p>Super of <strong class="num">${money(c.superAtStart)}</strong>${outsideLeft > 0 ? ` plus <strong class="num">${money(outsideLeft)}</strong> left outside super` : ""}. At a ${pct(state.w)} withdrawal rate, ${money(input.spending)} a year needs <strong class="num">${money(c.needed)}</strong>.</p>`;

  const warn = $("super-warn");
  if (input.spending > input.takeHome) {
    warn.hidden = false;
    warn.textContent = "Your yearly spending is more than your take-home pay, so savings outside super shrink while you work.";
  } else {
    warn.hidden = true;
  }

  $("super-sum").textContent = `${pct(state.r)} outside super, ${pct(state.rs)} in super, ${pct(state.w)} withdrawal rate`;
  drawBars(input, plan);
}

function drawBars(input, plan) {
  const svg = $("bars");
  const W = svg.getBoundingClientRect().width;
  if (!W) return;
  const pres = DEFAULTS.preservationAge;
  const stopAge = plan.stopAge ?? plan.check.stopAge;
  const endAge = Math.max(90, input.age + 10);
  const rows = project(input, stopAge, endAge);
  const needed = plan.check.needed;

  const H = Math.round(Math.min(320, Math.max(240, W * 0.6)));
  const m = { l: 48, r: 8, t: 30, b: 26 };
  const pw = W - m.l - m.r;
  const ph = H - m.t - m.b;
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
  svg.setAttribute("height", H);
  svg.replaceChildren();

  const totals = rows.map((r) => Math.max(0, r.outside) + Math.max(0, r.super));
  const top = Math.max(1, ...totals, Number.isFinite(needed) ? needed : 0);
  const step = niceStep(top, 4);
  const yMax = Math.ceil(top / step) * step;
  const band = pw / rows.length;
  const barW = Math.max(1, band * 0.72);
  const x = (age) => m.l + (age - rows[0].age) * band;
  const y = (v) => m.t + ph - (v / yMax) * ph;

  for (let v = 0; v <= yMax + 1e-6; v += step) {
    svg.append(el("line", { class: "grid", x1: m.l, x2: W - m.r, y1: y(v), y2: y(v) }));
    svg.append(el("text", { x: m.l - 6, y: y(v) + 4, "text-anchor": "end" }, v === 0 ? "$0" : compactMoney(v)));
  }

  // Shade the bridge years.
  if (stopAge < pres) {
    const x0 = x(stopAge);
    const x1 = x(Math.min(pres, endAge));
    svg.append(el("rect", { x: x0, y: m.t, width: Math.max(0, x1 - x0), height: ph, fill: "var(--ochre)", "fill-opacity": 0.06 }));
  }

  for (const r of rows) {
    const s = Math.max(0, r.super);
    const o = Math.max(0, r.outside);
    const bx = x(r.age) + (band - barW) / 2;
    if (s > 0) svg.append(el("rect", { x: bx, y: y(s), width: barW, height: y(0) - y(s), fill: "var(--primary)", rx: Math.min(2, barW / 3) }));
    if (o > 0) svg.append(el("rect", { x: bx, y: y(s + o), width: barW, height: y(s) - y(s + o), fill: "var(--ochre)", rx: Math.min(2, barW / 3) }));
  }

  // Needed from 60 (or the stopping age, if later).
  if (Number.isFinite(needed) && needed > 0) {
    const from = Math.max(pres, stopAge);
    if (from <= endAge) {
      svg.append(el("line", { x1: x(from), x2: W - m.r, y1: y(needed), y2: y(needed), stroke: "var(--text)", "stroke-width": 1.5, "stroke-dasharray": "5 4" }));
    }
  }

  // Markers for stopping age and preservation age.
  const marker = (age, label, color, row) => {
    if (age < rows[0].age || age > endAge) return;
    const mx = x(age);
    svg.append(el("line", { x1: mx, x2: mx, y1: m.t - 4 - row * 14, y2: y(0), stroke: color, "stroke-width": 1.5 }));
    const anchor = mx > W - 90 ? "end" : "start";
    svg.append(el("text", { x: mx + (anchor === "end" ? -4 : 4), y: m.t - 8 - row * 14, "text-anchor": anchor, style: `fill: ${color}; font-weight: 600` }, label));
  };
  const stopLabel = plan.stopAge === null ? `Shown stopping at ${stopAge}` : `Stop at ${stopAge}`;
  if (stopAge === pres) {
    marker(pres, `${stopLabel} · super unlocks`, "var(--text)", 0);
  } else {
    marker(stopAge, stopLabel, "var(--text)", stopAge > pres ? 0 : 1);
    marker(pres, "Super unlocks", "var(--muted)", stopAge > pres ? 1 : 0);
  }

  // Age labels.
  const every = band * 5 >= 30 ? 5 : 10;
  for (const r of rows) {
    if (r.age % every === 0) {
      svg.append(el("text", { x: x(r.age) + band / 2, y: H - 8, "text-anchor": "middle" }, String(r.age)));
    }
  }

  $("bars").setAttribute(
    "aria-label",
    `Bar chart of balances from age ${rows[0].age} to ${endAge}. ${plan.stopAge === null ? "Shown stopping" : "Stopping"} at ${stopAge}.`,
  );
}

// ---- Wiring ---------------------------------------------------------------
function setMode(mode, focus = false) {
  for (const [name, tab, panel] of [["simple", "tab-simple", "panel-simple"], ["super", "tab-super", "panel-super"]]) {
    const on = name === mode;
    $(tab).setAttribute("aria-selected", String(on));
    $(tab).tabIndex = on ? 0 : -1;
    $(panel).hidden = !on;
    if (on && focus) $(tab).focus();
  }
  if (mode === "simple") renderSimple();
  else renderSuper();
}

$("tab-simple").addEventListener("click", () => setMode("simple"));
$("tab-super").addEventListener("click", () => setMode("super"));
document.querySelector(".modes").addEventListener("keydown", (e) => {
  if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
    e.preventDefault();
    setMode($("tab-simple").getAttribute("aria-selected") === "true" ? "super" : "simple", true);
  }
});
document.querySelectorAll("[data-goto]").forEach((a) =>
  a.addEventListener("click", (e) => {
    e.preventDefault();
    setMode(a.dataset.goto);
    window.scrollTo({ top: 0 });
  }),
);

$("rate").addEventListener("input", renderSimple);

// Assumption inputs: the same assumption can appear in both modes.
document.querySelectorAll("[data-sync]").forEach((input) => {
  input.addEventListener("input", () => {
    const v = Number(input.value);
    const key = input.dataset.sync;
    const ok = input.value !== "" && Number.isFinite(v) && v >= Number(input.min) && v <= Number(input.max);
    input.setAttribute("aria-invalid", String(!ok));
    if (!ok) return;
    state[key] = v / 100;
    document.querySelectorAll(`[data-sync="${key}"]`).forEach((other) => {
      if (other !== input) { other.value = input.value; other.setAttribute("aria-invalid", "false"); }
    });
    renderSimple();
    renderSuper();
  });
});

document.querySelectorAll("#super-form input").forEach((input) => input.addEventListener("input", renderSuper));
document.querySelectorAll("input.money").forEach((input) => {
  input.addEventListener("blur", () => { input.value = int.format(parseMoney(input.value)); });
  input.addEventListener("focus", () => { input.value = input.value.replace(/,/g, ""); });
});
$("super-form").addEventListener("submit", (e) => e.preventDefault());

let resizeTimer;
let lastWidth = 0;
new ResizeObserver(([entry]) => {
  const width = Math.round(entry.contentRect.width);
  if (width === lastWidth) return;
  lastWidth = width;
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => { drawCurve(); renderSuper(); }, 60);
}).observe(document.querySelector(".sw .page"));

setMode("simple");
renderSuper();
