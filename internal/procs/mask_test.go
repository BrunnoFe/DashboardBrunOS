package procs

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMaskArgs(t *testing.T) {
	cases := []struct {
		in     string // argumentos separados por "|"
		want   string // resultado unido por espaço
		masked bool
	}{
		{"python3|analise.py", "python3 analise.py", false},
		{"node|server.js|--port|8080", "node server.js --port 8080", false},
		{"app|--password=segredo123", "app --password=••••", true},
		{"app|--password|segredo123", "app --password ••••", true},
		{"app|--api-token|abc", "app --api-token ••••", true},
		{"app|--verbose|--senha=x", "app --verbose --senha=••••", true},
		{"env|DB_PASSWORD=segredo|python3", "env DB_PASSWORD=•••• python3", true},
		{"psql|postgres://usuario:segredo@db:5432/app", "psql postgres://usuario:••••@db:5432/app", true},
		{"curl|-H|Authorization: Bearer eyJhbGciOi", "curl -H Authorization: Bearer ••••", true},
		{"app|aB3dE5fG7hJ9kL1mN3pQ5rS7tU9", "app ••••", true},
		// IDs do Docker (hex minúsculo) e caminhos ficam como estão
		{"containerd-shim|-id|" + strings.Repeat("ab12", 16), "containerd-shim -id " + strings.Repeat("ab12", 16), false},
		{"nginx|-c|/etc/nginx/nginx.conf", "nginx -c /etc/nginx/nginx.conf", false},
		// opção de segredo seguida de outra opção: não esconde a opção
		{"app|--token|--verbose", "app --token --verbose", false},
	}
	for _, c := range cases {
		got, m := maskArgs(strings.Split(c.in, "|"))
		if strings.Join(got, " ") != c.want || m != c.masked {
			t.Errorf("%q: got %q (masked %v), want %q (masked %v)", c.in, strings.Join(got, " "), m, c.want, c.masked)
		}
	}
}

func TestListMasksAndCmdlineShowsAll(t *testing.T) {
	dir := fake(t, 1000, 100, 10)
	put(t, filepath.Join(dir, "300/stat"), "300 (app) S 1 0 0 0 0 0 0 0 0 1 0 0 0 20 0 1 0 100 1000 10 0")
	put(t, filepath.Join(dir, "300/cmdline"), "app\x00--password=segredo123\x00")
	r := New(dir, "")
	list, _ := r.List(false)
	var p *Proc
	for i := range list {
		if list[i].PID == 300 {
			p = &list[i]
		} else if list[i].Masked {
			t.Fatalf("processo sem segredo marcado como escondido: %+v", list[i])
		}
	}
	if p == nil || p.Cmd != "app --password=••••" || !p.Masked {
		t.Fatalf("lista deveria esconder a senha: %+v", list)
	}
	full, ok := r.Cmdline(300)
	if !ok || full != "app --password=segredo123" {
		t.Fatalf("Cmdline = %q, %v", full, ok)
	}
	if _, ok := r.Cmdline(999); ok {
		t.Fatal("processo inexistente deveria falhar")
	}
	if _, ok := r.Cmdline(-1); ok {
		t.Fatal("pid negativo deveria falhar")
	}
}
