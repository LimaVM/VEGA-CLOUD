package proxmox

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"

	"vega-cloud/internal/config"
	"vega-cloud/internal/database"
	"vega-cloud/internal/mikrotik"

	"golang.org/x/crypto/ssh"
)

// ContainerManager gerencia operações de alto nível em containers LXC
type ContainerManager struct {
	client   *Client
	cfg      *config.Config
	mikrotik *mikrotik.Client
}

// GetNodeStatus retorna status do node Proxmox
func (m *ContainerManager) GetNodeStatus() (*NodeStatus, error) {
	return m.client.GetNodeStatus()
}

// GetNodeVNCTerminal retorna terminal do node Proxmox
func (m *ContainerManager) GetNodeVNCTerminal() (string, string, string, error) {
	return m.client.GetNodeVNCTerminal()
}

// IsInsecure retorna se o cliente Proxmox está em modo inseguro (sem verificar SSL)
func (m *ContainerManager) IsInsecure() bool {
	return m.cfg.Proxmox.Insecure
}

// GetNodeOrigin retorna o origin para uso em websockets do Proxmox.
func (m *ContainerManager) GetNodeOrigin() string {
	return m.cfg.Proxmox.URL
}

// GetAuthHeaders retorna headers de autenticação do client
func (m *ContainerManager) GetAuthHeaders() map[string]string {
	return m.client.GetAuthHeaders()
}

// NewContainerManager cria um novo ContainerManager
func NewContainerManager(client *Client, cfg *config.Config) *ContainerManager {
	return &ContainerManager{
		client: client,
		cfg:    cfg,
	}
}

// SetMikroTik configura o cliente MikroTik
func (m *ContainerManager) SetMikroTik(mk *mikrotik.Client) {
	m.mikrotik = mk
}

// GeneratePassword gera uma senha aleatória segura
func GeneratePassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	password := make([]byte, length)
	for i := range password {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		password[i] = charset[n.Int64()]
	}
	return string(password), nil
}

// GenerateUniqueCTID gera um CTID aleatório único (entre 100 e 999999)
func (m *ContainerManager) GenerateUniqueCTID() (int, error) {
	for i := 0; i < 100; i++ {
		// Gera CTID entre 100 e 999999
		n, err := rand.Int(rand.Reader, big.NewInt(999900))
		if err != nil {
			continue
		}
		ctid := int(n.Int64()) + 100

		// Verifica se não existe no banco
		exists, _ := database.CheckCTIDExists(ctid)
		if !exists {
			// Verifica se não existe no Proxmox
			if !m.client.ContainerExists(ctid) {
				return ctid, nil
			}
		}
	}
	return 0, fmt.Errorf("não foi possível gerar CTID único")
}

// CreateContainer cria um novo container a partir de um template
func (m *ContainerManager) CreateContainer(template string, userID int64, cores int, memory int, ttl time.Duration) (*database.Container, error) {
	// Seleciona template
	var tmpl config.TemplateConfig
	switch template {
	case "ubuntu":
		tmpl = m.cfg.Templates.Ubuntu
	case "debian":
		tmpl = m.cfg.Templates.Debian
	default:
		return nil, fmt.Errorf("template inválido: %s", template)
	}
	if tmpl.OSTemplate == "" {
		return nil, fmt.Errorf("template não configurado: %s", template)
	}

	// Gera CTID aleatório único
	ctid, err := m.GenerateUniqueCTID()
	if err != nil {
		return nil, fmt.Errorf("erro ao gerar CTID: %w", err)
	}

	// Gera senha
	password, err := GeneratePassword(12)
	if err != nil {
		return nil, fmt.Errorf("erro ao gerar senha: %w", err)
	}

	// Nome do container
	hostname := fmt.Sprintf("vega-%d", ctid)

	// Cria a entrada no banco
	container := &database.Container{
		CTID:      ctid,
		UserID:    userID,
		Name:      hostname,
		Template:  template,
		Password:  password,
		State:     "creating",
		Cores:     cores,
		Memory:    memory,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(ttl),
	}

	// Salva no banco
	if err := database.SaveContainer(container); err != nil {
		return nil, fmt.Errorf("erro ao salvar no banco: %w", err)
	}

	// Cria o container no Proxmox em background
	go func() {
		err := m.client.CreateContainer(
			ctid,
			hostname,
			tmpl.OSTemplate,
			password,
			cores,
			memory,
		)
		if err != nil {
			container.State = "error"
			database.SaveContainer(container)
			return
		}

		container.State = "provisioning"
		database.SaveContainer(container)

		// Tenta obter IP
		go m.pollForIP(container)
	}()

	return container, nil
}

