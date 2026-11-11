package integrations

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Tomada Tuya pelo protocolo local (sem nuvem), versões 3.3, 3.4 e 3.5, só com a biblioteca padrão.
// Formato conferido com o tinytuya 1.20 (que serviu de referência e gerou os testes):
//   - 3.3: quadro 0x55AA, conteúdo em AES-128-ECB com a chave local, CRC32.
//   - 3.4: negocia uma chave de sessão (nonces + HMAC-SHA256); quadro 0x55AA com HMAC; AES-ECB.
//   - 3.5: mesma negociação; quadro 0x6699 com AES-GCM (IV de 12 bytes, tag de 16, cabeçalho como AAD).
//
// A chave local (16 caracteres) é tirada uma vez com o tinytuya e fica cifrada como as outras
// credenciais. Os números (DPS) de tomadas com medidor seguem o padrão Tuya: 19 = potência (W×10),
// 20 = tensão (V×10), 18 = corrente (mA), 17 = energia somada (kWh×100), 1 = ligada.
type tuyaPlug struct{}

func init() { register(tuyaPlug{}) }

func (tuyaPlug) Info() KindInfo {
	return KindInfo{
		ID: "tuya", Name: "Tomada Tuya", Desc: "Consumo medido na tomada (W, V, A e kWh), pela rede de casa, sem nuvem",
		NoURL: true, Interval: 30,
		Note: "Precisa do IP, do ID e da chave local da tomada (tirados uma vez com o tinytuya). A chave fica guardada cifrada no BrunOS.",
		Fields: []Field{
			{ID: "host", Label: "IP da tomada", Type: "text", Placeholder: "192.168.1.60", Required: true},
			{ID: "devid", Label: "ID da tomada (Device ID)", Type: "text", Placeholder: "bf12...", Required: true},
			{ID: "secret", Label: "Chave local (local key)", Type: "secret", Required: true,
				Help: "No arquivo devices.json do tinytuya, campo \"key\" (16 caracteres)"},
			{ID: "version", Label: "Versão do protocolo", Type: "text", Placeholder: "3.5",
				Help: "Aparece no \"tinytuya scan\" (3.3, 3.4 ou 3.5). Vazio = 3.5"},
		},
		Metrics: []MetricDef{
			{ID: "power", Name: "Potência agora", Kind: Num, Unit: "W", Icon: "bolt"},
			{ID: "voltage", Name: "Tensão", Kind: Num, Unit: "V", Icon: "bolt"},
			{ID: "current", Name: "Corrente", Kind: Num, Unit: "A", Icon: "bolt"},
			{ID: "energy", Name: "Energia somada pela tomada", Kind: Num, Unit: "kWh", Icon: "chart"},
			{ID: "on", Name: "Ligada (1 = sim)", Kind: Num, Icon: "power"},
		},
		Alerts: []AlertDef{
			{ID: "down", Name: "Tomada sem responder", Desc: "Avisa quando a tomada para de responder por 5 minutos", Op: "down", Level: "info", Fixed: true},
		},
	}
}

const (
	tuyaPort      = "6668"
	tuyaTimeout   = 6 * time.Second
	tuyaQuery33   = 0x0a // DP_QUERY
	tuyaQueryNew  = 0x10 // DP_QUERY_NEW (3.4 e 3.5)
	tuyaNegStart  = 0x03
	tuyaNegResp   = 0x04
	tuyaNegFinish = 0x05
)

var (
	tuyaPrefix55 = []byte{0, 0, 0x55, 0xaa}
	tuyaSuffix55 = []byte{0, 0, 0xaa, 0x55}
	tuyaPrefix66 = []byte{0, 0, 0x66, 0x99}
	tuyaSuffix66 = []byte{0, 0, 0x99, 0x66}
)

