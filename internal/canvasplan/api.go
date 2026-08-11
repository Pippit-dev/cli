package canvasplan

import (
	"context"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/canvas"
	"github.com/Pippit-dev/pippit-cli/internal/common"
)

type canvasAPI interface {
	Create(context.Context, canvas.CreateOptions) (*canvas.CreateResult, error)
	ResumeCreate(context.Context, *canvas.CreateResult, canvas.ResumeCreateOptions) (*canvas.CreateResult, error)
	Allocate(context.Context, int) (*canvas.AllocateResult, error)
	Get(context.Context, []string) (*canvas.GetResult, error)
	GetExisting(context.Context, []string) (*canvas.GetResult, error)
	Apply(context.Context, canvas.ApplyOptions) (*canvas.ApplyResult, error)
}

type runnerCanvasAPI struct {
	runner *common.Runner
}

func (api runnerCanvasAPI) Create(ctx context.Context, opts canvas.CreateOptions) (*canvas.CreateResult, error) {
	return canvas.Create(ctx, opts, api.runner)
}

func (api runnerCanvasAPI) ResumeCreate(
	ctx context.Context,
	accepted *canvas.CreateResult,
	opts canvas.ResumeCreateOptions,
) (*canvas.CreateResult, error) {
	return canvas.ResumeCreate(ctx, accepted, opts, api.runner)
}

func (api runnerCanvasAPI) Allocate(ctx context.Context, count int) (*canvas.AllocateResult, error) {
	return canvas.Allocate(ctx, count, api.runner)
}

func (api runnerCanvasAPI) Get(ctx context.Context, assetIDs []string) (*canvas.GetResult, error) {
	return canvas.Get(ctx, canvas.GetOptions{AssetIDs: assetIDs}, api.runner)
}

func (api runnerCanvasAPI) GetExisting(ctx context.Context, assetIDs []string) (*canvas.GetResult, error) {
	return canvas.GetExisting(ctx, assetIDs, api.runner)
}

func (api runnerCanvasAPI) Apply(ctx context.Context, opts canvas.ApplyOptions) (*canvas.ApplyResult, error) {
	return canvas.Apply(ctx, opts, api.runner)
}

type Executor struct {
	api canvasAPI
}

func NewExecutor(runner *common.Runner) *Executor {
	return &Executor{api: runnerCanvasAPI{runner: runner}}
}

// Execute materializes a provider-neutral CanvasPlan into a personal novel
// canvas. An existing journal is resumed automatically and is never replayed
// after an ambiguous create or apply request.
func Execute(
	ctx context.Context,
	plan Plan,
	resolved ResolvedMediaSet,
	opts ExecuteOptions,
	runner *common.Runner,
) (*ExecutionResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("CanvasPlan runner client is missing")
	}
	return NewExecutor(runner).Execute(ctx, plan, resolved, opts)
}

// Resume is the existing-journal-only form of Execute. It fails without
// creating a journal when the requested operation has not been started.
func Resume(
	ctx context.Context,
	plan Plan,
	resolved ResolvedMediaSet,
	opts ExecuteOptions,
	runner *common.Runner,
) (*ExecutionResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("CanvasPlan runner client is missing")
	}
	return NewExecutor(runner).Resume(ctx, plan, resolved, opts)
}
