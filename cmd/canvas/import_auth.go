package canvas

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
	canvascore "github.com/Pippit-dev/pippit-cli/internal/canvas"
	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const canvasImportAuthProbeAssetID = "9223372036854775807"

var errCanvasImportAuthCanceled = errors.New("已取消小云雀授权")
var errCanvasImportReauthenticationRequired = errors.New("需要重新校验小云雀授权")

type importAuthAPI interface {
	AccessKey(context.Context) (string, error)
	Login(context.Context, io.Writer, importAuthLoginOptions) error
	HasExplicitAccessKey() bool
	CredentialScope(context.Context) (string, error)
	Probe(context.Context) error
}

type importAuthLoginOptions struct {
	ForceRefresh            bool
	ExpectedCredentialScope string
}

type runnerImportAuthAPI struct {
	runner *common.Runner
}

func (api runnerImportAuthAPI) AccessKey(ctx context.Context) (string, error) {
	if api.runner == nil {
		return "", fmt.Errorf("小云雀 CLI 运行时配置不完整")
	}
	if api.runner.Auth != nil {
		return api.runner.Auth.ResolveAccessKey(ctx)
	}
	if api.runner.Config == nil {
		return "", fmt.Errorf("小云雀 CLI 运行时配置不完整")
	}
	return strings.TrimSpace(api.runner.Config.AccessKey), nil
}

func (api runnerImportAuthAPI) Login(ctx context.Context, progress io.Writer, options importAuthLoginOptions) error {
	if api.runner == nil || api.runner.Auth == nil {
		return fmt.Errorf("小云雀 CLI 浏览器授权尚未配置")
	}
	_, err := api.runner.Auth.Login(ctx, internal_auth.LoginOptions{
		Progress:                progress,
		ForceRefresh:            options.ForceRefresh,
		ExpectedCredentialScope: options.ExpectedCredentialScope,
	})
	return err
}

func (api runnerImportAuthAPI) HasExplicitAccessKey() bool {
	return api.runner != nil && api.runner.Config != nil && strings.TrimSpace(api.runner.Config.AccessKey) != ""
}

func (api runnerImportAuthAPI) CredentialScope(ctx context.Context) (string, error) {
	if api.HasExplicitAccessKey() {
		return legacyCanvasImportAuthScope(strings.TrimSpace(api.runner.Config.AccessKey)), nil
	}
	if api.runner == nil || api.runner.Auth == nil {
		return "", fmt.Errorf("小云雀 CLI 浏览器授权尚未配置")
	}
	return api.runner.Auth.CredentialScope(ctx)
}

func (api runnerImportAuthAPI) Probe(ctx context.Context) error {
	if api.runner == nil || api.runner.Config == nil || api.runner.Client == nil {
		return fmt.Errorf("小云雀 CLI 运行时配置不完整")
	}
	var response struct {
		Ret string `json:"ret"`
	}
	err := api.runner.Client.SendRequest(ctx, canvascore.QueryPath, map[string]any{
		"pippit_asset_ids": []string{canvasImportAuthProbeAssetID},
		"Base":             map[string]any{},
	}, &response)
	if err != nil {
		return err
	}
	if strings.TrimSpace(response.Ret) != "0" {
		return fmt.Errorf("小云雀授权校验返回业务错误（ret=%s）", strings.TrimSpace(response.Ret))
	}
	return nil
}

type importAuthPromptAction uint8

const (
	importAuthPromptRetry importAuthPromptAction = iota + 1
	importAuthPromptLogin
	importAuthPromptCancel
)

type importAuthPromptRequest struct {
	HasCredential     bool
	ExplicitAccessKey bool
	Failure           string
}

type importAuthPromptResponse struct {
	Action importAuthPromptAction
}

type importAuthPrompt func(context.Context, importAuthPromptRequest) (importAuthPromptResponse, error)

