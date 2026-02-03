package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"vega-cloud/internal/database"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserExists         = errors.New("usuário já existe")
	ErrInvalidCredentials = errors.New("usuário ou senha inválidos")
	ErrTooManyAccounts    = errors.New("limite de contas por IP atingido")
	ErrNotLoggedIn        = errors.New("não autenticado")
)

const (
	SessionCookieName = "vega_session_v9_1_4"
	SessionDuration   = 24 * time.Hour
)

// Register registra um novo usuário
func Register(username, password, ip string, maxAccountsPerIP int) (*database.User, error) {
	// Verifica limite de contas por IP
	count, err := database.CountUsersByIP(ip)
	if err != nil {
		return nil, err
	}
	if count >= maxAccountsPerIP {
		return nil, ErrTooManyAccounts
	}

	// Verifica se usuário já existe
	_, err = database.GetUserByUsername(username)
	if err == nil {
		return nil, ErrUserExists
	}

	// Hash da senha
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// Cria usuário
	return database.CreateUser(username, string(hash), ip)
}

// Login valida credenciais e cria sessão
func Login(username, password string) (*database.User, string, error) {
	user, err := database.GetUserByUsername(username)
	if err != nil {
		return nil, "", ErrInvalidCredentials
	}

	// Verifica senha
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return nil, "", ErrInvalidCredentials
	}

	// Atualiza último login
	database.UpdateLastLogin(user.ID)

	// Cria sessão
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, "", err
	}

	err = database.CreateSession(sessionID, user.ID, SessionDuration)
	if err != nil {
		return nil, "", err
	}

	return user, sessionID, nil
}

// Logout invalida a sessão
func Logout(sessionID string) error {
	return database.DeleteSession(sessionID)
}

// ValidateSession valida uma sessão e retorna o usuário
func ValidateSession(sessionID string) (*database.User, error) {
	session, err := database.GetSession(sessionID)
	if err != nil {
		return nil, ErrNotLoggedIn
	}

	// Verifica expiração
	if time.Now().After(session.ExpiresAt) {
		database.DeleteSession(sessionID)
		return nil, ErrNotLoggedIn
	}

	// Busca usuário
	user, err := database.GetUserByID(session.UserID)
	if err != nil {
		return nil, ErrNotLoggedIn
	}

	return user, nil
}

// GetSessionFromRequest extrai sessão do request
func GetSessionFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// SetSessionCookie define o cookie de sessão
func SetSessionCookie(w http.ResponseWriter, r *http.Request, sessionID string) {
	secure := true
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(SessionDuration.Seconds()),
		Expires:  time.Now().Add(SessionDuration),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode, // Explicit Lax
	})
}

// ClearSessionCookie remove o cookie de sessão
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	secure := true
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func generateSessionID() (string, error) {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