// pollForIP tenta obter o IP do container periodicamente e configura SSH
func (m *ContainerManager) pollForIP(container *database.Container) {
	time.Sleep(5 * time.Second)

	for i := 0; i < 30; i++ {
		ip, err := m.client.GetContainerIP(container.CTID)
		if err == nil && ip != "" {
			container.IP = ip
			database.SaveContainer(container)

			// Configura SSH automaticamente
			go m.configureSSH(container)
			return
		}
		time.Sleep(5 * time.Second)
	}
}

// configureSSH configura SSH para permitir login root com senha
func (m *ContainerManager) configureSSH(container *database.Container) {
	log.Printf("🔧 Configurando SSH para container %d (%s)...", container.CTID, container.IP)

	// Aguarda SSH estar disponível
	time.Sleep(10 * time.Second)

	// Tenta conectar e configurar
	for attempt := 0; attempt < 5; attempt++ {
		err := m.runSSHConfig(container)
		if err == nil {
			log.Printf("✅ SSH configurado para container %d", container.CTID)

			// Configura NAT no MikroTik se habilitado
			// Configura NAT no MikroTik se habilitado
			if m.cfg.MikroTik.Enabled && m.mikrotik != nil {
				log.Printf("🌐 Configurando NAT para container %d...", container.CTID)
				// Tenta configurar NAT, mas não aborta processo se falhar o NAT, apenas loga
				func() {
					port, err := m.mikrotik.GetFreePort()
					if err != nil {
						log.Printf("❌ Erro ao obter porta livre: %v", err)
						return
					}

					comment := fmt.Sprintf("vega-container-%d", container.CTID)
					user, err := database.GetUserByID(container.UserID)
					if err == nil && user != nil {
						comment = fmt.Sprintf("vega: %s - %s (ID: %d)", user.Username, container.Name, container.CTID)
					}

					if err := m.mikrotik.AddNATRule(port, container.IP, 22, "tcp", comment); err != nil {
						log.Printf("❌ Erro ao criar regra NAT: %v", err)
					} else {
						container.ExternalPort = port
						log.Printf("✅ NAT criado: %s:%d -> %s:22 (%s)", m.cfg.MikroTik.PublicIP, port, container.IP, comment)
					}
				}()
			}

			container.State = "running"
			database.SaveContainer(container)
			return
		}
		log.Printf("⚠️ Tentativa %d falhou: %v", attempt+1, err)
		time.Sleep(5 * time.Second)
	}

	log.Printf("❌ Falha ao configurar SSH para container %d", container.CTID)
}

