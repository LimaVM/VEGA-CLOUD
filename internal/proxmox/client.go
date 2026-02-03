package proxmox

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vega-cloud/internal/config"
)

// Client é o cliente para a API do Proxmox
type Client struct {
	baseURL    string
	node       string
	httpClient *http.Client
	authHeader string
}

// NewClient cria um novo cliente Proxmox
func NewClient(cfg *config.ProxmoxConfig) (*Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.Insecure,
		},
	}

	client := &Client{
		baseURL: strings.TrimSuffix(cfg.URL, "/"),
		node:    cfg.Node,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second,
		},
	}

	// Autenticação via Token API
	if cfg.TokenID != "" && cfg.TokenSecret != "" {
		client.authHeader = fmt.Sprintf("PVEAPIToken=%s=%s", cfg.TokenID, cfg.TokenSecret)
	} else if cfg.User != "" && cfg.Password != "" {
		// Autenticação via ticket (user/password)
		ticket, csrf, err := client.getTicket(cfg.User, cfg.Password)
		if err != nil {
			return nil, fmt.Errorf("failed to get ticket: %w", err)
		}
		client.authHeader = fmt.Sprintf("PVEAuthCookie=%s;CSRFPreventionToken=%s", ticket, csrf)
	} else {
		return nil, fmt.Errorf("no authentication method configured")
	}

	return client, nil
}

func (c *Client) getTicket(user, password string) (string, string, error) {
	data := url.Values{}
	data.Set("username", user)
	data.Set("password", password)

	resp, err := c.httpClient.PostForm(c.baseURL+"/api2/json/access/ticket", data)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			Ticket              string `json:"ticket"`
			CSRFPreventionToken string `json:"CSRFPreventionToken"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}

	return result.Data.Ticket, result.Data.CSRFPreventionToken, nil
}

func (c *Client) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", c.authHeader)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	return c.httpClient.Do(req)
}

// GetAuthHeaders retorna os headers de autenticação para uso externo (ws)
func (c *Client) GetAuthHeaders() map[string]string {
	headers := make(map[string]string)
	// Se for Token, usa Authorization
	if strings.HasPrefix(c.authHeader, "PVEAPIToken=") {
		headers["Authorization"] = c.authHeader
	} else {
		// Se for Cookie (User/Pass), tenta extrair ou usa como Authorization (fallback)
		// O formato salvo em authHeader é "PVEAuthCookie=...;CSRF..."
		// Para WS, geralmente precisamos passar no header Cookie
		headers["Cookie"] = c.authHeader
		// Se necessário, também passar CSRF
		if strings.Contains(c.authHeader, "CSRFPreventionToken") {
			headers["CSRFPreventionToken"] = extractCSRF(c.authHeader)
		}
	}
	return headers
}

func extractCSRF(header string) string {
	parts := strings.Split(header, ";")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "CSRFPreventionToken=") {
			return strings.TrimPrefix(p, "CSRFPreventionToken=")
		}
	}
	return ""
}

// ========== LXC Container Operations ==========

// CreateContainer cria um novo container LXC a partir de um template
func (c *Client) CreateContainer(ctid int, hostname, template, password string, cores, memory int) error {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc", c.node)

	data := url.Values{}
	data.Set("vmid", fmt.Sprintf("%d", ctid))
	data.Set("hostname", hostname)
	data.Set("ostemplate", template)
	data.Set("password", password)
	data.Set("cores", fmt.Sprintf("%d", cores))
	data.Set("cpulimit", fmt.Sprintf("%d", cores))
	data.Set("memory", fmt.Sprintf("%d", memory))
	data.Set("swap", "512")
	data.Set("storage", "local-lvm")
	data.Set("rootfs", "local-lvm:16")
	data.Set("net0", "name=eth0,bridge=vmbr0,ip=dhcp")
	data.Set("start", "1")
	data.Set("unprivileged", "1")
	data.Set("features", "nesting=1")
	data.Set("startup", "order=1")

	resp, err := c.doRequest("POST", path, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create container failed: %s", string(body))
	}

	// Aguarda container iniciar (não bloqueia aqui, vm.go gerencia setup)
	return nil
}

// configureSSH configura SSH no container após criação
func (c *Client) configureSSH(ctid int, password string) {
	// Aguarda container estar rodando
	time.Sleep(10 * time.Second)

	// Executa comandos via exec API do Proxmox
	commands := []string{
		"sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config",
		"sed -i 's/PermitRootLogin prohibit-password/PermitRootLogin yes/' /etc/ssh/sshd_config",
		"sed -i 's/#PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config",
		"systemctl restart sshd || service ssh restart",
	}

	for _, cmd := range commands {
		c.execInContainer(ctid, cmd)
		time.Sleep(500 * time.Millisecond)
	}
}

// execInContainer executa um comando dentro do container via API Proxmox
func (c *Client) execInContainer(ctid int, command string) error {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/status/current", c.node, ctid)

	// Primeiro verifica se o container está rodando
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Para LXC, podemos usar o endpoint exec que executa via pct exec
	// Porém isso não está disponível diretamente na API REST
	// A solução é usar o terminal após o container estar pronto

	return nil
}

// StartContainer inicia um container
func (c *Client) StartContainer(ctid int) error {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/status/start", c.node, ctid)

	resp, err := c.doRequest("POST", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("start failed: %s", string(body))
	}

	return nil
}

// StopContainer para um container
func (c *Client) StopContainer(ctid int) error {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/status/stop", c.node, ctid)

	resp, err := c.doRequest("POST", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stop failed: %s", string(body))
	}

	return nil
}

// DeleteContainer deleta um container
func (c *Client) DeleteContainer(ctid int) error {
	// Primeiro para o container
	_ = c.StopContainer(ctid)

	// Aguarda um pouco para o container parar
	time.Sleep(3 * time.Second)

	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d", c.node, ctid)

	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed: %s", string(body))
	}

	return nil
}

// ContainerStatus representa o status de um container
type ContainerStatus struct {
	Status string  `json:"status"`
	VMID   int     `json:"vmid"`
	Name   string  `json:"name"`
	Uptime int     `json:"uptime"`
	CPUs   int     `json:"cpus"`
	Cpu    float64 `json:"cpu"`
	MaxMem int64   `json:"maxmem"`
	Mem    int64   `json:"mem"`
}

// GetContainerStatus retorna o status de um container
func (c *Client) GetContainerStatus(ctid int) (*ContainerStatus, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/status/current", c.node, ctid)

	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data ContainerStatus `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Debug Stats
	// log.Printf("[DEBUG] Stats for VM %d: Status=%s, CPU=%f, Mem=%d, MaxMem=%d", ctid, result.Data.Status, result.Data.Cpu, result.Data.Mem, result.Data.MaxMem)

	return &result.Data, nil
}

