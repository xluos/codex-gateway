package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"codex-gateway/internal/config"
	"codex-gateway/internal/oauth"
	"codex-gateway/internal/ui"
)

func Doctor(configPath string, out io.Writer) error {
	ui.PrintLines(out, ui.Banner(), ui.Section("环境诊断"))

	resolvedConfigPath, err := resolvePath(configPath)
	if err != nil {
		return err
	}

	var issueCount int
	if _, err := os.Stat(resolvedConfigPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			ui.PrintLines(out,
				ui.Error("配置文件不存在"),
				ui.KV("配置文件", resolvedConfigPath),
				ui.Muted("先执行 codexgateway init 生成配置。"),
			)
			return fmt.Errorf("doctor found 1 issue")
		}
		return fmt.Errorf("stat config: %w", err)
	}
	ui.PrintLines(out, ui.Success("配置文件存在"), ui.KV("配置文件", resolvedConfigPath))

	cfg, err := loadConfigForDoctor(resolvedConfigPath)
	if err != nil {
		ui.PrintLines(out, ui.Error("配置文件解析失败"), ui.Muted(err.Error()))
		return err
	}
	ui.PrintLines(out, ui.Success("配置文件可用"), ui.KV("上游模式", cfg.Upstream.Mode), ui.KV("监听地址", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)))

	if err := checkRuntimeWritable(cfg, out); err != nil {
		issueCount++
	}
	if err := checkOAuthHealth(cfg, out); err != nil {
		issueCount++
	}
	if err := checkRuntimeStateHealth(cfg, out); err != nil {
		issueCount++
	}
	if err := checkPortHealth(cfg, out); err != nil {
		issueCount++
	}

	if issueCount > 0 {
		ui.PrintLines(out, "", ui.Warn(fmt.Sprintf("doctor 发现 %d 个问题", issueCount)))
		return fmt.Errorf("doctor found %d issue(s)", issueCount)
	}
	ui.PrintLines(out, "", ui.Success("doctor 检查通过"))
	return nil
}

func loadConfigForDoctor(configPath string) (*config.Config, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return config.LoadConfig(file)
}

func checkRuntimeWritable(cfg *config.Config, out io.Writer) error {
	if err := os.MkdirAll(cfg.Runtime.Dir, 0o755); err != nil {
		ui.PrintLines(out, ui.Error("运行目录不可写"), ui.KV("目录", cfg.Runtime.Dir), ui.Muted(err.Error()))
		return err
	}
	testFile := filepath.Join(cfg.Runtime.Dir, ".doctor-write-test")
	if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
		ui.PrintLines(out, ui.Error("运行目录不可写"), ui.KV("目录", cfg.Runtime.Dir), ui.Muted(err.Error()))
		return err
	}
	_ = os.Remove(testFile)
	ui.PrintLines(out, ui.Success("运行目录可写"), ui.KV("运行目录", cfg.Runtime.Dir), ui.KV("日志文件", cfg.Runtime.LogFile))
	return nil
}

func checkOAuthHealth(cfg *config.Config, out io.Writer) error {
	if cfg.Upstream.Mode != "oauth" {
		ui.PrintLines(out, ui.Success("当前未使用 OAuth 模式"))
		return nil
	}
	store := oauth.NewStore(cfg.OAuth.CredentialsFile)
	cred, err := store.Load()
	if err != nil {
		ui.PrintLines(out, ui.Error("OAuth 凭证不可用"), ui.KV("凭证文件", cfg.OAuth.CredentialsFile), ui.Muted("先执行 codexgateway auth login。"))
		return err
	}
	expiresAt := time.Unix(cred.ExpiresAt, 0)
	if expiresAt.Before(time.Now()) {
		ui.PrintLines(out, ui.Warn("OAuth access token 已过期"), ui.KV("邮箱", cred.Email), ui.KV("过期时间", expiresAt.Format(time.RFC3339)), ui.Muted("可以执行 codexgateway auth refresh。"))
		return fmt.Errorf("oauth credentials expired")
	}
	status := ui.Success("OAuth 凭证可用")
	if expiresAt.Before(time.Now().Add(30 * time.Minute)) {
		status = ui.Warn("OAuth 凭证即将过期")
	}
	ui.PrintLines(out, status, ui.KV("邮箱", cred.Email), ui.KV("套餐", cred.PlanType), ui.KV("过期时间", expiresAt.Format(time.RFC3339)), ui.KV("凭证文件", cfg.OAuth.CredentialsFile))
	return nil
}

func checkRuntimeStateHealth(cfg *config.Config, out io.Writer) error {
	state, err := readState(cfg.Runtime.StateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			ui.PrintLines(out, ui.Warn("服务当前未运行"), ui.Muted("如需启动，执行 codexgateway start。"))
			return nil
		}
		ui.PrintLines(out, ui.Error("状态文件读取失败"), ui.KV("状态文件", cfg.Runtime.StateFile), ui.Muted(err.Error()))
		return err
	}
	if processExists(state.PID) {
		ui.PrintLines(out, ui.Success("服务正在运行"), ui.KV("PID", fmt.Sprintf("%d", state.PID)), ui.KV("地址", state.Address), ui.KV("日志文件", state.LogFile))
		return nil
	}
	ui.PrintLines(out, ui.Warn("检测到过期状态文件"), ui.KV("状态文件", cfg.Runtime.StateFile), ui.Muted("可以执行 codexgateway stop 清理，或重新 start。"))
	return fmt.Errorf("stale runtime state")
}

func checkPortHealth(cfg *config.Config, out io.Writer) error {
	addr := net.JoinHostPort(cfg.Server.Host, fmt.Sprintf("%d", cfg.Server.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		ui.PrintLines(out, ui.Warn("监听端口不可用"), ui.KV("地址", addr), ui.Muted("如果服务未运行，可能被其他进程占用。"))
		return err
	}
	_ = listener.Close()
	ui.PrintLines(out, ui.Success("监听端口可用"), ui.KV("地址", addr))
	return nil
}
