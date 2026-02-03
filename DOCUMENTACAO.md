# Vega Cloud - Documentação Técnica Completa

> Plataforma de Containers LXC Temporários & Premium

**Versão**: 9.1.3
**Última Atualização**: 2026-02-03

---

## Índice

1. [Visão Geral](#1-visão-geral)
2. [Arquitetura do Sistema](#2-arquitetura-do-sistema)
3. [Funcionalidades Principais](#3-funcionalidades-principais)
4. [Sistema Premium](#4-sistema-premium-novo)
5. [Painel Administrativo](#5-painel-administrativo)
6. [Snapshots e Backups](#6-snapshots-e-backups-novo)
7. [Gerenciador de Arquivos](#7-gerenciador-de-arquivos-novo)
8. [API REST](#8-api-rest)
9. [Estrutura do Banco de Dados](#9-estrutura-do-banco-de-dados)
10. [Instalação e Configuração](#10-instalação-e-configuração)
11. [Changelog](#11-changelog)

---

## 🚀 Sobre o Projeto

O **Vega Cloud** é uma plataforma de orquestração de containers LXC (Linux Containers) baseada em Proxmox VE. Sua missão é fornecer ambientes de desenvolvimento Linux instantâneos, descartáveis e seguros, com acesso root completo via SSH e Terminal Web.

A versão 8.0.0 consolida o modelo de negócio "Freemium", sistema de banimento, snapshots, gerenciador de arquivos web e melhorias significativas de segurança e UI.

---

## 2. Arquitetura do Sistema

O sistema opera como um monólito modular em Go, integrando-se diretamente às APIs de infraestrutura.

### Stack Tecnológico
*   **Backend**: Go 1.22+ (net/http, gorilla/websocket)
*   **Frontend**: Vanilla JS, CSS3 Moderno (Glassmorphism), Chart.js, xterm.js
*   **Database**: SQLite 3 (Armazenamento local leve e rápido)
*   **Infraestrutura**:
    *   **Hypervisor**: Proxmox VE (LXC)
    *   **Rede**: MikroTik RouterOS (NAT/Firewall)

### Diagrama de Fluxo
```mermaid
graph TD
    User[Usuário] -->|HTTPS| Vega[Vega Server (Go)]
    Vega -->|Auth| SQLite[(Vega DB)]
    Vega -->|LXC Mgmt| Proxmox[Proxmox Cluster]
    Vega -->|Net Rules| MikroTik[MikroTik Gateway]
    Vega -->|WebSocket| Terminal[Proxy SSH/VNC]
```

---

## 3. Funcionalidades Principais

### Para Todos os Usuários
*   **Criação Instantânea**: Containers Ubuntu 24.04 LTS prontos em segundos.
*   **Terminal Web**: Acesso root via navegador com suporte a copy/paste e redimensionamento.
*   **Monitoramento em Tempo Real**: Gráficos de CPU e RAM no dashboard e modal.
*   **Firewall Pessoal**: Abertura de portas TCP/UDP personalizadas via NAT.
*   **Segurança**: Senhas root geradas aleatoriamente, isolamento de recursos.

### Limitações do Plano Free
*   **Recursos**: 4 vCPUs, 2 GB RAM.
*   **Tempo**: Sessões de 2 horas (requer renovação manual).
*   **Snapshots**: Limite de 1 snapshot por container.
*   **Quantidade**: Máximo de 3 containers ativos.

---

## 4. Sistema Premium (NOVO)

A conta Premium remove limitações e oferece recursos de nível empresarial.

### Benefícios Premium
| Recurso | Free | Premium |
| :--- | :--- | :--- |
| **vCPUs** | 4 Cores | **24 Cores (Max)** |
| **RAM** | 2 GB | **16 GB (Max)** |
| **Tempo de Execução** | 2 Horas (Renovável) | **Ilimitado (Sem timer)** |
| **Controle de Energia** | Apenas Reset | **Start / Stop / Restart** |
| **Snapshots** | 1 | **5** |
| **Containers** | 3 | **Ilimitado** (Soft limit no config) |
| **Prioridade** | Normal | Alta (QoS de CPU) |

**Interface Premium**: Usuários Premium veem distintivos "Premium" dourados, o timer é substituído por "Ilimitado" e os seletores de recursos permitem alocar até o máximo do hardware.

---

## 5. Painel Administrativo

Acesso exclusivo para superusuários (`is_admin=true`).

### Funcionalidades do Admin
1.  **Dashboard de Métricas**:
    *   Uptime do servidor, uso de CPU/RAM do Node físico.
    *   Contagem global de containers e usuários.
2.  **Gestão de Usuários**:
    *   Listagem completa com IP, Last Login e Status.
    *   **Toggle Premium**: Botão para dar/remover status Premium instantaneamente.
    *   **Ban/Unban**: Bloqueio de contas com motivo personalizado.
    *   **Reset de Senha**: Redefinição forçada de credenciais.
    *   **Deletar Usuário (NOVO)**: Remoção total da conta e **todos** os recursos associados (Cascade: User -> Containers -> Rules -> Data).
3.  **Terminal do Node**: Acesso SSH root direto ao servidor Proxmox via navegador para manutenção.
4.  **Auditoria**: Visualização de todos os containers ativos no sistema.

---

## 6. Snapshots e Backups (NOVO)

Sistema de versionamento de estado dos containers.

*   **API**: Endpoints para `List`, `Create`, `Restore`, `Delete`.
*   **Funcionamento**: Utiliza a feature de snapshots nativa do LXC/Proxmox.
*   **Limites**: Controlados via código com base no tier do usuário.
*   **UI**: Aba dedicada no modal do container com lista de snapshots e ações rápidas.

---

## 7. Gerenciador de Arquivos (NOVO)

Interface web para manipulação de arquivos dentro do container.

*   **Navegador**: Interface estilo explorer para navegar diretórios.
*   **Editor**: Editor de texto integrado (modal) para arquivos de configuração e scripts.
*   **Backend**: Utiliza execução remota segura para listar e ler/escrever arquivos sem expor SFTP direto.

---

## 8. API REST

### Autenticação
*   `POST /api/auth/register`: Cadastro.
*   `POST /api/auth/login`: Login (retorna Cookie, `Secure: false` em dev/v8).
*   `POST /api/auth/logout`: Logout.
*   `GET /api/auth/me`: Dados do usuário logado (inclui `is_banned`, `is_premium`).

### Máquinas Virtuais (Containers)
*   `GET /api/vm/my`: Lista containers do usuário.
*   `POST /api/vm/create`: Cria container (payload: `cores`, `memory`, `template`).
*   `GET /api/vm/templates`: Lista templates disponíveis para criação.
*   `DELETE /api/vm/{id}`: Destrói container.
*   `POST /api/vm/{id}/reset`: Renova timer (+2h).
*   `POST /api/vm/{id}/start` (Premium): Inicia container.
*   `POST /api/vm/{id}/stop` (Premium): Para container.
*   `POST /api/vm/{id}/restart` (Premium): Reinicia container.
*   `GET /api/vm/{id}/console`: Informações de conexão.
*   `WS /api/vm/{id}/terminal`: WebSocket para terminal web.

### Recursos Estendidos
*   `GET /api/vm/{id}/graphs`: Dados históricos de CPU/RAM.
*   `GET /api/vm/{id}/files`: Lista arquivos em um caminho.
*   `GET/POST /api/vm/{id}/files/content`: Lê ou grava conteúdo de arquivo.
*   `GET/POST/DELETE /api/vm/{id}/snapshots*`: Gestão de snapshots.

### Admin (Protegido)
*   `GET /api/admin/stats`: Status do Node.
*   `GET /api/admin/users`: Lista usuários.
*   `DELETE /api/admin/users/{id}`: Deleta usuário e recursos.
*   `POST /api/admin/users/{id}/premium`: Toggle Premium.
*   `POST /api/admin/users/{id}/ban`: Banir usuário.
*   `WS /api/admin/node-terminal`: SSH do Host.

---

## 9. Estrutura do Banco de Dados

Arquivo: `vega-cloud.db`

### Tabela `users`
| Coluna | Tipo | Descrição |
| :--- | :--- | :--- |
| `id` | INTEGER PK | Identificador único |
| `username` | TEXT | Nome de usuário (único) |
| `password_hash` | TEXT | Hash bcrypt da senha |
| `register_ip` | TEXT | IP de registro |
| `is_admin` | BOOL | Flag de superusuário |
| **`is_premium`** | BOOL | Flag de conta Premium |
| **`is_banned`** | BOOL | Flag de banimento |
| **`ban_reason`** | TEXT | Motivo do banimento |
| `last_login` | DATETIME | Último acesso |

### Tabela `containers`
| Coluna | Tipo | Descrição |
| :--- | :--- | :--- |
| `id` | INTEGER PK | ID interno |
| `ctid` | INTEGER | ID no Proxmox (vmid) |
| `user_id` | INTEGER FK | Dono do container |
| `template` | TEXT | Template base (ex: ubuntu, debian) |
| `state` | TEXT | creating, running, stopped |
| `cores` | INTEGER | Núcleos alocados |
| `memory` | INTEGER | RAM alocada (MB) |
| `expires_at` | DATETIME | Data de expiração automática |

### Tabela `port_forwards`
| Coluna | Tipo | Descrição |
| :--- | :--- | :--- |
| `container_id` | INTEGER PK | Container alvo |
| `internal_port` | INTEGER | Porta no container (ex: 80) |
| `external_port` | INTEGER | Porta pública (ex: 10543) |
| `protocol` | TEXT | tcp, udp |

---

## 10. Instalação e Configuração

### Requisitos
*   Servidor Linux com Proxmox VE 7/8.
*   MikroTik RouterOS (para NAT 1:1 ou Port Forwarding avançado).
*   Golang 1.22+.

### Configuração (`config.yaml`)
```yaml
server:
  port: 80
  # Caminhos para SSL/TLS (Opcional em v8 para testes)
  cert_file: "path/to/cert.pem"
  key_file: "path/to/key.pem"

proxmox:
  url: "https://pve-host:8006"
  node: "pve1"
  token_id: "root@pam!vega"
  token_secret: "uuid..."

mikrotik:
  enabled: true
  host: "192.168.1.1"
  user: "admin"
  password: "password"
  public_ip: "200.x.x.x"

premium:
  max_cores: 24
  max_memory_mb: 16384
```

### Build e Deploy
```bash
# Compilar
go build -o vega-server.exe ./cmd/server/main.go

# Executar
./vega-server.exe
```

---

## 11. Changelog

### v8.0.0 (2026-02-03)
*   **Admin Delete User**: Funcionalidade completa para deletar usuários e limpar automaticamente todos os recursos associados (VMs, regras de firewall, dados).
*   **Login Loop Fix**: Alteração na política de Cookies para `Secure: false` e `SameSite: Default` para garantir compatibilidade em ambientes mistos (HTTP/HTTPS) e resolver loops de login. O cookie foi renomeado para `vega_session_v8` para forçar logout de sessões antigas.
*   **Reaper Fix**: Correção no loop de "Recompensa" (Reaper) que travava ao tentar parar containers que já estavam desligados.
*   **Snapshots**: Correção crítica nos botões de Restaurar e Deletar que não funcionavam devido a erro de roteamento.

### v7.0.0
*   Consolidação inicial do modelo Freemium.
*   Adição de botões Start/Stop/Restart para usuários Premium.
