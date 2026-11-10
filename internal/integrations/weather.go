package integrations

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Clima pelo Open-Meteo (https://open-meteo.com): grátis, sem cadastro e sem chave. O Open-Meteo só
// recebe as coordenadas da cidade escolhida. A cidade é escolhida por nome (Geocode) e o formulário
// guarda o nome, a latitude e a longitude nos campos extras. Ícones: códigos de tempo da OMM (WMO).
type weather struct{}

func init() { register(weather{}) }

// Endereços trocáveis nos testes.
var (
	weatherBase = "https://api.open-meteo.com"
	geoBase     = "https://geocoding-api.open-meteo.com"
)

func (weather) Info() KindInfo {
	return KindInfo{
		ID: "weather", Name: "Clima", Desc: "Tempo agora e previsão dos próximos dias (Open-Meteo, sem chave)",
		NoURL: true, Interval: 900,
		Note: "Os dados vêm do Open-Meteo, um serviço gratuito. Ele recebe só as coordenadas da cidade.",
		Fields: []Field{
			{ID: "city", Label: "Cidade", Type: "city", Placeholder: "Digite o nome da cidade", Required: true,
				Help: "Escreva o nome e escolha na lista"},
			{ID: "lat", Label: "Latitude", Type: "hidden"},
			{ID: "lon", Label: "Longitude", Type: "hidden"},
		},
		Metrics: []MetricDef{
			{ID: "temp", Name: "Temperatura agora", Kind: Num, Unit: "°C", Icon: "temp"},
			{ID: "feels", Name: "Sensação térmica", Kind: Num, Unit: "°C", Icon: "temp"},
			{ID: "cond", Name: "Condição", Kind: Text, Icon: "wx-partly"},
			{ID: "max", Name: "Máxima de hoje", Kind: Num, Unit: "°C", Icon: "arrowUp"},
			{ID: "min", Name: "Mínima de hoje", Kind: Num, Unit: "°C", Icon: "arrowDown"},
			{ID: "rain", Name: "Chance de chuva hoje", Kind: Num, Unit: "%", Icon: "wx-rain"},
			{ID: "humidity", Name: "Umidade", Kind: Num, Unit: "%", Icon: "wx-drop"},
			{ID: "wind", Name: "Vento", Kind: Num, Unit: "km/h", Icon: "wx-wind"},
			{ID: "forecast", Name: "Previsão (7 dias)", Kind: List, Icon: "wx-partly"},
			{ID: "city", Name: "Cidade", Kind: Text, Icon: "home"},
		},
		Alerts: []AlertDef{
			{ID: "hot", Name: "Calor forte", Desc: "Avisa quando a temperatura passar disto", Metric: "temp", Op: ">", Default: 35, Unit: "°C", Level: "info"},
			{ID: "rain", Name: "Chuva provável hoje", Desc: "Avisa quando a chance de chuva hoje passar disto", Metric: "rain", Op: ">", Default: 70, Unit: "%", Level: "info"},
		},
	}
}

// wmo traduz o código de tempo da OMM em texto e ícone (dia ou noite).
func wmo(code int, day bool) (string, string) {
	pick := func(d, n string) string {
		if day {
			return d
		}
		return n
	}
	switch code {
	case 0:
		return pick("Céu limpo", "Céu limpo"), pick("wx-sun", "wx-moon")
	case 1:
		return "Quase limpo", pick("wx-partly", "wx-partly-night")
	case 2:
		return "Parcialmente nublado", pick("wx-partly", "wx-partly-night")
	case 3:
		return "Nublado", "wx-cloud"
	case 45, 48:
		return "Neblina", "wx-fog"
	case 51, 53, 55, 56, 57:
		return "Garoa", "wx-drizzle"
	case 61, 63, 66, 80, 81:
		return "Chuva", "wx-rain"
	case 65, 67, 82:
		return "Chuva forte", "wx-heavy"
	case 71, 73, 75, 77, 85, 86:
		return "Neve", "wx-snow"
	case 95:
		return "Tempestade", "wx-storm"
	case 96, 99:
		return "Tempestade com granizo", "wx-storm"
	}
	return "—", "wx-cloud"
}

var weekdays = []string{"Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"}

type omResp struct {
	Current struct {
		Temp     *float64 `json:"temperature_2m"`
		Feels    *float64 `json:"apparent_temperature"`
		Humidity *float64 `json:"relative_humidity_2m"`
		Code     *int     `json:"weather_code"`
		Wind     *float64 `json:"wind_speed_10m"`
		IsDay    *int     `json:"is_day"`
	} `json:"current"`
	Daily struct {
		Time []string   `json:"time"`
		Code []*int     `json:"weather_code"`
		Max  []*float64 `json:"temperature_2m_max"`
		Min  []*float64 `json:"temperature_2m_min"`
		Rain []*float64 `json:"precipitation_probability_max"`
	} `json:"daily"`
}

func coords(cfg Config) (lat, lon float64, err error) {
	lat, e1 := strconv.ParseFloat(cfg.Options["lat"], 64)
	lon, e2 := strconv.ParseFloat(cfg.Options["lon"], 64)
	if e1 != nil || e2 != nil || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
		return 0, 0, ferr("escolha a cidade na lista que aparece ao digitar o nome")
	}
	return lat, lon, nil
}

