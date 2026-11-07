package main

import (
	"testing"

	"brunos/internal/events"
	"brunos/internal/files"
)

func TestCheckDisks(t *testing.T) {
	ev := events.New("")
	hd := files.DiskInfo{Name: "1TB_HDD", Mapped: true, Mounted: true}
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
	if len(l) != 3 || ev.IsActive("disk-unmounted:1TB_HDD") {
		t.Fatalf("desmapear deveria só encerrar o alerta: %+v", l)
	}
}
