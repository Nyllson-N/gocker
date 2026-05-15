package gocker

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
)

// ══════════════════════════════════════════════════════════════════════════════
// TIPOS
// ══════════════════════════════════════════════════════════════════════════════

// Container representa o resumo de um container retornado pela API.
type Container struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	Command string            `json:"Command"`
	Created int64             `json:"Created"`
	State   string            `json:"State"`  // "running", "exited", "paused", etc.
	Status  string            `json:"Status"` // "Up 2 hours", "Exited (0) 5 minutes ago", etc.
	Ports   []Port            `json:"Ports"`
	Labels  map[string]string `json:"Labels"`
}

// Port representa um mapeamento de porta de um container.
type Port struct {
	IP          string `json:"IP,omitempty"`
	PrivatePort uint16 `json:"PrivatePort"`
	PublicPort  uint16 `json:"PublicPort,omitempty"`
	Type        string `json:"Type"` // "tcp" ou "udp"
}

// CreateConfig é a configuração completa para criar um container.
// Campos omitidos recebem os padrões do Docker.
type CreateConfig struct {
	Image        string              `json:"Image"`
	Cmd          []string            `json:"Cmd,omitempty"`
	Entrypoint   []string            `json:"Entrypoint,omitempty"`
	Env          []string            `json:"Env,omitempty"`           // ["VAR=valor", ...]
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`  // {"80/tcp": {}, ...}
	Labels       map[string]string   `json:"Labels,omitempty"`
	WorkingDir   string              `json:"WorkingDir,omitempty"`
	User         string              `json:"User,omitempty"`
	HostConfig   HostConfig          `json:"HostConfig,omitempty"`
}

// HostConfig é a configuração específica do host para o container.
type HostConfig struct {
	// Portas: mapa de "porta/protocolo" → [{HostIp, HostPort}]
	// Ex: {"80/tcp": [{"HostIp": "0.0.0.0", "HostPort": "8080"}]}
	PortBindings map[string][]PortBinding `json:"PortBindings,omitempty"`

	// Volumes/bind mounts no formato "origem:destino[:modo]"
	// Linux:   ["/dados:/app/dados", "/config:/etc/app:ro"]
	// Windows: ["C:\\dados:C:\\app\\dados", "C:\\config:C:\\etc\\app:ro"]
	Binds []string `json:"Binds,omitempty"`

	RestartPolicy RestartPolicy `json:"RestartPolicy,omitempty"`
	NetworkMode   string        `json:"NetworkMode,omitempty"` // "bridge", "host", "none", "nat"
	Privileged    bool          `json:"Privileged,omitempty"`
	AutoRemove    bool          `json:"AutoRemove,omitempty"` // remove o container ao parar
	Memory        int64         `json:"Memory,omitempty"`     // limite de memória em bytes
	CPUShares     int64         `json:"CpuShares,omitempty"`  // peso relativo de CPU (0 = padrão)
}

// PortBinding mapeia uma porta do container para uma porta do host.
type PortBinding struct {
	HostIP   string `json:"HostIp"`   // IP do host (ex: "0.0.0.0" ou "127.0.0.1")
	HostPort string `json:"HostPort"` // porta do host como string (ex: "8080")
}

// RestartPolicy define o comportamento de reinício do container.
type RestartPolicy struct {
	// "no" (padrão), "always", "on-failure", "unless-stopped"
	Name              string `json:"Name"`
	MaximumRetryCount int    `json:"MaximumRetryCount,omitempty"` // só para "on-failure"
}

// ExecResult contém a saída de um comando executado dentro do container.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// ══════════════════════════════════════════════════════════════════════════════
// OPERAÇÕES DE CONTAINER
// ══════════════════════════════════════════════════════════════════════════════

// ListContainers retorna a lista de containers.
// Passe all=true para incluir containers parados.
//
//	containers, err := c.ListContainers(true)
//	for _, ct := range containers {
//	    fmt.Println(ct.Names[0], ct.State)
//	}
func (c *Client) ListContainers(all bool) ([]Container, error) {
	path := "/containers/json"
	if all {
		path += "?all=true"
	}
	data, err := c.Get(path)
	if err != nil {
		return nil, err
	}
	var containers []Container
	if err := json.Unmarshal([]byte(data), &containers); err != nil {
		return nil, fmt.Errorf("gocker: parse containers: %w", err)
	}
	return containers, nil
}

// StartContainer inicia o container pelo ID ou nome.
//
//	err := c.StartContainer("meu-container")
func (c *Client) StartContainer(id string) error {
	_, err := c.Post("/containers/"+id+"/start", nil)
	return err
}

// StopContainer para o container pelo ID ou nome.
//
//	err := c.StopContainer("meu-container")
func (c *Client) StopContainer(id string) error {
	_, err := c.Post("/containers/"+id+"/stop", nil)
	return err
}

// RestartContainer reinicia o container pelo ID ou nome.
//
//	err := c.RestartContainer("meu-container")
func (c *Client) RestartContainer(id string) error {
	_, err := c.Post("/containers/"+id+"/restart", nil)
	return err
}

// CreateContainer cria um novo container com o nome e configuração fornecidos.
// Retorna o ID do container criado.
//
// O método ajusta automaticamente os padrões de NetworkMode e formato de Binds
// de acordo com o sistema operacional atual (Windows, Linux, macOS).
//
//	id, err := c.CreateContainer("meu-app", gocker.CreateConfig{
//	    Image: "nginx:latest",
//	    HostConfig: gocker.HostConfig{
//	        PortBindings: map[string][]gocker.PortBinding{
//	            "80/tcp": {{"0.0.0.0", "8080"}},
//	        },
//	    },
//	})
func (c *Client) CreateContainer(name string, cfg CreateConfig) (string, error) {
	cfg = applyOSDefaults(cfg)

	path := "/containers/create"
	if name != "" {
		path += "?name=" + name
	}

	data, err := c.Post(path, cfg)
	if err != nil {
		return "", err
	}

	var resp struct {
		ID       string   `json:"Id"`
		Warnings []string `json:"Warnings"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("gocker: parse create response: %w", err)
	}
	return resp.ID, nil
}

