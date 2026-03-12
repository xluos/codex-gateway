package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"codex-gateway/internal/cli"
	"codex-gateway/internal/config"
	httpserver "codex-gateway/internal/http"
	"codex-gateway/internal/http/handler"
	"codex-gateway/internal/oauth"
	"codex-gateway/internal/upstream"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatalf("%v", err)
	}
}

func run(args []string, stdout io.Writer) error {
	command, commandArgs := parseCommand(args)
	switch command {
	case "help":
		return cli.Help(stdout)
	case "auth":
		return runAuth(commandArgs)
	case "start":
		return runStart(commandArgs, stdout)
	case "stop":
		return runStop(commandArgs, stdout)
	case "restart":
		return runRestart(commandArgs, stdout)
	case "status":
		return runStatus(commandArgs, stdout)
	case "logs":
		return runLogs(commandArgs, stdout)
	case "serve":
		return runServe(commandArgs)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func parseCommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "serve", nil
	}
	first := args[0]
	switch first {
	case "help", "-h", "--help":
		return "help", args[1:]
	case "auth", "serve", "start", "stop", "restart", "status", "logs":
		return first, args[1:]
	default:
		if strings.HasPrefix(first, "-") {
			return "serve", args
		}
		return first, args[1:]
	}
}

func runAuth(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing auth subcommand")
	}
	authFlags := flag.NewFlagSet("auth", flag.ContinueOnError)
	authFlags.SetOutput(io.Discard)
	configPath := authFlags.String("config", defaultConfigPath(), "path to config file")
	if err := authFlags.Parse(args[1:]); err != nil {
		return fmt.Errorf("parse auth flags: %w", err)
	}
	cfg, err := loadConfigFromFile(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	switch args[0] {
	case "login":
		return cli.AuthLogin(ctx, cfg, os.Stdout)
	case "status":
		return cli.AuthStatus(cfg, os.Stdout)
	case "refresh":
		return cli.AuthRefresh(ctx, cfg, os.Stdout)
	default:
		return fmt.Errorf("unknown auth command: %s", args[0])
	}
}

func runServe(args []string) error {
	serveFlags := flag.NewFlagSet("serve", flag.ContinueOnError)
	serveFlags.SetOutput(io.Discard)
	configPath := serveFlags.String("config", defaultConfigPath(), "path to config file")
	if err := serveFlags.Parse(args); err != nil {
		return fmt.Errorf("parse serve flags: %w", err)
	}
	cfg, err := loadConfigFromFile(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	return serve(*configPath, cfg)
}

func runStart(args []string, stdout io.Writer) error {
	startFlags := flag.NewFlagSet("start", flag.ContinueOnError)
	startFlags.SetOutput(io.Discard)
	configPath := startFlags.String("config", defaultConfigPath(), "path to config file")
	if err := startFlags.Parse(args); err != nil {
		return fmt.Errorf("parse start flags: %w", err)
	}
	cfg, err := loadConfigFromFile(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	return cli.Start(context.Background(), *configPath, cfg, stdout)
}

func runStop(args []string, stdout io.Writer) error {
	stopFlags := flag.NewFlagSet("stop", flag.ContinueOnError)
	stopFlags.SetOutput(io.Discard)
	configPath := stopFlags.String("config", defaultConfigPath(), "path to config file")
	if err := stopFlags.Parse(args); err != nil {
		return fmt.Errorf("parse stop flags: %w", err)
	}
	cfg, err := loadConfigFromFile(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	return cli.Stop(cfg, stdout)
}

func runRestart(args []string, stdout io.Writer) error {
	restartFlags := flag.NewFlagSet("restart", flag.ContinueOnError)
	restartFlags.SetOutput(io.Discard)
	configPath := restartFlags.String("config", defaultConfigPath(), "path to config file")
	if err := restartFlags.Parse(args); err != nil {
		return fmt.Errorf("parse restart flags: %w", err)
	}
	cfg, err := loadConfigFromFile(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	return cli.Restart(context.Background(), *configPath, cfg, stdout)
}

func runStatus(args []string, stdout io.Writer) error {
	statusFlags := flag.NewFlagSet("status", flag.ContinueOnError)
	statusFlags.SetOutput(io.Discard)
	configPath := statusFlags.String("config", defaultConfigPath(), "path to config file")
	if err := statusFlags.Parse(args); err != nil {
		return fmt.Errorf("parse status flags: %w", err)
	}
	cfg, err := loadConfigFromFile(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	return cli.Status(cfg, stdout)
}

func runLogs(args []string, stdout io.Writer) error {
	logsFlags := flag.NewFlagSet("logs", flag.ContinueOnError)
	logsFlags.SetOutput(io.Discard)
	configPath := logsFlags.String("config", defaultConfigPath(), "path to config file")
	lines := logsFlags.Int("n", 100, "number of log lines")
	if err := logsFlags.Parse(args); err != nil {
		return fmt.Errorf("parse logs flags: %w", err)
	}
	cfg, err := loadConfigFromFile(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	return cli.Logs(cfg, stdout, *lines)
}

func serve(configPath string, cfg *config.Config) error {
	logFile, err := configureLogging(cfg)
	if err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}
	if logFile != nil {
		defer logFile.Close()
	}

	upstreamClient, err := newUpstreamClient(cfg)
	if err != nil {
		return fmt.Errorf("init upstream client: %w", err)
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

	if err := cli.WriteRuntimeState(cfg, configPath, addr, os.Getpid()); err != nil {
		return fmt.Errorf("write runtime state: %w", err)
	}
	defer func() {
		if err := cli.RemoveRuntimeState(cfg); err != nil {
			log.Printf("remove runtime state failed: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errorsIsServerClosed(err) {
			log.Printf("server shutdown failed: %v", err)
		}
	}()

	log.Printf("codex-gateway listening on http://%s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server failed: %w", err)
	}
	log.Printf("codex-gateway stopped")
	return nil
}

func configureLogging(cfg *config.Config) (*os.File, error) {
	if err := os.MkdirAll(cfg.Runtime.Dir, 0o755); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(cfg.Runtime.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	writers := []io.Writer{logFile}
	if os.Getenv(cli.BackgroundStdoutEnvVar()) != "0" {
		writers = append(writers, os.Stdout)
	}
	log.SetOutput(io.MultiWriter(writers...))
	log.SetFlags(log.LstdFlags)
	return logFile, nil
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
	file, err := os.Open(expandCLIPath(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return config.LoadConfig(file)
}

func errorsIsServerClosed(err error) bool {
	return err == http.ErrServerClosed
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "config.yaml"
	}
	return filepath.Join(home, ".codex-gateway", "config.yaml")
}

func expandCLIPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return trimmed
	}
	if strings.HasPrefix(trimmed, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, trimmed[2:])
		}
	}
	return trimmed
}
