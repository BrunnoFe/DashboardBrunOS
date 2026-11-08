package logins

import (
	"path/filepath"
	"testing"
	"time"
)

const (
	chromeWin  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
	edgeWin    = chromeWin + " Edg/130.0.0.0"
	safariIOS  = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"
	chromeAndr = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36"
	firefoxLin = "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0"
)

func TestParseUA(t *testing.T) {
	cases := []struct{ ua, b, os string }{
		{chromeWin, "Chrome", "Windows"},
		{edgeWin, "Edge", "Windows"},
		{safariIOS, "Safari", "iPhone"},
		{chromeAndr, "Chrome", "Android"},
		{firefoxLin, "Firefox", "Linux"},
		{"curl/8.5.0", "curl", ""},
		{"", "Navegador desconhecido", ""},
	}
	for _, c := range cases {
		b, os := ParseUA(c.ua)
		if b != c.b || os != c.os {
			t.Errorf("%q → %q/%q, esperava %q/%q", c.ua, b, os, c.b, c.os)
		}
	}
}

func TestSeen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logins.json")
	s := New(path)
	if !s.Fresh() {
		t.Fatal("sem arquivo deveria ser Fresh")
	}
	s.Learn("192.168.0.10", chromeWin, time.Now())
	if s.Fresh() {
		t.Fatal("depois de aprender não é mais Fresh")
	}

	// mesmo navegador, outro IP da rede local: conhecido
	if d, isNew := s.Seen("192.168.0.77", chromeWin); isNew || d.IP != "192.168.0.77" || d.Count != 2 {
		t.Fatalf("rede local com outro IP não é novo: %+v %v", d, isNew)
	}
	// outro navegador na rede local: novo
	if d, isNew := s.Seen("192.168.0.10", edgeWin); !isNew || d.Describe() != "Edge no Windows" || d.Where() != "pela rede local (192.168.0.10)" {
		t.Fatalf("Edge deveria ser novo: %+v %v", d, isNew)
	}
	// internet: mesma faixa /24 é conhecida, outra faixa é nova
	if _, isNew := s.Seen("200.10.20.5", chromeAndr); !isNew {
		t.Fatal("primeiro acesso pela internet deveria ser novo")
	}
	if _, isNew := s.Seen("200.10.20.99", chromeAndr); isNew {
		t.Fatal("mesma faixa /24 não é nova")
	}
	if d, isNew := s.Seen("201.1.1.1", chromeAndr); !isNew || d.Where() != "pela internet (201.1.1.1)" {
		t.Fatalf("outra faixa deveria ser nova: %+v", d)
	}

	// relê do disco
	s2 := New(path)
	if s2.Fresh() {
		t.Fatal("arquivo existente não é Fresh")
	}
	if _, isNew := s2.Seen("192.168.0.10", edgeWin); isNew {
		t.Fatal("não gravou os aparelhos")
	}
}

func TestBlockAlertDue(t *testing.T) {
	s := New("")
	now := time.Unix(1_800_000_000, 0)
	s.Now = func() time.Time { return now }
	if !s.BlockAlertDue("1.2.3.4") {
		t.Fatal("primeiro bloqueio avisa")
	}
	if s.BlockAlertDue("1.2.3.4") {
		t.Fatal("não repete logo em seguida")
	}
	if !s.BlockAlertDue("5.6.7.8") {
		t.Fatal("outro endereço avisa")
	}
	now = now.Add(blockEvery)
	if !s.BlockAlertDue("1.2.3.4") {
		t.Fatal("depois de 6 h avisa de novo")
	}
}

func TestLimit(t *testing.T) {
	s := New("")
	base := time.Unix(1_800_000_000, 0)
	for i := 0; i < maxDevices+5; i++ {
		s.Learn("10.0.0.1", "agente"+string(rune('a'+i%26))+string(rune('a'+i/26))+"/1", base.Add(time.Duration(i)*time.Second))
	}
	if len(s.devices) != maxDevices {
		t.Fatalf("guardou %d aparelhos", len(s.devices))
	}
}
