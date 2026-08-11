package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const defaultUploadPollInterval = time.Second

var uploadContentTypeFallbacks = map[string]string{
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".mp4":  "video/mp4",
	".mov":  "video/quicktime",
}

type UploadOptions struct {
	Path         string
	FileName     string
	Reader       io.Reader
	PollInterval time.Duration
	WaitTimeout  time.Duration
}

// AssetLocator reports the durable Pippit ID and optional observed media
// locations. URLs may be signed or short lived and must not be persisted into
// Canvas patches; callers should persist PippitAssetID instead.
type AssetLocator struct {
	PippitAssetID string `json:"pippit_asset_id"`
	SourceID      string `json:"source_id,omitempty"`
	DownloadURL   string `json:"download_url,omitempty"`
	InternalURL   string `json:"internal_url,omitempty"`
	CoverURL      string `json:"cover_url,omitempty"`
	VID           string `json:"vid,omitempty"`
}

type UploadResult struct {
	State         string          `json:"state"`
	AssetID       string          `json:"asset_id,omitempty"`
	PippitAssetID string          `json:"pippit_asset_id"`
	Locator       AssetLocator    `json:"locator"`
	Asset         json.RawMessage `json:"asset,omitempty"`
	LogID         string          `json:"log_id,omitempty"`
	QueryLogID    string          `json:"query_log_id,omitempty"`
	PollAttempts  int             `json:"poll_attempts"`
	Warning       string          `json:"warning,omitempty"`
}

func Upload(ctx context.Context, opts UploadOptions, runner *common.Runner) (*UploadResult, error) {
	client, err := runnerClient(runner, "canvas upload")
	if err != nil {
		return nil, err
	}
	if opts.PollInterval < 0 || opts.WaitTimeout < 0 {
		return nil, fmt.Errorf("canvas upload polling durations must not be negative")
	}
	path := strings.TrimSpace(opts.Path)
	if path == "" && opts.Reader == nil {
		return nil, fmt.Errorf("canvas upload path is required")
	}
	if opts.Reader == nil {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect canvas upload file: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("canvas upload path %q is not a regular file", path)
		}
	}
	fileName := strings.TrimSpace(opts.FileName)
	if fileName == "" {
		fileName = filepath.Base(path)
	}
	if fileName == "" || fileName == "." {
		return nil, fmt.Errorf("canvas upload file name is required")
	}
	extension := strings.ToLower(filepath.Ext(fileName))
	contentType := mime.TypeByExtension(extension)
	if contentType == "" {
		contentType = uploadContentTypeFallbacks[extension]
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	var envelope responseEnvelope
	if err := client.SendMultipartRequest(ctx, UploadPath, nil, common.MultipartFile{
		FieldName:   "file",
		Path:        path,
		FileName:    fileName,
		ContentType: contentType,
		Reader:      opts.Reader,
	}, &envelope); err != nil {
		return nil, fmt.Errorf("canvas upload request failed; outcome may be ambiguous, check assets before retrying: %w", err)
	}
	if err := envelope.validate("canvas upload"); err != nil {
		return nil, err
	}
	assetID, pippitAssetID, err := parseUploadData(envelope.Data)
	if err != nil {
		return nil, common.NewLogIDError(fmt.Sprintf("canvas upload returned invalid data: %v", err), envelope.LogID)
	}
	result := &UploadResult{
		State:         StateProcessing,
		AssetID:       assetID,
		PippitAssetID: pippitAssetID,
		Locator:       AssetLocator{PippitAssetID: pippitAssetID},
		LogID:         strings.TrimSpace(envelope.LogID),
	}

	asset, queryLogID, attempts, err := waitForUploadedAsset(ctx, pippitAssetID, opts.PollInterval, opts.WaitTimeout, runner)
	result.PollAttempts = attempts
	if err != nil {
		// The upload response already returned a durable Pippit asset ID. Keep
		// it machine-readable so callers can resume querying without uploading
		// the same bytes again.
		result.Warning = err.Error()
		return result, nil
	}
	result.State = StateReady
	result.Locator = locatorFromAsset(asset, pippitAssetID)
	result.Asset = asset
	result.QueryLogID = queryLogID
	return result, nil
}

func parseUploadData(raw json.RawMessage) (string, string, error) {
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", "", err
	}
	assetID, err := optionalStringField(data, "asset_id", "AssetId", "assetId")
	if err != nil {
		return "", "", err
	}
	pippitAssetID, err := optionalStringField(data, "pippit_asset_id", "PippitAssetID", "pippitAssetId")
	if err != nil {
		return "", "", err
	}
	if pippitAssetID == "" {
		return "", "", fmt.Errorf("data.pippit_asset_id is missing")
	}
	return assetID, pippitAssetID, nil
}

func optionalStringField(values map[string]json.RawMessage, keys ...string) (string, error) {
	raw, ok := rawField(values, keys...)
	if !ok || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a JSON string", keys[0])
	}
	return strings.TrimSpace(value), nil
}

func waitForUploadedAsset(
	ctx context.Context,
	assetID string,
	pollInterval, waitTimeout time.Duration,
	runner *common.Runner,
) (json.RawMessage, string, int, error) {
	if pollInterval <= 0 {
		pollInterval = defaultUploadPollInterval
	}
	if waitTimeout <= 0 {
		waitTimeout = 2 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()
	for attempts := 1; ; attempts++ {
		result, err := queryAssets(waitCtx, []string{assetID}, false, runner)
		if err != nil {
			if waitCtx.Err() != nil {
				return nil, "", attempts, uploadWaitError(waitCtx.Err(), assetID, waitTimeout)
			}
			return nil, "", attempts, fmt.Errorf("query uploaded canvas asset: %w", err)
		}
		if len(result.Assets) == 1 {
			return result.Assets[0], result.LogID, attempts, nil
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return nil, result.LogID, attempts, uploadWaitError(waitCtx.Err(), assetID, waitTimeout)
		case <-timer.C:
		}
	}
}

func uploadWaitError(err error, assetID string, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("uploaded canvas asset %q was not queryable within %s", assetID, timeout)
	}
	return fmt.Errorf("wait for uploaded canvas asset %q canceled: %w", assetID, err)
}

func locatorFromAsset(raw json.RawMessage, pippitAssetID string) AssetLocator {
	var value any
	_ = json.Unmarshal(raw, &value)
	return AssetLocator{
		PippitAssetID: pippitAssetID,
		SourceID:      findFirstString(value, "SourceID", "source_id", "sourceId"),
		DownloadURL:   findFirstString(value, "DownloadUrl", "DownloadURL", "download_url", "downloadUrl"),
		InternalURL:   findFirstString(value, "InternalUrl", "InternalURL", "internal_url", "internalUrl"),
		CoverURL:      findFirstString(value, "CoverUrl", "CoverURL", "cover_url", "coverUrl"),
		VID:           findFirstString(value, "VID", "vid"),
	}
}

func findFirstString(value any, keys ...string) string {
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	return walkFirstString(value, keySet, 0)
}

func walkFirstString(value any, keys map[string]struct{}, depth int) string {
	if depth > 10 {
		return ""
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, candidate := range typed {
			if _, ok := keys[key]; ok {
				if text, ok := candidate.(string); ok && strings.TrimSpace(text) != "" {
					return strings.TrimSpace(text)
				}
			}
		}
		for _, candidate := range typed {
			if found := walkFirstString(candidate, keys, depth+1); found != "" {
				return found
			}
		}
	case []any:
		for _, candidate := range typed {
			if found := walkFirstString(candidate, keys, depth+1); found != "" {
				return found
			}
		}
	}
	return ""
}
