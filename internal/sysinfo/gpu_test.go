package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeGPU monta /sys/class/drm/<card> com o driver apontando para drv e os arquivos dados.
func fakeGPU(t *testing.T, sys, card, drv string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(sys, "class/drm", card)
	for p, v := range files {
		write(t, filepath.Join(dir, p), v)
	}
	if err := os.MkdirAll(filepath.Join(sys, "bus/pci/drivers", drv), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "device"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sys, "bus/pci/drivers", drv), filepath.Join(dir, "device/driver")); err != nil {
		t.Fatal(err)
	}
}

func TestReadGPUsIntelAMD(t *testing.T) {
	sys := t.TempDir()
	fakeGPU(t, sys, "card0", "i915", map[string]string{"gt_act_freq_mhz": "350", "gt_max_freq_mhz": "1100", "power/rc6_residency_ms": "1000"})
	fakeGPU(t, sys, "card1", "amdgpu", map[string]string{"device/gpu_busy_percent": "42", "device/mem_info_vram_used": "1073741824",
		"device/mem_info_vram_total": "8589934592", "device/hwmon/hwmon3/temp1_input": "61000", "device/hwmon/hwmon3/power1_average": "45500000",
		"device/hwmon/hwmon3/freq1_input": "1800000000"})
	write(t, filepath.Join(sys, "class/drm/card0-HDMI-A-1/status"), "connected") // conector: não é placa
	c := &Collector{cfg: Config{Sys: sys}}
	sensors := []Sensor{{Chip: "dell_smm", Label: "CPU", Value: 55}, {Chip: "dell_smm", Label: "GPU", Value: 48}}

	t0 := time.Unix(1_800_000_000, 0)
	g := c.readGPUs(sensors, t0)
	if len(g) != 2 {
		t.Fatalf("placas = %+v", g)
	}
	in, am := g[0], g[1]
	if in.Vendor != "intel" || *in.Freq != 350 || *in.FreqMax != 1100 || *in.Temp != 48 || in.Busy != nil || in.VramTotal != nil {
		t.Fatalf("intel (1ª leitura, sem uso ainda): %+v", in)
	}
	if am.Vendor != "amd" || *am.Busy != 42 || *am.VramUsed != 1073741824 || *am.VramTotal != 8589934592 || *am.Temp != 61 || *am.Power != 45.5 || *am.Freq != 1800 {
		t.Fatalf("amd: %+v", am)
	}
	// 2 s depois, dormiu 1,5 s: 25 % de uso estimado
	write(t, filepath.Join(sys, "class/drm/card0/power/rc6_residency_ms"), "2500")
	g = c.readGPUs(sensors, t0.Add(2*time.Second))
	if g[0].Busy == nil || *g[0].Busy != 25 || !g[0].BusyEst {
		t.Fatalf("uso estimado: %+v", g[0])
	}

	// consumo da Intel pelo script: 120 J em 1 min = 2 W
	c.SetGPUHost(GPUHost{UncoreUJ: 1_000_000, UncoreMax: 262_143_328_850, At: t0})
	c.SetGPUHost(GPUHost{UncoreUJ: 121_000_000, UncoreMax: 262_143_328_850, At: t0.Add(time.Minute)})
	g = c.readGPUs(sensors, t0.Add(time.Minute))
	if g[0].Power == nil || *g[0].Power != 2 {
		t.Fatalf("consumo da Intel: %+v", g[0].Power)
	}
}

func TestParseGPUHost(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	h := ParseGPUHost([]byte("uncore 123456 262143328850\nnvidia 0, NVIDIA GeForce RTX 3060, 37, 2048, 12288, 55, 98.40, 1650, 1900\nnvidia 1, Placa velha, [N/A], 100, 2048, 40, [N/A], 300, 1000\nnada\n"), now)
	if h.UncoreUJ != 123456 || h.UncoreMax != 262143328850 || len(h.Nvidia) != 2 {
		t.Fatalf("leitura: %+v", h)
	}
	n := h.Nvidia[0]
	if n.ID != "nvidia0" || n.Name != "NVIDIA GeForce RTX 3060" || *n.Busy != 37 || *n.VramUsed != 2048*1024*1024 || *n.Temp != 55 || *n.Power != 98.4 || *n.FreqMax != 1900 {
		t.Fatalf("nvidia 0: %+v", n)
	}
	if h.Nvidia[1].Busy != nil || h.Nvidia[1].Power != nil || *h.Nvidia[1].Freq != 300 {
		t.Fatalf("[N/A] deveria ficar vazio: %+v", h.Nvidia[1])
	}
	if e := ParseGPUHost([]byte("nada\n"), now); e.UncoreUJ != 0 || len(e.Nvidia) != 0 {
		t.Fatalf("sem nada: %+v", e)
	}
	// NVIDIA do script entra na lista enquanto a leitura é recente
	c := &Collector{cfg: Config{Sys: t.TempDir()}}
	c.SetGPUHost(h)
	if g := c.readGPUs(nil, now.Add(time.Minute)); len(g) != 2 || g[0].Vendor != "nvidia" {
		t.Fatalf("nvidia na lista: %+v", g)
	}
	if g := c.readGPUs(nil, now.Add(10*time.Minute)); len(g) != 0 {
		t.Fatalf("leitura velha deveria sair: %+v", g)
	}
}
