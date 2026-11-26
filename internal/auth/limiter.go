package auth

import (
	"sync"
	"time"
)

// limiter bloqueia um IP depois de várias senhas erradas, com espera que dobra
// a cada novo bloqueio (1 min, 2 min, 4 min… até 30 min). Fica só em memória.
//
// A tentativa é reservada ANTES de conferir a senha (reserve): pedidos em paralelo contam como
// tentativas em andamento, então não dá para mandar 20 de uma vez e conferir as 20 (antes o erro só
// era contado depois do hash, ~0,3 s mais tarde).
type limiter struct {
	mu sync.Mutex
	m  map[string]*attempt
}

type attempt struct {
	fails    int
	strikes  int
	inflight int // conferências em andamento
	until    time.Time
	last     time.Time
}

const (
	maxFails   = 5
	baseLock   = time.Minute
	maxLock    = 30 * time.Minute
	forgetTime = 24 * time.Hour
	maxEntries = 10000 // teto do mapa: IPs inventados não fazem a memória crescer sem fim
)

func newLimiter() *limiter { return &limiter{m: map[string]*attempt{}} }

// reserve marca uma conferência de senha em andamento. Devolve false (não confira) se o IP está
// bloqueado ou se os erros mais as conferências em andamento já chegam ao limite.
// Toda reserva aceita precisa terminar em fail ou success.
func (l *limiter) reserve(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a, ok := l.m[ip]
	if !ok {
		l.pruneLocked(now)
		a = &attempt{}
		l.m[ip] = a
	}
	if now.Before(a.until) || a.fails+a.inflight >= maxFails {
		return false
	}
	a.inflight++
	a.last = now
	return true
}

// fail conta uma senha errada. Devolve o tempo de bloqueio se este erro bloqueou o IP (senão 0).
func (l *limiter) fail(ip string) (lock time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a, ok := l.m[ip]
	if !ok {
		l.pruneLocked(now)
		a = &attempt{}
		l.m[ip] = a
	}
	if a.inflight > 0 {
		a.inflight--
	}
	a.fails++
	a.last = now
	if a.fails >= maxFails {
		lock = baseLock << a.strikes
		if lock > maxLock || lock <= 0 {
			lock = maxLock
		}
		a.until = now.Add(lock)
		a.fails = 0
		a.strikes++
	}
	return lock
}

func (l *limiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[ip]
	if !ok {
		return
	}
	if a.inflight > 1 { // outras conferências do mesmo IP ainda em andamento
		a.inflight--
		a.fails, a.strikes, a.until = 0, 0, time.Time{}
		return
	}
	delete(l.m, ip)
}

// pruneLocked limpa entradas velhas quando o mapa passa do teto; se ainda estiver cheio, tira as que
// não estão bloqueadas nem em andamento. Chamado com l.mu travado, só ao criar uma entrada nova.
func (l *limiter) pruneLocked(now time.Time) {
	if len(l.m) < maxEntries {
		return
	}
	for k, v := range l.m {
		if now.Sub(v.last) > forgetTime {
			delete(l.m, k)
		}
	}
	for k, v := range l.m {
		if len(l.m) < maxEntries {
			return
		}
		if v.inflight == 0 && !now.Before(v.until) {
			delete(l.m, k)
		}
	}
}
