// The CSP blocks inline event handlers (onclick="..."), so page behaviour
// lives here, hooked up through data- attributes.

// Toast notification listener
document.body.addEventListener("showToast", function(e) {
    const { msg, type } = e.detail;
    showToast(msg, type);
});

// Toasts stay long enough to read on a phone (errors longer), and a tap
// dismisses one early.
function showToast(msg, type) {
    const container = document.getElementById("toast-container");
    if (!container) return;
    const toast = document.createElement("div");
    toast.className = "toast toast-" + (type || "info");
    toast.setAttribute("role", type === "error" ? "alert" : "status");
    toast.textContent = msg;
    container.appendChild(toast);
    const hide = () => {
        clearTimeout(timer);
        toast.classList.remove("toast-show");
        setTimeout(() => toast.remove(), 300);
    };
    const timer = setTimeout(hide, type === "error" ? 9000 : 6000);
    toast.addEventListener("click", hide);
    setTimeout(() => toast.classList.add("toast-show"), 10);
}

// htmx config keeps 4xx/5xx responses out of the page, so say something.
document.body.addEventListener("htmx:response:error", function(e) {
    const status = e.detail.ctx.response.status;
    showToast(status === 429 ? "Slow down - too many requests"
            : status === 503 ? "The site is busy - please try again in a minute"
            : status === 403 ? "Session expired - please reload the page"
            : status >= 500 ? "Server error - please try again"
            : "Request failed", "error");
});
document.body.addEventListener("htmx:error", function() {
    showToast("Connection problem - please try again", "error");
});

// askConfirm shows the site's own confirm dialog (the browser's confirm()
// box can't be styled) and resolves true if the player says yes. The yes
// button takes okLabel, or the label of the button that asked, and is red
// when that button is btn-danger, with No focused so Enter is safe.
let confirmDialog;
function askConfirm(msg, btn, okLabel) {
    if (!confirmDialog) {
        confirmDialog = document.createElement("dialog");
        confirmDialog.className = "confirm";
        confirmDialog.innerHTML = '<form method="dialog"><p class="confirm-msg"></p><div class="confirm-actions">'
            + '<button value="" class="btn btn-secondary"></button><button value="ok" class="btn"></button></div></form>';
        document.body.appendChild(confirmDialog);
        // A tap on the backdrop says no.
        confirmDialog.addEventListener("click", function(e) {
            if (e.target === confirmDialog) confirmDialog.close();
        });
    }
    const no = confirmDialog.querySelector('[value=""]');
    const yes = confirmDialog.querySelector('[value="ok"]');
    const danger = !!btn && btn.classList.contains("btn-danger");
    const label = okLabel || (btn && btn.textContent.trim()) || "OK";
    confirmDialog.querySelector(".confirm-msg").textContent = msg;
    yes.textContent = label;
    yes.className = "btn " + (danger ? "btn-danger" : "btn-primary");
    no.textContent = /^cancel\b/i.test(label) ? "Go back" : "Cancel";
    return new Promise(function(resolve) {
        confirmDialog.addEventListener("close", function() {
            resolve(confirmDialog.returnValue === "ok");
        }, {once: true});
        confirmDialog.returnValue = "";
        confirmDialog.showModal();
        (danger ? no : yes).focus();
    });
}

// data-confirm on a submit button or a form: ask before submitting.
// data-confirm-ok beside it overrides the yes button's label.
let confirmedForm = null;
document.addEventListener("submit", function(e) {
    const form = e.target;
    const el = (e.submitter && e.submitter.closest("[data-confirm]")) || form.closest("form[data-confirm]");
    if (!el) return;
    if (confirmedForm === form) {
        confirmedForm = null;
        return;
    }
    e.preventDefault();
    const btn = e.submitter;
    askConfirm(el.getAttribute("data-confirm"), btn, el.getAttribute("data-confirm-ok")).then(function(ok) {
        if (!ok) return;
        confirmedForm = form;
        form.requestSubmit(btn);
    });
});

// hx-confirm: the same dialog for htmx requests.
document.body.addEventListener("htmx:confirm", function(e) {
    e.preventDefault();
    const d = e.detail, src = d.ctx.sourceElement;
    askConfirm(d.ctx.confirm, src.closest("button"), src.getAttribute("data-confirm-ok")).then(function(ok) {
        if (ok) d.issueRequest(); else d.dropRequest();
    });
});

