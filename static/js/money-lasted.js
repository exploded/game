// Would your money have lasted? (/tools/money-lasted): the page. Ported on 18
// November 2026 from the prototype's published artifact. Everything runs in
// the browser: nothing typed is sent anywhere or saved.
import {
  shareReturns,
  bondRealReturns,
  blendReturns,
  runPeriod,
  simulate,
  indexOfMonth,
  yearsAndMonths,
} from "./money-lasted-calc.js";
// The tool's wrapper holds the Shiller data's URL (static/data/shiller.json)
// and the colour tokens the canvas charts read.
const TOOL = document.querySelector("[data-tool]");
const DATA = await (await fetch(TOOL.dataset.data)).json();

const $ = (id) => document.getElementById(id);
const SHARES = shareReturns(DATA.realTotalReturn);
const BONDS = bondRealReturns(DATA.gs10, DATA.cpi);

const NAMED = [
  { year: 1929, color: "--pink", note: "The Great Crash" },
  { year: 1966, color: "--warning", note: "High inflation ahead" },
  { year: 1973, color: "--violet", note: "Oil shock and bear market" },
  { year: 2000, color: "--blue", note: "The dot-com peak" },
  { year: 2008, color: "--green", note: "The global financial crisis" },
];
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// ---- Formatting -----------------------------------------------------------
const aud = new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD", maximumFractionDigits: 0 });
const int = new Intl.NumberFormat("en-AU", { maximumFractionDigits: 0 });
const money = (n) => aud.format(Math.round(n));
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
const monthName = (i) => {
  const [y, m] = DATA.date[i].split("-").map(Number);
  return `${MONTHS[m - 1]} ${y}`;
};
function duration(months) {
  const { years, months: m } = yearsAndMonths(months);
  const parts = [];
  if (years) parts.push(`${years} ${years === 1 ? "year" : "years"}`);
  if (m) parts.push(`${m} ${m === 1 ? "month" : "months"}`);
  return parts.join(" ") || "less than a month";
}
const token = (name) => getComputedStyle(TOOL).getPropertyValue(name).trim();
function rgba(hex, a) {
  const h = hex.replace("#", "");
  const n = parseInt(h.length === 3 ? h.replace(/./g, "$&$&") : h, 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${a})`;
}

// ---- Inputs ---------------------------------------------------------------
const parseMoney = (s) => {
  const n = Number(String(s).replace(/[^0-9.]/g, ""));
  return Number.isFinite(n) ? n : 0;
};
function setPct(slider) {
  slider.style.setProperty("--pct", `${((slider.value - slider.min) / (slider.max - slider.min)) * 100}%`);
}
function readPlan() {
  const feesInput = $("fees");
  let fees = Number(feesInput.value);
  const feesOk = feesInput.value !== "" && Number.isFinite(fees) && fees >= 0 && fees <= 5;
  feesInput.setAttribute("aria-invalid", String(!feesOk));
  if (!feesOk) fees = 0.5;
  return {
    balance: parseMoney($("balance").value),
    spending: parseMoney($("spending").value),
    years: Number($("years").value),
    shares: Number($("shares").value) / 100,
    fees: fees / 100,
  };
}

// ---- Render ---------------------------------------------------------------
let last = null;
const shown = new Set(); // named start years drawn on the chart
let picked = null; // start month picked on the lower chart

function render() {
  const plan = readPlan();
  const years = $("years");
  const shares = $("shares");
  setPct(years);
  setPct(shares);
  $("years-out").textContent = `${years.value} years`;
  const sh = Number(shares.value);
  $("shares-out").textContent = sh === 100 ? "100% shares" : sh === 0 ? "100% bonds" : `${sh}% shares · ${100 - sh}% bonds`;
  const rate = plan.balance > 0 ? plan.spending / plan.balance : 0;
  $("spending-hint").textContent = plan.balance > 0
    ? `In today's dollars, rising with inflation. That's ${trim(rate * 100, 1)}% of your starting balance in the first year.`
    : "In today's dollars. It rises with inflation every year.";

  const returns = blendReturns(SHARES, BONDS, plan.shares);
  const result = simulate(returns, plan);
  last = { plan, returns, result, paths: result.paths, band: bandOf(result.paths, result.months) };
  if (picked !== null && picked >= result.tested) picked = null;

  // Headline
  const { tested, succeeded } = result;
  const pct = tested ? Math.round(result.rate * 100) : 0;
  const pctText = tested && succeeded < tested && pct === 100 ? ">99" : tested && succeeded > 0 && pct === 0 ? "<1" : String(pct);
  const tone = result.rate >= 0.9 ? "good" : result.rate >= 0.7 ? "mid" : "poor";
  $("headline").innerHTML = `Your money lasted in <span class="big ${tone}">${int.format(succeeded)} of ${int.format(tested)}</span> historical periods (${pctText}%)`;
  $("meter-ok").style.width = `${result.rate * 100}%`;
  $("meter-bad").style.width = `${(1 - result.rate) * 100}%`;
  $("n-ok").textContent = int.format(succeeded);
  $("n-bad").textContent = int.format(tested - succeeded);
  if (tested) {
    $("tested").textContent = `Tested ${int.format(tested)} start months, from ${monthName(0)} to ${monthName(tested - 1)}. Later start dates are left out because their ${plan.years} years haven't finished yet.`;
  }

  // Three figures
  const w = result.worst;
  $("worst-date").textContent = monthName(w.start);
  $("worst-date").classList.toggle("bad", !w.lasted);
  $("worst-what").textContent = w.lasted
    ? `The lowest ending balance of any period: ${money(w.end)} after ${plan.years} years. The money lasted in every period.`
    : `The money ran out soonest from this start: after ${duration(w.monthsLasted)}.`;
  // The typical path: the median balance across all periods, month by month.
  const p50 = last.band.p50;
  let lowAt = 0;
  for (let i = 1; i < p50.length; i++) if (p50[i] < p50[lowAt]) lowAt = i;
  $("median-end").textContent = money(result.median.end);
  $("median-min").textContent = money(p50[lowAt]);
  $("median-what").textContent = lowAt === 0
    ? "The typical balance never dropped below where it started."
    : `The typical balance, the middle of all periods month by month, was lowest ${duration(lowAt)} in.`;

  const span = tested ? `${monthName(0)} to ${monthName(tested - 1)}` : "";
  $("chart-sub").textContent = `Each line is ${money(plan.balance)}, spending ${money(plan.spending)} a year for ${plan.years} years, started in a different year from ${span} (one line a year; the figures above use every month). In today's dollars.`;
  $("ends-sub").textContent = `The balance left after ${plan.years} years, by the month the plan started. Red marks under the axis show start dates where the money ran out.`;

  renderNamed();
  renderPick();
  draw();
}

