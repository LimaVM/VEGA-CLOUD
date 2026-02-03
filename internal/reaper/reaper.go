package reaper

import (
	"log"
	"strings"
	"time"

	"vega-cloud/internal/config"
	"vega-cloud/internal/database"
	"vega-cloud/internal/proxmox"
)

// Reaper é o processo que deleta containers expirados e contas inativas
type Reaper struct {
	containerManager *proxmox.ContainerManager
	cfg              *config.Config
	checkInterval    time.Duration
	stopChan         chan struct{}
}

// New cria um novo Reaper
func New(containerManager *proxmox.ContainerManager, cfg *config.Config) *Reaper {
	return &Reaper{
		containerManager: containerManager,
		cfg:              cfg,
		checkInterval:    time.Duration(cfg.Reaper.CheckInterval) * time.Second,
		stopChan:         make(chan struct{}),
	}
}

// Start inicia o Reaper em background
func (r *Reaper) Start() {
	go r.run()
	log.Printf("☠️  The Reaper iniciado (intervalo: %v, inatividade: %d dias)",
		r.checkInterval, r.cfg.Auth.InactiveDays)
}

// Stop para o Reaper
func (r *Reaper) Stop() {
	close(r.stopChan)
}

func (r *Reaper) run() {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.reapContainers()
			r.reapInactiveUsers()
			r.cleanupSessions()
		case <-r.stopChan:
			log.Println("☠️  The Reaper encerrado")
			return
		}
	}
}

// reapContainers deleta containers expirados
func (r *Reaper) reapContainers() {
	containers, err := database.GetExpiredContainers()
	if err != nil {
		log.Printf("☠️  Erro ao buscar containers expirados: %v", err)
		return
	}

	for _, ct := range containers {
		log.Printf("☠️  Reaping container %d (%s) - expirou em %v", ct.CTID, ct.Name, ct.ExpiresAt)

		// Apenas PARA o container, não deleta (o plano Free para, o Reaper expira para)
		// Wait, the documentation said "The Reaper - deleção automática".
		// But here it seems to just Stop. "reapContainers deleta containers expirados" comment says delete.
		// However, line 70 says "Apenas PARA o container, não deleta".
		// In version 5.0.0/7.0.0, the "Reaper" usually deletes.
		// BUT verify if the logic is INTENDED to DELETE or just STOP.
		// "Timer de 2 horas com opção de reset" -> implies Stop.
		// If it was supposed to DELETE, it would call DeleteContainer.
		// Given the logs loop, we fix the loop first.
		err := r.containerManager.StopContainer(ct.CTID)
		if err != nil {
			// Se já estiver parado, o erro pode conter "not running"
			// Nesse caso, consideramos sucesso para atualizar o DB.
			// Precisamos checar a string do erro.
			// Simplest approach: check error string.
			if logStr := err.Error(); logStr != "" { // just to use err
				// Proxmox return "CT <id> not running" usually
				// We will verify this with client.go view if needed, but user log confirms "CT 836248 not running"
			}

			// Treat "not running" as success
			// Or just swallow error if we want to force update DB
			// But let's be safe.
		}

		// Actually, looking at previous code:
		// if err := ...; err != nil { return } else { save }
		// The return was missing but it didn't save.

		// Correct logic:
		// Try stop. If fails with "not running", assume stopped. If other error, log retry next time.
		// Using string check:
		if err != nil && !strings.Contains(err.Error(), "not running") {
			log.Printf("☠️  Erro ao parar container %d: %v", ct.CTID, err)
			continue // Next container, this one remains for next tick
		}

		log.Printf("☠️  Container %d parado por expiração (ou já estava parado)", ct.CTID)
		// Atualiza estado no banco para 'stopped' para não ser pego novamente na query
		ct.State = "stopped"
		database.SaveContainer(ct)
	}
}

// reapInactiveUsers deleta usuários inativos há mais de N dias
func (r *Reaper) reapInactiveUsers() {
	users, err := database.GetInactiveUsers(r.cfg.Auth.InactiveDays)
	if err != nil {
		log.Printf("☠️  Erro ao buscar usuários inativos: %v", err)
		return
	}

	for _, user := range users {
		log.Printf("☠️  Reaping usuário %s - inativo desde %v", user.Username, user.LastLogin)

		// Primeiro deleta os containers do usuário
		containers, _ := database.GetContainersByUserID(user.ID)
		for _, ct := range containers {
			log.Printf("☠️  Deletando container %d do usuário %s", ct.CTID, user.Username)
			r.containerManager.DeleteContainer(ct.CTID)
		}

		// Depois deleta o usuário (cascade deleta sessões)
		if err := database.DeleteUser(user.ID); err != nil {
			log.Printf("☠️  Erro ao deletar usuário %s: %v", user.Username, err)
		} else {
			log.Printf("☠️  Usuário %s e seus dados deletados", user.Username)
		}
	}
}

// cleanupSessions limpa sessões expiradas
func (r *Reaper) cleanupSessions() {
	if err := database.DeleteExpiredSessions(); err != nil {
		log.Printf("☠️  Erro ao limpar sessões: %v", err)
	}
}
