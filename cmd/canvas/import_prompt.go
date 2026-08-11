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

func importInputIsInteractive(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
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
		value, _, err := prompts.readLine("Source provider [libtv]: ")
		if err != nil {
			return opts, nil, err
		}
		if value == "" {
			value = "libtv"
		}
		opts.Provider = value
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
		value, _, err := prompts.readLine("Resume journal path [automatic]: ")
		if err != nil {
			return opts, nil, err
		}
		if value != "" {
			opts.JournalPath = value
			opts.JournalExplicit = true
		}
	}
	if !opts.OpenExplicit {
		open, err := prompts.askYesNo("Open the imported Canvas when finished? [Y/n]: ", true)
		if err != nil {
			return opts, nil, err
		}
		opts.Open = open
	}
	return opts, prompts, nil
}

func (prompts *importPromptSession) confirmDegradations(count int) (bool, error) {
	fmt.Fprintf(prompts.stderr, "LibTV export reports %d explicit degradation(s).\n", count)
	return prompts.askYesNo("Continue importing with these degradations? [y/N]: ", false)
}

func (prompts *importPromptSession) askYesNo(label string, defaultValue bool) (bool, error) {
	for {
		value, eof, err := prompts.readLine(label)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(value) {
		case "":
			return defaultValue, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			if eof {
				return defaultValue, nil
			}
			fmt.Fprintln(prompts.stderr, "Please answer y or n.")
		}
	}
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
