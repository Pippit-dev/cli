package canvas

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

const importFlagsHint = `--from libtv --url "https://www.liblib.tv/canvas?projectId=<project-id>"`

type importPromptSession struct {
	reader *bufio.Reader
	stderr io.Writer
	eof    bool
}

type importPromptChoice struct {
	label   string
	aliases []string
}

func importInputIsInteractive(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	return importFileIsTerminal(file)
}

func prepareCanvasImportOptions(
	input io.Reader,
	opts importOptions,
	isInteractive func(io.Reader) bool,
	stderr io.Writer,
) (importOptions, *importPromptSession, error) {
	needsWizard := strings.TrimSpace(opts.Provider) == "" || strings.TrimSpace(opts.SourceURL) == ""
	if !needsWizard {
		return opts, nil, nil
	}
	if isInteractive == nil || !isInteractive(input) {
		return opts, nil, fmt.Errorf(
			"canvas import is missing --from or --url and stdin is not interactive; pass %s",
			importFlagsHint,
		)
	}
	prompts := &importPromptSession{reader: bufio.NewReader(input), stderr: stderr}
	if strings.TrimSpace(opts.Provider) == "" {
		_, err := prompts.askChoice(
			"Source provider:",
			[]importPromptChoice{{label: "LibTV (default)", aliases: []string{"libtv"}}},
			1,
		)
		if err != nil {
			return opts, nil, err
		}
		opts.Provider = "libtv"
	}
	if strings.TrimSpace(opts.SourceURL) == "" {
		for {
			value, eof, err := prompts.readLine("LibTV canvas URL: ")
			if err != nil {
				return opts, nil, err
			}
			if value != "" {
				opts.SourceURL = value
				break
			}
			if eof {
				return opts, nil, fmt.Errorf(
					"interactive input ended before a LibTV URL was provided; rerun with %s",
					importFlagsHint,
				)
			}
			fmt.Fprintln(stderr, "A LibTV canvas URL is required.")
		}
	}
	if !opts.JournalExplicit {
		choice, err := prompts.askChoice(
			"Resume journal:",
			[]importPromptChoice{
				{label: "Automatic (recommended, default)"},
				{label: "Custom path"},
			},
			1,
		)
		if err != nil {
			return opts, nil, err
		}
		if choice == 2 {
			for {
				value, eof, readErr := prompts.readLine("Custom journal path: ")
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
						"interactive input ended before a custom journal path was provided; choose 1 for Automatic or pass --journal <path>",
					)
				}
				fmt.Fprintln(stderr, "A custom journal path is required after selecting option 2.")
			}
		}
	}
	if !opts.OpenExplicit {
		choice, err := prompts.askChoice(
			"After import:",
			[]importPromptChoice{
				{label: "Open Canvas (default)", aliases: []string{"y", "yes"}},
				{label: "Do not open", aliases: []string{"n", "no"}},
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
	for {
		fmt.Fprintln(prompts.stderr, title)
		for index, choice := range choices {
			fmt.Fprintf(prompts.stderr, "  %d) %s\n", index+1, choice.label)
		}
		value, eof, err := prompts.readLine(fmt.Sprintf("Select [%d]: ", defaultChoice))
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
		fmt.Fprintf(prompts.stderr, "Please select a number from 1 to %d.\n", len(choices))
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
	fmt.Fprint(prompts.stderr, label)
	if prompts.eof {
		return "", true, nil
	}
	line, err := prompts.reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", false, fmt.Errorf("read canvas import prompt: %w", err)
	}
	if err == io.EOF {
		prompts.eof = true
	}
	return strings.TrimSpace(line), prompts.eof, nil
}
