package power

// Limite de carga da bateria, sem depender do modelo do notebook.
//
// Detecção: só lê arquivos do /sys (sem privilégio), UMA vez quando o BrunOS liga (e quando o
// usuário pede "Verificar de novo"). Trocar de notebook é trocar de máquina: o BrunOS liga de
// novo e detecta sozinho. Três casos:
//   - range:  charge_control_end_threshold (ThinkPad, ASUS, Huawei, Dell novos, Framework...).
//   - toggle: charge_types com Long_Life, ou conservation_mode do ideapad_acpi (Lenovo IdeaPad):
//     só liga/desliga; o fabricante decide o limite.
//   - none:   nada disso. O vigia não faz mais nada (nem lê arquivo) até o próximo início.
//
// Aplicar precisa de root: vai pelo script brunos-host no servidor (pacote hostcmd).
// Alguns notebooks esquecem o limite ao reiniciar; com um limite pedido, o BrunOS relê UM
// arquivo a cada 10 minutos e reaplica se mudou (no máximo uma tentativa a cada 30 minutos).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"brunos/internal/hostcmd"
)

const (
	ChargeRange  = "range"
	ChargeToggle = "toggle"
	ChargeNone   = "none"

	chargeCheckEvery = 10 * time.Minute
	chargeRetryAfter = 30 * time.Minute
	chargeMin        = 50
)

type chargeCap struct {
	kind  string
	model string
	file  string // range: charge_control_end_threshold; toggle: charge_types ou conservation_mode
	types bool   // toggle pelo charge_types (senão, conservation_mode)
	at    int64
}

// ChargeInfo é o que a janela Energia mostra.
type ChargeInfo struct {
	Kind      string `json:"kind"`              // range | toggle | none
	Model     string `json:"model,omitempty"`   // fabricante e modelo (DMI)
	Current   int    `json:"current,omitempty"` // range: limite atual no notebook (100 = sem limite)
	On        bool   `json:"on"`                // toggle: proteção ligada agora
	Want      int    `json:"want"`              // range: limite pedido no BrunOS (0 = não mexe)
	Protect   string `json:"protect"`           // toggle: "on" | "off" | ""
	Ready     bool   `json:"ready"`             // servidor configurado e atualizado: dá para aplicar
	CheckedAt int64  `json:"checkedAt"`
}

func readTrim(p string) string { b, _ := os.ReadFile(p); return strings.TrimSpace(string(b)) }

func detectCharge(sys string) chargeCap {
	c := chargeCap{kind: ChargeNone, at: time.Now().Unix(), model: dmiModel(sys)}
	supplies, _ := filepath.Glob(filepath.Join(sys, "class/power_supply/*"))
	for _, s := range supplies {
		if readTrim(filepath.Join(s, "type")) != "Battery" {
			continue
		}
		if f := filepath.Join(s, "charge_control_end_threshold"); readTrim(f) != "" {
			c.kind, c.file = ChargeRange, f
			return c
		}
		if f := filepath.Join(s, "charge_types"); strings.Contains(readTrim(f), "Long_Life") {
			c.kind, c.file, c.types = ChargeToggle, f, true
		}
	}
	if c.kind == ChargeNone {
		if m, _ := filepath.Glob(filepath.Join(sys, "bus/platform/drivers/ideapad_acpi/*/conservation_mode")); len(m) > 0 {
			c.kind, c.file = ChargeToggle, m[0]
		}
	}
	return c
}

// dmiModel monta "Acme Notebook 15" a partir do DMI (legível sem root).
func dmiModel(sys string) string {
	vendor := strings.Fields(readTrim(filepath.Join(sys, "class/dmi/id/sys_vendor")))
	product := readTrim(filepath.Join(sys, "class/dmi/id/product_name"))
	if len(vendor) == 0 || strings.HasPrefix(strings.ToLower(product), strings.ToLower(vendor[0])) {
		return product
	}
	return strings.TrimSpace(vendor[0] + " " + product)
}

// read lê o estado atual (um arquivo). range: limite; toggle: ligado.
func (c chargeCap) read() (int, bool) {
	v := readTrim(c.file)
	switch c.kind {
	case ChargeRange:
		n, _ := strconv.Atoi(v)
		return n, false
	case ChargeToggle:
		if c.types {
			return 0, strings.Contains(v, "[Long_Life]") || v == "Long_Life"
		}
		return 0, v == "1"
	}
	return 0, false
}

// DetectCharge procura o limite de carga (chamado ao ligar e pelo "Verificar de novo").
func (m *Monitor) DetectCharge() {
	c := detectCharge(m.SysDir)
	m.mu.Lock()
	m.charge = c
	m.mu.Unlock()
	slog.Info("energia: limite de carga", "tipo", c.kind, "modelo", c.model)
}

func (m *Monitor) chargeReadyLocked() bool {
	return m.cfg.Host != "" && m.cfg.TestedAt > 0 && m.cfg.HostSetup >= hostcmd.SetupVersion
}

