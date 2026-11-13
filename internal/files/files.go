// Package files é o gerenciador de arquivos do BrunOS.
//
// Modelo de segurança (em camadas):
//  1. Docker: o container só enxerga o que o docker-compose monta em /files
//     (ex.: /home e /mnt). O resto do servidor simplesmente não existe aqui dentro.
//  2. Raízes: cada pasta permitida vira uma "raiz". Toda operação recebe (raiz, caminho relativo).
//  3. Leituras usam os.Root (Go 1.24): o kernel resolve o caminho sem nunca sair da raiz,
//     nem por "..", nem por links simbólicos que apontem para fora.
//  4. Escritas (renomear, mover, apagar) resolvem o diretório pai com EvalSymlinks e
//     conferem que ele continua dentro da raiz antes de agir.
//  5. Raízes ou montagens somente leitura recusam qualquer escrita.
//  6. O container roda como o seu usuário (PUID/PGID), então só mexe no que ele já poderia mexer.
package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"brunos/internal/store"
)

const trashDirName = ".brunos-trash"

var (
	ErrNotFound    = errors.New("arquivo ou pasta não encontrado")
	ErrReadOnly    = errors.New("esta pasta é somente leitura")
	ErrBadName     = errors.New("nome inválido")
	ErrBadPath     = errors.New("caminho inválido")
	ErrExists      = errors.New("já existe um item com esse nome")
	ErrIntoItself  = errors.New("não dá para mover uma pasta para dentro dela mesma")
	ErrUnknownRoot = errors.New("local desconhecido")
)

type RootConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"-"`
	ReadOnly bool   `json:"readOnly"`
	Kind     string `json:"kind"` // "place" (Home etc.) ou "disk" (pastas dentro de /mnt e /media)
}

type Root struct {
	RootConfig
	real string
	r    *os.Root
	dev  uint64 // dispositivo da pasta quando a raiz foi aberta
}

// Service guarda todas as raízes descobertas. Discos (Kind "disk") só ficam acessíveis
// quando mapeados pelo usuário; os mapeamentos ficam em MapFile (/data/disks.json).
type Service struct {
	mu        sync.RWMutex
	all       []*Root
	roots     []*Root // raízes ativas: lugares + discos mapeados
	byID      map[string]*Root
	mapped    map[string]bool // nomes dos discos mapeados; nil = nunca configurado
	cfgs      func() []RootConfig
	MaxUpload int64
	MapFile   string
	up        uploads         // envios em pedaços pendentes (resumable.go)
	usage     usageCache      // mapa do espaço (usage.go)
	released  map[string]bool // discos soltos para desmontar (0.16.0): não reabre até o BrunOS reiniciar
}

// Release fecha a pasta de um disco (pelo nome) e não reabre mais até o BrunOS reiniciar. Antes de
// desmontar: com a pasta aberta, o próprio BrunOS apareceria como "programa usando o disco".
func (s *Service) Release(name string) {
	s.mu.Lock()
	if s.released == nil {
		s.released = map[string]bool{}
	}
	s.released[name] = true
	keep := make([]*Root, 0, len(s.all))
	for _, r := range s.all {
		if r.Kind == "disk" && r.Name == name {
			_ = r.r.Close()
			continue
		}
		keep = append(keep, r)
	}
	s.all = keep
	s.mu.Unlock()
	s.apply()
}

// Unrelease deixa a pasta abrir de novo (a desmontagem foi recusada).
func (s *Service) Unrelease(name string) {
	s.mu.Lock()
	delete(s.released, name)
	s.mu.Unlock()
	s.Refresh()
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		s = "pasta"
	}
	return s
}

// ParseRoots lê BRUNOS_ROOTS ("Nome=/caminho[:ro];Outro=/caminho") ou, se vazio,
// descobre sozinho: cada pasta dentro de base vira uma raiz; dentro de "mnt" e
// "media", cada disco montado vira uma raiz própria.
func ParseRoots(spec, base string) []RootConfig {
	var out []RootConfig
	seen := map[string]int{}
	addCfg := func(name, p string, ro bool) {
		id := slug(name)
		seen[id]++
		if seen[id] > 1 {
			id = fmt.Sprintf("%s-%d", id, seen[id])
		}
		out = append(out, RootConfig{ID: id, Name: name, Path: p, ReadOnly: ro})
	}
	if strings.TrimSpace(spec) != "" {
		for _, item := range strings.FieldsFunc(spec, func(r rune) bool { return r == ';' || r == '\n' }) {
			name, p, ok := strings.Cut(strings.TrimSpace(item), "=")
			if !ok {
				continue
			}
			ro := false
			if strings.HasSuffix(p, ":ro") {
				p, ro = strings.TrimSuffix(p, ":ro"), true
			} else if strings.HasSuffix(p, ":rw") {
				p = strings.TrimSuffix(p, ":rw")
			}
			addCfg(strings.TrimSpace(name), strings.TrimSpace(p), ro)
		}
		for i := range out {
			out[i].Kind = "place"
		}
		return out
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(base, e.Name())
		if e.Name() == "mnt" || e.Name() == "media" {
			subs, _ := os.ReadDir(p)
			for _, s := range subs {
				if s.IsDir() && !strings.HasPrefix(s.Name(), ".") {
					addCfg(s.Name(), filepath.Join(p, s.Name()), false)
					out[len(out)-1].Kind = "disk"
				}
			}
			continue
		}
		name := e.Name()
		if name == "home" {
			name = "Home"
		}
		addCfg(name, p, false)
		out[len(out)-1].Kind = "place"
	}
	return out
}

// New abre as raízes. discover é chamado de novo em Refresh para achar pastas novas.
func New(discover func() []RootConfig) (*Service, []error) {
	s := &Service{byID: map[string]*Root{}, MaxUpload: 50 << 30, cfgs: discover}
	errs := s.open(discover())
	s.apply()
	return s, errs
}

func openRoot(c RootConfig) (*Root, error) {
	real, err := filepath.EvalSymlinks(c.Path)
	if err != nil {
		return nil, fmt.Errorf("raiz %q (%s): %w", c.Name, c.Path, err)
	}
	r, err := os.OpenRoot(real)
	if err != nil {
		return nil, fmt.Errorf("raiz %q: %w", c.Name, err)
	}
	if !c.ReadOnly && mountReadOnly(real) {
		c.ReadOnly = true
	}
	return &Root{RootConfig: c, real: real, r: r, dev: devOf(real)}, nil
}

// open acrescenta as raízes que ainda não existem (pelo caminho) e reabre as que
// mudaram de dispositivo (um disco montado por cima de uma pasta vazia, por exemplo).
func (s *Service) open(cfgs []RootConfig) []error {
	var errs []error
	s.mu.Lock()
	defer s.mu.Unlock()
	byPath := map[string]int{}
	for i, r := range s.all {
		byPath[r.Path] = i
	}
	for _, c := range cfgs {
		if c.Kind == "disk" && s.released[c.Name] {
			continue // solto para desmontar
		}
		if i, ok := byPath[c.Path]; ok {
			old := s.all[i]
			if d := devOf(old.real); d != 0 && d != old.dev {
				if nr, err := openRoot(old.RootConfig); err == nil {
					_ = old.r.Close()
					s.all[i] = nr
				}
			}
			continue
		}
		used := false
		for _, r := range s.all {
			used = used || r.ID == c.ID
		}
		if used {
			c.ID = fmt.Sprintf("%s-%d", c.ID, len(s.all)+1)
		}
		root, err := openRoot(c)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		s.all = append(s.all, root)
	}
	return errs
}

// apply recalcula as raízes ativas a partir dos mapeamentos. Chamar com s.mu livre.
func (s *Service) apply() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roots = s.roots[:0:0]
	s.byID = map[string]*Root{}
	for _, r := range s.all {
		if r.Kind == "disk" {
			if s.mapped == nil && !isMountPoint(r.real) {
				continue // sem configuração: mostra só o que tem disco montado
			}
			if s.mapped != nil && !s.mapped[r.Name] {
				continue
			}
		}
		s.roots = append(s.roots, r)
		s.byID[r.ID] = r
	}
}

func devOf(p string) uint64 {
	var st syscall.Stat_t
	if syscall.Stat(p, &st) != nil {
		return 0
	}
	return uint64(st.Dev)
}

// isMountPoint diz se a pasta é a raiz de outro sistema de arquivos (um disco montado),
// comparando o dispositivo dela com o da pasta pai.
func isMountPoint(p string) bool {
	d, parent := devOf(p), devOf(filepath.Dir(p))
	return d != 0 && parent != 0 && d != parent
}

// ── Discos mapeados ──────────────────────────────────────────────

type mapFile struct {
	Version int      `json:"version"`
	Mapped  []string `json:"mapped"`
}

// LoadMapping lê MapFile. Sem arquivo, valem os discos que têm algo montado.
func (s *Service) LoadMapping() error {
	if s.MapFile == "" {
		return nil
	}
	var m mapFile
	ok, err := store.ReadJSON(s.MapFile, &m)
	if err != nil || !ok {
		return err
	}
	s.mu.Lock()
	s.mapped = map[string]bool{}
	for _, n := range m.Mapped {
		s.mapped[n] = true
	}
	s.mu.Unlock()
	s.apply()
	return nil
}

type DiskInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Mounted bool   `json:"mounted"`
	Mapped  bool   `json:"mapped"`
	Total   uint64 `json:"total"`
	Free    uint64 `json:"free"`
}

// Refresh procura pastas novas em /mnt e /media e recalcula as raízes ativas.
func (s *Service) Refresh() {
	if s.cfgs != nil {
		s.open(s.cfgs())
	}
	s.apply()
}

// Disks lista todas as pastas candidatas a disco, mapeadas ou não.
func (s *Service) Disks() []DiskInfo {
	s.Refresh()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []DiskInfo{}
	for _, r := range s.all {
		if r.Kind != "disk" {
			continue
		}
		d := DiskInfo{ID: r.ID, Name: r.Name, Mounted: isMountPoint(r.real)}
		_, d.Mapped = s.byID[r.ID]
		if d.Mounted {
			var st syscall.Statfs_t
			if syscall.Statfs(r.real, &st) == nil {
				d.Total, d.Free = st.Blocks*uint64(st.Bsize), st.Bavail*uint64(st.Bsize)
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// SetMapped grava a lista de discos mapeados (por nome da pasta) e aplica na hora.
func (s *Service) SetMapped(names []string) error {
	s.Refresh()
	s.mu.Lock()
	valid := map[string]bool{}
	for _, r := range s.all {
		if r.Kind == "disk" {
			valid[r.Name] = true
		}
	}
	m := map[string]bool{}
	list := []string{}
	for _, n := range names {
		if valid[n] && !m[n] {
			m[n] = true
			list = append(list, n)
		}
	}
	s.mapped = m
	s.mu.Unlock()
	s.apply()
	if s.MapFile == "" {
		return nil
	}
	return store.WriteJSON(s.MapFile, mapFile{Version: 1, Mapped: list}, 0o600)
}

func mountReadOnly(p string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return false
	}
	return st.Flags&0x1 != 0 // ST_RDONLY
}

func (s *Service) Roots() []*Root {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*Root(nil), s.roots...)
}

func (s *Service) root(id string) (*Root, error) {
	s.mu.RLock()
	r, ok := s.byID[id]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownRoot
	}
	return r, nil
}

// clean transforma o caminho pedido em um caminho relativo seguro ("." = raiz).
func clean(p string) (string, error) {
	if strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", ErrBadPath
	}
	c := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(p, `\`, "/")), "/")
	if c == "" {
		return ".", nil
	}
	if c == trashDirName || strings.HasPrefix(c, trashDirName+"/") {
		return "", ErrBadPath // a lixeira só é acessada pelas rotas dela
	}
	return c, nil
}

func validName(n string) error {
	if n == "" || n == "." || n == ".." || len(n) > 255 || strings.ContainsAny(n, "/\\\x00") || n == trashDirName {
		return ErrBadName
	}
	return nil
}

// real resolve com segurança o caminho real de rel para operações de escrita.
// O diretório pai é resolvido (seguindo links) e precisa continuar dentro da raiz;
// o último componente não é seguido, então operações agem sobre o próprio link.
func (r *Root) realPath(rel string) (string, error) {
	if rel == "." {
		return r.real, nil
	}
	abs := filepath.Join(r.real, filepath.FromSlash(rel))
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	if !within(r.real, parent) {
		return "", ErrBadPath
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

// realDir resolve TODO o caminho (inclusive o último componente) e exige que o
// resultado continue dentro da raiz. Usado quando o caminho é uma pasta de destino
// (upload, colar, restaurar): um link para fora nunca pode virar destino de escrita.
func (r *Root) realDir(rel string) (string, error) {
	if rel == "." {
		return r.real, nil
	}
	p, err := filepath.EvalSymlinks(filepath.Join(r.real, filepath.FromSlash(rel)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	if !within(r.real, p) {
		return "", ErrBadPath
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", mapErr(err)
	}
	if !st.IsDir() {
		return "", ErrBadPath
	}
	return p, nil
}

func within(root, p string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, strings.TrimSuffix(root, string(os.PathSeparator))+string(os.PathSeparator))
}

func (r *Root) writable() error {
	if r.ReadOnly {
		return ErrReadOnly
	}
	return nil
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, fs.ErrExist):
		return ErrExists
	case errors.Is(err, syscall.EROFS):
		return ErrReadOnly
	case errors.Is(err, fs.ErrPermission):
		return errors.New("sem permissão para esta pasta")
	}
	var pe *fs.PathError
	if errors.As(err, &pe) && strings.Contains(pe.Err.Error(), "escapes from parent") {
		return ErrBadPath
	}
	return err
}

// ── Listagem ──────────────────────────────────────────────────────

type Entry struct {
	Name   string `json:"name"`
	Dir    bool   `json:"dir"`
	Size   int64  `json:"size"`
	Mtime  int64  `json:"mtime"`
	Link   bool   `json:"link,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

