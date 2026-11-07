package files

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"
	"time"

	"brunos/internal/store"
)

// Cada raiz tem sua própria lixeira em <raiz>/.brunos-trash, no mesmo disco,
// então mandar algo para a lixeira é só um rename (instantâneo, mesmo para pastas enormes).

type TrashItem struct {
	Root      string    `json:"root"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"` // onde estava, relativo à raiz
	DeletedAt time.Time `json:"deletedAt"`
	Dir       bool      `json:"dir"`
	Size      int64     `json:"size"`
}

var trashIDRe = regexp.MustCompile(`^[a-f0-9]{16}$`)

func newTrashID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (r *Root) trashDir() string { return filepath.Join(r.real, trashDirName) }

// Trash move os itens para a lixeira da raiz.
func (s *Service) Trash(rootID string, rels []string) error {
	r, err := s.root(rootID)
	if err != nil {
		return err
	}
	if err := r.writable(); err != nil {
		return err
	}
	if err := os.MkdirAll(r.trashDir(), 0o700); err != nil {
		return mapErr(err)
	}
	for _, rel := range rels {
		rel, err := clean(rel)
		if err != nil || rel == "." {
			return ErrBadPath
		}
		src, err := r.realPath(rel)
		if err != nil {
			return err
		}
		st, err := os.Lstat(src)
		if err != nil {
			return mapErr(err)
		}
		id := newTrashID()
		holder := filepath.Join(r.trashDir(), id)
		if err := os.Mkdir(holder, 0o700); err != nil {
			return mapErr(err)
		}
		item := TrashItem{ID: id, Name: filepath.Base(src), Path: rel, DeletedAt: time.Now().UTC(), Dir: st.IsDir()}
		if !item.Dir {
			item.Size = st.Size()
		}
		if err := os.Rename(src, filepath.Join(holder, item.Name)); err != nil {
			os.Remove(holder)
			if errors.Is(err, os.ErrInvalid) || isCrossDevice(err) {
				return errors.New("este item está em outro disco montado dentro da pasta; exclua permanentemente")
			}
			return mapErr(err)
		}
		if err := store.WriteJSON(holder+".json", item, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }

func (s *Service) TrashList() []TrashItem {
	items := []TrashItem{}
	for _, r := range s.Roots() {
		matches, _ := filepath.Glob(filepath.Join(r.trashDir(), "*.json"))
		for _, m := range matches {
			var it TrashItem
			if ok, err := store.ReadJSON(m, &it); !ok || err != nil || !trashIDRe.MatchString(it.ID) {
				continue
			}
			it.Root = r.ID
			items = append(items, it)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DeletedAt.After(items[j].DeletedAt) })
	return items
}

func (s *Service) trashItem(rootID, id string) (*Root, TrashItem, error) {
	r, err := s.root(rootID)
	if err != nil {
		return nil, TrashItem{}, err
	}
	if !trashIDRe.MatchString(id) {
		return nil, TrashItem{}, ErrBadPath
	}
	var it TrashItem
	ok, err := store.ReadJSON(filepath.Join(r.trashDir(), id+".json"), &it)
	if err != nil || !ok {
		return nil, TrashItem{}, ErrNotFound
	}
	if validName(it.Name) != nil {
		return nil, TrashItem{}, ErrBadName
	}
	return r, it, nil
}

// TrashRestore devolve o item para onde estava (ou para a raiz, se a pasta sumiu).
func (s *Service) TrashRestore(rootID, id string) (string, error) {
	r, it, err := s.trashItem(rootID, id)
	if err != nil {
		return "", err
	}
	if err := r.writable(); err != nil {
		return "", err
	}
	dirRel, err := clean(path.Dir(it.Path))
	if err != nil {
		dirRel = "."
	}
	dir, err := r.realDir(dirRel)
	if err != nil {
		dir, dirRel = r.real, "." // a pasta original sumiu (ou virou link para fora): volta para a raiz
	}
	name := uniqueName(dir, it.Name)
	holder := filepath.Join(r.trashDir(), id)
	if err := os.Rename(filepath.Join(holder, it.Name), filepath.Join(dir, name)); err != nil {
		return "", mapErr(err)
	}
	os.Remove(holder)
	os.Remove(holder + ".json")
	return path.Join(dirRel, name), nil
}

func (s *Service) TrashDelete(rootID, id string) error {
	r, _, err := s.trashItem(rootID, id)
	if err != nil {
		return err
	}
	if err := r.writable(); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(r.trashDir(), id)); err != nil {
		return mapErr(err)
	}
	if err := os.Remove(filepath.Join(r.trashDir(), id+".json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return mapErr(err)
	}
	return nil
}

// TrashEmpty esvazia a lixeira de uma raiz (ou de todas, se rootID == "").
func (s *Service) TrashEmpty(rootID string) error {
	for _, r := range s.Roots() {
		if rootID != "" && r.ID != rootID {
			continue
		}
		if r.ReadOnly {
			continue
		}
		entries, err := os.ReadDir(r.trashDir())
		if err != nil {
			continue
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(r.trashDir(), e.Name())); err != nil {
				return mapErr(err)
			}
		}
	}
	return nil
}
