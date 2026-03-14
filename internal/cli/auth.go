package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"codex-gateway/internal/config"
	"codex-gateway/internal/oauth"
	"codex-gateway/internal/ui"
)

var errAuthLoginCanceled = errors.New("auth login canceled")
var authLoginExecutor = runAuthLoginFlow

type OAuthAccountStatus struct {
	Name            string
	DefaultModel    string
	CredentialsFile string
	Status          string
	Email           string
	ExpiresAt       *time.Time
}

func AuthStatus(cfg *config.Config, out io.Writer) error {
	store := oauth.NewStore(cfg.OAuth.CredentialsFile)
	cred, err := store.Load()
	if err != nil {
		return err
	}
	ui.PrintLines(out,
		ui.Banner(out),
		ui.Section("OAuth 凭证"),
		ui.KV("邮箱", cred.Email),
		ui.KV("套餐", cred.PlanType),
		ui.KV("过期时间", time.Unix(cred.ExpiresAt, 0).Format(time.RFC3339)),
		ui.KV("凭证文件", cfg.OAuth.CredentialsFile),
	)
	return nil
}

func AuthRefresh(ctx context.Context, cfg *config.Config, out io.Writer) error {
	store := oauth.NewStore(cfg.OAuth.CredentialsFile)
	cred, err := store.Load()
	if err != nil {
		return err
	}
	flow := oauth.NewFlow(oauth.Config{
		RedirectURI: buildRedirectURI(cfg),
		ClientID:    oauth.DefaultClientID,
	}, nil)
	refreshed, err := flow.RefreshToken(ctx, cred.RefreshToken, cred.ClientID)
	if err != nil {
		return err
	}
	if err := store.Save(refreshed); err != nil {
		return err
	}
	ui.PrintLines(out, ui.Success(fmt.Sprintf("刷新成功：%s", refreshed.Email)))
	return nil
}

func AuthLogin(ctx context.Context, cfg *config.Config, in io.Reader, out io.Writer, accountName string) error {
	selectedCfg, _, err := selectOAuthLoginTarget(cfg, in, out, accountName)
	if err != nil {
		return err
	}
	return authLoginExecutor(ctx, selectedCfg, out)
}

func runAuthLoginFlow(ctx context.Context, cfg *config.Config, out io.Writer) error {
	callback := oauth.NewCallbackServer(callbackAddress(cfg), cfg.OAuth.CallbackPath)
	resultCh, err := callback.Start(ctx)
	if err != nil {
		return err
	}

	flow := oauth.NewFlow(oauth.Config{
		RedirectURI: buildRedirectURI(cfg),
		ClientID:    oauth.DefaultClientID,
	}, nil)
	session, err := flow.GenerateAuthURL()
	if err != nil {
		return err
	}
	ui.PrintLines(out, ui.Banner(out), ui.Section("OAuth 登录"), ui.KV("授权链接", session.AuthURL))

	if cfg.OAuth.AutoOpenBrowser {
		if err := openBrowser(session.AuthURL); err != nil {
			ui.PrintLines(out, ui.Warn("自动打开浏览器失败，请手动访问上面的授权链接。"))
		}
	} else {
		ui.PrintLines(out, ui.Muted("请在浏览器中打开上面的授权链接。"))
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-resultCh:
		if result.Err != "" {
			return fmt.Errorf("oauth callback returned error: %s", result.Err)
		}
		cred, err := flow.ExchangeCode(ctx, session, result.Code, result.State)
		if err != nil {
			return err
		}
		store := oauth.NewStore(cfg.OAuth.CredentialsFile)
		if err := store.Save(cred); err != nil {
			return err
		}
		ui.PrintLines(out, ui.Success(fmt.Sprintf("OAuth 登录成功：%s", cred.Email)))
		return nil
	}
}

func setAuthLoginExecutorForTest(executor func(context.Context, *config.Config, io.Writer) error) func() {
	previous := authLoginExecutor
	authLoginExecutor = executor
	return func() {
		authLoginExecutor = previous
	}
}

