package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serverError logs a real failure against the handler that called it, and
// says nothing about a request the browser abandoned.
func TestServerError(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{AddSource: true})))
	t.Cleanup(func() { slog.SetDefault(old) })
	h := &Handler{}

	rec := httptest.NewRecorder()
	h.serverError(rec, "stocks page", fmt.Errorf("query: %w", context.Canceled))
	if buf.Len() != 0 || rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("cancelled request: logged %q, sent %d %q; want nothing", buf.String(), rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.serverError(rec, "stocks page", errors.New("disk full"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if log := buf.String(); !strings.Contains(log, "servererror_test.go") || !strings.Contains(log, "disk full") {
		t.Errorf("log = %q, want the caller's file and the error", log)
	}
}
