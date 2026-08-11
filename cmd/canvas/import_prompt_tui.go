package canvas

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"
)

const importPromptWidth = 72

var errCanvasImportSetupCanceled = errors.New("已取消画布导入设置")

type importPromptTUI struct {
	ctx    context.Context
	input  io.Reader
	output io.Writer
}

func (prompt *importPromptTUI) askChoice(
	title string,
	choices []importPromptChoice,
	defaultChoice int,
) (int, error) {
	if defaultChoice < 1 || defaultChoice > len(choices) {
		return 0, fmt.Errorf("画布导入的默认选项 %d 无效", defaultChoice)
	}
	selected := defaultChoice
	options := make([]huh.Option[int], 0, len(choices))
	for index, choice := range choices {
		options = append(options, huh.NewOption(choice.label, index+1))
	}
	field := huh.NewSelect[int]().
		Title(strings.TrimSpace(title)).
		Description("使用 ↑/↓ 切换，按 Enter 确认").
		Options(options...).
		Value(&selected)
	if err := prompt.run(field); err != nil {
		return 0, err
	}
	return selected, nil
}

func (prompt *importPromptTUI) readLine(label string) (string, error) {
	value := ""
	title := strings.TrimSpace(strings.TrimSuffix(label, ": "))
	field := huh.NewInput().
		Title(title).
		Description("粘贴内容后按 Enter 确认").
		Prompt("› ").
		Value(&value)
	switch title {
	case "LibTV 画布链接":
		field.Validate(func(value string) error {
			if _, err := normalizeLibTVURL(value); err != nil {
				return err
			}
			return nil
		})
	case "自定义断点记录路径":
		field.Validate(func(value string) error {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("请输入自定义断点记录路径")
			}
			return nil
		})
	}
	if err := prompt.run(field); err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (prompt *importPromptTUI) run(field huh.Field) error {
	form := huh.NewForm(huh.NewGroup(field)).
		WithTheme(huh.ThemeCharm()).
		WithWidth(importPromptWidth).
		WithInput(prompt.input).
		WithOutput(prompt.output)
	ctx := prompt.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := form.RunWithContext(ctx); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", errCanvasImportSetupCanceled, ctx.Err())
		}
		if errors.Is(err, huh.ErrUserAborted) {
			return errCanvasImportSetupCanceled
		}
		return fmt.Errorf("运行画布导入终端交互失败：%w", err)
	}
	return nil
}
