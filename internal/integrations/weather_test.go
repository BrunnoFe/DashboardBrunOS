package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWeather(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/forecast":
			gotQuery = r.URL.RawQuery
			w.Write([]byte(`{"current":{"temperature_2m":27.36,"apparent_temperature":29.1,"relative_humidity_2m":61,"weather_code":2,"wind_speed_10m":11.6,"is_day":0},
			"daily":{"time":["2026-11-10","2026-11-11","2026-11-12"],"weather_code":[2,63,95],"temperature_2m_max":[30.4,25.2,24],"temperature_2m_min":[18.6,17.1,null],"precipitation_probability_max":[10,80,null]}}`))
		case "/v1/search":
			w.Write([]byte(`{"results":[{"name":"Belo Horizonte","admin1":"Minas Gerais","country":"Brasil","latitude":-19.92,"longitude":-43.94}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	weatherBase, geoBase = srv.URL, srv.URL
	defer func() { weatherBase, geoBase = "https://api.open-meteo.com", "https://geocoding-api.open-meteo.com" }()

	k := KindByID("weather")
	if k == nil || !k.Info().NoURL {
		t.Fatal("Clima deveria existir e não pedir endereço")
	}
	if _, err := k.Fetch(context.Background(), srv.Client(), Config{}); err == nil {
		t.Fatal("sem cidade deveria pedir para escolher")
	}
	v, err := k.Fetch(context.Background(), srv.Client(), Config{Options: map[string]string{"city": "Belo Horizonte", "lat": "-19.92", "lon": "-43.94"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "latitude=-19.9200") || !strings.Contains(gotQuery, "timezone=auto") {
		t.Fatalf("consulta inesperada: %s", gotQuery)
	}
	if *v["temp"].Num != 27.4 || *v["max"].Num != 30 || *v["min"].Num != 19 || *v["rain"].Num != 10 || *v["wind"].Num != 12 {
		t.Fatalf("números: %+v", v)
	}
	if c := v["cond"]; *c.Text != "Parcialmente nublado" || c.Icon != "wx-partly-night" {
		t.Fatalf("condição à noite: %+v", c)
	}
	fc := v["forecast"].List
	if len(fc) != 3 || fc[0].Label != "Hoje" || fc[1].Label != "Qua" || fc[1].Icon != "wx-rain" || fc[1].Text != "25° / 17° · 80%" || fc[2].Icon != "wx-storm" || fc[2].Text != "" {
		t.Fatalf("previsão: %+v", fc)
	}
	places, err := Geocode(context.Background(), srv.Client(), "Belo")
	if err != nil || len(places) != 1 || places[0].Region != "Minas Gerais" {
		t.Fatalf("geocode: %v %+v", err, places)
	}
	if p, _ := Geocode(context.Background(), srv.Client(), "a"); len(p) != 0 {
		t.Fatal("nome curto demais não procura")
	}
}
