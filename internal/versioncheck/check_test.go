package versioncheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func testOptions(t *testing.T, transport roundTripFunc) options {
	t.Helper()
	return options{
		cacheFile: filepath.Join(t.TempDir(), "version-check.json"),
		now:       func() time.Time { return time.UnixMilli(1_800_000_000_000) },
		getenv:    func(string) string { return "" },
		client:    &http.Client{Timeout: requestTimeout, Transport: transport},
	}
}

func response(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestCheckUsesSharedCacheAndWarnsForEveryInvocation(t *testing.T) {
	fetches := 0
	opts := testOptions(t, func(request *http.Request) (*http.Response, error) {
		fetches++
		if request.Method != "GET" || request.URL.String() != latestURL || request.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected registry request: %s %s", request.Method, request.URL)
		}
		if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > requestTimeout {
			t.Fatal("registry request has no bounded context deadline")
		}
		return response(`{"version":"1.0.42","unrelated_metadata":true}`), nil
	})
	for index := range 2 {
		var stderr bytes.Buffer
		check(context.Background(), &stderr, "1.0.41", "login", opts)
		if strings.Count(stderr.String(), "New version available: 1.0.41 -> 1.0.42") != 1 ||
			!strings.Contains(stderr.String(), "pippit-tool-cli update") || !strings.Contains(stderr.String(), "authorization fails") {
			t.Fatalf("invocation %d notice = %q", index, stderr.String())
		}
	}
	if fetches != 1 {
		t.Fatalf("fetches = %d, want one daily lookup", fetches)
	}
	data, err := os.ReadFile(opts.cacheFile)
	if err != nil {
		t.Fatal(err)
	}
	var shared map[string]any
	if json.Unmarshal(data, &shared) != nil || shared["latest"] != "1.0.42" || shared["checkedAt"] != float64(opts.now().UnixMilli()) {
		t.Fatalf("cache does not match the npm wrapper contract: %s", data)
	}
	if info, err := os.Stat(opts.cacheFile); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cache permissions are not 0600: %v", err)
	}
	for _, current := range []string{"1.0.42", "1.0.43"} {
		var stderr bytes.Buffer
		check(context.Background(), &stderr, current, "status", opts)
		if stderr.Len() != 0 {
			t.Fatalf("up-to-date binary received a notice: %q", stderr.String())
		}
	}
	oldNow := opts.now()
	opts.now = func() time.Time { return oldNow.Add(cacheTTL) }
	check(context.Background(), io.Discard, "1.0.41", "status", opts)
	if fetches != 2 {
		t.Fatalf("expired daily cache fetches = %d", fetches)
	}
}

func TestCheckSkipsDisabledAndNonBusinessInvocations(t *testing.T) {
	for _, tc := range []struct {
		name, current, command, env string
	}{
		{"dev", "dev", "status", ""}, {"invalid", "1.0", "status", ""},
		{"empty command", "1.0.41", "", ""}, {"help", "1.0.41", "help", ""},
		{"version", "1.0.41", "version", ""}, {"install", "1.0.41", "install", ""},
		{"update", "1.0.41", "update", ""}, {"CI", "1.0.41", "status", "CI"},
		{"disabled", "1.0.41", "status", "PIPPIT_CLI_DISABLE_UPDATE_CHECK"},
		{"wrapper checked", "1.0.41", "status", "PIPPIT_CLI_UPDATE_CHECKED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := testOptions(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("skipped invocation queried registry")
				return nil, errors.New("unexpected network")
			})
			opts.getenv = func(key string) string {
				if key == tc.env {
					return "1"
				}
				return ""
			}
			var stderr bytes.Buffer
			check(context.Background(), &stderr, tc.current, tc.command, opts)
			if _, err := os.Stat(opts.cacheFile); !os.IsNotExist(err) || stderr.Len() != 0 {
				t.Fatal("skipped invocation wrote cache or output")
			}
		})
	}
}

