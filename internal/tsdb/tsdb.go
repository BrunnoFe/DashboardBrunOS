// Package tsdb guarda os Históricos do BrunOS (0.14.0, decisão 53): séries de números no tempo, em três
// camadas de tamanho fixo por série:
//
//	fina:   1 ponto por minuto (média)          × 2880 = 48 h
//	média:  1 ponto a cada 5 min (média + pico)  × 2016 = 7 dias
//	grossa: 1 ponto a cada 20 min (média + pico) × 2160 = 30 dias
//
// Add recebe amostras soltas (a cada 2 s, 15 s ou 1 min) e faz as médias. Cerca de 45 KB por série.
//
// No disco (/data/tsdb/): uma foto completa (snapshot.bin) a cada 6 h e ao desligar, e um diário
// (journal.log) com só os pontos novos, acrescentado a cada 5 min. Ao abrir, lê a foto e reaplica o
// diário: um reinício brusco perde no máximo 5 min, e o SSD não é reescrito à toa (alguns KB a cada 5 min
// em vez de MB). Série sem dado novo há 30 dias é esquecida.
package tsdb

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"brunos/internal/store"
)

// Camadas.
const (
	FineStep, FineSlots     = 60, 2880
	MidStep, MidSlots       = 300, 2016
	CoarseStep, CoarseSlots = 1200, 2160

	MaxSeries    = 400             // proteção de memória (~18 MB no máximo)
	forgetAfter  = 30 * 24 * 3600  // série sem dado novo há 30 dias é esquecida
	journalEvery = 5 * time.Minute // diário
	snapshotEvry = 6 * time.Hour   // foto completa
	fileMagic    = "BTSD"
	fileVersion  = 1
)

var nan32 = float32(math.NaN())

// Point é um ponto devolvido nas consultas. Max só nas camadas média e grossa.
type Point struct {
	T   int64
	V   float32
	Max float32
}

type acc struct {
	start    int64
	n        int
	sum, max float64
}

func (a *acc) add(v, mx float64) {
	if a.n == 0 || mx > a.max {
		a.max = mx
	}
	a.sum += v
	a.n++
}
func (a *acc) avg() float64 { return a.sum / float64(a.n) }

type ring struct {
	step int64
	last int64 // época (t/step) do último ponto gravado; 0 = vazio
	avg  []float32
	max  []float32 // nil na camada fina
}

func newRing(step int64, slots int, withMax bool) ring {
	r := ring{step: step, avg: make([]float32, slots)}
	if withMax {
		r.max = make([]float32, slots)
	}
	for i := range r.avg {
		r.avg[i] = nan32
	}
	for i := range r.max {
		r.max[i] = nan32
	}
	return r
}

// put grava o ponto da época e (apagando o que ficou para trás sem dado).
func (r *ring) put(e int64, v, mx float32) {
	n := int64(len(r.avg))
	if r.last != 0 && e <= r.last-n {
		return // mais velho que o anel
	}
	if e > r.last {
		start := r.last + 1
		if r.last == 0 || e-r.last > n {
			start = e - n + 1
		}
		for k := start; k < e; k++ {
			i := k % n
			r.avg[i] = nan32
			if r.max != nil {
				r.max[i] = nan32
			}
		}
		r.last = e
	}
	i := e % n
	r.avg[i] = v
	if r.max != nil {
		r.max[i] = mx
	}
}

// since devolve os pontos válidos com época > from (até a última).
func (r *ring) since(from int64) []Point {
	n := int64(len(r.avg))
	if r.last == 0 {
		return nil
	}
	if from < r.last-n {
		from = r.last - n
	}
	out := make([]Point, 0, r.last-from)
	for e := from + 1; e <= r.last; e++ {
		i := e % n
		v := r.avg[i]
		if v != v { // NaN
			continue
		}
		p := Point{T: e * r.step, V: v, Max: v}
		if r.max != nil {
			p.Max = r.max[i]
		}
		out = append(out, p)
	}
	return out
}

type serie struct {
	lastSeen          int64
	cur, cur5, cur20  acc
	fine, mid, coarse ring
}

func newSerie() *serie {
	return &serie{fine: newRing(FineStep, FineSlots, false), mid: newRing(MidStep, MidSlots, true), coarse: newRing(CoarseStep, CoarseSlots, true)}
}

