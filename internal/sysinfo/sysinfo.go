// Package sysinfo lê CPU, memória, temperatura, discos e rede direto de /proc e /sys,
// sem nenhuma dependência e sem processos auxiliares. Cada amostra custa microssegundos.
package sysinfo

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type Config struct {
	Proc      string // /proc do container (CPU, memória e uptime já são do host)
	Sys       string // /sys
	HostProc  string // /proc do host montado (ex.: /host/proc) — usado só para a rede
	OSRelease string // /etc/os-release do host montado
	Hostname  string // /etc/hostname do host montado
}

type Info struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Kernel   string `json:"kernel"`
	CPUModel string `json:"cpuModel"`
	Cores    int    `json:"cores"`
	Arch     string `json:"arch"`
}

type Disk struct {
	Name       string `json:"name"`
	Path       string `json:"path"` // caminho dentro do container
	Device     string `json:"device"`
	Model      string `json:"model,omitempty"`
	FSType     string `json:"fsType"`
	Rotational bool   `json:"rotational"`
	System     bool   `json:"system"`
	Total      uint64 `json:"total"`
	Used       uint64 `json:"used"`
	Free       uint64 `json:"free"`
}

type Sensor struct {
	Chip  string  `json:"chip"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// Battery descreve a bateria (notebooks usados como servidor: ela funciona como nobreak).
// Campos em zero/nil quando o driver não informa.
type Battery struct {
	Percent      int     `json:"percent"`
	Status       string  `json:"status"`      // Charging, Discharging, Full, Not charging
	AC           *bool   `json:"ac"`          // na tomada (fonte "Mains" online)
	EnergyWh     float64 `json:"energyWh"`    // carga atual
	FullWh       float64 `json:"fullWh"`      // capacidade atual quando cheia
	DesignWh     float64 `json:"designWh"`    // capacidade de fábrica
	PowerW       float64 `json:"powerW"`      // potência entrando ou saindo (média móvel)
	Health       *int    `json:"health"`      // FullWh / DesignWh em %
	Cycles       *int    `json:"cycles"`      // ciclos de carga
	MinutesLeft  *int    `json:"minutesLeft"` // autonomia estimada (descarregando)
	MinutesFull  *int    `json:"minutesFull"` // tempo até encher (carregando)
	Estimate     *int    `json:"estimate"`    // na tomada: autonomia se a energia cair, pelo último consumo medido na bateria
	Voltage      float64 `json:"voltage"`     // V
	Tech         string  `json:"tech"`        // Li-ion etc.
	Model        string  `json:"model"`
	Manufacturer string  `json:"manufacturer"`
	ChargeLimit  *int    `json:"chargeLimit"` // limite de carga configurado (charge_control_end_threshold)
	History      []BatPt `json:"history,omitempty"`
}

// BatPt é um ponto do histórico de carga (um a cada 5 minutos, até 24 h).
type BatPt struct {
	T       int64 `json:"t"`
	Percent int   `json:"p"`
	AC      bool  `json:"ac"`
}

type cpuTimes struct{ idle, total uint64 }

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readInt(path string) (int64, bool) {
	v, err := strconv.ParseInt(readFile(path), 10, 64)
	return v, err == nil
}

func readCPU(proc string) (cpuTimes, bool) {
	f, err := os.Open(filepath.Join(proc, "stat"))
	if err != nil {
		return cpuTimes{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return cpuTimes{}, false
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, false
	}
	var t cpuTimes
	for i, s := range fields[1:] {
		if i >= 8 { // guest/guest_nice já estão contados em user/nice
			break
		}
		v, _ := strconv.ParseUint(s, 10, 64)
		t.total += v
		if i == 3 || i == 4 { // idle + iowait
			t.idle += v
		}
	}
	return t, true
}

type memInfo struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
	SwapTotal uint64 `json:"swapTotal"`
	SwapUsed  uint64 `json:"swapUsed"`
}

func readMem(proc string) memInfo {
	f, err := os.Open(filepath.Join(proc, "meminfo"))
	if err != nil {
		return memInfo{}
	}
	defer f.Close()
	vals := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fs := strings.Fields(rest)
		if len(fs) == 0 {
			continue
		}
		v, _ := strconv.ParseUint(fs[0], 10, 64)
		vals[k] = v * 1024
	}
	m := memInfo{Total: vals["MemTotal"], Available: vals["MemAvailable"], SwapTotal: vals["SwapTotal"]}
	if m.Available == 0 {
		m.Available = vals["MemFree"] + vals["Buffers"] + vals["Cached"]
	}
	if m.Total > m.Available {
		m.Used = m.Total - m.Available
	}
	if m.SwapTotal > vals["SwapFree"] {
		m.SwapUsed = m.SwapTotal - vals["SwapFree"]
	}
	return m
}

func readLoad(proc string) [3]float64 {
	var l [3]float64
	fs := strings.Fields(readFile(filepath.Join(proc, "loadavg")))
	for i := 0; i < 3 && i < len(fs); i++ {
		l[i], _ = strconv.ParseFloat(fs[i], 64)
	}
	return l
}

func readUptime(proc string) float64 {
	fs := strings.Fields(readFile(filepath.Join(proc, "uptime")))
	if len(fs) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fs[0], 64)
	return v
}

// ── Temperatura, ventoinha e bateria ─────────────────────────────

// Ordem de preferência para "a" temperatura da CPU.
var cpuSensorPrefs = []struct{ chip, label string }{
	{"coretemp", "Package id 0"}, {"k10temp", "Tctl"}, {"k10temp", "Tdie"}, {"zenpower", "Tdie"},
	{"cpu_thermal", ""}, {"dell_smm", "CPU"}, {"thinkpad", "CPU"}, {"acpitz", ""},
}

func readHwmon(sys string) (sensors []Sensor, cpu *float64, fan *int) {
	dirs, _ := filepath.Glob(filepath.Join(sys, "class/hwmon/hwmon*"))
	sort.Strings(dirs)
	for _, d := range dirs {
		chip := readFile(filepath.Join(d, "name"))
		inputs, _ := filepath.Glob(filepath.Join(d, "temp*_input"))
		sort.Strings(inputs)
		for _, in := range inputs {
			v, ok := readInt(in)
			if !ok || v <= 0 || v > 150000 {
				continue
			}
			label := readFile(strings.TrimSuffix(in, "_input") + "_label")
			if label == "" {
				label = strings.TrimSuffix(filepath.Base(in), "_input")
			}
			sensors = append(sensors, Sensor{Chip: chip, Label: label, Value: float64(v) / 1000})
		}
		if fan == nil {
			fans, _ := filepath.Glob(filepath.Join(d, "fan*_input"))
			sort.Strings(fans)
			for _, fi := range fans {
				if v, ok := readInt(fi); ok && v > 0 {
					rpm := int(v)
					fan = &rpm
					break
				}
			}
		}
	}
	for _, p := range cpuSensorPrefs {
		for _, s := range sensors {
			if s.Chip == p.chip && (p.label == "" || strings.EqualFold(s.Label, p.label)) {
				v := s.Value
				return sensors, &v, fan
			}
		}
	}
	// Sem sensor conhecido: tenta as thermal zones
	zones, _ := filepath.Glob(filepath.Join(sys, "class/thermal/thermal_zone*/temp"))
	for _, z := range zones {
		if v, ok := readInt(z); ok && v > 0 {
			t := float64(v) / 1000
			return sensors, &t, fan
		}
	}
	return sensors, nil, fan
}

func readBattery(sys string) *Battery {
	bats, _ := filepath.Glob(filepath.Join(sys, "class/power_supply/BAT*"))
	for _, b := range bats {
		v, ok := readInt(filepath.Join(b, "capacity"))
		if !ok {
			continue
		}
		bat := &Battery{Percent: int(v), Status: readFile(filepath.Join(b, "status")),
			Tech: readFile(filepath.Join(b, "technology")), Model: readFile(filepath.Join(b, "model_name")),
			Manufacturer: readFile(filepath.Join(b, "manufacturer"))}
		num := func(name string) float64 { n, _ := readInt(filepath.Join(b, name)); return float64(n) }
		volt := num("voltage_now") / 1e6
		vDesign := num("voltage_min_design") / 1e6
		if vDesign == 0 {
			vDesign = volt
		}
		bat.Voltage = round1(volt)
		if e := num("energy_full"); e > 0 { // µWh
			bat.EnergyWh, bat.FullWh, bat.DesignWh = num("energy_now")/1e6, e/1e6, num("energy_full_design")/1e6
		} else if c := num("charge_full"); c > 0 { // µAh × V
			bat.EnergyWh, bat.FullWh, bat.DesignWh = num("charge_now")/1e6*vDesign, c/1e6*vDesign, num("charge_full_design")/1e6*vDesign
		}
		if p := num("power_now"); p > 0 {
			bat.PowerW = p / 1e6
		} else if cur := num("current_now"); cur > 0 && volt > 0 {
			bat.PowerW = cur / 1e6 * volt
		}
		if bat.DesignWh > 0 && bat.FullWh > 0 {
			h := int(bat.FullWh/bat.DesignWh*100 + 0.5)
			bat.Health = &h
		}
		if c, ok := readInt(filepath.Join(b, "cycle_count")); ok && c > 0 {
			n := int(c)
			bat.Cycles = &n
		}
		if l, ok := readInt(filepath.Join(b, "charge_control_end_threshold")); ok && l > 0 && l < 100 {
			n := int(l)
			bat.ChargeLimit = &n
		}
		if ac := readAC(sys); ac != nil {
			bat.AC = ac
		} else if bat.Status != "" {
			on := bat.Status != "Discharging"
			bat.AC = &on
		}
		return bat
	}
	return nil
}

// readAC procura a fonte de energia externa (tipo "Mains": AC, ADP1, ACAD...).
func readAC(sys string) *bool {
	all, _ := filepath.Glob(filepath.Join(sys, "class/power_supply/*"))
	for _, p := range all {
		if readFile(filepath.Join(p, "type")) != "Mains" {
			continue
		}
		if v, ok := readInt(filepath.Join(p, "online")); ok {
			on := v == 1
			return &on
		}
	}
	return nil
}

// finishBattery suaviza a potência e calcula as estimativas de tempo.
func finishBattery(b *Battery, prevPower float64) float64 {
	if b == nil {
		return 0
	}
	p := b.PowerW
	if prevPower > 0 && p > 0 {
		p = prevPower*0.8 + p*0.2
	}
	b.PowerW = round1(p)
	if p > 0.3 {
		switch b.Status {
		case "Discharging":
			if b.EnergyWh > 0 {
				m := int(b.EnergyWh / p * 60)
				b.MinutesLeft = &m
			}
		case "Charging":
			target := b.FullWh
			if b.ChargeLimit != nil {
				target = b.FullWh * float64(*b.ChargeLimit) / 100
			}
			if target > b.EnergyWh {
				m := int((target - b.EnergyWh) / p * 60)
				b.MinutesFull = &m
			}
		}
	}
	b.EnergyWh, b.FullWh, b.DesignWh = round1(b.EnergyWh), round1(b.FullWh), round1(b.DesignWh)
	return p
}

// ── Rede ─────────────────────────────────────────────────────────

var virtualIface = regexp.MustCompile(`^(lo|docker|br-|veth|virbr|vnet|tun|tap|tailscale|wg|zt|cni|flannel|kube)`)

type netCounters struct {
	rx, tx uint64
	ifaces []string
}

func readNet(path string) (netCounters, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return netCounters{}, false
	}
	var n netCounters
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if virtualIface.MatchString(name) {
			continue
		}
		fs := strings.Fields(rest)
		if len(fs) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fs[0], 10, 64)
		tx, _ := strconv.ParseUint(fs[8], 10, 64)
		if rx == 0 && tx == 0 {
			continue
		}
		n.rx += rx
		n.tx += tx
		n.ifaces = append(n.ifaces, name)
	}
	return n, true
}

// ── Discos ───────────────────────────────────────────────────────

var blockDev = regexp.MustCompile(`^/dev/(sd[a-z]+\d*|nvme\d+n\d+(p\d+)?|mmcblk\d+(p\d+)?|vd[a-z]+\d*|xvd[a-z]+\d*|md\d+|mapper/[\w.-]+|dm-\d+)$`)
var partSuffix = regexp.MustCompile(`^(nvme\d+n\d+|mmcblk\d+)p\d+$|^([a-z]+)\d+$`)

// Arquivos que o Docker monta a partir do disco do sistema.
var systemMarkers = map[string]bool{"/etc/hosts": true, "/etc/hostname": true, "/etc/resolv.conf": true, "/data": true, "/runtipi": true}

func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

var diskFS = map[string]bool{"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "f2fs": true, "zfs": true,
	"ntfs": true, "ntfs3": true, "fuseblk": true, "exfat": true, "vfat": true, "jfs": true, "reiserfs": true, "bcachefs": true}

// devName descobre o nome real do dispositivo de um ponto de montagem pelo st_dev
// (/sys/dev/block/MAJ:MIN/uevent → DEVNAME=sda1).
func devName(sys, mnt string) string {
	var st syscall.Stat_t
	if err := syscall.Stat(mnt, &st); err != nil {
		return ""
	}
	dev := uint64(st.Dev)
	major := ((dev >> 8) & 0xfff) | ((dev >> 32) &^ 0xfff)
	minor := (dev & 0xff) | ((dev >> 12) &^ 0xff)
	for _, line := range strings.Split(readFile(filepath.Join(sys, "dev/block", fmt.Sprintf("%d:%d", major, minor), "uevent")), "\n") {
		if v, ok := strings.CutPrefix(line, "DEVNAME="); ok && v != "" {
			return "/dev/" + v
		}
	}
	return ""
}

func baseDevice(dev string) string {
	name := strings.TrimPrefix(dev, "/dev/")
	if m := partSuffix.FindStringSubmatch(name); m != nil {
		if m[1] != "" {
			return m[1]
		}
		return m[2]
	}
	return name
}

func readDisks(proc, sys string) []Disk {
	data, err := os.ReadFile(filepath.Join(proc, "self/mounts"))
	if err != nil {
		return nil
	}
	type cand struct {
		paths  []string
		fstype string
		system bool
	}
	// Agrupa pelo dispositivo real (st_dev), não pelo nome: em muitos sistemas o disco
	// raiz aparece como /dev/root ou /dev/disk/by-uuid/…, e o mesmo disco pode ter vários nomes.
	byDev := map[string]*cand{}
	order := []string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 3 || !(blockDev.MatchString(fs[0]) || diskFS[fs[2]] && strings.HasPrefix(fs[0], "/dev/")) {
			continue
		}
		if strings.HasPrefix(fs[0], "/dev/loop") {
			continue
		}
		dev, mnt := fs[0], unescapeMount(fs[1])
		if strings.HasPrefix(mnt, "/boot") || strings.Contains(mnt, "/boot/efi") || fs[2] == "vfat" && strings.Contains(mnt, "efi") {
			continue
		}
		if real := devName(sys, mnt); real != "" {
			dev = real
		}
		c, ok := byDev[dev]
		if !ok {
			c = &cand{fstype: fs[2]}
			byDev[dev] = c
			order = append(order, dev)
		}
		c.paths = append(c.paths, mnt)
		if systemMarkers[mnt] {
			c.system = true
		}
	}
	var disks []Disk
	for _, dev := range order {
		c := byDev[dev]
		// escolhe um diretório representativo (não um arquivo como /etc/hosts)
		path := ""
		for _, p := range c.paths {
			if st, err := os.Stat(p); err == nil && st.IsDir() && (path == "" || len(p) < len(path)) {
				path = p
			}
		}
		if path == "" {
			path = c.paths[0]
		}
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil || st.Blocks == 0 {
			continue
		}
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		if total < 1<<30 { // ignora partições minúsculas (boot, recuperação etc.)
			continue
		}
		free := st.Bavail * bsize
		used := (st.Blocks - st.Bfree) * bsize
		base := baseDevice(dev)
		name := filepath.Base(path)
		if c.system {
			name = "Sistema"
		}
		disks = append(disks, Disk{
			Name: name, Path: path, Device: dev, FSType: c.fstype, System: c.system,
			Model:      readFile(filepath.Join(sys, "block", base, "device/model")),
			Rotational: readFile(filepath.Join(sys, "block", base, "queue/rotational")) == "1",
			Total:      total, Used: used, Free: free,
		})
	}
	sort.SliceStable(disks, func(i, j int) bool {
		if disks[i].System != disks[j].System {
			return disks[i].System
		}
		return disks[i].Name < disks[j].Name
	})
	return disks
}

// ── Informações fixas ────────────────────────────────────────────

func readInfo(cfg Config) Info {
	info := Info{Arch: runtime.GOARCH}
	info.Kernel = readFile(filepath.Join(cfg.Proc, "sys/kernel/osrelease"))
	info.Hostname = readFile(cfg.Hostname)
	if info.Hostname == "" {
		info.Hostname, _ = os.Hostname()
	}
	for _, line := range strings.Split(readFile(cfg.OSRelease), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			info.OS = strings.Trim(v, `"`)
		}
	}
	f, err := os.Open(filepath.Join(cfg.Proc, "cpuinfo"))
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, ok := strings.Cut(sc.Text(), ":")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			if k == "processor" {
				info.Cores++
			}
			if (k == "model name" || k == "Model") && info.CPUModel == "" {
				info.CPUModel = strings.Join(strings.Fields(v), " ")
			}
		}
	}
	if info.Cores == 0 {
		info.Cores = runtime.NumCPU()
	}
	return info
}