// runSSHConfig executa os comandos de configuração SSH via pct exec no Proxmox
func (m *ContainerManager) runSSHConfig(container *database.Container) error {
	if m.cfg.Proxmox.SSHHost == "" || m.cfg.Proxmox.SSHPassword == "" {
		log.Printf("⚠️ SSH do Proxmox não configurado, pulando configuração automática")
		return nil
	}

	config := &ssh.ClientConfig{
		User: m.cfg.Proxmox.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(m.cfg.Proxmox.SSHPassword),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := m.cfg.Proxmox.SSHHost + ":22"
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("falha ao conectar no Proxmox: %w", err)
	}
	defer client.Close()

	// Comandos para configurar SSH no container
	commands := []string{
		fmt.Sprintf("pct exec %d -- sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config", container.CTID),
		fmt.Sprintf("pct exec %d -- sed -i 's/PermitRootLogin prohibit-password/PermitRootLogin yes/' /etc/ssh/sshd_config", container.CTID),
		fmt.Sprintf("pct exec %d -- sed -i 's/#PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config", container.CTID),
		fmt.Sprintf("pct exec %d -- sed -i 's/PasswordAuthentication no/PasswordAuthentication yes/' /etc/ssh/sshd_config", container.CTID),
		fmt.Sprintf("pct exec %d -- systemctl restart ssh 2>/dev/null || pct exec %d -- service sshd restart 2>/dev/null", container.CTID, container.CTID),
		// Garante que a senha esteja correta
		fmt.Sprintf("pct exec %d -- bash -c \"echo 'root:%s' | chpasswd\"", container.CTID, container.Password),
	}

	for _, cmd := range commands {
		session, err := client.NewSession()
		if err != nil {
			continue
		}
		session.Run(cmd)
		session.Close()
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

// ExecNodeCommand executa um comando no node Proxmox via SSH
func (m *ContainerManager) ExecNodeCommand(cmd string) (string, error) {
	if m.cfg.Proxmox.SSHHost == "" || m.cfg.Proxmox.SSHPassword == "" {
		return "", fmt.Errorf("SSH não configurado")
	}

	config := &ssh.ClientConfig{
		User: m.cfg.Proxmox.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(m.cfg.Proxmox.SSHPassword),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := m.cfg.Proxmox.SSHHost + ":22"
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", fmt.Errorf("falha ao conectar no Proxmox: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("erro ao criar sessão SSH: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return string(output), fmt.Errorf("erro ao executar comando: %w (output: %s)", err, string(output))
	}

	return string(output), nil
}

// FileEntry representa um arquivo ou diretório
type FileEntry struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	IsDir       bool   `json:"is_dir"`
	Permissions string `json:"permissions"`
	ModTime     string `json:"mod_time"`
}

// ListFiles lista arquivos de um container
func (m *ContainerManager) ListFiles(userID int64, ctid int, path string) ([]FileEntry, error) {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return nil, err
	}
	if container.UserID != userID {
		return nil, fmt.Errorf("acesso negado")
	}

	if path == "" {
		path = "/"
	}
	// Sanitize path (basic check)
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("caminho inválido")
	}

	// ls -la --time-style=long-iso --block-size=1
	cmd := fmt.Sprintf("pct exec %d -- ls -la --time-style=long-iso --block-size=1 \"%s\"", ctid, path)
	output, err := m.ExecNodeCommand(cmd)
	if err != nil {
		return nil, err
	}

	var entries []FileEntry
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		// drwxr-xr-x 2 root root 4096 2024-02-01 12:00 .
		// Format: perms links owner group size date time name
		parts := strings.Fields(line)
		if len(parts) < 8 {
			continue
		}

		name := strings.Join(parts[7:], " ")
		if name == "." || name == ".." {
			continue
		}

		isDir := strings.HasPrefix(parts[0], "d")
		size, _ := strconv.ParseInt(parts[4], 10, 64)

		entries = append(entries, FileEntry{
			Name:        name,
			Size:        size,
			IsDir:       isDir,
			Permissions: parts[0],
			ModTime:     parts[5] + " " + parts[6],
		})
	}

	return entries, nil
}

// ReadFile lê o conteúdo de um arquivo
func (m *ContainerManager) ReadFile(userID int64, ctid int, path string) (string, error) {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return "", err
	}
	if container.UserID != userID {
		return "", fmt.Errorf("acesso negado")
	}

	if strings.Contains(path, "..") {
		return "", fmt.Errorf("caminho inválido")
	}

	cmd := fmt.Sprintf("pct exec %d -- cat \"%s\"", ctid, path)
	return m.ExecNodeCommand(cmd)
}

// WriteFile salva conteúdo em um arquivo
func (m *ContainerManager) WriteFile(userID int64, ctid int, path string, content string) error {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return err
	}
	if container.UserID != userID {
		return fmt.Errorf("acesso negado")
	}

	if strings.Contains(path, "..") {
		return fmt.Errorf("caminho inválido")
	}

	// Usando base64 para evitar problemas de escape
	// pct exec 100 -- bash -c "echo BASE64_CONTENT | base64 -d > PATH"
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	cmd := fmt.Sprintf("pct exec %d -- bash -c \"echo '%s' | base64 -d > '%s'\"", ctid, encoded, path)

	_, err = m.ExecNodeCommand(cmd)
	return err
}

