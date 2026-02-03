package models

import (
	"sync"
	"time"
)

// VMState representa o estado atual de uma VM
type VMState string

const (
	VMStateCreating VMState = "creating"
	VMStateRunning  VMState = "running"
	VMStateStopped  VMState = "stopped"
	VMStateDeleting VMState = "deleting"
	VMStateError    VMState = "error"
)

// VM representa uma máquina virtual gerenciada pelo Vega Cloud
type VM struct {
	ID         int       `json:"id"`           // VMID no Proxmox
	Name       string    `json:"name"`         // Nome da VM
	Template   string    `json:"template"`     // "ubuntu", "debian", etc
	State      VMState   `json:"state"`        // Estado atual
	IP         string    `json:"ip,omitempty"` // IP da VM (quando disponível)
	Password   string    `json:"password"`     // Senha gerada
	OwnerIP    string    `json:"owner_ip"`     // IP do usuário que criou
	CreatedAt  time.Time `json:"created_at"`   // Quando foi criada
	ExpiresAt  time.Time `json:"expires_at"`   // Quando expira
	ConsoleURL string    `json:"console_url"`  // URL do noVNC
}

// TimeRemaining retorna o tempo restante antes da VM expirar
func (vm *VM) TimeRemaining() time.Duration {
	remaining := time.Until(vm.ExpiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// IsExpired verifica se a VM expirou
func (vm *VM) IsExpired() bool {
	return time.Now().After(vm.ExpiresAt)
}

// VMStore é um armazenamento thread-safe para VMs ativas
type VMStore struct {
	mu   sync.RWMutex
	vms  map[int]*VM
	byIP map[string][]*VM
}

// NewVMStore cria um novo VMStore
func NewVMStore() *VMStore {
	return &VMStore{
		vms:  make(map[int]*VM),
		byIP: make(map[string][]*VM),
	}
}

// Add adiciona uma VM ao store
func (s *VMStore) Add(vm *VM) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vms[vm.ID] = vm
	s.byIP[vm.OwnerIP] = append(s.byIP[vm.OwnerIP], vm)
}

// Get retorna uma VM pelo ID
func (s *VMStore) Get(id int) (*VM, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vm, ok := s.vms[id]
	return vm, ok
}

// Remove remove uma VM do store
func (s *VMStore) Remove(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	vm, ok := s.vms[id]
	if !ok {
		return
	}
	delete(s.vms, id)

	// Remove from byIP
	vms := s.byIP[vm.OwnerIP]
	for i, v := range vms {
		if v.ID == id {
			s.byIP[vm.OwnerIP] = append(vms[:i], vms[i+1:]...)
			break
		}
	}
}

// GetByIP retorna todas as VMs de um IP
func (s *VMStore) GetByIP(ip string) []*VM {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byIP[ip]
}

// CountByIP retorna o número de VMs de um IP
func (s *VMStore) CountByIP(ip string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byIP[ip])
}

// Count retorna o total de VMs
func (s *VMStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.vms)
}

// All retorna todas as VMs
func (s *VMStore) All() []*VM {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*VM, 0, len(s.vms))
	for _, vm := range s.vms {
		result = append(result, vm)
	}
	return result
}

// ResetTimer reseta o timer de uma VM
func (s *VMStore) ResetTimer(id int, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	vm, ok := s.vms[id]
	if !ok {
		return false
	}
	vm.ExpiresAt = time.Now().Add(ttl)
	return true
}
