package updatecmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
)

func TestUpdateCommandSelectsChannel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell scripts to stub npm and npx")
	}
	cases := []struct {
		name     string
		args     []string
		override string
		want     string
		wantErr  bool
	}{
		{name: "default stable", want: defaultPackage + "@latest"},
		{name: "explicit stable", args: []string{"--channel", "stable"}, want: defaultPackage + "@latest"},
		{name: "explicit canary", args: []string{"--channel", "canary"}, want: defaultPackage + "@canary"},
		{name: "package override preserved", override: defaultPackage + "@1.0.38", want: defaultPackage + "@1.0.38"},
		{name: "stable overrides environment", args: []string{"--channel", "stable"}, override: defaultPackage + "@canary", want: defaultPackage + "@latest"},
		{name: "canary overrides environment", args: []string{"--channel", "canary"}, override: defaultPackage + "@1.0.38", want: defaultPackage + "@canary"},
		{name: "invalid channel", args: []string{"--channel", "beta"}, wantErr: true},
		{name: "invalid channel with override", args: []string{"--channel", "beta"}, override: defaultPackage + "@1.0.38", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binDir := t.TempDir()
			npmRoot := t.TempDir()
			capturePath := filepath.Join(t.TempDir(), "npm-args.txt")
			if err := os.MkdirAll(filepath.Join(npmRoot, "@pippit-dev", "cli", "skills"), 0o755); err != nil {
				t.Fatal(err)
			}
			npmScript := "#!/bin/sh\ncase \"$1\" in\ninstall) printf '%s\\n' \"$@\" > \"$CAPTURE_ARGS\";;\nroot) printf '%s\\n' \"$TEST_NPM_ROOT\";;\n*) exit 1;;\nesac\n"
			for name, script := range map[string]string{"npm": npmScript, "npx": "#!/bin/sh\nexit 0\n"} {
				if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CAPTURE_ARGS", capturePath)
			t.Setenv("TEST_NPM_ROOT", npmRoot)
			t.Setenv("PIPPIT_CLI_DISABLE_TELEMETRY", "1")
			t.Setenv("PIPPIT_CLI_INSTALL_PACKAGE", tc.override)
			var stdout, stderr bytes.Buffer
			cmd := NewCommand(&stdout, &stderr)
			cmd.SetArgs(append([]string{}, tc.args...))
			err := cmd.Execute()
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "不支持的更新渠道") {
					t.Fatalf("error = %v, want invalid channel error", err)
				}
				if _, err := os.Stat(capturePath); !os.IsNotExist(err) {
					t.Fatalf("invalid channel invoked npm or stat failed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("update failed: %v, stderr: %s", err, stderr.String())
			}
			got, err := os.ReadFile(capturePath)
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Join([]string{"install", "-g", "--prefer-online", "--foreground-scripts", tc.want, ""}, "\n")
			if string(got) != want {
				t.Fatalf("npm args = %q, want %q", got, want)
			}
		})
	}
}

