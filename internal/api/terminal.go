package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"vega-cloud/internal/auth"
	"vega-cloud/internal/config"
	"vega-cloud/internal/database"
	"vega-cloud/internal/proxmox"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// TerminalProxy gerencia conexões WebSocket/SSH para o terminal
type TerminalProxy struct {
	containerManager *proxmox.ContainerManager
	cfg              *config.Config
}

// NewTerminalProxy cria um novo proxy de terminal
func NewTerminalProxy(containerManager *proxmox.ContainerManager, cfg *config.Config) *TerminalProxy {
	return &TerminalProxy{
		containerManager: containerManager,
		cfg:              cfg,
	}
}

// ServeWS lida com conexões WebSocket para o terminal SSH
func (tp *TerminalProxy) ServeWS(w http.ResponseWriter, r *http.Request) {
	// Verifica autenticação
	sessionID := auth.GetSessionFromRequest(r)
	user, err := auth.ValidateSession(sessionID)
	if err != nil {
		http.Error(w, "Não autenticado", http.StatusUnauthorized)
		return
	}

	// Extrai o CTID da URL
	path := strings.TrimPrefix(r.URL.Path, "/api/vm/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	ctid, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	// Verifica se o container pertence ao usuário
	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		http.Error(w, "Container não encontrado", http.StatusNotFound)
		return
	}

	if container.UserID != user.ID && !user.IsAdmin {
		http.Error(w, "Acesso negado", http.StatusForbidden)
		return
	}

	// Verifica se o container tem IP
	if container.IP == "" {
		http.Error(w, "Container não tem IP", http.StatusServiceUnavailable)
		return
	}

	// Upgrade da conexão do cliente para WebSocket
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Erro no upgrade WebSocket: %v", err)
		return
	}
	defer clientConn.Close()

	// Conecta via SSH ao container
	log.Printf("🔌 Tentando SSH: %s@%s (senha: %s)", "root", container.IP, container.Password)
	sshClient, err := tp.connectSSH(container.IP, "root", container.Password)
	if err != nil {
		log.Printf("Erro ao conectar SSH: %v", err)
		clientConn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31m✗ Erro ao conectar SSH: "+err.Error()+"\x1b[0m\r\n"))
		return
	}
	defer sshClient.Close()

	// Cria sessão SSH
	session, err := sshClient.NewSession()
	if err != nil {
		log.Printf("Erro ao criar sessão SSH: %v", err)
		clientConn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31m✗ Erro na sessão SSH\x1b[0m\r\n"))
		return
	}
	defer session.Close()

	// Configura PTY (tamanho maior para web terminal)
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", 30, 120, modes); err != nil {
		log.Printf("Erro ao solicitar PTY: %v", err)
		clientConn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31m✗ Erro ao configurar terminal\x1b[0m\r\n"))
		return
	}

	// Pipes de stdin/stdout
	stdin, err := session.StdinPipe()
	if err != nil {
		log.Printf("Erro ao obter stdin: %v", err)
		return
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		log.Printf("Erro ao obter stdout: %v", err)
		return
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		log.Printf("Erro ao obter stderr: %v", err)
		return
	}

	// Inicia shell
	if err := session.Shell(); err != nil {
		log.Printf("Erro ao iniciar shell: %v", err)
		clientConn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31m✗ Erro ao iniciar shell\x1b[0m\r\n"))
		return
	}

	log.Printf("🖥️  SSH conectado: container %d (user: %s, IP: %s)", ctid, user.Username, container.IP)

	var wg sync.WaitGroup
	done := make(chan struct{})
	var closeOnce sync.Once

	// WebSocket -> SSH (input do usuário)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stdin.Close()
		defer session.Close() // Garante que session.Wait retorne se WS cair
		for {
			select {
			case <-done:
				return
			default:
				_, msg, err := clientConn.ReadMessage()
				if err != nil {
					closeOnce.Do(func() { close(done) })
					return
				}

				// Verifica se é comando de resize
				if len(msg) > 0 && msg[0] == '{' {
					var resizeCmd struct {
						Type string `json:"type"`
						Cols int    `json:"cols"`
						Rows int    `json:"rows"`
					}
					if json.Unmarshal(msg, &resizeCmd) == nil && resizeCmd.Type == "resize" {
						session.WindowChange(resizeCmd.Rows, resizeCmd.Cols)
						continue
					}
				}

				if _, err := stdin.Write(msg); err != nil {
					closeOnce.Do(func() { close(done) })
					return
				}
			}
		}
	}()

	// SSH stdout -> WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
				n, err := stdout.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Printf("SSH stdout error: %v", err)
					}
					closeOnce.Do(func() { close(done) })
					return
				}
				if n > 0 {
					if err := clientConn.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
						closeOnce.Do(func() { close(done) })
						return
					}
				}
			}
		}
	}()

	// SSH stderr -> WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
				n, err := stderr.Read(buf)
				if err != nil {
					closeOnce.Do(func() { close(done) })
					return
				}
				if n > 0 {
					if err := clientConn.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
						closeOnce.Do(func() { close(done) })
						return
					}
				}
			}
		}
	}()

	// Aguarda sessão terminar
	session.Wait()
	closeOnce.Do(func() {
		close(done)
	})
	wg.Wait()

	log.Printf("🖥️  SSH desconectado: container %d", ctid)
}