// ensureCanvasImportPippitAuth verifies Pippit authorization before the source
// export starts. Interactive callers may retry a transient failure or complete
// browser authorization; Access Keys are never read from terminal input.
func ensureCanvasImportPippitAuth(
	ctx context.Context,
	auth importAuthAPI,
	interactive bool,
	prompt importAuthPrompt,
	progress ...io.Writer,
) error {
	return ensureCanvasImportPippitAuthForScope(ctx, auth, interactive, prompt, "", progress...)
}

func ensureCanvasImportPippitAuthForScope(
	ctx context.Context,
	auth importAuthAPI,
	interactive bool,
	prompt importAuthPrompt,
	expectedCredentialScope string,
	progress ...io.Writer,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if auth == nil {
		return fmt.Errorf("小云雀授权检查未配置")
	}

	progressWriter := io.Discard
	if len(progress) > 0 && progress[0] != nil {
		progressWriter = progress[0]
	}
	failure := ""
	for {
		accessKey, resolveErr := auth.AccessKey(ctx)
		accessKey = strings.TrimSpace(accessKey)
		if accessKey == "" {
			if !interactive {
				return fmt.Errorf("未登录小云雀 CLI；请先运行 pippit-tool-cli login（CI 仍可设置 XYQ_ACCESS_KEY）")
			}
			if resolveErr != nil && !errors.Is(resolveErr, internal_auth.ErrCredentialNotFound) &&
				!errors.Is(resolveErr, internal_auth.ErrCredentialExpired) {
				return fmt.Errorf("读取小云雀 CLI 登录凭证失败")
			}
			if err := auth.Login(ctx, progressWriter, importAuthLoginOptions{
				ForceRefresh:            expectedCredentialScope != "",
				ExpectedCredentialScope: expectedCredentialScope,
			}); err != nil {
				return fmt.Errorf("小云雀网页授权失败：%w", err)
			}
			failure = ""
			continue
		}

		if err := ctx.Err(); err != nil {
			return err
		}
		if err := auth.Probe(ctx); err == nil {
			return verifyCanvasImportCredentialScope(ctx, auth, expectedCredentialScope)
		} else {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			failure = describeCanvasImportAuthProbeFailure(err, accessKey)
		}
		if !interactive {
			return fmt.Errorf("小云雀 Access Key 校验失败：%s", failure)
		}
		if prompt == nil {
			return fmt.Errorf("小云雀 Access Key 校验失败，且交互授权引导未配置：%s", failure)
		}

		response, err := prompt(ctx, importAuthPromptRequest{
			HasCredential:     true,
			ExplicitAccessKey: auth.HasExplicitAccessKey(),
			Failure:           failure,
		})
		if err != nil {
			return canvasImportAuthPromptError(err)
		}
		switch response.Action {
		case importAuthPromptRetry:
			continue
		case importAuthPromptLogin:
			if auth.HasExplicitAccessKey() {
				return fmt.Errorf("当前 XYQ_ACCESS_KEY 会覆盖浏览器登录；请先取消导入并在 shell 中 unset XYQ_ACCESS_KEY")
			}
			if err := auth.Login(ctx, progressWriter, importAuthLoginOptions{
				ForceRefresh:            true,
				ExpectedCredentialScope: expectedCredentialScope,
			}); err != nil {
				return fmt.Errorf("小云雀网页授权失败：%w", err)
			}
			failure = ""
		case importAuthPromptCancel:
			return errCanvasImportAuthCanceled
		default:
			return fmt.Errorf("小云雀授权引导返回了未知操作")
		}
	}
}