func selectOAuthLoginTarget(cfg *config.Config, in io.Reader, out io.Writer, accountName string) (*config.Config, OAuthAccountStatus, error) {
	statuses := discoverOAuthAccountStatuses(cfg)
	if len(statuses) == 0 {
		return cfg, OAuthAccountStatus{}, nil
	}

	var selected OAuthAccountStatus
	if strings.TrimSpace(accountName) != "" {
		var ok bool
		selected, ok = findOAuthAccountStatus(statuses, accountName)
		if !ok {
			return nil, OAuthAccountStatus{}, fmt.Errorf("oauth account %q not found", accountName)
		}
	} else if len(statuses) == 1 {
		selected = statuses[0]
		printOAuthAccountSelector(out, statuses)
	} else {
		printOAuthAccountSelector(out, statuses)
		picked, err := promptOAuthAccountSelection(in, out, statuses)
		if err != nil {
			return nil, OAuthAccountStatus{}, err
		}
		selected = picked
	}

	if selected.Status == "valid" {
		proceed, err := confirmOverwriteValidCredential(in, out, selected)
		if err != nil {
			return nil, OAuthAccountStatus{}, err
		}
		if !proceed {
			return nil, OAuthAccountStatus{}, errAuthLoginCanceled
		}
	}

	cloned := *cfg
	cloned.OAuth = cfg.OAuth
	cloned.OAuth.CredentialsFile = selected.CredentialsFile
	return &cloned, selected, nil
}

func discoverOAuthAccountStatuses(cfg *config.Config) []OAuthAccountStatus {
	effective := cfg.EffectiveUpstreams()
	statuses := make([]OAuthAccountStatus, 0, len(effective))
	for _, upstreamCfg := range effective {
		if upstreamCfg.Mode != "oauth" {
			continue
		}
		credPath := strings.TrimSpace(upstreamCfg.OAuth.CredentialsFile)
		if credPath == "" && cfg != nil {
			credPath = cfg.OAuth.CredentialsFile
		}
		status := OAuthAccountStatus{
			Name:            upstreamCfg.Name,
			DefaultModel:    upstreamCfg.DefaultModel,
			CredentialsFile: credPath,
			Status:          "missing",
		}
		cred, err := oauth.NewStore(credPath).Load()
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				status.Status = "invalid"
			}
			statuses = append(statuses, status)
			continue
		}
		status.Email = cred.Email
		expiresAt := time.Unix(cred.ExpiresAt, 0).UTC()
		status.ExpiresAt = &expiresAt
		if expiresAt.After(time.Now().UTC()) {
			status.Status = "valid"
		} else {
			status.Status = "expired"
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func findOAuthAccountStatus(statuses []OAuthAccountStatus, name string) (OAuthAccountStatus, bool) {
	for _, status := range statuses {
		if status.Name == strings.TrimSpace(name) {
			return status, true
		}
	}
	return OAuthAccountStatus{}, false
}

func printOAuthAccountSelector(out io.Writer, statuses []OAuthAccountStatus) {
	ui.PrintLines(out, ui.Banner(out), ui.Section("选择要登录的 OpenAI 账号"))
	for i, status := range statuses {
		line := fmt.Sprintf("%d. %s  default=%s  status=%s", i+1, status.Name, displayDefaultModel(status.DefaultModel), status.Status)
		if status.Email != "" {
			line += "  email=" + status.Email
		}
		if status.ExpiresAt != nil {
			line += "  expires=" + status.ExpiresAt.Format("2006-01-02 15:04")
		} else {
			line += "  file=" + status.CredentialsFile
		}
		ui.PrintLines(out, line)
	}
}

func promptOAuthAccountSelection(in io.Reader, out io.Writer, statuses []OAuthAccountStatus) (OAuthAccountStatus, error) {
	reader := bufio.NewReader(in)
	for {
		_, _ = fmt.Fprintf(out, "请输入账号编号 [1-%d]: ", len(statuses))
		value, err := reader.ReadString('\n')
		if err != nil {
			return OAuthAccountStatus{}, err
		}
		idx, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || idx < 1 || idx > len(statuses) {
			ui.PrintLines(out, ui.Warn("无效编号，请重新输入。"))
			continue
		}
		return statuses[idx-1], nil
	}
}

func confirmOverwriteValidCredential(in io.Reader, out io.Writer, status OAuthAccountStatus) (bool, error) {
	reader := bufio.NewReader(in)
	_, _ = fmt.Fprintf(out, "账号 %s 当前凭证仍有效，是否仍要重新登录覆盖？ [y/N]: ", status.Name)
	value, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func displayDefaultModel(model string) string {
	if strings.TrimSpace(model) == "" {
		return "-"
	}
	return model
}

func callbackAddress(cfg *config.Config) string {
	return fmt.Sprintf("%s:%d", cfg.OAuth.CallbackHost, cfg.OAuth.CallbackPort)
}

func buildRedirectURI(cfg *config.Config) string {
	return fmt.Sprintf("http://%s%s", callbackAddress(cfg), cfg.OAuth.CallbackPath)
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
