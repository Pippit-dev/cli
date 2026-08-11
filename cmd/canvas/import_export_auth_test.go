package canvas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeLibTVAuthResultStrictlyValidatesContract(t *testing.T) {
	valid := `{"schema":"pippit-libtv-auth-result/0.1","provider":"libtv","authenticated":true,"cli_version":"1.1.3","login_performed":false}`
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{name: "invalid json", payload: `{`, want: "不是有效的 JSON"},
		{name: "trailing json", payload: valid + `{}`, want: "只能包含一个值"},
		{name: "unknown field", payload: strings.Replace(valid, `}`, `,"extra":true}`, 1), want: "unknown field"},
		{name: "wrong schema", payload: strings.Replace(valid, libTVAuthResultSchema, "other/0.1", 1), want: "schema 无效"},
		{name: "wrong provider", payload: strings.Replace(valid, `"provider":"libtv"`, `"provider":"other"`, 1), want: "provider 无效"},
		{name: "not authenticated", payload: strings.Replace(valid, `"authenticated":true`, `"authenticated":false`, 1), want: "授权尚未完成"},
		{name: "missing version", payload: strings.Replace(valid, `"cli_version":"1.1.3"`, `"cli_version":""`, 1), want: "CLI 版本无效"},
		{name: "malformed version", payload: strings.Replace(valid, `"cli_version":"1.1.3"`, `"cli_version":"latest"`, 1), want: "CLI 版本无效"},
	}

	result, err := decodeLibTVAuthResult([]byte(valid))
	if err != nil {
		t.Fatalf("decodeLibTVAuthResult(valid) error = %v", err)
	}
	if result.Provider != "libtv" || !result.Authenticated || result.CLIVersion != "1.1.3" || result.LoginPerformed {
		t.Fatalf("decodeLibTVAuthResult(valid) = %#v", result)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeLibTVAuthResult([]byte(test.payload))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decodeLibTVAuthResult() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNodeLibTVExporterAuthenticateUsesDedicatedAuthCommand(t *testing.T) {
	root := t.TempDir()
	adapterDir := filepath.Join(root, "adapters", "libtv")
	if err := os.MkdirAll(adapterDir, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "args.json")
	result := map[string]any{
		"schema":          libTVAuthResultSchema,
		"provider":        "libtv",
		"authenticated":   true,
		"cli_version":     "1.1.3",
		"login_performed": false,
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	logLiteral, _ := json.Marshal(logPath)
	resultLiteral, _ := json.Marshal(string(resultJSON) + "\n")
	script := fmt.Sprintf(
		"import { writeFileSync } from 'node:fs';\n"+
			"writeFileSync(%s, JSON.stringify(process.argv.slice(2)));\n"+
			"process.stderr.write('LibTV 授权测试提示\\n');\n"+
			"process.stdout.write(%s);\n",
		logLiteral,
		resultLiteral,
	)
	if err := os.WriteFile(filepath.Join(adapterDir, "cli.mjs"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PIPPIT_CLI_PACKAGE_ROOT", root)

	exporter := nodeLibTVExporter{}
	var stderr bytes.Buffer
	if err := exporter.Authenticate(context.Background(), false, &stderr); err != nil {
		t.Fatalf("Authenticate(non-interactive) error = %v; stderr = %s", err, stderr.String())
	}
	assertLibTVAdapterArgs(t, logPath, []string{"auth", "--non-interactive"})
	if !strings.Contains(stderr.String(), "LibTV 授权测试提示") {
		t.Fatalf("Authenticate() stderr = %q, want adapter progress", stderr.String())
	}

	stderr.Reset()
	if err := exporter.Authenticate(context.Background(), true, &stderr); err != nil {
		t.Fatalf("Authenticate(interactive) error = %v; stderr = %s", err, stderr.String())
	}
	assertLibTVAdapterArgs(t, logPath, []string{"auth"})
}

func TestNodeLibTVExporterAuthenticateRejectsInvalidAdapterResult(t *testing.T) {
	root := t.TempDir()
	adapterDir := filepath.Join(root, "adapters", "libtv")
	if err := os.MkdirAll(adapterDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(adapterDir, "cli.mjs"),
		[]byte("process.stdout.write('{\"schema\":\"wrong\"}\\n');\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PIPPIT_CLI_PACKAGE_ROOT", root)

	err := (nodeLibTVExporter{}).Authenticate(context.Background(), true, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "解析 LibTV 授权结果失败") {
		t.Fatalf("Authenticate() error = %v, want strict result rejection", err)
	}
}

func TestNodeLibTVExporterExportKeepsDefensiveAuthCheckNonInteractive(t *testing.T) {
	root := t.TempDir()
	adapterDir := filepath.Join(root, "adapters", "libtv")
	if err := os.MkdirAll(adapterDir, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "args.json")
	resultJSON, err := json.Marshal(libTVExportResult{})
	if err != nil {
		t.Fatal(err)
	}
	logLiteral, _ := json.Marshal(logPath)
	resultLiteral, _ := json.Marshal(string(resultJSON) + "\n")
	script := fmt.Sprintf(
		"import { writeFileSync } from 'node:fs';\n"+
			"writeFileSync(%s, JSON.stringify(process.argv.slice(2)));\n"+
			"process.stdout.write(%s);\n",
		logLiteral,
		resultLiteral,
	)
	if err := os.WriteFile(filepath.Join(adapterDir, "cli.mjs"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PIPPIT_CLI_PACKAGE_ROOT", root)

	if _, err := (nodeLibTVExporter{}).Export(
		context.Background(), testLibTVURL, filepath.Join(root, "bundle"), &bytes.Buffer{},
	); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	assertLibTVAdapterArgs(t, logPath, []string{
		"export", "--url", testLibTVURL, "--output-dir", filepath.Join(root, "bundle"), "--non-interactive",
	})
}

func assertLibTVAdapterArgs(t *testing.T, path string, expected []string) {
	t.Helper()
	var actual []string
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("adapter args = %v, want %v", actual, expected)
	}
}