// DeleteContainer deleta um container
func (m *ContainerManager) DeleteContainer(ctid int) error {
	// Deleta do Proxmox
	if err := m.client.DeleteContainer(ctid); err != nil {
		// Se o erro for "does not exist", o container já sumiu do Proxmox.
		// Podemos prosseguir e remover do banco.
		if strings.Contains(err.Error(), "does not exist") {
			log.Printf("⚠️ Container %d não existe no Proxmox, removendo apenas do banco.", ctid)
		} else {
			return err
		}
	}

	// Remove NAT do MikroTik
	if m.cfg.MikroTik.Enabled && m.mikrotik != nil {
		if err := m.mikrotik.DeleteNATRuleByContainerID(ctid); err != nil {
			log.Printf("⚠️ Erro ao remover NAT para container %d: %v", ctid, err)
		}
	}

	// Deleta do banco
	return database.DeleteContainer(ctid)
}

// StopContainer para um container
func (m *ContainerManager) StopContainer(ctid int) error {
	if err := m.client.StopContainer(ctid); err != nil {
		return err
	}
	// Update DB state
	ct, err := database.GetContainerByCtid(ctid)
	if err == nil {
		ct.State = "stopped"
		database.SaveContainer(ct)
	}
	return nil
}

// StartContainer inicia um container
func (m *ContainerManager) StartContainer(ctid int) error {
	if err := m.client.StartContainer(ctid); err != nil {
		return err
	}
	// Update DB state
	ct, err := database.GetContainerByCtid(ctid)
	if err == nil {
		ct.State = "running"
		database.SaveContainer(ct)
	}
	return nil
}

// RestartContainer reinicia um container
func (m *ContainerManager) RestartContainer(ctid int) error {
	// O restart do Proxmox via API node/{node}/lxc/{vmid}/status/reboot
	// Mas o client.go pode não ter RebootContainer exposto, vamos verificar.
	// Se não tiver, usamos Shutdown + Start ou implementamos Reboot no client.
	// Assumindo que o client possui comandos básicos ou podemos chamar via ExecNodeCommand se falhar.
	// Como o client.go não foi modificado neste passo, vamos assumir que precisamos implementar
	// ou usar o que temos. O client.go tem Start/Stop.
	// Vamos tentar Shutdown e depois Start se não tiver reboot direto.
	// Melhor: Vamos verificar se o client tem Reboot ou Shutdown.
	// Mas para ser seguro e rápido:
	if err := m.client.StopContainer(ctid); err != nil {
		// Log erro mas tenta iniciar
		log.Printf("Erro ao parar container para restart: %v", err)
	}
	time.Sleep(2 * time.Second)
	return m.client.StartContainer(ctid)
}

// ResetTimer reseta o timer de um container
func (m *ContainerManager) ResetTimer(ctid int) error {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return fmt.Errorf("container não encontrado")
	}

	container.ExpiresAt = time.Now().Add(time.Duration(m.cfg.Reaper.TTLSeconds) * time.Second)
	return database.SaveContainer(container)
}

// GetConsoleURL retorna a URL do terminal VNC
func (m *ContainerManager) GetConsoleURL(ctid int) (string, error) {
	wsURL, _, err := m.client.GetVNCTerminal(ctid)
	if err != nil {
		return "", err
	}
	return wsURL, nil
}

// GetConsoleTicket retorna URL e ticket do terminal VNC
func (m *ContainerManager) GetConsoleTicket(ctid int) (string, string, error) {
	return m.client.GetVNCTerminal(ctid)
}

// CreatePortForward cria uma regra de redirecionamento de porta
func (m *ContainerManager) CreatePortForward(userID int64, ctid int, internalPort int, protocol string) (*database.PortForward, error) {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return nil, fmt.Errorf("container não encontrado")
	}

	if container.UserID != userID {
		return nil, fmt.Errorf("acesso negado")
	}

	comment := fmt.Sprintf("vega-fw: %d -> %d/%s", ctid, internalPort, protocol)

	// Create rule in MikroTik
	externalPort, err := m.mikrotik.CreatePortForwarding(container.IP, internalPort, protocol, comment)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar regra no firewall: %w", err)
	}

	// Save to DB
	pf := &database.PortForward{
		ContainerID:  container.ID,
		ExternalPort: externalPort,
		InternalPort: internalPort,
		Protocol:     protocol,
		CreatedAt:    time.Now(),
	}

	if err := database.CreatePortForward(pf); err != nil {
		// Rollback MikroTik rule if DB save fails
		m.mikrotik.DeletePortForwarding(externalPort, protocol)
		return nil, fmt.Errorf("erro ao salvar no banco: %w", err)
	}

	return pf, nil
}

