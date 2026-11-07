// Package web embute a interface (HTML/CSS/JS/imagens) dentro do binário.
// Na partida, cada arquivo de texto é comprimido uma vez com gzip e ganha um ETag,
// então o servidor só copia bytes prontos da memória.
package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:static
var files embed.FS

type asset struct {
	data  []byte
	gz    []byte
	etag  string
	ctype string
}

func Handler(version string) http.Handler {
	sub, _ := fs.Sub(files, "static")
	assets := map[string]*asset{}
	_ = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(sub, p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		a := &asset{data: data, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		a.ctype = mime.TypeByExtension(path.Ext(p))
		if path.Ext(p) == ".avif" {
			a.ctype = "image/avif"
		}
		if a.ctype == "" {
			a.ctype = "application/octet-stream"
		}
		if isText(a.ctype) && len(data) > 1024 {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(data)
			zw.Close()
			if buf.Len() < len(data) {
				a.gz = buf.Bytes()
			}
		}
		assets["/"+p] = a
		return nil
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "método não permitido", http.StatusMethodNotAllowed)
			return
		}
		p := r.URL.Path
		if p == "/" {
			p = "/index.html"
		}
		a, ok := assets[p]
		if !ok {
			// rotas da interface (ex.: /#configuracoes) caem no index
			if !strings.Contains(path.Base(p), ".") {
				a = assets["/index.html"]
			}
			if a == nil {
				http.NotFound(w, r)
				return
			}
		}
		h := w.Header()
		h.Set("Content-Type", a.ctype)
		h.Set("ETag", a.etag)
		h.Set("X-BrunOS-Version", version)
		if strings.HasPrefix(p, "/assets/") {
			h.Set("Cache-Control", "public, max-age=604800")
		} else {
			h.Set("Cache-Control", "no-cache") // revalida com ETag: sempre a versão nova após atualizar
		}
		h.Add("Vary", "Accept-Encoding")
		if match := r.Header.Get("If-None-Match"); match != "" && match == a.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		body := a.data
		if a.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.Set("Content-Encoding", "gzip")
			body = a.gz
		}
		_, _ = w.Write(body)
	})
}

func isText(ct string) bool {
	return strings.HasPrefix(ct, "text/") || strings.Contains(ct, "javascript") || strings.Contains(ct, "json") || strings.Contains(ct, "svg")
}
