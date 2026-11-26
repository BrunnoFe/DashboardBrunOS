package integrations

import (
	"path/filepath"
	"testing"

	"brunos/internal/events"
)

// 0.39.0: o sino avisa uma vez de app compatível instalado depois; os que já existiam na primeira
// conferência não avisam, nem os de um Tipo que já tem Integração.
func TestAppWatch(t *testing.T) {
	dir := t.TempDir()
	ev := events.New("")
	st := NewStore("", testBox(t))
	pass := "senha"
	if _, err := st.Add(Input{Kind: "adguard", URL: "http://10.0.0.2:3000", User: "admin", Secret: &pass}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "integrations-seen.json")
	w := &AppWatch{File: file, Events: ev, Store: st}
	if n := w.Check([]AppRef{{ID: "plex", Name: "Plex"}, {ID: "homarr", Name: "Homarr"}}); n != 0 {
		t.Fatalf("primeira conferência avisou %d", n)
	}
	// instalou o Transmission (compatível, sem Integração) e o AdGuard (já tem Integração)
	apps := []AppRef{{ID: "plex"}, {ID: "homarr"}, {ID: "transmission", Name: "Transmission"}, {ID: "adguard", Name: "AdGuard Home"}}
	if n := w.Check(apps); n != 1 {
		t.Fatalf("avisos: %d", n)
	}
	list, _ := ev.List()
	if len(list) != 1 || list[0].Kind != "integration-suggest" || list[0].Title != "Transmission pode ser conectado" || list[0].Meta["kind"] != "transmission" {
		t.Fatalf("aviso: %+v", list)
	}
	if n := w.Check(apps); n != 0 {
		t.Fatalf("repetiu: %d", n)
	}
	// depois de reiniciar o BrunOS, lembra o que já viu
	w2 := &AppWatch{File: file, Events: ev, Store: st}
	if n := w2.Check(append(apps, AppRef{ID: "syncthing", Name: "Syncthing"})); n != 1 {
		t.Fatalf("depois de reiniciar: %d", n)
	}
	if KindOfApp("transmission-openvpn") == nil || KindOfApp("homarr") != nil {
		t.Fatal("KindOfApp")
	}
}