type rec struct {
	tier    byte
	name    string
	e       int64
	avg, mx float32
}

// DB é o armazenamento. Seguro para uso concorrente.
type DB struct {
	Dir string
	Now func() time.Time

	mu        sync.Mutex
	series    map[string]*serie
	pending   []rec // pontos ainda não escritos no diário
	journalAt time.Time
	snapAt    time.Time
	full      bool // já avisou que bateu no limite de séries
}

// Open abre (ou cria) o armazenamento em dir. dir vazio = só na memória (testes).
func Open(dir string) *DB {
	db := &DB{Dir: dir, Now: time.Now, series: map[string]*serie{}}
	db.journalAt, db.snapAt = db.Now(), db.Now()
	if dir == "" {
		return db
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		slog.Error("históricos: não foi possível criar a pasta", "erro", err)
	}
	if err := db.loadSnapshot(); err != nil {
		slog.Warn("históricos: foto ilegível, começando do zero", "erro", err)
		db.series = map[string]*serie{}
	}
	if err := db.replayJournal(); err != nil {
		slog.Warn("históricos: diário ilegível (o resto foi aproveitado)", "erro", err)
	}
	return db
}

// Clean deixa o nome seguro para o diário (sem espaços nem quebras de linha).
func Clean(name string) string {
	return strings.Map(func(r rune) rune {
		if r <= ' ' || r == 0x7f {
			return '_'
		}
		return r
	}, name)
}

func (db *DB) get(name string) *serie {
	s := db.series[name]
	if s == nil {
		if len(db.series) >= MaxSeries {
			if !db.full {
				slog.Warn("históricos: limite de séries atingido; séries novas não são guardadas", "limite", MaxSeries)
				db.full = true
			}
			return nil
		}
		s = newSerie()
		db.series[name] = s
	}
	return s
}

// Add soma uma amostra à série (média do minuto; o pico das camadas maiores vem das amostras).
func (db *DB) Add(name string, v float64) { db.AddAt(name, v, db.Now()) }

// AddAt é o Add com hora explícita (testes e importação).
func (db *DB) AddAt(name string, v float64, t time.Time) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	name = Clean(name)
	u := t.Unix()
	db.mu.Lock()
	defer db.mu.Unlock()
	s := db.get(name)
	if s == nil {
		return
	}
	m := u / FineStep
	if s.cur.n > 0 && s.cur.start != m {
		db.flushMinute(name, s)
	}
	if s.cur.n == 0 {
		s.cur.start = m
	}
	s.cur.add(v, v)
	s.lastSeen = u
}

// flushMinute fecha o minuto em andamento: grava na camada fina e soma nas outras duas.
func (db *DB) flushMinute(name string, s *serie) {
	if s.cur.n == 0 {
		return
	}
	m, avg, mx := s.cur.start, s.cur.avg(), s.cur.max
	s.cur = acc{}
	db.push(name, s, 0, m, float32(avg), float32(avg))
	t := m * FineStep
	for _, c := range []struct {
		a    *acc
		step int64
		tier byte
	}{{&s.cur5, MidStep, 1}, {&s.cur20, CoarseStep, 2}} {
		e := t / c.step
		if c.a.n > 0 && c.a.start != e {
			db.push(name, s, c.tier, c.a.start, float32(c.a.avg()), float32(c.a.max))
			*c.a = acc{}
		}
		if c.a.n == 0 {
			c.a.start = e
		}
		c.a.add(avg, mx)
	}
}

func (db *DB) push(name string, s *serie, tier byte, e int64, avg, mx float32) {
	switch tier {
	case 0:
		s.fine.put(e, avg, avg)
	case 1:
		s.mid.put(e, avg, mx)
	case 2:
		s.coarse.put(e, avg, mx)
	}
	if db.Dir != "" {
		db.pending = append(db.pending, rec{tier, name, e, avg, mx})
	}
}

// Import grava um ponto pronto direto numa camada (0 fina, 1 média, 2 grossa), sem passar pelas médias.
// Usado para trazer o histórico antigo (history.json); chame Save depois.
func (db *DB) Import(name string, tier byte, t int64, avg, mx float64) {
	name = Clean(name)
	db.mu.Lock()
	defer db.mu.Unlock()
	s := db.get(name)
	if s == nil {
		return
	}
	switch tier {
	case 0:
		s.fine.put(t/FineStep, float32(avg), float32(avg))
	case 1:
		s.mid.put(t/MidStep, float32(avg), float32(mx))
	case 2:
		s.coarse.put(t/CoarseStep, float32(avg), float32(mx))
	}
	s.lastSeen = max(s.lastSeen, t)
}

