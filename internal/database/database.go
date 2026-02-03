package database

import (
	"database/sql"
	"log"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// DB é a conexão global com o banco de dados
var DB *sql.DB

// User representa um usuário
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	RegisterIP   string
	IsAdmin      bool
	IsPremium    bool // Novo campo
	IsBanned     bool
	BanReason    string
	LastLogin    time.Time
	CreatedAt    time.Time
}

// Container representa um container no banco
type Container struct {
	ID           int64
	CTID         int
	UserID       int64
	Name         string
	Template     string
	IP           string
	Password     string
	State        string
	Cores        int // vCPUs alocados
	Memory       int // RAM em MB
	ExternalPort int
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// PortForward representa uma regra de redirecionamento de porta
type PortForward struct {
	ID           int64
	ContainerID  int64
	ExternalPort int
	InternalPort int
	Protocol     string // tcp, udp
	CreatedAt    time.Time
}

// Session representa uma sessão de usuário
type Session struct {
	ID        string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Init inicializa o banco de dados
func Init(dbPath string) error {
	var err error
	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}

	// Criar tabelas
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		register_ip TEXT NOT NULL,
		is_admin BOOLEAN DEFAULT FALSE,
		is_premium BOOLEAN DEFAULT FALSE,
		is_banned BOOLEAN DEFAULT FALSE,
		ban_reason TEXT,
		last_login DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS containers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ctid INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		name TEXT,
		template TEXT DEFAULT 'ubuntu',
		ip TEXT,
		password TEXT,
		state TEXT DEFAULT 'creating',
		cores INTEGER DEFAULT 2,
		memory INTEGER DEFAULT 2048,
		external_port INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS port_forwards (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		container_id INTEGER NOT NULL,
		external_port INTEGER NOT NULL,
		internal_port INTEGER NOT NULL,
		protocol TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (container_id) REFERENCES containers(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_containers_user ON containers(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_port_forwards_container ON port_forwards(container_id);
	`

	_, err = DB.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: Add is_admin column if not exists
	var adminColumnExists int
	err = DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='is_admin'").Scan(&adminColumnExists)
	if err == nil && adminColumnExists == 0 {
		log.Println("🔄 Migrando banco: Adicionando coluna is_admin na tabela users...")
		if _, err := DB.Exec("ALTER TABLE users ADD COLUMN is_admin BOOLEAN DEFAULT FALSE"); err != nil {
			log.Printf("❌ Erro na migração: %v", err)
		}
	}

	// Migration: Add is_premium column if not exists
	var premiumColumnExists int
	err = DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='is_premium'").Scan(&premiumColumnExists)
	if err == nil && premiumColumnExists == 0 {
		log.Println("🔄 Migrando banco: Adicionando coluna is_premium na tabela users...")
		if _, err := DB.Exec("ALTER TABLE users ADD COLUMN is_premium BOOLEAN DEFAULT FALSE"); err != nil {
			log.Printf("❌ Erro na migração: %v", err)
		}
	}

	// Migration: Add is_banned column if not exists
	var bannedColumnExists int
	err = DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='is_banned'").Scan(&bannedColumnExists)
	if err == nil && bannedColumnExists == 0 {
		log.Println("🔄 Migrando banco: Adicionando coluna is_banned na tabela users...")
		if _, err := DB.Exec("ALTER TABLE users ADD COLUMN is_banned BOOLEAN DEFAULT FALSE"); err != nil {
			log.Printf("❌ Erro na migração: %v", err)
		}
	}

	// Migration: Add ban_reason column if not exists
	var banReasonColumnExists int
	err = DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='ban_reason'").Scan(&banReasonColumnExists)
	if err == nil && banReasonColumnExists == 0 {
		log.Println("🔄 Migrando banco: Adicionando coluna ban_reason na tabela users...")
		if _, err := DB.Exec("ALTER TABLE users ADD COLUMN ban_reason TEXT"); err != nil {
			log.Printf("❌ Erro na migração: %v", err)
		}
	}

	// Migration: Add template column if not exists
	var templateColumnExists int
	err = DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('containers') WHERE name='template'").Scan(&templateColumnExists)
	if err == nil && templateColumnExists == 0 {
		log.Println("🔄 Migrando banco: Adicionando coluna template na tabela containers...")
		if _, err := DB.Exec("ALTER TABLE containers ADD COLUMN template TEXT DEFAULT 'ubuntu'"); err != nil {
			log.Printf("❌ Erro na migração: %v", err)
		} else {
			if _, err := DB.Exec("UPDATE containers SET template = 'ubuntu' WHERE template IS NULL OR template = ''"); err != nil {
				log.Printf("❌ Erro ao normalizar template: %v", err)
			}
		}
	}

	// Seed Admin User
	const adminUser = "vega-admin"
	existingAdmin, err := GetUserByUsername(adminUser)
	if err != nil {
		// Log erro but continue if it's just not found? No, GetUserByUsername returns error if not found?
		// Usually sql.ErrNoRows.
		// If scanning error occurs (due to NULL), it returns error too.
		// Check if it's ErrNoRows
		if err == sql.ErrNoRows {
			log.Println("👑 Criando usuário admin padrão...")
			hash, _ := bcrypt.GenerateFromPassword([]byte("Juk!ra12"), bcrypt.DefaultCost)
			if _, err := DB.Exec("INSERT INTO users (username, password_hash, register_ip, is_admin, last_login) VALUES (?, ?, ?, ?, ?)", adminUser, string(hash), "127.0.0.1", true, time.Now()); err != nil {
				log.Printf("❌ Erro ao criar admin: %v", err)
			}
		} else {
			// Scan error or other error
			log.Printf("⚠️ Erro ao verificar admin existente: %v (tentando criar mesmo assim...)", err)
			hash, _ := bcrypt.GenerateFromPassword([]byte("Juk!ra12"), bcrypt.DefaultCost)
			// Ignore error if it fails due to unique constraint
			DB.Exec("INSERT INTO users (username, password_hash, register_ip, is_admin, last_login) VALUES (?, ?, ?, ?, ?)", adminUser, string(hash), "127.0.0.1", true, time.Now())
		}
	} else if !existingAdmin.IsAdmin {
		log.Println("👑 Promovendo vega-admin para administrador...")
		if _, err := DB.Exec("UPDATE users SET is_admin = true WHERE id = ?", existingAdmin.ID); err != nil {
			log.Printf("❌ Erro ao promover admin: %v", err)
		}
	}

	log.Println("✅ Database inicializado")
	return nil
}

// Close fecha a conexão com o banco
func Close() {
	if DB != nil {
		DB.Close()
	}
}

// CountUsersByIP conta quantos usuários foram registrados de um IP
func CountUsersByIP(ip string) (int, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM users WHERE register_ip = ?", ip).Scan(&count)
	return count, err
}

// CreateUser cria um novo usuário
func CreateUser(username, passwordHash, ip string) (*User, error) {
	result, err := DB.Exec(
		"INSERT INTO users (username, password_hash, register_ip, is_admin, is_premium, last_login) VALUES (?, ?, ?, ?, ?, ?)",
		username, passwordHash, ip, false, false, time.Now(),
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return &User{
		ID:           id,
		Username:     username,
		PasswordHash: passwordHash,
		RegisterIP:   ip,
		IsAdmin:      false,
		IsPremium:    false,
		LastLogin:    time.Now(),
		CreatedAt:    time.Now(),
	}, nil
}

// GetUserByUsername busca usuário por username
func GetUserByUsername(username string) (*User, error) {
	user := &User{}
	err := DB.QueryRow(
		"SELECT id, username, password_hash, register_ip, is_admin, is_premium, is_banned, COALESCE(ban_reason, ''), last_login, created_at FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.RegisterIP, &user.IsAdmin, &user.IsPremium, &user.IsBanned, &user.BanReason, &user.LastLogin, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// GetUserByID busca usuário por ID
func GetUserByID(id int64) (*User, error) {
	user := &User{}
	err := DB.QueryRow(
		"SELECT id, username, password_hash, register_ip, is_admin, is_premium, is_banned, COALESCE(ban_reason, ''), last_login, created_at FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.RegisterIP, &user.IsAdmin, &user.IsPremium, &user.IsBanned, &user.BanReason, &user.LastLogin, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// UpdateLastLogin atualiza último login
func UpdateLastLogin(userID int64) error {
	_, err := DB.Exec("UPDATE users SET last_login = ? WHERE id = ?", time.Now(), userID)
	return err
}

// DeleteUser deleta um usuário (cascade deleta containers e sessions)
func DeleteUser(userID int64) error {
	_, err := DB.Exec("DELETE FROM users WHERE id = ?", userID)
	return err
}

// GetInactiveUsers retorna usuários inativos há mais de N dias
func GetInactiveUsers(days int) ([]*User, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	rows, err := DB.Query(
		"SELECT id, username, password_hash, register_ip, last_login, created_at FROM users WHERE last_login < ?",
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		user := &User{}
		err := rows.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.RegisterIP, &user.LastLogin, &user.CreatedAt)
		if err != nil {
			continue
		}
		users = append(users, user)
	}
	return users, nil
}

// CreateSession cria uma nova sessão
func CreateSession(sessionID string, userID int64, duration time.Duration) error {
	expiresAt := time.Now().Add(duration)
	_, err := DB.Exec(
		"INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)",
		sessionID, userID, expiresAt,
	)
	return err
}

// GetSession busca uma sessão
func GetSession(sessionID string) (*Session, error) {
	session := &Session{}
	err := DB.QueryRow(
		"SELECT id, user_id, created_at, expires_at FROM sessions WHERE id = ?",
		sessionID,
	).Scan(&session.ID, &session.UserID, &session.CreatedAt, &session.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// DeleteSession deleta uma sessão
func DeleteSession(sessionID string) error {
	_, err := DB.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	return err
}

// DeleteExpiredSessions limpa sessões expiradas
func DeleteExpiredSessions() error {
	_, err := DB.Exec("DELETE FROM sessions WHERE expires_at < ?", time.Now())
	return err
}

// SaveContainer salva um container no banco
func SaveContainer(ct *Container) error {
	if ct.ID == 0 {
		result, err := DB.Exec(
			"INSERT INTO containers (ctid, user_id, name, template, ip, password, state, cores, memory, external_port, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			ct.CTID, ct.UserID, ct.Name, ct.Template, ct.IP, ct.Password, ct.State, ct.Cores, ct.Memory, ct.ExternalPort, ct.ExpiresAt,
		)
		if err != nil {
			return err
		}
		ct.ID, _ = result.LastInsertId()
		return nil
	}

	_, err := DB.Exec(
		"UPDATE containers SET template = ?, ip = ?, state = ?, cores = ?, memory = ?, external_port = ?, expires_at = ? WHERE id = ?",
		ct.Template, ct.IP, ct.State, ct.Cores, ct.Memory, ct.ExternalPort, ct.ExpiresAt, ct.ID,
	)
	return err
}

// GetContainerByCtid busca container por CTID
func GetContainerByCtid(ctid int) (*Container, error) {
	ct := &Container{}
	err := DB.QueryRow(
		"SELECT id, ctid, user_id, name, template, ip, password, state, cores, memory, external_port, created_at, expires_at FROM containers WHERE ctid = ?",
		ctid,
	).Scan(&ct.ID, &ct.CTID, &ct.UserID, &ct.Name, &ct.Template, &ct.IP, &ct.Password, &ct.State, &ct.Cores, &ct.Memory, &ct.ExternalPort, &ct.CreatedAt, &ct.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if ct.Template == "" {
		ct.Template = "ubuntu"
	}
	return ct, nil
}

// GetContainersByUserID busca containers de um usuário
func GetContainersByUserID(userID int64) ([]*Container, error) {
	rows, err := DB.Query(
		"SELECT id, ctid, user_id, name, template, ip, password, state, cores, memory, external_port, created_at, expires_at FROM containers WHERE user_id = ?",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []*Container
	for rows.Next() {
		ct := &Container{}
		err := rows.Scan(&ct.ID, &ct.CTID, &ct.UserID, &ct.Name, &ct.Template, &ct.IP, &ct.Password, &ct.State, &ct.Cores, &ct.Memory, &ct.ExternalPort, &ct.CreatedAt, &ct.ExpiresAt)
		if err != nil {
			continue
		}
		if ct.Template == "" {
			ct.Template = "ubuntu"
		}
		containers = append(containers, ct)
	}
	return containers, nil
}

// CountContainersByUserID conta containers de um usuário
func CountContainersByUserID(userID int64) (int, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM containers WHERE user_id = ?", userID).Scan(&count)
	return count, err
}

// DeleteContainer deleta um container do banco
func DeleteContainer(ctid int) error {
	_, err := DB.Exec("DELETE FROM containers WHERE ctid = ?", ctid)
	return err
}

// GetAllContainers retorna todos os containers
func GetAllContainers() ([]*Container, error) {
	rows, err := DB.Query(
		"SELECT id, ctid, user_id, name, template, ip, password, state, cores, memory, external_port, created_at, expires_at FROM containers",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []*Container
	for rows.Next() {
		ct := &Container{}
		err := rows.Scan(&ct.ID, &ct.CTID, &ct.UserID, &ct.Name, &ct.Template, &ct.IP, &ct.Password, &ct.State, &ct.Cores, &ct.Memory, &ct.ExternalPort, &ct.CreatedAt, &ct.ExpiresAt)
		if err != nil {
			continue
		}
		if ct.Template == "" {
			ct.Template = "ubuntu"
		}
		containers = append(containers, ct)
	}
	return containers, nil
}

// GetExpiredContainers retorna containers expirados
func GetExpiredContainers() ([]*Container, error) {
	rows, err := DB.Query(
		"SELECT id, ctid, user_id, name, template, ip, password, state, cores, memory, external_port, created_at, expires_at FROM containers WHERE expires_at < ? AND state != 'stopped'",
		time.Now(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []*Container
	for rows.Next() {
		ct := &Container{}
		err := rows.Scan(&ct.ID, &ct.CTID, &ct.UserID, &ct.Name, &ct.Template, &ct.IP, &ct.Password, &ct.State, &ct.Cores, &ct.Memory, &ct.ExternalPort, &ct.CreatedAt, &ct.ExpiresAt)
		if err != nil {
			continue
		}
		if ct.Template == "" {
			ct.Template = "ubuntu"
		}
		containers = append(containers, ct)
	}
	return containers, nil
}

// GetUserResourceUsage retorna o total de cores e memória usados por um usuário
func GetUserResourceUsage(userID int64) (int, int, error) {
	var cores, memory int
	err := DB.QueryRow(`
		SELECT COALESCE(SUM(cores), 0), COALESCE(SUM(memory), 0) 
		FROM containers 
		WHERE user_id = ? AND state != 'error'
	`, userID).Scan(&cores, &memory)
	return cores, memory, err
}

// CheckCTIDExists verifica se um CTID já existe no banco
func CheckCTIDExists(ctid int) (bool, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM containers WHERE ctid = ?", ctid).Scan(&count)
	return count > 0, err
}

// CreatePortForward cria uma nova regra de firewall
func CreatePortForward(pf *PortForward) error {
	result, err := DB.Exec(
		"INSERT INTO port_forwards (container_id, external_port, internal_port, protocol) VALUES (?, ?, ?, ?)",
		pf.ContainerID, pf.ExternalPort, pf.InternalPort, pf.Protocol,
	)
	if err != nil {
		return err
	}
	pf.ID, _ = result.LastInsertId()
	return nil
}

// GetPortForwardsByContainerID lista regras de um container
func GetPortForwardsByContainerID(containerID int64) ([]*PortForward, error) {
	rows, err := DB.Query(
		"SELECT id, container_id, external_port, internal_port, protocol, created_at FROM port_forwards WHERE container_id = ?",
		containerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pfs []*PortForward
	for rows.Next() {
		pf := &PortForward{}
		err := rows.Scan(&pf.ID, &pf.ContainerID, &pf.ExternalPort, &pf.InternalPort, &pf.Protocol, &pf.CreatedAt)
		if err != nil {
			continue
		}
		pfs = append(pfs, pf)
	}
	return pfs, nil
}

// GetPortForwardByID busca uma regra por ID
func GetPortForwardByID(id int64) (*PortForward, error) {
	pf := &PortForward{}
	err := DB.QueryRow(
		"SELECT id, container_id, external_port, internal_port, protocol, created_at FROM port_forwards WHERE id = ?",
		id,
	).Scan(&pf.ID, &pf.ContainerID, &pf.ExternalPort, &pf.InternalPort, &pf.Protocol, &pf.CreatedAt)
	if err != nil {
		return nil, err
	}
	return pf, nil
}

// DeletePortForward remove uma regra
func DeletePortForward(id int64) error {
	_, err := DB.Exec("DELETE FROM port_forwards WHERE id = ?", id)
	return err
}

// GetAllUsers retorna todos os usuários cadastrados
func GetAllUsers() ([]*User, error) {
	// Use NULL-safe scanning
	rows, err := DB.Query("SELECT id, username, register_ip, is_admin, is_premium, is_banned, ban_reason, last_login, created_at FROM users ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		user := &User{}
		var banReason sql.NullString
		var lastLogin sql.NullTime

		if err := rows.Scan(&user.ID, &user.Username, &user.RegisterIP, &user.IsAdmin, &user.IsPremium, &user.IsBanned, &banReason, &lastLogin, &user.CreatedAt); err != nil {
			log.Printf("Erro ao scanear usuário ID %d: %v", user.ID, err)
			// Try to continue or return? If we skip, the table stays empty.
			// Let's create a minimal user object if scan fails partially?
			// Actually, standard Scan fails fast. We need safe scanning.
			// If RegisterIP or other NOT NULLs are somehow NULL, it will fail.
			// Let's assume schema enforcement works for NOT NULLs.
			continue
		}

		user.BanReason = banReason.String
		if lastLogin.Valid {
			user.LastLogin = lastLogin.Time
		} else {
			user.LastLogin = time.Time{} // Zero time
		}

		users = append(users, user)
	}
	return users, nil
}

// SetUserPremium atualiza o status premium de um usuário
func SetUserPremium(userID int64, premium bool) error {
	_, err := DB.Exec("UPDATE users SET is_premium = ? WHERE id = ?", premium, userID)
	return err
}

// SetUserAdmin atualiza o status de administrador de um usuário
func SetUserAdmin(userID int64, isAdmin bool) error {
	_, err := DB.Exec("UPDATE users SET is_admin = ? WHERE id = ?", isAdmin, userID)
	return err
}

// SetUserBan atualiza o status de banimento do usuário
func SetUserBan(userID int64, banned bool, reason string) error {
	_, err := DB.Exec("UPDATE users SET is_banned = ?, ban_reason = ? WHERE id = ?", banned, reason, userID)
	return err
}

// UpdateUserPassword atualiza a senha de um usuário
func UpdateUserPassword(userID int64, passwordHash string) error {
	_, err := DB.Exec("UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, userID)
	return err
}

// DeleteSessionsByUserID remove todas as sessões ativas de um usuário
func DeleteSessionsByUserID(userID int64) error {
	_, err := DB.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	return err
}

// IsExternalPortUsed verifica se uma porta externa já está em uso
func IsExternalPortUsed(port int) (bool, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM port_forwards WHERE external_port = ?", port).Scan(&count)
	return count > 0, err
}

// CountUsers retorna o total de usuários cadastrados.
func CountUsers() (int, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

// CountContainers retorna o total de containers cadastrados.
func CountContainers() (int, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM containers").Scan(&count)
	return count, err
}

// CountRunningContainers retorna o total de containers em execução.
func CountRunningContainers() (int, error) {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM containers WHERE state = 'running'").Scan(&count)
	return count, err
}

// DeleteUser deleta um usuário e seus dados (Cascade deve ser handled pelo DB se configurado, mas Proxmox precisa ser manual)
