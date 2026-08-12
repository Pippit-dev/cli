package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const defaultCreationPollInterval = time.Second

var terminalCreationStates = map[string]struct{}{
	"4": {}, "5": {}, "9": {}, "failed": {}, "canceled": {}, "hitl_interrupt": {},
}

type CreateOptions struct {
	Title        string
	RequestID    string
	Wait         bool
	PollInterval time.Duration
	WaitTimeout  time.Duration
}

type CreateResult struct {
	RequestID             string `json:"request_id"`
	State                 string `json:"state"`
	ProjectID             string `json:"project_id"`
	ThreadID              string `json:"thread_id"`
	RunID                 string `json:"run_id"`
	CanvasAssetID         string `json:"canvas_asset_id"`
	OverviewPippitAssetID string `json:"overview_pippit_asset_id,omitempty"`
	WebURL                string `json:"web_url"`
	LogID                 string `json:"log_id,omitempty"`
	PollAttempts          int    `json:"poll_attempts,omitempty"`
	Warning               string `json:"warning,omitempty"`
}

type CreationTerminalError struct {
	State string
}

func (e *CreationTerminalError) Error() string {
	return fmt.Sprintf("canvas creation reached terminal state %q", strings.TrimSpace(e.State))
}

type createRequest struct {
	Surface   string         `json:"surface"`
	Title     string         `json:"title,omitempty"`
	RequestID string         `json:"request_id"`
	Base      map[string]any `json:"Base"`
}

type createData struct {
	State                 string `json:"state"`
	ProjectID             string `json:"project_id"`
	ThreadID              string `json:"thread_id"`
	RunID                 string `json:"run_id"`
	CanvasAssetID         string `json:"canvas_asset_id"`
	OverviewPippitAssetID string `json:"overview_pippit_asset_id"`
	WebURL                string `json:"web_url"`
}

func Create(ctx context.Context, opts CreateOptions, runner *common.Runner) (*CreateResult, error) {
	client, err := runnerClient(runner, "canvas create")
	if err != nil {
		return nil, err
	}
	if opts.PollInterval < 0 || opts.WaitTimeout < 0 {
		return nil, fmt.Errorf("canvas create polling durations must not be negative")
	}
	requestID := strings.TrimSpace(opts.RequestID)
	if requestID == "" {
		return nil, fmt.Errorf("canvas create request_id is required")
	}
	if len([]rune(requestID)) > 128 {
		return nil, fmt.Errorf("canvas create request_id must not exceed 128 characters")
	}
	title := strings.TrimSpace(opts.Title)
	if len([]rune(title)) > 50 {
		return nil, fmt.Errorf("canvas create title must not exceed 50 characters")
	}

	var envelope responseEnvelope
	err = client.SendRequest(ctx, CreatePath, createRequest{
		Surface:   SurfaceNovel,
		Title:     title,
		RequestID: requestID,
		Base:      base(),
	}, &envelope)
	if err != nil {
		if IsCredentialUnavailable(err) {
			// The authorizer rejected before http.Client.Do, or the service
			// explicitly rejected authentication with 401/403. Both prove that
			// no create write was accepted and are safe to retry after login.
			return nil, fmt.Errorf("canvas create was not accepted because authentication is unavailable or rejected: %w", err)
		}
		return nil, fmt.Errorf("canvas create request failed; outcome may be ambiguous, do not retry blindly: %w", err)
	}
	if err := envelope.validate("canvas create"); err != nil {
		return nil, err
	}
	var data createData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, common.NewLogIDError(fmt.Sprintf("canvas create returned invalid data: %v", err), envelope.LogID)
	}
	result := &CreateResult{
		RequestID:             requestID,
		State:                 strings.TrimSpace(data.State),
		ProjectID:             strings.TrimSpace(data.ProjectID),
		ThreadID:              strings.TrimSpace(data.ThreadID),
		RunID:                 strings.TrimSpace(data.RunID),
		CanvasAssetID:         strings.TrimSpace(data.CanvasAssetID),
		OverviewPippitAssetID: strings.TrimSpace(data.OverviewPippitAssetID),
		WebURL:                strings.TrimSpace(data.WebURL),
		LogID:                 strings.TrimSpace(envelope.LogID),
	}
	if err := validateCreateResult(result); err != nil {
		return nil, common.NewLogIDError(err.Error(), envelope.LogID)
	}
	if !opts.Wait {
		return result, nil
	}
	if result.OverviewPippitAssetID != "" {
		return finalizeReadyCanvas(ctx, result, runner)
	}

	artifact, attempts, waitErr := waitForCreationArtifact(ctx, runner, result.ThreadID, result.RunID, opts.PollInterval, opts.WaitTimeout)
	result.PollAttempts = attempts
	if waitErr != nil {
		result.Warning = waitErr.Error()
		if IsCredentialUnavailable(waitErr) {
			return result, acceptedCreationError(result, waitErr)
		}
		var terminal *CreationTerminalError
		if errors.As(waitErr, &terminal) {
			result.State = "failed"
			return result, acceptedCreationError(result, waitErr)
		}
		// The create response was already accepted. A timeout or transient
		// get_thread failure must preserve the operation IDs and must not make
		// callers assume it is safe to create another project.
		return result, nil
	}
	if artifact.CanvasAssetID != "" && artifact.CanvasAssetID != result.CanvasAssetID {
		result.State = "failed"
		result.Warning = fmt.Sprintf("canvas create artifact canvas_asset_id mismatch: got %q, want %q", artifact.CanvasAssetID, result.CanvasAssetID)
		return result, acceptedCreationError(result, errors.New(result.Warning))
	}
	result.OverviewPippitAssetID = artifact.OverviewPippitAssetID
	return finalizeReadyCanvas(ctx, result, runner)
}