func (s *Service) List(rootID, rel string) ([]Entry, error) {
	r, err := s.root(rootID)
	if err != nil {
		return nil, err
	}
	rel, err = clean(rel)
	if err != nil {
		return nil, err
	}
	f, err := r.r.Open(rel)
	if err != nil {
		return nil, mapErr(err)
	}
	defer f.Close()
	des, err := f.ReadDir(-1)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		name := de.Name()
		if rel == "." && name == trashDirName {
			continue
		}
		e := Entry{Name: name, Hidden: strings.HasPrefix(name, ".")}
		info, err := de.Info()
		if err != nil {
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			e.Link = true
			// segue o link só se ele continuar dentro da raiz
			if st, err := r.r.Stat(path.Join(rel, name)); err == nil {
				info = st
			}
		}
		e.Dir = info.IsDir()
		if !e.Dir {
			e.Size = info.Size()
		}
		e.Mtime = info.ModTime().Unix()
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Open abre um arquivo para leitura dentro da raiz.
func (s *Service) Open(rootID, rel string) (*os.File, fs.FileInfo, error) {
	r, err := s.root(rootID)
	if err != nil {
		return nil, nil, err
	}
	rel, err = clean(rel)
	if err != nil {
		return nil, nil, err
	}
	f, err := r.r.Open(rel)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, mapErr(err)
	}
	return f, st, nil
}