func TestInstallSkillsInstallsAllBundledSkills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script to stub npx")
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	capturePath := filepath.Join(t.TempDir(), "args.txt")
	npxPath := filepath.Join(binDir, "npx")
	script := "#!/bin/sh\nif [ \"$npm_config_global\" = true ] && [ \"$1\" != --global=false ]; then exit 1; fi\nfor arg in \"$@\"; do printf '%s\\n' \"$arg\"; done > \"$CAPTURE_ARGS\"\n"
	if err := os.WriteFile(npxPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE_ARGS", capturePath)
	t.Setenv("npm_config_global", "true")
	t.Setenv("HOME", t.TempDir())

	var stderr bytes.Buffer
	if err := installSkills(root, &stderr); err != nil {
		t.Fatalf("installSkills() error = %v, stderr = %s", err, stderr.String())
	}

	gotBytes, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(gotBytes)), "\n")
	want := []string{"--global=false", "-y", "skills", "add", root, "-g", "-y", "--skill", "*"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("npx args mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestCleanupLegacyGlobalSkills(t *testing.T) {
	globalSkillsDir := t.TempDir()
	for _, skillName := range []string{"pippit-short-drama-skill", "xyq-nest-skill", "xyq-short-drama-skill", "xyq-skill"} {
		if err := os.Mkdir(filepath.Join(globalSkillsDir, skillName), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := cleanupLegacyGlobalSkills(globalSkillsDir); err != nil {
		t.Fatalf("cleanupLegacyGlobalSkills() error = %v", err)
	}

	for _, skillName := range []string{"pippit-short-drama-skill", "xyq-nest-skill"} {
		if _, err := os.Stat(filepath.Join(globalSkillsDir, skillName)); !os.IsNotExist(err) {
			t.Fatalf("legacy skill %s still exists or stat failed: %v", skillName, err)
		}
	}
	for _, skillName := range []string{"xyq-short-drama-skill", "xyq-skill"} {
		if _, err := os.Stat(filepath.Join(globalSkillsDir, skillName)); err != nil {
			t.Fatalf("new skill %s was not preserved: %v", skillName, err)
		}
	}
}

func TestReportSkillTelemetry(t *testing.T) {
	var gotAuth string
	var gotPayload telemetryPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != telemetryPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, telemetryPath)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ret":"0","errmsg":""}`))
	}))
	defer server.Close()

	t.Setenv("PIPPIT_CLI_TELEMETRY_BASE_URL", server.URL+"/")
	err := newUpdater(adapter.Default{}).reportSkillTelemetry(telemetryPayload{
		Event:        "update",
		SkillName:    "xyq-skill",
		Source:       "cli_update",
		HostPlatform: "workbuddy",
		CliVersion:   "0.0.26",
		Platform:     "darwin",
		Arch:         "arm64",
	})
	if err != nil {
		t.Fatalf("reportSkillTelemetry() error = %v", err)
	}
	if gotAuth != telemetryAuthHeader {
		t.Fatalf("Authorization = %q, want %q", gotAuth, telemetryAuthHeader)
	}
	if gotPayload.Event != "update" || gotPayload.SkillName != "xyq-skill" || gotPayload.Source != "cli_update" || gotPayload.HostPlatform != "workbuddy" {
		t.Fatalf("payload = %#v", gotPayload)
	}
}

func TestReportBundledSkillTelemetryWaitsBriefly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(`{"ret":"0","errmsg":""}`))
	}))
	defer server.Close()

	t.Setenv("PIPPIT_CLI_TELEMETRY_BASE_URL", server.URL)
	start := time.Now()
	newUpdater(adapter.Default{}).reportBundledSkillTelemetry("update", "cli_update", " workbuddy ", &bytes.Buffer{})
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("reportBundledSkillTelemetry() blocked for %v, want <= 1.5s", elapsed)
	}
}

func TestReportBundledSkillTelemetryReportsBothSkills(t *testing.T) {
	skillNames := make(chan string, len(telemetrySkillNames))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload telemetryPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload.Source != "cli_update" || payload.HostPlatform != "workbuddy" || payload.Platform != runtime.GOOS {
			t.Errorf("unexpected attribution: %#v", payload)
		}
		skillNames <- payload.SkillName
		_, _ = w.Write([]byte(`{"ret":"0","errmsg":""}`))
	}))
	defer server.Close()

	t.Setenv("PIPPIT_CLI_TELEMETRY_BASE_URL", server.URL)
	newUpdater(adapter.Default{}).reportBundledSkillTelemetry("update", "cli_update", " workbuddy ", &bytes.Buffer{})

	got := map[string]bool{}
	for i := 0; i < len(telemetrySkillNames); i++ {
		got[<-skillNames] = true
	}
	for _, skillName := range telemetrySkillNames {
		if !got[skillName] {
			t.Fatalf("missing telemetry for %s, got %#v", skillName, got)
		}
	}
}

func TestTelemetryBaseURL(t *testing.T) {
	t.Setenv("PIPPIT_CLI_TELEMETRY_BASE_URL", "   ")
	t.Setenv("XYQ_OPENAPI_BASE", "https://example.com///")
	if got := telemetryBaseURL(); got != "https://example.com" {
		t.Fatalf("telemetryBaseURL() = %q, want https://example.com", got)
	}
}

