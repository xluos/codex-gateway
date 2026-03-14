package main

import (
	"context"
	"encoding/json"
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

var accountsStatusProvider = loadAccountsStatus
var accountsStatusStreamer = streamAccountsStatus
var accountPoolFactory = newUpstreamAccountPool
var accountStatusLoader = upstream.LoadAccountStatuses
var accountStatusSaver = upstream.SaveAccountStatuses
var accountStatusProber = probeMissingAccountStatuses
var authLoginRunner = cli.AuthLogin

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
		newAccountsCommand(app),
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

func newAccountsCommand(app *appContext) *cobra.Command {
	accounts := &cobra.Command{
		Use:   "accounts",
		Short: "Inspect upstream OpenAI accounts",
	}
	var jsonOutput bool
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show account quota and availability status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFromFile(app.configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if jsonOutput {
				statuses, err := accountsStatusProvider(cfg)
				if err != nil {
					return fmt.Errorf("init account pool: %w", err)
				}
				encoder := json.NewEncoder(app.stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(statuses)
			}
			return accountsStatusStreamer(cfg, app.stdout)
		},
	}
	statusCmd.Flags().BoolVar(&jsonOutput, "json", false, "print account status as JSON")
	accounts.AddCommand(statusCmd)
	return accounts
}

func streamAccountsStatus(cfg *config.Config, w io.Writer) error {
	pool, err := accountPoolFactory(cfg)
	if err != nil {
		return fmt.Errorf("init account pool: %w", err)
	}
	if pool == nil {
		return nil
	}

	statusPath := upstream.AccountStatusPath(cfg.Runtime.Dir)
	if persisted, err := accountStatusLoader(statusPath); err == nil {
		pool.ApplyPersistedStatuses(persisted)
	} else if !os.IsNotExist(err) {
		return err
	}

	statuses := pool.AccountsStatus()
	for i, status := range statuses {
		if !hasUsableAccountSnapshot(status) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = pool.ProbeAccount(ctx, status.Name)
			cancel()
			for _, updated := range pool.AccountsStatus() {
				if updated.Name == status.Name {
					statuses[i] = updated
					break
				}
			}
		}
		printAccountStatus(w, statuses[i])
	}
	_ = accountStatusSaver(statusPath, statuses)
	return nil
}

func printAccountStatus(w io.Writer, item upstream.AccountStatus) {
	_, _ = fmt.Fprintf(w, "%s  %s  default=%s  mode=%s  priority=%d\n",
		item.Name, item.Status, firstNonEmptyString(item.DefaultModel, "-"), item.Mode, item.Priority)
	_, _ = fmt.Fprintf(w, "  5h  %s %5s  %s\n",
		renderUsageBar(item.Codex5hUsedPercent), formatPercent(item.Codex5hUsedPercent), formatResetTime(item.Codex5hResetAt))
	_, _ = fmt.Fprintf(w, "  7d  %s %5s  %s\n",
		renderUsageBar(item.Codex7dUsedPercent), formatPercent(item.Codex7dUsedPercent), formatResetTime(item.Codex7dResetAt))
	if item.LastError != "" {
		_, _ = fmt.Fprintf(w, "  last_error  %s\n", item.LastError)
	}
}

func loadAccountsStatus(cfg *config.Config) ([]upstream.AccountStatus, error) {
	pool, err := accountPoolFactory(cfg)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, nil
	}

	statusPath := upstream.AccountStatusPath(cfg.Runtime.Dir)
	if persisted, err := accountStatusLoader(statusPath); err == nil {
		pool.ApplyPersistedStatuses(persisted)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	statuses := pool.AccountsStatus()
	if hasMissingAccountSnapshots(statuses) {
		_ = accountStatusProber(pool, statuses)
		statuses = pool.AccountsStatus()
	}
	_ = accountStatusSaver(statusPath, statuses)
	return statuses, nil
}

func setAccountsStatusProviderForTest(provider func(*config.Config) ([]upstream.AccountStatus, error)) func() {
	previous := accountsStatusProvider
	accountsStatusProvider = provider
	return func() {
		accountsStatusProvider = previous
	}
}

func setAccountsStatusStreamerForTest(streamer func(*config.Config, io.Writer) error) func() {
	previous := accountsStatusStreamer
	accountsStatusStreamer = streamer
	return func() {
		accountsStatusStreamer = previous
	}
}

func setAccountPoolFactoryForTest(factory func(*config.Config) (*upstream.OpenAIAccountPool, error)) func() {
	previous := accountPoolFactory
	accountPoolFactory = factory
	return func() {
		accountPoolFactory = previous
	}
}

func setAccountStatusLoaderForTest(loader func(string) ([]upstream.AccountStatus, error)) func() {
	previous := accountStatusLoader
	accountStatusLoader = loader
	return func() {
		accountStatusLoader = previous
	}
}

func setAccountStatusProberForTest(prober func(*upstream.OpenAIAccountPool, []upstream.AccountStatus) error) func() {
	previous := accountStatusProber
	accountStatusProber = prober
	return func() {
		accountStatusProber = previous
	}
}

func setAuthLoginRunnerForTest(runner func(context.Context, *config.Config, io.Reader, io.Writer, string) error) func() {
	previous := authLoginRunner
	authLoginRunner = runner
	return func() {
		authLoginRunner = previous
	}
}

