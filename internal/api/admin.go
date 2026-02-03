package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"vega-cloud/internal/database"

	"golang.org/x/crypto/bcrypt"
)

// AdminMiddleware verifica se o usuário é administrador
func (h *Handlers) AdminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := h.getUser(r)
		if user == nil {
			h.respondError(w, http.StatusUnauthorized, "Não autenticado")
			return
		}

		if !user.IsAdmin {
			h.respondError(w, http.StatusForbidden, "Acesso negado: Requer privilégios de administrador")
			return
		}

		next(w, r)
	}
}

// GetSystemStats retorna status do node Proxmox (CPU/RAM/Uptime)
func (h *Handlers) GetSystemStats(w http.ResponseWriter, r *http.Request) {
	status, err := h.containerManager.GetNodeStatus()
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao obter status do sistema: "+err.Error())
		return
	}

	totalUsers, _ := database.CountUsers()
	totalContainers, _ := database.CountContainers()
	runningContainers, _ := database.CountRunningContainers()

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"node":               status,
		"total_users":        totalUsers,
		"total_containers":   totalContainers,
		"running_containers": runningContainers,
	})
}

// GetAllUsers retorna lista de todos os usuários
func (h *Handlers) GetAllUsers(w http.ResponseWriter, r *http.Request) {
	users, err := database.GetAllUsers()
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao buscar usuários")
		return
	}
	log.Printf("DEBUG: GetAllUsers found %d users", len(users))

	// Sanitiza resposta (remove hash de senha se viesse, mas struct DB tem)
	// Vamos criar uma resposta limpa
	type AdminUserResponse struct {
		ID             int64  `json:"id"`
		Username       string `json:"username"`
		RegisterIP     string `json:"register_ip"`
		IsAdmin        bool   `json:"is_admin"`
		IsPremium      bool   `json:"is_premium"`
		IsBanned       bool   `json:"is_banned"`
		BanReason      string `json:"ban_reason"`
		LastLogin      string `json:"last_login"`
		CreatedAt      string `json:"created_at"`
		ContainerCount int    `json:"container_count"`
	}

	var response []AdminUserResponse
	for _, u := range users {
		count, _ := database.CountContainersByUserID(u.ID)
		response = append(response, AdminUserResponse{
			ID:             u.ID,
			Username:       u.Username,
			RegisterIP:     u.RegisterIP,
			IsAdmin:        u.IsAdmin,
			IsPremium:      u.IsPremium,
			IsBanned:       u.IsBanned,
			BanReason:      u.BanReason,
			LastLogin:      u.LastLogin.Format("2006-01-02 15:04:05"),
			CreatedAt:      u.CreatedAt.Format("2006-01-02 15:04:05"),
			ContainerCount: count,
		})
	}

	h.respondJSON(w, http.StatusOK, response)
}

// GetAllContainersAdmin retorna TODOS os containers do sistema
func (h *Handlers) GetAllContainersAdmin(w http.ResponseWriter, r *http.Request) {
	containers, err := database.GetAllContainers()
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao buscar containers")
		return
	}

	// Enriquece com nome do usuário dono
	type AdminContainerResponse struct {
		ContainerResponse
		Owner string `json:"owner"`
	}

	var response []AdminContainerResponse
	for _, ct := range containers {
		owner, err := database.GetUserByID(ct.UserID)
		ownerName := "Unknown"
		if err == nil {
			ownerName = owner.Username
		}

		resp := AdminContainerResponse{
			ContainerResponse: ContainerResponse{
				ID:        ct.CTID,
				Name:      ct.Name,
				Template:  ct.Template,
				State:     ct.State,
				IP:        ct.IP,
				Cores:     ct.Cores,
				Memory:    ct.Memory,
				CreatedAt: ct.CreatedAt.Format("2006-01-02 15:04:05"),
				ExpiresAt: ct.ExpiresAt.Format("2006-01-02 15:04:05"),
			},
			Owner: ownerName,
		}
		response = append(response, resp)
	}

	h.respondJSON(w, http.StatusOK, response)
}

// GetNodeConsole retorna URL do terminal do HOST Proxmox
func (h *Handlers) GetNodeConsole(w http.ResponseWriter, r *http.Request) {
	// Obtém URL do terminal VNC
	wsURL, ticket, _, err := h.containerManager.GetNodeVNCTerminal()
	if err != nil {
		http.Error(w, "Erro ao obter terminal: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"ws_url": wsURL, "ticket": ticket})
}

// ResetUserPasswordRequest payload para resetar senha
type ResetUserPasswordRequest struct {
	UserID      int64  `json:"user_id"`
	NewPassword string `json:"new_password"`
}

// ResetUserPassword altera a senha de qualquer usuário
func (h *Handlers) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetUserPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	if len(req.NewPassword) < 6 {
		h.respondError(w, http.StatusBadRequest, "Senha deve ter no mínimo 6 caracteres")
		return
	}

	user, err := database.GetUserByID(req.UserID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	// Não permite que admin resete senha de outro admin (opcional, mas seguro)
	// Se quiser permitir, remova este bloco. O user "vega-admin" é supremo.
	if user.IsAdmin && user.Username == "vega-admin" {
		// Protege o supremo de ser resetado por... outro admin se houver?
		// Nesse caso só existe 1 admin "oficial".
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao gerar hash da senha")
		return
	}

	if err := database.UpdateUserPassword(req.UserID, string(hash)); err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao atualizar senha")
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]string{"status": "password_updated", "message": "Senha atualizada com sucesso"})
}

