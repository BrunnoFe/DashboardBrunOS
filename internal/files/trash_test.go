package files

// Lixeira trocada por atalho (auditoria de 19/11/2026): esvaziar não pode seguir o atalho e apagar a
// pasta de destino, e restaurar não pode trazer arquivo de fora.

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTrashEmptyRefusesLink(t *testing.T) {
	s, dir := newSvc(t)
	alvo := t.TempDir()
	put(t, filepath.Join(alvo, "a", "importante.txt"), "não apague")
	if err := os.Symlink(alvo, filepath.Join(dir, trashDirName)); err != nil {
		t.Fatal(err)
	}
	if err := s.TrashEmpty(""); !errors.Is(err, ErrBadTrash) {
		t.Fatalf("esvaziar uma lixeira que é atalho: esperava ErrBadTrash, veio %v", err)
	}
	if _, err := os.Stat(filepath.Join(alvo, "a", "importante.txt")); err != nil {
		t.Fatal("a pasta para onde o atalho aponta foi apagada")
	}
	if items := s.TrashList(); len(items) != 0 {
		t.Fatalf("a lista não deveria mostrar itens de fora: %v", items)
	}
	put(t, filepath.Join(dir, "x.txt"), "x")
	if err := s.Trash("h", []string{"x.txt"}); !errors.Is(err, ErrBadTrash) {
		t.Fatalf("mandar para uma lixeira que é atalho: veio %v", err)
	}
}

func TestTrashRestoreRefusesLinkedItem(t *testing.T) {
	s, dir := newSvc(t)
	put(t, filepath.Join(dir, "doc.txt"), "doc")
	if err := s.Trash("h", []string{"doc.txt"}); err != nil {
		t.Fatal(err)
	}
	items := s.TrashList()
	if len(items) != 1 {
		t.Fatalf("esperava 1 item, veio %v", items)
	}
	// troca a pasta do item por um atalho para fora
	fora := t.TempDir()
	put(t, filepath.Join(fora, "doc.txt"), "segredo de fora")
	holder := filepath.Join(dir, trashDirName, items[0].ID)
	if err := os.RemoveAll(holder); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fora, holder); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TrashRestore("h", items[0].ID); !errors.Is(err, ErrBadTrash) {
		t.Fatalf("restaurar item que é atalho: esperava ErrBadTrash, veio %v", err)
	}
	if _, err := os.Stat(filepath.Join(fora, "doc.txt")); err != nil {
		t.Fatal("o arquivo de fora foi movido")
	}
}

func TestNoFakeTrashByPasteOrExtract(t *testing.T) {
	s, dir := newSvc(t)
	put(t, filepath.Join(dir, "baixados", trashDirName, "x.json"), "{}")
	if err := s.Paste("h", []string{"baixados/" + trashDirName}, "h", ".", false); !errors.Is(err, ErrBadName) {
		t.Fatalf("mover uma .brunos-trash para a raiz: esperava ErrBadName, veio %v", err)
	}
	f, err := os.Create(filepath.Join(dir, trashDirName+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("abc.json")
	w.Write([]byte("{}"))
	zw.Close()
	f.Close()
	if _, err := s.Extract("h", trashDirName+".zip"); !errors.Is(err, ErrBadName) {
		t.Fatalf("extrair .brunos-trash.zip: esperava ErrBadName, veio %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, trashDirName)); err == nil {
		t.Fatal("uma lixeira falsa foi criada na raiz")
	}
}
