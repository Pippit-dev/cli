// Package versioncheck reports available CLI updates without changing command results.
package versioncheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	latestURL        = "https://registry.npmjs.org/@pippit-dev%2Fcli/latest"
	cacheTTL         = 24 * time.Hour
	failureTTL       = 5 * time.Minute
	requestTimeout   = time.Second
	maxResponseBytes = 64 << 10
)

type cache struct {
	Latest    string `json:"latest"`
	CheckedAt int64  `json:"checkedAt"`
}

type options struct {
	cacheFile string
	now       func() time.Time
	getenv    func(string) string
	client    *http.Client
}

// Check writes notices only to stderr. Registry, cache and output failures never
// affect the business command, and no background work outlives this call.
func Check(ctx context.Context, stderr io.Writer, current, command string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	check(ctx, stderr, current, command, options{
		cacheFile: filepath.Join(home, ".pippit_tool_cli", "version-check.json"),
		now:       time.Now,
		getenv:    os.Getenv,
		client: &http.Client{
			Timeout: requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	})
}

func check(ctx context.Context, stderr io.Writer, current, command string, opts options) {
	if command == "" || command == "help" || command == "version" || command == "install" || command == "update" ||
		opts.getenv("CI") != "" || opts.getenv("PIPPIT_CLI_DISABLE_UPDATE_CHECK") == "1" ||
		opts.getenv("PIPPIT_CLI_UPDATE_CHECKED") == "1" {
		return
	}
	if _, ok := parseVersion(current); !ok {
		return
	}
	now := opts.now()
	cached := readCache(opts.cacheFile)
	ttl := cacheTTL
	if cached.Latest == "" {
		ttl = failureTTL
	}
	age := now.Sub(time.UnixMilli(cached.CheckedAt))
	if cached.CheckedAt <= 0 || age < 0 || age >= ttl {
		cached = cache{Latest: fetchLatest(ctx, opts.client), CheckedAt: now.UnixMilli()}
		writeCache(opts.cacheFile, cached)
	}
	if !isNewer(cached.Latest, current) || stderr == nil {
		return
	}
	_, _ = fmt.Fprintf(stderr, "[pippit-tool-cli] New version available: %s -> %s. Run: pippit-tool-cli update\n", current, cached.Latest)
	if command == "login" {
		_, _ = fmt.Fprintln(stderr, "[pippit-tool-cli] If browser authorization fails, update the CLI and run pippit-tool-cli login again.")
	}
}

func fetchLatest(ctx context.Context, client *http.Client) string {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return ""
	}
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	if _, ok := parseVersion(pkg.Version); !ok {
		return ""
	}
	return pkg.Version
}

func readCache(path string) cache {
	file, err := os.Open(path)
	if err != nil {
		return cache{}
	}
	defer file.Close()
	var value cache
	if json.NewDecoder(io.LimitReader(file, maxResponseBytes)).Decode(&value) != nil {
		return cache{}
	}
	if value.Latest != "" {
		if _, ok := parseVersion(value.Latest); !ok {
			return cache{}
		}
	}
	return value
}

func writeCache(path string, value cache) {
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".version-check-*")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(value)
	closeErr := file.Close()
	if err == nil && closeErr == nil {
		_ = os.Rename(file.Name(), path)
	}
}
