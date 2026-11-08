// Package procs lista os processos do servidor (como o htop), lendo o /proc do host montado em
// /host/proc. Só leitura: o BrunOS não mata nem muda processos.
//
// CPU de cada processo = tempo de CPU que ele usou entre duas leituras ÷ tempo total, × núcleos
// (100% = um núcleo inteiro, igual ao htop). Na primeira chamada, lê duas vezes com um intervalo curto.
package procs

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Proc struct {
	PID       int     `json:"pid"`
	PPID      int     `json:"ppid"`
	Name      string  `json:"name"`
	Cmd       string  `json:"cmd"`
	User      string  `json:"user"`
	State     string  `json:"state"`
	Threads   int     `json:"threads"`
	CPU       float64 `json:"cpu"`
	Mem       uint64  `json:"mem"` // RSS em bytes
	MemPct    float64 `json:"memPct"`
	Container string  `json:"-"`             // id do container Docker (se for de um)
	App       string  `json:"app,omitempty"` // preenchido pelo main a partir do container
}

type Summary struct {
	Total   int     `json:"total"`
	Threads int     `json:"threads"`
	Running int     `json:"running"`
	Cores   int     `json:"cores"`
	MemKB   uint64  `json:"memKB"`
	Load    float64 `json:"load"`
}

type sample struct {
	ticks map[int]uint64
	total uint64
	at    time.Time
}

type Reader struct {
	Proc   string // /host/proc
	Passwd string // /host/passwd (opcional: nomes dos usuários)
	// Self é o id do container do próprio BrunOS. Com o cgroup isolado (padrão do Docker), os processos
	// do mesmo container aparecem como "0::/" no cgroup, sem id: esses ganham este.
	Self string

	mu      sync.Mutex
	prev    *sample
	users   map[int]string
	usersAt time.Time
}

func New(proc, passwd string) *Reader { return &Reader{Proc: proc, Passwd: passwd} }

var selfMount = regexp.MustCompile(`/containers/([0-9a-f]{64})/`)

// SelfID descobre o id do próprio container: o Docker monta /etc/hostname, /etc/hosts etc. a partir de
// /var/lib/docker/containers/<id>/, e isso aparece no mountinfo.
func SelfID(mountinfo string) string {
	if m := selfMount.FindSubmatch(readFile(mountinfo)); m != nil {
		return string(m[1])
	}
	return ""
}

var containerID = regexp.MustCompile(`[0-9a-f]{64}`)

const pageSize = 4096

func readFile(p string) []byte {
	b, _ := os.ReadFile(p)
	return b
}

// cpuTotal soma todos os tempos da linha "cpu" do /proc/stat e conta os núcleos.
func (r *Reader) cpuTotal() (total uint64, cores int) {
	sc := bufio.NewScanner(bytes.NewReader(readFile(filepath.Join(r.Proc, "stat"))))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "cpu ") {
			for _, f := range strings.Fields(line)[1:] {
				v, _ := strconv.ParseUint(f, 10, 64)
				total += v
			}
		} else if strings.HasPrefix(line, "cpu") {
			cores++
		}
	}
	if cores == 0 {
		cores = 1
	}
	return total, cores
}

func (r *Reader) userName(uid int) string {
	if time.Since(r.usersAt) > 5*time.Minute {
		r.users = map[int]string{0: "root"}
		if r.Passwd != "" {
			for _, line := range strings.Split(string(readFile(r.Passwd)), "\n") {
				f := strings.Split(line, ":")
				if len(f) >= 3 {
					if id, err := strconv.Atoi(f[2]); err == nil {
						r.users[id] = f[0]
					}
				}
			}
		}
		r.usersAt = time.Now()
	}
	if n, ok := r.users[uid]; ok {
		return n
	}
	return "uid " + strconv.Itoa(uid)
}

type raw struct {
	Proc
	ticks uint64
	uid   int
}

// parseStat lê /proc/<pid>/stat. O nome vem entre parênteses e pode ter espaços: corta no último ")".
func parseStat(b []byte) (name, state string, ppid int, ticks uint64, threads int, rssPages uint64, ok bool) {
	s := string(b)
	open, close := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || close < open {
		return
	}
	name = s[open+1 : close]
	f := strings.Fields(s[close+1:])
	if len(f) < 22 {
		return
	}
	state = f[0]
	ppid, _ = strconv.Atoi(f[1])
	ut, _ := strconv.ParseUint(f[11], 10, 64)
	st, _ := strconv.ParseUint(f[12], 10, 64)
	threads, _ = strconv.Atoi(f[17])
	rssPages, _ = strconv.ParseUint(f[21], 10, 64)
	return name, state, ppid, ut + st, threads, rssPages, true
}