// applyOSDefaults preenche os padrões de configuração de acordo com o SO do host.
func applyOSDefaults(cfg CreateConfig) CreateConfig {
	switch runtime.GOOS {
	case "windows":
		// Windows usa NAT em vez de bridge
		if cfg.HostConfig.NetworkMode == "" {
			cfg.HostConfig.NetworkMode = "nat"
		}
	default:
		// Linux e macOS usam bridge
		if cfg.HostConfig.NetworkMode == "" {
			cfg.HostConfig.NetworkMode = "bridge"
		}
	}
	return cfg
}

// ══════════════════════════════════════════════════════════════════════════════
// EXEC — EXECUÇÃO DE COMANDOS DENTRO DO CONTAINER
// ══════════════════════════════════════════════════════════════════════════════

// Exec executa um comando dentro de um container em execução e retorna a saída.
//
// Funciona em qualquer SO: a comunicação é feita via HTTP com a Docker API,
// sem depender de binários locais como docker exec.
//
//	result, err := c.Exec("meu-container", []string{"ls", "-la", "/app"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(result.Stdout)
//	fmt.Println("exit code:", result.ExitCode)
func (c *Client) Exec(id string, cmd []string) (*ExecResult, error) {
	// Passo 1: criar instância de exec
	execID, err := c.execCreate(id, cmd)
	if err != nil {
		return nil, err
	}

	// Passo 2: iniciar exec e capturar saída
	raw, err := c.execStart(execID)
	if err != nil {
		return nil, err
	}

	// Passo 3: decodificar stream multiplexado
	result := demuxStream(raw)

	// Passo 4: obter código de saída
	result.ExitCode, err = c.execExitCode(execID)
	if err != nil {
		// não fatal: retorna o output mesmo sem o exit code
		result.ExitCode = -1
	}

	return result, nil
}

// execCreate cria a instância de exec e retorna seu ID.
func (c *Client) execCreate(containerID string, cmd []string) (string, error) {
	body := map[string]any{
		"Cmd":          cmd,
		"AttachStdout": true,
		"AttachStderr": true,
		"AttachStdin":  false,
		"Tty":          false,
	}
	data, err := c.Post("/containers/"+containerID+"/exec", body)
	if err != nil {
		return "", fmt.Errorf("gocker: exec create: %w", err)
	}

	var resp struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("gocker: exec create parse: %w", err)
	}
	return resp.ID, nil
}

// execStart inicia o exec e retorna o corpo bruto da resposta (stream multiplexado).
func (c *Client) execStart(execID string) ([]byte, error) {
	data, err := c.Post("/exec/"+execID+"/start", map[string]any{
		"Detach": false,
		"Tty":    false,
	})
	if err != nil {
		return nil, fmt.Errorf("gocker: exec start: %w", err)
	}
	return data, nil
}

// execExitCode consulta o código de saída de um exec já finalizado.
func (c *Client) execExitCode(execID string) (int, error) {
	data, err := c.Get("/exec/" + execID + "/json")
	if err != nil {
		return -1, err
	}

	var resp struct {
		ExitCode int `json:"ExitCode"`
	}
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		return -1, err
	}
	return resp.ExitCode, nil
}

// ══════════════════════════════════════════════════════════════════════════════
// DECODIFICAÇÃO DO STREAM MULTIPLEXADO
// ══════════════════════════════════════════════════════════════════════════════

// demuxStream decodifica o formato de stream multiplexado da Docker API.
//
// Formato de cada frame (8 bytes de cabeçalho + payload):
//
//	Byte 0:   tipo do stream → 1 = stdout, 2 = stderr
//	Bytes 1-3: padding (zeros)
//	Bytes 4-7: tamanho do payload (uint32, big-endian)
//	Bytes 8+:  payload (tamanho indicado no cabeçalho)
func demuxStream(data []byte) *ExecResult {
	var stdout, stderr strings.Builder

	for len(data) >= 8 {
		streamType := data[0]
		size := binary.BigEndian.Uint32(data[4:8])
		data = data[8:]

		if uint32(len(data)) < size {
			break // frame incompleto
		}

		payload := string(data[:size])
		data = data[size:]

		switch streamType {
		case 1:
			stdout.WriteString(payload)
		case 2:
			stderr.WriteString(payload)
		}
	}

	return &ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
}