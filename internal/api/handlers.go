package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vega-cloud/internal/auth"
	"vega-cloud/internal/config"
	"vega-cloud/internal/database"
	"vega-cloud/internal/proxmox"
)

// Handlers contém os handlers HTTP
type Handlers struct {
	containerManager *proxmox.ContainerManager
	cfg              *config.Config
}

// NewHandlers cria novos handlers
func NewHandlers(containerManager *proxmox.ContainerManager, cfg *config.Config) *Handlers {
	return &Handlers{
		containerManager: containerManager,
		cfg:              cfg,
	}
}

// === AUTH HANDLERS ===

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UserResponse struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	IsAdmin   bool   `json:"is_admin"`
	IsPremium bool   `json:"is_premium"`
	IsBanned  bool   `json:"is_banned,omitempty"`
	BanReason string `json:"ban_reason,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func (h *Handlers) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handlers) getContainerID(r *http.Request) (int, error) {
	path := r.URL.Path
	// Estilo /api/vm/{id}/...
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return 0, fmt.Errorf("URL inválida")
	}

	// parts[3] deve ser o ID (ex: ["", "api", "vm", "123", "snapshots"])
	return strconv.Atoi(parts[3])
}

func (h *Handlers) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, ErrorResponse{Error: message})
}

func getClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}
	addr := r.RemoteAddr
	if colonIndex := strings.LastIndex(addr, ":"); colonIndex != -1 {
		addr = addr[:colonIndex]
	}
	return addr
}

// Register registra um novo usuário
func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	if len(req.Username) < 3 || len(req.Username) > 20 {
		h.respondError(w, http.StatusBadRequest, "Username deve ter entre 3 e 20 caracteres")
		return
	}
	if len(req.Password) < 6 {
		h.respondError(w, http.StatusBadRequest, "Senha deve ter pelo menos 6 caracteres")
		return
	}

	clientIP := getClientIP(r)
	user, err := auth.Register(req.Username, req.Password, clientIP, h.cfg.Limits.MaxAccountsPerIP)
	if err != nil {
		switch err {
		case auth.ErrUserExists:
			h.respondError(w, http.StatusConflict, "Usuário já existe")
		case auth.ErrTooManyAccounts:
			h.respondError(w, http.StatusForbidden, "Limite de contas por IP atingido (máx: 2)")
		default:
			h.respondError(w, http.StatusInternalServerError, "Erro ao criar conta")
		}
		return
	}

	// Auto-login após registro
	_, sessionID, err := auth.Login(req.Username, req.Password)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao fazer login")
		return
	}

	auth.SetSessionCookie(w, r, sessionID)
	log.Printf("✅ Novo usuário registrado: %s (IP: %s)", req.Username, clientIP)

	h.respondJSON(w, http.StatusCreated, UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		IsAdmin:   user.IsAdmin,
		IsPremium: user.IsPremium,
	})
}

// Login autentica um usuário
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	user, sessionID, err := auth.Login(req.Username, req.Password)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "Usuário ou senha inválidos")
		return
	}

	if user.IsBanned {
		h.respondError(w, http.StatusForbidden, fmt.Sprintf("Conta banida: %s", user.BanReason))
		return
	}

	auth.SetSessionCookie(w, r, sessionID)
	log.Printf("✅ Login: %s", req.Username)

	h.respondJSON(w, http.StatusOK, UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		IsAdmin:   user.IsAdmin,
		IsPremium: user.IsPremium,
	})
}

// Logout encerra a sessão
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	sessionID := auth.GetSessionFromRequest(r)
	if sessionID != "" {
		auth.Logout(sessionID)
	}
	auth.ClearSessionCookie(w, r)
	h.respondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// GetMe retorna informações do usuário logado
func (h *Handlers) GetMe(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	// Conta containers
	count, _ := database.CountContainersByUserID(user.ID)

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"id":             user.ID,
		"username":       user.Username,
		"containers":     count,
		"max_containers": h.cfg.Limits.MaxContainersPerUser,
		"is_admin":       user.IsAdmin,
		"is_premium":     user.IsPremium,
		"is_banned":      user.IsBanned,
		"ban_reason":     user.BanReason,
	})
}

func (h *Handlers) getUser(r *http.Request) *database.User {
	sessionID := auth.GetSessionFromRequest(r)

	if sessionID == "" {
		return nil
	}

	user, err := auth.ValidateSession(sessionID)
	if err != nil {
		return nil
	}
	return user
}

// === CONTAINER HANDLERS ===

type CreateContainerRequest struct {
	Template string `json:"template"`
	Cores    int    `json:"cores"`
	Memory   int    `json:"memory"` // Em MB
}

type ContainerResponse struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	Template      string  `json:"template"`
	State         string  `json:"state"`
	Cores         int     `json:"cores"`
	Memory        int     `json:"memory"`
	IP            string  `json:"ip,omitempty"`
	Password      string  `json:"password,omitempty"`
	User          string  `json:"user"`
	ExternalPort  int     `json:"external_port,omitempty"`
	PublicIP      string  `json:"public_ip,omitempty"`
	CreatedAt     string  `json:"created_at"`
	ExpiresAt     string  `json:"expires_at"`
	TimeRemaining int     `json:"time_remaining"`
	CpuUsage      float64 `json:"cpu_usage"`
	MemoryUsage   int64   `json:"memory_usage"`
}

type TemplateInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func (h *Handlers) listTemplates() []TemplateInfo {
	templates := make([]TemplateInfo, 0, 2)
	if h.cfg.Templates.Ubuntu.OSTemplate != "" {
		templates = append(templates, TemplateInfo{
			ID:          "ubuntu",
			Name:        h.cfg.Templates.Ubuntu.Name,
			Description: h.cfg.Templates.Ubuntu.Description,
		})
	}
	if h.cfg.Templates.Debian.OSTemplate != "" {
		templates = append(templates, TemplateInfo{
			ID:          "debian",
			Name:        h.cfg.Templates.Debian.Name,
			Description: h.cfg.Templates.Debian.Description,
		})
	}
	return templates
}

func (h *Handlers) templateExists(id string) bool {
	for _, tmpl := range h.listTemplates() {
		if tmpl.ID == id {
			return true
		}
	}
	return false
}

// GetTemplates lista templates disponíveis
func (h *Handlers) GetTemplates(w http.ResponseWriter, r *http.Request) {
	h.respondJSON(w, http.StatusOK, h.listTemplates())
}

// CreateContainer cria um novo container
func (h *Handlers) CreateContainer(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	var req CreateContainerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	if req.Template == "" {
		templates := h.listTemplates()
		if len(templates) > 0 {
			req.Template = templates[0].ID
		}
	}

	if !h.templateExists(req.Template) {
		h.respondError(w, http.StatusBadRequest, "Template inválido ou indisponível")
		return
	}

	// Defaults se não especificado
	if req.Cores == 0 {
		req.Cores = 2
	}
	if req.Memory == 0 {
		req.Memory = 2048
	}

	// Define limits based on Premium status
	maxCores := h.cfg.Limits.MaxCoresPerContainer
	maxMemory := h.cfg.Limits.MaxMemoryPerContainer
	maxCoresAccount := h.cfg.Limits.MaxCoresPerAccount
	maxMemoryAccount := h.cfg.Limits.MaxMemoryPerAccount
	ttlSeconds := h.cfg.Reaper.TTLSeconds

	if user.IsPremium {
		maxCores = h.cfg.Limits.PremiumMaxCoresPerContainer
		maxMemory = h.cfg.Limits.PremiumMaxMemoryPerContainer
		maxCoresAccount = h.cfg.Limits.PremiumMaxCoresPerAccount
		maxMemoryAccount = h.cfg.Limits.PremiumMaxMemoryPerAccount
		// Premium users: "Unlimited" time (100 years)
		ttlSeconds = 31536000 * 100
	}

	// Validar range de recursos
	if req.Cores < h.cfg.Limits.MinCoresPerContainer || req.Cores > maxCores {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("vCPUs deve ser entre %d e %d",
			h.cfg.Limits.MinCoresPerContainer, maxCores))
		return
	}
	if req.Memory < h.cfg.Limits.MinMemoryPerContainer || req.Memory > maxMemory {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("RAM deve ser entre %dMB e %dMB",
			h.cfg.Limits.MinMemoryPerContainer, maxMemory))
		return
	}

	// Verifica limite de containers
	count, _ := database.CountContainersByUserID(user.ID)
	if count >= h.cfg.Limits.MaxContainersPerUser {
		h.respondError(w, http.StatusForbidden, "Limite de containers atingido (máx: 3)")
		return
	}

	// Verifica recursos disponíveis na conta
	usedCores, usedMemory, _ := database.GetUserResourceUsage(user.ID)
	if usedCores+req.Cores > maxCoresAccount {
		h.respondError(w, http.StatusForbidden, fmt.Sprintf("Limite de vCPUs da conta excedido (usado: %d, solicitado: %d, máx: %d)",
			usedCores, req.Cores, maxCoresAccount))
		return
	}
	if usedMemory+req.Memory > maxMemoryAccount {
		h.respondError(w, http.StatusForbidden, fmt.Sprintf("Limite de RAM da conta excedido (usado: %dMB, solicitado: %dMB, máx: %dMB)",
			usedMemory, req.Memory, maxMemoryAccount))
		return
	}

	container, err := h.containerManager.CreateContainer(req.Template, user.ID, req.Cores, req.Memory, time.Duration(ttlSeconds)*time.Second)
	if err != nil {
		log.Printf("❌ Erro ao criar container: %v", err)
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	log.Printf("✅ Container %d criado para %s (%d vCPUs, %dMB RAM)", container.CTID, user.Username, req.Cores, req.Memory)

	h.respondJSON(w, http.StatusCreated, ContainerResponse{
		ID:            container.CTID,
		Name:          container.Name,
		Template:      container.Template,
		State:         container.State,
		Cores:         container.Cores,
		Memory:        container.Memory,
		Password:      container.Password,
		User:          "root",
		ExternalPort:  container.ExternalPort,
		PublicIP:      h.cfg.MikroTik.PublicIP,
		CreatedAt:     container.CreatedAt.Format(time.RFC3339),
		ExpiresAt:     container.ExpiresAt.Format(time.RFC3339),
		TimeRemaining: int(time.Until(container.ExpiresAt).Seconds()),
	})
}

// GetMyContainers retorna os containers do usuário
func (h *Handlers) GetMyContainers(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	containers, err := database.GetContainersByUserID(user.ID)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao buscar containers")
		return
	}

	response := make([]ContainerResponse, 0, len(containers))
	for _, ct := range containers {
		resp := ContainerResponse{
			ID:            ct.CTID,
			Name:          ct.Name,
			Template:      ct.Template,
			State:         ct.State,
			IP:            ct.IP,
			Password:      ct.Password,
			User:          "root",
			ExternalPort:  ct.ExternalPort,
			PublicIP:      h.cfg.MikroTik.PublicIP,
			CreatedAt:     ct.CreatedAt.Format(time.RFC3339),
			ExpiresAt:     ct.ExpiresAt.Format(time.RFC3339),
			TimeRemaining: int(time.Until(ct.ExpiresAt).Seconds()),
			Cores:         ct.Cores, // Ensure these fields exist in DB struct too or populated
			Memory:        ct.Memory,
		}

		// Fetch Stats if running
		if ct.State == "running" {
			stats, err := h.containerManager.GetContainerStats(ct.CTID)
			if err == nil {
				resp.CpuUsage = stats.Cpu * 100 // Proxmox usually returns 0.0-1.0 or similar.
				// Actually Proxmox CPU usage is 0.05 for 5%. Wait.
				// If Proxmox returns 0.05, that is 5% of one core? No, it's usually relative to total.
				// Let's assume standard float 0.05 = 5%.
				// Let's check typical Proxmox API output.
				// Usually 'cpu' is percentage (0.00 to 1.00+). E.g. 0.015
				// So *100 gives percentage.
				resp.CpuUsage = stats.Cpu * 100
				resp.MemoryUsage = stats.Mem
			}
		}

		response = append(response, resp)
	}

	h.respondJSON(w, http.StatusOK, response)
}

// GetContainer retorna informações de um container
func (h *Handlers) GetContainer(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	ctidStr := strings.TrimPrefix(r.URL.Path, "/api/vm/")
	ctidStr = strings.Split(ctidStr, "/")[0]
	ctid, err := strconv.Atoi(ctidStr)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Container não encontrado")
		return
	}

	if container.UserID != user.ID && !user.IsAdmin {
		h.respondError(w, http.StatusForbidden, "Acesso negado")
		return
	}

	h.respondJSON(w, http.StatusOK, ContainerResponse{
		ID:            container.CTID,
		Name:          container.Name,
		Template:      container.Template,
		State:         container.State,
		IP:            container.IP,
		Password:      container.Password,
		User:          "root",
		ExternalPort:  container.ExternalPort,
		PublicIP:      h.cfg.MikroTik.PublicIP,
		CreatedAt:     container.CreatedAt.Format(time.RFC3339),
		ExpiresAt:     container.ExpiresAt.Format(time.RFC3339),
		TimeRemaining: int(time.Until(container.ExpiresAt).Seconds()),
	})
}

// ResetTimer reseta o timer de um container
func (h *Handlers) ResetTimer(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/vm/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	ctid, err := strconv.Atoi(parts[0])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Container não encontrado")
		return
	}

	if container.UserID != user.ID && !user.IsAdmin {
		h.respondError(w, http.StatusForbidden, "Acesso negado")
		return
	}

	if err := h.containerManager.ResetTimer(ctid); err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	container, _ = database.GetContainerByCtid(ctid)
	log.Printf("🔄 Timer resetado para container %d", ctid)

	// AUTO-START: Se o container estiver 'stopped', inicia automaticamente
	if container.State == "stopped" {
		go func() {
			log.Printf("🔄 Auto-starting container %d after timer reset...", ctid)
			if err := h.containerManager.StartContainer(ctid); err != nil {
				log.Printf("❌ Falha no auto-start do container %d: %v", ctid, err)
			}
		}()
		// Atualiza estado localmente para refletir na resposta imediata se possível, mas state real vem do Proxmox
		container.State = "running" // Otimista
	}

	h.respondJSON(w, http.StatusOK, ContainerResponse{
		ID:            container.CTID,
		Name:          container.Name,
		Template:      container.Template,
		State:         container.State,
		IP:            container.IP,
		Password:      container.Password,
		User:          "root",
		ExternalPort:  container.ExternalPort,
		PublicIP:      h.cfg.MikroTik.PublicIP,
		CreatedAt:     container.CreatedAt.Format(time.RFC3339),
		ExpiresAt:     container.ExpiresAt.Format(time.RFC3339),
		TimeRemaining: int(time.Until(container.ExpiresAt).Seconds()),
	})
}

// StartContainer inicia um container
func (h *Handlers) StartContainer(w http.ResponseWriter, r *http.Request) {
	h.handlePowerAction(w, r, "start")
}

// StopContainer para um container
func (h *Handlers) StopContainer(w http.ResponseWriter, r *http.Request) {
	h.handlePowerAction(w, r, "stop")
}

// RestartContainer reinicia um container
func (h *Handlers) RestartContainer(w http.ResponseWriter, r *http.Request) {
	h.handlePowerAction(w, r, "restart")
}

func (h *Handlers) handlePowerAction(w http.ResponseWriter, r *http.Request, action string) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	ctidStr := strings.TrimPrefix(r.URL.Path, "/api/vm/")
	ctidStr = strings.Split(ctidStr, "/")[0]
	ctid, err := strconv.Atoi(ctidStr)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Container não encontrado")
		return
	}

	if container.UserID != user.ID && !user.IsAdmin {
		h.respondError(w, http.StatusForbidden, "Acesso negado")
		return
	}

	// RESTRICTION: Power actions allowed for everyone, but subject to expiration check (handled in ContainerManager)
	// if !user.IsPremium && !user.IsAdmin {
	// 	h.respondError(w, http.StatusForbidden, "Recurso exclusivo para usuários Premium")
	// 	return
	// }

	var actionErr error
	switch action {
	case "start":
		actionErr = h.containerManager.StartContainer(ctid)
	case "stop":
		actionErr = h.containerManager.StopContainer(ctid)
	case "restart":
		actionErr = h.containerManager.RestartContainer(ctid)
	default:
		actionErr = fmt.Errorf("ação inválida")
	}

	if actionErr != nil {
		log.Printf("❌ Erro ao executar %s no container %d: %v", action, ctid, actionErr)
		h.respondError(w, http.StatusInternalServerError, actionErr.Error())
		return
	}

	log.Printf("⚡ Ação %s executada com sucesso no container %d por %s", action, ctid, user.Username)
	h.respondJSON(w, http.StatusOK, map[string]string{"status": "success", "action": action})
}

// DeleteContainer deleta um container
func (h *Handlers) DeleteContainer(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	ctidStr := strings.TrimPrefix(r.URL.Path, "/api/vm/")
	ctidStr = strings.Split(ctidStr, "/")[0]
	ctid, err := strconv.Atoi(ctidStr)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Container não encontrado")
		return
	}

	if container.UserID != user.ID && !user.IsAdmin {
		h.respondError(w, http.StatusForbidden, "Acesso negado")
		return
	}

	if err := h.containerManager.DeleteContainer(ctid); err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Printf("🗑️ Container %d deletado por %s", ctid, user.Username)
	h.respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetConsole retorna a URL do terminal
func (h *Handlers) GetConsole(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/vm/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	ctid, err := strconv.Atoi(parts[0])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	container, err := database.GetContainerByCtid(ctid)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Container não encontrado")
		return
	}

	if container.UserID != user.ID {
		h.respondError(w, http.StatusForbidden, "Acesso negado")
		return
	}

	consoleURL, err := h.containerManager.GetConsoleURL(ctid)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Console não disponível")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"console_url": consoleURL})
}

// AccountResourcesResponse representa a resposta de recursos da conta
type AccountResourcesResponse struct {
	MaxCoresPerAccount    int `json:"max_cores_per_account"`
	MaxMemoryPerAccount   int `json:"max_memory_per_account"`
	MaxCoresPerContainer  int `json:"max_cores_per_container"`
	MaxMemoryPerContainer int `json:"max_memory_per_container"`
	MinCoresPerContainer  int `json:"min_cores_per_container"`
	MinMemoryPerContainer int `json:"min_memory_per_container"`
	UsedCores             int `json:"used_cores"`
	UsedMemory            int `json:"used_memory"`
	AvailableCores        int `json:"available_cores"`
	AvailableMemory       int `json:"available_memory"`
}

// GetAccountResources retorna os recursos da conta (limites e uso)
func (h *Handlers) GetAccountResources(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	usedCores, usedMemory, err := database.GetUserResourceUsage(user.ID)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao buscar uso de recursos")
		return
	}

	maxCores := h.cfg.Limits.MaxCoresPerAccount
	maxMemory := h.cfg.Limits.MaxMemoryPerAccount
	maxCoresContainer := h.cfg.Limits.MaxCoresPerContainer
	maxMemoryContainer := h.cfg.Limits.MaxMemoryPerContainer

	if user.IsPremium {
		maxCores = h.cfg.Limits.PremiumMaxCoresPerAccount
		maxMemory = h.cfg.Limits.PremiumMaxMemoryPerAccount
		maxCoresContainer = h.cfg.Limits.PremiumMaxCoresPerContainer
		maxMemoryContainer = h.cfg.Limits.PremiumMaxMemoryPerContainer
	}

	h.respondJSON(w, http.StatusOK, AccountResourcesResponse{
		MaxCoresPerAccount:    maxCores,
		MaxMemoryPerAccount:   maxMemory,
		MaxCoresPerContainer:  maxCoresContainer,
		MaxMemoryPerContainer: maxMemoryContainer,
		MinCoresPerContainer:  h.cfg.Limits.MinCoresPerContainer,
		MinMemoryPerContainer: h.cfg.Limits.MinMemoryPerContainer,
		UsedCores:             usedCores,
		UsedMemory:            usedMemory,
		AvailableCores:        maxCores - usedCores,
		AvailableMemory:       maxMemory - usedMemory,
	})
}

// === FIREWALL HANDLERS ===

type CreatePortForwardRequest struct {
	ContainerID  int    `json:"container_id"`
	InternalPort int    `json:"internal_port"`
	Protocol     string `json:"protocol"`
}

type PortForwardResponse struct {
	ID           int64  `json:"id"`
	ContainerID  int64  `json:"container_id"`
	ExternalPort int    `json:"external_port"`
	InternalPort int    `json:"internal_port"`
	Protocol     string `json:"protocol"`
	CreatedAt    string `json:"created_at"`
}

// CreatePortForward cria uma regra de redirecionamento de porta
func (h *Handlers) CreatePortForward(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	var req CreatePortForwardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	pf, err := h.containerManager.CreatePortForward(user.ID, req.ContainerID, req.InternalPort, req.Protocol)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusCreated, PortForwardResponse{
		ID:           pf.ID,
		ContainerID:  pf.ContainerID,
		ExternalPort: pf.ExternalPort,
		InternalPort: pf.InternalPort,
		Protocol:     pf.Protocol,
		CreatedAt:    pf.CreatedAt.Format(time.RFC3339),
	})
}

// GetPortForwards lista regras de firewall de um container
func (h *Handlers) GetPortForwards(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	containerIDStr := r.URL.Query().Get("container_id")
	containerID, err := strconv.Atoi(containerIDStr)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Container ID inválido")
		return
	}

	pfs, err := h.containerManager.GetPortForwards(user.ID, containerID)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var response []PortForwardResponse
	for _, pf := range pfs {
		response = append(response, PortForwardResponse{
			ID:           pf.ID,
			ContainerID:  pf.ContainerID,
			ExternalPort: pf.ExternalPort,
			InternalPort: pf.InternalPort,
			Protocol:     pf.Protocol,
			CreatedAt:    pf.CreatedAt.Format(time.RFC3339),
		})
	}

	// Retornar array vazio em vez de null
	if response == nil {
		response = []PortForwardResponse{}
	}

	h.respondJSON(w, http.StatusOK, response)
}

// DeletePortForward remove uma regra
func (h *Handlers) DeletePortForward(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/firewall/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	if err := h.containerManager.DeletePortForward(user.ID, id); err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ========== Graphs Handler ==========

func (h *Handlers) GetContainerGraphs(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}
	ctid, err := h.getContainerID(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	timeframe := r.URL.Query().Get("timeframe")
	if timeframe == "" {
		timeframe = "hour"
	}

	data, err := h.containerManager.GetContainerGraphs(user.ID, ctid, timeframe)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, data)
}

// ========== File Manager Handlers ==========

func (h *Handlers) ListFiles(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}
	ctid, err := h.getContainerID(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	path := r.URL.Query().Get("path")
	files, err := h.containerManager.ListFiles(user.ID, ctid, path)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, files)
}

func (h *Handlers) ReadFile(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}
	ctid, err := h.getContainerID(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		h.respondError(w, http.StatusBadRequest, "Path obrigatório")
		return
	}

	content, err := h.containerManager.ReadFile(user.ID, ctid, path)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (h *Handlers) WriteFile(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}
	ctid, err := h.getContainerID(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.Path == "" {
		h.respondError(w, http.StatusBadRequest, "Path obrigatório")
		return
	}

	if err := h.containerManager.WriteFile(user.ID, ctid, req.Path, req.Content); err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// ========== Snapshots Handlers ==========

func (h *Handlers) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}
	ctid, err := h.getContainerID(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	snapshots, err := h.containerManager.ListSnapshots(user.ID, ctid)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, snapshots)
}

func (h *Handlers) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}
	ctid, err := h.getContainerID(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	if req.Name == "" {
		h.respondError(w, http.StatusBadRequest, "Nome obrigatório")
		return
	}

	if err := h.containerManager.CreateSnapshot(user.ID, ctid, req.Name); err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

func (h *Handlers) RestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}
	ctid, err := h.getContainerID(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	path := r.URL.Path
	// /api/vm/{id}/snapshots/{name}/restore
	parts := strings.Split(path, "/")
	if len(parts) < 7 { // ["", "api", "vm", "id", "snapshots", "name", "restore"]
		h.respondError(w, http.StatusBadRequest, "URL inválida")
		return
	}
	name := parts[5]

	if err := h.containerManager.RestoreSnapshot(user.ID, ctid, name); err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

func (h *Handlers) DeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	if user == nil {
		h.respondError(w, http.StatusUnauthorized, "Não autenticado")
		return
	}
	ctid, err := h.getContainerID(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	path := r.URL.Path
	// /api/vm/{id}/snapshots/{name}
	parts := strings.Split(path, "/")
	if len(parts) < 6 { // ["", "api", "vm", "id", "snapshots", "name"]
		h.respondError(w, http.StatusBadRequest, "URL inválida")
		return
	}
	name := parts[5]

	if err := h.containerManager.DeleteSnapshot(user.ID, ctid, name); err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