func (r *Reader) scan() (map[int]raw, uint64, int) {
	total, cores := r.cpuTotal()
	out := map[int]raw{}
	entries, _ := os.ReadDir(r.Proc)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		dir := filepath.Join(r.Proc, e.Name())
		name, state, ppid, ticks, threads, rss, ok := parseStat(readFile(filepath.Join(dir, "stat")))
		if !ok {
			continue
		}
		p := raw{Proc: Proc{PID: pid, PPID: ppid, Name: name, State: state, Threads: threads, Mem: rss * pageSize}, ticks: ticks, uid: -1}
		for _, line := range strings.Split(string(readFile(filepath.Join(dir, "status"))), "\n") {
			if v, ok := strings.CutPrefix(line, "Uid:"); ok {
				if f := strings.Fields(v); len(f) > 0 {
					p.uid, _ = strconv.Atoi(f[0])
				}
				break
			}
		}
		cmd := bytes.TrimRight(readFile(filepath.Join(dir, "cmdline")), "\x00")
		p.Cmd = string(bytes.ReplaceAll(cmd, []byte{0}, []byte{' '}))
		if len(p.Cmd) > 400 {
			p.Cmd = p.Cmd[:400] + "…"
		}
		cg := readFile(filepath.Join(dir, "cgroup"))
		if m := containerID.Find(cg); m != nil {
			p.Container = string(m)
		} else if r.Self != "" && string(bytes.TrimSpace(cg)) == "0::/" {
			p.Container = r.Self
		}
		out[pid] = p
	}
	return out, total, cores
}

// isKernelThread: threads do kernel (kthreadd e filhos) não têm linha de comando.
func isKernelThread(p raw) bool { return p.Cmd == "" && (p.PID == 2 || p.PPID == 2) }

// List devolve os processos (sem as threads do kernel, a menos que kernel=true), do que mais usa
// CPU para o que menos usa.
func (r *Reader) List(kernel bool) ([]Proc, Summary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.prev == nil || time.Since(r.prev.at) > time.Minute {
		procs, total, _ := r.scan()
		r.prev = &sample{ticks: ticksOf(procs), total: total, at: time.Now()}
		r.mu.Unlock()
		time.Sleep(400 * time.Millisecond)
		r.mu.Lock()
	}
	procs, total, cores := r.scan()
	prev := r.prev
	r.prev = &sample{ticks: ticksOf(procs), total: total, at: time.Now()}

	var memKB uint64
	for _, line := range strings.Split(string(readFile(filepath.Join(r.Proc, "meminfo"))), "\n") {
		if v, ok := strings.CutPrefix(line, "MemTotal:"); ok {
			memKB, _ = strconv.ParseUint(strings.Fields(v)[0], 10, 64)
		}
	}
	sum := Summary{Cores: cores, MemKB: memKB}
	if f := strings.Fields(string(readFile(filepath.Join(r.Proc, "loadavg")))); len(f) > 0 {
		sum.Load, _ = strconv.ParseFloat(f[0], 64)
	}
	dTotal := float64(total - prev.total)
	out := make([]Proc, 0, len(procs))
	for _, p := range procs {
		if !kernel && isKernelThread(p) {
			continue
		}
		sum.Total++
		sum.Threads += p.Threads
		if p.State == "R" {
			sum.Running++
		}
		if old, ok := prev.ticks[p.PID]; ok && dTotal > 0 && p.ticks >= old {
			p.CPU = round1(float64(p.ticks-old) / dTotal * 100 * float64(cores))
		}
		if memKB > 0 {
			p.MemPct = round1(float64(p.Mem) / float64(memKB*1024) * 100)
		}
		if p.uid >= 0 {
			p.User = r.userName(p.uid)
		}
		out = append(out, p.Proc)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPU != out[j].CPU {
			return out[i].CPU > out[j].CPU
		}
		return out[i].Mem > out[j].Mem
	})
	return out, sum
}

func ticksOf(m map[int]raw) map[int]uint64 {
	t := make(map[int]uint64, len(m))
	for pid, p := range m {
		t[pid] = p.ticks
	}
	return t
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
