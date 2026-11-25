package procs

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Linha de comando dos processos (V13 da revisão de segurança): a lista mostra o comando com o que
// parece segredo trocado por "••••"; o comando inteiro só sai por Cmdline, numa rota que pede a senha.

const hidden = "••••"

// secretName: nomes de opção ou de variável que costumam carregar segredo (--password, API_TOKEN, ...).
var secretName = regexp.MustCompile(`(?i)(pass|pwd|secret|token|key|auth|cred|cookie|session|private|signature|senha)`)

// urlCreds: usuário:senha@ dentro de um endereço (postgres://user:senha@host).
var urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]*:)[^/\s@]+@`)

// bearer: cabeçalho de autorização passado como argumento (curl -H "Authorization: Bearer ...").
var bearer = regexp.MustCompile(`(?i)\b(bearer|basic|token)\s+\S+`)

// opaque: textos longos com cara de chave (letras maiúsculas, minúsculas e números misturados).
// IDs em hexadecimal minúsculo (containers do Docker) não entram: não são segredo e ajudam a reconhecer.
var opaque = regexp.MustCompile(`^[A-Za-z0-9+/=_.-]{24,}$`)

func looksOpaque(s string) bool {
	if !opaque.MatchString(s) {
		return false
	}
	var up, low, dig bool
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
			up = true
		case c >= 'a' && c <= 'z':
			low = true
		case c >= '0' && c <= '9':
			dig = true
		}
	}
	return up && low && dig
}

// maskArgs devolve os argumentos com os segredos escondidos e diz se escondeu algum.
func maskArgs(args []string) ([]string, bool) {
	out := make([]string, len(args))
	masked := false
	hide := func(i int, v string) { out[i] = v; masked = true }
	for i, a := range args {
		out[i] = a
		if i > 0 && strings.HasPrefix(args[i-1], "-") && !strings.Contains(args[i-1], "=") &&
			secretName.MatchString(args[i-1]) && !strings.HasPrefix(a, "-") {
			hide(i, hidden) // --password segredo
			continue
		}
		if name, _, ok := strings.Cut(a, "="); ok && name != "" && secretName.MatchString(name) && !strings.ContainsAny(name, " /") {
			hide(i, name+"="+hidden) // --password=segredo, API_TOKEN=segredo
			continue
		}
		v := a
		if urlCreds.MatchString(v) {
			v = urlCreds.ReplaceAllString(v, "${1}"+hidden+"@")
		}
		if bearer.MatchString(v) {
			v = bearer.ReplaceAllString(v, "${1} "+hidden)
		}
		if looksOpaque(v) {
			v = hidden
		}
		if v != a {
			hide(i, v)
		}
	}
	return out, masked
}

// splitCmdline separa o conteúdo de /proc/<pid>/cmdline (argumentos terminados em \0).
func splitCmdline(b []byte) []string {
	b = bytes.TrimRight(b, "\x00")
	if len(b) == 0 {
		return nil
	}
	return strings.Split(string(b), "\x00")
}

// Cmdline devolve o comando inteiro de um processo, sem esconder nada (até 8 KB).
func (r *Reader) Cmdline(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(r.Proc, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return "", false
	}
	s := strings.Join(splitCmdline(b), " ")
	if len(s) > 8192 {
		s = s[:8192] + "…"
	}
	return s, true
}
