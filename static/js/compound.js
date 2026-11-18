// Compound interest calculator (/tools/compound). Our own page, built to the
// assumptions Moneysmart states for its compound interest calculator:
// - The first deposit is made today.
// - Regular deposits are made at the end of each day, week, fortnight, month
//   or year, as chosen.
// - Interest is credited monthly or yearly. Between credits it accrues on the
//   balance day by day (simple interest), as banks do, and only starts
//   earning interest itself once it's credited.
// - Results are future dollars, not adjusted for inflation, before tax.
// - A later start makes the same deposits, beginning some years later, and is
//   valued at the same end date.
//
// The interest is split in two: interest earned on the money deposited, and
// interest earned on interest already credited (what compounding adds).
//
// Everything happens here in the browser: nothing typed is sent anywhere or
// saved, not even to localStorage.
(function () {
    "use strict";

    var EPS = 1e-9;

    // simulate runs one plan for a number of years and returns a point at the
    // end of every month: { t (years), balance, deposits, onDeposits, onInterest }.
    // perYear is the deposit frequency, credits the crediting frequency
    // (12 or 1) and rate the yearly rate as a fraction.
    function simulate(initial, deposit, perYear, rate, credits, years) {
        var principal = initial, credited = 0;   // balance = principal + credited
        var accDep = 0, accInt = 0;              // accrued, not yet credited
        var onDeposits = 0, onInterest = 0;      // credited so far, by source
        var now = 0;
        var points = [{ t: 0, balance: initial, deposits: initial, onDeposits: 0, onInterest: 0 }];
        var nextDeposit = 1, nextCredit = 1, nextPoint = 1;
        var totalDeposits = Math.round(perYear * years);
        var totalCredits = credits * years, totalPoints = 12 * years;

        while (nextPoint <= totalPoints) {
            var tDep = nextDeposit <= totalDeposits ? nextDeposit / perYear : Infinity;
            var tCred = nextCredit / credits;
            var tPoint = nextPoint / 12;
            var t = Math.min(tDep, tCred, tPoint);

            var dt = t - now;
            if (dt > 0) {
                accDep += principal * rate * dt;
                accInt += credited * rate * dt;
                now = t;
            }
            // At the same moment: credit the interest, then take the deposit
            // (made at the end of its period, so it earned nothing yet).
            if (Math.abs(t - tCred) < EPS && nextCredit <= totalCredits) {
                onDeposits += accDep;
                onInterest += accInt;
                credited += accDep + accInt;
                accDep = accInt = 0;
                nextCredit++;
            }
            if (Math.abs(t - tDep) < EPS) {
                principal += deposit;
                nextDeposit++;
            }
            if (Math.abs(t - tPoint) < EPS) {
                points.push({
                    t: t, balance: principal + credited, deposits: principal,
                    onDeposits: onDeposits, onInterest: onInterest
                });
                nextPoint++;
            }
        }
        return points;
    }

    // calc runs the plan and, with a delay of 1 year or more, the same plan
    // started that many years later, padded with empty months at the front so
    // both end on the same date.
    function calc(p) {
        var now = simulate(p.initial, p.deposit, p.perYear, p.rate, p.credits, p.years);
        var later = null;
        if (p.delay > 0 && p.delay < p.years) {
            var run = simulate(p.initial, p.deposit, p.perYear, p.rate, p.credits, p.years - p.delay);
            later = [];
            for (var m = 0; m < 12 * p.delay; m++) {
                later.push({ t: m / 12, balance: 0, deposits: 0, onDeposits: 0, onInterest: 0 });
            }
            run.forEach(function (pt) {
                later.push({
                    t: pt.t + p.delay, balance: pt.balance, deposits: pt.deposits,
                    onDeposits: pt.onDeposits, onInterest: pt.onInterest
                });
            });
        }
        return { now: now, later: later, end: now[now.length - 1], laterEnd: later ? later[later.length - 1] : null };
    }

    if (typeof module !== "undefined") {
        module.exports = { simulate: simulate, calc: calc };
        return;
    }

    var root = document.querySelector("[data-ccalc]");
    if (!root) return;

    var money = new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD", maximumFractionDigits: 0 });
    var out = function (name) { return root.querySelector('[data-out="' + name + '"]'); };
    var field = function (name) { return root.querySelector('[data-cc="' + name + '"]'); };
    var FREQ = { 365: "day", 52: "week", 26: "fortnight", 12: "month", 1: "year" };
    var last = null, lastP = null;

    function num(el) {
        var s = el.value.replace(/[^0-9.]/g, "");
        return s === "" ? NaN : parseFloat(s);
    }

    function years(n) {
        return n + (n === 1 ? " year" : " years");
    }

    function read() {
        var credits = root.querySelector('[data-cc="credits"]:checked');
        return {
            initial: num(field("initial")),
            deposit: num(field("deposit")),
            perYear: Number(field("freq").value),
            rate: num(field("rate")) / 100,
            credits: credits ? Number(credits.value) : 12,
            years: num(field("years")),
            delay: num(field("delay"))
        };
    }

    function check(p) {
        if (isNaN(p.initial)) p.initial = 0;
        if (isNaN(p.deposit)) p.deposit = 0;
        if (isNaN(p.delay)) p.delay = 0;
        if (p.initial > 100e6 || p.deposit > 10e6) return "Enter amounts of $10 million or less.";
        if (p.initial <= 0 && p.deposit <= 0) return "Enter a first deposit, a regular deposit or both.";
        if (!(p.rate >= 0 && p.rate <= 0.3)) return "Enter an interest rate between 0% and 30%.";
        if (!(p.years >= 1 && p.years <= 60) || p.years % 1) return "Enter a whole number of years from 1 to 60.";
        if (p.delay % 1 || p.delay < 0 || (p.delay > 0 && p.delay >= p.years)) {
            return "Enter a later start of fewer whole years than you're saving for (0 to leave it out).";
        }
        return "";
    }

    function update() {
        var p = read();
        var err = check(p);
        var errEl = out("error");
        root.classList.toggle("mcalc-invalid", !!err);
        errEl.hidden = !err;
        errEl.textContent = err;
        if (err) return;

        var res = calc(p), e = res.end;
        var interest = e.onDeposits + e.onInterest;
        out("balance").textContent = money.format(e.balance);
        out("balance-note").textContent = "After " + years(p.years) + ", in future dollars";
        out("deposits").textContent = money.format(e.deposits);
        out("on-deposits").textContent = money.format(e.onDeposits);
        out("on-interest").textContent = money.format(e.onInterest);

        var later = out("later");
        later.hidden = !res.laterEnd;
        if (res.laterEnd) {
            var gap = e.balance - res.laterEnd.balance;
            out("later-title").textContent = "If you started " + years(p.delay) + " later";
            out("later-value").textContent = money.format(res.laterEnd.balance);
            out("later-note").textContent = money.format(gap) + " less, though you'd deposit " +
                money.format(e.deposits - res.laterEnd.deposits) + " less.";
        }

        var banner = "";
        if (e.onInterest >= 1 && interest > 0) {
            banner = "Interest on interest is " + Math.round(e.onInterest / interest * 100) +
                "% of the interest you'd earn. That's compounding at work.";
        } else if (interest <= 0) {
            banner = "With no interest, your balance is just what you put in.";
        } else {
            banner = "Over a short time, interest on interest is small. Try more years to see it grow.";
        }
        out("banner").textContent = banner;

        last = res;
        lastP = p;
        drawChart();
    }

    // ---- Chart: stacked areas (deposits, interest on them, interest on
    // interest) and a dashed line for the later start, redrawn on resize ----

    var chartEl = out("chart");
    var NS = "http://www.w3.org/2000/svg";

    function el(name, attrs, parent) {
        var e = document.createElementNS(NS, name);
        for (var k in attrs) e.setAttribute(k, attrs[k]);
        if (parent) parent.appendChild(e);
        return e;
    }

    function niceStep(raw) {
        var pw = Math.pow(10, Math.floor(Math.log10(raw)));
        var f = raw / pw;
        return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10) * pw;
    }

    // yearStep is a whole number of years between axis labels, for at
    // most about n labels.
    function yearStep(span, n) {
        var steps = [1, 2, 5, 10, 20];
        for (var i = 0; i < steps.length; i++) if (span / steps[i] <= n) return steps[i];
        return 20;
    }

    function shortMoney(v) {
        if (v >= 1e6) return "$" + +(v / 1e6).toFixed(1) + "m";
        if (v >= 1e3) return "$" + Math.round(v / 1e3) + "k";
        return "$" + Math.round(v);
    }

    function when(t) {
        var m = Math.round(t * 12), y = Math.floor(m / 12), mo = m % 12;
        if (m === 0) return "Today";
        if (mo === 0) return "After " + years(y);
        return "After " + (y ? years(y) + " " : "") + mo + (mo === 1 ? " month" : " months");
    }

    function drawChart() {
        if (!last) return;
        var pts = last.now, Y = lastP.years;
        var W = Math.max(280, chartEl.clientWidth), H = W < 500 ? 240 : 320;
        var pad = { l: 52, r: 14, t: 14, b: 28 };
        var iw = W - pad.l - pad.r, ih = H - pad.t - pad.b;
        var top = Math.max(1, last.end.balance);
        var step = niceStep(top / 4), yMax = Math.ceil(top / step) * step;
        var x = function (t) { return pad.l + t / Y * iw; };
        var y = function (v) { return pad.t + ih - v / yMax * ih; };

        chartEl.textContent = "";
        var svg = el("svg", { width: W, height: H, viewBox: "0 0 " + W + " " + H }, chartEl);
        for (var v = 0; v <= yMax + 0.5; v += step) {
            el("line", { x1: pad.l, x2: W - pad.r, y1: y(v), y2: y(v), "class": "mc-grid" }, svg);
            el("text", { x: pad.l - 8, y: y(v) + 4, "class": "mc-axis", "text-anchor": "end" }, svg).textContent = shortMoney(v);
        }
        var xStep = yearStep(Y, W < 500 ? 4 : 8);
        for (var a = 0; a <= Y + EPS; a += xStep) {
            el("text", { x: x(a), y: H - 8, "class": "mc-axis", "text-anchor": a === 0 ? "start" : "middle" }, svg).textContent = a === 0 ? "Now" : a + (a === Y || W >= 500 ? " yr" : "");
        }

        // Each band is the area between two running totals.
        var band = function (lo, hi, cls) {
            var upper = pts.map(function (p) { return x(p.t).toFixed(1) + "," + y(hi(p)).toFixed(1); });
            var lower = pts.slice().reverse().map(function (p) { return x(p.t).toFixed(1) + "," + y(lo(p)).toFixed(1); });
            el("polygon", { points: upper.concat(lower).join(" "), "class": cls }, svg);
        };
        band(function () { return 0; }, function (p) { return p.deposits; }, "cc-band-dep");
        band(function (p) { return p.deposits; }, function (p) { return p.deposits + p.onDeposits; }, "cc-band-int");
        band(function (p) { return p.deposits + p.onDeposits; }, function (p) { return p.balance; }, "cc-band-ii");
        if (last.later) {
            var lp = last.later.slice(12 * lastP.delay).map(function (p) { return x(p.t).toFixed(1) + "," + y(p.balance).toFixed(1); }).join(" ");
            el("polyline", { points: lp, "class": "mc-line cc-line-later" }, svg);
        }

        var guide = el("line", { y1: pad.t, y2: pad.t + ih, "class": "mc-guide", visibility: "hidden" }, svg);
        var tip = document.createElement("div");
        tip.className = "mc-tip";
        tip.hidden = true;
        chartEl.appendChild(tip);

        var row = function (cls, text) {
            var line = document.createElement("div");
            var sw = document.createElement("span");
            sw.className = "mcalc-swatch " + cls;
            line.appendChild(sw);
            line.appendChild(document.createTextNode(text));
            tip.appendChild(line);
        };
        var show = function (ev) {
            var box = svg.getBoundingClientRect();
            var i = Math.round((ev.clientX - box.left - pad.l) / iw * Y * 12);
            i = Math.max(0, Math.min(pts.length - 1, i));
            var p = pts[i], gx = x(p.t);
            guide.setAttribute("x1", gx); guide.setAttribute("x2", gx);
            guide.setAttribute("visibility", "visible");
            tip.textContent = "";
            var head = document.createElement("strong");
            head.textContent = when(p.t) + ": " + money.format(p.balance);
            tip.appendChild(head);
            row("cc-sw-dep", "Deposits: " + money.format(p.deposits));
            row("cc-sw-int", "Interest on deposits: " + money.format(p.onDeposits));
            row("cc-sw-ii", "Interest on interest: " + money.format(p.onInterest));
            if (last.later) row("cc-sw-later", "Starting later: " + money.format(last.later[i].balance));
            tip.hidden = false;
            var left = gx + 12;
            if (left + tip.offsetWidth > W) left = gx - 12 - tip.offsetWidth;
            tip.style.left = Math.max(0, left) + "px";
        };
        var hide = function () {
            guide.setAttribute("visibility", "hidden");
            tip.hidden = true;
        };
        svg.addEventListener("pointermove", show);
        svg.addEventListener("pointerdown", show);
        svg.addEventListener("pointerleave", hide);
    }

    // ---- Inputs ----

    root.addEventListener("input", update);
    root.addEventListener("change", update);
    root.addEventListener("focusout", function (ev) {
        if (!ev.target.hasAttribute || !ev.target.hasAttribute("data-money")) return;
        var v = num(ev.target);
        if (!isNaN(v)) ev.target.value = Math.round(v).toLocaleString("en-AU");
    });
    var freqWord = out("freq-word");
    field("freq").addEventListener("change", function () {
        freqWord.textContent = FREQ[field("freq").value] || "period";
    });

    var width = 0;
    if (window.ResizeObserver) {
        new ResizeObserver(function () {
            if (chartEl.clientWidth !== width) { width = chartEl.clientWidth; drawChart(); }
        }).observe(chartEl);
    }
    update();
})();
