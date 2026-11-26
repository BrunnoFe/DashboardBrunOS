package sysinfo

// Placa de vídeo (0.36.0, decisão 73): Métricas da fonte BrunOS, lidas pelo sistema sem privilégio.
//
//   - Intel (i915): frequência atual e máxima (gt_act/cur_freq_mhz, gt_max_freq_mhz) e uso ESTIMADO pelo
//     tempo em que a GPU dorme (power/rc6_residency_ms: o que não é sono é trabalho). Memória de vídeo
//     não existe (a integrada usa a RAM). Temperatura: o sensor "GPU" da placa-mãe, se houver (Dell).
//   - AMD (amdgpu): uso (gpu_busy_percent), memória de vídeo (mem_info_vram_*), temperatura, consumo e
//     frequência pelo hwmon da própria placa.
//   - Consumo da Intel (RAPL "uncore", que só o root lê) e NVIDIA (nvidia-smi, que não existe dentro do
//     container) vêm do script do servidor: SetGPUHost, chamado pelo main uma vez por minuto.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GPU é uma placa de vídeo. Campos nil = o driver não informa.
type GPU struct {
	ID        string   `json:"id"`     // card0, nvidia0
	Vendor    string   `json:"vendor"` // intel | amd | nvidia
	Name      string   `json:"name"`
	Busy      *float64 `json:"busy,omitempty"` // % de uso
	BusyEst   bool     `json:"busyEst,omitempty"`
	Freq      *float64 `json:"freq,omitempty"`    // MHz agora
	FreqMax   *float64 `json:"freqMax,omitempty"` // MHz máximo
	VramUsed  *float64 `json:"vramUsed,omitempty"`
	VramTotal *float64 `json:"vramTotal,omitempty"`
	Temp      *float64 `json:"temp,omitempty"`  // °C
	Power     *float64 `json:"power,omitempty"` // W
}

// GPUHost é o que o script do servidor mediu (SetGPUHost).
type GPUHost struct {
	UncoreUJ, UncoreMax uint64 // contador do RAPL "uncore" (GPU integrada da Intel); 0 = não tem
	Nvidia              []GPU  // uma por placa NVIDIA
	At                  time.Time
}

type gpuState struct {
	mu       sync.Mutex
	rc6      map[string][2]int64 // card → [rc6 ms, quando (ms)]
	host     GPUHost
	prevUJ   uint64
	prevAt   time.Time
	intelPow *float64 // W, do último par de leituras do uncore
}

var cardRe = regexp.MustCompile(`^card\d+$`)

// SetGPUHost recebe a leitura do script (main). Calcula o consumo da Intel pela diferença do contador.
func (c *Collector) SetGPUHost(h GPUHost) {
	g := &c.gpu
	g.mu.Lock()
	defer g.mu.Unlock()
	if h.UncoreUJ > 0 && !g.prevAt.IsZero() && h.At.After(g.prevAt) {
		d := float64(h.UncoreUJ) - float64(g.prevUJ)
		if d < 0 && h.UncoreMax > 0 { // o contador deu a volta
			d += float64(h.UncoreMax)
		}
		if w := d / 1e6 / h.At.Sub(g.prevAt).Seconds(); d >= 0 && w < 500 {
			w = round1(w)
			g.intelPow = &w
		}
	}
	if h.UncoreUJ == 0 {
		g.intelPow = nil
	}
	g.prevUJ, g.prevAt, g.host = h.UncoreUJ, h.At, h
}

// readGPUs lê as placas do /sys e junta o que veio do script.
func (c *Collector) readGPUs(sensors []Sensor, now time.Time) []GPU {
	g := &c.gpu
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.rc6 == nil {
		g.rc6 = map[string][2]int64{}
	}
	var out []GPU
	cards, _ := filepath.Glob(filepath.Join(c.cfg.Sys, "class/drm/card*"))
	sort.Strings(cards)
	for _, card := range cards {
		id := filepath.Base(card)
		if !cardRe.MatchString(id) {
			continue // conectores (card0-HDMI-A-1)
		}
		drv := ""
		if l, err := os.Readlink(filepath.Join(card, "device/driver")); err == nil {
			drv = filepath.Base(l)
		}
		switch drv {
		case "i915", "xe":
			out = append(out, g.intel(card, id, sensors, now))
		case "amdgpu":
			out = append(out, amd(card, id))
		}
	}
	// NVIDIA pelo script; leitura com mais de 3 min é velha demais (o script parou de responder)
	if now.Sub(g.host.At) < 3*time.Minute {
		out = append(out, g.host.Nvidia...)
	}
	return out
}