// ── Escrita ───────────────────────────────────────────────────────

func (s *Service) Mkdir(rootID, rel, name string) (string, error) {
	r, err := s.root(rootID)
	if err != nil {
		return "", err
	}
	if err := r.writable(); err != nil {
		return "", err
	}
	if err := validName(name); err != nil {
		return "", err
	}
	rel, err = clean(rel)
	if err != nil {
		return "", err
	}
	target := path.Join(rel, name)
	return target, mapErr(r.r.Mkdir(target, 0o755))
}

func (s *Service) Rename(rootID, rel, newName string) error {
	r, err := s.root(rootID)
	if err != nil {
		return err
	}
	if err := r.writable(); err != nil {
		return err
	}
	if err := validName(newName); err != nil {
		return err
	}
	rel, err = clean(rel)
	if err != nil || rel == "." {
		return ErrBadPath
	}
	src, err := r.realPath(rel)
	if err != nil {
		return err
	}
	dst := filepath.Join(filepath.Dir(src), newName)
	if _, err := os.Lstat(dst); err == nil && !strings.EqualFold(src, dst) {
		return ErrExists
	}
	return mapErr(os.Rename(src, dst))
}

// uniqueName devolve "nome (2).ext" se nome já existir em dir.
func uniqueName(dir, name string) string {
	if _, err := os.Lstat(filepath.Join(dir, name)); errors.Is(err, fs.ErrNotExist) {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; i < 10000; i++ {
		n := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Lstat(filepath.Join(dir, n)); errors.Is(err, fs.ErrNotExist) {
			return n
		}
	}
	return fmt.Sprintf("%s-%d%s", base, time.Now().UnixNano(), ext)
}

// Paste move ou copia itens de uma pasta para outra (inclusive entre discos).
func (s *Service) Paste(srcRoot string, rels []string, dstRoot, dstRel string, copyMode bool) error {
	sr, err := s.root(srcRoot)
	if err != nil {
		return err
	}
	dr, err := s.root(dstRoot)
	if err != nil {
		return err
	}
	if err := dr.writable(); err != nil {
		return err
	}
	if !copyMode {
		if err := sr.writable(); err != nil {
			return err
		}
	}
	dstRel, err = clean(dstRel)
	if err != nil {
		return err
	}
	dstDir, err := dr.realDir(dstRel)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		rel, err := clean(rel)
		if err != nil || rel == "." {
			return ErrBadPath
		}
		src, err := sr.realPath(rel)
		if err != nil {
			return err
		}
		if within(src, dstDir) {
			return ErrIntoItself
		}
		if !copyMode && filepath.Dir(src) == dstDir {
			continue // mover para a mesma pasta: nada a fazer
		}
		dst := filepath.Join(dstDir, uniqueName(dstDir, filepath.Base(src)))
		if copyMode {
			if err := copyTree(src, dst); err != nil {
				return mapErr(err)
			}
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			if !errors.Is(err, syscall.EXDEV) {
				return mapErr(err)
			}
			// discos diferentes: copia e depois apaga a origem
			if err := copyTree(src, dst); err != nil {
				_ = os.RemoveAll(dst)
				return mapErr(err)
			}
			if err := os.RemoveAll(src); err != nil {
				return mapErr(err)
			}
		}
	}
	return nil
}

