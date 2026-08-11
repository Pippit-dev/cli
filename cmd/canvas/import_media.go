package canvas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	canvascore "github.com/Pippit-dev/pippit-cli/internal/canvas"
	"github.com/Pippit-dev/pippit-cli/internal/canvasplan"
	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const (
	mediaCheckpointSchema          = "pippit-canvas-import-media/0.1"
	mediaStatusReady               = "ready"
	mediaStatusProcessing          = "processing"
	mediaStatusUploadRequested     = "upload-requested"
	mediaStatusBlocked             = "blocked"
	mediaStatusBlockedInterruption = "blocked-on-interruption"
	maxMediaCheckpointBytes        = 8 << 20
)

type importMediaPreflighter interface {
	PreflightUpload(context.Context) error
}

type importMediaAPI interface {
	Upload(context.Context, string) (*canvascore.UploadResult, error)
	Query(context.Context, string) error
}

type runnerImportMediaAPI struct {
	runner *common.Runner
}

func (api runnerImportMediaAPI) Upload(ctx context.Context, path string) (*canvascore.UploadResult, error) {
	return canvascore.Upload(ctx, canvascore.UploadOptions{Path: path}, api.runner)
}

func (api runnerImportMediaAPI) Query(ctx context.Context, pippitAssetID string) error {
	_, err := canvascore.Get(ctx, canvascore.GetOptions{AssetIDs: []string{pippitAssetID}}, api.runner)
	return err
}

// PreflightUpload mirrors the current Access Key authorizer's local guard. It
// runs immediately before the durable upload-requested marker is written, so
// a missing key cannot turn into an ambiguous remote outcome.
func (api runnerImportMediaAPI) PreflightUpload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api.runner == nil || api.runner.Config == nil {
		return fmt.Errorf("canvas media uploader is not configured")
	}
	if strings.TrimSpace(api.runner.Config.AccessKey) == "" {
		return fmt.Errorf("XYQ_ACCESS_KEY 缺失; authenticate the Pippit CLI before importing media")
	}
	return nil
}

type validatedImportMedia struct {
	LogicalID string
	MediaType string
	LocalPath string
	SHA256    string
	ByteSize  int64
}

type mediaResolutionOptions struct {
	Plan              canvasplan.Plan
	Media             []validatedImportMedia
	Target            string
	BundleDir         string
	BundleRoot        string
	CanvasJournalPath string
	CheckpointPath    string
}

type mediaCheckpoint struct {
	Schema     string                 `json:"schema"`
	Source     canvasplan.Source      `json:"source"`
	Target     string                 `json:"target"`
	BundleDirs []string               `json:"bundle_dirs,omitempty"`
	Entries    []mediaCheckpointEntry `json:"entries"`
}