function namedRuns() {
  const { plan, returns, result } = last;
  return NAMED.map((n) => {
    const start = indexOfMonth(DATA.first, `${n.year}-01`);
    const available = returns.length - start;
    const months = Math.min(result.months, available);
    const run = runPeriod(returns, start, months, plan, true);
    return { ...n, start, months, partial: months < result.months, run };
  });
}

function outcome(run, months, partial) {
  const { plan } = last;
  if (!run.lasted) return `<b class="bad">Ran out</b> after ${duration(run.monthsLasted)}`;
  if (partial) return `So far, ${duration(months)} in: <b>${money(run.end)}</b>. Not counted, as the ${plan.years} years haven't finished.`;
  return `<b class="ok">Lasted</b>, ending with <b>${money(run.end)}</b>`;
}

function renderNamed() {
  $("named").innerHTML = namedRuns()
    .map((n) => {
      const on = shown.has(n.year);
      return `<li><button type="button" class="named-btn" data-year="${n.year}" aria-pressed="${on}"><i class="swatch" style="background:var(${n.color})"></i><span class="ttl"><span class="yr">January ${n.year}</span><span class="note">${n.note}</span></span><span class="toggle">${on ? "Shown" : "Show"}</span><span class="what">${outcome(n.run, n.months, n.partial)}</span></button></li>`;
    })
    .join("");
}

