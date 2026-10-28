// Package web отдаёт встроенный интерфейс mbtorrent.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets
var assets embed.FS

// Handler возвращает файлы интерфейса из бинарника.
func Handler() http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})
}
