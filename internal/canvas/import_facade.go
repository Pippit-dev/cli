package canvas

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

// ResumeCreateOptions controls polling an already accepted create operation.
// It never creates a second project.
type ResumeCreateOptions struct {
	PollInterval time.Duration
	WaitTimeout  time.Duration
}

// ResumeCreate waits for an already accepted create operation and preserves
// its IDs on transient polling failures. Callers must not use it without a
// previously returned CreateResult.
func ResumeCreate(
	ctx context.Context,
	accepted *CreateResult,
	opts ResumeCreateOptions,
	runner *common.Runner,
) (*CreateResult, error) {
	if accepted == nil {
		return nil, fmt.Errorf("accepted canvas create result is required")
	}
	result := *accepted
	result.Warning = ""
	if err := validateCreateResult(&result); err != nil {
		return nil, err
	}
	if opts.PollInterval < 0 || opts.WaitTimeout < 0 {
		return nil, fmt.Errorf("canvas create polling durations must not be negative")
	}
	if result.OverviewPippitAssetID != "" {
		return finalizeReadyCanvas(ctx, &result, runner)
	}
	artifact, attempts, waitErr := waitForCreationArtifact(
		ctx,
		runner,
		result.ThreadID,
		result.RunID,
		opts.PollInterval,
		opts.WaitTimeout,
	)
	result.PollAttempts += attempts
	if waitErr != nil {
		result.Warning = waitErr.Error()
		if IsCredentialUnavailable(waitErr) {
			return &result, acceptedCreationError(&result, waitErr)
		}
		var terminal *CreationTerminalError
		if errors.As(waitErr, &terminal) {
			result.State = "failed"
			return &result, acceptedCreationError(&result, waitErr)
		}
		result.State = StateCreating
		return &result, nil
	}
	if artifact.CanvasAssetID != "" && artifact.CanvasAssetID != result.CanvasAssetID {
		result.State = "failed"
		result.Warning = fmt.Sprintf(
			"canvas create artifact canvas_asset_id mismatch: got %q, want %q",
			artifact.CanvasAssetID,
			result.CanvasAssetID,
		)
		return &result, acceptedCreationError(&result, errors.New(result.Warning))
	}
	result.OverviewPippitAssetID = strings.TrimSpace(artifact.OverviewPippitAssetID)
	return finalizeReadyCanvas(ctx, &result, runner)
}

// GetExisting returns the requested assets that currently exist without
// treating absent IDs as a protocol error. The response envelope and every
// returned ID remain strictly validated.
func GetExisting(ctx context.Context, assetIDs []string, runner *common.Runner) (*GetResult, error) {
	normalized, err := normalizeAssetIDs(assetIDs)
	if err != nil {
		return nil, err
	}
	return queryAssets(ctx, normalized, false, runner)
}
