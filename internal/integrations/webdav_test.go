package integrations

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"brunos/internal/files"
)

// fakeDAV é um Nextcloud de mentira: arquivos e pastas em memória, com a senha de app conferida.
type fakeDAV struct {
	mu      sync.Mutex
	files   map[string][]byte // caminho → conteúdo
	dirs    map[string]bool
	trashed []string
}

const davPrefix = "/remote.php/dav/files/bruno"

func newFakeDAV() *fakeDAV {
	return &fakeDAV{files: map[string][]byte{}, dirs: map[string]bool{"": true}}
}

func parentOf(p string) string {
	d := path.Dir("/" + p)
	return strings.TrimPrefix(d, "/")
}

func (f *fakeDAV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if u, p, ok := r.BasicAuth(); !ok || u != "bruno" || p != "senha-app" {
		w.WriteHeader(401)
		return
	}
	if !strings.HasPrefix(r.URL.Path, davPrefix) {
		w.WriteHeader(404)
		return
	}
	p := strings.Trim(strings.TrimPrefix(r.URL.Path, davPrefix), "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case "PROPFIND":
		if !f.dirs[p] && f.files[p] == nil {
			w.WriteHeader(404)
			return
		}
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
		item := func(q string) {
			href := davPrefix + "/" + (&url.URL{Path: q}).EscapedPath()
			if f.dirs[q] {
				fmt.Fprintf(&b, `<d:response><d:href>%s/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype><d:getlastmodified>Wed, 25 Nov 2026 10:00:00 GMT</d:getlastmodified></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, strings.TrimRight(href, "/"))
			} else {
				fmt.Fprintf(&b, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>%d</d:getcontentlength><d:getlastmodified>Wed, 25 Nov 2026 10:00:00 GMT</d:getlastmodified></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, href, len(f.files[q]))
			}
		}
		item(p)
		if r.Header.Get("Depth") == "1" && f.dirs[p] {
			var kids []string
			for k := range f.dirs {
				if k != "" && parentOf(k) == p {
					kids = append(kids, k)
				}
			}
			for k := range f.files {
				if parentOf(k) == p {
					kids = append(kids, k)
				}
			}
			sort.Strings(kids)
			for _, k := range kids {
				item(k)
			}
		}
		b.WriteString(`</d:multistatus>`)
		w.WriteHeader(207)
		_, _ = io.WriteString(w, b.String())
	case http.MethodGet:
		data, ok := f.files[p]
		if !ok {
			w.WriteHeader(404)
			return
		}
		var a, z int
		if n, _ := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &a, &z); n == 2 {
			if z >= len(data) {
				z = len(data) - 1
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, z, len(data)))
			w.WriteHeader(206)
			_, _ = w.Write(data[a : z+1])
			return
		}
		_, _ = w.Write(data)
	case http.MethodPut:
		if !f.dirs[parentOf(p)] {
			w.WriteHeader(409)
			return
		}
		data, _ := io.ReadAll(r.Body)
		f.files[p] = data
		w.WriteHeader(201)
	case "MKCOL":
		if f.dirs[p] || f.files[p] != nil {
			w.WriteHeader(405)
			return
		}
		f.dirs[p] = true
		w.WriteHeader(201)
	case "MOVE", "COPY":
		d, _ := url.Parse(r.Header.Get("Destination"))
		to := strings.Trim(strings.TrimPrefix(d.Path, davPrefix), "/")
		if f.dirs[to] || f.files[to] != nil {
			w.WriteHeader(412)
			return
		}
		switch {
		case f.files[p] != nil:
			f.files[to] = f.files[p]
			if r.Method == "MOVE" {
				delete(f.files, p)
			}
		case f.dirs[p]: // pasta: leva junto o que está dentro
			for k, v := range f.files {
				if strings.HasPrefix(k, p+"/") {
					f.files[to+strings.TrimPrefix(k, p)] = v
					if r.Method == "MOVE" {
						delete(f.files, k)
					}
				}
			}
			for k := range f.dirs {
				if k == p || strings.HasPrefix(k, p+"/") {
					f.dirs[to+strings.TrimPrefix(k, p)] = true
					if r.Method == "MOVE" {
						delete(f.dirs, k)
					}
				}
			}
		default:
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(201)
	case http.MethodDelete:
		f.trashed = append(f.trashed, p)
		for k := range f.files {
			if k == p || strings.HasPrefix(k, p+"/") {
				delete(f.files, k)
			}
		}
		for k := range f.dirs {
			if k == p || strings.HasPrefix(k, p+"/") {
				delete(f.dirs, k)
			}
		}
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func TestWebDAVRemote(t *testing.T) {
	fd := newFakeDAV()
	fd.dirs["Fotos"], fd.files["Fotos/praia.jpg"], fd.files["nota com espaço.txt"] = true, []byte("JPEGDATA"), []byte("olá")
	srv := httptest.NewServer(fd)
	defer srv.Close()
	d := &davRemote{c: srv.Client(), base: srv.URL + davPrefix, user: "bruno", pass: "senha-app"}
	ctx := context.Background()

	list, err := d.List(ctx, ".")
	if err != nil || len(list) != 2 {
		t.Fatalf("raiz: %+v %v", list, err)
	}
	if list[0].Name != "Fotos" || !list[0].Dir || list[1].Name != "nota com espaço.txt" || list[1].Size != 4 || list[1].Mtime == 0 {
		t.Fatalf("itens: %+v", list)
	}
	resp, err := d.Get(ctx, "Fotos/praia.jpg", "bytes=2-4")
	if err != nil || resp.StatusCode != 206 {
		t.Fatalf("range: %v %v", resp, err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "EGD" {
		t.Fatalf("pedaço = %q", b)
	}
	if err := d.Mkdir(ctx, "Fotos"); err != files.ErrExists {
		t.Fatalf("pasta repetida: %v", err)
	}
	if err := d.Move(ctx, "nota com espaço.txt", "Fotos/nota com espaço.txt"); err != nil {
		t.Fatal(err)
	}
	if err := d.Copy(ctx, "Fotos/praia.jpg", "Fotos/nota com espaço.txt"); err != files.ErrExists {
		t.Fatalf("copiar por cima: %v", err)
	}
	if err := d.Delete(ctx, "Fotos/praia.jpg"); err != nil || len(fd.trashed) != 1 {
		t.Fatalf("apagar: %v %v", err, fd.trashed)
	}
	bad := &davRemote{c: srv.Client(), base: srv.URL + davPrefix, user: "bruno", pass: "errada"}
	if _, err := bad.List(ctx, "."); err == nil || !strings.Contains(err.Error(), "senha de app") {
		t.Fatalf("senha errada: %v", err)
	}
}

// O caminho todo pela janela Arquivos: disco → Nextcloud, renomear, Nextcloud → disco, apagar.
func TestWebDAVThroughFiles(t *testing.T) {
	fd := newFakeDAV()
	srv := httptest.NewServer(fd)
	defer srv.Close()
	disk := t.TempDir()
	if err := os.MkdirAll(filepath.Join(disk, "Album/sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(disk, "Album/a.txt"), []byte("AAA"), 0o644)
	_ = os.WriteFile(filepath.Join(disk, "Album/sub/b.txt"), []byte("BB"), 0o644)
	s, errs := files.New(func() []files.RootConfig {
		return []files.RootConfig{{ID: "d1", Name: "Disco1", Path: disk, Kind: "place"}}
	})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	s.MaxUpload = 1 << 30
	nc := &davRemote{c: srv.Client(), base: srv.URL + davPrefix, user: "bruno", pass: "senha-app"}
	s.Remotes = func() []files.RemoteRoot { return []files.RemoteRoot{{ID: "nc-x", Name: "Nextcloud", R: nc}} }

	infos := s.RootInfos()
	if last := infos[len(infos)-1]; last.ID != "nc-x" || last.Kind != "remote" {
		t.Fatalf("lugar remoto na lista: %+v", infos)
	}
	// disco → Nextcloud (cópia de uma pasta inteira)
	if err := s.Paste("d1", []string{"Album"}, "nc-x", "", true); err != nil {
		t.Fatal(err)
	}
	if string(fd.files["Album/a.txt"]) != "AAA" || string(fd.files["Album/sub/b.txt"]) != "BB" || !fd.dirs["Album/sub"] {
		t.Fatalf("no Nextcloud: %v %v", fd.files, fd.dirs)
	}
	// renomear e criar pasta no Nextcloud
	if err := s.Rename("nc-x", "Album/a.txt", "c.txt"); err != nil || string(fd.files["Album/c.txt"]) != "AAA" {
		t.Fatalf("renomear: %v %v", err, fd.files)
	}
	if _, err := s.Mkdir("nc-x", "", "Novo"); err != nil || !fd.dirs["Novo"] {
		t.Fatalf("pasta: %v", err)
	}
	// Nextcloud → disco, movendo (sai do Nextcloud)
	if _, err := s.Mkdir("d1", "", "Volta"); err != nil {
		t.Fatal(err)
	}
	if err := s.Paste("nc-x", []string{"Album"}, "d1", "Volta", false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(disk, "Volta/Album/sub/b.txt")); string(b) != "BB" {
		t.Fatalf("no disco: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(disk, "Volta/Album/c.txt")); string(b) != "AAA" {
		t.Fatalf("no disco: %q", b)
	}
	if fd.dirs["Album"] {
		t.Fatal("mover deveria tirar do Nextcloud")
	}
	// apagar vai para a lixeira do Nextcloud
	if err := s.Trash("nc-x", []string{"Novo"}); err != nil || fd.dirs["Novo"] {
		t.Fatalf("lixeira: %v", err)
	}
	// o que ainda não funciona responde com mensagem clara
	if _, err := s.StartUpload("nc-x", "", "x.bin", 10); err != files.ErrRemoteUnsupported {
		t.Fatalf("envio em pedaços: %v", err)
	}
}

func TestFileRemotesNeedsBoth(t *testing.T) {
	st := NewStore("", testBox(t))
	tok, pass := "tok", "senha-app"
	if _, err := st.Add(Input{Kind: "nextcloud", URL: "http://10.0.0.5:8083", Secret: &tok}); err != nil {
		t.Fatal(err)
	}
	it, err := st.Add(Input{Kind: "nextcloud", Name: "Nuvem", URL: "http://10.0.0.5:8083", Secret: &tok, Secret2: &pass, Options: map[string]string{"files_user": "bruno"}})
	if err != nil {
		t.Fatal(err)
	}
	if it.Secret2 == "" || it.Secret2 == pass || !it.Public().HasSecret2 {
		t.Fatalf("senha de app deveria ficar cifrada: %+v", it)
	}
	h := NewHub(st, NewSeriesStore(""), nil)
	r := h.FileRemotes()
	if len(r) != 1 || r[0].ID != "nc-"+it.ID || r[0].Name != "Nuvem" {
		t.Fatalf("lugares: %+v", r)
	}
	if d := r[0].R.(*davRemote); d.base != "http://10.0.0.5:8083/remote.php/dav/files/bruno" || d.pass != pass {
		t.Fatalf("endereço: %+v", d)
	}
	// trocar o endereço sem digitar a senha de app de novo: recusa (ela vale só para o endereço dela)
	if _, err := st.Update(it.ID, Input{URL: "http://10.0.0.9:8083", Secret: &tok, Options: map[string]string{"files_user": "bruno"}}); err == nil {
		t.Fatal("deveria pedir a senha de app de novo")
	}
	// outro Tipo nunca guarda a segunda credencial
	x, _ := st.Add(Input{Kind: "speedtest", URL: "http://10.0.0.5:8765", Secret: &tok, Secret2: &pass})
	if x.Secret2 != "" {
		t.Fatal("speedtest não tem senha de app")
	}
}