function renderPick() {
  const { result } = last;
  const readout = $("pick-readout");
  $("pick-clear").hidden = picked === null;
  if (picked === null) {
    readout.textContent = "Tap or point at a start date to draw it on the chart above.";
    return;
  }
  const p = result.periods[picked];
  const low = p.lasted && p.min < p.end - 0.5 ? ` Its lowest point was <b>${money(p.min)}</b>.` : "";
  readout.innerHTML = `Starting in <b>${monthName(p.start)}</b>: ${outcome(p, result.months, false)}.${low}`;
}

// Balances at each month across every period: 10th and 90th percentiles.
function bandOf(paths, months) {
  const p10 = new Float64Array(months + 1);
  const p90 = new Float64Array(months + 1);
  const p50 = new Float64Array(months + 1);
  const n = paths.length;
  if (!n) return { p10, p50, p90 };
  const col = new Float64Array(n);
  const k10 = Math.floor((n - 1) * 0.1);
  const k50 = Math.floor((n - 1) * 0.5);
  const k90 = Math.floor((n - 1) * 0.9);
  for (let i = 0; i <= months; i++) {
    for (let k = 0; k < n; k++) col[k] = paths[k].path[i];
    // Partial selection instead of a full sort: much faster while dragging a slider.
    p90[i] = nth(col, 0, n - 1, k90);
    p50[i] = nth(col, 0, k90, k50);
    p10[i] = nth(col, 0, k50, k10);
  }
  return { p10, p50, p90 };
}

// nth puts the k-th smallest value of a[lo..hi] at a[k] and returns it
// (Hoare's quickselect). Values left of k end up ≤ it, right of k ≥ it.
function nth(a, lo, hi, k) {
  while (hi > lo) {
    const pivot = a[(lo + hi) >> 1];
    let i = lo, j = hi;
    while (i <= j) {
      while (a[i] < pivot) i++;
      while (a[j] > pivot) j--;
      if (i <= j) { const t = a[i]; a[i] = a[j]; a[j] = t; i++; j--; }
    }
    if (k <= j) hi = j;
    else if (k >= i) lo = i;
    else break;
  }
  return a[k];
}

function setupCanvas(canvas, W, H) {
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.round(W * dpr);
  canvas.height = Math.round(H * dpr);
  canvas.style.height = `${H}px`;
  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, W, H);
  ctx.font = `12px ${token("--font")}`;
  return ctx;
}

function yAxis(ctx, { m, W, y, yMax, step }) {
  ctx.fillStyle = token("--muted");
  ctx.strokeStyle = token("--border");
  ctx.lineWidth = 1;
  ctx.textAlign = "right";
  ctx.textBaseline = "middle";
  for (let v = 0; v <= yMax + 1e-6; v += step) {
    ctx.beginPath();
    ctx.moveTo(m.l, Math.round(y(v)) + 0.5);
    ctx.lineTo(W - m.r, Math.round(y(v)) + 0.5);
    ctx.stroke();
    ctx.fillText(v === 0 ? "$0" : compactMoney(v), m.l - 6, y(v));
  }
  ctx.textAlign = "center";
  ctx.textBaseline = "alphabetic";
}

