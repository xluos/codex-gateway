package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"openai-local-gateway/internal/cli"
	"openai-local-gateway/internal/config"
	httpserver "openai-local-gateway/internal/http"
	"openai-local-gateway/internal/http/handler"
	"openai-local-gateway/internal/oauth"
	"openai-local-gateway/internal/upstream"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "auth" {
		authFlags := flag.NewFlagSet("auth", flag.ExitOnError)
		configPath := authFlags.String("config", "config.yaml", "path to config file")
		if err := authFlags.Parse(os.Args[3:]); err != nil {
			log.Fatalf("parse auth flags: %v", err)
		}
		cfg, err := loadConfigFromFile(*configPath)
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		switch os.Args[2] {
		case "login":
			if err := cli.AuthLogin(ctx, cfg, os.Stdout); err != nil {
				log.Fatalf("auth login failed: %v", err)
			}
		case "status":
			if err := cli.AuthStatus(cfg, os.Stdout); err != nil {
				log.Fatalf("auth status failed: %v", err)
			}
		case "refresh":
			if err := cli.AuthRefresh(ctx, cfg, os.Stdout); err != nil {
				log.Fatalf("auth refresh failed: %v", err)
			}
		default:
			log.Fatalf("unknown auth command: %s", os.Args[2])
		}
		return
	}

	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := loadConfigFromFile(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	upstreamClient, err := newUpstreamClient(cfg)
	if err != nil {
		log.Fatalf("init upstream client: %v", err)
	}
	handlerOptions := []handler.Option{
		handler.WithLogger(log.Default()),
		handler.WithDebugDumpHTTP(cfg.Logging.DebugDumpHTTP),
	}
	if cfg.Upstream.Mode == "oauth" {
		handlerOptions = append(handlerOptions, handler.WithCredentialsLoader(oauth.NewStore(cfg.OAuth.CredentialsFile)))
	}
	openAIHandler := handler.NewOpenAIHandler(upstreamClient, handlerOptions...)
	router := httpserver.NewRouter(cfg, openAIHandler)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeoutSeconds) * time.Second,
		WriteTimeout:      time.Duration(cfg.Server.WriteTimeoutSeconds) * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("openai-local-gateway listening on http://%s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

func newUpstreamClient(cfg *config.Config) (*upstream.Client, error) {
	timeout := time.Duration(cfg.Upstream.TimeoutSeconds) * time.Second
	switch cfg.Upstream.Mode {
	case "oauth":
		store := oauth.NewStore(cfg.OAuth.CredentialsFile)
		flow := oauth.NewFlow(oauth.Config{
			RedirectURI: buildRedirectURI(cfg),
			ClientID:    oauth.DefaultClientID,
		}, nil)
		tokenSource := oauth.NewTokenSource(store, flow, 30*time.Second)
		return upstream.NewOAuthClient(cfg.Upstream.BaseURL, tokenSource, timeout), nil
	default:
		return upstream.NewClient(cfg.Upstream.BaseURL, cfg.Upstream.APIKey, timeout), nil
	}
}

func buildRedirectURI(cfg *config.Config) string {
	return fmt.Sprintf("http://%s:%d%s", cfg.OAuth.CallbackHost, cfg.OAuth.CallbackPort, cfg.OAuth.CallbackPath)
}

func loadConfigFromFile(path string) (*config.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return config.LoadConfig(file)
}
