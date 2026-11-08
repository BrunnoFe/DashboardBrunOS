package files

// Mapa do espaço: quanto cada pasta ocupa. Uma varredura soma o tamanho de todas as subpastas de uma
// vez e fica guardada por 10 minutos, então entrar numa subpasta responde na hora. Com limites de
// tempo (30 s) e de itens (1 milhão); se passar, o resultado vem marcado como parcial.

import (
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	usageTTL        = 10 * time.Minute
	usageMaxTime    = 30 * time.Second
	usageMaxEntries = 1_000_000
	usageMaxItems   = 300
)

type dirTotal struct {
	size  int64
	count int64 // arquivos dentro (em qualquer nível)
}

type usageScan struct {
	rel       string
	at        time.Time
	dirs      map[string]dirTotal
	truncated bool
}

type usageCache struct {
	mu    sync.Mutex
	scan  sync.Mutex // uma varredura por vez (pasta grande pesa no disco)
	scans map[string]*usageScan
}

type UsageItem struct {
	Name  string `json:"name"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	Count int64  `json:"count,omitempty"`
}

type UsageResult struct {
	Path      string      `json:"path"`
	Size      int64       `json:"size"`
	Count     int64       `json:"count"`
	Items     []UsageItem `json:"items"`
	Others    int64       `json:"others,omitempty"` // bytes dos itens que não couberam na lista
	Truncated bool        `json:"truncated"`
	ScannedAt int64       `json:"scannedAt"`
}

func (c *usageCache) get(rootID, rel string) *usageScan {
	c.mu.Lock()
	defer c.mu.Unlock()
	sc := c.scans[rootID]
	if sc == nil || time.Since(sc.at) > usageTTL {
		return nil
	}
	if sc.rel == "." || rel == sc.rel || strings.HasPrefix(rel, sc.rel+"/") {
		return sc
	}
	return nil
}

func scanUsage(fsys fs.FS, rel string) *usageScan {
	sc := &usageScan{rel: rel, at: time.Now(), dirs: map[string]dirTotal{rel: {}}}
	start, n := time.Now(), 0
	stop := errors.New("stop")
	_ = fs.WalkDir(fsys, rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		n++
		if n > usageMaxEntries || n%1000 == 0 && time.Since(start) > usageMaxTime {
			sc.truncated = true
			return stop
		}
		if d.IsDir() {
			if d.Name() == trashDirName && p != rel {
				return fs.SkipDir
			}
			if _, ok := sc.dirs[p]; !ok {
				sc.dirs[p] = dirTotal{}
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		sz := info.Size()
		for dir := path.Dir(p); ; dir = path.Dir(dir) { // soma em todas as pastas acima, até rel
			t := sc.dirs[dir]
			t.size += sz
			t.count++
			sc.dirs[dir] = t
			if dir == rel || dir == "." {
				break
			}
		}
		return nil
	})
	return sc
}

// Usage diz quanto ocupa cada item de rel. refresh força uma varredura nova.
func (s *Service) Usage(rootID, rel string, refresh bool) (UsageResult, error) {
	r, err := s.root(rootID)
	if err != nil {
		return UsageResult{}, err
	}
	rel, err = clean(rel)
	if err != nil {
		return UsageResult{}, err
	}
	fsys := r.r.FS()
	if st, err := fs.Stat(fsys, rel); err != nil {
		return UsageResult{}, mapErr(err)
	} else if !st.IsDir() {
		return UsageResult{}, ErrBadPath
	}
	sc := s.usage.get(rootID, rel)
	if sc == nil || refresh {
		s.usage.scan.Lock()
		if sc = s.usage.get(rootID, rel); sc == nil || refresh {
			sc = scanUsage(fsys, rel)
			s.usage.mu.Lock()
			if s.usage.scans == nil {
				s.usage.scans = map[string]*usageScan{}
			}
			s.usage.scans[rootID] = sc
			s.usage.mu.Unlock()
		}
		s.usage.scan.Unlock()
	}

	total := sc.dirs[rel]
	res := UsageResult{Path: rel, Size: total.size, Count: total.count, Truncated: sc.truncated, ScannedAt: sc.at.Unix(), Items: []UsageItem{}}
	if res.Path == "." {
		res.Path = ""
	}
	entries, err := fs.ReadDir(fsys, rel)
	if err != nil {
		return UsageResult{}, mapErr(err)
	}
	for _, e := range entries {
		if e.Name() == trashDirName && rel == "." {
			continue
		}
		p := path.Join(rel, e.Name())
		switch {
		case e.IsDir():
			t := sc.dirs[p]
			res.Items = append(res.Items, UsageItem{Name: e.Name(), Dir: true, Size: t.size, Count: t.count})
		case e.Type().IsRegular():
			if info, err := e.Info(); err == nil {
				res.Items = append(res.Items, UsageItem{Name: e.Name(), Size: info.Size()})
			}
		}
	}
	sort.Slice(res.Items, func(i, j int) bool { return res.Items[i].Size > res.Items[j].Size })
	if len(res.Items) > usageMaxItems {
		for _, it := range res.Items[usageMaxItems:] {
			res.Others += it.Size
		}
		res.Items = res.Items[:usageMaxItems]
	}
	return res, nil
}
