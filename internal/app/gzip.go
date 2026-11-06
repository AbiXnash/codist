package app

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

var zippers = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed); return w }}

type gzipWriter struct {
	http.ResponseWriter
	z *gzip.Writer
}

func (g gzipWriter) Write(b []byte) (int, error) { return g.z.Write(b) }

// gzipped compresses API answers for browsers that accept it. The graph of a large repository is
// megabytes of JSON that shrinks many times over. The event stream is left alone: it must flush.
func gzipped(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/events" || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		z := zippers.Get().(*gzip.Writer)
		defer zippers.Put(z)
		z.Reset(w)
		defer z.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Del("Content-Length")
		next.ServeHTTP(gzipWriter{ResponseWriter: w, z: z}, r)
	})
}
