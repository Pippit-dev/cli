package canvas

import (
	"context"
	"fmt"
	"io"
	"strings"
)

func preflightCanvasImportAuth(
	ctx context.Context,
	dependencies importDependencies,
	prompts *importPromptSession,
	stderr io.Writer,
) error {
	interactive := prompts != nil
	var pippitPrompt importAuthPrompt
	if prompts != nil {
		pippitPrompt = prompts.promptPippitAuth
	}

	fmt.Fprintln(stderr, "阶段：正在检查小云雀授权…")
	if err := ensureCanvasImportPippitAuth(ctx, dependencies.pippitAuth, interactive, pippitPrompt, stderr); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "小云雀授权校验通过。")
	return ensureLibTVImportAuth(ctx, dependencies.sourceAuth, prompts, stderr)
}

func ensureLibTVImportAuth(
	ctx context.Context,
	auth importSourceAuthenticator,
	prompts *importPromptSession,
	stderr io.Writer,
) error {
	if auth == nil {
		return fmt.Errorf("LibTV 授权检查未配置")
	}
	interactive := prompts != nil
	for {
		fmt.Fprintln(stderr, "阶段：正在检查 LibTV 授权…")
		if err := auth.Authenticate(ctx, interactive, stderr); err == nil {
			fmt.Fprintln(stderr, "LibTV 授权校验通过；现在开始导出，不会在下载中途再补做登录。")
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		} else if !interactive || prompts == nil {
			return fmt.Errorf("LibTV 授权校验失败，尚未开始下载任何项目或素材：%w", err)
		} else {
			fmt.Fprintf(stderr, "LibTV 授权未完成：%v\n", err)
		}

		choice, err := prompts.askChoice(
			"LibTV 授权下一步：",
			[]importPromptChoice{
				{label: "重新打开浏览器授权（默认）"},
				{label: "取消导入"},
			},
			1,
		)
		if err != nil {
			return err
		}
		if choice == 2 {
			return fmt.Errorf("已取消 LibTV 授权和画布导入")
		}
	}
}

func (prompts *importPromptSession) promptPippitAuth(
	_ context.Context,
	request importAuthPromptRequest,
) (importAuthPromptResponse, error) {
	if strings.TrimSpace(request.Failure) != "" {
		fmt.Fprintf(prompts.stderr, "小云雀授权提示：%s\n", request.Failure)
	}
	if request.ExplicitAccessKey {
		choice, err := prompts.askChoice(
			"当前 XYQ_ACCESS_KEY 校验失败：",
			[]importPromptChoice{
				{label: "重新校验当前环境变量"},
				{label: "取消导入并在 shell 中取消 XYQ_ACCESS_KEY（默认）"},
			},
			2,
		)
		if err != nil {
			return importAuthPromptResponse{}, err
		}
		if choice == 1 {
			return importAuthPromptResponse{Action: importAuthPromptRetry}, nil
		}
		return importAuthPromptResponse{Action: importAuthPromptCancel}, nil
	}
	choice, err := prompts.askChoice(
		"小云雀授权下一步：",
		[]importPromptChoice{
			{label: "重新打开浏览器授权（默认）"},
			{label: "重新校验当前登录"},
			{label: "取消导入"},
		},
		1,
	)
	if err != nil {
		return importAuthPromptResponse{}, err
	}
	switch choice {
	case 1:
		return importAuthPromptResponse{Action: importAuthPromptLogin}, nil
	case 2:
		return importAuthPromptResponse{Action: importAuthPromptRetry}, nil
	default:
		return importAuthPromptResponse{Action: importAuthPromptCancel}, nil
	}
}