func (m *Monitor) ChargeInfo() ChargeInfo {
	m.mu.Lock()
	c, cfg, ready := m.charge, m.cfg, m.chargeReadyLocked()
	m.mu.Unlock()
	if c.kind == "" {
		c.kind = ChargeNone
	}
	info := ChargeInfo{Kind: c.kind, Model: c.model, Want: cfg.ChargeLimit, Protect: cfg.ChargeProtect, Ready: ready, CheckedAt: c.at}
	if c.kind != ChargeNone {
		info.Current, info.On = c.read()
	}
	return info
}

// chargeCmd valida o pedido para o tipo de controle deste notebook.
func chargeCmd(kind string, limit int, protect string) (string, error) {
	switch kind {
	case ChargeRange:
		if limit < chargeMin || limit > 100 {
			return "", fmt.Errorf("o limite deve ficar entre %d%% e 100%%", chargeMin)
		}
		return hostcmd.CmdLimit(limit), nil
	case ChargeToggle:
		if protect != "on" && protect != "off" {
			return "", errors.New("escolha ligar ou desligar a proteção de carga")
		}
		return hostcmd.CmdProtect(protect == "on"), nil
	}
	return "", errors.New("este notebook não permite limitar a carga pelo Linux")
}

// SetCharge aplica o limite (range) ou a proteção (toggle) pelo servidor e guarda o pedido
// para reaplicar. limit 100 = sem limite (o BrunOS para de mexer).
func (m *Monitor) SetCharge(ctx context.Context, limit int, protect string) (ChargeInfo, error) {
	m.mu.Lock()
	c, cfg, ready := m.charge, m.cfg, m.chargeReadyLocked()
	m.mu.Unlock()
	cmd, err := chargeCmd(c.kind, limit, protect)
	if err != nil {
		return m.ChargeInfo(), err
	}
	if !ready {
		return m.ChargeInfo(), errors.New("configure (ou atualize) a conexão com o servidor nesta janela e clique em Testar")
	}
	if err := m.runCharge(ctx, cfg, cmd); err != nil {
		return m.ChargeInfo(), err
	}
	m.mu.Lock()
	nc := m.cfg
	if c.kind == ChargeRange {
		nc.ChargeLimit = limit
		if limit >= 100 {
			nc.ChargeLimit = 0
		}
	} else {
		nc.ChargeProtect = protect
	}
	err = m.writeConfigLocked(nc)
	m.chargeTry = time.Time{}
	m.mu.Unlock()
	slog.Info("energia: limite de carga aplicado", "pedido", cmd)
	return m.ChargeInfo(), err
}

func (m *Monitor) runCharge(ctx context.Context, cfg Config, cmd string) error {
	if m.HostRun == nil {
		return errors.New("comandos no servidor indisponíveis")
	}
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	out, err := m.HostRun(ctx, cfg, cmd)
	if err != nil {
		// o script explica em português (ex.: "o notebook recusou o limite de 85%")
		if _, msg, ok := strings.Cut(err.Error(), ": "); ok && strings.Contains(err.Error(), "código") && strings.TrimSpace(msg) != "" {
			return errors.New(strings.TrimSpace(msg))
		}
		return err
	}
	_ = out
	return nil
}

// checkCharge roda a cada 10 minutos: sem suporte ou sem pedido, não faz nada; senão relê um
// arquivo e reaplica se o notebook esqueceu (ex.: depois de reiniciar).
func (m *Monitor) checkCharge() {
	m.mu.Lock()
	c, cfg, ready := m.charge, m.cfg, m.chargeReadyLocked()
	busy := m.chargeBusy || time.Since(m.chargeTry) < chargeRetryAfter
	m.mu.Unlock()
	if c.kind == "" || c.kind == ChargeNone || !ready || busy {
		return
	}
	var cmd string
	switch c.kind {
	case ChargeRange:
		if cfg.ChargeLimit == 0 {
			return
		}
		if cur, _ := c.read(); cur == cfg.ChargeLimit {
			return
		}
		cmd = hostcmd.CmdLimit(cfg.ChargeLimit)
	case ChargeToggle:
		if cfg.ChargeProtect == "" {
			return
		}
		if _, on := c.read(); on == (cfg.ChargeProtect == "on") {
			return
		}
		cmd = hostcmd.CmdProtect(cfg.ChargeProtect == "on")
	}
	m.mu.Lock()
	m.chargeBusy, m.chargeTry = true, time.Now()
	m.mu.Unlock()
	go func() {
		err := m.runCharge(context.Background(), cfg, cmd)
		m.mu.Lock()
		m.chargeBusy = false
		m.mu.Unlock()
		if err != nil {
			slog.Warn("energia: não consegui reaplicar o limite de carga", "pedido", cmd, "erro", err)
			return
		}
		slog.Info("energia: limite de carga reaplicado", "pedido", cmd)
	}()
}