// data-autosubmit on a select: submit its form when it changes.
document.addEventListener("change", function(e) {
    if (e.target.matches("select[data-autosubmit]")) {
        e.target.form.requestSubmit();
    }
});

// data-prefill on a form (Add a stream): JSON of field values for each
// value of the select named by data-prefill-by. Choosing an option fills the
// fields, leaving alone any the admin has typed in.
document.addEventListener("DOMContentLoaded", function() {
    document.querySelectorAll("form[data-prefill]").forEach(function(form) {
        let sets;
        try { sets = JSON.parse(form.getAttribute("data-prefill")); } catch (err) { return; }
        const select = form.elements[form.getAttribute("data-prefill-by")];
        if (!select) return;
        const names = new Set();
        Object.values(sets).forEach(function(set) { Object.keys(set).forEach(function(n) { names.add(n); }); });
        const fill = function() {
            const set = sets[select.value] || {};
            names.forEach(function(name) {
                const field = form.elements[name];
                if (!field || (field.value !== "" && field.value !== field.dataset.filled)) return;
                field.value = set[name] || "";
                field.dataset.filled = field.value;
            });
        };
        select.addEventListener("change", fill);
        fill();
    });
});

// data-clear-search on a form (the stocks page's chips): changing it
// empties the search box it names, so the chip's list shows, not a search.
document.addEventListener("change", function(e) {
    const form = e.target.closest("form[data-clear-search]");
    const box = form && document.querySelector(form.getAttribute("data-clear-search"));
    if (box) box.value = "";
});

// A logo that fails to load (Twelve Data names some it doesn't have) becomes
// the stock's letter in a list, and disappears on the stock page. Error
// events don't bubble, so this listens in the capture phase, and logos that
// failed before this script ran are caught once the page has loaded.
function brokenLogo(img) {
    if (img.matches("img.sc-logo")) {
        const letter = document.createElement("span");
        letter.className = "sc-logo sc-letter";
        letter.setAttribute("aria-hidden", "true");
        letter.textContent = img.dataset.initial || "";
        img.replaceWith(letter);
    } else if (img.matches("img.stock-logo")) {
        img.remove();
    }
}
document.addEventListener("error", function(e) {
    if (e.target instanceof HTMLImageElement) brokenLogo(e.target);
}, true);
document.addEventListener("DOMContentLoaded", function() {
    document.querySelectorAll("img.sc-logo, img.stock-logo").forEach(function(img) {
        if (img.complete && img.naturalWidth === 0) brokenLogo(img);
    });
});

// A row of chips scrolls sideways on a phone: bring the chosen one into view.
document.addEventListener("DOMContentLoaded", function() {
    document.querySelectorAll(".chips .chip:has(:checked)").forEach(function(chip) {
        chip.scrollIntoView({block: "nearest", inline: "center"});
    });
});

// data-back on a link (the stock page's Back): the server points it at the
// page the player came from. When that's the page before this one in the
// browser's history, go back instead, so the list keeps its scroll position.
document.addEventListener("click", function(e) {
    const link = e.target.closest("a[data-back]");
    if (!link || e.metaKey || e.ctrlKey || e.shiftKey) return;
    let prev;
    try { prev = new URL(document.referrer); } catch (err) { return; }
    const target = new URL(link.href);
    if (history.length > 1 && prev.origin === location.origin
        && prev.pathname + prev.search === target.pathname + target.search) {
        e.preventDefault();
        history.back();
    }
});

// a.stretch in a table row (the stocks list, holdings): a click anywhere in
// the row follows the link, as the link's overlay does on a phone (see
// app.css for why tables can't have one). The star, buttons and other links
// keep their own clicks, and dragging to select text doesn't count.
document.addEventListener("click", function(e) {
    const row = e.target.closest(".rtable tr");
    const link = row && row.querySelector("a.stretch");
    if (!link || e.defaultPrevented || e.button !== 0
        || e.target.closest("a, button, input, select, textarea, label, .c-action, .c-watch")) return;
    const sel = window.getSelection();
    if (sel && !sel.isCollapsed) return;
    if (e.ctrlKey || e.metaKey) window.open(link.href);
    else link.click();
});

