package main

import (
	"context"
	"time"

	"brunos/internal/events"
	"brunos/internal/files"
	"brunos/internal/power"
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

// watchDisks confere a cada minuto se algum disco mapeado se desmontou. Disco mapeado sem
// nada montado é perigoso: os apps continuam gravando na pasta vazia, ou seja, no SSD do sistema.
func (s *server) watchDisks(ctx context.Context) {
	time.Sleep(20 * time.Second) // deixa os discos montarem depois de ligar o servidor
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		checkDisks(s.files.Disks(), s.events)
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