function drawPaths() {
  if (!last) return;
  const { plan, returns, result } = last;
  const canvas = $("paths");
  const W = $("chart-wrap").clientWidth;
  if (!W) return;
  const H = W < 520 ? 300 : 360;
  const ctx = setupCanvas(canvas, W, H);
  const m = { l: 50, r: 10, t: 12, b: 26 };
  const pw = W - m.l - m.r;
  const ph = H - m.t - m.b;
  const months = result.months;

  // Paths and the band for every complete period, kept until the plan changes.
  last.paths ??= result.periods.map((p) => runPeriod(returns, p.start, months, plan, true));
  const paths = last.paths;
  last.band ??= bandOf(paths, months);
  const { p10, p90 } = last.band;

  // Scale: the band and the start balance fit; the highest lines may run off the top.
  const top = Math.max(plan.balance * 1.25, ...p90, 1);
  const step = niceStep(top, 4);
  const yMax = Math.ceil(top / step) * step;
  const x = (i) => m.l + (i / months) * pw;
  const y = (v) => m.t + ph - (Math.min(v, yMax * 1.05) / yMax) * ph;

  yAxis(ctx, { m, W, y, yMax, step });
  const every = plan.years <= 20 ? 5 : (pw / plan.years) * 5 >= 40 ? 5 : 10;
  for (let yr = 0; yr <= plan.years; yr += every) {
    ctx.fillText(yr === 0 ? "Start" : `${yr}y`, Math.min(x(yr * 12), W - m.r - 10), H - 8);
  }

  ctx.save();
  ctx.beginPath();
  ctx.rect(m.l, m.t - 2, pw + 2, ph + 4);
  ctx.clip();

  // `every` thins the faint lines to about one point per 2 pixels.
  const trace = (p, n = p.path.length - 1, every = 1) => {
    ctx.moveTo(x(0), y(p.path[0]));
    const stopAt = p.lasted ? n : Math.min(n, p.monthsLasted + 1);
    for (let i = every; i < stopAt; i += every) ctx.lineTo(x(i), y(p.path[i]));
    ctx.lineTo(x(stopAt), y(p.path[stopAt]));
  };
  const thin = Math.max(1, Math.floor(months / (pw / 2)));

  // Middle 80% band.
  ctx.fillStyle = rgba(token("--primary"), 0.16);
  ctx.beginPath();
  ctx.moveTo(x(0), y(p90[0]));
  for (let i = 1; i <= months; i++) ctx.lineTo(x(i), y(p90[i]));
  for (let i = months; i >= 0; i--) ctx.lineTo(x(i), y(p10[i]));
  ctx.closePath();
  ctx.fill();

  // Every period: those that lasted, then those that ran out on top.
  ctx.lineWidth = 1;
  // Neighbouring start months follow almost the same path, so with many
  // periods draw one start a year: each line stays visible instead of
  // 1,500 of them blurring into a solid fill. Every period that ran out is
  // drawn. The figures still use all periods.
  const stride = paths.length > 600 ? 12 : 1;
  ctx.strokeStyle = rgba(token("--primary"), stride > 1 ? 0.35 : 0.2);
  ctx.beginPath();
  for (let k = 0; k < paths.length; k += stride) if (paths[k].lasted) trace(paths[k], months, thin);
  ctx.stroke();
  ctx.strokeStyle = rgba(token("--red"), stride > 1 ? 0.35 : 0.3);
  ctx.beginPath();
  for (const p of paths) if (!p.lasted) trace(p, months, thin);
  ctx.stroke();
  ctx.lineJoin = "round";

  // Edges of the middle 80% band, on top of the lines.
  ctx.strokeStyle = rgba(token("--primary"), 0.9);
  ctx.lineWidth = 1.5;
  for (const edge of [p10, p90]) {
    ctx.beginPath();
    ctx.moveTo(x(0), y(edge[0]));
    for (let i = 1; i <= months; i++) ctx.lineTo(x(i), y(edge[i]));
    ctx.stroke();
  }

  // Starting balance.
  ctx.setLineDash([2, 4]);
  ctx.strokeStyle = rgba(token("--text"), 0.35);
  ctx.beginPath();
  ctx.moveTo(m.l, y(plan.balance));
  ctx.lineTo(W - m.r, y(plan.balance));
  ctx.stroke();

  // Typical path: the median balance of all periods at each month.
  ctx.setLineDash([5, 4]);
  ctx.lineWidth = 2;
  ctx.strokeStyle = token("--text");
  ctx.beginPath();
  ctx.moveTo(x(0), y(last.band.p50[0]));
  for (let i = 1; i <= months; i++) ctx.lineTo(x(i), y(last.band.p50[i]));
  ctx.stroke();
  ctx.setLineDash([]);

  // Worst period.
  const worst = paths[result.worst.start];
  ctx.strokeStyle = token("--red");
  ctx.lineWidth = 2;
  ctx.beginPath();
  trace(worst);
  ctx.stroke();

  const endDot = (run, color) => {
    if (run.lasted) return;
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.arc(x(run.monthsLasted), y(0), 4, 0, Math.PI * 2);
    ctx.fill();
  };
  endDot(worst, token("--red"));

  // Named start years the viewer turned on.
  ctx.lineWidth = 2.5;
  for (const n of namedRuns()) {
    if (!shown.has(n.year)) continue;
    ctx.strokeStyle = token(n.color);
    ctx.beginPath();
    trace(n.run, n.months);
    ctx.stroke();
    endDot(n.run, token(n.color));
  }

  // The start date picked on the lower chart.
  if (picked !== null) {
    const p = paths[picked];
    ctx.strokeStyle = token("--text");
    ctx.lineWidth = 3;
    ctx.beginPath();
    trace(p);
    ctx.stroke();
    endDot(p, token("--text"));
  }
  ctx.restore();

  if (picked !== null) {
    const p = paths[picked];
    const at = p.lasted ? months : p.monthsLasted;
    const lx = x(at);
    ctx.fillStyle = token("--text");
    ctx.font = `600 12px ${token("--font")}`;
    ctx.textAlign = lx > W - 80 ? "right" : "left";
    ctx.fillText(monthName(picked), lx + (lx > W - 80 ? -8 : 8), Math.max(m.t + 12, y(p.path[at]) - 8));
  }

  canvas.setAttribute(
    "aria-label",
    `Line chart of a ${money(plan.balance)} plan spending ${money(plan.spending)} a year over ${plan.years} years, for ${int.format(result.tested)} historical start months. ${int.format(result.succeeded)} lasted.`,
  );
}

