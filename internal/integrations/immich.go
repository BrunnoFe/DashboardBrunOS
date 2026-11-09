package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Immich (API 1.x/2.x/3.x): chave de API no cabeçalho x-api-key.
//   - GET  /api/assets/statistics          fotos e vídeos do dono da chave (permissão asset.statistics)
//   - GET  /api/server/statistics          espaço usado no servidor (só chave de administrador)
//   - GET  /api/memories?for=…             lembranças "neste dia" (memory.read)
//   - POST /api/search/random              fotos aleatórias, favoritas ou de um álbum (asset.read)
//   - GET  /api/assets/{id}/thumbnail?size=preview   a imagem do formato Mídia (asset.view)
//
// As listas de fotos são trocadas uma vez por dia (a troca de foto no widget é feita no navegador,
// percorrendo a lista): assim a foto não muda a cada busca e o Immich quase não trabalha.
type immich struct{}

func init() { register(immich{}) }

const immichPhotos = 24

func (immich) Info() KindInfo {
	return KindInfo{
		ID: "immich", Name: "Immich", Desc: "Fotos e vídeos: totais e fotos no formato Mídia (neste dia, aleatórias, favoritas ou de um álbum)",
		Fields: []Field{
			{ID: "url", Label: "Endereço", Type: "url", Placeholder: "http://192.168.0.10:2283", Required: true},
			{ID: "secret", Label: "Chave de API", Type: "secret", Required: true,
				Help: "No Immich: foto do perfil → Configurações da conta → Chaves de API → Nova chave. Permissões: asset.read, asset.view, asset.statistics, memory.read e album.read (ou todas). Chave de administrador também mostra o espaço usado."},
			{ID: "album", Label: "Álbum (opcional)", Type: "text", Placeholder: "ex.: Viagens",
				Help: "Nome do álbum usado na métrica “Fotos do álbum”"},
		},
		Interval: 600,
		Metrics: []MetricDef{
			{ID: "photos", Name: "Fotos", Kind: Num, Icon: "image"},
			{ID: "videos", Name: "Vídeos", Kind: Num, Icon: "film"},
			{ID: "total", Name: "Fotos e vídeos", Kind: Num, Icon: "image"},
			{ID: "usage", Name: "Espaço usado (chave de administrador)", Kind: Num, Unit: "B", Icon: "disks"},
			{ID: "version", Name: "Versão", Kind: Text, Icon: "info"},
			{ID: "photo_today", Name: "Fotos: neste dia", Kind: Media, Icon: "image", Look: "photo"},
			{ID: "photo_random", Name: "Fotos: aleatórias", Kind: Media, Icon: "image", Look: "photo"},
			{ID: "photo_favorite", Name: "Fotos: favoritas", Kind: Media, Icon: "star", Look: "photo"},
			{ID: "photo_album", Name: "Fotos: do álbum escolhido", Kind: Media, Icon: "image", Look: "photo"},
		},
		Alerts: []AlertDef{
			{ID: "down", Name: "Immich sem responder", Desc: "Avisa quando o Immich para de responder por 5 minutos", Op: "down", Level: "aviso", Fixed: true},
		},
		Runtipi: []string{"immich"},
	}
}

type immichAsset struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	LocalDateTime string `json:"localDateTime"`
	FileCreatedAt string `json:"fileCreatedAt"`
	Exif          *struct {
		City    string `json:"city"`
		Country string `json:"country"`
	} `json:"exifInfo"`
}

// postJSON faz um POST com corpo JSON e decodifica a resposta (mesmas mensagens do getJSON).
func postJSON(ctx context.Context, c *http.Client, u string, hdr map[string]string, body, v any) (int, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return 0, ferr("endereço inválido")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "BrunOS")
	for k, val := range hdr {
		req.Header.Set(k, val)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, ferr("não consegui falar com o app (confira o endereço e se ele está ligado)")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return resp.StatusCode, ferr("o app recusou o acesso (confira a chave e as permissões)")
		}
		return resp.StatusCode, ferr("o app respondeu com erro %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v); err != nil {
		return resp.StatusCode, ferr("resposta do app em formato inesperado")
	}
	return resp.StatusCode, nil
}

var meses = [...]string{"jan.", "fev.", "mar.", "abr.", "maio", "jun.", "jul.", "ago.", "set.", "out.", "nov.", "dez."}

// dataBR: "8 de nov. de 2019".
func dataBR(t time.Time) string {
	return fmt.Sprintf("%d de %s de %d", t.Day(), meses[t.Month()-1], t.Year())
}

