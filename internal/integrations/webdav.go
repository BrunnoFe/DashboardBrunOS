package integrations

// Arquivos do Nextcloud por WebDAV (0.37.0, decisão 73): com "Usuário dos arquivos" + "Senha de app" na
// Integração Nextcloud, o lugar "Nextcloud" aparece na janela Arquivos. Só a biblioteca padrão: PROPFIND
// (listar), GET com Range (ver e baixar), PUT (enviar), MKCOL (pasta), MOVE/COPY e DELETE (que no Nextcloud
// vai para a lixeira dele). Mesma proteção das outras integrações: nada de endereço interno nem redirecionar.

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"brunos/internal/files"
)

type davRemote struct {
	c          *http.Client
	base       string // .../remote.php/dav/files/<usuário> (sem barra no fim)
	user, pass string
}

// FileRemotes devolve os lugares remotos para a janela Arquivos: cada Integração Nextcloud com usuário
// dos arquivos e senha de app.
func (h *Hub) FileRemotes() []files.RemoteRoot {
	var out []files.RemoteRoot
	for _, it := range h.Store.List() {
		if it.Kind != "nextcloud" || it.Secret2 == "" || strings.TrimSpace(it.Options["files_user"]) == "" {
			continue
		}
		pass, err := h.Store.box.Open(it.Secret2)
		if err != nil {
			continue
		}
		user := strings.TrimSpace(it.Options["files_user"])
		out = append(out, files.RemoteRoot{ID: "nc-" + it.ID, Name: it.Name, R: &davRemote{
			c:    h.davClient(),
			base: strings.TrimRight(it.URL, "/") + "/remote.php/dav/files/" + url.PathEscape(user),
			user: user, pass: pass,
		}})
	}
	return out
}

// davClient: sem limite de tempo total (um vídeo pode tocar por horas); quem chama passa o contexto.
func (h *Hub) davClient() *http.Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.dav == nil {
		h.dav = &http.Client{Transport: safeTransport(), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return h.dav
}

// href de um caminho relativo: cada pedaço codificado (nomes com espaço, acento, #...).
func (d *davRemote) href(rel string) string {
	if rel == "." || rel == "" {
		return d.base + "/"
	}
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return d.base + "/" + strings.Join(parts, "/")
}

func (d *davRemote) do(ctx context.Context, method, rel string, body io.Reader, size int64, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.href(rel), body)
	if err != nil {
		return nil, files.ErrBadPath
	}
	if size >= 0 && body != nil {
		req.ContentLength = size
	}
	req.SetBasicAuth(d.user, d.pass)
	req.Header.Set("User-Agent", "BrunOS")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := d.c.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ferr("não consegui falar com o Nextcloud (confira o endereço e se ele está ligado)")
	}
	if resp.StatusCode < 300 {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	return nil, davErr(resp.StatusCode)
}

// davErr traduz a resposta do Nextcloud nos erros da janela Arquivos (mensagens em pt-BR).
func davErr(code int) error {
	switch code {
	case 401:
		return ferr("o Nextcloud recusou o acesso: confira o usuário e a senha de app na integração")
	case 403:
		return ferr("o Nextcloud não deixa fazer isso nesta pasta")
	case 404:
		return files.ErrNotFound
	case 405, 412:
		return files.ErrExists
	case 409:
		return ferr("a pasta de destino não existe no Nextcloud")
	case 423:
		return ferr("o arquivo está travado no Nextcloud (alguém está editando)")
	case 507:
		return files.ErrNoSpace
	}
	return ferr("o Nextcloud respondeu com erro %d", code)
}

type davProp struct {
	Href  string `xml:"href"`
	Props []struct {
		Status string `xml:"status"`
		Prop   struct {
			Type struct {
				Coll *struct{} `xml:"collection"`
			} `xml:"resourcetype"`
			Length   string `xml:"getcontentlength"`
			Modified string `xml:"getlastmodified"`
		} `xml:"prop"`
	} `xml:"propstat"`
}

