package api

import (
	"net/http"
	"strings"
	"time"

	"vega-cloud/internal/config"
	"vega-cloud/internal/proxmox"
)

// Router configura todas as rotas da API
type Router struct {
	mux           *http.ServeMux
	handlers      *Handlers
	terminalProxy *TerminalProxy
	limiter       *RateLimiter
}

// NewRouter cria um novo router
func NewRouter(containerManager *proxmox.ContainerManager, cfg *config.Config) *Router {
	r := &Router{
		mux:           http.NewServeMux(),
		handlers:      NewHandlers(containerManager, cfg),
		terminalProxy: NewTerminalProxy(containerManager, cfg),
		limiter:       NewRateLimiter(30, time.Minute), // 30 requests por minuto
	}

	r.setupRoutes()
	return r
}

func (r *Router) setupRoutes() {
	// Auth routes
	r.mux.HandleFunc("/api/auth/register", r.methodHandler("POST", r.handlers.Register))
	r.mux.HandleFunc("/api/auth/login", r.methodHandler("POST", r.handlers.Login))
	r.mux.HandleFunc("/api/auth/logout", r.methodHandler("POST", r.handlers.Logout))
	r.mux.HandleFunc("/api/auth/me", r.methodHandler("GET", r.handlers.GetMe))

	// Container routes
	r.mux.HandleFunc("/api/vm/create", r.methodHandler("POST", r.handlers.CreateContainer))
	r.mux.HandleFunc("/api/vm/my", r.methodHandler("GET", r.handlers.GetMyContainers))
	r.mux.HandleFunc("/api/vm/templates", r.methodHandler("GET", r.handlers.GetTemplates))
	r.mux.HandleFunc("/api/vm/", r.vmRouteHandler)

	// Account routes
	r.mux.HandleFunc("/api/account/resources", r.methodHandler("GET", r.handlers.GetAccountResources))

	// Firewall routes
	r.mux.HandleFunc("/api/firewall/create", r.methodHandler("POST", r.handlers.CreatePortForward))
	r.mux.HandleFunc("/api/firewall", r.methodHandler("GET", r.handlers.GetPortForwards))
	r.mux.HandleFunc("/api/firewall/", r.firewallRouteHandler)

	// Admin routes
	r.mux.HandleFunc("/api/admin/stats", r.handlers.AdminMiddleware(r.methodHandler("GET", r.handlers.GetSystemStats)))
	// r.mux.HandleFunc("/api/admin/users", r.handlers.AdminMiddleware(r.methodHandler("GET", r.handlers.GetAllUsers))) // Moved into the /api/admin/users/ handler
	r.mux.HandleFunc("/api/admin/containers", r.handlers.AdminMiddleware(r.methodHandler("GET", r.handlers.GetAllContainersAdmin)))
	r.mux.HandleFunc("/api/admin/node-console", r.handlers.AdminMiddleware(r.methodHandler("GET", r.handlers.GetNodeConsole)))
	r.mux.HandleFunc("/api/admin/node-terminal", r.terminalProxy.ServeNodeWS)
	r.mux.HandleFunc("/api/admin/users/", func(w http.ResponseWriter, req *http.Request) {
		// Manual routing for backward compatibility
		path := req.URL.Path

		// /api/admin/users (GET)
		if (path == "/api/admin/users" || path == "/api/admin/users/") && req.Method == "GET" {
			r.handlers.AdminMiddleware(r.handlers.GetAllUsers)(w, req)
			return
		}

		// /api/admin/users/{id} (DELETE)
		if strings.HasPrefix(path, "/api/admin/users/") && req.Method == "DELETE" {
			r.handlers.AdminMiddleware(r.handlers.DeleteUser)(w, req)
			return
		}

		// /api/admin/users/{id}/premium
		if strings.HasPrefix(path, "/api/admin/users/") && strings.HasSuffix(path, "/premium") && req.Method == "POST" {
			r.handlers.AdminMiddleware(r.handlers.ToggleUserPremium)(w, req)
			return
		}
		// /api/admin/users/{id}/admin
		if strings.HasPrefix(path, "/api/admin/users/") && strings.HasSuffix(path, "/admin") && req.Method == "POST" {
			r.handlers.AdminMiddleware(r.handlers.ToggleUserAdmin)(w, req)
			return
		}
		// /api/admin/users/{id}/ban
		if strings.HasPrefix(path, "/api/admin/users/") && strings.HasSuffix(path, "/ban") && req.Method == "POST" {
			r.handlers.AdminMiddleware(r.handlers.BanUser)(w, req)
			return
		}
		// /api/admin/users/{id}/sessions/revoke
		if strings.HasPrefix(path, "/api/admin/users/") && strings.HasSuffix(path, "/sessions/revoke") && req.Method == "POST" {
			r.handlers.AdminMiddleware(r.handlers.RevokeUserSessions)(w, req)
			return
		}
		http.NotFound(w, req)
	})
	r.mux.HandleFunc("/api/admin/reset-password", r.handlers.AdminMiddleware(r.methodHandler("POST", r.handlers.ResetUserPassword)))

	// Serve arquivos estáticos
	fs := http.FileServer(http.Dir("frontend"))
	r.mux.Handle("/", fs)
}

