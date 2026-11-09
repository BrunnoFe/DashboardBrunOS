package integrations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTransmission(t *testing.T) {
	var conflicts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transmission/rpc" || r.Method != http.MethodPost {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("X-Transmission-Session-Id") != "abc" {
			atomic.AddInt32(&conflicts, 1)
			w.Header().Set("X-Transmission-Session-Id", "abc")
			w.WriteHeader(409)
			return
		}
		var req struct {
			Method string `json:"method"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		switch req.Method {
		case "session-stats":
			_, _ = w.Write([]byte(`{"result":"success","arguments":{"activeTorrentCount":3,"pausedTorrentCount":1,"torrentCount":12,"downloadSpeed":2100000,"uploadSpeed":50000,"cumulative-stats":{"downloadedBytes":5e11,"uploadedBytes":2e11}}}`))
		case "torrent-get":
			_, _ = w.Write([]byte(`{"result":"success","arguments":{"torrents":[
				{"name":"distro.iso","percentDone":0.38,"rateDownload":2100000,"status":4,"eta":700,"error":0},
				{"name":"outro","percentDone":1,"rateDownload":0,"status":6,"eta":-1,"error":0},
				{"name":"quebrado","percentDone":0.1,"rateDownload":0,"status":0,"eta":-1,"error":3}]}}`))
		}
	}))
	defer srv.Close()
	vals, err := transmission{}.Fetch(context.Background(), srv.Client(), Config{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	l := vals["downloading_list"].List
	if *vals["down"].Num != 2100000 || *vals["downloading"].Num != 1 || *vals["seeding"].Num != 1 || *vals["errors"].Num != 1 || len(l) != 1 || l[0].Text != "38% · 2,1 MB/s · falta 12 min" {
		t.Fatalf("valores: %v %v %v %+v", *vals["down"].Num, *vals["downloading"].Num, *vals["errors"].Num, l)
	}
	// a sessão é lembrada: a próxima busca não toma 409
	before := conflicts
	if _, err := (transmission{}).Fetch(context.Background(), srv.Client(), Config{URL: srv.URL + "/transmission/rpc", Prev: vals}); err != nil {
		t.Fatal(err)
	}
	if conflicts != before {
		t.Fatal("deveria reaproveitar o id de sessão")
	}
}

func TestSyncthing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/rest/system/status":
			_, _ = w.Write([]byte(`{"myID":"SELF","uptime":3600}`))
		case "/rest/system/version":
			_, _ = w.Write([]byte(`{"version":"v1.29.0"}`))
		case "/rest/config/devices":
			_, _ = w.Write([]byte(`[{"deviceID":"SELF","name":"servidor"},{"deviceID":"PHONEAAAA","name":"Celular"},{"deviceID":"PCBBBBBBB","name":""}]`))
		case "/rest/system/connections":
			_, _ = w.Write([]byte(`{"connections":{"PHONEAAAA":{"connected":true},"PCBBBBBBB":{"connected":false}}}`))
		case "/rest/config/folders":
			_, _ = w.Write([]byte(`[{"id":"fotos","label":"Fotos"},{"id":"docs","label":""},{"id":"velha","label":"Velha","paused":true}]`))
		case "/rest/db/status":
			if r.URL.Query().Get("folder") == "fotos" {
				_, _ = w.Write([]byte(`{"state":"idle","needBytes":0,"errors":0,"pullErrors":0}`))
			} else {
				_, _ = w.Write([]byte(`{"state":"syncing","needBytes":2500000000,"errors":0,"pullErrors":2}`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	vals, err := syncthing{}.Fetch(context.Background(), srv.Client(), Config{URL: srv.URL, Secret: "k"})
	if err != nil {
		t.Fatal(err)
	}
	f := vals["folders_list"].List
	if *vals["devices"].Num != 2 || *vals["devices_online"].Num != 1 || *vals["synced"].Num != 1 || *vals["errors"].Num != 2 || *vals["need"].Num != 2.5e9 {
		t.Fatalf("valores: %+v", vals)
	}
	if f[1].Label != "docs" || f[1].Text != "sincronizando · falta 2,5 GB · erro" || f[2].Text != "pausada" {
		t.Fatalf("pastas: %+v", f)
	}
	if d := vals["devices_list"].List; d[0].Text != "conectado" || d[1].Label != "PCBBBBB" {
		t.Fatalf("aparelhos: %+v", d)
	}
	if _, err := (syncthing{}).Fetch(context.Background(), srv.Client(), Config{URL: srv.URL, Secret: "errada"}); err == nil || !strings.Contains(err.Error(), "recusou") {
		t.Fatalf("chave errada: %v", err)
	}
}

func TestNextcloud(t *testing.T) {
	var lastAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ocs/v2.php/apps/serverinfo/api/v1/info" || r.Header.Get("OCS-APIRequest") != "true" || r.URL.Query().Get("skipApps") != "true" {
			w.WriteHeader(404)
			return
		}
		u, p, basic := r.BasicAuth()
		switch {
		case r.Header.Get("NC-Token") == "tok":
			lastAuth.Store("token")
		case basic && u == "admin" && p == "app-pass":
			lastAuth.Store("basic")
		default:
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"ocs":{"data":{"nextcloud":{"system":{"version":"30.0.2.1","freespace":150000000000},
			"storage":{"num_users":4,"num_files":"120345"},"shares":{"num_shares":17}},
			"server":{"database":{"size":"536870912"}},"activeUsers":{"last5minutes":1,"last1hour":2,"last24hours":3}}}}`))
	}))
	defer srv.Close()
	vals, err := nextcloud{}.Fetch(context.Background(), srv.Client(), Config{URL: srv.URL, Secret: "tok"})
	if err != nil || lastAuth.Load() != "token" {
		t.Fatalf("token: %v %v", err, lastAuth.Load())
	}
	if *vals["users"].Num != 4 || *vals["files"].Num != 120345 || *vals["free_gb"].Num != 150 || *vals["db_size"].Num != 536870912 || *vals["version"].Text != "30.0.2.1" {
		t.Fatalf("valores: %+v", vals)
	}
	if _, err := (nextcloud{}).Fetch(context.Background(), srv.Client(), Config{URL: srv.URL, User: "admin", Secret: "app-pass"}); err != nil || lastAuth.Load() != "basic" {
		t.Fatalf("usuário e senha de app: %v", err)
	}
}
