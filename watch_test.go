package main

import (
	"strings"
	"testing"
	"time"

	"brunos/internal/events"
	"brunos/internal/files"
	"brunos/internal/logins"
	"brunos/internal/sysinfo"
)

func TestCheckDisks(t *testing.T) {
	ev := events.New("")
	hd := files.DiskInfo{Name: "Disco1", Mapped: true, Mounted: true}
	other := files.DiskInfo{Name: "SSD", Mapped: false, Mounted: false} // não mapeado: nunca alerta

	checkDisks([]files.DiskInfo{hd, other}, ev)
	if l, _ := ev.List(); len(l) != 0 {
		t.Fatalf("tudo montado não deveria avisar: %+v", l)
	}
	hd.Mounted = false
	checkDisks([]files.DiskInfo{hd, other}, ev)
	checkDisks([]files.DiskInfo{hd, other}, ev) // de novo: não repete
	l, _ := ev.List()
	if len(l) != 1 || l[0].Level != events.Critico || l[0].Kind != "disk-unmounted" {
		t.Fatalf("esperava um alerta crítico: %+v", l)
	}
	hd.Mounted = true
	checkDisks([]files.DiskInfo{hd, other}, ev)
	l, _ = ev.List()
	if len(l) != 2 || l[0].Kind != "disk-mounted" {
		t.Fatalf("esperava o aviso de volta: %+v", l)
	}
	// desmontou e depois deixou de ser mapeado: fecha sem aviso de "voltou"
	hd.Mounted = false
	checkDisks([]files.DiskInfo{hd}, ev)
	hd.Mapped = false
	checkDisks([]files.DiskInfo{hd}, ev)
	l, _ = ev.List()
	if len(l) != 3 || ev.IsActive("disk-unmounted:Disco1") {
		t.Fatalf("desmapear deveria só encerrar o alerta: %+v", l)
	}
}

func TestCheckHot(t *testing.T) {
	ev := events.New("")
	temp := func(v float64) *float64 { return &v }
	hd := sysinfo.Disk{Name: "Disco1", Rotational: true, Temp: temp(45)}
	ssd := sysinfo.Disk{Name: "Sistema", Temp: temp(60)} // 60 °C é normal para SSD
	none := sysinfo.Disk{Name: "Sem sensor"}

	checkHot([]sysinfo.Disk{hd, ssd, none}, ev)
	if l, _ := ev.List(); len(l) != 0 {
		t.Fatalf("temperaturas normais não avisam: %+v", l)
	}
	hd.Temp = temp(56)
	checkHot([]sysinfo.Disk{hd, ssd}, ev)
	checkHot([]sysinfo.Disk{hd, ssd}, ev)
	l, _ := ev.List()
	if len(l) != 1 || l[0].Kind != "disk-hot" || l[0].Title != "O disco Disco1 está quente (56 °C)" {
		t.Fatalf("esperava um aviso de HD quente: %+v", l)
	}
	hd.Temp = temp(52) // entre 50 e 55: continua aberto
	checkHot([]sysinfo.Disk{hd}, ev)
	if !ev.IsActive("disk-hot:Disco1") {
		t.Fatal("ainda não esfriou o bastante")
	}
	hd.Temp = temp(48)
	checkHot([]sysinfo.Disk{hd}, ev)
	if l, _ = ev.List(); len(l) != 2 || l[0].Kind != "disk-cool" {
		t.Fatalf("esperava aviso de que esfriou: %+v", l)
	}
}

func TestLoginAlerts(t *testing.T) {
	s := &server{events: events.New(""), logins: logins.New("")}
	s.logins.IsLocal = isPrivate
	const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
	s.logins.Learn("192.168.0.10", ua, time.Now())

	s.loginSeen("192.168.0.20", ua) // mesmo navegador, rede local: nada
	if l, _ := s.events.List(); len(l) != 0 {
		t.Fatalf("aparelho conhecido não avisa: %+v", l)
	}
	s.loginSeen("200.1.2.3", ua) // pela internet: aviso
	l, _ := s.events.List()
	if len(l) != 1 || l[0].Kind != "login-new" || l[0].Level != events.Aviso || l[0].Title != "Novo acesso: Chrome no Windows" {
		t.Fatalf("esperava aviso de acesso novo: %+v", l)
	}

	s.loginBlocked("200.9.9.9", 2*time.Minute)
	s.loginBlocked("200.9.9.9", 4*time.Minute) // não repete em seguida
	l, _ = s.events.List()
	if len(l) != 2 || l[0].Kind != "login-blocked" || !strings.Contains(l[0].Body, "bloqueado por 2 minutos") {
		t.Fatalf("esperava um aviso de bloqueio: %+v", l)
	}
}
