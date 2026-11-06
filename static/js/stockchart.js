// Stock page chart (stocks/detail.html). Under 768px it's a line drawn as
// SVG from the page's #chart-line data (lineSeries in linechart.go): light,
// and it lets a vertical swipe scroll the page, which TradingView's candles
// didn't. Wider screens get TradingView Lightweight Charts candles, loading
// the library and the bars (StockBars) only then. Both are built lazily and
// kept, so turning an iPad switches between them.
(function () {
    'use strict';

    const container = document.getElementById('chart-container');
    const dataEl = document.getElementById('chart-line');
    if (!container || !dataEl) return;

    // {"12": {d: [dates], c: [closes in cents]}, ...}; "0" is all of it.
    const line = JSON.parse(dataEl.textContent);
    if (!line['0'] || !line['0'].d.length) return;

    const buttons = [...document.querySelectorAll('.chart-ranges [data-range]')];
    const moveEl = document.querySelector('#chart-change .chart-move');
    const spanEl = document.querySelector('#chart-change .chart-span');
    const wide = window.matchMedia('(min-width: 768px)');

    let range = line['12'] ? '12' : '0';
    let lineView = null;
    let candleView = null;
    let candleFailed = false;

    // ---- Formatting ----

    const money = cents => (cents < 0 ? '-$' : '$') +
        (Math.abs(cents) / 100).toLocaleString('en-US', {minimumFractionDigits: 2, maximumFractionDigits: 2});

    const toDate = d => new Date(d + 'T00:00:00Z');
    const dayLabel = d => toDate(d).toLocaleDateString('en-AU', {day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC'});

    const spanText = {
        '1': 'over 1 month',
        '6': 'over 6 months',
        '12': 'over 1 year',
        '60': 'over 5 years',
    };

    // The change over the range, as the price header words a day's change.
    function showChange() {
        const s = line[range];
        const first = s.c[0], last = s.c[s.c.length - 1];
        const diff = last - first;
        const pct = first ? diff / first * 100 : 0;
        const sign = diff >= 0 ? '+' : '-';
        moveEl.textContent = sign + money(Math.abs(diff)) + ' (' + sign + Math.abs(pct).toFixed(2) + '%)';
        moveEl.className = 'chart-move ' + (diff >= 0 ? 'text-green' : 'text-red');
        spanEl.textContent = spanText[range] || 'since ' + toDate(s.d[0]).getUTCFullYear();
    }

    // ---- Line (SVG) ----

    const NS = 'http://www.w3.org/2000/svg';
    function el(name, attrs, parent) {
        const e = document.createElementNS(NS, name);
        for (const k in attrs) e.setAttribute(k, attrs[k]);
        if (parent) parent.appendChild(e);
        return e;
    }

    // A round step near span/3, for about four gridlines.
    function niceStep(span) {
        const raw = span / 3;
        const mag = Math.pow(10, Math.floor(Math.log10(raw)));
        const f = raw / mag;
        return (f < 1.5 ? 1 : f < 2.25 ? 2 : f < 3.5 ? 2.5 : f < 7.5 ? 5 : 10) * mag;
    }

    // Indexes to label along the bottom: where the day, month or year
    // changes, thinned to what fits.
    function dateTicks(d, width) {
        const unit = range === '1' ? 'week' : range === '6' || range === '12' ? 'month' : 'year';
        const key = s => {
            const t = toDate(s);
            if (unit === 'year') return t.getUTCFullYear();
            if (unit === 'month') return t.getUTCFullYear() * 12 + t.getUTCMonth();
            return Math.floor((t.getTime() / 86400000 + 3) / 7); // Monday-start weeks
        };
        let ticks = [];
        for (let i = 1; i < d.length; i++) if (key(d[i]) !== key(d[i - 1])) ticks.push(i);
        const fit = Math.max(2, Math.floor(width / 72));
        if (ticks.length > fit) {
            const step = Math.ceil(ticks.length / fit);
            ticks = ticks.filter((_, j) => j % step === 0);
        }
        return ticks.map(i => {
            const t = toDate(d[i]);
            let text;
            if (unit === 'year') text = String(t.getUTCFullYear());
            else if (unit === 'month' && t.getUTCMonth() === 0) text = String(t.getUTCFullYear());
            else if (unit === 'month') text = t.toLocaleDateString('en-AU', {month: 'short', timeZone: 'UTC'});
            else text = t.toLocaleDateString('en-AU', {day: 'numeric', month: 'short', timeZone: 'UTC'});
            return {i, text};
        });
    }

    function buildLine() {
        const box = document.createElement('div');
        box.className = 'chart-line';
        container.appendChild(box);
        const view = {box, svg: null, geo: null};

        function draw() {
            const w = box.clientWidth, h = box.clientHeight;
            if (!w || !h) return;
            const s = line[range];
            const n = s.c.length;
            const padR = 56, padT = 14, padB = 26;
            const plotW = w - padR, plotH = h - padT - padB;

            let lo = Math.min(...s.c), hi = Math.max(...s.c);
            if (hi === lo) { lo -= Math.max(1, lo * 0.01); hi += Math.max(1, hi * 0.01); }
            const pad = (hi - lo) * 0.06;
            const top = hi + pad, bot = Math.max(0, lo - pad);
            const x = i => n > 1 ? i / (n - 1) * plotW : plotW / 2;
            const y = c => padT + (top - c) / (top - bot) * plotH;

            const up = s.c[n - 1] >= s.c[0];
            const svg = el('svg', {
                width: w, height: h, viewBox: '0 0 ' + w + ' ' + h,
                class: up ? 'up' : 'down', role: 'img',
                'aria-label': 'Price chart, ' + moveEl.textContent + ' ' + spanEl.textContent,
            });

            const defs = el('defs', {}, svg);
            const grad = el('linearGradient', {id: 'chart-fill', x1: 0, y1: 0, x2: 0, y2: 1}, defs);
            el('stop', {offset: '0%', class: 'fill-top'}, grad);
            el('stop', {offset: '100%', class: 'fill-bottom'}, grad);

            // Gridlines and price labels.
            const step = niceStep(hi - lo);
            const dec = step % 100 === 0 ? 0 : step % 10 === 0 ? 1 : 2;
            for (let v = Math.ceil(lo / step) * step; v <= hi + 1e-9; v += step) {
                const gy = y(v);
                el('line', {x1: 0, x2: plotW, y1: gy, y2: gy, class: 'grid'}, svg);
                const t = el('text', {x: plotW + 8, y: gy + 4, class: 'axis'}, svg);
                t.textContent = '$' + (v / 100).toLocaleString('en-US', {minimumFractionDigits: dec, maximumFractionDigits: dec});
            }

            // Date labels, leaving out any too near an edge to fit.
            for (const tk of dateTicks(s.d, plotW)) {
                if (x(tk.i) < 18 || x(tk.i) > plotW - 18) continue;
                const t = el('text', {x: x(tk.i), y: h - 8, class: 'axis', 'text-anchor': 'middle'}, svg);
                t.textContent = tk.text;
            }

            // The line and the fill under it.
            let path = '';
            for (let i = 0; i < n; i++) path += (i ? 'L' : 'M') + x(i).toFixed(1) + ' ' + y(s.c[i]).toFixed(1);
            if (n === 1) path += 'h0.1';
            el('path', {d: path + 'L' + x(n - 1).toFixed(1) + ' ' + (padT + plotH) + 'L' + x(0).toFixed(1) + ' ' + (padT + plotH) + 'Z', class: 'area'}, svg);
            el('path', {d: path, class: 'stroke'}, svg);

            // The scrub marker, shown while a finger or the mouse is on it.
            const mark = el('g', {class: 'mark', visibility: 'hidden'}, svg);
            const mline = el('line', {y1: padT, y2: padT + plotH}, mark);
            const mdot = el('circle', {r: 4.5}, mark);

            if (view.svg) view.svg.replaceWith(svg); else box.appendChild(svg);
            view.svg = svg;
            view.geo = {n, plotW, x, y, mark, mline, mdot, s};
        }

        function scrub(e) {
            const g = view.geo;
            if (!g) return;
            const r = view.svg.getBoundingClientRect();
            const px = Math.min(Math.max(e.clientX - r.left, 0), g.plotW);
            const i = g.n > 1 ? Math.round(px / g.plotW * (g.n - 1)) : 0;
            const mx = g.x(i), my = g.y(g.s.c[i]);
            g.mline.setAttribute('x1', mx);
            g.mline.setAttribute('x2', mx);
            g.mdot.setAttribute('cx', mx);
            g.mdot.setAttribute('cy', my);
            g.mark.setAttribute('visibility', 'visible');
            moveEl.textContent = money(g.s.c[i]);
            moveEl.className = 'chart-move';
            spanEl.textContent = dayLabel(g.s.d[i]);
        }

        function endScrub() {
            if (view.geo) view.geo.mark.setAttribute('visibility', 'hidden');
            showChange();
        }

        // touch-action: pan-y (app.css) leaves vertical swipes to the page;
        // a vertical swipe cancels the pointer, which ends the scrub.
        box.addEventListener('pointerdown', scrub);
        box.addEventListener('pointermove', scrub);
        box.addEventListener('pointerup', e => { if (e.pointerType !== 'mouse') endScrub(); });
        box.addEventListener('pointercancel', endScrub);
        box.addEventListener('pointerleave', endScrub);

        new ResizeObserver(draw).observe(box);
        view.draw = draw;
        return view;
    }

    // ---- Candles (TradingView Lightweight Charts) ----

    function loadLibrary() {
        return new Promise((resolve, reject) => {
            if (window.LightweightCharts) return resolve();
            const s = document.createElement('script');
            s.src = container.dataset.lib;
            s.onload = resolve;
            s.onerror = reject;
            document.head.appendChild(s);
        });
    }

    function buildCandles() {
        const box = document.createElement('div');
        box.className = 'chart-candles';
        container.appendChild(box);
        const view = {box, ready: false, showRange: null, update: null};

        Promise.all([
            loadLibrary(),
            fetch(container.dataset.bars, {credentials: 'same-origin'}).then(r => {
                if (!r.ok) throw new Error('bars ' + r.status);
                return r.json();
            }),
        ]).then(([, bars]) => {
            if (!bars.length) throw new Error('no bars');
            drawCandles(view, bars);
        }).catch(() => {
            // Fall back to the line rather than an empty box.
            candleFailed = true;
            box.remove();
            candleView = null;
            show();
        });
        return view;
    }

    function drawCandles(view, raw) {
        // The bars are daily for the last two years and weekly before that
        // (chartBars in chartbars.go). The chart spaces bars evenly, not by
        // date, so ranges over two years use weekly bars throughout, or the
        // last two years would fill most of the width.
        function weekOf(date) {
            const d = toDate(date);
            d.setUTCDate(d.getUTCDate() - (d.getUTCDay() + 6) % 7);
            return d.toISOString().slice(0, 10);
        }
        const weeks = [];
        for (const d of raw) {
            const w = weeks[weeks.length - 1];
            if (!w || weekOf(w.t) !== weekOf(d.t)) { weeks.push({...d}); continue; }
            w.h = Math.max(w.h, d.h);
            w.l = Math.min(w.l, d.l);
            w.c = d.c;
            w.v += d.v;
        }

        const candles = rows => rows.map(d => ({time: d.t, open: d.o / 100, high: d.h / 100, low: d.l / 100, close: d.c / 100}));
        const volumes = rows => rows.map(d => ({
            time: d.t,
            value: d.v,
            color: d.c >= d.o ? 'rgba(34,197,94,0.35)' : 'rgba(239,68,68,0.35)',
        }));

        const box = view.box;
        const chart = LightweightCharts.createChart(box, {
            width: box.clientWidth,
            height: box.clientHeight,
            layout: {
                background: {color: '#1a1d27'},
                textColor: '#9ca3af',
                fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
            },
            grid: {vertLines: {color: '#2a2d3a'}, horzLines: {color: '#2a2d3a'}},
            crosshair: {
                mode: LightweightCharts.CrosshairMode.Normal,
                vertLine: {labelBackgroundColor: '#3b82f6'},
                horzLine: {labelBackgroundColor: '#3b82f6'},
            },
            rightPriceScale: {borderColor: '#2a2d3a', scaleMargins: {top: 0.05, bottom: 0.25}},
            timeScale: {borderColor: '#2a2d3a', timeVisible: false, fixLeftEdge: true, fixRightEdge: true},
            // On a touch screen (an iPad), a vertical swipe scrolls the page.
            handleScroll: {vertTouchDrag: false},
        });

        const candleSeries = chart.addCandlestickSeries({
            // The price axis leaves room under the lowest price for the volume
            // bars, which on a long chart runs below $0. A price is never
            // negative, so those labels stay blank.
            priceFormat: {type: 'custom', minMove: 0.01, formatter: p => p < 0 ? '' : p.toFixed(2)},
            upColor: '#22c55e', downColor: '#ef4444',
            borderUpColor: '#22c55e', borderDownColor: '#ef4444',
            wickUpColor: '#22c55e', wickDownColor: '#ef4444',
        });
        const volumeSeries = chart.addHistogramSeries({priceFormat: {type: 'volume'}, priceScaleId: 'volume'});
        chart.priceScale('volume').applyOptions({scaleMargins: {top: 0.8, bottom: 0}});

        const last = raw[raw.length - 1].t;
        let rows = null;
        view.showRange = function (r) {
            const months = +r;
            const want = months === 0 || months > 24 ? weeks : raw;
            if (want !== rows) {
                rows = want;
                candleSeries.setData(candles(rows));
                volumeSeries.setData(volumes(rows));
            }
            if (months) chart.timeScale().setVisibleRange({from: monthsBack(last, months), to: last});
            else chart.timeScale().fitContent();
        };

        // Today's candle follows the price header. On weekly bars, today
        // joins this week's bar.
        view.update = function (d) {
            if (d.date < last) return;
            let bar = {time: d.date, open: d.o / 100, high: d.h / 100, low: d.l / 100, close: d.c / 100};
            const w = weeks[weeks.length - 1];
            if (rows === weeks && weekOf(w.t) === weekOf(d.date)) {
                bar = {time: w.t, open: w.o / 100, high: Math.max(w.h, d.h) / 100, low: Math.min(w.l, d.l) / 100, close: d.c / 100};
            }
            candleSeries.update(bar);
        };

        new ResizeObserver(() => {
            if (box.clientWidth) chart.applyOptions({width: box.clientWidth, height: box.clientHeight});
        }).observe(box);

        view.ready = true;
        if (candleView === view && !view.box.hidden) view.showRange(range);
    }

    function monthsBack(last, n) {
        const d = toDate(last);
        d.setUTCMonth(d.getUTCMonth() - n);
        return d.toISOString().slice(0, 10);
    }

    // ---- Switching ----

    function show() {
        const useCandles = wide.matches && !candleFailed;
        if (useCandles && !candleView) candleView = buildCandles();
        if (!useCandles && !lineView) lineView = buildLine();
        if (lineView) lineView.box.hidden = useCandles;
        if (candleView) candleView.box.hidden = !useCandles;
        showChange();
        if (useCandles) { if (candleView.ready) candleView.showRange(range); }
        else lineView.draw();
        for (const b of buttons) {
            const on = b.dataset.range === range;
            b.classList.toggle('chip-on', on);
            b.setAttribute('aria-pressed', on);
        }
    }

    for (const b of buttons) {
        if (!line[b.dataset.range]) { b.hidden = true; continue; }
        b.addEventListener('click', () => { range = b.dataset.range; show(); });
    }
    wide.addEventListener('change', show);
    show();

    // The price header refreshes itself while the market is open (see
    // _price_stats.html). Move the line's last point and today's candle to
    // match, and tell the ticket (ticket.js).
    document.body.addEventListener('htmx:after:swap', function () {
        const ds = document.getElementById('price-stats')?.dataset;
        const all = line['0'];
        if (!ds || !ds.date || ds.date < all.d[all.d.length - 1]) return;
        const d = {date: ds.date, o: parseInt(ds.o, 10), h: parseInt(ds.h, 10), l: parseInt(ds.l, 10), c: parseInt(ds.c, 10)};
        for (const k in line) {
            const s = line[k];
            if (s.d[s.d.length - 1] === d.date) s.c[s.c.length - 1] = d.c;
            else { s.d.push(d.date); s.c.push(d.c); }
        }
        if (candleView && candleView.ready) candleView.update(d);
        if (lineView && !lineView.box.hidden) lineView.draw();
        showChange();
        document.dispatchEvent(new CustomEvent('price:update', {detail: {close: d.c}}));
    });
})();