func (tuyaPlug) Fetch(ctx context.Context, _ *http.Client, cfg Config) (map[string]Value, error) {
	host := strings.TrimSpace(cfg.Options["host"])
	dev := strings.TrimSpace(cfg.Options["devid"])
	if host == "" || dev == "" || len(cfg.Secret) != 16 {
		return nil, ferr("preencha o IP, o ID e a chave local (16 caracteres) da tomada")
	}
	dps, err := TuyaStatus(ctx, host, dev, cfg.Secret, cfg.Options["version"])
	if err != nil {
		return nil, err
	}
	out := map[string]Value{}
	num := func(k string) (float64, bool) {
		switch v := dps[k].(type) {
		case float64:
			return v, true
		case json.Number:
			f, err := v.Float64()
			return f, err == nil
		}
		return 0, false
	}
	if v, ok := num("19"); ok {
		out["power"] = N(round1(v / 10))
	}
	if v, ok := num("20"); ok {
		out["voltage"] = N(round1(v / 10))
	}
	if v, ok := num("18"); ok {
		out["current"] = N(math.Round(v) / 1000)
	}
	if v, ok := num("17"); ok {
		out["energy"] = N(v / 100)
	}
	if b, ok := dps["1"].(bool); ok {
		if b {
			out["on"] = N(1)
		} else {
			out["on"] = N(0)
		}
	}
	if _, ok := out["power"]; !ok {
		return nil, ferr("a tomada respondeu, mas sem medidor de consumo (não mandou o número 19)")
	}
	return out, nil
}

// PlugPower lê a potência (W) agora de uma integração Tomada Tuya (para o Medidor do consumo).
func (h *Hub) PlugPower(ctx context.Context, id string) (float64, error) {
	it, ok := h.Store.Get(id)
	if !ok || it.Kind != "tuya" {
		return 0, ferr("a tomada escolhida não existe mais (Configurações → Energia)")
	}
	cfg, err := h.Store.Config(it)
	if err != nil {
		return 0, err
	}
	vals, err := tuyaPlug{}.Fetch(ctx, nil, cfg)
	if err != nil {
		return 0, err
	}
	return *vals["power"].Num, nil
}

// PlugIDs lista as integrações Tomada Tuya cadastradas.
func (h *Hub) PlugIDs() []string {
	var out []string
	for _, it := range h.Store.List() {
		if it.Kind == "tuya" {
			out = append(out, it.ID)
		}
	}
	return out
}

// TuyaStatus pede o estado (dps) da tomada. version: "3.3", "3.4" ou "3.5" (vazio = 3.5).
func TuyaStatus(ctx context.Context, host, devID, localKey, version string) (map[string]any, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "3.5"
	}
	if version != "3.3" && version != "3.4" && version != "3.5" {
		return nil, ferr("versão do protocolo inválida (use 3.3, 3.4 ou 3.5)")
	}
	key := []byte(localKey)
	if len(key) != 16 {
		return nil, ferr("a chave local precisa ter 16 caracteres")
	}
	addr := host
	if _, _, err := net.SplitHostPort(host); err != nil {
		addr = net.JoinHostPort(host, tuyaPort) // sem porta: a padrão da Tuya
	}
	d := net.Dialer{Timeout: tuyaTimeout}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, ferr("não consegui falar com a tomada em %s (confira o IP e se ela está na mesma rede)", host)
	}
	defer nc.Close()
	dl := time.Now().Add(tuyaTimeout)
	if t, ok := ctx.Deadline(); ok && t.Before(dl) {
		dl = t
	}
	_ = nc.SetDeadline(dl)
	c := &tuyaConn{rw: nc, ver: version, key: key, real: key, seq: 1}
	if version != "3.3" {
		if err := c.negotiate(); err != nil {
			return nil, err
		}
	}
	var payload []byte
	cmd := uint32(tuyaQueryNew)
	if version == "3.3" {
		cmd = tuyaQuery33
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		payload, _ = json.Marshal(map[string]string{"gwId": devID, "devId": devID, "uid": devID, "t": ts})
	} else {
		payload = []byte("{}")
	}
	if err := c.send(cmd, payload); err != nil {
		return nil, err
	}
	// a resposta pode vir depois de outros quadros (ex.: um "heartbeat"); procura a do pedido
	for range 4 {
		rcmd, body, err := c.recv()
		if err != nil {
			return nil, err
		}
		if rcmd != cmd && rcmd != 0x08 { // 0x08 = STATUS (alguns aparelhos respondem assim)
			continue
		}
		dps, err := tuyaDPS(body, version, key)
		if err != nil {
			return nil, err
		}
		if dps != nil {
			return dps, nil
		}
	}
	return nil, ferr("a tomada não mandou o estado")
}

type tuyaConn struct {
	rw   io.ReadWriter
	ver  string
	key  []byte // chave em uso (local, depois a de sessão)
	real []byte // chave local
	seq  uint32
}

