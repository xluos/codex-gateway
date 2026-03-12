package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"

	"codex-gateway/internal/config"
	"codex-gateway/internal/oauth"
	"codex-gateway/internal/ui"
)

func AuthStatus(cfg *config.Config, out io.Writer) error {
	store := oauth.NewStore(cfg.OAuth.CredentialsFile)
	cred, err := store.Load()
	if err != nil {
		return err
	}
	ui.PrintLines(out,
		ui.Banner(),
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

func AuthLogin(ctx context.Context, cfg *config.Config, out io.Writer) error {
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
	ui.PrintLines(out, ui.Banner(), ui.Section("OAuth 登录"), ui.KV("授权链接", session.AuthURL))

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
