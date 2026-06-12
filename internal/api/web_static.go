package api

import (
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

func newWebAppHandler(dist fs.FS) http.Handler {
	return &webAppHandler{dist: dist}
}

type webAppHandler struct {
	dist fs.FS
}

func (h *webAppHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	filePath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if filePath == "." || filePath == "" {
		filePath = "index.html"
	}

	if h.serveNamedFile(w, r, filePath) {
		return
	}

	if !h.serveNamedFile(w, r, "index.html") {
		http.NotFound(w, r)
	}
}

func (h *webAppHandler) serveNamedFile(w http.ResponseWriter, r *http.Request, name string) bool {
	file, err := h.dist.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		return false
	}

	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else if name == "index.html" {
		w.Header().Set("Cache-Control", "no-store, max-age=0, must-revalidate")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}

	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	reader, ok := file.(interface {
		Read([]byte) (int, error)
		Seek(int64, int) (int64, error)
	})
	if !ok {
		http.Error(w, os.ErrInvalid.Error(), http.StatusInternalServerError)
		return true
	}

	http.ServeContent(w, r, name, stat.ModTime(), reader)
	return true
}