func ecbEncrypt(key, plain []byte) []byte {
	b, _ := aes.NewCipher(key)
	pad := 16 - len(plain)%16
	p := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(p))
	for i := 0; i < len(p); i += 16 {
		b.Encrypt(out[i:i+16], p[i:i+16])
	}
	return out
}

func ecbDecrypt(key, enc []byte) ([]byte, error) {
	if len(enc) == 0 || len(enc)%16 != 0 {
		return nil, errors.New("tamanho inválido")
	}
	b, _ := aes.NewCipher(key)
	out := make([]byte, len(enc))
	for i := 0; i < len(enc); i += 16 {
		b.Decrypt(out[i:i+16], enc[i:i+16])
	}
	pad := int(out[len(out)-1])
	if pad < 1 || pad > 16 || pad > len(out) {
		return nil, errors.New("preenchimento inválido")
	}
	return out[:len(out)-pad], nil
}

func hmac256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

// send monta e envia um quadro. Pedidos de status e negociação não levam o cabeçalho "3.x".
func (c *tuyaConn) send(cmd uint32, payload []byte) error {
	var frame []byte
	switch c.ver {
	case "3.5":
		iv := make([]byte, 12)
		if _, err := rand.Read(iv); err != nil {
			return err
		}
		hdr := make([]byte, 18)
		copy(hdr, tuyaPrefix66)
		binary.BigEndian.PutUint32(hdr[6:], c.seq)
		binary.BigEndian.PutUint32(hdr[10:], cmd)
		binary.BigEndian.PutUint32(hdr[14:], uint32(12+len(payload)+16))
		b, _ := aes.NewCipher(c.key)
		g, _ := cipher.NewGCM(b)
		sealed := g.Seal(nil, iv, payload, hdr[4:])
		frame = append(append(append(hdr, iv...), sealed...), tuyaSuffix66...)
	default:
		var body []byte
		if c.ver == "3.4" || c.ver == "3.3" {
			body = ecbEncrypt(c.key, payload)
		}
		end := 8 // CRC32 + sufixo
		if c.ver == "3.4" {
			end = 36 // HMAC + sufixo
		}
		hdr := make([]byte, 16)
		copy(hdr, tuyaPrefix55)
		binary.BigEndian.PutUint32(hdr[4:], c.seq)
		binary.BigEndian.PutUint32(hdr[8:], cmd)
		binary.BigEndian.PutUint32(hdr[12:], uint32(len(body)+end))
		frame = append(hdr, body...)
		if c.ver == "3.4" {
			frame = append(frame, hmac256(c.key, frame)...)
		} else {
			crc := make([]byte, 4)
			binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(frame))
			frame = append(frame, crc...)
		}
		frame = append(frame, tuyaSuffix55...)
	}
	c.seq++
	_, err := c.rw.Write(frame)
	if err != nil {
		return ferr("a tomada fechou a conexão")
	}
	return nil
}

