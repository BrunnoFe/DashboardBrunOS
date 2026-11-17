package main

// Logs de antes (0.21.1): quando o BrunOS pede ao Runtipi para ligar, reiniciar ou reconfigurar um app, o
// Runtipi RECRIA o container e os logs do anterior somem junto. Antes de pedir, o BrunOS guarda as últimas
// linhas de cada container do app (na memória, só a última cópia por app), para Detalhes do app mostrar o
// que aconteceu antes. Caso que motivou: um app instável logo depois de instalar, em que o erro de verdade
// estava no primeiro início, num container que já não existia mais.

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"brunos/internal/dockerc"
)

const (
	snapTail  = 300      // linhas por container
	snapLimit = 96 << 10 // bytes por container
)

type snapContainer struct {
	Name    string `json:"name"`
	Service string `json:"service"`
	Created int64  `json:"created"` // quando o container foi criado (segundos)
	Status  string `json:"status"`
	Text    string `json:"text"`
}

type logSnapshot struct {
	At         int64           `json:"at"`     // quando a cópia foi feita (segundos)
	Reason     string          `json:"reason"` // o que o BrunOS ia pedir ("ligar", "reiniciar"...)
	Containers []snapContainer `json:"containers"`
}

var (
	snapMu sync.Mutex
	snaps  = map[string]logSnapshot{}
)

// snapshotLogs guarda os logs atuais do app antes de um pedido que recria os containers. Falhar aqui
// nunca impede o pedido (é só para ajudar a rastrear problemas).
func (s *server) snapshotLogs(ctx context.Context, urn, reason string) {
	if s.docker == nil {
		return
	}
	a, ok := s.appByURN(urn)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cts, err := s.appContainers(ctx, a)
	if err != nil || len(cts) == 0 {
		return
	}
	snap := logSnapshot{At: time.Now().Unix(), Reason: reason, Containers: []snapContainer{}}
	for _, c := range cts {
		body, mux, err := s.docker.Logs(ctx, c.ID, snapTail, false)
		if err != nil {
			continue
		}
		var b bytes.Buffer
		lw := &limitWriter{w: &b, n: snapLimit}
		if mux {
			_ = dockerc.Demux(body, lw)
		} else {
			_, _ = io.Copy(lw, body)
		}
		body.Close()
		snap.Containers = append(snap.Containers, snapContainer{Name: c.Name(), Service: c.Service(), Created: c.Created, Status: c.Status, Text: b.String()})
	}
	if len(snap.Containers) == 0 {
		return
	}
	snapMu.Lock()
	snaps[urn] = snap
	snapMu.Unlock()
}

// hAppLogsPrev: GET /api/apps/logs/prev?urn= — a última cópia dos logs feita antes de recriar o app.
func (s *server) hAppLogsPrev(w http.ResponseWriter, r *http.Request) {
	snapMu.Lock()
	snap, ok := snaps[r.URL.Query().Get("urn")]
	snapMu.Unlock()
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "nenhuma cópia dos logs de antes"})
		return
	}
	writeJSON(w, 200, snap)
}

// tipiBusy diz se o Runtipi está no meio de algo com o app (instalando, atualizando...). Nesse tempo,
// ligar, parar, reconfigurar ou desinstalar recriaria o container no meio do caminho (foi o que estragou o
// banco de um app logo na instalação, 17/11/2026). Sem resposta do Runtipi (script antigo), não trava.
func (s *server) tipiBusy(ctx context.Context, urn string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	t, err := s.tipiInfo(ctx, urn)
	if err != nil || t.App == nil {
		return "", false
	}
	switch t.App.Status {
	case "running", "stopped", "missing", "":
		return t.App.Status, false
	}
	return t.App.Status, true
}

// tipiStates traduz o estado do Runtipi para a tela.
var tipiStates = map[string]string{"installing": "instalando", "updating": "atualizando", "uninstalling": "desinstalando",
	"starting": "ligando", "stopping": "parando", "restarting": "reiniciando", "backing_up": "fazendo backup",
	"restoring": "restaurando um backup", "resetting": "reiniciando do zero"}

func busyMsg(st string) string {
	t := tipiStates[st]
	if t == "" {
		t = st
	}
	return "o Runtipi ainda está " + t + " este app: espere terminar (ligar, parar ou mudar agora recriaria o app no meio do caminho)"
}
