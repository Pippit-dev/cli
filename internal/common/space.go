package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/space"
)

type spaceContextKey struct{}
type spaceRequestKey struct{}
type spaceRequest struct {
	operation string
	refs      space.References
}

func WithSpace(ctx context.Context, session *space.Session) context.Context {
	return context.WithValue(ctx, spaceContextKey{}, session)
}

func SpaceSession(ctx context.Context) *space.Session {
	session, _ := ctx.Value(spaceContextKey{}).(*space.Session)
	return session
}

func SpaceTeamID(ctx context.Context) string { return SpaceSession(ctx).TeamID() }

// spaceOperation classifies successful responses for local records only.
// It does not decide whether a request carries the selected TeamID.
func spaceOperation(path string) string {
	u, err := url.Parse(path)
	if err != nil {
		return ""
	}
	switch u.Path {
	case config.SubmitRunPath, config.SubmitMarketingRunPath:
		return "submit"
	case config.GetThreadPath, config.QueryGenerateVideoResultPath:
		return "query"
	case config.ListThreadFilePath:
		return "files"
	case config.UploadFilePath:
		return "upload"
	}
	return ""
}

func SpaceReferences(body any) (space.References, error) {
	var ref space.References
	if body == nil {
		return ref, nil
	}
	data, err := json.Marshal(body)
	if err != nil {
		return ref, err
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		return ref, fmt.Errorf("任务或素材 ID 格式无效: %w", err)
	}
	return ref, nil
}

func (c *httpClient) prepareSpaceRequest(ctx context.Context, path string, body any) (context.Context, error) {
	session := SpaceSession(ctx)
	if session == nil {
		return ctx, nil
	}
	if target, err := url.Parse(path); err == nil && target.IsAbs() {
		trusted, err := c.isBaseURLOrigin(target)
		if err != nil {
			return ctx, err
		}
		if !trusted {
			return ctx, nil
		}
	}
	ref, err := SpaceReferences(body)
	if err != nil {
		return ctx, err
	}
	if _, err := session.Resolve(ref); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, spaceRequestKey{}, spaceRequest{operation: spaceOperation(path), refs: ref}), nil
}

func rememberSpaceResponse(ctx context.Context, data []byte) {
	session := SpaceSession(ctx)
	request, ok := ctx.Value(spaceRequestKey{}).(spaceRequest)
	if session == nil || !ok || request.operation == "" {
		return
	}
	var response struct {
		Ret  json.RawMessage `json:"ret"`
		Data *struct {
			ThreadID      string `json:"thread_id"`
			RunID         string `json:"run_id"`
			PippitAssetID string `json:"pippit_asset_id"`
			Run           struct {
				ThreadID string `json:"thread_id"`
				RunID    string `json:"run_id"`
			} `json:"run"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &response) != nil || response.Data == nil || (string(response.Ret) != `"0"` && string(response.Ret) != "0") {
		return
	}
	ref := request.refs
	switch request.operation {
	case "submit":
		if response.Data.Run.ThreadID == "" || response.Data.Run.RunID == "" {
			return
		}
		if ref.ThreadID != "" && ref.ThreadID != response.Data.Run.ThreadID {
			return
		}
		ref.ThreadID, ref.RunID = response.Data.Run.ThreadID, response.Data.Run.RunID
	case "query", "files":
		if ref.ThreadID == "" {
			return
		}
		if response.Data.ThreadID != "" && response.Data.ThreadID != ref.ThreadID {
			return
		}
		if response.Data.RunID != "" && response.Data.RunID != ref.RunID {
			return
		}
	case "upload":
		if response.Data.PippitAssetID == "" {
			return
		}
		ref.AssetIDs = []string{response.Data.PippitAssetID}
	default:
		return
	}
	session.Remember(ref)
}

func SpaceAccount(ctx context.Context, runner *Runner) (string, error) {
	// Without an AuthManager, authentication belongs to the adapter. An unused
	// environment AK must not stand in for the adapter's actual account.
	if runner.Auth == nil {
		return "", nil
	}
	if provider, ok := runner.Auth.(interface {
		SpaceAccount(context.Context) (string, error)
	}); ok {
		return provider.SpaceAccount(ctx)
	}
	status, err := runner.Auth.Status(ctx)
	if err != nil {
		return "", err
	}
	if status != nil && status.UID != "" {
		return "user:" + space.Digest(status.UID), nil
	}
	return "", nil
}

// SpaceStoreForRunner returns nil when the current account cannot be identified.
// Callers can use an explicit space without persisting local records.
func SpaceStoreForRunner(ctx context.Context, runner *Runner) (*space.Store, error) {
	account, err := SpaceAccount(ctx, runner)
	if err != nil {
		return nil, err
	}
	if account == "" {
		return nil, nil
	}
	base := config.DefaultBaseURL
	if runner.Config != nil && runner.Config.BaseURL != "" {
		base = runner.Config.BaseURL
	}
	store, err := space.NewStore(base, account)
	if err != nil {
		return nil, err
	}
	if runner.SpaceStore != nil {
		store.Dir = runner.SpaceStore.Dir
	}
	return store, nil
}
