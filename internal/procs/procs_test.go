package procs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// proc falso: 2 núcleos, um processo de usuário (com espaço e parênteses no nome), um container e
// uma thread do kernel.
func fake(t *testing.T, total, ticks1, ticks2 uint64) string {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "stat"), fmt.Sprintf("cpu  %d 0 0 0 0 0 0 0 0 0\ncpu0 1 0 0 0\ncpu1 1 0 0 0\n", total))
	put(t, filepath.Join(dir, "meminfo"), "MemTotal:        8000000 kB\nMemFree: 1 kB\n")
	put(t, filepath.Join(dir, "loadavg"), "0.42 0.30 0.20 1/300 999\n")
	stat := func(pid int, name, state string, ppid int, ticks uint64, rss int) string {
		return fmt.Sprintf("%d (%s) %s %d 0 0 0 0 0 0 0 0 0 %d 0 0 0 20 0 3 0 100 1000 %d 0", pid, name, state, ppid, ticks, rss)
	}
	put(t, filepath.Join(dir, "100/stat"), stat(100, "my (weird) app", "R", 1, ticks1, 2500))
	put(t, filepath.Join(dir, "100/status"), "Name:\tx\nUid:\t1000\t1000\t1000\t1000\n")
	put(t, filepath.Join(dir, "100/cmdline"), "python3\x00analise.py\x00")
	put(t, filepath.Join(dir, "100/cgroup"), "0::/user.slice\n")
	put(t, filepath.Join(dir, "200/stat"), stat(200, "immich", "S", 1, ticks2, 50000))
	put(t, filepath.Join(dir, "200/status"), "Uid:\t0\t0\t0\t0\n")
	put(t, filepath.Join(dir, "200/cmdline"), "node\x00server.js\x00")
	put(t, filepath.Join(dir, "200/cgroup"), "0::/system.slice/docker-"+fmt.Sprintf("%064x", 7)+".scope\n")
	put(t, filepath.Join(dir, "3/stat"), stat(3, "kworker/0:1", "I", 2, 0, 0))
	put(t, filepath.Join(dir, "3/status"), "Uid:\t0\n")
	put(t, filepath.Join(dir, "passwd"), "root:x:0:0:root:/root:/bin/bash\nptbruno:x:1000:1000::/home/ptbruno:/bin/bash\n")
	return dir
}

func TestList(t *testing.T) {
	dir := fake(t, 1000, 100, 10)
	r := New(dir, filepath.Join(dir, "passwd"))
	// primeira leitura (guarda a amostra); depois os contadores andam
	r.List(false)
	put(t, filepath.Join(dir, "stat"), "cpu  1200 0 0 0 0 0 0 0 0 0\ncpu0 1 0 0 0\ncpu1 1 0 0 0\n")
	stat := func(pid int, name, state string, ppid int, ticks uint64, rss int) string {
		return fmt.Sprintf("%d (%s) %s %d 0 0 0 0 0 0 0 0 0 %d 0 0 0 20 0 3 0 100 1000 %d 0", pid, name, state, ppid, ticks, rss)
	}
	put(t, filepath.Join(dir, "100/stat"), stat(100, "my (weird) app", "R", 1, 150, 2500)) // +50 de 200 → 25% × 2 núcleos = 50%
	put(t, filepath.Join(dir, "200/stat"), stat(200, "immich", "S", 1, 20, 50000))         // +10 → 10%
	list, sum := r.List(false)
	if len(list) != 2 || sum.Total != 2 || sum.Cores != 2 || sum.Running != 1 || sum.Load != 0.42 {
		t.Fatalf("lista/resumo: %+v %+v", list, sum)
	}
	p := list[0]
	if p.PID != 100 || p.Name != "my (weird) app" || p.CPU != 50 || p.User != "ptbruno" || p.Cmd != "python3 analise.py" || p.Mem != 2500*4096 || p.Threads != 3 {
		t.Fatalf("processo 100: %+v", p)
	}
	q := list[1]
	if q.CPU != 10 || q.User != "root" || q.Container != fmt.Sprintf("%064x", 7) || q.MemPct != 2.5 {
		t.Fatalf("processo 200: %+v", q)
	}
	if all, _ := r.List(true); len(all) != 3 {
		t.Fatalf("com threads do kernel: %d", len(all))
	}
	// processo do próprio container do BrunOS: cgroup "0::/" ganha o id descoberto no mountinfo
	self := fmt.Sprintf("%064x", 9)
	put(t, filepath.Join(dir, "mountinfo"), "1 2 0:1 /var/lib/docker/containers/"+self+"/hostname /etc/hostname rw - ext4 /dev/sda1 rw\n")
	r.Self = SelfID(filepath.Join(dir, "mountinfo"))
	put(t, filepath.Join(dir, "100/cgroup"), "0::/\n")
	for _, p := range func() []Proc { l, _ := r.List(false); return l }() {
		if p.PID == 100 && p.Container != self {
			t.Fatalf("processo do próprio container sem id: %+v", p)
		}
	}
}