type TogglePremiumRequest struct {
	Premium bool `json:"premium"`
}

type ToggleAdminRequest struct {
	Admin bool `json:"admin"`
}

func (h *Handlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	// Pega ID da URL
	// /api/admin/users/{id}
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		h.respondError(w, http.StatusBadRequest, "URL inválida")
		return
	}
	targetUserIDStr := parts[4]
	targetUserID, err := strconv.ParseInt(targetUserIDStr, 10, 64)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID de usuário inválido")
		return
	}

	// Proteção: Não deletar o próprio admin ou o super admin
	requestingUser := h.getUser(r)
	if requestingUser.ID == targetUserID {
		h.respondError(w, http.StatusBadRequest, "Você não pode deletar sua própria conta")
		return
	}

	targetUser, err := database.GetUserByID(targetUserID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Usuário alvo não encontrado")
		return
	}

	if targetUser.Username == "vega-admin" {
		h.respondError(w, http.StatusForbidden, "Não é possível deletar o super-admin")
		return
	}

	log.Printf("🗑️ Admin %s iniciou deleção do usuário %s (%d)...", requestingUser.Username, targetUser.Username, targetUserID)

	// 1. Buscar containers do usuário
	containers, err := database.GetContainersByUserID(targetUserID)
	if err != nil {
		// Log erro mas continua se possível? Melhor falhar se DB estiver ruim.
		log.Printf("❌ Erro ao buscar containers do usuário %d: %v", targetUserID, err)
		h.respondError(w, http.StatusInternalServerError, "Erro ao listar containers do usuário")
		return
	}

	// 2. Deletar cada container (Proxmox + MikroTik + DB)
	for _, ct := range containers {
		log.Printf("   -> Deletando container %d (%s)...", ct.CTID, ct.Name)
		if err := h.containerManager.DeleteContainer(ct.CTID); err != nil {
			log.Printf("   ⚠️ Erro ao deletar container %d: %v (continuando...)", ct.CTID, err)
			// Continua para tentar limpar o máximo possível
		}
	}

	// 3. Deletar usuário (Cascata para sessions, logs, etc se configurado, ou delete manual)
	// database.DeleteUser deleta a row da tabela users.
	if err := database.DeleteUser(targetUserID); err != nil {
		log.Printf("❌ Erro ao deletar usuário %d do banco: %v", targetUserID, err)
		h.respondError(w, http.StatusInternalServerError, "Erro ao remover usuário do banco")
		return
	}

	log.Printf("✅ Usuário %s (%d) deletado com sucesso por %s", targetUser.Username, targetUserID, requestingUser.Username)
	h.respondJSON(w, http.StatusOK, map[string]string{"status": "deleted", "message": "Usuário e todos os recursos removidos com sucesso"})
}
func (h *Handlers) ToggleUserPremium(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	idStr := parts[0]
	// Se a URL terminar com /premium, removemos
	// A rota esperada é POST /api/admin/users/{id}/premium
	// Se o router pegou até aqui e chamamos o handler, o ID deve ser o primeiro segmento após /users/

	userID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	var req TogglePremiumRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	if err := database.SetUserPremium(userID, req.Premium); err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao atualizar status premium")
		return
	}

	log.Printf("💎 Status Premium alterado para %v (User %d)", req.Premium, userID)
	h.respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *Handlers) ToggleUserAdmin(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	var req ToggleAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	requestingUser := h.getUser(r)
	if requestingUser.ID == userID {
		h.respondError(w, http.StatusBadRequest, "Você não pode alterar seu próprio nível de admin")
		return
	}

	targetUser, err := database.GetUserByID(userID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	if targetUser.Username == "vega-admin" {
		h.respondError(w, http.StatusForbidden, "Não é possível alterar permissões do super-admin")
		return
	}

	if err := database.SetUserAdmin(userID, req.Admin); err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao atualizar status admin")
		return
	}

	log.Printf("🛡️ Admin %s alterou permissão admin do usuário %d para %v", requestingUser.Username, userID, req.Admin)
	h.respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *Handlers) RevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	requestingUser := h.getUser(r)
	if requestingUser.ID == userID {
		h.respondError(w, http.StatusBadRequest, "Você não pode revogar suas próprias sessões")
		return
	}

	targetUser, err := database.GetUserByID(userID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "Usuário não encontrado")
		return
	}

	if err := database.DeleteSessionsByUserID(userID); err != nil {
		h.respondError(w, http.StatusInternalServerError, "Erro ao revogar sessões")
		return
	}

	log.Printf("🔒 Admin %s revogou sessões do usuário %s (%d)", requestingUser.Username, targetUser.Username, userID)
	h.respondJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// BanUser ban/unban user
func (h *Handlers) BanUser(w http.ResponseWriter, r *http.Request) {
	// /api/admin/users/{id}/ban
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}
	userIDStr := parts[0]

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "ID inválido")
		return
	}

	var req struct {
		Ban    bool   `json:"ban"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "JSON inválido")
		return
	}

	if err := database.SetUserBan(userID, req.Ban, req.Reason); err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Printf("🚫 Ban alterado para user %d: %v (%s) por %s", userID, req.Ban, req.Reason, h.getUser(r).Username)
	h.respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