func TestStripPrereleaseVersion(t *testing.T) {
	cases := map[string]string{
		"0.0.27":       "0.0.27",
		"0.0.27-rc.1":  "0.0.27",
		"1.2.3-beta.4": "1.2.3",
	}
	for input, want := range cases {
		if got := stripPrereleaseVersion(input); got != want {
			t.Fatalf("stripPrereleaseVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRunInheritEnvSanitizesPippitCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script to capture the child environment")
	}

	binDir := t.TempDir()
	capturePath := filepath.Join(t.TempDir(), "environment.txt")
	commandPath := filepath.Join(binDir, "capture-update-environment")
	script := "#!/bin/sh\n/usr/bin/env > \"$CAPTURE_ENV\"\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	basePath := binDir + string(os.PathListSeparator) + "/base/path"
	overriddenPath := binDir + string(os.PathListSeparator) + "/overridden/path"
	t.Setenv("PATH", basePath)
	t.Setenv("CAPTURE_ENV", capturePath)
	t.Setenv("SAFE_INHERITED", "kept")
	t.Setenv("XYQ_ACCESS_KEY", "xyq-secret")
	t.Setenv("PIPPIT_ACCESS_KEY", "pippit-secret")
	t.Setenv("PIPPIT_AK", "legacy-secret")
	t.Setenv("PIPPIT_CLI_TOKEN", "pippit-token")
	t.Setenv("XYQ_CLIENT_SECRET", "xyq-client-secret")
	t.Setenv("NPM_TOKEN", "npm-secret")
	t.Setenv("NODE_AUTH_TOKEN", "registry-secret")

	var stderr bytes.Buffer
	err := runInheritEnv(&stderr, []string{
		"PATH=" + overriddenPath,
		"SAFE_INHERITED=overridden",
		"SAFE_EXPLICIT=kept",
		"PIPPIT_CLI_SKIP_SKILLS=1",
		"XYQ_ACCESS_KEY=override-must-not-leak",
		"PIPPIT_OVERRIDE_SECRET=override-must-not-leak",
		"REGISTRY_TOKEN=explicit-registry-secret",
	}, "capture-update-environment")
	if err != nil {
		t.Fatalf("runInheritEnv() error = %v, stderr = %s", err, stderr.String())
	}

	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	got := parseEnvironment(string(captured))
	for _, forbidden := range []string{
		"XYQ_ACCESS_KEY",
		"PIPPIT_ACCESS_KEY",
		"PIPPIT_AK",
		"PIPPIT_CLI_TOKEN",
		"XYQ_CLIENT_SECRET",
		"PIPPIT_OVERRIDE_SECRET",
	} {
		if value, exists := got[forbidden]; exists {
			t.Fatalf("child environment retained %s=%q", forbidden, value)
		}
	}
	for name, want := range map[string]string{
		"PATH":                   overriddenPath,
		"CAPTURE_ENV":            capturePath,
		"SAFE_INHERITED":         "overridden",
		"SAFE_EXPLICIT":          "kept",
		"PIPPIT_CLI_SKIP_SKILLS": "1",
		"NPM_TOKEN":              "npm-secret",
		"NODE_AUTH_TOKEN":        "registry-secret",
		"REGISTRY_TOKEN":         "explicit-registry-secret",
	} {
		if value := got[name]; value != want {
			t.Fatalf("child environment %s = %q, want %q", name, value, want)
		}
	}
}

func parseEnvironment(environment string) map[string]string {
	result := make(map[string]string)
	for _, entry := range strings.Split(environment, "\n") {
		name, value, found := strings.Cut(entry, "=")
		if found {
			result[name] = value
		}
	}
	return result
}

func TestTelemetryOmitsUnknownHostPlatform(t *testing.T) {
	body, err := json.Marshal(telemetryPayload{Event: "update", Source: "cli_update"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte(`"host_platform"`)) {
		t.Fatalf("unknown host_platform must be omitted: %s", body)
	}
}