// The Stocks tab goes back to the list as the player left it (chip, sector
// or search) rather than the default list. Remembered for this browser tab.
// data-stocks-tab on the link names its game, so it works from any page.
(function() {
    try {
        const m = location.pathname.match(/^\/games\/(\d+)\/stocks\/?$/);
        if (m) {
            // The chips and search change the address without reloading.
            const save = function() { sessionStorage.setItem("stocks-list:" + m[1], location.pathname + location.search); };
            save();
            window.addEventListener("pagehide", save);
            document.body.addEventListener("htmx:after:swap", save);
        }
        document.querySelectorAll("a[data-stocks-tab]").forEach(function(a) {
            const list = sessionStorage.getItem("stocks-list:" + a.getAttribute("data-stocks-tab"));
            if (list) a.href = list;
        });
    } catch (err) {
        // Storage blocked: the tab opens the default list.
    }
})();

// details[data-dismiss] (the account menu and the game switcher): close
// when tapping anywhere outside it.
document.addEventListener("click", function(e) {
    document.querySelectorAll("details[data-dismiss][open]").forEach(function(d) {
        if (!d.contains(e.target)) d.removeAttribute("open");
    });
});

// data-reset-on-success on an htmx form: clear it and refocus after a
// successful request (the lobby). A 204 is a refusal with a toast, so the
// text stays to be changed.
document.body.addEventListener("htmx:after:request", function(e) {
    const form = e.target.closest("form[data-reset-on-success]");
    const status = e.detail.ctx.response.status;
    if (!form || status >= 400 || status === 204) return;
    form.reset();
    const input = form.querySelector("input[type=text]");
    if (input) input.focus();
});

// Auto-inject CSRF hidden field into all POST forms.
document.addEventListener("DOMContentLoaded", function() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    if (!meta) return;
    var token = meta.getAttribute("content");
    if (!token) return;
    document.querySelectorAll("form").forEach(function(form) {
        var method = (form.getAttribute("method") || "GET").toUpperCase();
        if (method !== "GET" && !form.querySelector('input[name="_csrf"]')) {
            var input = document.createElement("input");
            input.type = "hidden";
            input.name = "_csrf";
            input.value = token;
            form.appendChild(input);
        }
    });
});

// Promote flash banners into toasts.
document.addEventListener("DOMContentLoaded", function() {
    document.querySelectorAll(".flash").forEach(function(el) {
        const type = el.classList.contains("flash-success") ? "success"
                   : el.classList.contains("flash-error") ? "error" : "info";
        showToast(el.textContent.trim(), type);
        el.remove();
    });
});

// data-live-poll on #live-slot (base.html): show or hide the "Rob is live"
// banner without a reload. The page asks once a minute, plus up to 10
// seconds' jitter so tabs opened together (after a deploy, an email) spread
// out, and only while the tab is on screen: a hidden tab stops asking, and
// asks straight away when it comes back if a minute has gone by. The server
// answers from memory, and an unchanged banner is a 304 with no body.
// Failures (a 503 when the site is busy, no signal) back off to 10 minutes.
document.addEventListener("DOMContentLoaded", function() {
    const slot = document.querySelector("[data-live-poll]");
    if (!slot) return;
    const url = slot.getAttribute("data-live-poll");
    const every = 60000;
    let etag = "", last = Date.now(), wait = every, timer = 0;
    function schedule() {
        clearTimeout(timer);
        if (!document.hidden) timer = setTimeout(check, wait + Math.random() * 10000);
    }
    function check() {
        last = Date.now();
        fetch(url, { headers: etag ? { "If-None-Match": etag } : {}, credentials: "omit" })
            .then(function(res) {
                if (res.status === 304) { wait = every; return; }
                if (!res.ok) throw new Error("live banner: " + res.status);
                etag = res.headers.get("ETag") || "";
                wait = every;
                return res.text().then(function(html) { slot.innerHTML = html; });
            })
            .catch(function() { wait = Math.min(wait * 2, 600000); })
            .finally(schedule);
    }
    document.addEventListener("visibilitychange", function() {
        if (document.hidden) clearTimeout(timer);
        else if (Date.now() - last >= every) check();
        else schedule();
    });
    schedule();
});

// data-play on a video's button: swap in the player only when it's tapped,
// so a page of videos doesn't load a dozen third-party players up front.
document.addEventListener("click", function(e) {
    const btn = e.target.closest("[data-play]");
    if (btn) startPlayer(btn);
});

