package main

import (
	"context"
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

	"github.com/spf13/cobra"
)

type appContext struct {
	stdin      io.Reader
	stdout     io.Writer
	configPath string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatalf("%v", err)
	}
}

func run(args []string, stdout io.Writer) error {
	ctx := &appContext{
		stdin:  os.Stdin,
		stdout: stdout,
	}
	cmd := newRootCommand(ctx)
	cmd.SetOut(stdout)
	cmd.SetErr(stdout)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func newRootCommand(app *appContext) *cobra.Command {
	root := &cobra.Command{
		Use:           "codexgateway",
		Short:         "Local OpenAI OAuth gateway",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFromFile(app.configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return serve(app.configPath, cfg)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringVar(&app.configPath, "config", defaultConfigPath(), "path to config file")
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		_ = cli.Help(app.stdout)
	})

	root.AddCommand(
		newCompletionCommand(root),
		newInitCommand(app),
		newServeCommand(app),
		newStartCommand(app),
		newStopCommand(app),
		newRestartCommand(app),
		newDoctorCommand(app),
		newStatusCommand(app),
		newLogsCommand(app),
		newAuthCommand(app),
	)
	return root
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:       "completion [bash|zsh|fish]",
		Short:     "Generate shell completion script",
		ValidArgs: []string{"bash", "zsh", "fish"},
		Args:      cobra.ExactValidArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}
	return cmd
}

func newInitCommand(app *appContext) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize config interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.Init(app.configPath, force, app.stdin, app.stdout)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config")
	return cmd
}

func newServeCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run gateway in foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFromFile(app.configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return serve(app.configPath, cfg)
		},
	}
}

func newStartCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start gateway in background",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFromFile(app.configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return cli.Start(context.Background(), app.configPath, cfg, app.stdout)
		},
	}
}

func newStopCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop background gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFromFile(app.configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return cli.Stop(cfg, app.stdout)
		},
	}
}

func newRestartCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart background gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFromFile(app.configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return cli.Restart(context.Background(), app.configPath, cfg, app.stdout)
		},
	}
}

func newStatusCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show gateway status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFromFile(app.configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return cli.Status(cfg, app.stdout)
		},
	}
}

func newDoctorCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose local gateway environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.Doctor(app.configPath, app.stdout)
		},
	}
}

func newLogsCommand(app *appContext) *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show recent logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFromFile(app.configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return cli.Logs(cfg, app.stdout, lines)
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "number of log lines")
	return cmd
}

func newAuthCommand(app *appContext) *cobra.Command {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Manage OpenAI OAuth credentials",
	}
	auth.AddCommand(
		&cobra.Command{
			Use:   "login",
			Short: "Run OAuth login flow",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadConfigFromFile(app.configPath)
				if err != nil {
					return fmt.Errorf("load config: %w", err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				return cli.AuthLogin(ctx, cfg, app.stdout)
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show OAuth credential status",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadConfigFromFile(app.configPath)
				if err != nil {
					return fmt.Errorf("load config: %w", err)
				}
				return cli.AuthStatus(cfg, app.stdout)
			},
		},
		&cobra.Command{
			Use:   "refresh",
			Short: "Refresh OAuth credential",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadConfigFromFile(app.configPath)
				if err != nil {
					return fmt.Errorf("load config: %w", err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				return cli.AuthRefresh(ctx, cfg, app.stdout)
			},
		},
	)
	return auth
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
