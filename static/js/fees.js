// Managed fund fee calculator (/tools/fees). Our own page, built to the
// assumptions Moneysmart states for its managed funds fee calculator:
// - The starting amount is already net of any up-front establishment fee.
// - Returns are reinvested, before tax, and not adjusted for inflation.
// - Buy/sell spreads, performance, switching and exit fees and brokerage
//   aren't included.
// Moneysmart doesn't say how it times the fees, so here everything moves a
// little each day (365 days a year): the balance grows by the daily share of
// the yearly return before fees, then the ongoing fee (a percentage of the
// balance) and the yearly dollar fee are taken out pro rata. Regular
// contributions arrive at the end of each week, fortnight, month or year,
// less the contribution fee.
//
// "Effect of fees" is how much more you'd have with no fees at all: the fees
// themselves plus the returns they would have earned.
//
// Everything happens here in the browser: nothing typed is sent anywhere or
// saved, not even to localStorage.
(function () {
    "use strict";

    var DAYS = 365;

    // simulate returns a point at the end of every month:
    // { t (years), balance, paid (all fees so far), contributed }.
    // fund is { ongoing (fraction a year), entry (fraction of each
    // contribution), fixed (dollars a year) }.
    function simulate(p, fund) {
        var balance = p.initial, paid = 0, contributed = p.initial;
        var grow = Math.pow(1 + p.ret, 1 / DAYS);
        var nextC = 1, nextDay = Math.round(DAYS / p.perYear);
        var totalC = p.perYear * p.years, days = DAYS * p.years;
        var nextPoint = 1;
        var points = [{ t: 0, balance: balance, paid: 0, contributed: contributed }];

        for (var d = 1; d <= days; d++) {
            balance *= grow;
            var fee = Math.min(balance, balance * fund.ongoing / DAYS + fund.fixed / DAYS);
            balance -= fee;
            paid += fee;
            if (d === nextDay && nextC <= totalC) {
                var entry = p.contribution * fund.entry;
                balance += p.contribution - entry;
                paid += entry;
                contributed += p.contribution;
                nextC++;
                nextDay = Math.round(nextC * DAYS / p.perYear);
            }
            if (d === Math.round(nextPoint * DAYS / 12)) {
                points.push({ t: nextPoint / 12, balance: balance, paid: paid, contributed: contributed });
                nextPoint++;
            }
        }
        return points;
    }

    var NO_FEES = { ongoing: 0, entry: 0, fixed: 0 };

    // calc runs the plan with no fees and through each fund.
    function calc(p, funds) {
        var none = simulate(p, NO_FEES), noneEnd = none[none.length - 1];
        return {
            none: none,
            noneEnd: noneEnd,
            funds: funds.map(function (f) {
                var pts = simulate(p, f), end = pts[pts.length - 1];
                return { points: pts, end: end, effect: noneEnd.balance - end.balance };
            })
        };
    }

    if (typeof module !== "undefined") {
        module.exports = { simulate: simulate, calc: calc };
        return;
    }

    var root = document.querySelector("[data-fcalc]");
    if (!root) return;

    var money = new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD", maximumFractionDigits: 0 });
    var out = function (name) { return root.querySelector('[data-out="' + name + '"]'); };
    var field = function (name) { return root.querySelector('[data-fc="' + name + '"]'); };
    var last = null, lastP = null;

    function num(el) {
        var s = el.value.replace(/[^0-9.]/g, "");
        return s === "" ? NaN : parseFloat(s);
    }

    function orZero(v) {
        return isNaN(v) ? 0 : v;
    }

    function years(n) {
        return n + (n === 1 ? " year" : " years");
    }

    function readFund(k) {
        return {
            ongoing: orZero(num(field(k + "-ongoing"))) / 100,
            entry: orZero(num(field(k + "-entry"))) / 100,
            fixed: orZero(num(field(k + "-fixed")))
        };
    }

    function update() {
        var p = {
            initial: orZero(num(field("initial"))),
            contribution: orZero(num(field("contribution"))),
            perYear: Number(field("freq").value),
            years: num(field("years")),
            ret: num(field("return")) / 100
        };
        var funds = [readFund("a"), readFund("b")];

        var err = "";
        if (p.initial > 100e6 || p.contribution > 10e6) err = "Enter amounts of $10 million or less.";
        else if (p.initial <= 0 && p.contribution <= 0) err = "Enter a starting amount, a regular contribution or both.";
        else if (!(p.years >= 1 && p.years <= 60) || p.years % 1) err = "Enter a whole number of years from 1 to 60.";
        else if (!(p.ret >= 0 && p.ret <= 0.2)) err = "Enter a return between 0% and 20%.";
        else if (funds.some(function (f) { return f.ongoing > 0.1 || f.entry > 0.1; })) err = "Enter fees of 10% or less.";
        else if (funds.some(function (f) { return f.fixed > 100000; })) err = "Enter a yearly dollar fee of $100,000 or less.";
        var errEl = out("error");
        root.classList.toggle("mcalc-invalid", !!err);
        errEl.hidden = !err;
        errEl.textContent = err;
        if (err) return;

        var res = calc(p, funds);
        out("none").textContent = money.format(res.noneEnd.balance);
        out("none-note").textContent = "Put in " + money.format(res.noneEnd.contributed) + " over " + years(p.years);
        ["a", "b"].forEach(function (k, i) {
            var f = res.funds[i];
            out(k + "-balance").textContent = money.format(f.end.balance);
            out(k + "-paid").textContent = money.format(f.end.paid);
            out(k + "-effect").textContent = money.format(f.effect);
        });

        var a = res.funds[0], b = res.funds[1];
        var gap = Math.abs(a.end.balance - b.end.balance);
        var banner;
        if (gap < 1) {
            banner = "Both funds end with the same balance.";
        } else {
            var cheaper = a.end.balance > b.end.balance ? "Fund A" : "Fund B";
            banner = "After " + years(p.years) + ", " + cheaper + " leaves you " + money.format(gap) + " better off.";
        }
        out("banner").textContent = banner;

        last = res;
        lastP = p;
        drawChart();
    }

    // ---- Chart: the balance with no fees and in each fund ----

    var chartEl = out("chart");
    var NS = "http://www.w3.org/2000/svg";
    var SERIES = [
        { cls: "fc-line-none", sw: "fc-sw-none", name: "No fees" },
        { cls: "fc-line-a", sw: "fc-sw-a", name: "Fund A" },
        { cls: "fc-line-b", sw: "fc-sw-b", name: "Fund B" }
    ];

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
        var lines = [last.none, last.funds[0].points, last.funds[1].points];
        var Y = lastP.years;
        var W = Math.max(280, chartEl.clientWidth), H = W < 500 ? 240 : 320;
        var pad = { l: 52, r: 14, t: 14, b: 28 };
        var iw = W - pad.l - pad.r, ih = H - pad.t - pad.b;
        var top = Math.max(1, last.noneEnd.balance, last.funds[0].end.balance, last.funds[1].end.balance);
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
        for (var a = 0; a <= Y + 1e-9; a += xStep) {
            el("text", { x: x(a), y: H - 8, "class": "mc-axis", "text-anchor": a === 0 ? "start" : "middle" }, svg).textContent = a === 0 ? "Now" : a + (a === Y || W >= 500 ? " yr" : "");
        }
        // Shade what fees take from the dearer fund.
        var dear = last.funds[0].end.balance < last.funds[1].end.balance ? lines[1] : lines[2];
        var upper = lines[0].map(function (p) { return x(p.t).toFixed(1) + "," + y(p.balance).toFixed(1); });
        var lower = dear.slice().reverse().map(function (p) { return x(p.t).toFixed(1) + "," + y(p.balance).toFixed(1); });
        el("polygon", { points: upper.concat(lower).join(" "), "class": "fc-gap" }, svg);
        lines.forEach(function (pts, i) {
            var s = pts.map(function (p) { return x(p.t).toFixed(1) + "," + y(p.balance).toFixed(1); }).join(" ");
            el("polyline", { points: s, "class": "mc-line " + SERIES[i].cls }, svg);
        });

        var guide = el("line", { y1: pad.t, y2: pad.t + ih, "class": "mc-guide", visibility: "hidden" }, svg);
        var dots = SERIES.map(function (s) { return el("circle", { r: 4, "class": "fc-dot " + s.cls, visibility: "hidden" }, svg); });
        var tip = document.createElement("div");
        tip.className = "mc-tip";
        tip.hidden = true;
        chartEl.appendChild(tip);

        var show = function (ev) {
            var box = svg.getBoundingClientRect();
            var i = Math.round((ev.clientX - box.left - pad.l) / iw * Y * 12);
            i = Math.max(0, Math.min(lines[0].length - 1, i));
            var gx = x(lines[0][i].t);
            guide.setAttribute("x1", gx); guide.setAttribute("x2", gx);
            guide.setAttribute("visibility", "visible");
            tip.textContent = "";
            var head = document.createElement("strong");
            head.textContent = when(lines[0][i].t);
            tip.appendChild(head);
            lines.forEach(function (pts, k) {
                var val = pts[i].balance;
                dots[k].setAttribute("cx", gx); dots[k].setAttribute("cy", y(val));
                dots[k].setAttribute("visibility", "visible");
                var line = document.createElement("div");
                var sw = document.createElement("span");
                sw.className = "mcalc-swatch " + SERIES[k].sw;
                line.appendChild(sw);
                line.appendChild(document.createTextNode(SERIES[k].name + ": " + money.format(val)));
                tip.appendChild(line);
            });
            tip.hidden = false;
            var left = gx + 12;
            if (left + tip.offsetWidth > W) left = gx - 12 - tip.offsetWidth;
            tip.style.left = Math.max(0, left) + "px";
        };
        var hide = function () {
            guide.setAttribute("visibility", "hidden");
            dots.forEach(function (d) { d.setAttribute("visibility", "hidden"); });
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

    var width = 0;
    if (window.ResizeObserver) {
        new ResizeObserver(function () {
            if (chartEl.clientWidth !== width) { width = chartEl.clientWidth; drawChart(); }
        }).observe(chartEl);
    }
    update();
})();
