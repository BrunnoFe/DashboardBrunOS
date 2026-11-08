package files

// Encontrar arquivos duplicados: agrupa por tamanho, depois por um "resumo" rápido (começo + fim do
// arquivo) e só então confirma com SHA-256 do arquivo inteiro. Com limites de tempo e de leitura
// para não travar o servidor numa pasta enorme.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"sort"
	"time"
)

const (
	dupMaxEntries = 300_000
	dupWalkTime   = 20 * time.Second
	dupTotalTime  = 90 * time.Second
	dupMaxHashed  = 50 << 30 // no máximo 50 GB lidos para conferir
	dupMaxGroups  = 500
	dupEdge       = 64 << 10
)

type DupFile struct {
	Path  string `json:"path"`
	Mtime int64  `json:"mtime"`
}

type DupGroup struct {
	Size  int64     `json:"size"`
	Files []DupFile `json:"files"`
}

type DupResult struct {
	Groups    []DupGroup `json:"groups"`
	Wasted    int64      `json:"wasted"`  // bytes que dá para liberar (todas as cópias menos uma)
	Scanned   int        `json:"scanned"` // arquivos olhados
	Truncated bool       `json:"truncated"`
}

func hashFile(fsys fs.FS, p string, partial bool, size int64) (string, int64, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	if !partial || size <= 2*dupEdge {
		n, err := io.Copy(h, f)
		return hex.EncodeToString(h.Sum(nil)), n, err
	}
	if _, err := io.CopyN(h, f, dupEdge); err != nil {
		return "", 0, err
	}
	sk, ok := f.(io.Seeker)
	if !ok {
		return "", 0, errors.New("sem seek")
	}
	if _, err := sk.Seek(-dupEdge, io.SeekEnd); err != nil {
		return "", 0, err
	}
	if _, err := io.CopyN(h, f, dupEdge); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), 2 * dupEdge, nil
}

// Duplicates procura arquivos iguais (de pelo menos minSize bytes) dentro de rel.
func (s *Service) Duplicates(rootID, rel string, minSize int64) (DupResult, error) {
	r, err := s.root(rootID)
	if err != nil {
		return DupResult{}, err
	}
	rel, err = clean(rel)
	if err != nil {
		return DupResult{}, err
	}
	if minSize < 1 {
		minSize = 1
	}
	start := time.Now()
	fsys := r.r.FS()
	bySize := map[int64][]DupFile{}
	res := DupResult{Groups: []DupGroup{}}
	stop := errors.New("stop")
	err = fs.WalkDir(fsys, rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == trashDirName || d.Name() == ".git" || d.Name() == "node_modules") {
			return fs.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		res.Scanned++
		if res.Scanned > dupMaxEntries || time.Since(start) > dupWalkTime {
			res.Truncated = true
			return stop
		}
		info, err := d.Info()
		if err != nil || info.Size() < minSize {
			return nil
		}
		bySize[info.Size()] = append(bySize[info.Size()], DupFile{Path: p, Mtime: info.ModTime().Unix()})
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return DupResult{}, mapErr(err)
	}

	// maiores primeiro: são os que mais liberam espaço
	sizes := make([]int64, 0, len(bySize))
	for sz, l := range bySize {
		if len(l) > 1 {
			sizes = append(sizes, sz)
		}
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i] > sizes[j] })
	var hashed int64
	group := func(list []DupFile, size int64, partial bool) map[string][]DupFile {
		out := map[string][]DupFile{}
		for _, f := range list {
			if time.Since(start) > dupTotalTime || hashed > dupMaxHashed {
				res.Truncated = true
				return out
			}
			h, n, err := hashFile(fsys, f.Path, partial, size)
			hashed += n
			if err == nil {
				out[h] = append(out[h], f)
			}
		}
		return out
	}
	for _, sz := range sizes {
		if len(res.Groups) >= dupMaxGroups {
			res.Truncated = true
			break
		}
		for _, cands := range group(bySize[sz], sz, true) {
			if len(cands) < 2 {
				continue
			}
			full := map[string][]DupFile{"": cands}
			if sz > 2*dupEdge { // o resumo já leu o arquivo inteiro nos pequenos
				full = group(cands, sz, false)
			}
			for _, same := range full {
				if len(same) < 2 {
					continue
				}
				sort.Slice(same, func(i, j int) bool { return same[i].Mtime < same[j].Mtime })
				res.Groups = append(res.Groups, DupGroup{Size: sz, Files: same})
				res.Wasted += sz * int64(len(same)-1)
			}
		}
		if res.Truncated && time.Since(start) > dupTotalTime {
			break
		}
	}
	sort.SliceStable(res.Groups, func(i, j int) bool {
		return res.Groups[i].Size*int64(len(res.Groups[i].Files)-1) > res.Groups[j].Size*int64(len(res.Groups[j].Files)-1)
	})
	return res, nil
}
