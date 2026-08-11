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

var errCanvasImportSetupCanceled = errors.New("Canvas import setup canceled")

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
		return 0, fmt.Errorf("invalid default Canvas import choice %d", defaultChoice)
	}
	selected := defaultChoice
	options := make([]huh.Option[int], 0, len(choices))
	for index, choice := range choices {
		options = append(options, huh.NewOption(choice.label, index+1))
	}
	field := huh.NewSelect[int]().
		Title(strings.TrimSpace(title)).
		Description("Use ↑/↓ to move, Enter to select").
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
		Description("Paste a value, then press Enter").
		Prompt("› ").
		Value(&value)
	switch title {
	case "LibTV canvas URL":
		field.Validate(func(value string) error {
			if _, err := normalizeLibTVURL(value); err != nil {
				return err
			}
			return nil
		})
	case "Custom journal path":
		field.Validate(func(value string) error {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("a custom journal path is required")
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
		return fmt.Errorf("run Canvas import terminal prompt: %w", err)
	}
	return nil
}