// ParseGPUHost lê a resposta do pedido "gpu" do script: "uncore <µJ> <máx>", "nvidia <csv do
// nvidia-smi>" (índice, nome, uso %, memória usada e total em MiB, °C, W, MHz agora, MHz máx.) e "nada".
func ParseGPUHost(out []byte, at time.Time) GPUHost {
	h := GPUHost{At: at}
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		switch {
		case len(f) == 3 && f[0] == "uncore":
			h.UncoreUJ, _ = strconv.ParseUint(f[1], 10, 64)
			h.UncoreMax, _ = strconv.ParseUint(f[2], 10, 64)
		case len(f) > 1 && f[0] == "nvidia":
			c := strings.Split(strings.TrimSpace(strings.TrimPrefix(ln, "nvidia")), ",")
			if len(c) < 9 {
				continue
			}
			num := func(i int) *float64 { // "[N/A]" e afins = sem valor
				v, err := strconv.ParseFloat(strings.TrimSpace(c[i]), 64)
				if err != nil {
					return nil
				}
				return &v
			}
			mib := func(i int) *float64 {
				if v := num(i); v != nil {
					b := *v * 1024 * 1024
					return &b
				}
				return nil
			}
			name := strings.TrimSpace(c[1])
			if len(name) > 80 {
				name = name[:80]
			}
			h.Nvidia = append(h.Nvidia, GPU{ID: "nvidia" + strings.TrimSpace(c[0]), Vendor: "nvidia", Name: name,
				Busy: num(2), VramUsed: mib(3), VramTotal: mib(4), Temp: num(5), Power: num(6), Freq: num(7), FreqMax: num(8)})
		}
	}
	return h
}

func fp(v float64) *float64 { return &v }

func readF(p string) (float64, bool) {
	v, ok := readInt(p)
	return float64(v), ok
}

func (g *gpuState) intel(card, id string, sensors []Sensor, now time.Time) GPU {
	x := GPU{ID: id, Vendor: "intel", Name: "Intel (integrada)"}
	for _, f := range []string{"gt_act_freq_mhz", "gt_cur_freq_mhz"} {
		if v, ok := readF(filepath.Join(card, f)); ok {
			x.Freq = fp(v)
			break
		}
	}
	for _, f := range []string{"gt_max_freq_mhz", "gt_RP0_freq_mhz"} {
		if v, ok := readF(filepath.Join(card, f)); ok && v > 0 {
			x.FreqMax = fp(v)
			break
		}
	}
	// uso estimado: 100 − % do tempo em sono profundo (RC6) desde a última amostra
	if ms, ok := readInt(filepath.Join(card, "power/rc6_residency_ms")); ok {
		at := now.UnixMilli()
		if p, had := g.rc6[id]; had && at > p[1] {
			idle := float64(ms-p[0]) / float64(at-p[1]) * 100
			busy := round1(100 - max(0, min(100, idle)))
			x.Busy, x.BusyEst = &busy, true
		}
		g.rc6[id] = [2]int64{ms, at}
	}
	for _, s := range sensors { // sensor "GPU" da placa-mãe (ex.: dell_smm)
		if strings.Contains(strings.ToLower(s.Label), "gpu") {
			x.Temp = fp(s.Value)
			break
		}
	}
	x.Power = g.intelPow
	if n := gpuName(card); n != "" {
		x.Name = n
	}
	return x
}

func amd(card, id string) GPU {
	dev := filepath.Join(card, "device")
	x := GPU{ID: id, Vendor: "amd", Name: "AMD"}
	if n := gpuName(card); n != "" {
		x.Name = n
	}
	if v, ok := readF(filepath.Join(dev, "gpu_busy_percent")); ok {
		x.Busy = fp(v)
	}
	if v, ok := readF(filepath.Join(dev, "mem_info_vram_used")); ok {
		x.VramUsed = fp(v)
	}
	if v, ok := readF(filepath.Join(dev, "mem_info_vram_total")); ok && v > 0 {
		x.VramTotal = fp(v)
	}
	hw, _ := filepath.Glob(filepath.Join(dev, "hwmon/hwmon*"))
	if len(hw) > 0 {
		h := hw[0]
		if v, ok := readF(filepath.Join(h, "temp1_input")); ok && v > 0 {
			x.Temp = fp(round1(v / 1000))
		}
		for _, f := range []string{"power1_average", "power1_input"} {
			if v, ok := readF(filepath.Join(h, f)); ok && v > 0 {
				x.Power = fp(round1(v / 1e6))
				break
			}
		}
		if v, ok := readF(filepath.Join(h, "freq1_input")); ok && v > 0 {
			x.Freq = fp(round1(v / 1e6))
		}
	}
	return x
}

// gpuName: o nome do modelo, quando o sistema informa (label do dispositivo); senão, "".
func gpuName(card string) string {
	for _, f := range []string{"device/label", "device/product_name"} {
		if s := strings.TrimSpace(readFile(filepath.Join(card, f))); s != "" && len(s) < 80 {
			return s
		}
	}
	return ""
}
