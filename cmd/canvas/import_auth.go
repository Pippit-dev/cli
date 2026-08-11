package canvas

import (
	"context"
	"errors"
	"fmt"
	"strings"

	canvascore "github.com/Pippit-dev/pippit-cli/internal/canvas"
	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const canvasImportAuthProbeAssetID = "9223372036854775807"

var errCanvasImportAuthCanceled = errors.New("已取消小云雀授权")
var errCanvasImportReauthenticationRequired = errors.New("需要重新校验小云雀授权")

type importAuthAPI interface {
	AccessKey() string
	SetAccessKey(string) error
	Probe(context.Context) error
}

type runnerImportAuthAPI struct {
	runner *common.Runner
}

func (api runnerImportAuthAPI) AccessKey() string {
	if api.runner == nil || api.runner.Config == nil {
		return ""
	}
	return strings.TrimSpace(api.runner.Config.AccessKey)
}

func (api runnerImportAuthAPI) SetAccessKey(accessKey string) error {
	if api.runner == nil || api.runner.Config == nil {
		return fmt.Errorf("小云雀 CLI 运行时配置不完整")
	}
	api.runner.Config.AccessKey = strings.TrimSpace(accessKey)
	return nil
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
	importAuthPromptReplace
	importAuthPromptCancel
)

type importAuthPromptRequest struct {
	HasAccessKey bool
	Failure      string
}

type importAuthPromptResponse struct {
	Action    importAuthPromptAction
	AccessKey string
}

type importAuthPrompt func(context.Context, importAuthPromptRequest) (importAuthPromptResponse, error)

// ensureCanvasImportPippitAuth verifies Pippit authorization before the source
// export starts. Interactive callers may retry a transient failure, replace an
// invalid key in memory, or cancel. The key is never persisted by this flow.
func ensureCanvasImportPippitAuth(
	ctx context.Context,
	auth importAuthAPI,
	interactive bool,
	prompt importAuthPrompt,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if auth == nil {
		return fmt.Errorf("小云雀授权检查未配置")
	}

	accessKey := strings.TrimSpace(auth.AccessKey())
	failure := ""
	for {
		if accessKey == "" {
			if !interactive {
				return fmt.Errorf("未找到小云雀 Access Key；请先设置 XYQ_ACCESS_KEY，或在交互模式中安全粘贴 Access Key")
			}
			if prompt == nil {
				return fmt.Errorf("未找到小云雀 Access Key，且交互授权引导未配置")
			}
			response, err := prompt(ctx, importAuthPromptRequest{
				HasAccessKey: false,
				Failure:      failure,
			})
			if err != nil {
				return canvasImportAuthPromptError(err)
			}
			switch response.Action {
			case importAuthPromptCancel:
				return errCanvasImportAuthCanceled
			case importAuthPromptReplace:
				accessKey = strings.TrimSpace(response.AccessKey)
				if accessKey == "" {
					failure = "Access Key 不能为空，请重新粘贴"
					continue
				}
				if err := auth.SetAccessKey(accessKey); err != nil {
					return fmt.Errorf("更新小云雀内存授权信息失败：%s", redactCanvasImportAuthFailure(err, accessKey))
				}
			case importAuthPromptRetry:
				failure = "当前没有可重试的 Access Key，请先粘贴"
				continue
			default:
				return fmt.Errorf("小云雀授权引导返回了未知操作")
			}
		}

		if err := ctx.Err(); err != nil {
			return err
		}
		if err := auth.Probe(ctx); err == nil {
			return nil
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
			HasAccessKey: true,
			Failure:      failure,
		})
		if err != nil {
			return canvasImportAuthPromptError(err)
		}
		switch response.Action {
		case importAuthPromptRetry:
			continue
		case importAuthPromptReplace:
			replacement := strings.TrimSpace(response.AccessKey)
			if replacement == "" {
				accessKey = ""
				failure = "Access Key 不能为空，请重新粘贴"
				continue
			}
			if err := auth.SetAccessKey(replacement); err != nil {
				return fmt.Errorf("更新小云雀内存授权信息失败：%s", redactCanvasImportAuthFailure(err, replacement))
			}
			accessKey = replacement
		case importAuthPromptCancel:
			return errCanvasImportAuthCanceled
		default:
			return fmt.Errorf("小云雀授权引导返回了未知操作")
		}
	}
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
	redacted := redactCanvasImportAuthFailure(err, auth.AccessKey())
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
	if errors.Is(err, errCanvasImportReauthenticationRequired) {
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
