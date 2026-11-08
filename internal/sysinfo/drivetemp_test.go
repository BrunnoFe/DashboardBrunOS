package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, p, v string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sysfs falso: sda (HD, drivetemp) e nvme0n1 (NVMe).
func fakeSys(t *testing.T) string {
	sys := t.TempDir()
	write(t, filepath.Join(sys, "devices/scsi/0:0:0:0/block/sda/size"), "1")
	write(t, filepath.Join(sys, "class/hwmon/hwmon0/name"), "drivetemp")
	write(t, filepath.Join(sys, "class/hwmon/hwmon0/temp1_input"), "38000")
	if err := os.Symlink(filepath.Join(sys, "devices/scsi/0:0:0:0"), filepath.Join(sys, "class/hwmon/hwmon0/device")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(sys, "devices/pci/nvme/nvme0/nvme0n1/size"), "1")
	write(t, filepath.Join(sys, "class/hwmon/hwmon1/name"), "nvme")
	write(t, filepath.Join(sys, "class/hwmon/hwmon1/temp1_input"), "45850")
	if err := os.Symlink(filepath.Join(sys, "devices/pci/nvme/nvme0"), filepath.Join(sys, "class/hwmon/hwmon1/device")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(sys, "block/sda/stat"), "100 0 0 0 50 0 0 0 0 0 0")
	write(t, filepath.Join(sys, "block/nvme0n1/stat"), "1 0 0 0 1 0 0 0 0 0 0")
	return sys
}

func TestDriveTemps(t *testing.T) {
	sys := fakeSys(t)
	c := &Collector{cfg: Config{Sys: sys}, temps: map[string]*diskTemp{}}
	disks := func() []Disk {
		return []Disk{{Name: "HD", Device: "/dev/sda", Rotational: true}, {Name: "NVMe", Device: "/dev/nvme0n1p2"}}
	}
	t0 := time.Unix(1_800_000_000, 0)

	d := disks()
	c.fillTemps(d, t0)
	if d[0].Temp == nil || *d[0].Temp != 38 || d[1].Temp == nil || *d[1].Temp != 45.9 {
		t.Fatalf("temperaturas: %+v %+v", d[0], d[1])
	}

	// HD parado (stat igual): não lê de novo, mesmo depois de muito tempo
	write(t, filepath.Join(sys, "class/hwmon/hwmon0/temp1_input"), "41000")
	write(t, filepath.Join(sys, "class/hwmon/hwmon1/temp1_input"), "50000")
	d = disks()
	c.fillTemps(d, t0.Add(10*time.Minute))
	if *d[0].Temp != 38 || d[0].TempAt != t0.Unix() {
		t.Fatalf("HD parado foi lido de novo: %v", *d[0].Temp)
	}
	if *d[1].Temp != 50 {
		t.Fatalf("NVMe deveria ter sido lido de novo: %v", *d[1].Temp)
	}

	// HD com atividade, mas antes de 1 minuto: ainda não
	write(t, filepath.Join(sys, "block/sda/stat"), "120 0 0 0 50 0 0 0 0 0 0")
	d = disks()
	c.fillTemps(d, t0.Add(10*time.Minute+30*time.Second))
	if *d[0].Temp != 38 {
		t.Fatalf("leu antes da hora")
	}
	// passou o minuto e o disco trabalhou: lê
	d = disks()
	c.fillTemps(d, t0.Add(12*time.Minute))
	if *d[0].Temp != 41 {
		t.Fatalf("HD ativo não foi lido: %v", *d[0].Temp)
	}
}

func TestHwmonSkipsDrivetemp(t *testing.T) {
	sys := fakeSys(t)
	sensors, _, _ := readHwmon(sys)
	for _, s := range sensors {
		if s.Chip == "drivetemp" {
			t.Fatal("readHwmon (a cada 2 s) não pode ler o drivetemp")
		}
	}
}
