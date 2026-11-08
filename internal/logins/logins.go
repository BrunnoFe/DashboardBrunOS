// Package logins lembra de onde o BrunOS já foi acessado (navegador + sistema + rede) para
// avisar na central quando alguém entra de um lugar novo, e quando um endereço erra a senha
// tantas vezes que é bloqueado.
//
// "Lugar" é a rede: qualquer IP local (casa, Tailscale) conta como "rede local"; IPs da
// internet são agrupados pela faixa (/24 no IPv4, /48 no IPv6), para que trocar de IP dentro
// do mesmo provedor não gere aviso toda hora. Fica em /data/logins.json (até 100 aparelhos).
package logins

import (
	"log/slog"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"brunos/internal/store"
)

const (
	maxDevices = 100
	blockEvery = 6 * time.Hour // aviso de bloqueio: no máximo um a cada 6 h por endereço
)

type Device struct {
	Key     string `json:"key"`
	Browser string `json:"browser"`
	OS      string `json:"os"`
	Local   bool   `json:"local"`
	IP      string `json:"ip"` // último IP usado
	First   int64  `json:"first"`
	Last    int64  `json:"last"`
	Count   int    `json:"count"`
}

// Describe devolve algo como "Chrome no Windows".
func (d Device) Describe() string {
	if d.OS == "" {
		return d.Browser
	}
	return d.Browser + " no " + d.OS
}

// Where devolve "pela rede local (192.168.0.10)" ou "pela internet (200.1.2.3)".
func (d Device) Where() string {
	if d.Local {
		return "pela rede local (" + d.IP + ")"
	}
	return "pela internet (" + d.IP + ")"
}

type file struct {
	Version int      `json:"version"`
	Devices []Device `json:"devices"`
}

type Store struct {
	File string
	Now  func() time.Time
	// IsLocal decide se um IP é da rede de casa (o main usa a mesma lista do resto do BrunOS).
	IsLocal func(net.IP) bool

	mu      sync.Mutex
	devices []Device
	existed bool
	blocks  map[string]time.Time
}

// New lê o arquivo (se existir). path vazio = só em memória (testes).
func New(path string) *Store {
	s := &Store{File: path, Now: time.Now, IsLocal: defaultLocal, blocks: map[string]time.Time{}}
	if path != "" {
		var f file
		ok, err := store.ReadJSON(path, &f)
		if err != nil {
			slog.Warn("acessos conhecidos: arquivo ilegível, começando do zero", "erro", err)
		}
		s.existed = ok && err == nil
		s.devices = f.Devices
	}
	return s
}

func defaultLocal(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// Fresh diz se ainda não havia nenhum registro (primeira vez com esta versão): nesse caso o
// main ensina os acessos das sessões já abertas, sem avisar.
func (s *Store) Fresh() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.existed && len(s.devices) == 0
}

func (s *Store) device(ip, ua string) Device {
	browser, os := ParseUA(ua)
	d := Device{Browser: browser, OS: os, IP: ip}
	place := "?"
	if p := net.ParseIP(ip); p != nil {
		if s.IsLocal(p) {
			d.Local, place = true, "local"
		} else if v4 := p.To4(); v4 != nil {
			place = v4.Mask(net.CIDRMask(24, 32)).String() + "/24"
		} else {
			place = p.Mask(net.CIDRMask(48, 128)).String() + "/48"
		}
	}
	d.Key = strings.ToLower(browser + "|" + os + "|" + place)
	return d
}

// Learn registra um acesso sem considerá-lo novo (primeira configuração, sessões antigas).
func (s *Store) Learn(ip, ua string, at time.Time) {
	s.seen(ip, ua, at)
}

// Seen registra um login. isNew = este navegador/sistema nunca entrou desta rede.
func (s *Store) Seen(ip, ua string) (Device, bool) {
	return s.seen(ip, ua, s.Now())
}

func (s *Store) seen(ip, ua string, at time.Time) (Device, bool) {
	d := s.device(ip, ua)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices {
		if s.devices[i].Key == d.Key {
			e := &s.devices[i]
			e.IP, e.Count = ip, e.Count+1
			if at.Unix() > e.Last {
				e.Last = at.Unix()
			}
			s.saveLocked()
			return *e, false
		}
	}
	d.First, d.Last, d.Count = at.Unix(), at.Unix(), 1
	s.devices = append(s.devices, d)
	if len(s.devices) > maxDevices { // esquece os que não aparecem há mais tempo
		sort.Slice(s.devices, func(i, j int) bool { return s.devices[i].Last > s.devices[j].Last })
		s.devices = s.devices[:maxDevices]
	}
	s.saveLocked()
	return d, true
}

func (s *Store) saveLocked() {
	s.existed = true
	if s.File == "" {
		return
	}
	if err := store.WriteJSON(s.File, file{Version: 1, Devices: s.devices}, 0o600); err != nil {
		slog.Error("acessos conhecidos: não foi possível gravar", "erro", err)
	}
}

// BlockAlertDue diz se já pode avisar de novo sobre bloqueios deste endereço.
func (s *Store) BlockAlertDue(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	if t, ok := s.blocks[ip]; ok && now.Sub(t) < blockEvery {
		return false
	}
	s.blocks[ip] = now
	if len(s.blocks) > 1000 {
		for k, t := range s.blocks {
			if now.Sub(t) >= blockEvery {
				delete(s.blocks, k)
			}
		}
	}
	return true
}

var tool = regexp.MustCompile(`^([A-Za-z][\w.-]{0,30})/`)

// ParseUA reconhece o navegador e o sistema pelo User-Agent (sem precisão de versão: só o
// suficiente para "Chrome no Android").
func ParseUA(ua string) (browser, os string) {
	has := func(s string) bool { return strings.Contains(ua, s) }
	switch {
	case has("Edg/") || has("EdgA/") || has("EdgiOS/"):
		browser = "Edge"
	case has("OPR/") || has("Opera"):
		browser = "Opera"
	case has("SamsungBrowser/"):
		browser = "Samsung Internet"
	case has("Firefox/") || has("FxiOS/"):
		browser = "Firefox"
	case has("Chrome/") || has("CriOS/") || has("Chromium/"):
		browser = "Chrome"
	case has("Safari/"):
		browser = "Safari"
	default:
		if m := tool.FindStringSubmatch(ua); m != nil && !strings.HasPrefix(ua, "Mozilla/") {
			browser = m[1] // curl, python-requests, Go-http-client…
		} else {
			browser = "Navegador desconhecido"
		}
	}
	switch {
	case has("Windows"):
		os = "Windows"
	case has("Android"):
		os = "Android"
	case has("iPhone"):
		os = "iPhone"
	case has("iPad"):
		os = "iPad"
	case has("CrOS"):
		os = "ChromeOS"
	case has("Mac OS X") || has("Macintosh"):
		os = "macOS"
	case has("Linux"):
		os = "Linux"
	}
	return browser, os
}
