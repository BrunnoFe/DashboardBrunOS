package disks

import (
	"bytes"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"brunos/internal/hostcmd"
)

// Part é uma partição (ou o disco inteiro, quando ele foi formatado sem tabela de partições).
type Part struct {
	Name   string   `json:"name"` // sdb1, nvme0n1p2
	Path   string   `json:"path"`
	FSType string   `json:"fsType,omitempty"`
	Label  string   `json:"label,omitempty"`
	UUID   string   `json:"uuid,omitempty"`
	Size   int64    `json:"size"`
	Used   *int64   `json:"used,omitempty"`  // só com o sistema de arquivos montado
	Avail  *int64   `json:"avail,omitempty"` // idem
	Mounts []string `json:"mounts,omitempty"`
	Holder string   `json:"holder,omitempty"` // lvm, crypt, raid1... (algo usa a partição por baixo)
	Fstab  string   `json:"fstab,omitempty"`  // pasta desta partição no /etc/fstab
	Whole  bool     `json:"whole,omitempty"`  // é o próprio disco (sem tabela de partições)
	// preenchidos na hora de responder
	Hist string `json:"hist,omitempty"` // nome no histórico de espaço (diskhist)
	Root string `json:"root,omitempty"` // id da raiz no Arquivos (Mapa do espaço, Duplicados)
}

// Disk é um disco inteiro como o servidor o vê (lsblk).
type Disk struct {
	Key        string `json:"key"` // número de série (como no SMART) ou "dev:sdb"
	Dev        string `json:"dev"` // sdb (pode mudar ao reiniciar)
	Path       string `json:"path"`
	ByID       string `json:"byId,omitempty"` // caminho fixo em /dev/disk/by-id
	Model      string `json:"model,omitempty"`
	Vendor     string `json:"vendor,omitempty"`
	Serial     string `json:"serial,omitempty"`
	WWN        string `json:"wwn,omitempty"`
	Rev        string `json:"rev,omitempty"`
	Tran       string `json:"tran,omitempty"` // usb, sata, nvme
	PTType     string `json:"ptType,omitempty"`
	Size       int64  `json:"size"`
	Rotational bool   `json:"rotational"`
	Removable  bool   `json:"removable,omitempty"`
	System     bool   `json:"system,omitempty"` // tem a raiz, o /boot ou a swap
	Parts      []Part `json:"parts"`
}

// Mounts devolve todas as pastas onde algo deste disco está montado.
func (d Disk) Mounts() []string {
	var out []string
	for _, p := range d.Parts {
		out = append(out, p.Mounts...)
	}
	sort.Strings(out)
	return out
}

// flexInt aceita número, texto ("123") ou null (o lsblk mudou o formato entre versões).
type flexInt struct {
	v  int64
	ok bool
}

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	f.v, f.ok = n, true
	return nil
}

// flexBool aceita true/false, "1"/"0" e 1/0.
type flexBool bool

func (f *flexBool) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	*f = flexBool(s == "true" || s == "1")
	return nil
}

// flexStr aceita texto ou null.
type flexStr string

func (f *flexStr) UnmarshalJSON(b []byte) error {
	var s *string
	if json.Unmarshal(b, &s) == nil && s != nil {
		*f = flexStr(strings.TrimSpace(*s))
	}
	return nil
}

type node struct {
	Name        flexStr   `json:"name"`
	Path        flexStr   `json:"path"`
	Type        flexStr   `json:"type"`
	Size        flexInt   `json:"size"`
	Model       flexStr   `json:"model"`
	Vendor      flexStr   `json:"vendor"`
	Serial      flexStr   `json:"serial"`
	WWN         flexStr   `json:"wwn"`
	Rev         flexStr   `json:"rev"`
	Tran        flexStr   `json:"tran"`
	Rota        flexBool  `json:"rota"`
	RM          flexBool  `json:"rm"`
	FSType      flexStr   `json:"fstype"`
	Label       flexStr   `json:"label"`
	UUID        flexStr   `json:"uuid"`
	FSUsed      flexInt   `json:"fsused"`
	FSAvail     flexInt   `json:"fsavail"`
	PTType      flexStr   `json:"pttype"`
	Mountpoints []flexStr `json:"mountpoints"`
	Mountpoint  flexStr   `json:"mountpoint"`
	Children    []node    `json:"children"`
}

func (n node) mounts() []string {
	var out []string
	for _, m := range n.Mountpoints {
		if m != "" {
			out = append(out, string(m))
		}
	}
	if len(out) == 0 && n.Mountpoint != "" {
		out = append(out, string(n.Mountpoint))
	}
	return out
}

// below junta as montagens e os tipos dos filhos (LVM, criptografia, RAID) de uma partição.
func (n node) below() (mounts []string, holders []string) {
	for _, c := range n.Children {
		mounts = append(mounts, c.mounts()...)
		if t := string(c.Type); t != "" {
			holders = append(holders, t)
		}
		cm, ch := c.below()
		mounts, holders = append(mounts, cm...), append(holders, ch...)
	}
	return
}

func isSystemMount(m string) bool {
	return m == "/" || m == "[SWAP]" || m == "/boot" || strings.HasPrefix(m, "/boot/")
}

// ErrBadOutput: a resposta do "discos-detalhe" não veio no formato esperado.
var ErrBadOutput = errors.New("o servidor respondeu a lista de discos num formato inesperado")

