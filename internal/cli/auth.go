package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"

	"openai-local-gateway/internal/config"
	"openai-local-gateway/internal/oauth"
)

func AuthStatus(cfg *config.Config, out io.Writer) error {
	store := oauth.NewStore(cfg.OAuth.CredentialsFile)
	cred, err := store.Load()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "email: %s\nplan: %s\nexpires_at: %s\n", cred.Email, cred.PlanType, time.Unix(cred.ExpiresAt, 0).Format(time.RFC3339))
	return err
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
	_, err = fmt.Fprintf(out, "refresh succeeded for %s\n", refreshed.Email)
	return err
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
	_, _ = fmt.Fprintf(out, "authorization URL:\n%s\n", session.AuthURL)

	if cfg.OAuth.AutoOpenBrowser {
		if err := openBrowser(session.AuthURL); err != nil {
			_, _ = fmt.Fprintf(out, "failed to open browser automatically, open this URL manually:\n%s\n", session.AuthURL)
		}
	} else {
		_, _ = fmt.Fprintf(out, "open this URL in your browser:\n%s\n", session.AuthURL)
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
		_, err = fmt.Fprintf(out, "oauth login succeeded for %s\n", cred.Email)
		return err
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