const ENDS_M = { l: 50, r: 10, t: 10, b: 40 };

function drawEnds() {
  if (!last) return;
  const { plan, result } = last;
  const canvas = $("ends");
  const W = $("ends-wrap").clientWidth;
  if (!W || !result.tested) return;
  const H = W < 520 ? 200 : 230;
  const ctx = setupCanvas(canvas, W, H);
  const m = ENDS_M;
  const pw = W - m.l - m.r;
  const ph = H - m.t - m.b;
  const n = result.tested;
  const ends = result.periods.map((p) => p.end);

  const sorted = [...ends].sort((a, b) => a - b);
  const top = Math.max(plan.balance * 1.25, sorted[Math.floor((n - 1) * 0.95)], 1);
  const step = niceStep(top, 3);
  const yMax = Math.ceil(top / step) * step;
  const x = (i) => m.l + (n === 1 ? pw / 2 : (i / (n - 1)) * pw);
  const y = (v) => m.t + ph - (Math.min(v, yMax * 1.04) / yMax) * ph;

  yAxis(ctx, { m, W, y, yMax, step });

  // Ending balance for each start month.
  ctx.save();
  ctx.beginPath();
  ctx.rect(m.l, m.t - 2, pw + 2, ph + 2);
  ctx.clip();
  ctx.beginPath();
  ctx.moveTo(x(0), y(0));
  for (let i = 0; i < n; i++) ctx.lineTo(x(i), y(ends[i]));
  ctx.lineTo(x(n - 1), y(0));
  ctx.closePath();
  ctx.fillStyle = rgba(token("--primary"), 0.3);
  ctx.fill();
  ctx.strokeStyle = token("--primary");
  ctx.lineWidth = 1;
  ctx.beginPath();
  for (let i = 0; i < n; i++) (i ? ctx.lineTo : ctx.moveTo).call(ctx, x(i), y(ends[i]));
  ctx.stroke();

  ctx.setLineDash([2, 4]);
  ctx.strokeStyle = rgba(token("--text"), 0.35);
  ctx.beginPath();
  ctx.moveTo(m.l, y(plan.balance));
  ctx.lineTo(W - m.r, y(plan.balance));
  ctx.stroke();
  ctx.setLineDash([]);
  ctx.restore();

  // Red marks under the axis where the money ran out.
  const tick = Math.max(1.5, pw / n);
  ctx.fillStyle = token("--red");
  for (let i = 0; i < n; i++) {
    if (!result.periods[i].lasted) ctx.fillRect(x(i) - tick / 2, y(0) + 4, tick, 8);
  }

  // Start years.
  ctx.fillStyle = token("--muted");
  const span = Math.ceil(n / 12);
  const gap = span > 80 ? 20 : 10;
  const firstYear = Number(DATA.first.slice(0, 4));
  for (let yr = Math.ceil(firstYear / gap) * gap; ; yr += gap) {
    const i = indexOfMonth(DATA.first, `${yr}-01`);
    if (i >= n) break;
    if (i >= 0) ctx.fillText(String(yr), x(i), H - 8);
  }

  if (picked !== null && picked < n) {
    const px = Math.round(x(picked)) + 0.5;
    ctx.strokeStyle = token("--text");
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    ctx.moveTo(px, m.t);
    ctx.lineTo(px, y(0) + 12);
    ctx.stroke();
    ctx.fillStyle = token("--text");
    ctx.beginPath();
    ctx.arc(px, y(ends[picked]), 4, 0, Math.PI * 2);
    ctx.fill();
  }

  canvas.setAttribute(
    "aria-label",
    `Ending balance after ${plan.years} years for each start month from ${monthName(0)} to ${monthName(n - 1)}. The money ran out for ${int.format(n - result.succeeded)} start months.`,
  );
}

