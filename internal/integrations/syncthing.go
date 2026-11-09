package integrations

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Syncthing (REST): chave de API no cabeçalho X-API-Key (Ações → Configurações → Geral). Só lê:
// /rest/system/{status,version,connections}, /rest/config/{folders,devices} e /rest/db/status de cada
// pasta (até 20).
type syncthing struct{}

func init() { register(syncthing{}) }

func (syncthing) Info() KindInfo {
	return KindInfo{
		ID: "syncthing", Name: "Syncthing", Desc: "Pastas sincronizadas, quanto falta e aparelhos conectados",
		Fields: []Field{
			{ID: "url", Label: "Endereço", Type: "url", Placeholder: "http://192.168.0.10:8384", Required: true},
			{ID: "secret", Label: "Chave da API", Type: "secret", Required: true,
				Help: "No Syncthing: Ações → Configurações → Geral → Chave da API"},
		},
		Interval: 60,
		Metrics: []MetricDef{
			{ID: "folders", Name: "Pastas", Kind: Num, Icon: "folder"},
			{ID: "synced", Name: "Pastas em dia", Kind: Num, Icon: "check"},
			{ID: "need", Name: "Falta sincronizar", Kind: Num, Unit: "B", Icon: "refresh"},
			{ID: "errors", Name: "Arquivos com erro", Kind: Num, Icon: "alert"},
			{ID: "devices_online", Name: "Aparelhos conectados", Kind: Num, Icon: "network"},
			{ID: "devices", Name: "Aparelhos no total", Kind: Num, Icon: "network"},
			{ID: "folders_list", Name: "Lista: pastas", Kind: List, Icon: "folder"},
			{ID: "devices_list", Name: "Lista: aparelhos", Kind: List, Icon: "network"},
			{ID: "version", Name: "Versão", Kind: Text, Icon: "info"},
			{ID: "uptime", Name: "Tempo ligado", Kind: Num, Unit: "s", Icon: "clock"},
		},
		Alerts: []AlertDef{
			{ID: "errors", Name: "Erro de sincronização", Desc: "Avisa quando alguma pasta tem arquivos que não conseguem sincronizar", Metric: "errors", Op: ">", Default: 0, Level: "aviso", Fixed: true},
			{ID: "down", Name: "Syncthing sem responder", Desc: "Avisa quando o Syncthing para de responder por 5 minutos", Op: "down", Level: "info", Fixed: true},
		},
		Runtipi: []string{"syncthing"},
	}
}

var stStates = map[string]string{"idle": "em dia", "syncing": "sincronizando", "scanning": "verificando", "scan-waiting": "esperando para verificar",
	"sync-waiting": "esperando para sincronizar", "sync-preparing": "preparando", "cleaning": "limpando", "error": "com erro", "unknown": "desconhecido"}

func (syncthing) Fetch(ctx context.Context, c *http.Client, cfg Config) (map[string]Value, error) {
	base := strings.TrimRight(cfg.URL, "/") + "/rest"
	hdr := map[string]string{"X-API-Key": cfg.Secret}
	var sys struct {
		MyID   string  `json:"myID"`
		Uptime float64 `json:"uptime"`
	}
	if _, err := getJSON(ctx, c, base+"/system/status", hdr, "", "", &sys); err != nil {
		return nil, err
	}
	out := map[string]Value{"uptime": N(sys.Uptime)}
	var ver struct {
		Version string `json:"version"`
	}
	if _, err := getJSON(ctx, c, base+"/system/version", hdr, "", "", &ver); err == nil {
		out["version"] = T(ver.Version)
	}

	// aparelhos (menos este) e quais estão conectados
	var devs []struct {
		ID   string `json:"deviceID"`
		Name string `json:"name"`
	}
	var conns struct {
		Connections map[string]struct {
			Connected bool `json:"connected"`
		} `json:"connections"`
	}
	_, _ = getJSON(ctx, c, base+"/config/devices", hdr, "", "", &devs)
	_, _ = getJSON(ctx, c, base+"/system/connections", hdr, "", "", &conns)
	devList := []Item{}
	var online float64
	for _, d := range devs {
		if d.ID == sys.MyID {
			continue
		}
		name := d.Name
		if name == "" && len(d.ID) >= 7 {
			name = d.ID[:7]
		}
		state := "desconectado"
		if conns.Connections[d.ID].Connected {
			state, online = "conectado", online+1
		}
		devList = append(devList, Item{Label: name, Text: state, ID: d.ID})
	}
	sort.SliceStable(devList, func(i, j int) bool { return devList[i].Text < devList[j].Text }) // conectados primeiro
	out["devices"], out["devices_online"], out["devices_list"] = N(float64(len(devList))), N(online), Value{List: devList}

	// pastas: estado e quanto falta de cada uma
	var folders []struct {
		ID     string `json:"id"`
		Label  string `json:"label"`
		Paused bool   `json:"paused"`
	}
	if _, err := getJSON(ctx, c, base+"/config/folders", hdr, "", "", &folders); err != nil {
		return nil, err
	}
	if len(folders) > 20 {
		folders = folders[:20]
	}
	var need, errs, synced float64
	fl := []Item{}
	for _, f := range folders {
		name := f.Label
		if name == "" {
			name = f.ID
		}
		if f.Paused {
			fl = append(fl, Item{Label: name, Text: "pausada", ID: f.ID})
			continue
		}
		var st struct {
			State      string  `json:"state"`
			NeedBytes  float64 `json:"needBytes"`
			Errors     float64 `json:"errors"`
			PullErrors float64 `json:"pullErrors"`
		}
		if _, err := getJSON(ctx, c, base+"/db/status?folder="+url.QueryEscape(f.ID), hdr, "", "", &st); err != nil {
			fl = append(fl, Item{Label: name, Text: "sem resposta", ID: f.ID})
			continue
		}
		e := st.Errors + st.PullErrors
		need += st.NeedBytes
		errs += e
		text := stStates[st.State]
		if text == "" {
			text = st.State
		}
		if st.State == "idle" && st.NeedBytes == 0 && e == 0 {
			synced++
		} else if st.NeedBytes > 0 {
			text += " · falta " + strings.TrimSuffix(fmtRateBR(st.NeedBytes), "/s")
		}
		if e > 0 {
			text += " · erro"
		}
		fl = append(fl, Item{Label: name, Text: text, ID: f.ID})
	}
	out["folders"], out["synced"], out["need"], out["errors"], out["folders_list"] = N(float64(len(folders))), N(synced), N(need), N(errs), Value{List: fl}
	return out, nil
}
