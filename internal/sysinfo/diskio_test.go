package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

const stats1 = `   8       0 sda 100 0 2000 50 200 0 4000 80 0 500 130 0 0 0 0
   8       1 sda1 90 0 1800 40 190 0 3800 70 0 400 110 0 0 0 0
 259       0 nvme0n1 10 0 100 5 20 0 200 8 0 30 13 0 0 0 0
   7       0 loop0 5 0 10 1 0 0 0 0 0 1 1 0 0 0 0
`

const stats2 = `   8       0 sda 120 0 4048 60 260 0 8096 90 0 1500 150 0 0 0 0
   8       1 sda1 110 0 3800 50 250 0 7800 80 0 1400 130 0 0 0 0
 259       0 nvme0n1 10 0 100 5 20 0 200 8 0 30 13 0 0 0 0
`

func TestDiskRates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "diskstats")
	os.WriteFile(p, []byte(stats1), 0o644)
	a := readDiskstats(p)
	if len(a) != 2 { // sda e nvme0n1 (partição e loop ficam de fora)
		t.Fatalf("discos inteiros: %v", a)
	}
	os.WriteFile(p, []byte(stats2), 0o644)
	b := readDiskstats(p)
	r := diskRates(a, b, 2)
	if len(r) != 2 || r[0].Name != "nvme0n1" || r[1].Name != "sda" {
		t.Fatalf("ordem/nomes: %+v", r)
	}
	sda := r[1]
	// 2048 setores lidos em 2 s = 524288 B/s; 4096 escritos = 1048576 B/s; 1000 ms de E/S em 2 s = 50 %
	if sda.Read != 524288 || sda.Write != 1048576 || sda.Busy != 50 {
		t.Fatalf("sda: %+v", sda)
	}
	if r[0].Read != 0 || r[0].Busy != 0 {
		t.Fatalf("nvme parado: %+v", r[0])
	}
	// contador que voltou (disco reconectado): espera a próxima
	if got := diskRates(b, a, 2); len(got) != 1 || got[0].Name != "nvme0n1" {
		t.Fatalf("contador zerado não pode virar número absurdo: %+v", got)
	}
	if diskRates(nil, b, 2) != nil {
		t.Fatal("sem leitura anterior: nada")
	}
}
