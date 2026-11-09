package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// Nextcloud: app "serverinfo" (vem com o Nextcloud). GET /ocs/v2.php/apps/serverinfo/api/v1/info com o
// cabeçalho OCS-APIRequest. Preferimos o token do próprio serverinfo (cabeçalho NC-Token, só leitura,
// criado com "occ config:app:set serverinfo token --value ..."); sem ele, usuário administrador + senha
// de app. skipApps/skipUpdate deixam a resposta leve.
type nextcloud struct{}

func init() { register(nextcloud{}) }

func (nextcloud) Info() KindInfo {
	return KindInfo{
		ID: "nextcloud", Name: "Nextcloud", Desc: "Usuários, arquivos, compartilhamentos e espaço livre",
		Fields: []Field{
			{ID: "url", Label: "Endereço", Type: "url", Placeholder: "http://192.168.0.10:8083", Required: true},
			{ID: "user", Label: "Usuário administrador (deixe vazio para usar o token)", Type: "text"},
			{ID: "secret", Label: "Token do serverinfo (ou senha de app)", Type: "secret", Required: true,
				Help: "Recomendado: um token só de leitura. No servidor: sudo docker exec -u www-data <container do Nextcloud> php occ config:app:set serverinfo token --value <um texto aleatório> e cole o mesmo texto aqui."},
		},
		Interval: 300,
		Metrics: []MetricDef{
			{ID: "users", Name: "Usuários", Kind: Num, Icon: "user"},
			{ID: "active_5m", Name: "Ativos nos últimos 5 min", Kind: Num, Icon: "activity"},
			{ID: "active_24h", Name: "Ativos nas últimas 24 h", Kind: Num, Icon: "activity"},
			{ID: "files", Name: "Arquivos", Kind: Num, Icon: "doc"},
			{ID: "shares", Name: "Compartilhamentos", Kind: Num, Icon: "link"},
			{ID: "free", Name: "Espaço livre", Kind: Num, Unit: "B", Icon: "disks"},
			{ID: "free_gb", Name: "Espaço livre (GB)", Kind: Num, Unit: "GB", Icon: "disks"},
			{ID: "db_size", Name: "Tamanho do banco de dados", Kind: Num, Unit: "B", Icon: "box"},
			{ID: "version", Name: "Versão", Kind: Text, Icon: "info"},
		},
		Alerts: []AlertDef{
			{ID: "low_space", Name: "Pouco espaço no Nextcloud", Desc: "Avisa quando o espaço livre fica abaixo disto", Metric: "free_gb", Op: "<", Default: 20, Unit: "GB", Level: "aviso"},
			{ID: "down", Name: "Nextcloud sem responder", Desc: "Avisa quando o Nextcloud para de responder por 5 minutos", Op: "down", Level: "aviso", Fixed: true},
		},
		Runtipi: []string{"nextcloud"},
	}
}

// num aceita número ou texto com número (o serverinfo varia entre versões)
type ncNum float64

func (n *ncNum) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = ncNum(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		var v float64
		if json.Unmarshal([]byte(s), &v) == nil {
			*n = ncNum(v)
		}
	}
	return nil
}

func (nextcloud) Fetch(ctx context.Context, c *http.Client, cfg Config) (map[string]Value, error) {
	hdr := map[string]string{"OCS-APIRequest": "true"}
	user, pass := "", ""
	if strings.TrimSpace(cfg.User) == "" {
		hdr["NC-Token"] = cfg.Secret
	} else {
		user, pass = cfg.User, cfg.Secret
	}
	var r struct {
		OCS struct {
			Data struct {
				Nextcloud struct {
					System struct {
						Version   string `json:"version"`
						FreeSpace ncNum  `json:"freespace"`
					} `json:"system"`
					Storage struct {
						Users ncNum `json:"num_users"`
						Files ncNum `json:"num_files"`
					} `json:"storage"`
					Shares struct {
						Shares ncNum `json:"num_shares"`
					} `json:"shares"`
				} `json:"nextcloud"`
				Server struct {
					Database struct {
						Size ncNum `json:"size"`
					} `json:"database"`
				} `json:"server"`
				Active struct {
					Last5  ncNum `json:"last5minutes"`
					Last24 ncNum `json:"last24hours"`
				} `json:"activeUsers"`
			} `json:"data"`
		} `json:"ocs"`
	}
	u := strings.TrimRight(cfg.URL, "/") + "/ocs/v2.php/apps/serverinfo/api/v1/info?format=json&skipApps=true&skipUpdate=true"
	if _, err := getJSON(ctx, c, u, hdr, user, pass, &r); err != nil {
		return nil, err
	}
	d := r.OCS.Data
	free := float64(d.Nextcloud.System.FreeSpace)
	out := map[string]Value{
		"users": N(float64(d.Nextcloud.Storage.Users)), "files": N(float64(d.Nextcloud.Storage.Files)),
		"shares": N(float64(d.Nextcloud.Shares.Shares)), "active_5m": N(float64(d.Active.Last5)), "active_24h": N(float64(d.Active.Last24)),
		"free": N(free), "free_gb": N(round1(free / 1e9)), "db_size": N(float64(d.Server.Database.Size)),
	}
	if v := d.Nextcloud.System.Version; v != "" {
		out["version"] = T(v)
	}
	return out, nil
}
