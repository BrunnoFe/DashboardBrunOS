package integrations

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Plex Media Server: token no cabeçalho X-Plex-Token e respostas em JSON (Accept).
//   - GET /status/sessions     o que está tocando agora (quem, onde, quanto já foi, se converte)
//   - GET /library/sections    bibliotecas; o total de cada uma vem de …/all com Container-Size=0
//   - GET /identity            versão
//   - GET /photo/:/transcode   capa já reduzida pelo próprio Plex (formato Mídia)
//
// As contagens das bibliotecas são refeitas uma vez por hora.
type plex struct{}

func init() { register(plex{}) }

func (plex) Info() KindInfo {
	return KindInfo{
		ID: "plex", Name: "Plex", Desc: "O que está tocando agora, com a capa, e o tamanho das bibliotecas",
		Fields: []Field{
			{ID: "url", Label: "Endereço", Type: "url", Placeholder: "http://192.168.0.10:32400", Required: true},
			{ID: "secret", Label: "Token do Plex", Type: "secret", Required: true,
				Help: "No Plex pelo navegador: abra qualquer filme → ⋯ → Obter informações → Ver XML. O token é o texto depois de “X-Plex-Token=” no endereço da página que abrir."},
		},
		Interval: 15,
		Metrics: []MetricDef{
			{ID: "sessions", Name: "Assistindo agora", Kind: Num, Icon: "film"},
			{ID: "transcodes", Name: "Convertendo agora (transcode)", Kind: Num, Icon: "cpu"},
			{ID: "now_playing", Name: "O que está tocando", Kind: List, Icon: "film"},
			{ID: "now_media", Name: "Capa do que está tocando", Kind: Media, Icon: "film", Look: "poster"},
			{ID: "movies", Name: "Filmes", Kind: Num, Icon: "film"},
			{ID: "shows", Name: "Séries", Kind: Num, Icon: "monitor"},
			{ID: "artists", Name: "Artistas (música)", Kind: Num, Icon: "music"},
			{ID: "version", Name: "Versão", Kind: Text, Icon: "info"},
		},
		Alerts: []AlertDef{
			{ID: "down", Name: "Plex sem responder", Desc: "Avisa quando o Plex para de responder por 5 minutos", Op: "down", Level: "aviso", Fixed: true},
		},
		Runtipi: []string{"plex"},
	}
}

type plexSession struct {
	Type             string  `json:"type"`
	Title            string  `json:"title"`
	GrandparentTitle string  `json:"grandparentTitle"`
	Index            int     `json:"index"`
	ParentIndex      int     `json:"parentIndex"`
	Thumb            string  `json:"thumb"`
	ParentThumb      string  `json:"parentThumb"`
	GrandparentThumb string  `json:"grandparentThumb"`
	Duration         float64 `json:"duration"`
	ViewOffset       float64 `json:"viewOffset"`
	User             struct {
		Title string `json:"title"`
	} `json:"User"`
	Player struct {
		Title string `json:"title"`
		State string `json:"state"`
	} `json:"Player"`
	Transcode *struct{} `json:"TranscodeSession"`
}

// título amigável: "Série · T2E5 · Episódio", "Artista — Música" ou o filme
func (s plexSession) name() string {
	switch s.Type {
	case "episode":
		return strings.Trim(fmt.Sprintf("%s · T%dE%d · %s", s.GrandparentTitle, s.ParentIndex, s.Index, s.Title), " ·")
	case "track":
		return strings.Trim(s.GrandparentTitle+" — "+s.Title, " —")
	default:
		return s.Title
	}
}

func (s plexSession) cover() string {
	switch {
	case s.Type == "episode" && s.GrandparentThumb != "":
		return s.GrandparentThumb
	case s.Type == "track" && s.ParentThumb != "":
		return s.ParentThumb
	}
	return s.Thumb
}

