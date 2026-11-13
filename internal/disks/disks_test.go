package disks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"brunos/internal/events"
	"brunos/internal/files"
	"brunos/internal/hostcmd"
	"brunos/internal/smart"
	"brunos/internal/sysinfo"
)

// Exemplo inventado (nomes e números genéricos): NVMe do sistema com LVM, HD USB montado, HD USB com
// partição sem montagem, SSD SATA formatado direto (sem partições), pendrive sem número de série e
// lsblk antigo (rota/rm em texto, size em texto, mountpoint em vez de mountpoints) no último.
const lsblkSample = `=== lsblk
{"blockdevices":[
 {"name":"nvme0n1","path":"/dev/nvme0n1","type":"disk","size":256060514304,"model":"NVMe SSD 256GB","vendor":null,"serial":"NV123","wwn":"eui.0025","rev":"1.0","tran":"nvme","rota":false,"rm":false,"fstype":null,"label":null,"uuid":null,"partuuid":null,"fsused":null,"fsavail":null,"pttype":"gpt","mountpoints":[null],
  "children":[
   {"name":"nvme0n1p1","path":"/dev/nvme0n1p1","type":"part","size":1127219200,"fstype":"vfat","label":null,"uuid":"AAAA-1111","fsused":6000000,"fsavail":1100000000,"mountpoints":["/boot/efi"]},
   {"name":"nvme0n1p3","path":"/dev/nvme0n1p3","type":"part","size":252000000000,"fstype":"LVM2_member","uuid":"lvm-uuid","fsused":null,"fsavail":null,"mountpoints":[null],
    "children":[{"name":"ubuntu--vg-ubuntu--lv","path":"/dev/mapper/ubuntu--vg-ubuntu--lv","type":"lvm","size":100000000000,"fstype":"ext4","fsused":30000000000,"fsavail":60000000000,"mountpoints":["/"]}]}
  ]},
 {"name":"sda","path":"/dev/sda","type":"disk","size":1000204886016,"model":"HD USB 1TB","vendor":"Genérico","serial":"HD-AAA","wwn":null,"rev":"0001","tran":"usb","rota":true,"rm":false,"pttype":"gpt","mountpoints":[null],
  "children":[{"name":"sda1","path":"/dev/sda1","type":"part","size":1000202273280,"fstype":"ext4","label":"Fotos","uuid":"uuid-fotos","fsused":400000000000,"fsavail":550000000000,"mountpoints":["/mnt/disco1"]}]},
 {"name":"sdb","path":"/dev/sdb","type":"disk","size":500107862016,"model":"HD USB 500GB","serial":"HD-BBB","tran":"usb","rota":true,"rm":false,"pttype":"dos","mountpoints":[null],
  "children":[{"name":"sdb1","path":"/dev/sdb1","type":"part","size":500105217024,"fstype":"ntfs","label":"Backup","uuid":"uuid-backup","fsused":null,"fsavail":null,"mountpoints":[null]}]},
 {"name":"sdc","path":"/dev/sdc","type":"disk","size":240057409536,"model":"SSD SATA 240GB","serial":"SSD-CCC","tran":"sata","rota":false,"rm":false,"fstype":"xfs","label":"Rapido","uuid":"uuid-rapido","fsused":1000000000,"fsavail":230000000000,"pttype":null,"mountpoints":["/mnt/rapido"]},
 {"name":"loop0","path":"/dev/loop0","type":"loop","size":4096,"mountpoints":["/snap/x"]},
 {"name":"sr0","path":"/dev/sr0","type":"rom","size":1073741312},
 {"name":"sdd","path":"/dev/sdd","type":"disk","size":"16008609792","model":"Pendrive","serial":null,"tran":"usb","rota":"0","rm":"1","pttype":"dos","mountpoint":null,
  "children":[{"name":"sdd1","path":"/dev/sdd1","type":"part","size":"16007561216","fstype":"vfat","label":"PEN","uuid":"PEN-1","mountpoint":null}]}
]}
=== fstab
UUID=uuid-fotos /mnt/disco1
UUID=uuid-backup /mnt/backup
LABEL=Rapido /mnt/rapido
/dev/mapper/ubuntu--vg-ubuntu--lv /
=== by-id
wwn-0x5000aaaa /dev/sda
usb-Generico_HD_USB_1TB_HD-AAA-0:0 /dev/sda
usb-Generico_HD_USB_1TB_HD-AAA-0:0-part1 /dev/sda1
nvme-eui.0025 /dev/nvme0n1
nvme-NVMe_SSD_256GB_NV123 /dev/nvme0n1
ata-SSD_SATA_240GB_SSD-CCC /dev/sdc
`