func finalizeReadyCanvas(ctx context.Context, result *CreateResult, runner *common.Runner) (*CreateResult, error) {
	// Preserve the confirmed overview locator even if the final root visibility
	// check is temporarily unavailable. The state remains creating until that
	// check succeeds, but callers can resume with the canonical project URL.
	result.WebURL = readyCanvasURL(result.WebURL, result.ProjectID, result.OverviewPippitAssetID)
	if _, err := queryAssets(ctx, []string{result.CanvasAssetID}, true, runner); err != nil {
		result.State = StateCreating
		result.Warning = fmt.Sprintf("canvas overview is complete but the root asset is not queryable yet: %v", err)
		if IsCredentialUnavailable(err) {
			return result, acceptedCreationError(result, err)
		}
		return result, nil
	}
	result.State = StateReady
	return result, nil
}

// IsCredentialUnavailable identifies local credential state errors that the
// shared authorizer returns before issuing an HTTP request. errors.Is remains
// intact through every canvas/create wrapper so callers never infer safety
// from user-facing warning text.
func IsCredentialUnavailable(err error) bool {
	return errors.Is(err, internal_auth.ErrCredentialNotFound) ||
		errors.Is(err, internal_auth.ErrCredentialExpired) ||
		errors.Is(err, internal_auth.ErrCredentialRejected)
}

func acceptedCreationError(result *CreateResult, cause error) error {
	return fmt.Errorf(
		"%w; accepted canvas IDs: project_id=%s thread_id=%s run_id=%s canvas_asset_id=%s; do not create again blindly",
		cause, result.ProjectID, result.ThreadID, result.RunID, result.CanvasAssetID,
	)
}

func validateCreateResult(result *CreateResult) error {
	if result == nil {
		return fmt.Errorf("canvas create response is empty")
	}
	missing := make([]string, 0, 4)
	for name, value := range map[string]string{
		"project_id": result.ProjectID, "thread_id": result.ThreadID,
		"run_id": result.RunID, "canvas_asset_id": result.CanvasAssetID,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("canvas create response is missing %s", strings.Join(missing, ", "))
	}
	if result.State == "" {
		return fmt.Errorf("canvas create response is missing state")
	}
	if result.WebURL == "" {
		return fmt.Errorf("canvas create response is missing web_url")
	}
	return nil
}

type creationArtifact struct {
	OverviewPippitAssetID string
	CanvasAssetID         string
}

