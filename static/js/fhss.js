// First Home Super Saver Calculator (/tools/fhss), rebuilt from Rob's
// spreadsheet "CPW_FHSS_Calculator_v2" (November 2026). Everything happens
// here in the browser: nothing typed is sent anywhere or saved, not even to
// localStorage.
//
// The sums follow the sheet's hidden columns (FHSS Calculator!L:T) and its
// Settings & Notes sheet, so the results match it to the cent:
// - Each year up to the purchase year, employer super is salary x SG rate.
//   The contribution counted is the amount entered, capped by what's left of
//   the $32,500 concessional cap after employer super, the $15,000 yearly
//   limit, and the $50,000 total less amounts already contributed and
//   counted in earlier years.
// - 85% of each counted contribution can be released. Associated earnings
//   are that 85% grown at the earnings rate for (purchase year - year + 0.5)
//   years: paid mid-year, released at the end of the purchase year.
// - Tax on release: (85% of counted contributions + earnings) x (marginal
//   rate - 30% offset), never below 0. The marginal rate (with the 2%
//   Medicare levy) is for the salary in the purchase year, grown by the pay
//   rise, less that year's contribution.
// - The bank comparison saves each year's contribution after income tax at
//   that year's marginal rate (on salary less the contribution), with
//   interest taxed at the same rate, for the same time.
// One change from the sheet: "Most you can add" and the $50,000 check count
// earlier years' amounts as counted, where the sheet uses the amounts typed.
// The sheet's data validation stops an over-limit amount being typed, so the
// two only differ after one is; the page allows it and flags it, and would
// otherwise call a later year over the total when all of it is counted.
// The sheet ignores "FHSS amounts already contributed" in the results: they
// only use up the limits. Its hidden after-tax column (G) is always 0, so
// after-tax contributions are left out here too.
//
// What the sheet fixes and law sets (caps, 85%, the 30% offset, tax rates,
// Medicare levy) stays fixed. The earnings and bank rates can be changed, as
// ASIC's super calculator relief requires (Instrument 2022/603 s 7(1)(d)),
// and today's dollars are added (s 7(1)(e)), which the sheet didn't have.
(function () {
    "use strict";

    // LAW holds the 2026-27 rules from Settings & Notes.
    var LAW = {
        capCC: 32500,   // concessional (before-tax) contributions cap
        capYr: 15000,   // FHSS yearly limit on counted contributions
        capLife: 50000, // FHSS overall limit
        relCC: 0.85,    // releasable share of before-tax contributions
        offset: 0.3,    // FHSS tax offset on the taxable part released
        medicare: 0.02,
        // 2026-27 resident tax rates: [taxable income from, rate].
        tax: [[0, 0], [18201, 0.15], [45001, 0.30], [135001, 0.37], [190001, 0.45]],
    };
    var YEARS = 6, FIRST_FY = 2026;

    // The default assumptions: the sheet's (Settings & Notes C16, C17), and
    // 2.5% inflation for today's dollars. The page's inputs override them.
    var DEFAULTS = { earnRate: 0.0751, bankRate: 0.045, inflation: 0.025 };

    // marginal is VLOOKUP(income, TaxTable, 2, TRUE): the rate of the last
    // bracket starting at or below income.
    function marginal(income) {
        var r = 0;
        for (var i = 0; i < LAW.tax.length; i++) if (income >= LAW.tax[i][0]) r = LAW.tax[i][1];
        return r;
    }

    // calc runs the plan. p holds salary, sgRate, payRise, years (1 to 6),
    // already (FHSS amounts contributed before) and contrib (six yearly
    // amounts); a holds the assumptions (DEFAULTS' keys).
    function calc(p, a) {
        a = Object.assign({}, DEFAULTS, a || {});
        var rows = [], countedSoFar = 0;
        for (var i = 1; i <= YEARS; i++) {
            var after = i > p.years;
            var entered = p.contrib[i - 1] || 0;
            var salary = after ? 0 : p.salary;
            var employer = salary * p.sgRate;
            var lifeLeft = LAW.capLife - p.already - countedSoFar;
            var row = {
                year: i,
                after: after,
                salary: salary,
                employer: employer,
                most: after ? 0 : Math.max(0, Math.min(LAW.capYr, LAW.capCC - employer, lifeLeft)),
                entered: entered,
                counted: after ? 0 : Math.max(0, Math.min(entered, LAW.capCC - employer, LAW.capYr, lifeLeft)),
            };
            countedSoFar += row.counted;
            row.left = Math.max(0, LAW.capLife - p.already - countedSoFar);
            row.releasable = row.counted * LAW.relCC;
            row.invested = after ? 0 : p.years - i + 0.5;
            row.earnings = row.releasable * (Math.pow(1 + a.earnRate, row.invested) - 1);
            row.rate = salary === 0 ? 0 : marginal(Math.max(0, salary - row.counted)) + LAW.medicare;
            row.takeHome = row.counted * (1 - row.rate);
            row.bank = row.takeHome * Math.pow(1 + a.bankRate * (1 - row.rate), row.invested);

            // The sheet's "Contribution check" (column H), first failure wins.
            if (after) row.check = entered > 0 ? "after-entered" : "after";
            else if (employer + entered > LAW.capCC) { row.check = "cap"; row.over = employer + entered - LAW.capCC; }
            else if (entered > LAW.capYr) { row.check = "yearly"; row.over = entered - LAW.capYr; }
            else if (entered > lifeLeft) row.check = "total";
            else row.check = "ok";
            rows.push(row);
        }

        var sum = function (k) { return rows.reduce(function (t, r) { return t + r[k]; }, 0); };
        var counted = sum("counted"), releasable = sum("releasable"), earnings = sum("earnings");
        var buySalary = p.salary * Math.pow(1 + p.payRise, p.years - 1);
        var buyRate = marginal(Math.max(0, buySalary - rows[p.years - 1].counted)) + LAW.medicare;
        var tax = Math.max(0, (counted * LAW.relCC + earnings) * (buyRate - LAW.offset));
        var deposit = releasable + earnings - tax;
        var bank = sum("bank"), takeHome = sum("takeHome");
        return {
            rows: rows,
            counted: counted,
            releasable: releasable,
            earnings: earnings,
            buySalary: buySalary,
            buyRate: buyRate,
            tax: tax,
            deposit: deposit,
            bank: bank,
            takeHome: takeHome,
            extra: deposit - bank,
            allOK: rows.every(function (r) { return r.check === "ok" || r.check === "after"; }),
            // In today's dollars, discounted for the whole wait: the release
            // is at the end of the purchase year.
            real: function (v) { return v / Math.pow(1 + a.inflation, p.years); },
        };
    }

    // fy names year i (1 is 2026-27).
    function fy(i) {
        var y = FIRST_FY + i - 1;
        return y + "–" + String(y + 1).slice(2);
    }

    if (typeof module !== "undefined") {
        module.exports = { calc: calc, DEFAULTS: DEFAULTS, LAW: LAW, fy: fy };
        return;
    }

    var root = document.querySelector("[data-hcalc]");
    if (!root) return;

    var money = new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD", maximumFractionDigits: 0 });
    var whole = function (v) { return money.format(Math.round(v)); };
    var pctFmt = function (v) { return (Math.round(v * 1000) / 10) + "%"; };
    var out = function (name) { return root.querySelector('[data-out="' + name + '"]'); };
    var field = function (name) { return root.querySelector('[data-hc="' + name + '"]'); };
    var yearRows = Array.prototype.slice.call(root.querySelectorAll("[data-hc-year]"));
    var yearInputs = yearRows.map(function (r) { return r.querySelector("[data-hc-contrib]"); });

    function num(el) {
        var s = el.value.replace(/[^0-9.]/g, "");
        return s === "" ? NaN : parseFloat(s);
    }
    var pct = function (name) { return num(field(name)) / 100; };

    function years() {
        var on = root.querySelector('[data-hc="years"]:checked');
        return on ? Number(on.value) : YEARS;
    }

    function plan() {
        var or0 = function (v) { return isNaN(v) ? 0 : v; };
        return {
            salary: or0(num(field("salary"))),
            sgRate: pct("sg"),
            payRise: pct("payrise"),
            years: years(),
            already: or0(num(field("already"))),
            contrib: yearInputs.map(function (el) { return or0(num(el)); }),
        };
    }

    function problem(p, a) {
        if (p.salary > 1000000) return "Enter a salary of $1,000,000 or less.";
        if (!(p.sgRate >= 0 && p.sgRate <= 0.3)) return "Enter an employer super rate between 0% and 30%.";
        if (!(p.payRise >= 0 && p.payRise <= 0.2)) return "Enter a pay rise between 0% and 20%.";
        if (p.already > LAW.capLife) return "FHSS amounts already contributed can’t be more than $50,000.";
        if (p.contrib.some(function (c) { return c > 1000000; })) return "Enter yearly contributions of $1,000,000 or less.";
        if (!(a.earnRate >= 0 && a.earnRate <= 0.2)) return "Enter an earnings rate between 0% and 20%.";
        if (!(a.bankRate >= 0 && a.bankRate <= 0.2)) return "Enter a bank interest rate between 0% and 20%.";
        if (!(a.inflation >= 0 && a.inflation <= 0.1)) return "Enter inflation between 0% and 10%.";
        return "";
    }

    function checkText(r) {
        var counted = r.counted > 0 ? " Only " + whole(r.counted) + " is counted." : " None of it is counted.";
        switch (r.check) {
            case "cap": return "Over the $32,500 before-tax cap by " + whole(r.over) + ", counting employer super." + counted;
            case "yearly": return "Over the $15,000 yearly limit by " + whole(r.over) + "." + counted;
            case "total": return "Over the $50,000 total limit." + counted;
            case "after-entered": return "After your purchase year, so it isn’t counted.";
        }
        return "";
    }

    var last = null;

    function update() {
        var p = plan();
        var a = { earnRate: pct("earn"), bankRate: pct("bank"), inflation: pct("inflation") };
        var err = problem(p, a);
        var errEl = out("error");
        root.classList.toggle("mcalc-invalid", !!err);
        errEl.hidden = !err;
        errEl.textContent = err;
        if (err) return;

        var res = calc(p, a);
        out("buy-year").textContent = fy(p.years);
        res.rows.forEach(function (r, i) {
            var tr = yearRows[i];
            tr.hidden = r.after && r.check !== "after-entered";
            tr.classList.toggle("hc-row-bad", r.check !== "ok" && r.check !== "after");
            tr.querySelector("[data-hc-employer]").textContent = whole(r.employer);
            tr.querySelector("[data-hc-most]").textContent = r.after ? "–" : whole(r.most);
            var msg = tr.querySelector("[data-hc-check]");
            msg.textContent = checkText(r);
            msg.hidden = !msg.textContent;
        });
        out("total-entered").textContent = whole(res.rows.reduce(function (t, r) { return t + (r.after ? 0 : r.entered); }, 0));
        out("total-counted").textContent = whole(res.counted);
        out("left").textContent = whole(res.rows[p.years - 1].left);

        out("deposit").textContent = whole(res.deposit);
        out("bank").textContent = whole(res.bank);
        var extra = out("extra");
        extra.textContent = (res.extra < 0 ? "−" : "") + whole(Math.abs(res.extra));
        extra.classList.toggle("mcalc-green", res.extra > 0);
        out("extra-label").textContent = res.extra < 0 ? "Less using FHSS" : "Extra deposit using FHSS";
        var showReal = p.years >= 2;
        ["deposit", "bank", "extra"].forEach(function (k) {
            var el = out(k + "-real");
            el.hidden = !showReal;
            if (showReal) el.textContent = "About " + whole(Math.abs(res.real(res[k]))) + " in today’s dollars";
        });

        out("counted").textContent = whole(res.counted);
        out("releasable").textContent = whole(res.releasable);
        out("earnings").textContent = whole(res.earnings);
        out("tax").textContent = (res.tax > 0 ? "−" : "") + whole(res.tax);
        out("sum").textContent = whole(res.deposit);
        out("buy-salary").textContent = whole(res.buySalary);
        out("buy-rate").textContent = pctFmt(res.buyRate);

        var banner = out("banner");
        banner.classList.toggle("hc-banner-warn", !res.allOK);
        banner.textContent = res.allOK
            ? "Your plan is within the $32,500 cap, the $15,000 yearly limit and the $50,000 total limit."
            : "Check the years marked above. Amounts over the limits aren’t counted in your results.";
        last = res;
        drawBars();
    }

    // ---- At a glance: two bars, each what goes in plus what it grows by ----

    function drawBars() {
        var res = last;
        var top = Math.max(1, res.deposit, res.bank);
        // If tax on release is more than the earnings, the whole bar is the
        // deposit, shown as what went in.
        var fIn = Math.max(0, Math.min(res.releasable, res.deposit)), fGrow = Math.max(0, res.deposit - fIn);
        var bIn = res.takeHome, bGrow = Math.max(0, res.bank - bIn);
        var set = function (name, v) { out(name).style.width = (v / top * 100) + "%"; };
        set("bar-fhss-in", fIn);
        set("bar-fhss-grow", fGrow);
        set("bar-bank-in", bIn);
        set("bar-bank-grow", bGrow);
        out("bar-fhss-total").textContent = out("bar-fhss-total-2").textContent = whole(res.deposit);
        out("bar-bank-total").textContent = out("bar-bank-total-2").textContent = whole(res.bank);
        out("glance-fhss-in").textContent = whole(res.releasable);
        out("glance-bank-in").textContent = whole(res.takeHome);
        out("glance-fhss-grow").textContent = (res.earnings - res.tax < 0 ? "−" : "") + whole(Math.abs(res.earnings - res.tax));
        out("glance-bank-grow").textContent = whole(res.bank - res.takeHome);
    }

    // ---- Inputs ----

    // fillMost puts the most allowed into each year in turn, so each year
    // sees the ones before it.
    function fillMost() {
        var p = plan();
        for (var i = 0; i < YEARS; i++) {
            p.contrib[i] = 0;
            if (i < p.years) p.contrib[i] = calc(p, {}).rows[i].most;
            yearInputs[i].value = Math.round(p.contrib[i]).toLocaleString("en-AU");
        }
        update();
    }

    root.addEventListener("input", update);
    root.addEventListener("change", update);
    root.addEventListener("click", function (ev) {
        if (ev.target.closest("[data-hc-fill]")) fillMost();
    });
    root.addEventListener("focusout", function (ev) {
        if (!ev.target.hasAttribute || !ev.target.hasAttribute("data-money")) return;
        var v = num(ev.target);
        if (!isNaN(v)) ev.target.value = Math.round(v).toLocaleString("en-AU");
    });
    update();
})();