// connectSSH estabelece conexão SSH com o container
func (tp *TerminalProxy) connectSSH(host, user, password string) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := host + ":22"
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// ServeNodeWS lida com conexões WebSocket para o terminal do Node Proxmox
func (tp *TerminalProxy) ServeNodeWS(w http.ResponseWriter, r *http.Request) {
	// Verifica autenticação
	sessionID := auth.GetSessionFromRequest(r)
	user, err := auth.ValidateSession(sessionID)
	if err != nil {
		http.Error(w, "Não autenticado", http.StatusUnauthorized)
		return
	}

	if !user.IsAdmin {
		http.Error(w, "Acesso negado", http.StatusForbidden)
		return
	}

	// Obtém URL do terminal VNC
	wsURL, ticket, proxmoxUser, err := tp.containerManager.GetNodeVNCTerminal()
	if err != nil {
		log.Printf("Erro ao obter terminal do node: %v", err)
		http.Error(w, "Erro ao obter terminal", http.StatusInternalServerError)
		return
	}

	// Upgrade conexão do cliente (Browser <-> Go)
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Erro no upgrade WebSocket: %v", err)
		return
	}
	defer clientConn.Close()

	// Configurar Dialer para o Proxmox (Go <-> Proxmox)
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
		Subprotocols:     []string{"binary"}, // Proxmox usually expects binary subprotocol
	}

	// Se config diz insecure, ignorar certificado (necessário custom TLS config)
	if tp.containerManager.IsInsecure() {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	headers := http.Header{}
	// Usa headers de autenticação corretos (Token ou Cookie do Admin)
	authHeaders := tp.containerManager.GetAuthHeaders()
	for k, v := range authHeaders {
		headers.Set(k, v)
	}

	headers.Set("Origin", tp.containerManager.GetNodeOrigin())

	// Conectar ao Proxmox
	proxmoxConn, resp, err := dialer.Dial(wsURL, headers)
	if err != nil {
		log.Printf("Erro ao conectar WebSocket do Proxmox (%s): %v", wsURL, err)
		if resp != nil {
			log.Printf("Status Proxmox: %s", resp.Status)
		}
		clientConn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31m✗ Erro ao conectar ao Proxmox: Bad Handshake ("+err.Error()+")\x1b[0m\r\n"))
		return
	}
	defer proxmoxConn.Close()

	// CRITICAL: Proxmox termproxy expects "username:ticket" as the first message
	// This confirms the connection authorization for the specific ticket
	// And it must be sent as BinaryMessage if using binary subprotocol, effectively just bytes.
	handshake := fmt.Sprintf("%s:%s\n", proxmoxUser, ticket)
	if err := proxmoxConn.WriteMessage(websocket.BinaryMessage, []byte(handshake)); err != nil {
		log.Printf("Erro ao enviar handshake de authenticação para Proxmox: %v", err)
		clientConn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31m✗ Erro de handshake com Proxmox\x1b[0m\r\n"))
		return
	}

	// Consumir a resposta "OK" do Proxmox para não sujar o terminal do usuário
	// O Proxmox envia "OK" se o handshake for aceito.
	_, response, err := proxmoxConn.ReadMessage()
	if err != nil {
		log.Printf("Erro ao ler resposta de handshake do Proxmox: %v", err)
		clientConn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31m✗ Erro ao validar conexao com Proxmox\x1b[0m\r\n"))
		return
	}

	// Debug log
	// log.Printf("Global Terminal Handshake Response: %s", string(response))

	if string(response) != "OK" {
		if !strings.HasPrefix(string(response), "OK") {
			clientConn.WriteMessage(websocket.TextMessage, response)
		} else {
			if len(response) > 2 {
				clientConn.WriteMessage(websocket.TextMessage, response[2:])
			}
		}
	}

	// Pipe bidirecional
	var wg sync.WaitGroup
	wg.Add(2)

	// Browser -> Proxmox (Input)
	go func() {
		defer wg.Done()
		for {
			_, msg, err := clientConn.ReadMessage()
			if err != nil {
				proxmoxConn.Close()
				return
			}

			// Filtrar comandos de resize JSON
			if len(msg) > 0 && msg[0] == '{' {
				var js map[string]interface{}
				if json.Unmarshal(msg, &js) == nil {
					// log.Printf("Ignorando comando resize JSON: %s", string(msg))
					continue
				}
			}

			// Debugar input
			// log.Printf("Enviando input para Proxmox (%d bytes): %s", len(msg), string(msg))

			// IMPORTANTE: Se o subprotocolo é binary, DEVEMOS enviar BinaryMessage
			// Caso contrário o servidor websocket pode fechar a conexão ou ignorar
			if err := proxmoxConn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				// log.Printf("Erro ao escrever no Proxmox: %v", err)
				return
			}
		}
	}()

	// Proxmox -> Browser (Output)
	go func() {
		defer wg.Done()
		for {
			_, msg, err := proxmoxConn.ReadMessage()
			if err != nil {
				clientConn.Close()
				return
			}
			// Envia como TextMessage para o Browser (xterm.js lida bem)
			if err := clientConn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	wg.Wait()
}
