// Would your money have lasted? — the maths.
//
// Ported on 18 November 2026 from James's prototype in
// C:\Projects\go\investor-tools (tools/money-lasted/calc.js), with the
// published artifact's later change: simulate keeps every run's path for the
// chart. The prototype's calc.test.js tests this file too: point its import
// here and run it after any change.
//
// Pure functions: numbers in, numbers out. No DOM, no libraries, no globals.
// Everything is in real terms (after inflation). Spending that "rises with
// inflation" is therefore a constant real amount, and balances are in
// start-date dollars.

export const DEFAULTS = Object.freeze({
  balance: 1_000_000,
  spending: 40_000, // a year, in today's dollars
  years: 30,
  shares: 1, // share of the portfolio in shares; the rest is in bonds
  fees: 0.005, // a year, as a share of the balance
});

// ---------------------------------------------------------------------------
// Monthly returns from the data
// ---------------------------------------------------------------------------

// shareReturns turns a real total return index (dividends reinvested) into
// monthly real returns. returns[i] is the return from month i to month i + 1,
// so there is one fewer return than index values.
export function shareReturns(realTotalReturn) {
  const out = new Float64Array(realTotalReturn.length - 1);
  for (let i = 0; i < out.length; i++) {
    out[i] = realTotalReturn[i + 1] / realTotalReturn[i] - 1;
  }
  return out;
}

// bondPrice is the price, per $1 of face value, of a bond paying a yearly
// coupon in `paymentsPerYear` instalments, with `years` to maturity, when the
// market yield is `yieldRate`. Rates are decimals (0.05 is 5%).
export function bondPrice(coupon, yieldRate, years, paymentsPerYear = 2) {
  const n = years * paymentsPerYear;
  const i = yieldRate / paymentsPerYear;
  if (i === 0) return 1 + coupon * years;
  const v = (1 + i) ** -n;
  return ((coupon / paymentsPerYear) * (1 - v)) / i + v;
}

// bondReturn is the nominal return for one month on a 10-year government bond,
// worked out from the 10-year yield (GS10) the usual way: buy a new 10-year
// bond at par at the start of the month, so its coupon equals that month's
// yield; earn a month of coupon; then value it at the next month's yield.
// Yields are in percent a year, as in Shiller's data.
export function bondReturn(yieldNow, yieldNext) {
  const c = yieldNow / 100;
  const y = yieldNext / 100;
  return bondPrice(c, y, 10, 2) - 1 + c / 12;
}

// bondRealReturns gives monthly real bond returns: the nominal return from
// bondReturn, deflated by that month's change in CPI.
export function bondRealReturns(gs10, cpi) {
  const out = new Float64Array(gs10.length - 1);
  for (let i = 0; i < out.length; i++) {
    const nominal = 1 + bondReturn(gs10[i], gs10[i + 1]);
    out[i] = (nominal * cpi[i]) / cpi[i + 1] - 1;
  }
  return out;
}

// blendReturns mixes share and bond returns, rebalanced to the same split
// every month.
export function blendReturns(shares, bonds, shareWeight) {
  const w = Math.min(1, Math.max(0, shareWeight));
  const out = new Float64Array(shares.length);
  for (let i = 0; i < out.length; i++) {
    out[i] = w * shares[i] + (1 - w) * bonds[i];
  }
  return out;
}

// ---------------------------------------------------------------------------
// One retirement period
// ---------------------------------------------------------------------------

// runPeriod steps through `months` months of `returns` from index `start`.
// Each month it takes out a month's spending, applies that month's real
// return, then takes off a month's fees.
//
// The money runs out in the first month that the balance can't pay a full
// month's spending. The balance is 0 from then on.
//
// It returns:
//   lasted       true if the money never ran out
//   monthsLasted months of spending paid in full
//   end          balance after the last month (0 if it ran out)
//   min          lowest balance after any month
//   path         balances at the start and after each month (if keepPath)
export function runPeriod(returns, start, months, plan, keepPath = false) {
  const spend = plan.spending / 12;
  const feeKeep = 1 - plan.fees / 12;
  let b = plan.balance;
  let min = b;
  let monthsLasted = months;
  const path = keepPath ? new Float64Array(months + 1) : null;
  if (path) path[0] = b;

  for (let m = 0; m < months; m++) {
    if (monthsLasted === months) {
      if (b < spend - 1e-9) {
        monthsLasted = m;
        b = 0;
      } else {
        b = (b - spend) * (1 + returns[start + m]) * feeKeep;
        if (b < 0) b = 0; // a return below −100% can't happen, but stay safe
      }
    }
    if (b < min) min = b;
    if (path) path[m + 1] = b;
  }
  return { lasted: monthsLasted === months, monthsLasted, end: b, min, path };
}

// ---------------------------------------------------------------------------
// Every period in history
// ---------------------------------------------------------------------------

// simulate runs the plan from every start month that has a complete run of
// `years` years of returns. `returns` is the blended monthly series.
//
// It returns:
//   months       length of each period in months
//   tested       how many start months were tested
//   succeeded    how many of them lasted
//   rate         succeeded ÷ tested
//   periods      one { start, lasted, monthsLasted, end, min } per start month
//   worst        the period that ran out soonest, or if none ran out, the one
//                with the lowest ending balance
//   median       the period with the median ending balance (ran out counts
//                as 0); median.end and median.min are the median ending
//                balance and the lowest balance on that path
export function simulate(returns, plan) {
  const months = Math.round(plan.years * 12);
  const tested = Math.max(0, returns.length - months + 1);
  const periods = new Array(tested);
  const paths = new Array(tested); // full runs with paths, reused by the chart
  let succeeded = 0;
  for (let s = 0; s < tested; s++) {
    const r = runPeriod(returns, s, months, plan, true);
    paths[s] = r;
    periods[s] = { start: s, lasted: r.lasted, monthsLasted: r.monthsLasted, end: r.end, min: r.min };
    if (r.lasted) succeeded++;
  }

  let worst = null;
  for (const p of periods) {
    if (
      !worst ||
      p.monthsLasted < worst.monthsLasted ||
      (p.monthsLasted === worst.monthsLasted && p.end < worst.end)
    ) {
      worst = p;
    }
  }

  let median = null;
  if (tested > 0) {
    const byEnd = [...periods].sort((a, b) => a.end - b.end || a.start - b.start);
    median = byEnd[Math.floor((tested - 1) / 2)];
  }

  return {
    months,
    tested,
    succeeded,
    rate: tested ? succeeded / tested : 0,
    periods,
    paths,
    worst,
    median,
  };
}

// ---------------------------------------------------------------------------
// Helpers for dates in "YYYY-MM" form
// ---------------------------------------------------------------------------

// indexOfMonth finds the index of a "YYYY-MM" date in a list that starts at
// `first` and has one entry a month. It returns -1 if it's before the start.
export function indexOfMonth(first, date) {
  const [fy, fm] = first.split("-").map(Number);
  const [y, m] = date.split("-").map(Number);
  const i = (y - fy) * 12 + (m - fm);
  return i >= 0 ? i : -1;
}

// yearsAndMonths splits a count of months into whole years and months.
export function yearsAndMonths(months) {
  return { years: Math.floor(months / 12), months: months % 12 };
}

