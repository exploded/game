// Mortgage Freedom Calculator (/tools/mortgage), rebuilt from Rob's
// spreadsheet "CPW Mortgage Freedom Calculator Aug 26". Everything happens
// here in the browser: nothing typed is sent anywhere or saved, not even to
// localStorage.
//
// The sums follow the spreadsheet's hidden "Calculation Schedule" sheet:
// - Regular repayment is Excel's PMT over the whole term at rate/periods.
// - Each period: interest = balance x rate / periods a year, then the
//   repayment (regular, or regular + extra) up to what's owed.
// - Interest saved = original schedule's interest - interest with extras.
// - Time saved = (term - payoff periods / periods a year) x 12 months.
// - Mortgage-free date = today + payoff periods (months for monthly,
//   7 or 14 days for weekly or fortnightly).
// Two fixes over the sheet: a balance under half a cent counts as paid (the
// sheet's MATCH(0, ...) gave #N/A with no extra, when floating point left
// 0.00000002 owing), and time saved is rounded to whole months before it's
// split, so it can't read "5 years 12 months".
(function () {
    "use strict";

    var MAX_PERIODS = 2600; // the sheet's 2,600 rows: 50 years of weekly payments
    var PAID = 0.005;       // under half a cent owing counts as paid off

    function pmt(rate, n, principal) {
        if (rate === 0) return principal / n;
        return principal * rate / (1 - Math.pow(1 + rate, -n));
    }

    // calc works out both schedules. loan and extra are dollars, rate is a
    // fraction (0.06), term is whole years, ppy is repayments a year.
    function calc(o) {
        var r = o.rate / o.ppy;
        var n = Math.round(o.term * o.ppy);
        var regular = pmt(r, n, o.loan);

        var run = function (payment) {
            var bal = o.loan, interest = 0, balances = [bal], paidAt = null;
            for (var k = 1; k <= MAX_PERIODS; k++) {
                var due = bal * r;
                var pay = Math.min(bal + due, payment);
                bal = Math.max(0, bal + due - pay);
                if (bal < PAID) bal = 0;
                interest += due;
                balances.push(bal);
                if (bal === 0) { paidAt = k; break; }
            }
            return { interest: interest, balances: balances, paidAt: paidAt };
        };

        var base = run(regular);
        var extra = run(regular + o.extra);
        var res = {
            regular: regular,
            total: regular + o.extra,
            baseInterest: base.interest,
            interest: extra.interest,
            saved: Math.max(0, base.interest - extra.interest),
            periods: extra.paidAt,
            base: base.balances,
            withExtra: extra.balances,
            monthsSaved: 0
        };
        if (extra.paidAt) {
            res.monthsSaved = Math.max(0, Math.round((o.term - extra.paidAt / o.ppy) * 12));
        }
        return res;
    }

    // payoffDate is the sheet's I16: EDATE for monthly, days otherwise.
    function payoffDate(periods, ppy, today) {
        var d = new Date(today.getFullYear(), today.getMonth(), today.getDate());
        if (ppy === 12) {
            var day = d.getDate();
            d.setDate(1);
            d.setMonth(d.getMonth() + periods);
            var last = new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate();
            d.setDate(Math.min(day, last));
            return d;
        }
        d.setDate(d.getDate() + periods * (ppy === 52 ? 7 : 14));
        return d;
    }

    function timeText(months) {
        if (months <= 0) return "–";
        var y = Math.floor(months / 12), m = months % 12;
        var parts = [];
        if (y) parts.push(y + (y === 1 ? " year" : " years"));
        if (m) parts.push(m + (m === 1 ? " month" : " months"));
        return parts.join(" ");
    }

    if (typeof module !== "undefined") {
        module.exports = { calc: calc, payoffDate: payoffDate, timeText: timeText };
        return;
    }

    var root = document.querySelector("[data-mcalc]");
    if (!root) return;

    var money = new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD", maximumFractionDigits: 0 });
    var dateFmt = new Intl.DateTimeFormat("en-AU", { day: "numeric", month: "long", year: "numeric" });
    var $ = function (sel) { return root.querySelector(sel); };
    var out = function (name) { return root.querySelector('[data-out="' + name + '"]'); };
    var slider = $("[data-mc-slider]");
    var extraInput = $('[data-mc="extra"]');
    var last = null;

    function num(el) {
        var s = el.value.replace(/[^0-9.]/g, "");
        return s === "" ? NaN : parseFloat(s);
    }

    function read() {
        var o = {
            loan: num($('[data-mc="loan"]')),
            rate: num($('[data-mc="rate"]')) / 100,
            term: num($('[data-mc="term"]')),
            ppy: parseInt($('[data-mc="freq"]:checked').value, 10),
            extra: num(extraInput)
        };
        if (isNaN(o.extra)) o.extra = 0;
        var err = "";
        if (!(o.loan >= 1000 && o.loan <= 100000000)) err = "Enter a loan amount between $1,000 and $100,000,000.";
        else if (!(o.rate >= 0 && o.rate <= 0.3)) err = "Enter an interest rate between 0% and 30%.";
        else if (!(o.term >= 1 && o.term <= 50 && o.term === Math.floor(o.term))) err = "Enter a loan term of 1 to 50 whole years.";
        else if (o.extra > 1000000) err = "Enter an extra repayment under $1,000,000.";
        return { o: o, err: err };
    }

    function update() {
        var input = read();
        var errEl = out("error");
        root.classList.toggle("mcalc-invalid", !!input.err);
        errEl.hidden = !input.err;
        errEl.textContent = input.err;
        if (input.err) return;

        var o = input.o, res = calc(o);
        out("regular").textContent = money.format(res.regular);
        out("total").textContent = money.format(res.total);
        out("interest").textContent = money.format(res.interest);
        out("interest-base").textContent = o.extra > 0
            ? "Without the extra: " + money.format(res.baseInterest)
            : "";
        out("saved").textContent = money.format(res.saved);
        out("time").textContent = timeText(res.monthsSaved);
        var when = res.periods ? payoffDate(res.periods, o.ppy, new Date()) : null;
        out("date").textContent = when ? dateFmt.format(when) : "–";
        out("banner").textContent = when
            ? (o.extra > 0
                ? "On your current extra-payment plan, the mortgage could be fully repaid by " + dateFmt.format(when) + "."
                : "On your current repayments, the mortgage could be fully repaid by " + dateFmt.format(when) + ". Add an extra amount to see how much sooner.")
            : " ";
        last = { o: o, res: res };
        drawChart();
    }

    // ---- Chart: inline SVG, redrawn when its box changes width ----

    var chartEl = out("chart");
    var NS = "http://www.w3.org/2000/svg";

    function el(name, attrs, parent) {
        var e = document.createElementNS(NS, name);
        for (var k in attrs) e.setAttribute(k, attrs[k]);
        if (parent) parent.appendChild(e);
        return e;
    }

    function niceStep(raw) {
        var p = Math.pow(10, Math.floor(Math.log10(raw)));
        var f = raw / p;
        return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10) * p;
    }

    function shortMoney(v) {
        if (v >= 1e6) return "$" + (v / 1e6).toFixed(v % 1e6 ? 1 : 0) + "m";
        if (v >= 1e3) return "$" + Math.round(v / 1e3) + "k";
        return "$" + Math.round(v);
    }

    // balanceAt reads a schedule at a time in years (0 once it's paid off).
    function balanceAt(balances, years, ppy) {
        var k = Math.round(years * ppy);
        return k < balances.length ? balances[k] : 0;
    }

    function drawChart() {
        if (!last) return;
        var o = last.o, res = last.res;
        var W = Math.max(280, chartEl.clientWidth), H = W < 500 ? 220 : 300;
        var pad = { l: 52, r: 14, t: 14, b: 28 };
        var iw = W - pad.l - pad.r, ih = H - pad.t - pad.b;
        var step = niceStep(o.loan / 4), yMax = Math.ceil(o.loan / step) * step;
        var x = function (yr) { return pad.l + yr / o.term * iw; };
        var y = function (v) { return pad.t + ih - v / yMax * ih; };

        chartEl.textContent = "";
        var svg = el("svg", { width: W, height: H, viewBox: "0 0 " + W + " " + H }, chartEl);

        for (var v = 0; v <= yMax + 0.5; v += step) {
            el("line", { x1: pad.l, x2: W - pad.r, y1: y(v), y2: y(v), "class": "mc-grid" }, svg);
            el("text", { x: pad.l - 8, y: y(v) + 4, "class": "mc-axis", "text-anchor": "end" }, svg).textContent = shortMoney(v);
        }
        var xStep = o.term <= 10 ? 1 : o.term <= 40 ? 5 : 10;
        if (W < 400 && xStep < 10 && o.term > 20) xStep = 10;
        for (var t = 0; t <= o.term; t += xStep) {
            el("text", { x: x(t), y: H - 8, "class": "mc-axis", "text-anchor": "middle" }, svg).textContent = t === 0 ? "Now" : "Yr " + t;
        }

        // One point a month, plus the exact payoff point for the extra line.
        var basePts = [], extraPts = [];
        for (var m = 0; m <= o.term * 12; m++) {
            var yr = m / 12;
            basePts.push([x(yr), y(balanceAt(res.base, yr, o.ppy))]);
            extraPts.push([x(yr), y(balanceAt(res.withExtra, yr, o.ppy))]);
        }
        var payoffYr = res.periods ? res.periods / o.ppy : null;
        var pts = function (a) { return a.map(function (p) { return p[0].toFixed(1) + "," + p[1].toFixed(1); }).join(" "); };

        if (o.extra > 0) {
            el("polygon", { points: pts(extraPts) + " " + pts(basePts.slice().reverse()), "class": "mc-gap" }, svg);
        }
        el("polyline", { points: pts(basePts), "class": "mc-line mc-line-base" }, svg);
        el("polyline", { points: pts(extraPts), "class": "mc-line mc-line-extra" }, svg);

        if (payoffYr !== null && o.extra > 0 && payoffYr < o.term) {
            var px = x(payoffYr), py = y(0);
            el("circle", { cx: px, cy: py, r: 5, "class": "mc-dot" }, svg);
            // To the right of the dot, where the teal line is flat at $0.
            var right = px < W - pad.r - 100;
            var label = el("text", { x: right ? px + 8 : px - 8, y: py - 10, "class": "mc-label", "text-anchor": right ? "start" : "end" }, svg);
            label.textContent = "Mortgage-free";
        }

        // Hover or drag: a guide line and the two balances for that year.
        var guide = el("line", { y1: pad.t, y2: pad.t + ih, "class": "mc-guide", visibility: "hidden" }, svg);
        var dotB = el("circle", { r: 4, "class": "mc-dot-base", visibility: "hidden" }, svg);
        var dotE = el("circle", { r: 4, "class": "mc-dot-extra", visibility: "hidden" }, svg);
        var tip = document.createElement("div");
        tip.className = "mc-tip";
        tip.hidden = true;
        chartEl.appendChild(tip);

        var show = function (ev) {
            var box = svg.getBoundingClientRect();
            var yr = Math.round((ev.clientX - box.left - pad.l) / iw * o.term);
            yr = Math.max(0, Math.min(o.term, yr));
            var b = balanceAt(res.base, yr, o.ppy), e = balanceAt(res.withExtra, yr, o.ppy);
            var gx = x(yr);
            guide.setAttribute("x1", gx); guide.setAttribute("x2", gx);
            dotB.setAttribute("cx", gx); dotB.setAttribute("cy", y(b));
            dotE.setAttribute("cx", gx); dotE.setAttribute("cy", y(e));
            [guide, dotB, dotE].forEach(function (n) { n.setAttribute("visibility", "visible"); });
            tip.innerHTML = "";
            var head = document.createElement("strong");
            head.textContent = yr === 0 ? "Today" : "After " + yr + (yr === 1 ? " year" : " years");
            tip.appendChild(head);
            [["mcalc-swatch-base", "Original", b], ["mcalc-swatch-extra", "With extra", e]].forEach(function (row) {
                var line = document.createElement("div");
                var sw = document.createElement("span");
                sw.className = "mcalc-swatch " + row[0];
                line.appendChild(sw);
                line.appendChild(document.createTextNode(row[1] + " " + money.format(row[2])));
                tip.appendChild(line);
            });
            tip.hidden = false;
            var left = gx + 12;
            if (left + tip.offsetWidth > W) left = gx - 12 - tip.offsetWidth;
            tip.style.left = left + "px";
        };
        var hide = function () {
            [guide, dotB, dotE].forEach(function (n) { n.setAttribute("visibility", "hidden"); });
            tip.hidden = true;
        };
        svg.addEventListener("pointermove", show);
        svg.addEventListener("pointerdown", show);
        svg.addEventListener("pointerleave", hide);
    }

    // ---- Inputs ----

    root.addEventListener("input", function (ev) {
        if (ev.target === slider) {
            extraInput.value = Number(slider.value).toLocaleString("en-AU");
        } else if (ev.target === extraInput) {
            var v = num(extraInput);
            slider.value = isNaN(v) ? 0 : Math.min(v, Number(slider.max));
        }
        update();
    });
    root.addEventListener("change", update);
    // Money boxes get their commas back when the player leaves them.
    root.addEventListener("focusout", function (ev) {
        if (!ev.target.hasAttribute || !ev.target.hasAttribute("data-money")) return;
        var v = num(ev.target);
        if (!isNaN(v)) ev.target.value = Math.round(v).toLocaleString("en-AU");
    });

    var width = 0;
    if (window.ResizeObserver) {
        new ResizeObserver(function () {
            if (chartEl.clientWidth !== width) { width = chartEl.clientWidth; drawChart(); }
        }).observe(chartEl);
    }
    update();
})();