func (plex) Fetch(ctx context.Context, c *http.Client, cfg Config) (map[string]Value, error) {
	base := strings.TrimRight(cfg.URL, "/")
	hdr := map[string]string{"X-Plex-Token": cfg.Secret}
	var ss struct {
		MediaContainer struct {
			Metadata []plexSession `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if _, err := getJSON(ctx, c, base+"/status/sessions", hdr, "", "", &ss); err != nil {
		return nil, err
	}
	list, media := []Item{}, []Item{}
	trans := 0
	for _, s := range ss.MediaContainer.Metadata {
		if s.Transcode != nil {
			trans++
		}
		who := strings.Trim(s.User.Title+" · "+s.Player.Title, " ·")
		if s.Player.State == "paused" {
			who = strings.Trim(who+" · pausado", " ·")
		}
		var pct *float64
		if s.Duration > 0 {
			pct = fp(round1(s.ViewOffset / s.Duration * 100))
		}
		list = append(list, Item{Label: s.name(), Text: who, Num: pct})
		media = append(media, Item{Label: s.name(), Text: who, Num: pct, Img: s.cover()})
	}
	out := map[string]Value{
		"sessions": N(float64(len(list))), "transcodes": N(float64(trans)),
		"now_playing": {List: list}, "now_media": {List: media},
	}

	hour := nowFunc().Format("2006-01-02T15")
	if p, ok := cfg.Prev["_hour"]; ok && p.Text != nil && *p.Text == hour {
		for _, k := range []string{"movies", "shows", "artists", "version", "_hour"} {
			if v, ok := cfg.Prev[k]; ok {
				out[k] = v
			}
		}
		return out, nil
	}
	var secs struct {
		MediaContainer struct {
			Directory []struct {
				Key  string `json:"key"`
				Type string `json:"type"`
			} `json:"Directory"`
		} `json:"MediaContainer"`
	}
	totals := map[string]float64{}
	if _, err := getJSON(ctx, c, base+"/library/sections", hdr, "", "", &secs); err == nil {
		for _, d := range secs.MediaContainer.Directory {
			if d.Type != "movie" && d.Type != "show" && d.Type != "artist" {
				continue
			}
			var all struct {
				MediaContainer struct {
					TotalSize float64 `json:"totalSize"`
					Size      float64 `json:"size"`
				} `json:"MediaContainer"`
			}
			u := base + "/library/sections/" + url.PathEscape(d.Key) + "/all?X-Plex-Container-Start=0&X-Plex-Container-Size=0"
			if _, err := getJSON(ctx, c, u, hdr, "", "", &all); err == nil {
				n := all.MediaContainer.TotalSize
				if n == 0 {
					n = all.MediaContainer.Size
				}
				totals[d.Type] += n
			}
		}
		out["movies"], out["shows"], out["artists"] = N(totals["movie"]), N(totals["show"]), N(totals["artist"])
	}
	var id struct {
		MediaContainer struct {
			Version string `json:"version"`
		} `json:"MediaContainer"`
	}
	if _, err := getJSON(ctx, c, base+"/identity", hdr, "", "", &id); err == nil && id.MediaContainer.Version != "" {
		v := id.MediaContainer.Version
		if i := strings.Index(v, "-"); i > 0 {
			v = v[:i]
		}
		out["version"] = T(v)
	}
	out["_hour"] = T(hour)
	return out, nil
}

// ── Abrir no Plex (decisão 73) ────────────────────────────────────
// Vídeo que o navegador não toca: procura o mesmo arquivo na biblioteca do Plex (que converte o som) pelo
// nome do arquivo. A busca usa o título tirado do nome; o resultado só vale se o nome do arquivo do Plex
// for igual (nunca abre um filme parecido). Séries: procura dentro dos episódios da série achada.

var (
	plexCut   = regexp.MustCompile(`(?i)\b((19|20)\d{2}|s\d{1,2}e\d{1,3}|\d{3,4}p|bluray|blu-ray|web-?dl|webrip|hdtv|dvdrip|remux|x26[45]|h\.?26[45]|hevc|remastered|proper|extended|unrated)\b`)
	plexKey   = regexp.MustCompile(`^[0-9]+$`)
	plexMID   = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	plexSpace = regexp.MustCompile(`[._\[\]()-]+`)
)

// plexQuery tira do nome do arquivo o título provável ("X2.2003.REMASTERED.mkv" → "X2").
func plexQuery(file string) string {
	name := strings.TrimSuffix(file, pathExt(file))
	name = plexSpace.ReplaceAllString(name, " ")
	for _, loc := range plexCut.FindAllStringIndex(name, -1) {
		if loc[0] > 0 { // "2001 Uma Odisseia… 1968": o ano do começo é título
			name = name[:loc[0]]
			break
		}
	}
	return strings.Join(strings.Fields(name), " ")
}

func pathExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 && len(name)-i <= 6 {
		return name[i:]
	}
	return ""
}

type plexItem struct {
	RatingKey string `json:"ratingKey"`
	Type      string `json:"type"`
	Media     []struct {
		Part []struct {
			File string `json:"file"`
		} `json:"Part"`
	} `json:"Media"`
}

func (p plexItem) has(file string) bool {
	for _, m := range p.Media {
		for _, pt := range m.Part {
			f := pt.File
			if i := strings.LastIndexAny(f, `/\`); i >= 0 {
				f = f[i+1:]
			}
			if strings.EqualFold(f, file) {
				return true
			}
		}
	}
	return false
}

// plexFind devolve o endereço da página do arquivo no Plex ("" = não achou).
func plexFind(ctx context.Context, c *http.Client, cfg Config, file string) (string, error) {
	q := plexQuery(file)
	if q == "" {
		return "", nil
	}
	base := strings.TrimRight(cfg.URL, "/")
	hdr := map[string]string{"X-Plex-Token": cfg.Secret}
	var res struct {
		MediaContainer struct {
			Metadata []plexItem `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if _, err := getJSON(ctx, c, base+"/search?limit=30&query="+url.QueryEscape(q), hdr, "", "", &res); err != nil {
		return "", err
	}
	key, shows := "", 0
	for _, it := range res.MediaContainer.Metadata {
		if it.has(file) {
			key = it.RatingKey
			break
		}
	}
	for _, it := range res.MediaContainer.Metadata {
		if key != "" || shows >= 3 {
			break
		}
		if it.Type != "show" || !plexKey.MatchString(it.RatingKey) {
			continue
		}
		shows++
		var eps struct {
			MediaContainer struct {
				Metadata []plexItem `json:"Metadata"`
			} `json:"MediaContainer"`
		}
		if _, err := getJSON(ctx, c, base+"/library/metadata/"+it.RatingKey+"/allLeaves", hdr, "", "", &eps); err != nil {
			continue
		}
		for _, ep := range eps.MediaContainer.Metadata {
			if ep.has(file) {
				key = ep.RatingKey
				break
			}
		}
	}
	if !plexKey.MatchString(key) {
		return "", nil
	}
	var id struct {
		MediaContainer struct {
			MachineIdentifier string `json:"machineIdentifier"`
		} `json:"MediaContainer"`
	}
	if _, err := getJSON(ctx, c, base+"/identity", hdr, "", "", &id); err != nil {
		return "", err
	}
	mid := id.MediaContainer.MachineIdentifier
	if !plexMID.MatchString(mid) {
		return "", nil
	}
	return base + "/web/index.html#!/server/" + mid + "/details?key=" + url.QueryEscape("/library/metadata/"+key), nil
}

// hPlexFind procura o arquivo em todas as Integrações Plex. {"url": ""} = não achou; {"none": true} =
// não há Plex cadastrado (a interface nem mostra o botão).
func (h *Hub) hPlexFind(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" || len(name) > 300 || strings.ContainsAny(name, `/\`) {
		writeErr(w, &ErrInput{"nome inválido"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	found := false
	for _, it := range h.Store.List() {
		if it.Kind != "plex" {
			continue
		}
		found = true
		cfg, err := h.Store.Config(it)
		if err != nil {
			continue
		}
		if u, err := plexFind(ctx, h.Client, cfg, name); err == nil && u != "" {
			writeJSON(w, 200, map[string]any{"url": u})
			return
		}
	}
	writeJSON(w, 200, map[string]any{"url": "", "none": !found})
}

// só caminhos de capa do próprio Plex (ex.: /library/metadata/123/thumb/1700000000)
var plexThumb = regexp.MustCompile(`^/library/metadata/[0-9]+/[A-Za-z]+(/[0-9]+)?$`)

// Image pede a capa já reduzida ao próprio Plex (transcodificador de fotos), em vez do arquivo original.
func (plex) Image(ctx context.Context, c *http.Client, cfg Config, ref string) (io.ReadCloser, error) {
	if !plexThumb.MatchString(ref) {
		return nil, &ErrInput{"capa inválida"}
	}
	q := url.Values{"width": {"360"}, "height": {"540"}, "minSize": {"1"}, "upscale": {"0"}, "url": {ref}}
	return getImage(ctx, c, strings.TrimRight(cfg.URL, "/")+"/photo/:/transcode?"+q.Encode(), map[string]string{"X-Plex-Token": cfg.Secret})
}
