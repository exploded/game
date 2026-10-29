package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

// Static serves the embedded static files. Each file's URL carries a hash of
// its contents (?v=...), so browsers can cache it for a year and still pick
// up a new version the moment it changes.
type Static struct {
	fsys   fs.FS
	hashes map[string]string // path relative to static/ -> short content hash
}

// NewStatic hashes every file in fsys. Only files found here are served, so
// directory listings and unknown paths are a 404.
func NewStatic(fsys fs.FS) (*Static, error) {
	s := &Static{fsys: fsys, hashes: map[string]string{}}
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		s.hashes[path] = hex.EncodeToString(sum[:6])
		return nil
	})
	return s, err
}

// URL returns the cache-busting URL for a static file, e.g. "css/app.css" ->
// "/static/css/app.css?v=1a2b3c4d5e6f". Used by the "asset" template func.
func (s *Static) URL(path string) string {
	hash, ok := s.hashes[path]
	if !ok {
		slog.Error("asset not found", "path", path)
		return "/static/" + path
	}
	return "/static/" + path + "?v=" + hash
}

// ServeHTTP serves /static/<path>.
func (s *Static) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.ServeFile(w, r, strings.TrimPrefix(r.URL.Path, "/static/"))
}

// ServeFile serves one static file. A request carrying the current hash is
// cached for a year; anything else must revalidate, which the ETag makes a
// cheap 304.
func (s *Static) ServeFile(w http.ResponseWriter, r *http.Request, path string) {
	hash, ok := s.hashes[path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("ETag", `"`+hash+`"`)
	if r.URL.Query().Get("v") == hash {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFileFS(w, r, s.fsys, path)
}