func (r *Router) firewallRouteHandler(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/firewall/")
	if path == "" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if req.Method == "DELETE" {
		r.handlers.DeletePortForward(w, req)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (r *Router) methodHandler(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != method {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(w, req)
	}
}

func (r *Router) vmRouteHandler(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/vm/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// /api/vm/{id}
	if len(parts) == 1 {
		switch req.Method {
		case "GET":
			r.handlers.GetContainer(w, req)
		case "DELETE":
			r.handlers.DeleteContainer(w, req)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/vm/{id}/reset
	if len(parts) == 2 && parts[1] == "reset" && req.Method == "POST" {
		r.handlers.ResetTimer(w, req)
		return
	}

	// /api/vm/{id}/start
	if len(parts) == 2 && parts[1] == "start" && req.Method == "POST" {
		r.handlers.StartContainer(w, req)
		return
	}

	// /api/vm/{id}/stop
	if len(parts) == 2 && parts[1] == "stop" && req.Method == "POST" {
		r.handlers.StopContainer(w, req)
		return
	}

	// /api/vm/{id}/restart
	if len(parts) == 2 && parts[1] == "restart" && req.Method == "POST" {
		r.handlers.RestartContainer(w, req)
		return
	}

	// /api/vm/{id}/console
	if len(parts) == 2 && parts[1] == "console" && req.Method == "GET" {
		r.handlers.GetConsole(w, req)
		return
	}

	// /api/vm/{id}/terminal (WebSocket)
	if len(parts) == 2 && parts[1] == "terminal" {
		r.terminalProxy.ServeWS(w, req)
		return
	}

	// /api/vm/{id}/snapshots (GET, POST)
	if len(parts) == 2 && parts[1] == "snapshots" {
		if req.Method == "GET" {
			r.handlers.ListSnapshots(w, req)
		} else if req.Method == "POST" {
			r.handlers.CreateSnapshot(w, req)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/vm/{id}/snapshots/{name} (DELETE)
	if len(parts) == 3 && parts[1] == "snapshots" {
		if req.Method == "DELETE" {
			r.handlers.DeleteSnapshot(w, req)
			return
		}
	}

	// /api/vm/{id}/snapshots/{name}/restore (POST)
	if len(parts) == 4 && parts[1] == "snapshots" && parts[3] == "restore" {
		if req.Method == "POST" {
			r.handlers.RestoreSnapshot(w, req)
			return
		}
	}

	// /api/vm/{id}/graphs (GET)
	if len(parts) == 2 && parts[1] == "graphs" {
		if req.Method == "GET" {
			r.handlers.GetContainerGraphs(w, req)
			return
		}
	}

	// /api/vm/{id}/files (GET)
	if len(parts) == 2 && parts[1] == "files" {
		if req.Method == "GET" {
			r.handlers.ListFiles(w, req)
			return
		}
	}

	// /api/vm/{id}/files/content (GET, POST)
	if len(parts) == 3 && parts[1] == "files" && parts[2] == "content" {
		if req.Method == "GET" {
			r.handlers.ReadFile(w, req)
		} else if req.Method == "POST" {
			r.handlers.WriteFile(w, req)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

// Handler retorna o handler HTTP com todos os middlewares
func (r *Router) Handler() http.Handler {
	handler := http.Handler(r.mux)
	handler = RateLimit(r.limiter)(handler)
	handler = Logging(handler)
	handler = CORS(handler)
	return handler
}
