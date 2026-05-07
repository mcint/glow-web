// Package web serves rendered markdown over HTTP. The v0 surface is a
// single-file viewer; directory mode and live-reload are deferred.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"

	"github.com/mcint/glow-web/internal/render"
)

//go:embed page.html.tmpl
var assets embed.FS

var pageTmpl = template.Must(template.ParseFS(assets, "page.html.tmpl"))

type pageData struct {
	Title string
	Body  template.HTML
}

// FileHandler returns an http.Handler that re-reads and re-renders mdPath on
// every request. Cheap-and-correct for v0; caching is left for later.
func FileHandler(mdPath string) http.Handler {
	abs, _ := filepath.Abs(mdPath)
	title := filepath.Base(abs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		src, err := os.ReadFile(abs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body, err := render.HTML(src)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pageTmpl.Execute(w, pageData{
			Title: title,
			Body:  template.HTML(body),
		})
	})
}

// ServeFile blocks, serving mdPath at addr.
func ServeFile(addr, mdPath string) error {
	abs, err := filepath.Abs(mdPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "glow-web: serving %s on http://%s\n", abs, addr)
	return http.ListenAndServe(addr, FileHandler(abs))
}
