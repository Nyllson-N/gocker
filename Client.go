// Package godocker é uma biblioteca Go para comunicar com o Docker Engine API v1.54.
//
// Sem dependências externas — usa apenas a biblioteca padrão do Go.
//
// # Uso mínimo
//
//	import docker "github.com/Nyllson-N/godocker"
//
//	// Conexão local (detecta automaticamente Unix socket ou TCP)
//	c := docker.New()
//
//	// Conexão com host remoto via IP público
//	c := docker.NewHost("203.0.113.10")
//
//	// Conexão com máquina na rede local
//	c := docker.NewLAN("192.168.1.50")
package gocker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// apiVersion é a versão fixa da Docker Engine API usada por esta biblioteca.
const apiVersion = "v1.54"

// dockerTCPPort é a porta padrão da Docker Engine API.
const dockerTCPPort = "2375"

// Client representa uma conexão com o daemon Docker.
type Client struct {
	baseURL string
	http    *http.Client
}

// ── Construtores públicos ─────────────────────────────────────────────────────

// New cria um Client com detecção automática de ambiente local.
//
// Ordem de prioridade:
//  1. DOCKER_HOST (ex: "tcp://192.168.1.10:2375" ou "unix:///run/docker.sock")
//  2. Windows nativo → TCP localhost:2375
//  3. WSL2 com Docker Desktop → TCP localhost:2375
//  4. Linux/macOS → Unix socket /var/run/docker.sock
//  5. Docker rootless → socket em $XDG_RUNTIME_DIR/docker.sock
//  6. Podman compatível → socket em $XDG_RUNTIME_DIR/podman/podman.sock
//  7. Fallback → TCP localhost:2375
func New() *Client {
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		if path, ok := strings.CutPrefix(host, "unix://"); ok {
			return buildSocket(path)
		}
		host = strings.TrimPrefix(host, "tcp://")
		host = strings.TrimPrefix(host, "http://")
		if !strings.Contains(host, ":") {
			host += ":" + dockerTCPPort
		}
		return buildTCP("http://" + host)
	}

	if runtime.GOOS == "windows" {
		return buildTCP("http://localhost:" + dockerTCPPort)
	}

	if isWSL() && canReachTCP("localhost:"+dockerTCPPort) {
		return buildTCP("http://localhost:" + dockerTCPPort)
	}

	const sockPath = "/var/run/docker.sock"
	if _, err := os.Stat(sockPath); err == nil {
		return buildSocket(sockPath)
	}

	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		for _, name := range []string{"docker.sock", "podman/podman.sock"} {
			p := xdg + "/" + name
			if _, err := os.Stat(p); err == nil {
				return buildSocket(p)
			}
		}
	}

	return buildTCP("http://localhost:" + dockerTCPPort)
}

// NewLAN cria um Client para uma máquina na rede local (192.168.x.x, 10.x.x.x, etc.).
//
//	c := docker.NewLAN("192.168.1.50")       // porta 2375
//	c := docker.NewLAN("192.168.1.50:2376")  // porta customizada
func NewLAN(addr string) *Client {
	return buildTCP("http://" + normalizeAddr(addr))
}

// NewHost cria um Client para um host remoto via IP público ou hostname.
//
//	c := docker.NewHost("203.0.113.10")        // IP público, porta 2375
//	c := docker.NewHost("docker.meusite.com")  // hostname
//	c := docker.NewHost("203.0.113.10:2376")   // porta customizada
func NewHost(addr string) *Client {
	return buildTCP("http://" + normalizeAddr(addr))
}

// NewUnix cria um Client via socket Unix.
//
//	c := docker.NewUnix("/run/user/1000/docker.sock")
func NewUnix(sockPath string) *Client {
	return buildSocket(sockPath)
}

// ── Construtores internos ─────────────────────────────────────────────────────

func buildSocket(sockPath string) *Client {
	return &Client{
		baseURL: "http://docker",
		http: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
				},
			},
		},
	}
}

func buildTCP(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// ── Detecção de ambiente ──────────────────────────────────────────────────────

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

func canReachTCP(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func normalizeAddr(addr string) string {
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "tcp://")
	if !strings.Contains(addr, ":") {
		addr += ":" + dockerTCPPort
	}
	return addr
}

// ── Métodos HTTP ──────────────────────────────────────────────────────────────

// Get faz um GET na Docker Engine API v1.54 e retorna o corpo da resposta.
//
//	data, err := c.Get("/containers/json")
func (c *Client) Get(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/"+apiVersion+path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("docker API %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// Post faz um POST na Docker Engine API v1.54 com body JSON opcional.
//
//	data, err := c.Post("/containers/create", payload)
//	data, err := c.Post("/containers/"+id+"/start", nil)
func (c *Client) Post(path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/"+apiVersion+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("docker API %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}
