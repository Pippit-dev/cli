package canvas

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

const importFlagsHint = `--from libtv --url "https://www.liblib.tv/canvas?projectId=<project-id>"`

type importPromptSession struct {
	input  io.Reader
	reader *bufio.Reader
	stderr io.Writer
	eof    bool
	tui    *importPromptTUI
}

type importPromptChoice struct {
	label   string
	aliases []string
}

func newImportPromptSession(ctx context.Context, input io.Reader, stderr io.Writer) *importPromptSession {
	return newImportPromptSessionWithTUI(
		ctx,
		input,
		stderr,
		importInputIsInteractive(input) && importOutputIsInteractive(stderr) &&
			os.Getenv("PIPPIT_CLI_ACCESSIBLE") == "",
	)
}

func importOutputIsInteractive(output io.Writer) bool {
	file, ok := output.(*os.File)
	return ok && importFileIsTerminal(file)
}

func newImportPromptSessionWithTUI(
	ctx context.Context,
	input io.Reader,
	stderr io.Writer,
	enableTUI bool,
) *importPromptSession {
	session := &importPromptSession{
		input:  input,
		reader: bufio.NewReader(input),
		stderr: stderr,
	}
	if enableTUI {
		session.tui = &importPromptTUI{ctx: ctx, input: input, output: stderr}
	}
	return session
}

func importInputIsInteractive(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	return importFileIsTerminal(file)
}

func prepareCanvasImportOptions(
	ctx context.Context,
	input io.Reader,
	opts importOptions,
	isInteractive func(io.Reader) bool,
	stderr io.Writer,
) (importOptions, *importPromptSession, error) {
	needsWizard := strings.TrimSpace(opts.Provider) == "" || strings.TrimSpace(opts.SourceURL) == ""
	if !needsWizard {
		if isInteractive != nil && isInteractive(input) {
			return opts, newImportPromptSession(ctx, input, stderr), nil
		}
		return opts, nil, nil
	}
	if isInteractive == nil || !isInteractive(input) {
		return opts, nil, fmt.Errorf(
			"canvas import 缺少 --from 或 --url，且当前输入不是交互式终端；请传入 %s",
			importFlagsHint,
		)
	}
	prompts := newImportPromptSession(ctx, input, stderr)
	if strings.TrimSpace(opts.Provider) == "" {
		_, err := prompts.askChoice(
			"导入来源：",
			[]importPromptChoice{{label: "LibTV（默认）", aliases: []string{"libtv"}}},
			1,
		)
		if err != nil {
			return opts, nil, err
		}
		opts.Provider = "libtv"
	}
	if strings.TrimSpace(opts.SourceURL) == "" {
		for {
			value, eof, err := prompts.readLine("LibTV 画布链接：")
			if err != nil {
				return opts, nil, err
			}
			if value != "" {
				opts.SourceURL = value
				break
			}
			if eof {
				return opts, nil, fmt.Errorf(
					"尚未提供 LibTV 画布链接，交互输入就已结束；请重新运行并传入 %s",
					importFlagsHint,
				)
			}
			fmt.Fprintln(stderr, "请输入 LibTV 画布链接。")
		}
	}
	if !opts.JournalExplicit {
		choice, err := prompts.askChoice(
			"断点续跑记录：",
			[]importPromptChoice{
				{label: "自动生成（推荐，默认）"},
				{label: "自定义路径"},
			},
			1,
		)
		if err != nil {
			return opts, nil, err
		}
		if choice == 2 {
			for {
				value, eof, readErr := prompts.readLine("自定义断点记录路径：")
				if readErr != nil {
					return opts, nil, readErr
				}
				if value != "" {
					opts.JournalPath = value
					opts.JournalExplicit = true
					break
				}
				if eof {
					return opts, nil, fmt.Errorf(
						"尚未提供自定义断点记录路径，交互输入就已结束；请选择 1 自动生成，或传入 --journal <路径>",
					)
				}
				fmt.Fprintln(stderr, "选择自定义路径后，请输入断点记录路径。")
			}
		}
	}
	if !opts.OpenExplicit {
		choice, err := prompts.askChoice(
			"导入完成后：",
			[]importPromptChoice{
				{label: "打开画布（默认）", aliases: []string{"y", "yes"}},
				{label: "暂不打开", aliases: []string{"n", "no"}},
			},
			1,
		)
		if err != nil {
			return opts, nil, err
		}
		opts.Open = choice == 1
	}
	return opts, prompts, nil
}

func (prompts *importPromptSession) askChoice(
	title string,
	choices []importPromptChoice,
	defaultChoice int,
) (int, error) {
	if prompts.tui != nil {
		return prompts.tui.askChoice(title, choices, defaultChoice)
	}
	for {
		fmt.Fprintln(prompts.stderr, title)
		for index, choice := range choices {
			fmt.Fprintf(prompts.stderr, "  %d) %s\n", index+1, choice.label)
		}
		value, eof, err := prompts.readLine(fmt.Sprintf("请选择 [%d]：", defaultChoice))
		if err != nil {
			return 0, err
		}
		if value == "" {
			return defaultChoice, nil
		}
		normalized := strings.ToLower(value)
		for index, choice := range choices {
			if normalized == fmt.Sprint(index+1) || containsImportPromptAlias(choice.aliases, normalized) {
				return index + 1, nil
			}
		}
		if eof {
			return defaultChoice, nil
		}
		fmt.Fprintf(prompts.stderr, "请输入 1 到 %d 之间的数字。\n", len(choices))
	}
}

func containsImportPromptAlias(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func (prompts *importPromptSession) readLine(label string) (string, bool, error) {
	if prompts.tui != nil {
		value, err := prompts.tui.readLine(label)
		return value, false, err
	}
	fmt.Fprint(prompts.stderr, label)
	if prompts.eof {
		return "", true, nil
	}
	line, err := prompts.reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", false, fmt.Errorf("读取画布导入提示失败：%w", err)
	}
	if err == io.EOF {
		prompts.eof = true
	}
	return strings.TrimSpace(line), prompts.eof, nil
}
