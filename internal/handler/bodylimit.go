package handler

import (
	"net/http"
	"os"
)

// MaxBodySize limits the size of incoming request bodies.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// NoListFileServer serves static files but returns 404 for directory listings.
type NoListFileServer struct {
	fs http.FileSystem
}

func NewNoListFileServer(dir string) *NoListFileServer {
	return &NoListFileServer{fs: http.Dir(dir)}
}

func (nfs *NoListFileServer) Open(name string) (http.File, error) {
	f, err := nfs.fs.Open(name)
	if err != nil {
		return nil, err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if stat.IsDir() {
		f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}