func waitForCreationArtifact(
	ctx context.Context,
	runner *common.Runner,
	threadID, runID string,
	pollInterval, waitTimeout time.Duration,
) (creationArtifact, int, error) {
	if pollInterval <= 0 {
		pollInterval = defaultCreationPollInterval
	}
	if waitTimeout <= 0 {
		waitTimeout = 2 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()
	attempts := 0
	for {
		attempts++
		result, err := common.GetThread(waitCtx, &common.GetThreadOptions{ThreadID: threadID, RunID: runID}, runner)
		if err != nil {
			if waitCtx.Err() != nil {
				return creationArtifact{}, attempts, creationWaitError(waitCtx.Err(), waitTimeout)
			}
			return creationArtifact{}, attempts, fmt.Errorf("poll canvas creation: %w", err)
		}
		run := findRunByID(result.RawData, runID)
		if run != nil {
			state := normalizeState(run["state"])
			artifact := findCreationArtifact(run)
			if (state == "3" || state == "completed") && artifact.OverviewPippitAssetID != "" {
				return artifact, attempts, nil
			}
			if _, terminal := terminalCreationStates[state]; terminal {
				return creationArtifact{}, attempts, &CreationTerminalError{State: state}
			}
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return creationArtifact{}, attempts, creationWaitError(waitCtx.Err(), waitTimeout)
		case <-timer.C:
		}
	}
}

func creationWaitError(err error, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("canvas creation artifact was not ready within %s", timeout)
	}
	return fmt.Errorf("canvas creation wait canceled: %w", err)
}

func findRunByID(raw json.RawMessage, runID string) map[string]any {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return walkForRun(value, runID, 0)
}

func walkForRun(value any, runID string, depth int) map[string]any {
	if depth > 14 {
		return nil
	}
	switch typed := parseJSONValue(value).(type) {
	case map[string]any:
		if stringValue(firstValue(typed, "run_id", "runId")) == runID {
			return typed
		}
		for _, child := range typed {
			if found := walkForRun(child, runID, depth+1); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := walkForRun(child, runID, depth+1); found != nil {
				return found
			}
		}
	}
	return nil
}

func findCreationArtifact(value any) creationArtifact {
	artifact := creationArtifact{}
	walkArtifact(parseJSONValue(value), 0, &artifact)
	return artifact
}

func walkArtifact(value any, depth int, artifact *creationArtifact) {
	if depth > 14 || (artifact.OverviewPippitAssetID != "" && artifact.CanvasAssetID != "") {
		return
	}
	switch typed := parseJSONValue(value).(type) {
	case map[string]any:
		if stringValue(firstValue(typed, "sub_type", "subType")) == overviewPartSubtype {
			data, _ := parseJSONValue(typed["data"]).(map[string]any)
			if data == nil {
				data = map[string]any{}
			}
			artifact.OverviewPippitAssetID = firstNonEmpty(
				stringValue(firstValue(data, "pippit_asset_id", "pippitAssetId")),
				stringValue(firstValue(typed, "pippit_asset_id", "pippitAssetId")),
			)
			artifact.CanvasAssetID = stringValue(firstValue(data, "canvas_asset_id", "canvasAssetId"))
		}
		for _, child := range typed {
			walkArtifact(child, depth+1, artifact)
		}
	case []any:
		for _, child := range typed {
			walkArtifact(child, depth+1, artifact)
		}
	}
}

func parseJSONValue(value any) any {
	text, ok := value.(string)
	if !ok {
		return value
	}
	text = strings.TrimSpace(text)
	if text == "" || (text[0] != '{' && text[0] != '[') {
		return value
	}
	var parsed any
	if json.Unmarshal([]byte(text), &parsed) == nil {
		return parsed
	}
	return value
}

func firstValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return ""
	}
}

func normalizeState(value any) string {
	return strings.ToLower(stringValue(value))
}

func readyCanvasURL(raw, projectID, overviewID string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		parsed, _ = url.Parse("https://xyq.jianying.com/novel/detail/canvas")
	}
	query := parsed.Query()
	query.Set("projectId", projectID)
	query.Set("overviewPippitAssetId", overviewID)
	query.Del("canvasId")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
