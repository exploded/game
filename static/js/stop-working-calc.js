// When can I stop working? — the maths.
//
// Ported on 18 November 2026 from James's prototype in
// C:\Projects\go\investor-tools (tools/stop-working/calc.js). The prototype's
// calc.test.js tests this file too: point its import here and run it after
// any change.
//
// Pure functions: numbers in, numbers out. No DOM, no libraries, no globals.
// All amounts are in today's dollars and all returns are real (after
// inflation), so nothing needs indexing.

export const DEFAULTS = Object.freeze({
  realReturn: 0.05, // real return on money outside super
  superReturn: 0.05, // real return inside super, after fund tax
  withdrawalRate: 0.04, // share of savings taken out in the first year of retirement
  superGuarantee: 0.12, // employer super, share of salary before tax
  contributionsTax: 0.15, // tax on employer contributions as they enter the fund
  preservationAge: 60, // super is locked until this age (born after 30 June 1964)
  maxAge: 75, // stop searching after this age
});

// ---------------------------------------------------------------------------
// Simple mode
// ---------------------------------------------------------------------------

// yearsToStop returns how many years of saving it takes, starting from
// nothing, until savings reach yearly spending ÷ withdrawal rate.
//
// Each year you invest savingsRate × income at the end of the year and spend
// the rest. Income cancels out, so only the rate matters. The result is a
// fractional number of years. It returns Infinity when the target can never
// be reached (for example, a 0% savings rate, or a negative return that caps
// savings below the target).
export function yearsToStop(
  savingsRate,
  realReturn = DEFAULTS.realReturn,
  withdrawalRate = DEFAULTS.withdrawalRate,
) {
  const s = savingsRate;
  const r = realReturn;
  const w = withdrawalRate;
  if (!(w > 0)) return Infinity;
  if (s >= 1) return 0;
  if (!(s > 0)) return Infinity;

  // Target balance measured in years of contributions.
  const target = (1 - s) / (w * s);
  if (r === 0) return target;

  // Future value of end-of-year contributions: ((1 + r)^n − 1) / r = target.
  const x = 1 + r * target;
  if (!(x > 0) || r <= -1) return Infinity;
  return Math.log(x) / Math.log(1 + r);
}

// savingsCurve returns yearsToStop for each rate, for tables and charts.
export function savingsCurve(rates, realReturn, withdrawalRate) {
  return rates.map((rate) => ({
    rate,
    years: yearsToStop(rate, realReturn, withdrawalRate),
  }));
}

// ---------------------------------------------------------------------------
// Australian mode: add your super
// ---------------------------------------------------------------------------

// normalise fills in defaults and turns the input into plain numbers.
function normalise(input) {
  const p = { ...DEFAULTS, ...input };
  for (const key of [
    "age",
    "takeHome",
    "spending",
    "outside",
    "superBalance",
    "salary",
  ]) {
    p[key] = Number(p[key]) || 0;
  }
  p.age = Math.floor(p.age);
  return p;
}

// netSuperContribution is the employer contribution that lands in the fund
// each year: Super Guarantee on salary before tax, less contributions tax.
export function netSuperContribution(
  salary,
  superGuarantee = DEFAULTS.superGuarantee,
  contributionsTax = DEFAULTS.contributionsTax,
) {
  return Math.max(0, salary) * superGuarantee * (1 - contributionsTax);
}

// project steps through the years from the current age to endAge, stopping
// work at stopAge. It returns one row per age with the balances at the start
// of that age.
//
//   Working years: the gap between take-home pay and spending goes into
//   savings outside super, and employer super goes into super. Both are added
//   at the end of the year, as in simple mode.
//   Bridge years (stopping age to preservation age): a year's spending comes
//   out of savings outside super at the start of the year. Super grows
//   untouched.
//   From preservation age: a year's spending comes out at the start of the
//   year, from savings outside super first, then from super.
export function project(input, stopAge, endAge) {
  const p = normalise(input);
  const contribution = netSuperContribution(
    p.salary,
    p.superGuarantee,
    p.contributionsTax,
  );
  let outside = p.outside;
  let sup = p.superBalance;
  const rows = [];
  const phaseAt = (age) =>
    age < stopAge ? "work" : age < p.preservationAge ? "bridge" : "retired";

  for (let age = p.age; age <= endAge; age++) {
    const phase = phaseAt(age);
    rows.push({ age, outside, super: sup, phase });
    if (age === endAge) break;

    if (phase === "work") {
      outside = outside * (1 + p.realReturn) + (p.takeHome - p.spending);
      sup = sup * (1 + p.superReturn) + contribution;
    } else if (phase === "bridge") {
      outside = (outside - p.spending) * (1 + p.realReturn);
      sup = sup * (1 + p.superReturn);
    } else {
      const fromOutside = Math.min(Math.max(outside, 0), p.spending);
      outside -= fromOutside;
      sup = Math.max(0, sup - (p.spending - fromOutside));
      outside *= 1 + p.realReturn;
      sup *= 1 + p.superReturn;
    }
  }
  return rows;
}

// checkStopAge tests whether stopping work at stopAge works. Both parts must
// hold:
//
//   Bridge: savings outside super cover every year's spending from the
//   stopping age until preservation age.
//   From preservation age (or the stopping age, if later): super plus any
//   savings left outside super are at least spending ÷ withdrawal rate.
export function checkStopAge(input, stopAge) {
  const p = normalise(input);
  const startAge = Math.max(stopAge, p.preservationAge);
  const rows = project(p, stopAge, startAge);
  const needed = p.withdrawalRate > 0 ? p.spending / p.withdrawalRate : Infinity;

  let bridgeOk = true;
  let bridgeShortAge = null;
  for (const row of rows) {
    if (row.phase === "bridge" && row.outside < p.spending - 1e-6) {
      bridgeOk = false;
      bridgeShortAge = row.age;
      break;
    }
  }

  const atStop = rows.find((row) => row.age === stopAge) ?? rows[0];
  const atStart = rows[rows.length - 1];
  const pool = atStart.super + atStart.outside;
  const retiredOk = pool >= needed - 1e-6;

  return {
    stopAge,
    ok: bridgeOk && retiredOk,
    bridgeOk,
    bridgeShortAge,
    retiredOk,
    bridgeYears: Math.max(0, p.preservationAge - stopAge),
    outsideAtStop: atStop.outside,
    superAtStop: atStop.super,
    startAge, // the age the withdrawal-rate test is applied
    outsideAtStart: atStart.outside,
    superAtStart: atStart.super,
    pool,
    needed,
  };
}

// planWithSuper finds the earliest whole age, from the current age up to
// maxAge, where both parts of the plan work. stopAge is null when no age up
// to maxAge works; check then describes stopping at maxAge.
export function planWithSuper(input) {
  const p = normalise(input);
  const maxAge = Math.max(p.age, p.maxAge);
  for (let age = p.age; age <= maxAge; age++) {
    const check = checkStopAge(p, age);
    if (check.ok) {
      return { stopAge: age, yearsFromNow: age - p.age, check };
    }
  }
  return { stopAge: null, yearsFromNow: null, check: checkStopAge(p, maxAge) };
}

