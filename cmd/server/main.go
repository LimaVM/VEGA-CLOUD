package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"vega-cloud/internal/api"
	"vega-cloud/internal/config"
	"vega-cloud/internal/database"
	"vega-cloud/internal/mikrotik"
	"vega-cloud/internal/proxmox"
	"vega-cloud/internal/reaper"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Banner
	fmt.Println(`
 ╦  ╦┌─┐┌─┐┌─┐  ╔═╗┬  ┌─┐┬ ┬┌┬┐
 ╚╗╔╝├┤ │ ┬├─┤  ║  │  │ ││ │ ││
 ╚╝ └─┘└─┘┴ ┴  ╚═╝┴─┘└─┘└─┘─┴┘
  LXC Container Platform v9.0.0
	`)

	// Carrega configuração
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("❌ Erro ao carregar configuração: %v", err)
	}
	log.Println("✅ Configuração carregada")

	// Inicializa database
	if err := database.Init(cfg.Database.Path); err != nil {
		log.Fatalf("❌ Erro ao inicializar database: %v", err)
	}
	defer database.Close()
	log.Printf("✅ Database: %s", cfg.Database.Path)

	// Conecta ao Proxmox
	proxmoxClient, err := proxmox.NewClient(&cfg.Proxmox)
	if err != nil {
		log.Fatalf("❌ Erro ao conectar ao Proxmox: %v", err)
	}
	log.Printf("✅ Conectado ao Proxmox: %s (node: %s)", cfg.Proxmox.URL, cfg.Proxmox.Node)

	// Inicializa o Container Manager
	containerManager := proxmox.NewContainerManager(proxmoxClient, cfg)

	// Inicializa MikroTik se habilitado
	if cfg.MikroTik.Enabled {
		mk := mikrotik.NewClient(&cfg.MikroTik)
		containerManager.SetMikroTik(mk)
		log.Printf("✅ MikroTik integrado: %s (Public IP: %s)", cfg.MikroTik.Host, cfg.MikroTik.PublicIP)
	}

	log.Println("✅ Container Manager inicializado")

	// Inicia o Reaper
	reaperProcess := reaper.New(containerManager, cfg)
	reaperProcess.Start()

	// Configura o router e servidor HTTP
	router := api.NewRouter(containerManager, cfg)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: router.Handler(),
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("\n🛑 Encerrando servidor...")
		reaperProcess.Stop()
		server.Close()
	}()

	var serverErr error
	if cfg.Server.CertFile != "" && cfg.Server.KeyFile != "" {
		// Modo HTTPS: App na 443, Redirect na 80

		// Inicia servidor de redirecionamento HTTP -> HTTPS em background
		go func() {
			httpRedirect := &http.Server{
				Addr: fmt.Sprintf("%s:80", cfg.Server.Host),
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					newURL := "https://" + r.Host + r.RequestURI
					http.Redirect(w, r, newURL, http.StatusMovedPermanently)
				}),
			}
			log.Printf("🔀 Redirecionamento HTTP iniciado em http://%s:80", cfg.Server.Host)
			if err := httpRedirect.ListenAndServe(); err != http.ErrServerClosed {
				log.Printf("❌ Erro no servidor de redirecionamento HTTP: %v", err)
			}
		}()

		// Servidor principal HTTPS na porta 443
		// Ignora cfg.Server.Port neste modo para garantir padrão HTTPS
		server.Addr = fmt.Sprintf("%s:443", cfg.Server.Host)
		log.Printf("🚀 Servidor HTTPS iniciado em https://%s:443", cfg.Server.Host)
		serverErr = server.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile)
	} else {
		log.Printf("🚀 Servidor iniciado em http://%s", addr)
		serverErr = server.ListenAndServe()
	}

	if serverErr != nil && serverErr != http.ErrServerClosed {
		log.Fatalf("❌ Erro no servidor: %v", serverErr)
	}
}
