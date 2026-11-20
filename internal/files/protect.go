package files

// Proteções do Arquivos (revisão de segurança de 19/11/2026, lote 3, escolhas do Bruno):
//
//   - Arquivos e pastas ocultos da pasta pessoal (ex.: <usuário>/.ssh, .bashrc, .config) ficam SÓ
//     LEITURA pelo BrunOS: quem roubasse uma Sessão gravaria uma chave em .ssh/authorized_keys e
//     entraria no servidor como o dono. Para mudar esses arquivos, o Terminal.
//   - Segredos somem do Arquivos (lista, abrir, baixar, zip, busca, duplicados, mapa do espaço, copiar):
//     a pasta de dados do próprio BrunOS (chave SSH do script do servidor, chave das integrações), o
//     app.env de cada app e o .env do Runtipi (senhas), e as chaves privadas ~/.ssh/id_*.
//
// A conferência é sempre no caminho REAL (atalhos resolvidos): um atalho para .ssh não escapa.

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrProtected: escrita num arquivo ou pasta oculta da pasta pessoal.
var ErrProtected = errors.New("arquivos e pastas ocultos da pasta pessoal são só leitura pelo BrunOS (para mudar, use o Terminal)")

// protectedReal diz se o caminho real (absoluto) é um arquivo/pasta oculta da pasta pessoal, ou fica
// dentro de uma: <raiz Home>/<usuário>/.<algo>[/...].
func (r *Root) protectedReal(real string) bool {
	if r.Kind != "place" {
		return false
	}
	rel, err := filepath.Rel(r.real, real)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	p := strings.Split(filepath.ToSlash(rel), "/")
	return len(p) >= 2 && strings.HasPrefix(p[1], ".")
}

// guardWrite recusa escrever em qualquer um dos caminhos reais dados (origem e destino).
func (r *Root) guardWrite(reals ...string) error {
	for _, p := range reals {
		if r.protectedReal(p) {
			return ErrProtected
		}
	}
	return nil
}

// secretDirs: pastas reconhecidas pela identidade (dispositivo + inode), não pelo nome: a pasta de dados
// do BrunOS aparece no Arquivos por outro caminho (dentro da pasta do Runtipi), mas é a mesma pasta.
type secretDirs struct {
	data    []os.FileInfo // pastas inteiras escondidas (a pasta de dados do BrunOS)
	runtipi os.FileInfo   // a pasta do Runtipi (para achar .env e app-data/*/*/app.env)
}

// Protect informa as pastas de segredos: dataDir (o /data do BrunOS) e runtipiDir (a pasta do Runtipi).
func (s *Service) Protect(dataDir, runtipiDir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets = secretDirs{}
	if st, err := os.Stat(dataDir); err == nil && st.IsDir() {
		s.secrets.data = append(s.secrets.data, st)
	}
	if st, err := os.Stat(runtipiDir); err == nil && st.IsDir() {
		s.secrets.runtipi = st
	}
}

func (s *Service) secretSet() secretDirs {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.secrets
}

// secretName: nomes que podem ser segredo (só esses passam pela conferência completa, que é mais cara).
func secretName(name string) bool {
	return name == ".env" || name == "app.env" || (strings.HasPrefix(name, "id_") && !strings.HasSuffix(name, ".pub"))
}

// isSecretReal confere um caminho real (absoluto): dentro da pasta de dados do BrunOS, um .env/app.env do
// Runtipi ou uma chave privada de .ssh.
func (s *Service) isSecretReal(real string) bool {
	sec := s.secretSet()
	base := filepath.Base(real)
	if strings.HasPrefix(base, "id_") && !strings.HasSuffix(base, ".pub") && filepath.Base(filepath.Dir(real)) == ".ssh" {
		return true
	}
	if len(sec.data) == 0 && sec.runtipi == nil {
		return false
	}
	// sobe pelas pastas: alguma é a pasta de dados do BrunOS ou a do Runtipi?
	p := real
	for i := 0; i < 64; i++ {
		st, err := os.Stat(p)
		if err == nil {
			for _, d := range sec.data {
				if os.SameFile(st, d) {
					return true
				}
			}
			if sec.runtipi != nil && os.SameFile(st, sec.runtipi) {
				rel, err := filepath.Rel(p, real)
				if err != nil {
					return false
				}
				rel = filepath.ToSlash(rel)
				if rel == ".env" {
					return true
				}
				for _, pat := range []string{"app-data/*/*/app.env", "user-config/*/*/app.env"} {
					if ok, _ := path.Match(pat, rel); ok {
						return true
					}
				}
				return false
			}
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	return false
}

// isSecret confere um caminho relativo de uma raiz (resolve os atalhos antes).
func (s *Service) isSecret(r *Root, rel string) bool {
	real, err := filepath.EvalSymlinks(filepath.Join(r.real, filepath.FromSlash(rel)))
	if err != nil {
		return false // não existe: quem abrir recebe "não encontrado" de qualquer jeito
	}
	return s.isSecretReal(real)
}

// hideEntry decide, numa listagem ou varredura, se um item some: pastas só pela identidade (barato:
// compara com as pastas de segredos), arquivos só se o nome for de segredo.
func (s *Service) hideEntry(r *Root, rel string, d fs.DirEntry) bool {
	if d.IsDir() {
		sec := s.secretSet()
		if len(sec.data) == 0 {
			return false
		}
		info, err := os.Stat(filepath.Join(r.real, filepath.FromSlash(rel)))
		if err != nil {
			return false
		}
		for _, x := range sec.data {
			if os.SameFile(info, x) {
				return true
			}
		}
		return false
	}
	return secretName(d.Name()) && s.isSecret(r, rel)
}
