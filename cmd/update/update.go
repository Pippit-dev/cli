package updatecmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const defaultPackage = "@pippit-dev/cli"

var legacyGlobalSkills = []string{
	"pippit-short-drama-skill",
	"xyq-nest-skill",
}

// NewCommand builds the update command.
func NewCommand(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update pippit-tool-cli and bundled skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(stdout, stderr)
		},
	}
	return cmd
}

func runUpdate(stdout, stderr io.Writer) error {
	pkg := os.Getenv("PIPPIT_CLI_INSTALL_PACKAGE")
	if pkg == "" {
		pkg = defaultPackage + "@latest"
	}

	fmt.Fprintf(stderr, "Updating pippit-tool-cli via npm: %s\n", pkg)
	restore, err := prepareSelfReplace()
	if err != nil {
		return fmt.Errorf("prepare self replace: %w", err)
	}
	if err := runInheritEnv(stderr, []string{"PIPPIT_CLI_SKIP_SKILLS=1"}, "npm", "install", "-g", pkg); err != nil {
		restore()
		return fmt.Errorf("update pippit-tool-cli: %w", err)
	}

	root, err := globalPackageRoot(defaultPackage)
	if err != nil {
		return fmt.Errorf("locate global package: %w", err)
	}

	fmt.Fprintln(stderr, "Updating pippit-tool-cli skills...")
	if err := installSkills(root, stderr); err != nil {
		return fmt.Errorf("update pippit-tool-cli skills: %w", err)
	}

	fmt.Fprintln(stdout, "pippit-tool-cli and skills updated")
	return nil
}

func globalPackageRoot(pkg string) (string, error) {
	out, err := command("npm", "root", "-g").Output()
	if err != nil {
		return "", err
	}
	npmRoot := strings.TrimSpace(string(out))
	if npmRoot == "" {
		return "", fmt.Errorf("npm root -g returned empty output")
	}
	return filepath.Join(append([]string{npmRoot}, strings.Split(pkg, "/")...)...), nil
}

func installSkills(root string, stderr io.Writer) error {
	if info, err := os.Stat(filepath.Join(root, "skills")); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", filepath.Join(root, "skills"))
	}
	if err := runInherit(stderr, "npx", "-y", "skills", "add", root, "-g", "-y", "--skill", "*"); err != nil {
		return err
	}
	return cleanupLegacyGlobalSkills(defaultGlobalSkillsDir())
}

func defaultGlobalSkillsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agents", "skills")
}

func cleanupLegacyGlobalSkills(globalSkillsDir string) error {
	if globalSkillsDir == "" {
		return nil
	}
	for _, skillName := range legacyGlobalSkills {
		if err := os.RemoveAll(filepath.Join(globalSkillsDir, skillName)); err != nil {
			return err
		}
	}
	return nil
}

func runInherit(stderr io.Writer, name string, args ...string) error {
	return runInheritEnv(stderr, nil, name, args...)
}

func runInheritEnv(stderr io.Writer, env []string, name string, args ...string) error {
	cmd := command(name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	return cmd.Run()
}

func command(name string, args ...string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		cmdArgs := append([]string{"/c", name}, args...)
		return exec.Command("cmd.exe", cmdArgs...)
	}
	return exec.Command(name, args...)
}

func prepareSelfReplace() (func(), error) {
	if runtime.GOOS != "windows" {
		return func() {}, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if filepath.Base(exe) != "pippit-tool-cli.exe" || !strings.Contains(exe, "node_modules") {
		return func() {}, nil
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return nil, err
	}
	return func() {
		if _, err := os.Stat(exe); err == nil {
			return
		}
		_ = os.Rename(old, exe)
	}, nil
}
