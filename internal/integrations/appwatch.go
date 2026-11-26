package integrations

// Aviso de app compatível (0.39.0, decisão 74): quando aparece no Runtipi um app que o BrunOS sabe ler
// e ainda não há Integração daquele Tipo, o sino avisa uma vez ("O Transmission pode ser conectado").
// Os apps já vistos ficam num arquivo pequeno em /data. Na primeira vez (sem o arquivo), só anota os
// apps que já estão instalados, sem avisar: o aviso é para o que for instalado depois.

import (
	"sort"
	"strings"
	"sync"

	"brunos/internal/events"
	"brunos/internal/store"
)

// AppWatch guarda os apps compatíveis já vistos e avisa dos novos.
type AppWatch struct {
	File   string // vazio = só em memória (testes)
	Events *events.Store
	Store  *Store

	mu     sync.Mutex
	loaded bool
	seen   map[string]bool
}

type appWatchFile struct {
	Apps []string `json:"apps"`
}

// KindOfApp devolve o Tipo de integração de um app do Runtipi pelo id dele ("" = nenhum).
func KindOfApp(appID string) *KindInfo {
	id := strings.ToLower(appID)
	for _, k := range kinds {
		info := k.Info()
		for _, rid := range info.Runtipi {
			if id == rid || strings.HasPrefix(id, rid+"-") {
				return &info
			}
		}
	}
	return nil
}

// Check confere a lista de apps instalados e avisa dos compatíveis que apareceram desde a última vez.
// Devolve quantos avisos deu.
func (w *AppWatch) Check(apps []AppRef) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	first := false
	if !w.loaded {
		w.loaded, w.seen = true, map[string]bool{}
		var f appWatchFile
		found := false
		if w.File != "" {
			if ok, err := store.ReadJSON(w.File, &f); err == nil && ok {
				found = true
			}
		}
		for _, a := range f.Apps {
			w.seen[a] = true
		}
		first = !found
	}
	have := map[string]bool{} // Tipos que já têm Integração
	if w.Store != nil {
		for _, it := range w.Store.List() {
			have[it.Kind] = true
		}
	}
	sent, changed := 0, false
	for _, a := range apps {
		info := KindOfApp(a.ID)
		if info == nil || w.seen[a.ID] {
			continue
		}
		w.seen[a.ID], changed = true, true
		if first || have[info.ID] || w.Events == nil {
			continue
		}
		name := a.Name
		if name == "" {
			name = info.Name
		}
		if w.Events.Emit(events.Event{Kind: "integration-suggest", Level: events.Info, Key: "integ-suggest:" + a.ID,
			Title: name + " pode ser conectado",
			Body:  "O BrunOS sabe ler o " + name + ". Abra Integrações, no dock, para ter widgets, avisos e mais.",
			Meta:  map[string]any{"kind": info.ID, "app": a.ID}}) {
			sent++
		}
	}
	if (changed || first) && w.File != "" {
		list := make([]string, 0, len(w.seen))
		for id := range w.seen {
			list = append(list, id)
		}
		sort.Strings(list)
		_ = store.WriteJSON(w.File, appWatchFile{Apps: list}, 0o600)
	}
	return sent
}
