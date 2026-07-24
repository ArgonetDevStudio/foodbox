package web

import (
	"bytes"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

const indexFile = "index.html"

// New serves a Svelte build from staticDir. The directory may be absent while
// the backend is built independently; requests then receive 404 responses.
func New(staticDir string) http.Handler {
	return NewFS(os.DirFS(staticDir))
}

// NewFS is the packaging seam for embedded or on-disk Svelte assets. A future
// embed.FS can be passed directly (or through fs.Sub) without changing routing.
func NewFS(staticFS fs.FS) http.Handler {
	return &staticHandler{staticFS: staticFS}
}

type staticHandler struct {
	staticFS fs.FS
}

func (handler *staticHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		http.NotFound(response, request)
		return
	}
	cleanedPath := path.Clean("/" + request.URL.Path)
	if reservedPath(cleanedPath) {
		http.NotFound(response, request)
		return
	}

	requestedFile := strings.TrimPrefix(cleanedPath, "/")
	if requestedFile == "." || requestedFile == "" {
		requestedFile = indexFile
	}
	if !fs.ValidPath(requestedFile) {
		http.NotFound(response, request)
		return
	}

	if handler.serveFile(response, request, requestedFile) {
		return
	}
	if requestedFile != indexFile && handler.serveFile(response, request, indexFile) {
		return
	}
	http.NotFound(response, request)
}

func (handler *staticHandler) serveFile(response http.ResponseWriter, request *http.Request, name string) bool {
	if handler.staticFS == nil {
		return false
	}
	info, err := fs.Stat(handler.staticFS, name)
	if err != nil || info.IsDir() {
		return false
	}
	contents, err := fs.ReadFile(handler.staticFS, name)
	if err != nil {
		return false
	}

	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(response, request, name, fileModTime(info), bytes.NewReader(contents))
	return true
}

func fileModTime(info fs.FileInfo) time.Time {
	if info.ModTime().IsZero() {
		return time.Unix(0, 0)
	}
	return info.ModTime()
}

func reservedPath(requestPath string) bool {
	return requestPath == "/api" ||
		strings.HasPrefix(requestPath, "/api/") ||
		requestPath == "/healthz" ||
		strings.HasPrefix(requestPath, "/healthz/")
}