// ContainerExists verifica se um container com o CTID existe no Proxmox
func (c *Client) ContainerExists(ctid int) bool {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/status/current", c.node, ctid)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// GetContainerIP obtém o IP do container via interface de rede
func (c *Client) GetContainerIP(ctid int) (string, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/interfaces", c.node, ctid)

	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("interfaces not available")
	}

	var result struct {
		Data []struct {
			Name  string `json:"name"`
			Inet  string `json:"inet"`
			Inet6 string `json:"inet6"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	// Procura por eth0 com IP
	for _, iface := range result.Data {
		if iface.Name == "eth0" && iface.Inet != "" {
			// Remove CIDR notation se existir
			ip := strings.Split(iface.Inet, "/")[0]
			return ip, nil
		}
	}

	return "", fmt.Errorf("no IP found")
}

// GetNextVMID retorna o próximo VMID disponível
func (c *Client) GetNextVMID() (int, error) {
	path := "/api2/json/cluster/nextid"

	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Data json.Number `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	vmid, err := result.Data.Int64()
	if err != nil {
		return 0, fmt.Errorf("invalid vmid: %w", err)
	}

	return int(vmid), nil
}

// GetVNCTerminal retorna as informações para terminal do container (usando termproxy para LXC)
func (c *Client) GetVNCTerminal(ctid int) (string, string, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/termproxy", c.node, ctid)

	resp, err := c.doRequest("POST", path, nil)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("termproxy failed: %s", string(body))
	}

	var result struct {
		Data struct {
			Ticket string      `json:"ticket"`
			Port   json.Number `json:"port"`
			User   string      `json:"user"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}

	port, _ := result.Data.Port.Int64()

	// URL do websocket terminal
	host := strings.TrimPrefix(strings.TrimPrefix(c.baseURL, "https://"), "http://")
	wsURL := fmt.Sprintf("wss://%s/api2/json/nodes/%s/lxc/%d/vncwebsocket?port=%d&vncticket=%s",
		host, c.node, ctid, port, url.QueryEscape(result.Data.Ticket))

	return wsURL, result.Data.Ticket, nil
}

// NodeStatus representa o status do node Proxmox
type NodeStatus struct {
	Uptime float64 `json:"uptime"`
	Cpu    float64 `json:"cpu"`
	Memory struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
		Free  int64 `json:"free"`
	} `json:"memory"`
	BootTime int64 `json:"boot-time"`
}

// GetNodeStatus retorna o status do node
func (c *Client) GetNodeStatus() (*NodeStatus, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/status", c.node)

	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Proxmox node status response structure
	var result struct {
		Data NodeStatus `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result.Data, nil
}

// GetNodeVNCTerminal retorna as informações para terminal do HOST Proxmox
func (c *Client) GetNodeVNCTerminal() (string, string, string, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/termproxy", c.node)

	resp, err := c.doRequest("POST", path, nil)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", "", "", fmt.Errorf("node termproxy failed: %s", string(body))
	}

	var result struct {
		Data struct {
			Ticket string      `json:"ticket"`
			Port   json.Number `json:"port"`
			User   string      `json:"user"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", err
	}

	port, _ := result.Data.Port.Int64()

	// URL do websocket terminal para o NODE
	host := strings.TrimPrefix(strings.TrimPrefix(c.baseURL, "https://"), "http://")
	scheme := "wss"
	if strings.HasPrefix(c.baseURL, "http://") {
		scheme = "ws"
	}
	wsURL := fmt.Sprintf("%s://%s/api2/json/nodes/%s/vncwebsocket?port=%d&vncticket=%s",
		scheme, host, c.node, port, url.QueryEscape(result.Data.Ticket))

	return wsURL, result.Data.Ticket, result.Data.User, nil
}

// RRDDataPoint represents a single point in RRD data
type RRDDataPoint struct {
	Time   int64   `json:"time"`
	CPU    float64 `json:"cpu,omitempty"`
	Mem    float64 `json:"mem,omitempty"`
	MaxMem float64 `json:"maxmem,omitempty"`
}

// GetContainerRRD retorna dados históricos (RRD) do container
// timeframe: "hour", "day", "week", "month", "year"
func (c *Client) GetContainerRRD(ctid int, timeframe string) ([]RRDDataPoint, error) {
	if timeframe == "" {
		timeframe = "hour"
	}
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/rrddata?timeframe=%s", c.node, ctid, timeframe)

	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get rrd data: status %d", resp.StatusCode)
	}

	var result struct {
		Data []RRDDataPoint `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Data, nil
}