// copyTree copia arquivos e pastas; links simbólicos são ignorados por segurança.
func copyTree(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case st.Mode()&fs.ModeSymlink != 0:
		return nil
	case st.IsDir():
		if err := os.Mkdir(dst, st.Mode().Perm()|0o700); err != nil {
			return err
		}
		des, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, de := range des {
			if err := copyTree(filepath.Join(src, de.Name()), filepath.Join(dst, de.Name())); err != nil {
				return err
			}
		}
		return os.Chtimes(dst, st.ModTime(), st.ModTime())
	case st.Mode().IsRegular():
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			os.Remove(dst)
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Chtimes(dst, st.ModTime(), st.ModTime())
	}
	return nil // ignora dispositivos, sockets etc.
}

// Delete apaga de vez (sem lixeira).
func (s *Service) Delete(rootID string, rels []string) error {
	r, err := s.root(rootID)
	if err != nil {
		return err
	}
	if err := r.writable(); err != nil {
		return err
	}
	for _, rel := range rels {
		rel, err := clean(rel)
		if err != nil || rel == "." {
			return ErrBadPath
		}
		p, err := r.realPath(rel)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(p); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

// ── Upload ────────────────────────────────────────────────────────

// Upload grava o conteúdo em rel/relName (relName pode conter subpastas, vindas de
// um upload de pasta). Escreve primeiro num arquivo temporário e só renomeia no fim,
// então um upload interrompido nunca deixa arquivo pela metade.
func (s *Service) Upload(rootID, rel, relName string, body io.Reader) (string, error) {
	r, err := s.root(rootID)
	if err != nil {
		return "", err
	}
	if err := r.writable(); err != nil {
		return "", err
	}
	rel, err = clean(rel)
	if err != nil {
		return "", err
	}
	relName = strings.Trim(strings.ReplaceAll(relName, `\`, "/"), "/")
	parts := strings.Split(relName, "/")
	for _, p := range parts {
		if err := validName(p); err != nil {
			return "", err
		}
	}
	dirRel := path.Join(append([]string{rel}, parts[:len(parts)-1]...)...)
	// cria subpastas uma a uma, sempre pelo os.Root
	cur := rel
	for _, p := range parts[:len(parts)-1] {
		cur = path.Join(cur, p)
		if err := r.r.Mkdir(cur, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", mapErr(err)
		}
	}
	dir, err := r.realDir(dirRel)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".brunos-upload-*")
	if err != nil {
		return "", mapErr(err)
	}
	tmpName := tmp.Name()
	limited := &io.LimitedReader{R: body, N: s.MaxUpload + 1}
	n, err := io.Copy(tmp, limited)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > s.MaxUpload {
		err = errors.New("arquivo maior que o limite de upload")
	}
	if err != nil {
		os.Remove(tmpName)
		return "", mapErr(err)
	}
	_ = os.Chmod(tmpName, 0o644)
	final := uniqueName(dir, parts[len(parts)-1])
	if err := os.Rename(tmpName, filepath.Join(dir, final)); err != nil {
		os.Remove(tmpName)
		return "", mapErr(err)
	}
	return path.Join(dirRel, final), nil
}

// ── Busca ─────────────────────────────────────────────────────────

type Hit struct {
	Entry
	Path string `json:"path"`
}

func (s *Service) Search(rootID, rel, q string, limit int) ([]Hit, bool, error) {
	r, err := s.root(rootID)
	if err != nil {
		return nil, false, err
	}
	rel, err = clean(rel)
	if err != nil {
		return nil, false, err
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return []Hit{}, false, nil
	}
	deadline := time.Now().Add(3 * time.Second)
	visited, truncated := 0, false
	hits := []Hit{}
	stop := errors.New("stop")
	err = fs.WalkDir(r.r.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // pasta sem permissão: pula
		}
		if d.IsDir() && (d.Name() == trashDirName || d.Name() == "node_modules" || d.Name() == ".git") {
			return fs.SkipDir
		}
		visited++
		if visited > 50000 || time.Now().After(deadline) || len(hits) >= limit {
			truncated = true
			return stop
		}
		if p != rel && strings.Contains(strings.ToLower(d.Name()), q) {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			h := Hit{Entry: Entry{Name: d.Name(), Dir: d.IsDir(), Mtime: info.ModTime().Unix(), Hidden: strings.HasPrefix(d.Name(), ".")}, Path: p}
			if !h.Dir {
				h.Size = info.Size()
			}
			hits = append(hits, h)
		}
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return nil, false, mapErr(err)
	}
	return hits, truncated, nil
}

// ── Uso de disco por raiz ─────────────────────────────────────────

type RootInfo struct {
	RootConfig
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
}

func (s *Service) RootInfos() []RootInfo {
	roots := s.Roots()
	out := make([]RootInfo, 0, len(roots))
	for _, r := range roots {
		ri := RootInfo{RootConfig: r.RootConfig}
		var st syscall.Statfs_t
		if syscall.Statfs(r.real, &st) == nil {
			ri.Total = st.Blocks * uint64(st.Bsize)
			ri.Free = st.Bavail * uint64(st.Bsize)
		}
		out = append(out, ri)
	}
	return out
}
