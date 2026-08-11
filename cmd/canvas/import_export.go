package canvas

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/canvasplan"
)

const (
	maxLibTVExporterOutputBytes = 4 << 20
	libTVExportResultSchema     = "pippit-libtv-export-result/0.1"
)

type libTVExportResult struct {
	BundleDir         string             `json:"bundle_dir"`
	SnapshotPath      string             `json:"snapshot_path"`
	MediaManifestPath string             `json:"media_manifest_path"`
	PlanPath          string             `json:"plan_path"`
	Schema            string             `json:"schema"`
	PlanSchema        string             `json:"plan_schema"`
	Source            canvasplan.Source  `json:"source"`
	Media             []libTVExportMedia `json:"media"`
	MediaCount        int                `json:"media_count"`
	NodeCount         int                `json:"node_count"`
	GroupCount        int                `json:"group_count"`
	EdgeCount         int                `json:"edge_count"`
	DegradationCount  int                `json:"degradation_count"`
}

type libTVExportMedia struct {
	LogicalID string `json:"logical_id"`
	MediaType string `json:"media_type"`
	LocalPath string `json:"local_path"`
}

type nodeLibTVExporter struct{}

func (nodeLibTVExporter) Export(
	ctx context.Context,
	sourceURL string,
	outputDir string,
	stderr io.Writer,
) (*libTVExportResult, error) {
	root, err := findCLIPackageRoot()
	if err != nil {
		return nil, err
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("find Node.js for LibTV exporter: %w", err)
	}
	adapterPath := filepath.Join(root, "adapters", "libtv", "cli.mjs")
	command := exec.CommandContext(ctx, node, adapterPath,
		"export", "--url", sourceURL, "--output-dir", outputDir,
	)
	command.Env = sanitizedExporterEnv(os.Environ())
	command.Stdin = os.Stdin
	command.Stderr = stderr
	var stdout boundedBuffer
	stdout.maximum = maxLibTVExporterOutputBytes
	command.Stdout = &stdout
	if err := command.Run(); err != nil {
		if stdout.exceeded {
			return nil, fmt.Errorf("LibTV exporter output exceeds %d bytes", maxLibTVExporterOutputBytes)
		}
		return nil, fmt.Errorf("LibTV exporter failed: %w", err)
	}
	if stdout.exceeded {
		return nil, fmt.Errorf("LibTV exporter output exceeds %d bytes", maxLibTVExporterOutputBytes)
	}
	var result libTVExportResult
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode LibTV exporter result: %w", err)
	}
	if err := ensureImportJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode LibTV exporter result: %w", err)
	}
	return &result, nil
}

func ensureImportJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return fmt.Errorf("JSON input must contain exactly one value")
}

type boundedBuffer struct {
	bytes.Buffer
	maximum  int
	exceeded bool
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	remaining := buffer.maximum - buffer.Len()
	if remaining > 0 {
		written := len(value)
		if written > remaining {
			written = remaining
		}
		_, _ = buffer.Buffer.Write(value[:written])
	}
	if len(value) > remaining {
		buffer.exceeded = true
	}
	return len(value), nil
}

func sanitizedExporterEnv(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		upper := strings.ToUpper(strings.TrimSpace(key))
		if !allowedExporterEnvKey(upper) || !safeExporterEnvValue(upper, value) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func allowedExporterEnvKey(key string) bool {
	if strings.HasPrefix(key, "LC_") {
		return true
	}
	switch key {
	case "ALL_PROXY",
		"APPDATA",
		"COLORTERM",
		"COMSPEC",
		"CURL_CA_BUNDLE",
		"DBUS_SESSION_BUS_ADDRESS",
		"DISPLAY",
		"HOME",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"LANG",
		"LIBTV_CLI_BINARY",
		"LIBTV_CLI_PATH",
		"LIBTV_CONFIG_DIR",
		"LOCALAPPDATA",
		"NODE_EXTRA_CA_CERTS",
		"NO_PROXY",
		"PATH",
		"PATHEXT",
		"PIPPIT_CLI_LIBTV_CACHE_DIR",
		"SHELL",
		"SSL_CERT_DIR",
		"SSL_CERT_FILE",
		"SYSTEMROOT",
		"TEMP",
		"TERM",
		"TMP",
		"TMPDIR",
		"TZ",
		"USER",
		"USERNAME",
		"USERPROFILE",
		"WAYLAND_DISPLAY",
		"WINDIR",
		"XAUTHORITY",
		"XDG_CACHE_HOME",
		"XDG_CONFIG_HOME",
		"XDG_DATA_HOME",
		"XDG_RUNTIME_DIR":
		return true
	default:
		return false
	}
}

func safeExporterEnvValue(key, value string) bool {
	if key == "NO_PROXY" {
		return true
	}
	if key != "HTTP_PROXY" && key != "HTTPS_PROXY" && key != "ALL_PROXY" {
		return true
	}
	if strings.TrimSpace(value) == "" {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks", "socks4", "socks4a", "socks5", "socks5h":
		return true
	default:
		return false
	}
}

func findCLIPackageRoot() (string, error) {
	candidates := make([]string, 0, 3)
	if configured := strings.TrimSpace(os.Getenv("PIPPIT_CLI_PACKAGE_ROOT")); configured != "" {
		candidates = append(candidates, configured)
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		candidates = append(candidates, workingDirectory)
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(executable))
	}
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		for directory := filepath.Clean(absolute); ; directory = filepath.Dir(directory) {
			if _, duplicate := seen[directory]; !duplicate {
				seen[directory] = struct{}{}
				adapter := filepath.Join(directory, "adapters", "libtv", "cli.mjs")
				if info, statErr := os.Stat(adapter); statErr == nil && info.Mode().IsRegular() {
					return directory, nil
				}
			}
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
		}
	}
	return "", fmt.Errorf("locate packaged LibTV exporter; run through the @pippit-dev/cli entry point or set PIPPIT_CLI_PACKAGE_ROOT")
}

