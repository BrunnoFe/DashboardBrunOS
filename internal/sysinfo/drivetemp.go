package sysinfo

import (
	"path/filepath"
	"regexp"
	"time"
)

// Temperatura dos discos.
//
//   - HDs e SSDs SATA: driver "drivetemp" do kernel, que não vem ativo no Ubuntu
//     ("sudo modprobe drivetemp"). Ler a temperatura manda um comando ao disco e, em alguns
//     modelos, acorda um HD que estava parado. Por isso um HD só é lido de novo quando teve
//     leitura ou gravação desde a última medição (o contador de /sys/block/sdX/stat mudou).
//     Parado, ele continua mostrando a última medida, com o horário dela.
//   - NVMe: driver "nvme", já ativo, sem esse problema.
const tempEvery = time.Minute

type diskTemp struct {
	val  float64
	stat string // /sys/block/X/stat na hora da medição
	at   time.Time
}

var nvmeNS = regexp.MustCompile(`^(nvme\d+)(c\d+)?n\d+$`)

// driveSensors liga cada disco (sda, nvme0) ao arquivo de temperatura do hwmon dele.
func driveSensors(sys string) map[string]string {
	out := map[string]string{}
	dirs, _ := filepath.Glob(filepath.Join(sys, "class/hwmon/hwmon*"))
	for _, d := range dirs {
		in := filepath.Join(d, "temp1_input")
		switch readFile(filepath.Join(d, "name")) {
		case "drivetemp": // hwmonN/device → dispositivo SCSI, que tem block/sda
			blocks, _ := filepath.Glob(filepath.Join(d, "device/block/*"))
			for _, b := range blocks {
				out[filepath.Base(b)] = in
			}
		case "nvme": // hwmonN/device → controlador nvme0
			if real, err := filepath.EvalSymlinks(filepath.Join(d, "device")); err == nil {
				out[filepath.Base(real)] = in
			}
		}
	}
	return out
}

// fillTemps põe a temperatura em cada disco, usando o valor guardado quando não é hora
// (ou não é seguro) de ler de novo.
func (c *Collector) fillTemps(disks []Disk, now time.Time) {
	var sensors map[string]string
	for i := range disks {
		d := &disks[i]
		base := baseDevice(d.Device)
		prev := c.temps[base]
		stat := readFile(filepath.Join(c.cfg.Sys, "block", base, "stat"))
		due := prev == nil || now.Sub(prev.at) >= tempEvery && (!d.Rotational || stat != prev.stat)
		if due {
			if sensors == nil {
				sensors = driveSensors(c.cfg.Sys)
			}
			in := sensors[base]
			if m := nvmeNS.FindStringSubmatch(base); in == "" && m != nil {
				in = sensors[m[1]]
			}
			if v, ok := readInt(in); in != "" && ok && v > 0 && v < 150000 {
				prev = &diskTemp{val: round1(float64(v) / 1000), stat: stat, at: now}
				c.temps[base] = prev
			} else if prev != nil && now.Sub(prev.at) > 24*time.Hour {
				delete(c.temps, base) // sensor sumiu (driver descarregado, disco removido)
				prev = nil
			}
		}
		if prev != nil {
			v := prev.val
			d.Temp, d.TempAt = &v, prev.at.Unix()
		} else if e, ok := c.extTemp(base); ok { // sem drivetemp (ex.: gaveta USB): a do SMART
			v := e.val
			d.Temp, d.TempAt = &v, e.at.Unix()
		}
	}
}

// SetDiskTemp guarda a temperatura de um disco lida pelo SMART (ex.: "sda"). Só aparece quando o
// drivetemp não tem leitura para ele; vale por até 24 h.
func (c *Collector) SetDiskTemp(dev string, celsius float64, at time.Time) {
	c.extMu.Lock()
	defer c.extMu.Unlock()
	if c.ext == nil {
		c.ext = map[string]diskTemp{}
	}
	c.ext[dev] = diskTemp{val: round1(celsius), at: at}
}

func (c *Collector) extTemp(dev string) (diskTemp, bool) {
	c.extMu.Lock()
	defer c.extMu.Unlock()
	e, ok := c.ext[dev]
	if !ok || time.Since(e.at) > 24*time.Hour {
		return diskTemp{}, false
	}
	return e, true
}
