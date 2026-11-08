package files

// Envio em pedaços com retomada.
//
// O navegador pede um id (Start), manda pedaços em ordem (Chunk, cada um dizendo em que posição
// começa) e no fim pede para concluir (Finish). O conteúdo vai para ".brunos-part-<id>" na própria
// pasta de destino (mesmo disco: concluir é só um rename). Se a conexão cair, Status diz quantos
// bytes chegaram e o envio continua dali. Os envios pendentes ficam em /data/uploads.json, então
// sobrevivem a um reinício do BrunOS; os abandonados há mais de 2 dias são apagados.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"brunos/internal/store"
)

const (
	ChunkMax      = 32 << 20 // maior pedaço aceito por requisição
	maxPending    = 100
	pendingMaxAge = 48 * time.Hour
	partPrefix    = ".brunos-part-"
)

var (
	ErrUploadGone = errors.New("envio não encontrado (terminou, foi cancelado ou expirou); comece de novo")
	ErrNoSpace    = errors.New("não há espaço livre suficiente neste disco")
	ErrOffset     = errors.New("posição do pedaço não confere")
	uploadIDRe    = regexp.MustCompile(`^[a-f0-9]{24}$`)
)

type pendingUpload struct {
	ID      string `json:"id"`
	Root    string `json:"root"`
	Dir     string `json:"dir"`  // pasta de destino, relativa à raiz
	Name    string `json:"name"` // nome final
	Size    int64  `json:"size"`
	Part    string `json:"part"` // caminho real do arquivo parcial
	Updated int64  `json:"updated"`

	mu sync.Mutex
}

type uploadsFile struct {
	Version int              `json:"version"`
	Uploads []*pendingUpload `json:"uploads"`
}

type uploads struct {
	mu   sync.Mutex
	m    map[string]*pendingUpload
	file string
}

// LoadUploads lê os envios pendentes (chamar uma vez ao iniciar, depois de definir UploadsFile).
func (s *Service) LoadUploads(file string) {
	s.up.mu.Lock()
	defer s.up.mu.Unlock()
	s.up.file, s.up.m = file, map[string]*pendingUpload{}
	var f uploadsFile
	if file != "" {
		_, _ = store.ReadJSON(file, &f)
	}
	for _, u := range f.Uploads {
		if u != nil && uploadIDRe.MatchString(u.ID) && strings.HasPrefix(filepath.Base(u.Part), partPrefix) {
			s.up.m[u.ID] = u
		}
	}
	s.cleanupLocked(time.Now())
}

func (s *Service) saveUploadsLocked() {
	if s.up.file == "" {
		return
	}
	list := make([]*pendingUpload, 0, len(s.up.m))
	for _, u := range s.up.m {
		list = append(list, u)
	}
	_ = store.WriteJSON(s.up.file, uploadsFile{Version: 1, Uploads: list}, 0o600)
}

// cleanupLocked apaga envios abandonados e os que perderam o arquivo parcial.
func (s *Service) cleanupLocked(now time.Time) {
	changed := false
	for id, u := range s.up.m {
		_, err := os.Lstat(u.Part)
		if now.Sub(time.Unix(u.Updated, 0)) > pendingMaxAge || errors.Is(err, fs.ErrNotExist) {
			os.Remove(u.Part)
			delete(s.up.m, id)
			changed = true
		}
	}
	if changed {
		s.saveUploadsLocked()
	}
}

func freeSpace(dir string) (uint64, bool) {
	var st syscall.Statfs_t
	if syscall.Statfs(dir, &st) != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}