func probeMissingAccountStatuses(pool *upstream.OpenAIAccountPool, statuses []upstream.AccountStatus) error {
	if pool == nil {
		return nil
	}
	var missing []string
	for _, status := range statuses {
		if !hasUsableAccountSnapshot(status) {
			missing = append(missing, status.Name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return pool.ProbeAccounts(ctx, missing)
}

func hasMissingAccountSnapshots(statuses []upstream.AccountStatus) bool {
	for _, status := range statuses {
		if !hasUsableAccountSnapshot(status) {
			return true
		}
	}
	return false
}

func hasUsableAccountSnapshot(status upstream.AccountStatus) bool {
	return status.Codex5hUsedPercent != nil || status.Codex5hResetAt != nil ||
		status.Codex7dUsedPercent != nil || status.Codex7dResetAt != nil
}

func renderUsageBar(percent *float64) string {
	const width = 20
	if percent == nil {
		return strings.Repeat("░", width)
	}
	value := *percent
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	filled := int((value / 100.0) * width)
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatPercent(percent *float64) string {
	if percent == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", *percent)
}

func formatResetTime(resetAt *time.Time) string {
	if resetAt == nil {
		return "reset -"
	}
	delta := time.Until(resetAt.UTC())
	if delta <= 0 {
		return "reset now"
	}
	return "reset in " + humanizeDuration(delta)
}

func humanizeDuration(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	totalMinutes := int(d.Round(time.Minute).Minutes())
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes % (24 * 60)) / 60
	minutes := totalMinutes % 60
	switch {
	case days > 0:
		if hours > 0 {
			return fmt.Sprintf("%dd%dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	case hours > 0:
		if minutes > 0 {
			return fmt.Sprintf("%dh%dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
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
		func() *cobra.Command {
			var account string
			cmd := &cobra.Command{
				Use:   "login",
				Short: "Run OAuth login flow",
				RunE: func(cmd *cobra.Command, args []string) error {
					cfg, err := loadConfigFromFile(app.configPath)
					if err != nil {
						return fmt.Errorf("load config: %w", err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					return authLoginRunner(ctx, cfg, app.stdin, app.stdout, account)
				},
			}
			cmd.Flags().StringVar(&account, "account", "", "oauth account name")
			return cmd
		}(),
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
	upstreamPool, err := newUpstreamAccountPool(cfg)
	if err != nil {
		return fmt.Errorf("init upstream account pool: %w", err)
	}
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	handlerOptions := []handler.Option{
		handler.WithLogger(log.Default()),
		handler.WithDebugDumpHTTP(cfg.Logging.DebugDumpHTTP),
		handler.WithInstanceLabel(addr),
	}
	if upstreamPool != nil {
		handlerOptions = append(
			handlerOptions,
			handler.WithAccountPool(upstreamPool),
			handler.WithAccountStatusPersister(func(statuses []upstream.AccountStatus) error {
				return upstream.SaveAccountStatuses(upstream.AccountStatusPath(cfg.Runtime.Dir), statuses)
			}),
		)
	}
	if cfg.Upstream.Mode == "oauth" {
		handlerOptions = append(handlerOptions, handler.WithCredentialsLoader(oauth.NewStore(cfg.OAuth.CredentialsFile)))
	}
	openAIHandler := handler.NewOpenAIHandler(upstreamClient, handlerOptions...)
	router := httpserver.NewRouter(cfg, openAIHandler)

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
	if cfg == nil {
		return nil, nil
	}
	if len(cfg.EffectiveUpstreams()) > 0 {
		return nil, nil
	}
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

func newUpstreamAccountPool(cfg *config.Config) (*upstream.OpenAIAccountPool, error) {
	if cfg == nil {
		return nil, nil
	}
	effective := cfg.EffectiveUpstreams()
	if len(effective) == 0 {
		return nil, nil
	}
	tokenSources := make(map[string]upstream.AccessTokenSource)
	for _, item := range effective {
		if item.Mode != upstream.ModeOAuth && item.Mode != "password_oauth" {
			continue
		}
		credPath := item.OAuth.CredentialsFile
		if strings.TrimSpace(credPath) == "" {
			credPath = cfg.OAuth.CredentialsFile
		}
		store := oauth.NewStore(credPath)
		flow := oauth.NewFlow(oauth.Config{
			RedirectURI: buildRedirectURI(cfg),
			ClientID:    oauth.DefaultClientID,
		}, nil)
		if item.Mode == "password_oauth" {
			tokenSources[item.Name] = oauth.NewPasswordTokenSource(
				store,
				flow,
				&oauth.HTTPPasswordLoginExecutor{},
				oauth.PasswordLoginRequest{
					Email:       item.Email,
					Password:    item.Password,
					ClientID:    oauth.DefaultClientID,
					RedirectURI: buildRedirectURI(cfg),
				},
				30*time.Second,
			)
			continue
		}
		tokenSources[item.Name] = oauth.NewTokenSource(store, flow, 30*time.Second)
	}
	timeout := time.Duration(cfg.Upstream.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return upstream.NewOpenAIAccountPool(effective, tokenSources, timeout), nil
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
