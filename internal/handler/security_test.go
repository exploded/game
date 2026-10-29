package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersNonce(t *testing.T) {
	var seen string
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = cspNonce(r)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	csp := w.Header().Get("Content-Security-Policy")

	if seen == "" {
		t.Fatal("no nonce in the request context")
	}
	if !strings.Contains(csp, "'nonce-"+seen+"'") {
		t.Errorf("CSP doesn't carry the context nonce %q: %s", seen, csp)
	}
	scriptSrc := directive(csp, "script-src")
	if strings.Contains(scriptSrc, "'unsafe-inline'") {
		t.Errorf("script-src allows 'unsafe-inline': %s", scriptSrc)
	}

	first := seen
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if seen == first {
		t.Error("nonce was reused across requests")
	}
}

// directive returns one directive from a CSP header, e.g. "script-src ...".
func directive(csp, name string) string {
	for _, d := range strings.Split(csp, ";") {
		if d = strings.TrimSpace(d); strings.HasPrefix(d, name+" ") {
			return d
		}
	}
	return ""
}