func newImportBundlePath(userCacheDir func() (string, error)) (string, string, error) {
	cacheDir, err := userCacheDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve canvas import cache directory: %w", err)
	}
	root := filepath.Join(cacheDir, "pippit-cli", "canvas-import", "exports")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", "", fmt.Errorf("create canvas import export directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", "", fmt.Errorf("secure canvas import export directory: %w", err)
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", "", fmt.Errorf("generate canvas import export directory: %w", err)
	}
	return root, filepath.Join(root, "export-"+hex.EncodeToString(random)), nil
}

func validateExportLocation(result *libTVExportResult, expectedBundleDir string) error {
	if result == nil {
		return fmt.Errorf("LibTV exporter returned no result")
	}
	expected, err := filepath.Abs(expectedBundleDir)
	if err != nil {
		return fmt.Errorf("resolve expected LibTV bundle path: %w", err)
	}
	bundle, err := filepath.Abs(strings.TrimSpace(result.BundleDir))
	if err != nil || filepath.Clean(bundle) != filepath.Clean(expected) {
		return fmt.Errorf("LibTV exporter returned an unexpected bundle directory")
	}
	for label, path := range map[string]string{
		"snapshot":       result.SnapshotPath,
		"media manifest": result.MediaManifestPath,
		"CanvasPlan":     result.PlanPath,
	} {
		if err := requireFileWithinBundle(path, bundle); err != nil {
			return fmt.Errorf("invalid LibTV %s path: %w", label, err)
		}
	}
	return nil
}

func requireFileWithinBundle(path, bundle string) error {
	if !filepath.IsAbs(strings.TrimSpace(path)) {
		return fmt.Errorf("path must be absolute")
	}
	realBundle, err := filepath.EvalSymlinks(bundle)
	if err != nil {
		return fmt.Errorf("resolve bundle directory: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve file: %w", err)
	}
	relative, err := filepath.Rel(realBundle, realPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes the export bundle")
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file")
	}
	return nil
}

func readCanvasPlan(path string) (canvasplan.Plan, error) {
	file, err := os.Open(path)
	if err != nil {
		return canvasplan.Plan{}, fmt.Errorf("open exported CanvasPlan: %w", err)
	}
	defer file.Close()
	plan, err := canvasplan.DecodePlan(file)
	if err != nil {
		return canvasplan.Plan{}, err
	}
	return plan, nil
}

func validateExportPlan(exported libTVExportResult, plan canvasplan.Plan) error {
	if exported.Schema != libTVExportResultSchema || exported.PlanSchema != canvasplan.PlanSchema {
		return fmt.Errorf("LibTV exporter returned unsupported schema %q", exported.Schema)
	}
	if exported.Source != plan.Source {
		return fmt.Errorf("LibTV exporter source does not match CanvasPlan source")
	}
	if exported.MediaCount != len(exported.Media) || exported.MediaCount != len(plan.RequiredMedia) {
		return fmt.Errorf("LibTV exporter media counts do not match CanvasPlan")
	}
	if exported.NodeCount != len(plan.Nodes) || exported.GroupCount != len(plan.Groups) ||
		exported.EdgeCount != len(plan.Edges) || exported.DegradationCount != len(plan.Degradations) {
		return fmt.Errorf("LibTV exporter counts do not match CanvasPlan")
	}
	requirements := make(map[string]canvasplan.MediaRequirement, len(plan.RequiredMedia))
	for _, requirement := range plan.RequiredMedia {
		requirements[requirement.LogicalID] = requirement
	}
	seen := make(map[string]struct{}, len(exported.Media))
	for _, media := range exported.Media {
		requirement, ok := requirements[media.LogicalID]
		if !ok || requirement.MediaType != media.MediaType {
			return fmt.Errorf("LibTV exporter media does not match CanvasPlan")
		}
		expectedPath, expectedErr := filepath.Abs(filepath.Join(
			exported.BundleDir,
			filepath.FromSlash(requirement.LocalPath),
		))
		actualPath, actualErr := filepath.Abs(strings.TrimSpace(media.LocalPath))
		if expectedErr != nil || actualErr != nil || filepath.Clean(actualPath) != filepath.Clean(expectedPath) {
			return fmt.Errorf("LibTV exporter media path does not match CanvasPlan")
		}
		if _, duplicate := seen[media.LogicalID]; duplicate {
			return fmt.Errorf("LibTV exporter returned duplicate media %q", media.LogicalID)
		}
		seen[media.LogicalID] = struct{}{}
	}
	return nil
}

func removeOwnedBundle(bundleDir, bundleRoot string) error {
	cleanRoot, err := filepath.Abs(bundleRoot)
	if err != nil {
		return err
	}
	cleanBundle, err := filepath.Abs(bundleDir)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(cleanRoot, cleanBundle)
	if err != nil || strings.Contains(relative, string(filepath.Separator)) ||
		!strings.HasPrefix(relative, "export-") {
		return fmt.Errorf("refusing to remove an unowned LibTV bundle")
	}
	return os.RemoveAll(cleanBundle)
}