// ?play=1 on a lesson's URL: the player got here by clicking through the
// course (Next, Previous, Mark done, the lesson list), so start the video
// straight away. A lesson opened from a shared link or a search result
// waits for a tap. The flag comes off the URL so a reload doesn't replay.
document.addEventListener("DOMContentLoaded", function() {
    const url = new URL(location.href);
    if (url.searchParams.get("play") !== "1") return;
    url.searchParams.delete("play");
    history.replaceState(history.state, "", url.pathname + url.search + url.hash);
    const btn = document.querySelector(".lesson-main [data-play]");
    if (btn) startPlayer(btn);
});

function startPlayer(btn) {
    const src = new URL(btn.getAttribute("data-play"));
    src.searchParams.set("autoplay", "1");
    // On a lesson, the page's own end card (#lesson-end) replaces the video
    // when it ends, so Vimeo's "More from ..." screen doesn't offer videos
    // outside the course. YouTube only reports its state with enablejsapi.
    const end = btn.closest(".lesson-main") ? document.getElementById("lesson-end") : null;
    if (end && src.hostname === "www.youtube-nocookie.com") {
        src.searchParams.set("enablejsapi", "1");
        src.searchParams.set("origin", location.origin);
    }
    const frame = playerFrame(src.toString(), btn.getAttribute("data-title"));
    btn.replaceWith(frame);
    if (end) showCardAtEnd(frame, end);
}

function playerFrame(src, title) {
    const frame = document.createElement("iframe");
    frame.src = src;
    frame.title = title || "Video";
    frame.allow = "accelerometer; autoplay; encrypted-media; gyroscope; picture-in-picture; fullscreen";
    frame.referrerPolicy = "strict-origin-when-cross-origin";
    return frame;
}

// showCardAtEnd listens to the player's postMessage events (Vimeo's
// "ended", YouTube's state 0) and swaps the frame for the end card.
function showCardAtEnd(frame, end) {
    const src = frame.src, title = frame.title;
    const origin = new URL(src).origin;
    const vimeo = origin === "https://player.vimeo.com";
    function send(msg) {
        if (frame.contentWindow) frame.contentWindow.postMessage(msg, origin);
    }
    function subscribe() {
        if (vimeo) {
            send({method: "addEventListener", value: "ended"});
        } else {
            send(JSON.stringify({event: "listening", id: 1, channel: "widget"}));
            send(JSON.stringify({event: "command", func: "addEventListener", args: ["onStateChange"], id: 1, channel: "widget"}));
        }
    }
    function onMessage(e) {
        if (e.source !== frame.contentWindow || e.origin !== origin) return;
        let d = e.data;
        if (typeof d === "string") {
            try { d = JSON.parse(d); } catch (_) { return; }
        }
        if (!d) return;
        if (d.event === "ready") subscribe();
        const ended = d.event === "ended" ||
            (d.event === "onStateChange" && d.info === 0) ||
            (d.event === "infoDelivery" && d.info && d.info.playerState === 0);
        if (!ended) return;
        window.removeEventListener("message", onMessage);
        const card = end.content.firstElementChild.cloneNode(true);
        card.querySelector("[data-lesson-replay]").addEventListener("click", function() {
            const again = playerFrame(src, title);
            card.replaceWith(again);
            showCardAtEnd(again, end);
        });
        frame.replaceWith(card);
    }
    window.addEventListener("message", onMessage);
    frame.addEventListener("load", subscribe);
}

// data-intro-play: the big play button over the site tour (the home and
// sign-in pages). It starts the video; once playing, the video's own controls
// take over. Without JavaScript the button is hidden and the controls work.
document.addEventListener("click", function(e) {
    const btn = e.target.closest("[data-intro-play]");
    if (!btn) return;
    const video = btn.parentElement.querySelector("video");
    if (video) video.play();
});
document.addEventListener("play", function(e) {
    const frame = e.target.closest && e.target.closest(".intro-frame");
    if (frame) frame.classList.add("playing");
}, true);

// data-copy on a button: copy the text of the element whose id it names
// (the API key page's boxes).
document.addEventListener("click", function(e) {
    const btn = e.target.closest("[data-copy]");
    if (!btn) return;
    const src = document.getElementById(btn.getAttribute("data-copy"));
    if (!src) return;
    const text = src.textContent.trim();
    const failed = function() { showToast("Couldn't copy. Select the text and copy it yourself.", "error"); };
    if (!navigator.clipboard) { failed(); return; }
    navigator.clipboard.writeText(text).then(function() { showToast("Copied", "success"); }, failed);
});