type mediaCheckpointEntry struct {
	LogicalID     string `json:"logical_id"`
	MediaType     string `json:"media_type"`
	SHA256        string `json:"sha256"`
	Status        string `json:"status"`
	AssetID       string `json:"asset_id,omitempty"`
	PippitAssetID string `json:"pippit_asset_id,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

func readAndValidateExportMedia(bundleDir string, plan canvasplan.Plan) ([]validatedImportMedia, error) {
	result := make([]validatedImportMedia, 0, len(plan.RequiredMedia))
	for _, requirement := range plan.RequiredMedia {
		if requirement.LocalPath == "" || requirement.URL != "" {
			return nil, fmt.Errorf("LibTV CanvasPlan media %q must use a local bundle path", requirement.LogicalID)
		}
		localPath := filepath.Join(bundleDir, filepath.FromSlash(requirement.LocalPath))
		if err := requireFileWithinBundle(localPath, bundleDir); err != nil {
			return nil, fmt.Errorf("invalid LibTV media %q: %w", requirement.LogicalID, err)
		}
		info, err := os.Stat(localPath)
		if err != nil {
			return nil, fmt.Errorf("inspect LibTV media %q: %w", requirement.LogicalID, err)
		}
		if requirement.Metadata.ByteSize == nil || info.Size() != *requirement.Metadata.ByteSize {
			return nil, fmt.Errorf("LibTV media %q byte size does not match CanvasPlan", requirement.LogicalID)
		}
		digest, err := fileSHA256(localPath)
		if err != nil {
			return nil, fmt.Errorf("hash LibTV media %q: %w", requirement.LogicalID, err)
		}
		if digest != requirement.SHA256 {
			return nil, fmt.Errorf("LibTV media %q SHA-256 does not match CanvasPlan", requirement.LogicalID)
		}
		result = append(result, validatedImportMedia{
			LogicalID: requirement.LogicalID,
			MediaType: requirement.MediaType,
			LocalPath: localPath,
			SHA256:    digest,
			ByteSize:  info.Size(),
		})
	}
	return result, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func resolveImportMedia(
	ctx context.Context,
	opts mediaResolutionOptions,
	api importMediaAPI,
	stderr io.Writer,
) (canvasplan.ResolvedMediaSet, error) {
	lock, err := acquireImportMediaCheckpointLock(opts.CheckpointPath + ".lock")
	if err != nil {
		return canvasplan.ResolvedMediaSet{}, err
	}
	defer func() {
		if err := lock.release(); err != nil {
			fmt.Fprintf(stderr, "Could not release canvas import media checkpoint lock: %v\n", err)
		}
	}()

	checkpoint, err := loadMediaCheckpoint(opts)
	if err != nil {
		return canvasplan.ResolvedMediaSet{}, err
	}
	if !containsString(checkpoint.BundleDirs, opts.BundleDir) {
		checkpoint.BundleDirs = append(checkpoint.BundleDirs, opts.BundleDir)
		if err := saveMediaCheckpoint(opts.CheckpointPath, checkpoint); err != nil {
			return canvasplan.ResolvedMediaSet{}, err
		}
	}
	entries, err := validateCheckpointEntries(checkpoint, opts.Media)
	if err != nil {
		return canvasplan.ResolvedMediaSet{}, err
	}
	queriedReadyAssetIDs := make(map[string]struct{})
	for _, media := range opts.Media {
		if existing := entries[media.LogicalID]; existing != nil {
			switch existing.Status {
			case mediaStatusBlocked:
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"media upload %q is blocked after an unknown outcome; inspect %s and do not retry the bytes blindly",
					media.LogicalID, opts.CheckpointPath,
				)
			case mediaStatusUploadRequested:
				existing.Status = mediaStatusBlockedInterruption
				existing.LastError = "the previous process stopped after persisting upload-requested and before a durable response was checkpointed"
				if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, *existing); err != nil {
					return canvasplan.ResolvedMediaSet{}, err
				}
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"media upload %q was interrupted with an unknown outcome; checkpointed as blocked-on-interruption in %s and will not be uploaded again automatically",
					media.LogicalID, opts.CheckpointPath,
				)
			case mediaStatusBlockedInterruption:
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"media upload %q is blocked-on-interruption after an unknown outcome; inspect %s and do not retry the bytes blindly",
					media.LogicalID, opts.CheckpointPath,
				)
			case mediaStatusProcessing:
				fmt.Fprintf(stderr, "Checking previously uploaded media %q...\n", media.LogicalID)
				if err := api.Query(ctx, existing.PippitAssetID); err != nil {
					return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
						"previous media upload %q is not queryable yet; durable IDs remain in %s: %w",
						media.LogicalID, opts.CheckpointPath, err,
					)
				}
				existing.Status = mediaStatusReady
				existing.LastError = ""
				queriedReadyAssetIDs[existing.PippitAssetID] = struct{}{}
				if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, *existing); err != nil {
					return canvasplan.ResolvedMediaSet{}, err
				}
			case mediaStatusReady:
				if _, queried := queriedReadyAssetIDs[existing.PippitAssetID]; !queried {
					fmt.Fprintf(stderr, "Verifying previously uploaded media %q for the current Pippit account...\n", media.LogicalID)
					if err := api.Query(ctx, existing.PippitAssetID); err != nil {
						return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
							"previously uploaded media %q is unavailable to the current Pippit account; refusing checkpoint reuse: %w",
							media.LogicalID, err,
						)
					}
					queriedReadyAssetIDs[existing.PippitAssetID] = struct{}{}
				}
			default:
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf("media checkpoint %q has invalid status %q", media.LogicalID, existing.Status)
			}
			continue
		}
		if duplicate := readyEntryByDigest(entries, media); duplicate != nil {
			if _, queried := queriedReadyAssetIDs[duplicate.PippitAssetID]; !queried {
				fmt.Fprintf(stderr, "Verifying deduplicated media %q for the current Pippit account...\n", media.LogicalID)
				if err := api.Query(ctx, duplicate.PippitAssetID); err != nil {
					return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
						"deduplicated media %q is unavailable to the current Pippit account; refusing checkpoint reuse: %w",
						media.LogicalID, err,
					)
				}
				queriedReadyAssetIDs[duplicate.PippitAssetID] = struct{}{}
			}
			entry := mediaCheckpointEntry{
				LogicalID:     media.LogicalID,
				MediaType:     media.MediaType,
				SHA256:        media.SHA256,
				Status:        mediaStatusReady,
				AssetID:       duplicate.AssetID,
				PippitAssetID: duplicate.PippitAssetID,
			}
			entries[media.LogicalID] = &entry
			if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); err != nil {
				return canvasplan.ResolvedMediaSet{}, err
			}
			continue
		}
		if preflighter, ok := api.(importMediaPreflighter); ok {
			if err := preflighter.PreflightUpload(ctx); err != nil {
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"Pippit authentication failed before media upload %q was requested: %w",
					media.LogicalID, err,
				)
			}
		}
		fmt.Fprintf(stderr, "Uploading LibTV media %d/%d...\n", len(entries)+1, len(opts.Media))
		entry := mediaCheckpointEntry{
			LogicalID: media.LogicalID,
			MediaType: media.MediaType,
			SHA256:    media.SHA256,
			Status:    mediaStatusUploadRequested,
			LastError: "upload request is about to be dispatched; interruption requires manual outcome confirmation",
		}
		if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); err != nil {
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
				"persist upload-requested checkpoint for media %q: %w",
				media.LogicalID, err,
			)
		}
		entries[media.LogicalID] = &entry
		uploaded, uploadErr := api.Upload(ctx, media.LocalPath)
		if uploadErr != nil && strings.Contains(uploadErr.Error(), "XYQ_ACCESS_KEY 缺失") {
			if checkpointErr := removeAndSaveMediaEntry(opts.CheckpointPath, checkpoint, media.LogicalID); checkpointErr != nil {
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"Pippit authentication failed before media upload %q was sent, but its upload-requested checkpoint could not be cleared; do not retry blindly: %w",
					media.LogicalID, checkpointErr,
				)
			}
			delete(entries, media.LogicalID)
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
				"Pippit authentication failed before media upload %q was sent: %w",
				media.LogicalID, uploadErr,
			)
		}
		entry.LastError = ""
		if uploaded != nil {
			entry.AssetID = strings.TrimSpace(uploaded.AssetID)
			entry.PippitAssetID = strings.TrimSpace(uploaded.PippitAssetID)
		}
		if uploadErr != nil || uploaded == nil {
			entry.Status = mediaStatusBlocked
			entry.LastError = errorText(uploadErr, "upload returned no result")
			if checkpointErr := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); checkpointErr != nil {
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"media upload %q has an unknown outcome and its blocked checkpoint could not be saved; do not retry the bytes blindly: %w",
					media.LogicalID, checkpointErr,
				)
			}
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
				"media upload %q has an unknown outcome; checkpointed as blocked in %s: %s",
				media.LogicalID, opts.CheckpointPath, errorText(uploadErr, "upload returned no result"),
			)
		}
		if entry.AssetID == "" || entry.PippitAssetID == "" {
			entry.Status = mediaStatusBlocked
			entry.LastError = "upload response omitted durable asset IDs"
			if checkpointErr := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); checkpointErr != nil {
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"media upload %q omitted durable IDs and its blocked checkpoint could not be saved; do not upload again blindly: %w",
					media.LogicalID, checkpointErr,
				)
			}
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
				"media upload %q omitted durable asset IDs; checkpointed as blocked in %s",
				media.LogicalID, opts.CheckpointPath,
			)
		}
		if uploaded.State != canvascore.StateReady {
			entry.Status = mediaStatusProcessing
			entry.LastError = strings.TrimSpace(uploaded.Warning)
			if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); err != nil {
				return canvasplan.ResolvedMediaSet{}, err
			}
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
				"media upload %q is still processing; durable IDs are checkpointed in %s, rerun to query without re-uploading",
				media.LogicalID, opts.CheckpointPath,
			)
		}
		entry.Status = mediaStatusReady
		queriedReadyAssetIDs[entry.PippitAssetID] = struct{}{}
		entries[media.LogicalID] = &entry
		if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); err != nil {
			return canvasplan.ResolvedMediaSet{}, err
		}
	}
	resolved := canvasplan.ResolvedMediaSet{Schema: canvasplan.ResolvedMediaSchema}
	for _, media := range opts.Media {
		entry := entries[media.LogicalID]
		resolved.Media = append(resolved.Media, canvasplan.ResolvedMedia{
			LogicalID:     entry.LogicalID,
			MediaType:     entry.MediaType,
			AssetID:       entry.AssetID,
			PippitAssetID: entry.PippitAssetID,
		})
	}
	resolved, err = canvasplan.NormalizeResolvedMedia(resolved)
	if err != nil {
		return canvasplan.ResolvedMediaSet{}, err
	}
	cleanupCheckpointBundles(checkpoint, opts, stderr)
	return resolved, nil
}

func loadMediaCheckpoint(opts mediaResolutionOptions) (*mediaCheckpoint, error) {
	if info, lstatErr := os.Lstat(opts.CheckpointPath); lstatErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("canvas import media checkpoint must not be a symbolic link")
		}
		if info.Size() > maxMediaCheckpointBytes {
			return nil, fmt.Errorf("canvas import media checkpoint exceeds %d bytes", maxMediaCheckpointBytes)
		}
	} else if !os.IsNotExist(lstatErr) {
		return nil, fmt.Errorf("inspect canvas import media checkpoint: %w", lstatErr)
	}
	file, err := os.Open(opts.CheckpointPath)
	if os.IsNotExist(err) {
		if _, journalErr := os.Stat(opts.CanvasJournalPath); journalErr == nil {
			return nil, fmt.Errorf("canvas journal exists but its media checkpoint is missing; refusing to upload again: %s", opts.CheckpointPath)
		} else if !os.IsNotExist(journalErr) {
			return nil, fmt.Errorf("inspect canvas import journal before media upload: %w", journalErr)
		}
		checkpoint := &mediaCheckpoint{
			Schema:  mediaCheckpointSchema,
			Source:  opts.Plan.Source,
			Target:  opts.Target,
			Entries: []mediaCheckpointEntry{},
		}
		if err := saveMediaCheckpoint(opts.CheckpointPath, checkpoint); err != nil {
			return nil, err
		}
		return checkpoint, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open canvas import media checkpoint: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxMediaCheckpointBytes+1))
	decoder.DisallowUnknownFields()
	var checkpoint mediaCheckpoint
	if err := decoder.Decode(&checkpoint); err != nil {
		return nil, fmt.Errorf("decode canvas import media checkpoint: %w", err)
	}
	if err := ensureImportJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode canvas import media checkpoint: %w", err)
	}
	if checkpoint.Schema != mediaCheckpointSchema || checkpoint.Source != opts.Plan.Source || checkpoint.Target != opts.Target {
		return nil, fmt.Errorf("canvas import media checkpoint does not match this source and target")
	}
	if err := os.Chmod(opts.CheckpointPath, 0o600); err != nil {
		return nil, fmt.Errorf("secure canvas import media checkpoint: %w", err)
	}
	return &checkpoint, nil
}

func validateCheckpointEntries(
	checkpoint *mediaCheckpoint,
	media []validatedImportMedia,
) (map[string]*mediaCheckpointEntry, error) {
	expected := make(map[string]validatedImportMedia, len(media))
	for _, item := range media {
		expected[item.LogicalID] = item
	}
	entries := make(map[string]*mediaCheckpointEntry, len(checkpoint.Entries))
	for index := range checkpoint.Entries {
		entry := &checkpoint.Entries[index]
		item, ok := expected[entry.LogicalID]
		if !ok || item.MediaType != entry.MediaType || item.SHA256 != entry.SHA256 {
			return nil, fmt.Errorf("canvas import media changed after checkpoint creation")
		}
		if _, duplicate := entries[entry.LogicalID]; duplicate {
			return nil, fmt.Errorf("canvas import media checkpoint contains duplicate logical ID %q", entry.LogicalID)
		}
		if (entry.Status == mediaStatusReady || entry.Status == mediaStatusProcessing) &&
			(strings.TrimSpace(entry.AssetID) == "" || strings.TrimSpace(entry.PippitAssetID) == "") {
			return nil, fmt.Errorf("canvas import media checkpoint entry %q has no durable IDs", entry.LogicalID)
		}
		entries[entry.LogicalID] = entry
	}
	return entries, nil
}

func readyEntryByDigest(entries map[string]*mediaCheckpointEntry, media validatedImportMedia) *mediaCheckpointEntry {
	for _, entry := range entries {
		if entry.Status == mediaStatusReady && entry.MediaType == media.MediaType && entry.SHA256 == media.SHA256 {
			return entry
		}
	}
	return nil
}

func replaceAndSaveMediaEntry(path string, checkpoint *mediaCheckpoint, entry mediaCheckpointEntry) error {
	replaced := false
	for index := range checkpoint.Entries {
		if checkpoint.Entries[index].LogicalID == entry.LogicalID {
			checkpoint.Entries[index] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		checkpoint.Entries = append(checkpoint.Entries, entry)
	}
	sort.Slice(checkpoint.Entries, func(i, j int) bool {
		return checkpoint.Entries[i].LogicalID < checkpoint.Entries[j].LogicalID
	})
	return saveMediaCheckpoint(path, checkpoint)
}

func removeAndSaveMediaEntry(path string, checkpoint *mediaCheckpoint, logicalID string) error {
	entries := make([]mediaCheckpointEntry, 0, len(checkpoint.Entries))
	for _, entry := range checkpoint.Entries {
		if entry.LogicalID != logicalID {
			entries = append(entries, entry)
		}
	}
	checkpoint.Entries = entries
	return saveMediaCheckpoint(path, checkpoint)
}

func saveMediaCheckpoint(path string, checkpoint *mediaCheckpoint) error {
	payload, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("encode canvas import media checkpoint: %w", err)
	}
	payload = append(payload, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create canvas import media checkpoint directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".canvas-import-media-*")
	if err != nil {
		return fmt.Errorf("create temporary canvas import media checkpoint: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary canvas import media checkpoint: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		return fmt.Errorf("write canvas import media checkpoint: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync canvas import media checkpoint: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close canvas import media checkpoint: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace canvas import media checkpoint: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure canvas import media checkpoint: %w", err)
	}
	return nil
}

func cleanupCheckpointBundles(
	checkpoint *mediaCheckpoint,
	opts mediaResolutionOptions,
	stderr io.Writer,
) {
	remaining := make([]string, 0, len(checkpoint.BundleDirs))
	for _, bundleDir := range checkpoint.BundleDirs {
		if err := removeOwnedBundle(bundleDir, opts.BundleRoot); err != nil {
			fmt.Fprintf(stderr, "Could not remove local LibTV export bundle %s: %v\n", bundleDir, err)
			remaining = append(remaining, bundleDir)
		}
	}
	checkpoint.BundleDirs = remaining
	if err := saveMediaCheckpoint(opts.CheckpointPath, checkpoint); err != nil {
		fmt.Fprintf(stderr, "Could not update media checkpoint cleanup state: %v\n", err)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func errorText(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}