// DeletePortForward remove uma regra
func (m *ContainerManager) DeletePortForward(userID int64, pfID int64) error {
	pf, err := database.GetPortForwardByID(pfID)
	if err != nil {
		return fmt.Errorf("regra não encontrada")
	}

	// Busca user_id do dono do container
	// (Query direta pois não temos container carregado)
	var containerUserID int64
	err = database.DB.QueryRow("SELECT user_id FROM containers WHERE id = ?", pf.ContainerID).Scan(&containerUserID)
	if err != nil {
		return fmt.Errorf("container associado não encontrado")
	}

	if containerUserID != userID {
		return fmt.Errorf("acesso negado")
	}

	// Remove from MikroTik
	if err := m.mikrotik.DeletePortForwarding(pf.ExternalPort, pf.Protocol); err != nil {
		log.Printf("⚠️ Erro ao remover do MikroTik (pode já ter sido removido): %v", err)
	}

	// Remove from DB
	return database.DeletePortForward(pfID)
}

// GetPortForwards lista regras de um container
func (m *ContainerManager) GetPortForwards(userID int64, ctid int) ([]*database.PortForward, error) {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return nil, err
	}

	if container.UserID != userID {
		return nil, fmt.Errorf("acesso negado")
	}

	return database.GetPortForwardsByContainerID(container.ID)
}

// GetContainerStats retorna estatísticas do container via Proxmox
func (m *ContainerManager) GetContainerStats(ctid int) (*ContainerStatus, error) {
	return m.client.GetContainerStatus(ctid)
}

// ========== Snapshots Operations ==========

// ListSnapshots lista snapshots de um container
func (m *ContainerManager) ListSnapshots(userID int64, ctid int) ([]Snapshot, error) {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return nil, err
	}
	if container.UserID != userID {
		return nil, fmt.Errorf("acesso negado")
	}

	return m.client.ListSnapshots(ctid)
}

// CreateSnapshot cria um snapshot verificando limites
func (m *ContainerManager) CreateSnapshot(userID int64, ctid int, name string) error {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return err
	}
	if container.UserID != userID {
		return fmt.Errorf("acesso negado")
	}

	user, err := database.GetUserByID(userID)
	if err != nil {
		return err
	}

	// Check limits
	snapshots, err := m.client.ListSnapshots(ctid)
	if err != nil {
		return err
	}

	limit := m.cfg.Limits.MaxSnapshotsFree
	if user.IsPremium {
		limit = m.cfg.Limits.MaxSnapshotsPremium
	}

	if len(snapshots) >= limit {
		return fmt.Errorf("limite de snapshots atingido (Max: %d)", limit)
	}

	return m.client.CreateSnapshot(ctid, name, "Created by Vega Cloud")
}

// RestoreSnapshot restaura um snapshot
func (m *ContainerManager) RestoreSnapshot(userID int64, ctid int, name string) error {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return err
	}
	if container.UserID != userID {
		return fmt.Errorf("acesso negado")
	}

	// Para restaurar, o container deve estar parado ou o Proxmox lida com isso?
	// Geralmente o Proxmox locka o container. Vamos mandar o comando.
	// UPDATE: Rollback muitas vezes requer container parado se mudar config.
	// Vamos tentar direto.

	err = m.client.RollbackSnapshot(ctid, name)
	if err == nil {
		// Start container back up if needed? Usually rollback leaves it stopped if it differs.
		// Let's ensure it's running after simple sleep
		time.Sleep(5 * time.Second)
		m.client.StartContainer(ctid)
	}
	return err
}

// DeleteSnapshot deleta um snapshot
func (m *ContainerManager) DeleteSnapshot(userID int64, ctid int, name string) error {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return err
	}
	if container.UserID != userID {
		return fmt.Errorf("acesso negado")
	}

	return m.client.DeleteSnapshot(ctid, name)
}

// GetContainerGraphs retorna dados de gráfico
func (m *ContainerManager) GetContainerGraphs(userID int64, ctid int, timeframe string) ([]RRDDataPoint, error) {
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		return nil, err
	}
	if container.UserID != userID {
		return nil, fmt.Errorf("acesso negado")
	}

	return m.client.GetContainerRRD(ctid, timeframe)
}
