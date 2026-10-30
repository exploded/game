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
            : status === 403 ? "Session expired - please reload the page"
            : status >= 500 ? "Server error - please try again"
            : "Request failed", "error");
});
document.body.addEventListener("htmx:error", function() {
    showToast("Connection problem - please try again", "error");
});

// data-confirm on a submit button or a form: ask before submitting.
document.addEventListener("submit", function(e) {
    const el = (e.submitter && e.submitter.closest("[data-confirm]")) || e.target.closest("form[data-confirm]");
    if (el && !confirm(el.getAttribute("data-confirm"))) {
        e.preventDefault();
    }
});

// data-autosubmit on a select: submit its form when it changes.
document.addEventListener("change", function(e) {
    if (e.target.matches("select[data-autosubmit]")) {
        e.target.form.requestSubmit();
    }
});

// data-nav-toggle: open and close the mobile nav.
document.addEventListener("click", function(e) {
    const btn = e.target.closest("[data-nav-toggle]");
    if (btn) {
        btn.parentElement.classList.toggle("nav-open");
    }
});

// details[data-dismiss] (the game switcher and the phone tab bar's More
// menu): close when tapping anywhere outside it.
document.addEventListener("click", function(e) {
    document.querySelectorAll("details[data-dismiss][open]").forEach(function(d) {
        if (!d.contains(e.target)) d.removeAttribute("open");
    });
});

// data-reset-on-success on an htmx form: clear it and refocus after a
// successful request (chat and lobby).
document.body.addEventListener("htmx:after:request", function(e) {
    const form = e.target.closest("form[data-reset-on-success]");
    if (!form || e.detail.ctx.response.status >= 400) return;
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

// data-play on a video's button: swap in the player only when it's tapped,
// so a page of videos doesn't load a dozen third-party players up front.
document.addEventListener("click", function(e) {
    const btn = e.target.closest("[data-play]");
    if (!btn) return;
    const src = new URL(btn.getAttribute("data-play"));
    src.searchParams.set("autoplay", "1");
    const frame = document.createElement("iframe");
    frame.src = src.toString();
    frame.title = btn.getAttribute("data-title") || "Video";
    frame.allow = "accelerometer; autoplay; encrypted-media; gyroscope; picture-in-picture; fullscreen";
    frame.referrerPolicy = "strict-origin-when-cross-origin";
    btn.replaceWith(frame);
});
