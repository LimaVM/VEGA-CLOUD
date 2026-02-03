# Vega Cloud v9.1.4

🚀 **Plataforma de containers LXC temporários gratuitos** com Ubuntu 24.04 LTS.

## Arquitetura

- **Backend**: Go com API REST
- **Virtualização**: Proxmox VE (LXC)
- **Rede**: Bridge direto via MikroTik (sem VLANs)
- **Automação**: Cloud-init para Windows/Linux

## Funcionalidades

- ✅ Criação de containers via clonagem de templates
- ✅ Geração automática de senhas (cloud-init)
- ✅ Timer de 2 horas com opção de reset
- ✅ "The Reaper" - deleção automática de VMs expiradas
- ✅ Limite de containers por IP
- ✅ Interface web moderna

## Configuração

1. Edite `config.yaml` com suas credenciais do Proxmox:

```yaml
proxmox:
  url: "https://SEU_PROXMOX:8006"
  node: "pve"
  token_id: "root@pam!vega"
  token_secret: "seu-token-aqui"

templates:
  ubuntu:
    ostemplate: "local:vztmpl/ubuntu-24.04-standard_24.04-1_amd64.tar.zst"
    name: "Ubuntu 24.04"
    description: "Standard Edition"
  debian:
    ostemplate: "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    name: "Debian 13"
    description: "Standard (Trixie)"
```

2. Instale dependências e compile:

```bash
go mod tidy
go build -o vega-cloud.exe ./cmd/server
```

3. Execute:

```bash
./vega-cloud.exe
```

4. Acesse: http://localhost:8080

## API Endpoints

| Método | Endpoint               | Descrição             |
|--------|------------------------|-----------------------|
| POST   | `/api/vm/create`       | Criar nova VM         |
| GET    | `/api/vm/my`           | Listar minhas VMs     |
| GET    | `/api/vm/templates`    | Listar templates      |
| GET    | `/api/vm/{id}`         | Status de uma VM      |
| POST   | `/api/vm/{id}/reset`   | Resetar timer         |
| DELETE | `/api/vm/{id}`         | Deletar VM            |
| GET    | `/api/vm/{id}/console` | URL do console VNC    |

## Pré-requisitos no Proxmox

1. **Templates prontos** com Cloud-init/Cloudbase-init
2. **QEMU Guest Agent** instalado nos templates
3. **Token de API** criado em Datacenter > Permissions > API Tokens

---

Powered by Proxmox VE 🖥️
