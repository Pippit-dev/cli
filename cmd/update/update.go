package updatecmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/version"
	"github.com/spf13/cobra"
)

const defaultPackage = "@pippit-dev/cli"

const (
	defaultTelemetryBaseURL = "https://xyq.jianying.com"
	telemetryPath           = "/api/biz/v1/skill/report_telemetry"
	telemetryAuthHeader     = "Bearer pippit-cli-skill-telemetry"
	telemetryWaitTimeout    = time.Second
)

var legacyGlobalSkills = []string{
	"pippit-short-drama-skill",
	"xyq-nest-skill",
}

var telemetrySkillNames = []string{
	"xyq-skill",
	"xyq-short-drama-skill",
}

var telemetryHTTPClient = &http.Client{Timeout: telemetryWaitTimeout}

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
		return fmt.Errorf("准备替换当前可执行文件失败: %w", err)
	}
	if err := runInheritEnv(stderr, []string{"PIPPIT_CLI_SKIP_SKILLS=1"}, "npm", "install", "-g", pkg); err != nil {
		restore()
		return fmt.Errorf("更新 pippit-tool-cli 失败: %w", err)
	}

	root, err := globalPackageRoot(defaultPackage)
	if err != nil {
		return fmt.Errorf("定位全局 npm 包失败: %w", err)
	}

	fmt.Fprintln(stderr, "Updating pippit-tool-cli skills...")
	if err := installSkills(root, stderr); err != nil {
		return fmt.Errorf("更新 pippit-tool-cli skills 失败: %w", err)
	}

	reportBundledSkillTelemetry("update", "cli_update", stderr)
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
		return "", fmt.Errorf("npm root -g 输出为空")
	}
	return filepath.Join(append([]string{npmRoot}, strings.Split(pkg, "/")...)...), nil
}

func installSkills(root string, stderr io.Writer) error {
	if info, err := os.Stat(filepath.Join(root, "skills")); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("%s 不是目录", filepath.Join(root, "skills"))
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

type telemetryPayload struct {
	Event      string `json:"event"`
	SkillName  string `json:"skill_name"`
	Source     string `json:"source"`
	CliVersion string `json:"cli_version"`
	Platform   string `json:"platform"`
	Arch       string `json:"arch"`
}

func reportBundledSkillTelemetry(event string, source string, stderr io.Writer) {
	if os.Getenv("PIPPIT_CLI_DISABLE_TELEMETRY") == "1" {
		return
	}
	var wg sync.WaitGroup
	for _, skillName := range telemetrySkillNames {
		payload := telemetryPayload{
			Event:      event,
			SkillName:  skillName,
			Source:     source,
			CliVersion: telemetryCliVersion(),
			Platform:   runtime.GOOS,
			Arch:       runtime.GOARCH,
		}
		wg.Add(1)
		go func(payload telemetryPayload) {
			defer wg.Done()
			if err := reportSkillTelemetry(payload); err != nil && os.Getenv("PIPPIT_CLI_DEBUG_TELEMETRY") == "1" {
				fmt.Fprintf(stderr, "[pippit-tool-cli] 埋点上报失败: %v\n", err)
			}
		}(payload)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(telemetryWaitTimeout):
	}
}

func reportSkillTelemetry(payload telemetryPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, telemetryBaseURL()+telemetryPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", telemetryAuthHeader)

	resp, err := telemetryHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("埋点请求返回 HTTP %d", resp.StatusCode)
	}
	return nil
}

func telemetryBaseURL() string {
	for _, key := range []string{"PIPPIT_CLI_TELEMETRY_BASE_URL", "XYQ_OPENAPI_BASE", "XYQ_BASE_URL"} {
		if value := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/"); value != "" {
			return value
		}
	}
	return defaultTelemetryBaseURL
}

func telemetryCliVersion() string {
	return stripPrereleaseVersion(version.Current())
}

func stripPrereleaseVersion(value string) string {
	if idx := strings.Index(value, "-"); idx >= 0 {
		return value[:idx]
	}
	return value
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
