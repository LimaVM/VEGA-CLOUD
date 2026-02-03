package mikrotik

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"vega-cloud/internal/config"

	"github.com/go-routeros/routeros"
)

// Client é o cliente para a API do MikroTik (Porta 8728)
type Client struct {
	address   string
	username  string
	password  string
	portStart int
	portEnd   int
	publicIP  string
	usedPorts map[int]bool
	mu        sync.Mutex
}

// NewClient cria um novo cliente MikroTik
func NewClient(cfg *config.MikroTikConfig) *Client {
	port := cfg.Port
	if port == 0 {
		port = 8728
	}

	return &Client{
		address:   fmt.Sprintf("%s:%d", cfg.Host, port),
		username:  cfg.User,
		password:  cfg.Password,
		portStart: cfg.PortRangeStart,
		portEnd:   cfg.PortRangeEnd,
		publicIP:  cfg.PublicIP,
		usedPorts: make(map[int]bool),
	}
}

// connect abre conexão com o MikroTik
func (c *Client) connect() (*routeros.Client, error) {
	return routeros.Dial(c.address, c.username, c.password)
}

// GetFreePort retorna a próxima porta livre
func (c *Client) GetFreePort() (int, error) {
	// Atualiza portas usadas
	conn, err := c.connect()
	if err == nil {
		defer conn.Close()
		reply, err := conn.Run("/ip/firewall/nat/print", "?chain=dstnat", "?.proplist=dst-port")
		if err == nil {
			for _, re := range reply.Re {
				dstPort := re.Map["dst-port"]
				var port int
				fmt.Sscanf(dstPort, "%d", &port)
				if port > 0 {
					c.usedPorts[port] = true
				}
			}
		}
	} else {
		log.Printf("⚠️ Aviso: não conseguiu conectar no MikroTik para listar portas: %v", err)
	}

	rand.Seed(time.Now().UnixNano())
	for i := 0; i < 10000; i++ {
		port := rand.Intn(c.portEnd-c.portStart) + c.portStart
		if !c.usedPorts[port] {
			return port, nil
		}
	}

	return 0, fmt.Errorf("não foi possível encontrar porta livre")
}

// AddNATRule adiciona uma regra DSTNAT
func (c *Client) AddNATRule(externalPort int, containerIP string, containerPort int, protocol, comment string) error {
	conn, err := c.connect()
	if err != nil {
		return fmt.Errorf("erro de conexão: %w", err)
	}
	defer conn.Close()

	_, err = conn.Run(
		"/ip/firewall/nat/add",
		"=chain=dstnat",
		"=action=dst-nat",
		fmt.Sprintf("=protocol=%s", protocol),
		fmt.Sprintf("=dst-address=%s", c.publicIP), // Fix: Restringe ao IP público
		fmt.Sprintf("=dst-port=%d", externalPort),
		fmt.Sprintf("=to-addresses=%s", containerIP),
		fmt.Sprintf("=to-ports=%d", containerPort),
		fmt.Sprintf("=comment=%s", comment),
	)

	if err != nil {
		return fmt.Errorf("erro API: %w", err)
	}

	c.mu.Lock()
	c.usedPorts[externalPort] = true
	c.mu.Unlock()

	log.Printf("🌐 NAT criado: porta %d -> %s:%d/%s (%s)", externalPort, containerIP, containerPort, protocol, comment)
	return nil
}

// CreatePortForwarding cria uma regra de redirecionamento com porta aleatória
func (c *Client) CreatePortForwarding(containerIP string, internalPort int, protocol string, comment string) (int, error) {
	externalPort, err := c.GetFreePort()
	if err != nil {
		return 0, err
	}

	err = c.AddNATRule(externalPort, containerIP, internalPort, protocol, comment)
	if err != nil {
		return 0, err
	}

	return externalPort, nil
}

// DeletePortForwarding remove uma regra por porta externa e protocolo
func (c *Client) DeletePortForwarding(externalPort int, protocol string) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Procura regra com dst-port e protocol específicos
	reply, err := conn.Run("/ip/firewall/nat/print", "=.proplist=.id,dst-port,protocol")
	if err != nil {
		// Fallback
		reply, err = conn.Run("/ip/firewall/nat/print")
		if err != nil {
			return err
		}
	}

	targetPortStr := fmt.Sprintf("%d", externalPort)

	for _, re := range reply.Re {
		p := re.Map["dst-port"]
		proto := re.Map["protocol"]

		if p == targetPortStr && proto == protocol {
			id := re.Map[".id"]
			conn.Run("/ip/firewall/nat/remove", "=.id="+id)

			c.mu.Lock()
			delete(c.usedPorts, externalPort)
			c.mu.Unlock()

			log.Printf("🗑️ Regra NAT removida: %d/%s", externalPort, protocol)
			return nil
		}
	}

	return fmt.Errorf("regra não encontrada")
}

// DeleteNATRule remove uma regra NAT pelo comentário
func (c *Client) DeleteNATRule(comment string) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Lista TODAS as regras DSTNAT (filtro manual é mais seguro contra erros de parser)
	reply, err := conn.Run("/ip/firewall/nat/print", "?chain=dstnat", "?.proplist=.id,comment")
	if err != nil {
		return err
	}

	for _, re := range reply.Re {
		// Verifica se comentário bate
		if re.Map["comment"] == comment {
			id := re.Map[".id"]
			_, err := conn.Run("/ip/firewall/nat/remove", "=.id="+id)
			if err != nil {
				log.Printf("❌ Erro ao remover regra %s: %v", id, err)
				continue
			}
			log.Printf("🗑️ NAT removido: %s", comment)
		}
	}

	return nil
}

// DeleteNATRuleByContainerID remove todas as regras NAT associadas a um CTID
func (c *Client) DeleteNATRuleByContainerID(ctid int) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Lista TODAS as regras NAT (sem queries complexas)
	reply, err := conn.Run("/ip/firewall/nat/print", "=.proplist=.id,comment")
	if err != nil {
		// Fallback sem proplist
		reply, err = conn.Run("/ip/firewall/nat/print")
		if err != nil {
			return err
		}
	}

	searchStr := fmt.Sprintf("(ID: %d)", ctid)
	fallbackStr := fmt.Sprintf("vega-container-%d", ctid)
	fwStr := fmt.Sprintf("vega-fw: %d", ctid)

	for _, re := range reply.Re {
		comment := re.Map["comment"]

		// Verifica se contém o ID ou é o formato antigo ou é regra de firewall
		if strings.Contains(comment, searchStr) || comment == fallbackStr || strings.Contains(comment, fwStr) {
			id := re.Map[".id"]
			_, err := conn.Run("/ip/firewall/nat/remove", "=.id="+id)
			if err != nil {
				log.Printf("❌ Erro ao remover regra NAT %s: %v", id, err)
				continue
			}
			log.Printf("🗑️ NAT removido para container %d (regra: %s)", ctid, comment)
		}
	}

	return nil
}