// parseDetail lê a saída do "discos-detalhe": o JSON do lsblk, as linhas do fstab e o /dev/disk/by-id.
func parseDetail(out []byte) ([]Disk, error) {
	sections := map[string][]byte{}
	cur := ""
	for _, line := range bytes.SplitAfter(out, []byte("\n")) {
		if t := strings.TrimSpace(string(line)); strings.HasPrefix(t, "=== ") {
			cur = strings.TrimPrefix(t, "=== ")
			continue
		}
		sections[cur] = append(sections[cur], line...)
	}
	var tree struct {
		Devices []node `json:"blockdevices"`
	}
	if err := json.Unmarshal(sections["lsblk"], &tree); err != nil {
		return nil, ErrBadOutput
	}
	fstab := parseFstab(sections["fstab"])
	byID := parseByID(sections["by-id"])
	var list []Disk
	for _, n := range tree.Devices {
		name := string(n.Name)
		if string(n.Type) != "disk" || !hostcmd.ValidDisk(name) {
			continue // loop, rom, zram, partições soltas
		}
		d := Disk{Dev: name, Path: string(n.Path), Model: string(n.Model), Vendor: string(n.Vendor), Serial: string(n.Serial),
			WWN: string(n.WWN), Rev: string(n.Rev), Tran: strings.ToLower(string(n.Tran)), PTType: string(n.PTType),
			Size: n.Size.v, Rotational: bool(n.Rota), Removable: bool(n.RM), ByID: byID["/dev/"+name]}
		if d.Path == "" {
			d.Path = "/dev/" + name
		}
		d.Key = d.Serial
		if d.Key == "" {
			d.Key = "dev:" + name
		}
		parts := n.Children
		if len(parts) == 0 && (n.FSType != "" || len(n.mounts()) > 0) {
			parts = []node{n} // formatado direto, sem partições
		}
		for _, c := range parts {
			p := Part{Name: string(c.Name), Path: string(c.Path), FSType: string(c.FSType), Label: string(c.Label),
				UUID: string(c.UUID), Size: c.Size.v, Mounts: c.mounts(), Whole: c.Name == n.Name}
			if p.Path == "" {
				p.Path = "/dev/" + p.Name
			}
			if c.FSUsed.ok {
				v := c.FSUsed.v
				p.Used = &v
			}
			if c.FSAvail.ok {
				v := c.FSAvail.v
				p.Avail = &v
			}
			if !p.Whole {
				bm, bh := c.below()
				p.Mounts = append(p.Mounts, bm...)
				p.Holder = strings.Join(uniq(bh), ", ")
			}
			p.Fstab = fstab["UUID="+p.UUID]
			if p.Fstab == "" && p.Label != "" {
				p.Fstab = fstab["LABEL="+p.Label]
			}
			if p.Fstab == "" {
				p.Fstab = fstab[p.Path]
			}
			for _, m := range p.Mounts {
				if isSystemMount(m) {
					d.System = true
				}
			}
			d.Parts = append(d.Parts, p)
		}
		if d.Parts == nil {
			d.Parts = []Part{}
		}
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Dev < list[j].Dev })
	return list, nil
}

func uniq(a []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// parseFstab: "UUID=abcd /mnt/disco1" → mapa identificador → pasta.
func parseFstab(b []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.HasPrefix(f[1], "/") {
			out[f[0]] = f[1]
		}
	}
	return out
}

// parseByID escolhe, para cada disco, o nome mais legível de /dev/disk/by-id (usb-, ata-, nvme-, scsi-;
// wwn- e eui. só se não houver outro).
func parseByID(b []byte) map[string]string {
	best := map[string]string{}
	rank := func(name string) int {
		switch {
		case strings.HasPrefix(name, "usb-"), strings.HasPrefix(name, "ata-"):
			return 0
		case strings.HasPrefix(name, "nvme-") && !strings.HasPrefix(name, "nvme-eui.") && !strings.HasPrefix(name, "nvme-nvme."):
			return 0
		case strings.HasPrefix(name, "scsi-"):
			return 1
		}
		return 2
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || strings.Contains(f[0], "-part") || !strings.HasPrefix(f[1], "/dev/") {
			continue
		}
		dev := path.Clean(f[1])
		if !hostcmd.ValidDisk(strings.TrimPrefix(dev, "/dev/")) {
			continue
		}
		cur, ok := best[dev]
		name := "/dev/disk/by-id/" + f[0]
		if !ok || rank(f[0]) < rank(path.Base(cur)) || (rank(f[0]) == rank(path.Base(cur)) && len(name) < len(cur)) {
			best[dev] = name
		}
	}
	return best
}

var speedRe = regexp.MustCompile(`(\d+) bytes .*copied, ([0-9.]+) s`)

// parseSpeed lê "velocidade 1073741824 bytes (1.1 GB, 1.0 GiB) copied, 7.1 s, 151 MB/s".
func parseSpeed(out []byte) (bytesRead int64, secs float64, ok bool) {
	m := speedRe.FindStringSubmatch(string(out))
	if m == nil {
		return 0, 0, false
	}
	bytesRead, _ = strconv.ParseInt(m[1], 10, 64)
	secs, _ = strconv.ParseFloat(m[2], 64)
	return bytesRead, secs, bytesRead > 0 && secs > 0
}

// LogLine é uma linha do registro do Linux sobre o disco.
type LogLine struct {
	At   string `json:"at"`   // horário (ISO, do journalctl)
	Text string `json:"text"` // a mensagem, sem o nome do servidor
}

// parseErrors lê as linhas do "erros": "2026-11-12T10:20:30-0300 servidor kernel: [sdb] ...".
func parseErrors(out []byte) []LogLine {
	lines := []LogLine{}
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		at, rest, _ := strings.Cut(l, " ")
		if _, msg, ok := strings.Cut(rest, "kernel: "); ok {
			rest = msg
		}
		if len(rest) > 400 {
			rest = rest[:400]
		}
		lines = append(lines, LogLine{At: at, Text: rest})
	}
	// o mais novo primeiro
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines
}