// closeDue fecha os períodos que já acabaram (sem amostra nova, eles ficariam abertos para sempre).
func (db *DB) closeDue(name string, s *serie, now int64) {
	if s.cur.n > 0 && s.cur.start < now/FineStep {
		db.flushMinute(name, s)
	}
	if s.cur5.n > 0 && s.cur5.start < now/MidStep && s.cur.n == 0 {
		db.push(name, s, 1, s.cur5.start, float32(s.cur5.avg()), float32(s.cur5.max))
		s.cur5 = acc{}
	}
	if s.cur20.n > 0 && s.cur20.start < now/CoarseStep && s.cur.n == 0 {
		db.push(name, s, 2, s.cur20.start, float32(s.cur20.avg()), float32(s.cur20.max))
		s.cur20 = acc{}
	}
}

// Periods aceitos nas consultas e a camada de cada um.
var Periods = map[string]struct {
	Step int64
	Span int64
}{
	"5m":  {FineStep, 5 * 60},
	"1h":  {FineStep, 3600},
	"24h": {FineStep, 24 * 3600},
	"48h": {FineStep, 48 * 3600},
	"7d":  {MidStep, 7 * 24 * 3600},
	"30d": {CoarseStep, 30 * 24 * 3600},
}

// Query devolve os pontos da série no período (o período em andamento entra como último ponto,
// parcial). Desconhecido → nil.
func (db *DB) Query(name, period string) (step int64, pts []Point) {
	p, ok := Periods[period]
	if !ok {
		return 0, nil
	}
	now := db.Now().Unix()
	db.mu.Lock()
	defer db.mu.Unlock()
	s := db.series[Clean(name)]
	if s == nil {
		return p.Step, []Point{}
	}
	db.closeDue(Clean(name), s, now)
	from := (now - p.Span) / p.Step
	var r *ring
	var cur *acc
	switch p.Step {
	case FineStep:
		r, cur = &s.fine, &s.cur
	case MidStep:
		r, cur = &s.mid, &s.cur5
	default:
		r, cur = &s.coarse, &s.cur20
	}
	pts = r.since(from)
	if pts == nil {
		pts = []Point{}
	}
	// período em andamento (minuto atual, ou os minutos já fechados dos 5/20 min atuais)
	if cur.n > 0 && cur.start > from && (len(pts) == 0 || cur.start*p.Step > pts[len(pts)-1].T) {
		pts = append(pts, Point{T: cur.start * p.Step, V: float32(cur.avg()), Max: float32(cur.max)})
	}
	return p.Step, pts
}