func reauthenticateCanvasImportPippit(
	ctx context.Context,
	auth importAuthAPI,
	prompt importAuthPrompt,
	expectedCredentialScope string,
	progress io.Writer,
) error {
	if auth == nil {
		return fmt.Errorf("小云雀授权检查未配置")
	}
	if auth.HasExplicitAccessKey() {
		return fmt.Errorf("当前 XYQ_ACCESS_KEY 会覆盖浏览器登录；请取消导入并在 shell 中 unset XYQ_ACCESS_KEY")
	}
	if err := auth.Login(ctx, progress, importAuthLoginOptions{
		ForceRefresh:            true,
		ExpectedCredentialScope: expectedCredentialScope,
	}); err != nil {
		return fmt.Errorf("小云雀网页重新授权失败：%w", err)
	}
	return ensureCanvasImportPippitAuthForScope(
		ctx, auth, true, prompt, expectedCredentialScope, progress,
	)
}

func verifyCanvasImportCredentialScope(ctx context.Context, auth importAuthAPI, expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	actual, err := auth.CredentialScope(ctx)
	if err != nil {
		return fmt.Errorf("重新授权后无法确认小云雀账号：%w", err)
	}
	if strings.TrimSpace(actual) != expected {
		return fmt.Errorf("%w；为避免复用上一账号的素材或画布断点，本次导入已安全停止", internal_auth.ErrCredentialAccountMismatch)
	}
	return nil
}

func canvasImportAuthPromptError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	// Prompt implementations may have already read a candidate key when they
	// fail. Do not include their raw error text in user-facing output.
	return errors.New("读取小云雀授权选择失败")
}

func redactCanvasImportAuthFailure(err error, accessKey string) string {
	if err == nil {
		return "未知错误"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "未知错误"
	}
	if accessKey = strings.TrimSpace(accessKey); accessKey != "" {
		message = strings.ReplaceAll(message, accessKey, "[已隐藏]")
	}
	return message
}

func redactCanvasImportFinalError(err error, auth importAuthAPI) error {
	if err == nil || auth == nil {
		return err
	}
	original := err.Error()
	accessKey, _ := auth.AccessKey(context.Background())
	redacted := redactCanvasImportAuthFailure(err, accessKey)
	if redacted == original {
		return err
	}
	return errors.New(redacted)
}

func describeCanvasImportAuthProbeFailure(err error, accessKey string) string {
	message := strings.ToLower(redactCanvasImportAuthFailure(err, accessKey))
	switch {
	case strings.Contains(message, "http 401"), strings.Contains(message, "unauthorized"):
		return "小云雀拒绝了当前 Access Key（HTTP 401），它可能无效、已过期或不属于当前环境"
	case strings.Contains(message, "http 403"), strings.Contains(message, "forbidden"):
		return "当前 Access Key 无权访问该小云雀环境（HTTP 403）"
	case strings.Contains(message, "timeout"), strings.Contains(message, "deadline exceeded"), strings.Contains(message, "超时"):
		return "小云雀授权校验请求超时，请检查网络后重试"
	default:
		return "小云雀授权校验未通过，请检查网络、PPE 环境和 Access Key 后重试"
	}
}

func isCanvasImportPippitAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errCanvasImportReauthenticationRequired) ||
		errors.Is(err, internal_auth.ErrCredentialNotFound) ||
		errors.Is(err, internal_auth.ErrCredentialExpired) ||
		errors.Is(err, internal_auth.ErrCredentialRejected) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"http 401", "http 403", "unauthorized", "forbidden",
		`ret="1015"`, "ret=1015", "xyq_access_key 缺失", "缺少 xyq_access_key",
		"access key 校验失败", "access key 无权",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// isProvenPrewriteCredentialFailure is intentionally stricter than the
// user-facing detector above. Only structured credential failures prove that
// a protected write was rejected before execution; message substrings are not
// sufficient evidence for clearing a durable upload-requested marker.
func isProvenPrewriteCredentialFailure(err error) bool {
	return errors.Is(err, internal_auth.ErrCredentialNotFound) ||
		errors.Is(err, internal_auth.ErrCredentialExpired) ||
		errors.Is(err, internal_auth.ErrCredentialRejected)
}