func TestParseDetail(t *testing.T) {
	list, err := parseDetail([]byte(lsblkSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 {
		t.Fatalf("queria 5 discos (sem loop e sr0), veio %d: %+v", len(list), list)
	}
	by := map[string]Disk{}
	for _, d := range list {
		by[d.Dev] = d
	}
	nv := by["nvme0n1"]
	if !nv.System || nv.Key != "NV123" || nv.ByID != "/dev/disk/by-id/nvme-NVMe_SSD_256GB_NV123" || len(nv.Parts) != 2 || nv.Tran != "nvme" || nv.Rotational {
		t.Fatalf("NVMe: %+v", nv)
	}
	if p := nv.Parts[1]; p.Holder != "lvm" || len(p.Mounts) != 1 || p.Mounts[0] != "/" {
		t.Fatalf("LVM: %+v", p)
	}
	sda := by["sda"]
	if sda.System || sda.ByID != "/dev/disk/by-id/usb-Generico_HD_USB_1TB_HD-AAA-0:0" || sda.Parts[0].Fstab != "/mnt/disco1" || sda.Parts[0].Label != "Fotos" ||
		sda.Parts[0].Used == nil || *sda.Parts[0].Used != 400000000000 || !sda.Rotational || sda.Size != 1000204886016 {
		t.Fatalf("HD USB: %+v %+v", sda, sda.Parts)
	}
	sdb := by["sdb"]
	if len(sdb.Mounts()) != 0 || sdb.Parts[0].Fstab != "/mnt/backup" || sdb.Parts[0].Used != nil {
		t.Fatalf("partição sem montagem: %+v", sdb.Parts)
	}
	sdc := by["sdc"]
	if len(sdc.Parts) != 1 || !sdc.Parts[0].Whole || sdc.Parts[0].Fstab != "/mnt/rapido" || sdc.Parts[0].FSType != "xfs" {
		t.Fatalf("formatado sem partições: %+v", sdc.Parts)
	}
	sdd := by["sdd"]
	if sdd.Key != "dev:sdd" || sdd.Size != 16008609792 || !sdd.Removable || sdd.Rotational || sdd.Parts[0].Size != 16007561216 {
		t.Fatalf("lsblk antigo: %+v", sdd)
	}
	if _, err := parseDetail([]byte("=== lsblk\nnão é json\n")); !errors.Is(err, ErrBadOutput) {
		t.Fatalf("saída ruim: %v", err)
	}
}

func TestParseSpeedAndErrors(t *testing.T) {
	b, s, ok := parseSpeed([]byte("velocidade 1073741824 bytes (1.1 GB, 1.0 GiB) copied, 7.15843 s, 150 MB/s\n"))
	if !ok || b != 1073741824 || s < 7.15 || s > 7.16 {
		t.Fatalf("velocidade: %d %f %v", b, s, ok)
	}
	if _, _, ok := parseSpeed([]byte("dd: erro")); ok {
		t.Fatal("deveria falhar")
	}
	lines := parseErrors([]byte("2026-11-01T10:00:00-0300 servidor kernel: sd 6:0:0:0: [sda] tag#3 FAILED Result\n" +
		"2026-11-02T11:00:00-0300 servidor kernel: usb 2-1: USB disconnect, device number 4\n"))
	if len(lines) != 2 || lines[0].At != "2026-11-02T11:00:00-0300" || lines[0].Text != "usb 2-1: USB disconnect, device number 4" || strings.Contains(lines[1].Text, "servidor") {
		t.Fatalf("erros: %+v", lines)
	}
}

// fake imita o script do servidor.
type fake struct {
	mu     sync.Mutex
	detail string
	acts   []string
	err    error
	state  string
}

func (f *fake) query(ctx context.Context, cmd string) ([]byte, error) {
	if !hostcmd.ReadAllowed(cmd) {
		return nil, fmt.Errorf("pedido não permitido: %q", cmd)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	switch {
	case cmd == hostcmd.CmdDiskDetail:
		return []byte(f.detail), nil
	case strings.HasPrefix(cmd, hostcmd.CmdDiskState+" "):
		return []byte(f.state), nil
	case strings.HasPrefix(cmd, hostcmd.CmdDiskErrors+" "):
		return []byte("2026-11-01T10:00:00-0300 h kernel: [sda] erro\n"), nil
	}
	return nil, errors.New("inesperado")
}

func (f *fake) act(ctx context.Context, cmd string) ([]byte, error) {
	if !hostcmd.DiskActAllowed(cmd) {
		return nil, fmt.Errorf("pedido não permitido: %q", cmd)
	}
	f.mu.Lock()
	f.acts = append(f.acts, cmd)
	f.mu.Unlock()
	if strings.HasPrefix(cmd, hostcmd.CmdSpeed) {
		return []byte("velocidade 1073741824 bytes (1.1 GB, 1.0 GiB) copied, 8 s, 134 MB/s\n"), nil
	}
	return []byte("ejetado\n"), nil
}

func (f *fake) set(s string) { f.mu.Lock(); f.detail = s; f.mu.Unlock() }

func without(sample, dev string) string {
	// tira um disco do exemplo (o bloco que começa em {"name":"<dev>" até o próximo disco)
	start := strings.Index(sample, `{"name":"`+dev+`"`)
	end := strings.Index(sample[start+1:], "\n {\"name\":")
	return sample[:start] + sample[start+1+end+2:]
}

func events_(ev *events.Store, kind string) []events.Event {
	list, _ := ev.List()
	var out []events.Event
	for _, e := range list {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestTimelineAndAlerts(t *testing.T) {
	dir := t.TempDir()
	ev := events.New(filepath.Join(dir, "events.json"))
	f := &fake{detail: without(lsblkSample, "sdb")}
	file := filepath.Join(dir, "disk-events.json")
	m := New(file, f.query, f.act, ev)
	now := time.Unix(1_800_000_000, 0)
	m.Now = func() time.Time { return now }
	ctx := context.Background()
	// primeira vez: todos "vistos", sem aviso de disco novo
	if err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(events_(ev, "disk-new")); n != 0 {
		t.Fatalf("primeira lista não deveria avisar disco novo (%d)", n)
	}
	if tl := m.Timeline("HD-AAA"); len(tl) != 1 || tl[0].Kind != "visto" {
		t.Fatalf("linha do tempo inicial: %+v", tl)
	}
	// conecta o sdb: aviso de disco novo
	now = now.Add(time.Minute)
	f.set(lsblkSample)
	_ = m.Refresh(ctx)
	if es := events_(ev, "disk-new"); len(es) != 1 || !strings.Contains(es[0].Title, "Backup") || !strings.Contains(es[0].Body, "500,1 GB") {
		t.Fatalf("disco novo: %+v", es)
	}
	// desmonta o sda1 e tira o sdc: desmontado + sumiu (com aviso)
	now = now.Add(time.Minute)
	f.set(strings.Replace(without(lsblkSample, "sdc"), `"mountpoints":["/mnt/disco1"]`, `"mountpoints":[null]`, 1))
	_ = m.Refresh(ctx)
	if tl := m.Timeline("HD-AAA"); tl[0].Kind != "desmontado" || tl[0].Text != "Desmontado de /mnt/disco1" {
		t.Fatalf("desmontado: %+v", tl)
	}
	if es := events_(ev, "disk-gone"); len(es) != 1 || !strings.Contains(es[0].Title, "Rapido") {
		t.Fatalf("sumiu: %+v", es)
	}
	if tl := m.Timeline("SSD-CCC"); tl[0].Kind != "sumiu" || tl[0].Level != "aviso" {
		t.Fatalf("sumiu na linha do tempo: %+v", tl)
	}
	// o sdc volta montado: conectado + montado e o aviso "sumiu" se resolve
	now = now.Add(time.Minute)
	f.set(lsblkSample)
	_ = m.Refresh(ctx)
	tl := m.Timeline("SSD-CCC")
	if tl[0].Kind != "conectado" || ev.IsActive("disk-gone:SSD-CCC") {
		t.Fatalf("voltou: %+v ativo=%v", tl, ev.IsActive("disk-gone:SSD-CCC"))
	}
	// o arquivo guarda tudo (0600) e um Monitor novo lê de volta sem achar que é a primeira vez
	if st, err := os.Stat(file); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("arquivo: %v %v", st, err)
	}
	m2 := New(file, f.query, f.act, ev)
	m2.Now = m.Now
	if len(m2.Timeline("SSD-CCC")) != len(tl) || m2.fresh {
		t.Fatal("não leu a linha do tempo do arquivo")
	}
	// erro do servidor: a lista anterior continua e a mensagem é amigável
	f.mu.Lock()
	f.err = hostcmd.ErrOldScript
	f.mu.Unlock()
	if err := m.Refresh(ctx); err == nil || !strings.Contains(err.Error(), "antigo") {
		t.Fatalf("script antigo: %v", err)
	}
	if s := m.Snapshot(ctx); len(s.Disks) != 5 || !s.OldScript {
		t.Fatalf("depois do erro: %d discos old=%v", len(s.Disks), s.OldScript)
	}
}

func TestMergeEjectSpeedState(t *testing.T) {
	dir := t.TempDir()
	ev := events.New(filepath.Join(dir, "events.json"))
	f := &fake{detail: lsblkSample, state: "estado dormindo\n"}
	m := New(filepath.Join(dir, "disk-events.json"), f.query, f.act, ev)
	now := time.Unix(1_800_000_000, 0)
	m.Now = func() time.Time { return now }
	temp, hot := 41.0, 58.0
	m.Smart = func() smart.Snapshot {
		return smart.Snapshot{Disks: []smart.Disk{{Key: "HD-AAA", Dev: "sda", Health: smart.HealthOK, Temp: &temp, TempAt: now.Unix()},
			{Key: "HD-BBB", Dev: "sdb", Temp: &hot, TempAt: now.Unix()},
			{Key: "dev:sdd", Dev: "sdd", NoSmart: true, NoSmartWhy: smart.WhyStuck}}}
	}
	m.SysDisks = func() []sysinfo.Disk { return []sysinfo.Disk{{Name: "disco1", Device: "/dev/sda1"}} }
	m.Roots = func() []files.DiskInfo { return []files.DiskInfo{{ID: "d-disco1", Name: "disco1", Mounted: true}} }
	m.Apps = func(context.Context) []AppMount {
		return []AppMount{{URN: "immich:migrated", Name: "Immich", Sources: []string{"/mnt/disco1/fotos", "/home/x/runtipi/app-data/immich"}},
			{URN: "plex:migrated", Name: "Plex", Sources: []string{"/mnt/disco10/filmes"}}, // /mnt/disco10 não é /mnt/disco1
			{Name: "Sistema", Sources: []string{"/"}}}
	}
	ctx := context.Background()
	if err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	snap := m.Snapshot(ctx)
	var sda, sdb, nv DiskOut
	for _, d := range snap.Disks {
		switch d.Dev {
		case "sda":
			sda = d
		case "sdb":
			sdb = d
		case "nvme0n1":
			nv = d
		}
	}
	if sda.Smart == nil || sda.Temp == nil || *sda.Temp != 41 || sda.Parts[0].Hist != "disco1" || sda.Parts[0].Root != "d-disco1" {
		t.Fatalf("junção: %+v %+v", sda, sda.Parts)
	}
	if len(sda.Apps) != 1 || sda.Apps[0].Name != "Immich" || sda.Apps[0].Folders[0] != "/mnt/disco1/fotos" {
		t.Fatalf("apps do sda: %+v", sda.Apps)
	}
	if len(nv.Apps) != 0 {
		t.Fatalf("disco do sistema não lista apps pela raiz: %+v", nv.Apps)
	}
	// sdb quente (58 °C) e sem pasta no Uso ao vivo: Linha do tempo e aviso no sino
	if tl := m.Timeline("HD-BBB"); len(tl) < 2 || tl[0].Kind != "temperatura" || sdb.Temp == nil {
		t.Fatalf("pico de temperatura: %+v", tl)
	}
	if es := events_(ev, "disk-hot"); len(es) != 1 {
		t.Fatalf("aviso de disco quente: %+v", es)
	}
	// estado: pergunta ao servidor; disco com SMART travado não é perguntado
	if st, _, err := m.State(ctx, "sda"); err != nil || st != "dormindo" {
		t.Fatalf("estado: %q %v", st, err)
	}
	if st, why, _ := m.State(ctx, "sdd"); st != "desconhecido" || why == "" {
		t.Fatalf("estado de disco travado: %q %q", st, why)
	}
	if _, _, err := m.State(ctx, "sdz"); !errors.Is(err, ErrNoDisk) {
		t.Fatalf("disco inexistente: %v", err)
	}
	if _, _, err := m.State(ctx, "../sda"); !errors.Is(err, ErrNoDisk) {
		t.Fatalf("nome inválido: %v", err)
	}
	if l, err := m.Errors(ctx, "sda"); err != nil || len(l) != 1 {
		t.Fatalf("erros: %+v %v", l, err)
	}
	// ejetar: recusa montado, sistema e não USB; aceita USB desmontado
	for dev, want := range map[string]string{"sda": "montado", "nvme0n1": "USB", "sdc": "USB"} {
		if err := m.Eject(ctx, dev); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ejetar %s: %v", dev, err)
		}
	}
	if err := m.Eject(ctx, "sdb"); err != nil {
		t.Fatal(err)
	}
	if tl := m.Timeline("HD-BBB"); tl[0].Kind != "ejetado" {
		t.Fatalf("ejetado: %+v", tl)
	}
	// o sdb some da lista depois de ejetado: sem aviso de "sumiu"
	f.set(without(lsblkSample, "sdb"))
	_ = m.Refresh(ctx)
	if es := events_(ev, "disk-gone"); len(es) != 0 {
		t.Fatalf("ejetado não deveria avisar que sumiu: %+v", es)
	}
	// velocidade: em segundo plano, um por vez
	if err := m.StartSpeed("sda"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if s := m.Snapshot(ctx); !s.Disks[1].SpeedBusy && s.Disks[1].Speed != nil {
			if s.Disks[1].Speed.MBps != 134.2 {
				t.Fatalf("MB/s: %+v", s.Disks[1].Speed)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if tl := m.Timeline("HD-AAA"); tl[0].Kind != "velocidade" {
		t.Fatalf("velocidade na linha do tempo: %+v", tl)
	}
	f.mu.Lock()
	acts := strings.Join(f.acts, ",")
	f.mu.Unlock()
	if acts != "ejetar sdb,velocidade sda" {
		t.Fatalf("pedidos ao servidor: %s", acts)
	}
}

func TestPresentFrom(t *testing.T) {
	p := filepath.Join(t.TempDir(), "diskstats")
	os.WriteFile(p, []byte("   8       0 sda 1 2 3\n   8       1 sda1 1 2 3\n 259 0 nvme0n1 1 2 3\n 7 0 loop0 1 2 3\n"), 0o600)
	if got := strings.Join(PresentFrom(p)(), ","); got != "nvme0n1,sda" {
		t.Fatal(got)
	}
}
