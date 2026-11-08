package files

// Testes da etapa 0.6.0: editor, envio em pedaços, extrair, duplicados e mapa do espaço.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newSvc(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	s, errs := New(func() []RootConfig { return []RootConfig{{ID: "h", Name: "Home", Path: dir, Kind: "place"}} })
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return s, dir
}

func put(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEditText(t *testing.T) {
	s, dir := newSvc(t)
	put(t, filepath.Join(dir, "conf/app.yml"), "a: 1\n")
	os.Chmod(filepath.Join(dir, "conf/app.yml"), 0o640)

	tf, err := s.ReadText("h", "conf/app.yml")
	if err != nil || tf.Text != "a: 1\n" || tf.ReadOnly {
		t.Fatalf("ler: %+v %v", tf, err)
	}
	mt, err := s.SaveText("h", "conf/app.yml", strings.NewReader("a: 2\n"), tf.Mtime)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "conf/app.yml"))
	st, _ := os.Stat(filepath.Join(dir, "conf/app.yml"))
	if string(b) != "a: 2\n" || st.Mode().Perm() != 0o640 {
		t.Fatalf("salvou %q com %v", b, st.Mode().Perm())
	}
	// alguém salvou depois: o mtime antigo é recusado
	time.Sleep(10 * time.Millisecond)
	os.Chtimes(filepath.Join(dir, "conf/app.yml"), time.Now(), time.Now().Add(time.Second))
	if _, err := s.SaveText("h", "conf/app.yml", strings.NewReader("x"), mt); !errors.Is(err, ErrConflict) {
		t.Fatalf("esperava conflito, veio %v", err)
	}
	// sem permissão de escrita no arquivo: não troca por rename
	os.Chmod(filepath.Join(dir, "conf/app.yml"), 0o440)
	if _, err := s.SaveText("h", "conf/app.yml", strings.NewReader("x"), 0); err == nil {
		t.Fatal("arquivo protegido não pode ser trocado")
	}
	// link: recusa
	os.Symlink(filepath.Join(dir, "conf/app.yml"), filepath.Join(dir, "link.yml"))
	if _, err := s.SaveText("h", "link.yml", strings.NewReader("x"), 0); !errors.Is(err, ErrLink) {
		t.Fatalf("link deveria ser recusado: %v", err)
	}
	// binário e não-UTF-8
	put(t, filepath.Join(dir, "bin.dat"), "ab\x00cd")
	if _, err := s.ReadText("h", "bin.dat"); !errors.Is(err, ErrBinary) {
		t.Fatalf("binário: %v", err)
	}
	put(t, filepath.Join(dir, "latin1.txt"), "acentua\xe7\xe3o")
	if _, err := s.ReadText("h", "latin1.txt"); !errors.Is(err, ErrEncoding) {
		t.Fatalf("latin1: %v", err)
	}
	// novo arquivo
	if p, err := s.NewFile("h", "conf", "novo.md"); err != nil || p != "conf/novo.md" {
		t.Fatalf("novo: %q %v", p, err)
	}
	if _, err := s.NewFile("h", "conf", "novo.md"); !errors.Is(err, ErrExists) {
		t.Fatalf("repetido: %v", err)
	}
}

func TestResumableUpload(t *testing.T) {
	s, dir := newSvc(t)
	state := filepath.Join(t.TempDir(), "uploads.json")
	s.LoadUploads(state)
	data := bytes.Repeat([]byte("0123456789"), 1000) // 10 kB

	id, err := s.StartUpload("h", "", "Pasta/video.mp4", int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.UploadChunk(id, 0, bytes.NewReader(data[:4000])); err != nil || n != 4000 {
		t.Fatalf("pedaço 1: %d %v", n, err)
	}
	// pedaço repetido (a resposta se perdeu): diz a posição certa
	if n, err := s.UploadChunk(id, 0, bytes.NewReader(data[:4000])); !errors.Is(err, ErrOffset) || n != 4000 {
		t.Fatalf("esperava ErrOffset em 4000: %d %v", n, err)
	}
	// o BrunOS reinicia no meio: o estado volta do arquivo
	s2, _ := New(func() []RootConfig { return []RootConfig{{ID: "h", Name: "Home", Path: dir, Kind: "place"}} })
	s2.LoadUploads(state)
	if off, size, err := s2.UploadStatus(id); err != nil || off != 4000 || size != int64(len(data)) {
		t.Fatalf("status depois de reiniciar: %d %d %v", off, size, err)
	}
	if _, err := s2.FinishUpload(id); err == nil {
		t.Fatal("não pode concluir incompleto")
	}
	// passar do tamanho anunciado é recusado e descartado
	if _, err := s2.UploadChunk(id, 4000, bytes.NewReader(append(append([]byte{}, data[4000:]...), 'x'))); err == nil {
		t.Fatal("deveria recusar bytes a mais")
	}
	if off, _, _ := s2.UploadStatus(id); off != 4000 {
		t.Fatalf("pedaço com problema deveria ser descartado: %d", off)
	}
	if _, err := s2.UploadChunk(id, 4000, bytes.NewReader(data[4000:])); err != nil {
		t.Fatal(err)
	}
	p, err := s2.FinishUpload(id)
	if err != nil || p != "Pasta/video.mp4" {
		t.Fatalf("concluir: %q %v", p, err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "Pasta/video.mp4"))
	if !bytes.Equal(got, data) {
		t.Fatal("conteúdo diferente")
	}
	if _, _, err := s2.UploadStatus(id); !errors.Is(err, ErrUploadGone) {
		t.Fatal("depois de concluir o envio some")
	}
	// cancelar apaga o parcial
	id2, _ := s2.StartUpload("h", "", "x.bin", 10)
	s2.UploadChunk(id2, 0, strings.NewReader("12345"))
	if err := s2.CancelUpload(id2); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, partPrefix+"*")); len(m) != 0 {
		t.Fatalf("parcial ficou: %v", m)
	}
	if _, err := s2.UploadChunk("../../etc", 0, strings.NewReader("x")); !errors.Is(err, ErrUploadGone) {
		t.Fatal("id inválido")
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(c))
	}
	zw.Close()
	return buf.Bytes()
}