function draw() {
  drawPaths();
  drawEnds();
}

// ---- Wiring ---------------------------------------------------------------
let frame = 0;
const schedule = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(render); };
document.querySelectorAll("#plan input").forEach((input) => input.addEventListener("input", schedule));
document.querySelectorAll("input.money").forEach((input) => {
  input.addEventListener("blur", () => { input.value = int.format(parseMoney(input.value)); });
  input.addEventListener("focus", () => { input.value = input.value.replace(/,/g, ""); });
});
$("plan").addEventListener("submit", (e) => e.preventDefault());

$("named").addEventListener("click", (e) => {
  const btn = e.target.closest(".named-btn");
  if (!btn) return;
  const year = Number(btn.dataset.year);
  if (shown.has(year)) shown.delete(year);
  else shown.add(year);
  renderNamed();
  drawPaths();
});

// Pick a start date on the lower chart.
let pickFrame = 0;
function pickAt(clientX) {
  if (!last || !last.result.tested) return;
  const rect = $("ends").getBoundingClientRect();
  const n = last.result.tested;
  const pw = rect.width - ENDS_M.l - ENDS_M.r;
  const i = Math.round(((clientX - rect.left - ENDS_M.l) / pw) * (n - 1));
  const next = Math.min(n - 1, Math.max(0, i));
  if (next === picked) return;
  picked = next;
  cancelAnimationFrame(pickFrame);
  pickFrame = requestAnimationFrame(() => { renderPick(); draw(); });
}
const ends = $("ends");
ends.addEventListener("pointerdown", (e) => pickAt(e.clientX));
ends.addEventListener("pointermove", (e) => {
  if (e.pointerType === "mouse" || e.buttons) pickAt(e.clientX);
});
$("pick-clear").addEventListener("click", () => { picked = null; renderPick(); draw(); });

let lastWidth = 0;
new ResizeObserver(([entry]) => {
  const width = Math.round(entry.contentRect.width);
  if (width !== lastWidth) { lastWidth = width; draw(); }
}).observe($("chart-wrap"));

render();
