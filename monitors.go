package main

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"brunos/internal/disks"
	"brunos/internal/energy"
	"brunos/internal/hostcmd"
	"brunos/internal/runtipi"
	"brunos/internal/smart"
	"brunos/internal/sysinfo"
	"brunos/internal/updates"
)

// setupMonitors liga o consumo e custo, a saúde dos discos e as atualizações de apps e do Linux. Os três
// leem o servidor pelo script do servidor quando precisam de root; as atualizações também instalam por
// ele (decisão 47).
func (s *server) setupMonitors(dataDir string) {
	host := func(ctx context.Context, cmd string) ([]byte, error) {
		if !hostcmd.ReadAllowed(cmd) {
			return nil, errors.New("pedido não permitido")
		}
		return s.power.HostOutput(ctx, cmd)
	}
	s.energy = energy.New(filepath.Join(dataDir, "energy.json"), energy.Sources{
		Plug:  s.integ.PlugPower,
		Plugs: s.integ.PlugIDs,
		Battery: func() (float64, bool) {
			b := s.stats.Latest().Battery
			if b == nil {
				return 0, false
			}
			discharging := b.Status == "Discharging" || (b.AC != nil && !*b.AC)
			return b.PowerW, discharging
		},
		RAPL: func(ctx context.Context) (uint64, uint64, error) {
			out, err := host(ctx, hostcmd.CmdEnergy)
			if err != nil {
				return 0, 0, err
			}
			return parseRAPL(string(out))
		},
	})
	s.smart = smart.New(filepath.Join(dataDir, "smart.json"), host, s.events)
	s.smart.Act = s.diskAct
	s.smart.SetTemp = func(dev string, c float64, at time.Time) {
		s.stats.SetDiskTemp(dev, c, at)
		s.tsdb.Add("disk."+dev+".temp", c) // Histórico da temperatura do disco (janela Discos)
	}
	s.updates = updates.New(filepath.Join(dataDir, "updates.json"))
	s.updates.Apps = s.tipi.CheckUpdates
	s.updates.Query = host
	// atualizar Linux e apps pela interface (decisão 47): só os pedidos de atualização
	s.updates.Act = func(ctx context.Context, cmd string) ([]byte, error) {
		if !hostcmd.ActAllowed(cmd) {
			return nil, errors.New("pedido não permitido")
		}
		return s.power.HostOutput(ctx, cmd)
	}
	// o script do servidor se troca sozinho pelo desta versão, assinado pelo CI (decisão 49); sem
	// assinatura (compilação local), fica desligado e o painel oferece atualizar pelo Terminal
	if cmd := hostcmd.UpdateScriptCmd(); cmd != "" {
		s.updates.UpgradeScript = func(ctx context.Context) error {
			_, err := s.power.HostOutputInput(ctx, cmd, []byte(hostcmd.HostScript))
			return err
		}
	}
	s.updates.Events = s.events
	s.updates.NotifyApps = func() bool { v, ok := s.readSettings()["appUpdateNotify"].(bool); return !ok || v }
}

// diskAct faz uma ação da janela Discos pelo script do servidor (só smart-teste, velocidade e ejetar).
func (s *server) diskAct(ctx context.Context, cmd string) ([]byte, error) {
	if !hostcmd.DiskActAllowed(cmd) {
		return nil, errors.New("pedido não permitido")
	}
	return s.power.HostOutput(ctx, cmd)
}

// setupDisks liga a janela Discos (0.15.0): lista detalhada pelo script do servidor, SMART, espaço,
// apps que usam cada disco e a Linha do tempo. Precisa do Arquivos (raízes) já pronto.
func (s *server) setupDisks(dataDir, hostProc string) {
	read := func(ctx context.Context, cmd string) ([]byte, error) {
		if !hostcmd.ReadAllowed(cmd) {
			return nil, errors.New("pedido não permitido")
		}
		return s.power.HostOutput(ctx, cmd)
	}
	s.disks = disks.New(filepath.Join(dataDir, "disk-events.json"), read, s.diskAct, s.events)
	s.disks.Smart = s.smart.Snapshot
	s.disks.SysDisks = func() []sysinfo.Disk { return s.stats.Latest().Disks }
	s.disks.Roots = s.files.Disks
	s.disks.Apps = s.appMounts
	stats := "/proc/diskstats"
	if hostProc != "" {
		stats = filepath.Join(hostProc, "diskstats")
	}
	s.disks.Present = disks.PresentFrom(stats)
	s.smart.Note = s.disks.Note // SMART mudou e testes entram na Linha do tempo
}

// appMounts lista as pastas do servidor montadas nos containers de cada app (Docker).
func (s *server) appMounts(ctx context.Context) []disks.AppMount {
	cts, err := s.containers(ctx)
	if err != nil {
		return nil
	}
	apps, _ := s.tipi.Apps()
	byProject := map[string]runtipi.App{}
	for _, a := range apps {
		byProject[a.Project] = a
	}
	agg := map[string]*disks.AppMount{}
	var order []string
	for _, c := range cts {
		p := c.Project()
		if p == "" {
			p = c.Name()
		}
		am := agg[p]
		if am == nil {
			am = &disks.AppMount{Name: p}
			if a, ok := byProject[p]; ok {
				am.Name, am.URN = a.Name, a.URN
			}
			agg[p] = am
			order = append(order, p)
		}
		for _, m := range c.Mounts {
			if m.Type == "bind" && m.Source != "" {
				am.Sources = append(am.Sources, m.Source)
			}
		}
	}
	out := make([]disks.AppMount, 0, len(order))
	for _, p := range order {
		out = append(out, *agg[p])
	}
	return out
}

// parseRAPL lê "energia <µJ> <volta>" do script do servidor.
func parseRAPL(out string) (uint64, uint64, error) {
	f := strings.Fields(strings.TrimSpace(out))
	if len(f) < 2 || f[0] != "energia" {
		return 0, 0, errors.New("resposta do medidor de energia inesperada")
	}
	uj, err := strconv.ParseUint(f[1], 10, 64)
	if err != nil {
		return 0, 0, errors.New("resposta do medidor de energia inesperada")
	}
	var max uint64
	if len(f) > 2 {
		max, _ = strconv.ParseUint(f[2], 10, 64)
	}
	return uj, max, nil
}
