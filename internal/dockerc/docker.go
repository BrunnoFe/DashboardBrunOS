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
	Mounts  []Mount           `json:"Mounts"` // pastas montadas (a janela Discos mostra quais apps usam cada disco)
}

// Mount é uma pasta montada num container (Source = caminho no servidor).
type Mount struct {
	Type        string `json:"Type"` // bind, volume
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
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

// Start e Stop ligam/param um container (o Docker responde 304 se já estava assim: não é erro).
func (c *Client) Start(ctx context.Context, id string) error {
	return ignore304(c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/start", nil))
}

func (c *Client) Stop(ctx context.Context, id string) error {
	return ignore304(c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/stop?t=20", nil))
}

func ignore304(err error) error {
	if err != nil && strings.Contains(err.Error(), ": 304 ") {
		return nil
	}
	return err
}

// Logs abre os logs do container (stdout e stderr). tail = últimas N linhas; follow = continua
// mandando as novas. Quem chama precisa fechar. O conteúdo vem no formato multiplexado do Docker
// quando o container não tem TTY: use Demux.
func (c *Client) Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, bool, error) {
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "tail": {fmt.Sprint(tail)}, "timestamps": {"1"}}
	if follow {
		q.Set("follow", "1")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/"+apiVersion+"/containers/"+url.PathEscape(id)+"/logs?"+q.Encode(), nil)
	if err != nil {
		return nil, false, err
	}
	hc := c.hc
	if follow { // o stream fica aberto: sem o timeout geral do cliente
		cp := *c.hc
		cp.Timeout = 0
		hc = &cp
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, false, fmt.Errorf("docker logs: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	// Sem TTY vêm quadros com cabeçalho de 8 bytes; com TTY, texto puro. Até a API 1.41 o Docker manda
	// "raw-stream" nos dois casos, então não dá para confiar no Content-Type: Demux reconhece sozinho
	// quando não há cabeçalho e copia o texto como está.
	return resp.Body, true, nil
}

// Demux lê o formato multiplexado (cabeçalho [tipo,0,0,0,tamanho uint32 big-endian] + dados) e
// escreve só os dados em w.
func Demux(r io.Reader, w io.Writer) error {
	var hdr [8]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		n := int64(hdr[4])<<24 | int64(hdr[5])<<16 | int64(hdr[6])<<8 | int64(hdr[7])
		if hdr[0] > 2 || n > 16<<20 { // não é multiplexado de verdade: copia o resto como está
			if _, err := w.Write(hdr[:]); err != nil {
				return err
			}
			_, err := io.Copy(w, r)
			return err
		}
		if _, err := io.CopyN(w, r, n); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
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
