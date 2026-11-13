package tsdb

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
)

// Prefixos de série que a API entrega (o resto é interno).
var allowed = []string{"sys.", "app.", "net.", "disk."}

const maxPerRequest = 16

// Register liga GET /api/history?serie=<nome>&serie=...&periodo=5m|1h|24h|48h|7d|30d.
// Resposta: {"periodo","step","series":{nome:[[t,v],[t,v,pico],...]}} (pico só em 7d e 30d).
// GET /api/history/series?prefixo=disk. lista as séries que existem. wrap aplica autenticação e CSRF.
func (db *DB) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/history", wrap(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		period := q.Get("periodo")
		if _, ok := Periods[period]; !ok {
			jsonErr(w, http.StatusBadRequest, "Período inválido (use 5m, 1h, 24h, 48h, 7d ou 30d)")
			return
		}
		names := q["serie"]
		if len(names) == 0 || len(names) > maxPerRequest {
			jsonErr(w, http.StatusBadRequest, "Informe de 1 a 16 séries")
			return
		}
		out := map[string][][]float64{}
		var step int64
		for _, n := range names {
			if !isAllowed(n) {
				jsonErr(w, http.StatusBadRequest, "Série desconhecida: "+n)
				return
			}
			st, pts := db.Query(n, period)
			step = st
			long := st > FineStep
			rows := make([][]float64, 0, len(pts))
			for _, p := range pts {
				row := []float64{float64(p.T), r2(p.V)}
				if long {
					row = append(row, r2(p.Max))
				}
				rows = append(rows, row)
			}
			out[n] = rows
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"periodo": period, "step": step, "series": out})
	}))
	mux.HandleFunc("GET /api/history/series", wrap(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("prefixo")
		if !isAllowed(p + "x") {
			jsonErr(w, http.StatusBadRequest, "Prefixo desconhecido")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"series": db.Names(p)})
	}))
}

func isAllowed(n string) bool {
	if len(n) > 128 || strings.ContainsAny(n, " \t\r\n") {
		return false
	}
	for _, p := range allowed {
		if strings.HasPrefix(n, p) && len(n) > len(p) {
			return true
		}
	}
	return false
}

func r2(v float32) float64 { return math.Round(float64(v)*100) / 100 }

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
