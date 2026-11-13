package sysinfo

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DiskIO é a leitura e escrita de um disco inteiro (sda, nvme0n1) desde a amostra anterior (0.14.0).
// Vem do /proc/diskstats (global, sem root e sem falar com o disco).
type DiskIO struct {
	Name  string  `json:"name"`
	Read  float64 `json:"read"`  // bytes/s
	Write float64 `json:"write"` // bytes/s
	Busy  float64 `json:"busy"`  // % do tempo fazendo E/S
}

type diskCounters struct {
	sectorsRead, sectorsWritten, msIO uint64
}

// Só discos inteiros: partições, loop, ram e dm ficam de fora.
var wholeDisk = regexp.MustCompile(`^(sd[a-z]+|hd[a-z]+|vd[a-z]+|xvd[a-z]+|nvme\d+n\d+|mmcblk\d+)$`)

func readDiskstats(path string) map[string]diskCounters {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]diskCounters{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		// major minor nome lidas lidas-junt setores-lidos ms-lendo escritas escr-junt setores-escritos ms-escrevendo em-andamento ms-io ...
		if len(p) < 13 || !wholeDisk.MatchString(p[2]) {
			continue
		}
		u := func(i int) uint64 { v, _ := strconv.ParseUint(p[i], 10, 64); return v }
		out[p[2]] = diskCounters{sectorsRead: u(5), sectorsWritten: u(9), msIO: u(12)}
	}
	return out
}

// diskstatsPath: o /proc/diskstats é o mesmo dentro e fora do container; prefere o do host se montado.
func diskstatsPath(cfg Config) string {
	if cfg.HostProc != "" {
		if p := filepath.Join(cfg.HostProc, "diskstats"); fileReadable(p) {
			return p
		}
	}
	return filepath.Join(cfg.Proc, "diskstats")
}

// diskRates calcula bytes/s e % ocupado entre duas leituras (setor = 512 bytes no diskstats).
func diskRates(prev, cur map[string]diskCounters, dt float64) []DiskIO {
	if dt <= 0 || prev == nil {
		return nil
	}
	out := []DiskIO{}
	for name, c := range cur {
		p, ok := prev[name]
		if !ok || c.sectorsRead < p.sectorsRead || c.sectorsWritten < p.sectorsWritten || c.msIO < p.msIO {
			continue // disco novo ou contador zerado (reconectado): espera a próxima
		}
		busy := float64(c.msIO-p.msIO) / (dt * 10) // ms / (dt × 1000) × 100
		if busy > 100 {
			busy = 100
		}
		out = append(out, DiskIO{
			Name:  name,
			Read:  round1(float64(c.sectorsRead-p.sectorsRead) * 512 / dt),
			Write: round1(float64(c.sectorsWritten-p.sectorsWritten) * 512 / dt),
			Busy:  round1(busy),
		})
	}
	sortDiskIO(out)
	return out
}

func sortDiskIO(a []DiskIO) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j].Name < a[j-1].Name; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