func TestFailedChecksStaySilentAndShareFiveMinuteCooldown(t *testing.T) {
	for _, failure := range []string{"network", "http", "json", "version", "oversize"} {
		t.Run(failure, func(t *testing.T) {
			fetches := 0
			opts := testOptions(t, func(*http.Request) (*http.Response, error) {
				fetches++
				switch failure {
				case "network":
					return nil, errors.New("offline")
				case "http":
					result := response(`{}`)
					result.StatusCode = http.StatusServiceUnavailable
					return result, nil
				case "json":
					return response(`not json`), nil
				case "version":
					return response(`{"version":"dev"}`), nil
				default:
					return response(strings.Repeat(" ", maxResponseBytes+1)), nil
				}
			})
			var stderr bytes.Buffer
			check(context.Background(), &stderr, "1.0.41", "status", opts)
			cached := readCache(opts.cacheFile)
			if cached.Latest != "" || cached.CheckedAt != opts.now().UnixMilli() {
				t.Fatal("failed lookup did not write the shared failure cooldown")
			}
			initial := opts.now()
			opts.now = func() time.Time { return initial.Add(failureTTL - time.Millisecond) }
			check(context.Background(), &stderr, "1.0.41", "status", opts)
			if fetches != 1 || stderr.Len() != 0 {
				t.Fatal("failed lookup retried during cooldown or emitted output")
			}
			opts.now = func() time.Time { return initial.Add(failureTTL) }
			check(context.Background(), &stderr, "1.0.41", "status", opts)
			if fetches != 2 || stderr.Len() != 0 {
				t.Fatal("expired failure cooldown did not retry silently")
			}
		})
	}
}

func TestRegistryLookupHasOneSecondTotalDeadline(t *testing.T) {
	opts := testOptions(t, func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	start := time.Now()
	var stderr bytes.Buffer
	check(context.Background(), &stderr, "1.0.41", "status", opts)
	if elapsed := time.Since(start); elapsed > 2*time.Second || stderr.Len() != 0 {
		t.Fatalf("deadline exceeded budget or emitted output: %v / %q", elapsed, stderr.String())
	}
}

func TestRegistryOrCacheFailureCannotSuppressKnownNotice(t *testing.T) {
	opts := testOptions(t, func(*http.Request) (*http.Response, error) {
		return response(`{"version":"1.0.42"}`), nil
	})
	// A regular file in place of the cache directory makes cache persistence fail.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	opts.cacheFile = filepath.Join(blocker, "version-check.json")
	var stderr bytes.Buffer
	check(context.Background(), &stderr, "1.0.41", "status", opts)
	if !strings.Contains(stderr.String(), "New version available") {
		t.Fatal("cache write failure suppressed a valid update notice")
	}
}

func TestSemanticVersionOrderingRejectsInvalidAndDevelopmentVersions(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		newer           bool
	}{
		{"1.0.42", "1.0.41", true}, {"1.1.0", "1.0.99", true}, {"2.0.0", "1.99.99", true},
		{"1.0.41", "1.0.42", false}, {"1.0.42", "1.0.42+build.1", false},
		{"1.0.42", "1.0.42-canary.1", true}, {"1.0.42-canary.2", "1.0.42-canary.1", true},
		{"1.0.42-canary.10", "1.0.42-canary.2", true}, {"1.0.42-beta", "1.0.42-alpha.1", true},
		{"1.0.42-alpha.1", "1.0.42-alpha", true}, {"v1.0.42", "v1.0.41", true},
		{"1.0.42", "dev", false}, {"bad", "1.0.41", false}, {"1.0.42", "1.0", false},
		{"1.0.42", "1.00.41", false}, {"1.0.42", "1.0.41-01", false}, {"1.0.42", "1.0.41 junk", false},
	} {
		if got := isNewer(tc.latest, tc.current); got != tc.newer {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.newer)
		}
	}
}