// StartUpload prepara um envio em pedaços. relName pode ter subpastas (envio de pasta).
func (s *Service) StartUpload(rootID, rel, relName string, size int64) (string, error) {
	r, err := s.root(rootID)
	if err != nil {
		return "", err
	}
	if err := r.writable(); err != nil {
		return "", err
	}
	if size < 0 || size > s.MaxUpload {
		return "", errors.New("arquivo maior que o limite de upload")
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
	cur := rel
	for _, p := range parts[:len(parts)-1] {
		cur = path.Join(cur, p)
		if err := r.r.Mkdir(cur, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", mapErr(err)
		}
	}
	dirRel := path.Join(append([]string{rel}, parts[:len(parts)-1]...)...)
	dir, err := r.realDir(dirRel)
	if err != nil {
		return "", err
	}
	if free, ok := freeSpace(dir); ok && uint64(size) > free {
		return "", ErrNoSpace
	}

	s.up.mu.Lock()
	defer s.up.mu.Unlock()
	if s.up.m == nil {
		s.up.m = map[string]*pendingUpload{}
	}
	s.cleanupLocked(time.Now())
	if len(s.up.m) >= maxPending {
		return "", errors.New("envios pendentes demais; conclua ou cancele alguns")
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	part := filepath.Join(dir, partPrefix+id)
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", mapErr(err)
	}
	f.Close()
	s.up.m[id] = &pendingUpload{ID: id, Root: rootID, Dir: dirRel, Name: parts[len(parts)-1], Size: size, Part: part, Updated: time.Now().Unix()}
	s.saveUploadsLocked()
	return id, nil
}

func (s *Service) pending(id string) (*pendingUpload, *Root, error) {
	if !uploadIDRe.MatchString(id) {
		return nil, nil, ErrUploadGone
	}
	s.up.mu.Lock()
	u := s.up.m[id]
	s.up.mu.Unlock()
	if u == nil {
		return nil, nil, ErrUploadGone
	}
	r, err := s.root(u.Root)
	if err != nil {
		return nil, nil, err
	}
	if !within(r.real, u.Part) {
		return nil, nil, ErrBadPath
	}
	return u, r, nil
}

// UploadStatus diz quantos bytes já chegaram.
func (s *Service) UploadStatus(id string) (offset, size int64, err error) {
	u, _, err := s.pending(id)
	if err != nil {
		return 0, 0, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	st, err := os.Stat(u.Part)
	if err != nil {
		return 0, 0, ErrUploadGone
	}
	return st.Size(), u.Size, nil
}

// UploadChunk acrescenta um pedaço que começa em offset. Se offset não for o tamanho atual do
// arquivo parcial, devolve ErrOffset e a posição certa (o navegador continua de lá).
func (s *Service) UploadChunk(id string, offset int64, body io.Reader) (int64, error) {
	u, _, err := s.pending(id)
	if err != nil {
		return 0, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	st, err := os.Stat(u.Part)
	if err != nil {
		return 0, ErrUploadGone
	}
	cur := st.Size()
	if offset != cur {
		return cur, ErrOffset
	}
	room := u.Size - cur
	f, err := os.OpenFile(u.Part, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return cur, mapErr(err)
	}
	n, err := io.Copy(f, io.LimitReader(body, min(room, ChunkMax)+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > room {
		err = errors.New("o envio passou do tamanho anunciado")
	}
	if err != nil {
		_ = os.Truncate(u.Part, cur) // descarta o pedaço com problema; o navegador manda de novo
		return cur, mapErr(err)
	}
	s.up.mu.Lock()
	u.Updated = time.Now().Unix()
	s.saveUploadsLocked()
	s.up.mu.Unlock()
	return cur + n, nil
}

// FinishUpload confere o tamanho e põe o arquivo no lugar (com nome único, como no envio normal).
func (s *Service) FinishUpload(id string) (string, error) {
	u, r, err := s.pending(id)
	if err != nil {
		return "", err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	st, err := os.Stat(u.Part)
	if err != nil {
		return "", ErrUploadGone
	}
	if st.Size() != u.Size {
		return "", errors.New("o envio ainda não terminou")
	}
	dir, err := r.realDir(u.Dir)
	if err != nil || filepath.Dir(u.Part) != dir {
		return "", ErrBadPath
	}
	_ = os.Chmod(u.Part, 0o644)
	final := uniqueName(dir, u.Name)
	if err := os.Rename(u.Part, filepath.Join(dir, final)); err != nil {
		return "", mapErr(err)
	}
	s.up.mu.Lock()
	delete(s.up.m, id)
	s.saveUploadsLocked()
	s.up.mu.Unlock()
	return path.Join(u.Dir, final), nil
}

// CancelUpload desiste do envio e apaga o arquivo parcial.
func (s *Service) CancelUpload(id string) error {
	u, _, err := s.pending(id)
	if err != nil {
		return err
	}
	u.mu.Lock()
	os.Remove(u.Part)
	u.mu.Unlock()
	s.up.mu.Lock()
	delete(s.up.m, id)
	s.saveUploadsLocked()
	s.up.mu.Unlock()
	return nil
}