func readFull(r io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

// recv lê um quadro e devolve o comando e o conteúdo já decifrado (sem o código de retorno).
func (c *tuyaConn) recv() (uint32, []byte, error) {
	timeout := func(err error) error {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return ferr("a tomada não respondeu (confira a versão do protocolo e a chave local)")
		}
		return ferr("a tomada fechou a conexão (confira a chave local e a versão do protocolo)")
	}
	pre, err := readFull(c.rw, 4)
	if err != nil {
		return 0, nil, timeout(err)
	}
	switch {
	case bytes.Equal(pre, tuyaPrefix66):
		h, err := readFull(c.rw, 14)
		if err != nil {
			return 0, nil, timeout(err)
		}
		cmd := binary.BigEndian.Uint32(h[6:])
		n := binary.BigEndian.Uint32(h[10:])
		if n < 28 || n > 64<<10 {
			return 0, nil, ferr("resposta da tomada com tamanho inválido")
		}
		rest, err := readFull(c.rw, int(n)+4)
		if err != nil {
			return 0, nil, timeout(err)
		}
		iv, sealed := rest[:12], rest[12:n]
		b, _ := aes.NewCipher(c.key)
		g, _ := cipher.NewGCM(b)
		plain, err := g.Open(nil, iv, sealed, h) // AAD = cabeçalho sem o prefixo
		if err != nil {
			return 0, nil, ferr("não consegui decifrar a resposta da tomada (chave local errada?)")
		}
		if len(plain) >= 4 && plain[0] != '{' { // código de retorno
			plain = plain[4:]
		}
		return cmd, plain, nil
	case bytes.Equal(pre, tuyaPrefix55):
		h, err := readFull(c.rw, 12)
		if err != nil {
			return 0, nil, timeout(err)
		}
		cmd := binary.BigEndian.Uint32(h[4:])
		n := binary.BigEndian.Uint32(h[8:])
		end := 8
		if c.ver == "3.4" {
			end = 36
		}
		if int(n) < end || n > 64<<10 {
			return 0, nil, ferr("resposta da tomada com tamanho inválido")
		}
		rest, err := readFull(c.rw, int(n))
		if err != nil {
			return 0, nil, timeout(err)
		}
		frame := append(append(append([]byte{}, pre...), h...), rest...)
		body := rest[:int(n)-end]
		if c.ver == "3.4" {
			if !hmac.Equal(hmac256(c.key, frame[:len(frame)-end]), rest[int(n)-end:int(n)-4]) {
				return 0, nil, ferr("a resposta da tomada não confere (chave local errada?)")
			}
		}
		if len(body) >= 4 { // código de retorno (fora da cifra)
			body = body[4:]
		}
		if len(body) == 0 {
			return cmd, nil, nil
		}
		if c.ver == "3.4" {
			plain, err := ecbDecrypt(c.key, body)
			if err != nil {
				return 0, nil, ferr("não consegui decifrar a resposta da tomada (chave local errada?)")
			}
			return cmd, plain, nil
		}
		return cmd, body, nil // 3.3: decifrado em tuyaDPS (pode ter o cabeçalho "3.3" na frente)
	}
	return 0, nil, ferr("resposta da tomada em formato desconhecido (confira a versão do protocolo)")
}

// negotiate faz a troca de chave de sessão da 3.4 e da 3.5.
func (c *tuyaConn) negotiate() error {
	local := make([]byte, 16)
	if _, err := rand.Read(local); err != nil {
		return err
	}
	if err := c.send(tuyaNegStart, local); err != nil {
		return err
	}
	cmd, body, err := c.recv()
	if err != nil {
		return err
	}
	if cmd != tuyaNegResp || len(body) < 48 {
		return ferr("a tomada recusou a conexão (confira a chave local e a versão do protocolo)")
	}
	remote := body[:16]
	if !hmac.Equal(body[16:48], hmac256(c.real, local)) {
		return ferr("a chave local não confere com a da tomada")
	}
	if err := c.send(tuyaNegFinish, hmac256(c.real, remote)); err != nil {
		return err
	}
	x := make([]byte, 16)
	for i := range x {
		x[i] = local[i] ^ remote[i]
	}
	b, _ := aes.NewCipher(c.real)
	if c.ver == "3.4" {
		k := make([]byte, 16)
		b.Encrypt(k, x)
		c.key = k
	} else {
		g, _ := cipher.NewGCM(b)
		c.key = g.Seal(nil, local[:12], x, nil)[:16]
	}
	return nil
}

// tuyaDPS tira o mapa "dps" do conteúdo (3.3: decifra; tira o cabeçalho "3.x" se houver).
func tuyaDPS(body []byte, version string, key []byte) (map[string]any, error) {
	if len(body) == 0 {
		return nil, nil
	}
	if len(body) >= 15 && body[0] == '3' && body[1] == '.' {
		body = body[15:]
	}
	if version == "3.3" && body[0] != '{' {
		plain, err := ecbDecrypt(key, body)
		if err != nil {
			return nil, ferr("não consegui decifrar a resposta da tomada (chave local errada?)")
		}
		body = plain
	}
	return parseDPS(body)
}

func parseDPS(body []byte) (map[string]any, error) {
	var m struct {
		DPS  map[string]any `json:"dps"`
		Data struct {
			DPS map[string]any `json:"dps"`
		} `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimRight(body, "\x00")))
	if err := dec.Decode(&m); err != nil {
		if bytes.Contains(body, []byte("data unvalid")) {
			return nil, ferr("a tomada recusou o pedido (\"data unvalid\"): confira o ID da tomada")
		}
		return nil, ferr("resposta da tomada em formato inesperado")
	}
	if m.DPS == nil {
		m.DPS = m.Data.DPS
	}
	return m.DPS, nil
}
