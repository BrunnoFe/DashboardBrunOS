package main

import (
	"context"
	"net"
	"strconv"
	"time"

	"brunos/internal/diskhist"
	"brunos/internal/events"
	"brunos/internal/files"
	"brunos/internal/power"
	"brunos/internal/sysinfo"
)

// batteryReading entrega ao vigia de energia a leitura mais recente da bateria.
func (s *server) batteryReading() *power.Reading {
	b := s.stats.Latest().Battery
	if b == nil {
		return nil
	}
	on := b.Status == "Discharging"
	if b.AC != nil {
		on = !*b.AC
	}
	return &power.Reading{Percent: b.Percent, OnBattery: on, PowerW: b.PowerW, EnergyWh: b.EnergyWh, MinutesLeft: b.MinutesLeft}
}

// watchDisks confere os discos a cada minuto: disco mapeado que se desmontou e disco quente.
// A cada hora, guarda o uso de cada disco e confere a previsão de disco cheio.
func (s *server) watchDisks(ctx context.Context) {
	time.Sleep(20 * time.Second) // deixa os discos montarem depois de ligar o servidor
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for i := 0; ; i++ {
		checkDisks(s.files.Disks(), s.events)
		disks := s.stats.Latest().Disks
		checkHot(disks, s.events)
		if i%60 == 0 && len(disks) > 0 {
			u := usage(disks)
			s.diskHist.Record(u)
			on, _ := s.readSettings()["diskForecast"].(bool) // Configurações → Previsão de disco cheio (padrão: desligada)
			s.diskHist.Check(s.events, u, on)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func checkDisks(disks []files.DiskInfo, ev *events.Store) {
	for _, d := range disks {
		key := "disk-unmounted:" + d.Name
		switch {
		case d.Mapped && !d.Mounted:
			ev.Emit(events.Event{Kind: "disk-unmounted", Level: events.Critico, Key: key,
				Title: "O disco " + d.Name + " se desmontou",
				Body:  "A pasta continua mapeada, mas não tem disco montado nela. O que for gravado ali vai para o disco do sistema. Confira o cabo e a montagem (sudo mount -a)."})
		case d.Mounted:
			ev.Resolve(key, &events.Event{Kind: "disk-mounted", Level: events.Info,
				Title: "O disco " + d.Name + " voltou", Body: "O disco está montado de novo."})
		default: // deixou de ser mapeado: encerra o alerta sem aviso
			ev.Resolve(key, nil)
		}
	}
}

func usage(disks []sysinfo.Disk) []diskhist.Usage {
	out := make([]diskhist.Usage, 0, len(disks))
	for _, d := range disks {
		out = append(out, diskhist.Usage{Name: d.Name, Used: d.Used, Free: d.Free})
	}
	return out
}

// Temperaturas de alerta: HDs sofrem acima de ~55 °C; SSD/NVMe aguentam mais.
func hotLimits(d sysinfo.Disk) (warn, clear float64) {
	if d.Rotational {
		return 55, 50
	}
	return 70, 65
}

func checkHot(disks []sysinfo.Disk, ev *events.Store) {
	for _, d := range disks {
		if d.Temp == nil {
			continue
		}
		warn, clear := hotLimits(d)
		key := "disk-hot:" + d.Name
		t := strconv.FormatFloat(*d.Temp, 'f', 0, 64)
		switch {
		case *d.Temp >= warn:
			kind := "SSD"
			if d.Rotational {
				kind = "HD"
			}
			ev.Emit(events.Event{Kind: "disk-hot", Level: events.Aviso, Key: key,
				Title: "O disco " + d.Name + " está quente (" + t + " °C)",
				Body:  "Acima de " + strconv.Itoa(int(warn)) + " °C o " + kind + " desgasta mais rápido. Veja se a ventilação está livre e se não há poeira.",
				Meta:  map[string]any{"temp": *d.Temp}})
		case *d.Temp <= clear:
			ev.Resolve(key, &events.Event{Kind: "disk-cool", Level: events.Info,
				Title: "O disco " + d.Name + " esfriou", Body: "Agora ele está a " + t + " °C."})
		}
	}
}

// loginSeen avisa na central quando alguém entra de um navegador/rede que nunca entrou.
func (s *server) loginSeen(ip, ua string) {
	d, isNew := s.logins.Seen(ip, ua)
	if !isNew {
		return
	}
	level := events.Aviso
	if d.Local {
		level = events.Info
	}
	s.events.Emit(events.Event{Kind: "login-new", Level: level,
		Title: "Novo acesso: " + d.Describe(),
		Body:  "Alguém entrou no BrunOS com " + d.Describe() + " " + d.Where() + ". Se foi você, pode ignorar. Se não foi, troque a senha em Configurações → Trocar senha.",
		Meta:  map[string]any{"ip": ip, "browser": d.Browser, "os": d.OS, "local": d.Local}})
}

// loginBlocked avisa quando um endereço errou a senha tantas vezes que foi bloqueado.
func (s *server) loginBlocked(ip string, lock time.Duration) {
	if !s.logins.BlockAlertDue(ip) {
		return
	}
	min := int(lock.Round(time.Minute) / time.Minute)
	if min < 1 {
		min = 1
	}
	where := "da rede local"
	if p := net.ParseIP(ip); p != nil && !isPrivate(p) {
		where = "da internet"
	}
	s.events.Emit(events.Event{Kind: "login-blocked", Level: events.Aviso,
		Title: "Senha errada várias vezes",
		Body: "O endereço " + ip + " (" + where + ") errou a senha muitas vezes e foi bloqueado por " + plural(min, "minuto", "minutos") +
			". Se não foi você, alguém pode estar tentando adivinhar a senha.",
		Meta: map[string]any{"ip": ip}})
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
