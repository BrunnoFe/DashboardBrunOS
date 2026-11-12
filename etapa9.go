package main

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"brunos/internal/energy"
	"brunos/internal/hostcmd"
	"brunos/internal/smart"
	"brunos/internal/updates"
)

// setupEtapa9 liga o consumo e custo (B3), a saúde dos discos (D3) e as atualizações de apps e do
// Linux (A1). Os três leem o servidor pelo script brunos-host 5 (só leitura) quando precisam de root.
func (s *server) setupEtapa9(dataDir string) {
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
	s.smart.SetTemp = s.stats.SetDiskTemp
	s.updates = updates.New(filepath.Join(dataDir, "updates.json"))
	s.updates.Apps = s.tipi.CheckUpdates
	s.updates.Query = host
	s.updates.Events = s.events
	s.updates.NotifyApps = func() bool { v, ok := s.readSettings()["appUpdateNotify"].(bool); return !ok || v }
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
