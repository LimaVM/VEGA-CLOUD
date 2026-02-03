package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Proxmox   ProxmoxConfig   `yaml:"proxmox"`
	Templates TemplatesConfig `yaml:"templates"`
	Auth      AuthConfig      `yaml:"auth"`
	Limits    LimitsConfig    `yaml:"limits"`
	Reaper    ReaperConfig    `yaml:"reaper"`
	Database  DatabaseConfig  `yaml:"database"`
	MikroTik  MikroTikConfig  `yaml:"mikrotik"`
}

type MikroTikConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"` // Porta da API (padrão 8728)
	User           string `yaml:"user"`
	Password       string `yaml:"password"`
	PublicIP       string `yaml:"public_ip"`
	PortRangeStart int    `yaml:"port_range_start"`
	PortRangeEnd   int    `yaml:"port_range_end"`
}

type ServerConfig struct {
	Port     int    `yaml:"port"`
	Host     string `yaml:"host"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type ProxmoxConfig struct {
	URL         string `yaml:"url"`
	Node        string `yaml:"node"`
	TokenID     string `yaml:"token_id"`
	TokenSecret string `yaml:"token_secret"`
	User        string `yaml:"user"`
	Password    string `yaml:"password"`
	Insecure    bool   `yaml:"insecure"`
	SSHHost     string `yaml:"ssh_host"`
	SSHUser     string `yaml:"ssh_user"`
	SSHPassword string `yaml:"ssh_password"`
}

type TemplateConfig struct {
	OSTemplate  string `yaml:"ostemplate"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type TemplatesConfig struct {
	Ubuntu TemplateConfig `yaml:"ubuntu"`
	Debian TemplateConfig `yaml:"debian"`
}

type AuthConfig struct {
	SessionHours int `yaml:"session_hours"`
	InactiveDays int `yaml:"inactive_days"`
}

type LimitsConfig struct {
	MaxAccountsPerIP     int `yaml:"max_accounts_per_ip"`
	MaxContainersPerUser int `yaml:"max_containers_per_user"`
	// Limites por conta (acumulativo)
	MaxCoresPerAccount  int `yaml:"max_cores_per_account"`
	MaxMemoryPerAccount int `yaml:"max_memory_per_account"`
	// Limites por container (máximo individual)
	MaxCoresPerContainer  int `yaml:"max_cores_per_container"`
	MaxMemoryPerContainer int `yaml:"max_memory_per_container"`
	// Mínimos por container
	MinCoresPerContainer  int `yaml:"min_cores_per_container"`
	MinMemoryPerContainer int `yaml:"min_memory_per_container"`

	// Limites Premium
	PremiumMaxCoresPerAccount    int `yaml:"premium_max_cores_per_account"`
	PremiumMaxMemoryPerAccount   int `yaml:"premium_max_memory_per_account"`
	PremiumMaxCoresPerContainer  int `yaml:"premium_max_cores_per_container"`
	PremiumMaxMemoryPerContainer int `yaml:"premium_max_memory_per_container"`

	// Snapshots limits
	MaxSnapshotsFree    int `yaml:"max_snapshots_free"`
	MaxSnapshotsPremium int `yaml:"max_snapshots_premium"`
}

type ReaperConfig struct {
	TTLSeconds    int `yaml:"ttl_seconds"`
	CheckInterval int `yaml:"check_interval"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Defaults
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 80
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Auth.SessionHours == 0 {
		cfg.Auth.SessionHours = 24
	}
	if cfg.Auth.InactiveDays == 0 {
		cfg.Auth.InactiveDays = 3
	}
	if cfg.Limits.MaxAccountsPerIP == 0 {
		cfg.Limits.MaxAccountsPerIP = 2
	}
	if cfg.Limits.MaxContainersPerUser == 0 {
		cfg.Limits.MaxContainersPerUser = 3
	}
	if cfg.Limits.MaxCoresPerAccount == 0 {
		cfg.Limits.MaxCoresPerAccount = 8
	}
	if cfg.Limits.MaxMemoryPerAccount == 0 {
		cfg.Limits.MaxMemoryPerAccount = 8192
	}
	if cfg.Limits.MaxCoresPerContainer == 0 {
		cfg.Limits.MaxCoresPerContainer = 8
	}
	if cfg.Limits.MaxMemoryPerContainer == 0 {
		cfg.Limits.MaxMemoryPerContainer = 8192
	}
	if cfg.Limits.MinCoresPerContainer == 0 {
		cfg.Limits.MinCoresPerContainer = 1
	}
	if cfg.Limits.MinMemoryPerContainer == 0 {
		cfg.Limits.MinMemoryPerContainer = 512
	}
	if cfg.Reaper.TTLSeconds == 0 {
		cfg.Reaper.TTLSeconds = 7200
	}
	if cfg.Reaper.CheckInterval == 0 {
		cfg.Reaper.CheckInterval = 30
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "vega-cloud.db"
	}

	// Defaults Premium
	if cfg.Limits.PremiumMaxCoresPerAccount == 0 {
		cfg.Limits.PremiumMaxCoresPerAccount = 24
	}
	if cfg.Limits.PremiumMaxMemoryPerAccount == 0 {
		cfg.Limits.PremiumMaxMemoryPerAccount = 16384 // 16GB
	}
	if cfg.Limits.PremiumMaxCoresPerContainer == 0 {
		cfg.Limits.PremiumMaxCoresPerContainer = 24
	}
	if cfg.Limits.PremiumMaxMemoryPerContainer == 0 {
		cfg.Limits.PremiumMaxMemoryPerContainer = 16384 // 16GB
	}
	if cfg.Limits.MaxSnapshotsFree == 0 {
		cfg.Limits.MaxSnapshotsFree = 1
	}
	if cfg.Limits.MaxSnapshotsPremium == 0 {
		cfg.Limits.MaxSnapshotsPremium = 5
	}

	return &cfg, nil
}
