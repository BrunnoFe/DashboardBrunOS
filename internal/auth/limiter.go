package auth

import (
	"sync"
	"time"
)

// limiter bloqueia um IP depois de várias senhas erradas, com espera que dobra
// a cada novo bloqueio (1 min, 2 min, 4 min… até 30 min). Fica só em memória.
type limiter struct {
	mu sync.Mutex
	m  map[string]*attempt
}

type attempt struct {
	fails   int
	strikes int
	until   time.Time
	last    time.Time
}

const (
	maxFails   = 5
	baseLock   = time.Minute
	maxLock    = 30 * time.Minute
	forgetTime = 24 * time.Hour
)

func newLimiter() *limiter { return &limiter{m: map[string]*attempt{}} }

func (l *limiter) blocked(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.m[ip]
	if !ok {
		return 0
	}
	if d := time.Until(a.until); d > 0 {
		return d
	}
	return 0
}

func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a, ok := l.m[ip]
	if !ok {
		a = &attempt{}
		l.m[ip] = a
	}
	a.fails++
	a.last = now
	if a.fails >= maxFails {
		lock := baseLock << a.strikes
		if lock > maxLock || lock <= 0 {
			lock = maxLock
		}
		a.until = now.Add(lock)
		a.fails = 0
		a.strikes++
	}
	if len(l.m) > 1000 { // limpeza ocasional
		for k, v := range l.m {
			if now.Sub(v.last) > forgetTime {
				delete(l.m, k)
			}
		}
	}
}

func (l *limiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, ip)
}
