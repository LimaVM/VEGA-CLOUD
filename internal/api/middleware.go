package api

import (
	"bufio"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// CORS middleware
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Logging middleware
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer para capturar status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		// Filtro de logs:
		// - Ignora GET 200/304 (acessos comuns, assets, pollings)
		// - Loga todo o resto (POST, PUT, DELETE, Erros 4xx/5xx)
		if r.Method == "GET" && (wrapped.statusCode == http.StatusOK || wrapped.statusCode == http.StatusNotModified) {
			return
		}

		// Loga com emoji baseado no status
		statusIcon := "✅"
		if wrapped.statusCode >= 400 && wrapped.statusCode < 500 {
			statusIcon = "⚠️"
		} else if wrapped.statusCode >= 500 {
			statusIcon = "❌"
		} else if r.Method != "GET" {
			statusIcon = "📝" // Ações de escrita
		}

		log.Printf("%s %s %s %d %v", statusIcon, r.Method, r.URL.Path, wrapped.statusCode, time.Since(start))
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implementa http.Hijacker para suportar WebSocket
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// RateLimiter é um rate limiter simples por IP
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

// NewRateLimiter cria um novo rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}

	// Cleanup goroutine
	go func() {
		for {
			time.Sleep(window)
			rl.cleanup()
		}
	}()

	return rl
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, times := range rl.requests {
		var valid []time.Time
		for _, t := range times {
			if now.Sub(t) < rl.window {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(rl.requests, ip)
		} else {
			rl.requests[ip] = valid
		}
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Remove requests antigos
	var valid []time.Time
	for _, t := range rl.requests[ip] {
		if now.Sub(t) < rl.window {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.limit {
		return false
	}

	rl.requests[ip] = append(valid, now)
	return true
}

// RateLimit middleware
func RateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)

			if !limiter.Allow(ip) {
				// Log desativado para evitar spam
				// log.Printf("⚠️ [RATE LIMIT] IP %s excedeu limite de reqs (Bloqueio temporariamente desativado)", ip)
				// w.Header().Set("Content-Type", "application/json")
				// w.WriteHeader(http.StatusTooManyRequests)
				// w.Write([]byte(`{"error":"Rate limit exceeded. Try again later."}`))
				// return
			}

			next.ServeHTTP(w, r)
		})
	}
}