func TestExtract(t *testing.T) {
	s, dir := newSvc(t)
	put(t, filepath.Join(dir, "fotos.zip"), string(makeZip(t, map[string]string{"a/1.txt": "um", "b.txt": "dois", "a/": ""})))
	p, err := s.Extract("h", "fotos.zip")
	if err != nil || p != "fotos" {
		t.Fatalf("extrair: %q %v", p, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "fotos/a/1.txt")); string(b) != "um" {
		t.Fatal("conteúdo errado")
	}
	// de novo: pasta com nome único
	if p, _ := s.Extract("h", "fotos.zip"); p != "fotos (2)" {
		t.Fatalf("segunda extração: %q", p)
	}

	// zip-slip: recusa e não deixa nada para trás
	put(t, filepath.Join(dir, "mau.zip"), string(makeZip(t, map[string]string{"../../fora.txt": "x"})))
	if _, err := s.Extract("h", "mau.zip"); !errors.Is(err, ErrArchive) {
		t.Fatalf("zip-slip deveria ser recusado: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mau")); !os.IsNotExist(err) {
		t.Fatal("extração recusada deixou a pasta")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "fora.txt")); !os.IsNotExist(err) {
		t.Fatal("escreveu fora!")
	}

	// tar.gz com link simbólico (ignorado)
	var tb bytes.Buffer
	gz := gzip.NewWriter(&tb)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "docs/", Typeflag: tar.TypeDir, Mode: 0o755})
	tw.WriteHeader(&tar.Header{Name: "docs/x.md", Typeflag: tar.TypeReg, Mode: 0o644, Size: 2})
	tw.Write([]byte("oi"))
	tw.WriteHeader(&tar.Header{Name: "docs/senha", Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"})
	tw.Close()
	gz.Close()
	put(t, filepath.Join(dir, "docs.tar.gz"), tb.String())
	if p, err := s.Extract("h", "docs.tar.gz"); err != nil || p != "docs" {
		t.Fatalf("tar.gz: %q %v", p, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "docs/docs/x.md")); string(b) != "oi" {
		t.Fatal("tar.gz sem o arquivo")
	}
	if _, err := os.Lstat(filepath.Join(dir, "docs/docs/senha")); !os.IsNotExist(err) {
		t.Fatal("link simbólico não deveria ser extraído")
	}
	if _, err := s.Extract("h", "fotos/b.txt"); !errors.Is(err, ErrArchive) {
		t.Fatal("formato não suportado")
	}
}

func TestDuplicatesAndUsage(t *testing.T) {
	s, dir := newSvc(t)
	big := strings.Repeat("x", 200<<10) // maior que 2×64 KB: usa resumo + hash completo
	put(t, filepath.Join(dir, "a/filme.mkv"), big)
	put(t, filepath.Join(dir, "b/copia.mkv"), big)
	put(t, filepath.Join(dir, "b/outro.mkv"), big[:len(big)-1]+"y") // mesmo tamanho, conteúdo diferente
	put(t, filepath.Join(dir, "c/p1.txt"), "igual")
	put(t, filepath.Join(dir, "c/p2.txt"), "igual")

	res, err := s.Duplicates("h", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 2 || res.Groups[0].Size != int64(len(big)) || len(res.Groups[0].Files) != 2 {
		t.Fatalf("duplicados: %+v", res)
	}
	if res.Wasted != int64(len(big))+5 {
		t.Fatalf("espaço a liberar: %d", res.Wasted)
	}
	if res, _ := s.Duplicates("h", "", 1<<20); len(res.Groups) != 0 {
		t.Fatal("tamanho mínimo não respeitado")
	}

	u, err := s.Usage("h", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if u.Size != int64(3*len(big)+10) || u.Count != 5 || u.Items[0].Name != "b" {
		t.Fatalf("uso: %+v", u)
	}
	// subpasta responde do cache
	sub, err := s.Usage("h", "b", false)
	if err != nil || sub.Size != int64(2*len(big)) || len(sub.Items) != 2 || sub.ScannedAt != u.ScannedAt {
		t.Fatalf("subpasta: %+v %v", sub, err)
	}
}
