// Package dockerc é um cliente mínimo da API do Docker (só o que o BrunOS usa),
// falando HTTP direto com o socket ou com um socket-proxy — sem o SDK oficial,
// que traria dezenas de dependências.
package dockerc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const apiVersion = "v1.41" // suportada por Docker 20.10+ e com "one-shot" nas estatísticas

type Client struct {
	hc   *http.Client
	base string

	mu   sync.Mutex
	prev map[string]cpuSample // última amostra de CPU por container, para calcular o delta
}

type cpuSample struct {
	total, system uint64
	at            time.Time
}

// New aceita "unix:///var/run/docker.sock", "tcp://socket-proxy:2375" ou "http://…".
func New(host string) (*Client, error) {
	c := &Client{prev: map[string]cpuSample{}}
	switch {
	case strings.HasPrefix(host, "unix://"):
		sock := strings.TrimPrefix(host, "unix://")
		tr := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
			MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second,
		}
		c.hc = &http.Client{Transport: tr, Timeout: 15 * time.Second}
		c.base = "http://docker"
	case strings.HasPrefix(host, "tcp://"), strings.HasPrefix(host, "http://"):
		u, err := url.Parse(strings.Replace(host, "tcp://", "http://", 1))
		if err != nil {
			return nil, err
		}
		c.hc = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}}
		c.base = strings.TrimRight(u.String(), "/")
	default:
		return nil, fmt.Errorf("DOCKER_HOST não suportado: %q", host)
	}
	return c, nil
}

type Container struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
	Created int64             `json:"Created"`
}

func (c Container) Name() string {
	if len(c.Names) == 0 {
		return c.ID[:12]
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

func (c Container) Project() string { return c.Labels["com.docker.compose.project"] }
func (c Container) Service() string { return c.Labels["com.docker.compose.service"] }

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/"+apiVersion+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
}

func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	var out []Container
	err := c.do(ctx, http.MethodGet, "/containers/json?all=1", &out)
	return out, err
}

type Stats struct {
	CPU    float64 `json:"cpu"`    // % de um núcleo × núcleos (igual ao docker stats)
	Memory uint64  `json:"memory"` // bytes, sem cache de arquivos
}

type rawStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}

// Stats pede uma amostra instantânea (one-shot) e calcula a CPU pelo delta com a anterior.
func (c *Client) Stats(ctx context.Context, id string) (Stats, error) {
	var r rawStats
	if err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/stats?stream=false&one-shot=true", &r); err != nil {
		return Stats{}, err
	}
	mem := r.MemoryStats.Usage
	if v, ok := r.MemoryStats.Stats["inactive_file"]; ok && v < mem { // cgroup v2
		mem -= v
	} else if v, ok := r.MemoryStats.Stats["total_inactive_file"]; ok && v < mem { // cgroup v1
		mem -= v
	}
	cur := cpuSample{total: r.CPUStats.CPUUsage.TotalUsage, system: r.CPUStats.SystemUsage, at: time.Now()}
	c.mu.Lock()
	prev, ok := c.prev[id]
	c.prev[id] = cur
	c.mu.Unlock()
	st := Stats{Memory: mem}
	if ok && cur.system > prev.system && cur.total >= prev.total {
		cpus := r.CPUStats.OnlineCPUs
		if cpus == 0 {
			cpus = 1
		}
		st.CPU = float64(cur.total-prev.total) / float64(cur.system-prev.system) * float64(cpus) * 100
	}
	return st, nil
}

func (c *Client) Restart(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/restart?t=10", nil)
}

func (c *Client) Ping(ctx context.Context) error {
	var v struct {
		Version string `json:"Version"`
	}
	if err := c.do(ctx, http.MethodGet, "/version", &v); err != nil {
		return err
	}
	if v.Version == "" {
		return errors.New("resposta inesperada do Docker")
	}
	return nil
}

// Forget limpa amostras de containers que não existem mais.
func (c *Client) Forget(alive map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.prev {
		if !alive[id] {
			delete(c.prev, id)
		}
	}
}
