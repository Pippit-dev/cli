package authcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

type loginResult struct {
	LoggedIn        bool   `json:"logged_in"`
	Source          string `json:"source"`
	UID             string `json:"uid,omitempty"`
	CredentialScope string `json:"credential_scope,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
}

type logoutResult struct {
	LoggedOut                 bool `json:"logged_out"`
	EnvironmentStillActive    bool `json:"environment_still_active,omitempty"`
	RemoteCredentialPreserved bool `json:"remote_credential_preserved"`
}

// NewLoginCommand creates the top-level `pippit-tool-cli login` command.
func NewLoginCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var forceRefresh bool
	command := &cobra.Command{
		Use:   "login",
		Short: "通过浏览器登录小云雀 CLI",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			manager, err := requireAuthManager(runner)
			if err != nil {
				return err
			}
			credential, err := manager.Login(command.Context(), internal_auth.LoginOptions{
				Progress: stderr, ForceRefresh: forceRefresh,
			})
			if err != nil {
				return err
			}
			if runner.Config != nil && strings.TrimSpace(runner.Config.AccessKey) != "" {
				_, _ = fmt.Fprintln(stderr, "提示：当前进程设置了 XYQ_ACCESS_KEY，它会继续优先于刚保存的浏览器登录凭证。")
			}
			return writeJSON(stdout, loginResult{
				LoggedIn:        true,
				Source:          "browser",
				UID:             credential.UID,
				CredentialScope: credential.CredentialScope,
				ExpiresAt:       time.Unix(credential.ExpiredAt, 0).Format(time.RFC3339),
			})
		},
	}
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.Flags().BoolVar(&forceRefresh, "force", false, "强制轮换当前设备的 CLI Access Key（仅在旧密钥被拒绝时使用）")
	return command
}

// NewStatusCommand creates the top-level `pippit-tool-cli status` command.
func NewStatusCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	command := &cobra.Command{
		Use:   "status",
		Short: "查看小云雀 CLI 登录状态",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			manager, err := requireAuthManager(runner)
			if err != nil {
				return err
			}
			status, err := manager.Status(command.Context())
			if err != nil {
				return err
			}
			result := loginResult{LoggedIn: status.LoggedIn, Source: status.Source, UID: status.UID, CredentialScope: status.CredentialScope}
			if !status.ExpiresAt.IsZero() {
				result.ExpiresAt = status.ExpiresAt.Format(time.RFC3339)
			}
			return writeJSON(stdout, result)
		},
	}
	command.SetOut(stdout)
	command.SetErr(stderr)
	return command
}

// NewLogoutCommand clears only the browser-managed local credential. An
// explicit XYQ_ACCESS_KEY belongs to the caller's environment and is never
// modified or revoked by this command.
func NewLogoutCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	command := &cobra.Command{
		Use:   "logout",
		Short: "清除本机小云雀 CLI 登录（不撤销远程 Access Key）",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			manager, err := requireAuthManager(runner)
			if err != nil {
				return err
			}
			if err := manager.Logout(command.Context(), false); err != nil && !errors.Is(err, internal_auth.ErrCredentialNotFound) {
				return err
			}
			environmentActive := runner.Config != nil && strings.TrimSpace(runner.Config.AccessKey) != ""
			if environmentActive {
				_, _ = fmt.Fprintln(stderr, "提示：XYQ_ACCESS_KEY 仍由当前 shell 提供，CLI 无法替你清除该环境变量。")
			}
			_, _ = fmt.Fprintln(stderr, "已清除本机登录密钥；远程 Access Key 未撤销，并保留非秘密设备标识供下次登录安全复用。")
			return writeJSON(stdout, logoutResult{
				LoggedOut: true, EnvironmentStillActive: environmentActive, RemoteCredentialPreserved: true,
			})
		},
	}
	command.SetOut(stdout)
	command.SetErr(stderr)
	return command
}

func requireAuthManager(runner *common.Runner) (common.AuthManager, error) {
	if runner == nil || runner.Auth == nil {
		return nil, fmt.Errorf("小云雀 CLI 浏览器授权尚未配置")
	}
	return runner.Auth, nil
}

func writeJSON(writer io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("编码登录结果失败: %w", err)
	}
	_, err = fmt.Fprintln(writer, string(payload))
	return err
}