// Names devolve as séries com o prefixo dado, em ordem.
func (db *DB) Names(prefix string) []string {
	db.mu.Lock()
	defer db.mu.Unlock()
	out := []string{}
	for n := range db.series {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Tick grava o diário a cada 5 min e a foto a cada 6 h. Chamar de tempos em tempos (ex.: a cada minuto).
func (db *DB) Tick() {
	if db.Dir == "" {
		return
	}
	now := db.Now()
	db.mu.Lock()
	snap := now.Sub(db.snapAt) >= snapshotEvry
	journal := now.Sub(db.journalAt) >= journalEvery
	db.mu.Unlock()
	if snap {
		db.Save()
	} else if journal {
		db.flushJournal()
	}
}

// Save grava a foto completa e zera o diário (a cada 6 h e ao desligar).
func (db *DB) Save() {
	if db.Dir == "" {
		return
	}
	now := db.Now()
	db.mu.Lock()
	u := now.Unix()
	for name, s := range db.series {
		if s.lastSeen > 0 && u-s.lastSeen > forgetAfter {
			delete(db.series, name)
		}
	}
	data := db.encode()
	db.pending = nil
	db.snapAt, db.journalAt = now, now
	db.mu.Unlock()
	if err := store.WriteFile(filepath.Join(db.Dir, "snapshot.bin"), data, 0o600); err != nil {
		slog.Error("históricos: não foi possível gravar a foto", "erro", err)
		return
	}
	if err := os.Remove(filepath.Join(db.Dir, "journal.log")); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("históricos: não foi possível zerar o diário", "erro", err)
	}
}

func (db *DB) flushJournal() {
	db.mu.Lock()
	recs := db.pending
	db.pending = nil
	db.journalAt = db.Now()
	db.mu.Unlock()
	if len(recs) == 0 {
		return
	}
	var b bytes.Buffer
	for _, r := range recs {
		fmt.Fprintf(&b, "%d %s %d %s %s\n", r.tier, r.name, r.e, fmtF(r.avg), fmtF(r.mx))
	}
	f, err := os.OpenFile(filepath.Join(db.Dir, "journal.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.Write(b.Bytes())
		if err == nil {
			err = f.Sync()
		}
		f.Close()
	}
	if err != nil {
		slog.Error("históricos: não foi possível escrever o diário", "erro", err)
	}
}

func fmtF(v float32) string { return strconv.FormatFloat(float64(v), 'g', 6, 32) }

func (db *DB) replayJournal() error {
	f, err := os.Open(filepath.Join(db.Dir, "journal.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	bad := 0
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) != 5 {
			bad++
			continue
		}
		tier, e1 := strconv.Atoi(parts[0])
		e, e2 := strconv.ParseInt(parts[2], 10, 64)
		avg, e3 := strconv.ParseFloat(parts[3], 32)
		mx, e4 := strconv.ParseFloat(parts[4], 32)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || tier < 0 || tier > 2 {
			bad++
			continue
		}
		s := db.get(parts[1])
		if s == nil {
			continue
		}
		switch tier {
		case 0:
			s.fine.put(e, float32(avg), float32(avg))
			s.lastSeen = max(s.lastSeen, e*FineStep)
		case 1:
			s.mid.put(e, float32(avg), float32(mx))
		case 2:
			s.coarse.put(e, float32(avg), float32(mx))
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d linhas ignoradas", bad)
	}
	return sc.Err()
}

// ── Foto binária ──────────────────────────────────────────────────────────────────────────────────
// "BTSD" + versão (u16) + nº de séries (u32); por série: nome (u16 + bytes), lastSeen, e as três
// camadas (last i64 + float32 little-endian; média e pico nas duas maiores).

func (db *DB) encode() []byte {
	var b bytes.Buffer
	b.WriteString(fileMagic)
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	w(uint16(fileVersion))
	names := make([]string, 0, len(db.series))
	for n := range db.series {
		names = append(names, n)
	}
	sort.Strings(names)
	w(uint32(len(names)))
	for _, n := range names {
		s := db.series[n]
		w(uint16(len(n)))
		b.WriteString(n)
		w(s.lastSeen)
		for _, r := range []*ring{&s.fine, &s.mid, &s.coarse} {
			w(r.last)
			w(r.avg)
			if r.max != nil {
				w(r.max)
			}
		}
	}
	return b.Bytes()
}

func (db *DB) loadSnapshot() error {
	data, err := os.ReadFile(filepath.Join(db.Dir, "snapshot.bin"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	rd := bytes.NewReader(data)
	magic := make([]byte, 4)
	if _, err := io.ReadFull(rd, magic); err != nil || string(magic) != fileMagic {
		return errors.New("formato desconhecido")
	}
	r := func(v any) error { return binary.Read(rd, binary.LittleEndian, v) }
	var ver uint16
	var count uint32
	if err := r(&ver); err != nil || ver != fileVersion {
		return fmt.Errorf("versão %d desconhecida", ver)
	}
	if err := r(&count); err != nil || count > MaxSeries {
		return errors.New("cabeçalho inválido")
	}
	out := map[string]*serie{}
	for i := uint32(0); i < count; i++ {
		var nl uint16
		if err := r(&nl); err != nil {
			return err
		}
		name := make([]byte, nl)
		if _, err := io.ReadFull(rd, name); err != nil {
			return err
		}
		s := newSerie()
		if err := r(&s.lastSeen); err != nil {
			return err
		}
		for _, rg := range []*ring{&s.fine, &s.mid, &s.coarse} {
			if err := r(&rg.last); err != nil {
				return err
			}
			if err := r(rg.avg); err != nil {
				return err
			}
			if rg.max != nil {
				if err := r(rg.max); err != nil {
					return err
				}
			}
		}
		out[string(name)] = s
	}
	db.series = out
	return nil
}