func parseWhen(s string) (time.Time, bool) {
	for _, l := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// immichItem: legenda com a data e a cidade da foto.
func immichItem(a immichAsset, extra string) Item {
	label := ""
	if t, ok := parseWhen(a.LocalDateTime); ok {
		label = dataBR(t)
	} else if t, ok := parseWhen(a.FileCreatedAt); ok {
		label = dataBR(t)
	}
	place := ""
	if a.Exif != nil {
		place = strings.Trim(a.Exif.City+", "+a.Exif.Country, ", ")
	}
	return Item{ID: a.ID, Img: a.ID, Label: label, Text: strings.Trim(strings.Join([]string{extra, place}, " · "), " ·")}
}

func immichItems(list []immichAsset, extra func(immichAsset) string) Value {
	items := []Item{}
	for _, a := range list {
		if a.ID == "" || (a.Type != "" && a.Type != "IMAGE") {
			continue
		}
		ex := ""
		if extra != nil {
			ex = extra(a)
		}
		items = append(items, immichItem(a, ex))
		if len(items) >= immichPhotos {
			break
		}
	}
	return Value{List: items}
}

func (immich) Fetch(ctx context.Context, c *http.Client, cfg Config) (map[string]Value, error) {
	base := strings.TrimRight(cfg.URL, "/") + "/api"
	hdr := map[string]string{"x-api-key": cfg.Secret}
	var st struct {
		Images float64 `json:"images"`
		Videos float64 `json:"videos"`
		Total  float64 `json:"total"`
	}
	if _, err := getJSON(ctx, c, base+"/assets/statistics", hdr, "", "", &st); err != nil {
		return nil, err
	}
	out := map[string]Value{"photos": N(st.Images), "videos": N(st.Videos), "total": N(st.Total)}

	now := nowFunc()
	day := now.Format("2006-01-02")
	prev := cfg.Prev
	keep := func(keys ...string) {
		for _, k := range keys {
			if v, ok := prev[k]; ok {
				out[k] = v
			}
		}
	}
	// espaço usado e versão: uma vez por hora
	if p, ok := prev["_hour"]; ok && p.Text != nil && *p.Text == now.Format("2006-01-02T15") {
		keep("usage", "version", "_hour")
	} else {
		var ss struct {
			Usage float64 `json:"usage"`
		}
		if _, err := getJSON(ctx, c, base+"/server/statistics", hdr, "", "", &ss); err == nil {
			out["usage"] = N(ss.Usage)
		}
		var ab struct {
			Version string `json:"version"`
		}
		if _, err := getJSON(ctx, c, base+"/server/about", hdr, "", "", &ab); err == nil && ab.Version != "" {
			out["version"] = T(ab.Version)
		}
		out["_hour"] = T(now.Format("2006-01-02T15"))
	}
	// listas de fotos: uma vez por dia (e de novo se o álbum escolhido mudar)
	album := cfg.Options["album"]
	if p, ok := prev["_day"]; ok && p.Text != nil && *p.Text == day+"|"+album {
		keep("photo_today", "photo_random", "photo_favorite", "photo_album", "_day")
		return out, nil
	}
	random := func(extra map[string]any) []immichAsset {
		body := map[string]any{"size": immichPhotos, "type": "IMAGE", "withExif": true}
		for k, v := range extra {
			body[k] = v
		}
		var list []immichAsset
		if _, err := postJSON(ctx, c, base+"/search/random", hdr, body, &list); err != nil {
			return nil
		}
		return list
	}
	rnd := random(nil)
	out["photo_random"] = immichItems(rnd, nil)
	out["photo_favorite"] = immichItems(random(map[string]any{"isFavorite": true}), nil)

	var mems []struct {
		Type string `json:"type"`
		Data struct {
			Year int `json:"year"`
		} `json:"data"`
		Assets []immichAsset `json:"assets"`
	}
	today := []immichAsset{}
	years := map[string]int{}
	q := url.Values{"for": {now.Format(time.RFC3339)}, "type": {"on_this_day"}}
	if _, err := getJSON(ctx, c, base+"/memories?"+q.Encode(), hdr, "", "", &mems); err == nil {
		for _, m := range mems {
			for _, a := range m.Assets {
				today = append(today, a)
				years[a.ID] = m.Data.Year
			}
		}
	}
	if len(today) > 0 {
		out["photo_today"] = immichItems(today, func(a immichAsset) string {
			y := years[a.ID]
			switch n := now.Year() - y; {
			case y == 0 || n <= 0:
				return ""
			case n == 1:
				return "há 1 ano"
			default:
				return fmt.Sprintf("há %d anos", n)
			}
		})
	} else {
		out["photo_today"] = immichItems(rnd, nil) // hoje não há lembranças: fotos aleatórias
	}
	out["photo_album"] = Value{List: []Item{}}
	if album != "" {
		var albums []struct {
			ID   string `json:"id"`
			Name string `json:"albumName"`
		}
		if _, err := getJSON(ctx, c, base+"/albums", hdr, "", "", &albums); err == nil {
			for _, a := range albums {
				if strings.EqualFold(strings.TrimSpace(a.Name), album) || a.ID == album {
					out["photo_album"] = immichItems(random(map[string]any{"albumIds": []string{a.ID}}), nil)
					break
				}
			}
		}
	}
	out["_day"] = T(day + "|" + album)
	return out, nil
}

var immichID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Image devolve a foto em tamanho "prévia" (algumas centenas de KB), nunca o original.
func (immich) Image(ctx context.Context, c *http.Client, cfg Config, ref string) (io.ReadCloser, error) {
	if !immichID.MatchString(ref) {
		return nil, &ErrInput{"foto inválida"}
	}
	return getImage(ctx, c, strings.TrimRight(cfg.URL, "/")+"/api/assets/"+ref+"/thumbnail?size=preview", map[string]string{"x-api-key": cfg.Secret})
}

// getImage faz o GET de uma imagem e devolve o corpo (quem chama limita o tamanho e fecha).
func getImage(ctx context.Context, c *http.Client, u string, hdr map[string]string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, ferr("endereço inválido")
	}
	req.Header.Set("User-Agent", "BrunOS")
	req.Header.Set("Accept", "image/*")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, ferr("não consegui falar com o app (confira o endereço e se ele está ligado)")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, ferr("o app não entregou a imagem (erro %d)", resp.StatusCode)
	}
	return resp.Body, nil
}
