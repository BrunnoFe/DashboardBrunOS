package sysinfo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Intervalos: rápido enquanto alguém está olhando, lento quando ninguém está.
const (
	activeInterval = 2 * time.Second
	idleInterval   = 15 * time.Second
	diskInterval   = 20 * time.Second
	historySize    = 150 // ~5 min no ritmo rápido
)

type Point struct {
	T    int64    `json:"t"`
	CPU  float64  `json:"cpu"`
	Mem  float64  `json:"mem"`
	Temp *float64 `json:"temp,omitempty"`
	Rx   float64  `json:"rx"`
	Tx   float64  `json:"tx"`
}

type CPU struct {
	Percent float64    `json:"percent"`
	Cores   int        `json:"cores"`
	Model   string     `json:"model"`
	Load    [3]float64 `json:"load"`
}

type Net struct {
	Rx      float64  `json:"rx"` // bytes/s
	Tx      float64  `json:"tx"`
	RxTotal uint64   `json:"rxTotal"`
	TxTotal uint64   `json:"txTotal"`
	Ifaces  []string `json:"ifaces"`
}

type Snapshot struct {
	T       int64    `json:"t"`
	CPU     CPU      `json:"cpu"`
	Mem     memInfo  `json:"mem"`
	Temp    *float64 `json:"temp"`
	Sensors []Sensor `json:"sensors"`
	FanRPM  *int     `json:"fanRpm"`
	Battery *Battery `json:"battery"`
	Disks   []Disk   `json:"disks"`
	Net     Net      `json:"net"`
	Uptime  float64  `json:"uptime"`
}

type Collector struct {
	cfg     Config
	info    Info
	netPath string

	mu      sync.RWMutex
	latest  Snapshot
	history []Point
	subs    map[chan []byte]struct{}

	prevCPU cpuTimes
	prevNet netCounters
	prevAt  time.Time
	disks   []Disk
	disksAt time.Time
	wake    chan struct{}
}

func NewCollector(cfg Config) *Collector {
	c := &Collector{cfg: cfg, subs: map[chan []byte]struct{}{}, wake: make(chan struct{}, 1)}
	c.info = readInfo(cfg)
	// rede do host: /host/proc/1/net/dev mostra as interfaces do namespace do host
	c.netPath = filepath.Join(cfg.Proc, "net/dev")
	if cfg.HostProc != "" {
		if p := filepath.Join(cfg.HostProc, "1/net/dev"); fileReadable(p) {
			c.netPath = p
		}
	}
	c.prevCPU, _ = readCPU(cfg.Proc)
	c.prevNet, _ = readNet(c.netPath)
	c.prevAt = time.Now()
	return c
}

func fileReadable(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func (c *Collector) Info() Info { return c.info }

func (c *Collector) Run(ctx context.Context) {
	time.Sleep(300 * time.Millisecond) // primeira amostra precisa de um intervalo para o delta da CPU
	for {
		c.sample()
		c.mu.RLock()
		n := len(c.subs)
		c.mu.RUnlock()
		interval := idleInterval
		if n > 0 {
			interval = activeInterval
		}
		t := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-c.wake:
			t.Stop()
		case <-t.C:
		}
	}
}

func (c *Collector) sample() {
	now := time.Now()
	dt := now.Sub(c.prevAt).Seconds()
	if dt <= 0 {
		dt = 1
	}
	s := Snapshot{T: now.Unix()}

	if cur, ok := readCPU(c.cfg.Proc); ok {
		dTotal := float64(cur.total - c.prevCPU.total)
		dIdle := float64(cur.idle - c.prevCPU.idle)
		if dTotal > 0 {
			s.CPU.Percent = round1(100 * (1 - dIdle/dTotal))
		}
		c.prevCPU = cur
	}
	s.CPU.Cores, s.CPU.Model, s.CPU.Load = c.info.Cores, c.info.CPUModel, readLoad(c.cfg.Proc)
	s.Mem = readMem(c.cfg.Proc)
	s.Sensors, s.Temp, s.FanRPM = readHwmon(c.cfg.Sys)
	s.Battery = readBattery(c.cfg.Sys)
	s.Uptime = readUptime(c.cfg.Proc)

	if n, ok := readNet(c.netPath); ok {
		if n.rx >= c.prevNet.rx && n.tx >= c.prevNet.tx {
			s.Net.Rx = round1(float64(n.rx-c.prevNet.rx) / dt)
			s.Net.Tx = round1(float64(n.tx-c.prevNet.tx) / dt)
		}
		s.Net.RxTotal, s.Net.TxTotal, s.Net.Ifaces = n.rx, n.tx, n.ifaces
		c.prevNet = n
	}
	c.prevAt = now

	if now.Sub(c.disksAt) > diskInterval || c.disks == nil {
		c.disks = readDisks(c.cfg.Proc, c.cfg.Sys)
		c.disksAt = now
	}
	s.Disks = c.disks

	memPct := 0.0
	if s.Mem.Total > 0 {
		memPct = round1(100 * float64(s.Mem.Used) / float64(s.Mem.Total))
	}
	p := Point{T: s.T, CPU: s.CPU.Percent, Mem: memPct, Temp: s.Temp, Rx: s.Net.Rx, Tx: s.Net.Tx}

	msg, _ := json.Marshal(map[string]any{"snapshot": s, "point": p})

	c.mu.Lock()
	c.latest = s
	c.history = append(c.history, p)
	if len(c.history) > historySize {
		c.history = c.history[len(c.history)-historySize:]
	}
	for ch := range c.subs {
		select {
		case ch <- msg:
		default: // cliente lento: perde esta amostra, recebe a próxima
		}
	}
	c.mu.Unlock()
}

func (c *Collector) Latest() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

func (c *Collector) History() []Point {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Point, len(c.history))
	copy(out, c.history)
	return out
}

// Subscribe registra um ouvinte (ex.: uma aba aberta via SSE) e acelera a coleta.
func (c *Collector) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 2)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