const propfindBody = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/></d:prop></d:propfind>`

const maxListing = 8 << 20 // resposta de uma pasta (milhares de arquivos cabem folgados)

func (d *davRemote) propfind(ctx context.Context, rel, depth string) ([]files.Entry, []string, error) {
	resp, err := d.do(ctx, "PROPFIND", rel, strings.NewReader(propfindBody), int64(len(propfindBody)),
		map[string]string{"Depth": depth, "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	var ms struct {
		R []davProp `xml:"response"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxListing)).Decode(&ms); err != nil {
		return nil, nil, ferr("resposta do Nextcloud em formato inesperado")
	}
	var out []files.Entry
	var hrefs []string
	for _, r := range ms.R {
		e := files.Entry{}
		h, err := url.PathUnescape(r.Href)
		if err != nil {
			continue
		}
		e.Name = path.Base(strings.TrimRight(h, "/"))
		for _, p := range r.Props {
			if !strings.Contains(p.Status, " 200") {
				continue
			}
			e.Dir = p.Prop.Type.Coll != nil
			e.Size, _ = strconv.ParseInt(p.Prop.Length, 10, 64)
			if t, err := time.Parse(time.RFC1123, p.Prop.Modified); err == nil {
				e.Mtime = t.Unix()
			}
		}
		e.Hidden = strings.HasPrefix(e.Name, ".")
		out = append(out, e)
		hrefs = append(hrefs, strings.TrimRight(h, "/"))
	}
	return out, hrefs, nil
}

func (d *davRemote) List(ctx context.Context, rel string) ([]files.Entry, error) {
	list, hrefs, err := d.propfind(ctx, rel, "1")
	if err != nil {
		return nil, err
	}
	// a primeira resposta é a própria pasta: fica de fora (pelo caminho, não pela posição)
	self := ""
	if u, err := url.Parse(d.href(rel)); err == nil {
		self = strings.TrimRight(u.Path, "/")
	}
	out := make([]files.Entry, 0, len(list))
	for i, e := range list {
		if hrefs[i] == self || e.Name == "" {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (d *davRemote) Stat(ctx context.Context, rel string) (files.Entry, error) {
	list, _, err := d.propfind(ctx, rel, "0")
	if err != nil {
		return files.Entry{}, err
	}
	if len(list) == 0 {
		return files.Entry{}, files.ErrNotFound
	}
	return list[0], nil
}

func (d *davRemote) Get(ctx context.Context, rel, rng string) (*http.Response, error) {
	hdr := map[string]string{}
	if rng != "" && len(rng) < 100 && strings.HasPrefix(rng, "bytes=") {
		hdr["Range"] = rng
	}
	return d.do(ctx, http.MethodGet, rel, nil, -1, hdr)
}

func (d *davRemote) Mkdir(ctx context.Context, rel string) error {
	resp, err := d.do(ctx, "MKCOL", rel, nil, -1, nil)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

func (d *davRemote) Put(ctx context.Context, rel string, body io.Reader, size int64) error {
	if body == nil {
		body = bytes.NewReader(nil)
		size = 0
	}
	resp, err := d.do(ctx, http.MethodPut, rel, body, size, map[string]string{"Content-Type": "application/octet-stream"})
	if err == nil {
		resp.Body.Close()
	}
	return err
}

func (d *davRemote) moveCopy(ctx context.Context, method, from, to string) error {
	resp, err := d.do(ctx, method, from, nil, -1, map[string]string{"Destination": d.href(to), "Overwrite": "F"})
	if err == nil {
		resp.Body.Close()
	}
	if errors.Is(err, files.ErrNotFound) {
		return files.ErrNotFound
	}
	return err
}

func (d *davRemote) Move(ctx context.Context, from, to string) error {
	return d.moveCopy(ctx, "MOVE", from, to)
}

func (d *davRemote) Copy(ctx context.Context, from, to string) error {
	return d.moveCopy(ctx, "COPY", from, to)
}

func (d *davRemote) Delete(ctx context.Context, rel string) error {
	resp, err := d.do(ctx, http.MethodDelete, rel, nil, -1, nil)
	if err == nil {
		resp.Body.Close()
	}
	return err
}