func (weather) Fetch(ctx context.Context, c *http.Client, cfg Config) (map[string]Value, error) {
	lat, lon, err := coords(cfg)
	if err != nil {
		return nil, err
	}
	q := url.Values{
		"latitude": {strconv.FormatFloat(lat, 'f', 4, 64)}, "longitude": {strconv.FormatFloat(lon, 'f', 4, 64)},
		"current":       {"temperature_2m,apparent_temperature,relative_humidity_2m,weather_code,wind_speed_10m,is_day"},
		"daily":         {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max"},
		"timezone":      {"auto"},
		"forecast_days": {"7"},
	}
	var r omResp
	if _, err := getJSON(ctx, c, weatherBase+"/v1/forecast?"+q.Encode(), nil, "", "", &r); err != nil {
		if fe, ok := err.(*FetchError); ok && strings.HasPrefix(fe.Msg, "não consegui falar") {
			return nil, ferr("não consegui falar com o Open-Meteo (o servidor está sem internet?)")
		}
		return nil, err
	}
	out := map[string]Value{}
	if v := r.Current.Temp; v != nil {
		out["temp"] = N(round1(*v))
	}
	if v := r.Current.Feels; v != nil {
		out["feels"] = N(round1(*v))
	}
	if v := r.Current.Humidity; v != nil {
		out["humidity"] = N(*v)
	}
	if v := r.Current.Wind; v != nil {
		out["wind"] = N(math.Round(*v))
	}
	if r.Current.Code != nil {
		day := r.Current.IsDay == nil || *r.Current.IsDay == 1
		txt, ic := wmo(*r.Current.Code, day)
		v := T(txt)
		v.Icon = ic
		out["cond"] = v
	}
	if name := strings.TrimSpace(cfg.Options["city"]); name != "" {
		out["city"] = T(name)
	}
	d := r.Daily
	at := func(xs []*float64, i int) *float64 {
		if i < len(xs) {
			return xs[i]
		}
		return nil
	}
	if v := at(d.Max, 0); v != nil {
		out["max"] = N(math.Round(*v))
	}
	if v := at(d.Min, 0); v != nil {
		out["min"] = N(math.Round(*v))
	}
	if v := at(d.Rain, 0); v != nil {
		out["rain"] = N(*v)
	}
	var items []Item
	for i, day := range d.Time {
		t, err := time.Parse("2006-01-02", day)
		if err != nil {
			continue
		}
		label := weekdays[t.Weekday()]
		if i == 0 {
			label = "Hoje"
		}
		it := Item{Label: label, ID: day}
		if i < len(d.Code) && d.Code[i] != nil {
			_, it.Icon = wmo(*d.Code[i], true)
		}
		mx, mn := at(d.Max, i), at(d.Min, i)
		if mx != nil && mn != nil {
			it.Text = fmt.Sprintf("%.0f° / %.0f°", math.Round(*mx), math.Round(*mn))
		}
		if p := at(d.Rain, i); p != nil && *p >= 30 {
			it.Text += fmt.Sprintf(" · %.0f%%", *p)
		}
		items = append(items, it)
	}
	if items != nil {
		out["forecast"] = Value{List: items}
	}
	if len(out) == 0 {
		return nil, ferr("resposta do Open-Meteo em formato inesperado")
	}
	return out, nil
}

// Place é uma cidade encontrada pelo nome.
type Place struct {
	Name    string  `json:"name"`
	Region  string  `json:"region"`
	Country string  `json:"country"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

// Geocode procura cidades pelo nome (para o formulário do Clima).
func Geocode(ctx context.Context, c *http.Client, name string) ([]Place, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 2 || len(name) > 100 {
		return []Place{}, nil
	}
	q := url.Values{"name": {name}, "count": {"8"}, "language": {"pt"}, "format": {"json"}}
	var r struct {
		Results []struct {
			Name    string  `json:"name"`
			Admin1  string  `json:"admin1"`
			Country string  `json:"country"`
			Lat     float64 `json:"latitude"`
			Lon     float64 `json:"longitude"`
		} `json:"results"`
	}
	if _, err := getJSON(ctx, c, geoBase+"/v1/search?"+q.Encode(), nil, "", "", &r); err != nil {
		return nil, ferr("não consegui procurar a cidade (o servidor está sem internet?)")
	}
	out := make([]Place, 0, len(r.Results))
	for _, x := range r.Results {
		out = append(out, Place{Name: x.Name, Region: x.Admin1, Country: x.Country, Lat: x.Lat, Lon: x.Lon})
	}
	return out, nil
}
