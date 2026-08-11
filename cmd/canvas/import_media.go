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
	"time"

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
	defaultImportMediaPollInterval = 2 * time.Second
	defaultImportMediaWaitTimeout  = 10 * time.Minute
	initialImportUploadWaitTimeout = 5 * time.Second
)

type importMediaPreflighter interface {
	PreflightUpload(context.Context) error
}

type importMediaAPI interface {
	Upload(context.Context, validatedImportMedia) (*canvascore.UploadResult, error)
	Query(context.Context, string) (bool, error)
}

type runnerImportMediaAPI struct {
	runner *common.Runner
}

func (api runnerImportMediaAPI) Upload(ctx context.Context, media validatedImportMedia) (*canvascore.UploadResult, error) {
	file, identity, err := openInspectedImportMediaFile(media.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("verify canvas import media immediately before upload: %w", err)
	}
	defer file.Close()
	if identity.RawSHA256 != media.SHA256 || identity.ContentFingerprint != media.ContentFingerprint ||
		identity.ByteSize != media.ByteSize {
		return nil, fmt.Errorf("canvas import media changed before upload dispatch")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind verified canvas import media before upload: %w", err)
	}
	return canvascore.Upload(ctx, canvascore.UploadOptions{
		FileName:     media.FileName,
		Reader:       file,
		PollInterval: time.Second,
		WaitTimeout:  initialImportUploadWaitTimeout,
	}, api.runner)
}

func (api runnerImportMediaAPI) Query(ctx context.Context, pippitAssetID string) (bool, error) {
	result, err := canvascore.GetExisting(ctx, []string{pippitAssetID}, api.runner)
	if err != nil {
		return false, err
	}
	return result != nil && len(result.Assets) == 1, nil
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
	LogicalID          string
	MediaType          string
	FileName           string
	LocalPath          string
	SHA256             string
	ContentFingerprint string
	ByteSize           int64
}

type mediaResolutionOptions struct {
	Plan              canvasplan.Plan
	Media             []validatedImportMedia
	Target            string
	BundleDir         string
	BundleRoot        string
	CanvasJournalPath string
	CheckpointPath    string
	PollInterval      time.Duration
	WaitTimeout       time.Duration
}

type mediaCheckpoint struct {
	Schema     string                 `json:"schema"`
	Source     canvasplan.Source      `json:"source"`
	Target     string                 `json:"target"`
	BundleDirs []string               `json:"bundle_dirs,omitempty"`
	Entries    []mediaCheckpointEntry `json:"entries"`
}

type mediaCheckpointEntry struct {
	LogicalID          string `json:"logical_id"`
	MediaType          string `json:"media_type"`
	SHA256             string `json:"sha256"`
	ContentFingerprint string `json:"content_fingerprint,omitempty"`
	CanonicalByteSize  int64  `json:"canonical_byte_size,omitempty"`
	Status             string `json:"status"`
	AssetID            string `json:"asset_id,omitempty"`
	PippitAssetID      string `json:"pippit_asset_id,omitempty"`
	LastError          string `json:"last_error,omitempty"`
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
		identity, err := inspectImportMediaFile(localPath)
		if err != nil {
			return nil, fmt.Errorf("inspect LibTV media %q: %w", requirement.LogicalID, err)
		}
		if requirement.Metadata.ByteSize == nil || identity.ByteSize != *requirement.Metadata.ByteSize {
			return nil, fmt.Errorf("LibTV media %q byte size does not match CanvasPlan", requirement.LogicalID)
		}
		if identity.RawSHA256 != requirement.SHA256 {
			return nil, fmt.Errorf("LibTV media %q SHA-256 does not match CanvasPlan", requirement.LogicalID)
		}
		result = append(result, validatedImportMedia{
			LogicalID:          requirement.LogicalID,
			MediaType:          requirement.MediaType,
			FileName:           requirement.FileName,
			LocalPath:          localPath,
			SHA256:             identity.RawSHA256,
			ContentFingerprint: identity.ContentFingerprint,
			ByteSize:           identity.ByteSize,
		})
	}
	return result, nil
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
	entries, migrated, err := validateCheckpointEntries(checkpoint, opts)
	if err != nil {
		return canvasplan.ResolvedMediaSet{}, err
	}
	if migrated {
		if err := saveMediaCheckpoint(opts.CheckpointPath, checkpoint); err != nil {
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf("save migrated canvas import media checkpoint: %w", err)
		}
	}
	if len(opts.Media) == 0 {
		reportImportMediaProgress(stderr, 0, 0, "complete", validatedImportMedia{FileName: "(none)"})
	}
	queriedReadyAssetIDs := make(map[string]struct{})
	for index, media := range opts.Media {
		if existing := entries[media.LogicalID]; existing != nil {
			action := "reused"
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
				if err := waitForImportMediaReady(ctx, opts, api, stderr, index, media, existing.PippitAssetID); err != nil {
					return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
						"wait for previous media upload %q; durable IDs remain in %s: %w",
						media.LogicalID, opts.CheckpointPath, err,
					)
				}
				existing.Status = mediaStatusReady
				existing.LastError = ""
				queriedReadyAssetIDs[existing.PippitAssetID] = struct{}{}
				if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, *existing); err != nil {
					return canvasplan.ResolvedMediaSet{}, err
				}
				action = "queried"
			case mediaStatusReady:
				if _, queried := queriedReadyAssetIDs[existing.PippitAssetID]; !queried {
					reportImportMediaProgress(stderr, index, len(opts.Media), "checking", media)
					ready, err := api.Query(ctx, existing.PippitAssetID)
					if err != nil {
						return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
							"previously uploaded media %q is unavailable to the current Pippit account; refusing checkpoint reuse: %w",
							media.LogicalID, err,
						)
					}
					if !ready {
						return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
							"previously uploaded media %q is not visible to the current Pippit account; refusing checkpoint reuse",
							media.LogicalID,
						)
					}
					queriedReadyAssetIDs[existing.PippitAssetID] = struct{}{}
				}
			default:
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf("media checkpoint %q has invalid status %q", media.LogicalID, existing.Status)
			}
			reportImportMediaProgress(stderr, index+1, len(opts.Media), action, media)
			continue
		}
		if duplicate := readyEntryByDigest(entries, media); duplicate != nil {
			if _, queried := queriedReadyAssetIDs[duplicate.PippitAssetID]; !queried {
				reportImportMediaProgress(stderr, index, len(opts.Media), "checking", media)
				ready, err := api.Query(ctx, duplicate.PippitAssetID)
				if err != nil {
					return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
						"deduplicated media %q is unavailable to the current Pippit account; refusing checkpoint reuse: %w",
						media.LogicalID, err,
					)
				}
				if !ready {
					return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
						"deduplicated media %q is not visible to the current Pippit account; refusing checkpoint reuse",
						media.LogicalID,
					)
				}
				queriedReadyAssetIDs[duplicate.PippitAssetID] = struct{}{}
			}
			entry := mediaCheckpointEntry{
				LogicalID:          media.LogicalID,
				MediaType:          media.MediaType,
				SHA256:             media.SHA256,
				ContentFingerprint: media.ContentFingerprint,
				CanonicalByteSize:  media.ByteSize,
				Status:             mediaStatusReady,
				AssetID:            duplicate.AssetID,
				PippitAssetID:      duplicate.PippitAssetID,
			}
			entries[media.LogicalID] = &entry
			if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); err != nil {
				return canvasplan.ResolvedMediaSet{}, err
			}
			reportImportMediaProgress(stderr, index+1, len(opts.Media), "reused", media)
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
		entry := mediaCheckpointEntry{
			LogicalID:          media.LogicalID,
			MediaType:          media.MediaType,
			SHA256:             media.SHA256,
			ContentFingerprint: media.ContentFingerprint,
			CanonicalByteSize:  media.ByteSize,
			Status:             mediaStatusUploadRequested,
			LastError:          "upload request is about to be dispatched; interruption requires manual outcome confirmation",
		}
		if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); err != nil {
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
				"persist upload-requested checkpoint for media %q: %w",
				media.LogicalID, err,
			)
		}
		entries[media.LogicalID] = &entry
		reportImportMediaProgress(stderr, index, len(opts.Media), "uploading", media)
		uploaded, uploadErr := api.Upload(ctx, media)
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
			if err := waitForImportMediaReady(ctx, opts, api, stderr, index, media, entry.PippitAssetID); err != nil {
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"wait for media upload %q; durable IDs remain in %s: %w",
					media.LogicalID, opts.CheckpointPath, err,
				)
			}
		}
		entry.Status = mediaStatusReady
		entry.LastError = ""
		queriedReadyAssetIDs[entry.PippitAssetID] = struct{}{}
		entries[media.LogicalID] = &entry
		if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); err != nil {
			return canvasplan.ResolvedMediaSet{}, err
		}
		reportImportMediaProgress(stderr, index+1, len(opts.Media), "uploaded", media)
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

func waitForImportMediaReady(
	ctx context.Context,
	opts mediaResolutionOptions,
	api importMediaAPI,
	stderr io.Writer,
	processed int,
	media validatedImportMedia,
	pippitAssetID string,
) error {
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultImportMediaPollInterval
	}
	waitTimeout := opts.WaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = defaultImportMediaWaitTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()
	for attempt := 1; ; attempt++ {
		action := "waiting"
		if attempt == 1 {
			action = "processing"
		}
		reportImportMediaProgress(stderr, processed, len(opts.Media), action, media)
		ready, err := api.Query(waitCtx, pippitAssetID)
		if err != nil {
			if waitCtx.Err() != nil {
				return importMediaWaitError(waitCtx.Err(), pippitAssetID, waitTimeout)
			}
			return fmt.Errorf(
				"query processing Pippit asset %q failed; this is a read/authentication error, not a processing signal: %w",
				pippitAssetID,
				err,
			)
		}
		if ready {
			return nil
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return importMediaWaitError(waitCtx.Err(), pippitAssetID, waitTimeout)
		case <-timer.C:
		}
	}
}

func importMediaWaitError(err error, pippitAssetID string, timeout time.Duration) error {
	if err == context.DeadlineExceeded {
		return fmt.Errorf("Pippit asset %q was not visible within %s; it will only be queried on the next run and will not be uploaded again", pippitAssetID, timeout)
	}
	return fmt.Errorf("wait for Pippit asset %q canceled; it will not be uploaded again: %w", pippitAssetID, err)
}

func reportImportMediaProgress(
	stderr io.Writer,
	processed int,
	total int,
	action string,
	media validatedImportMedia,
) {
	remaining := total - processed
	if remaining < 0 {
		remaining = 0
	}
	fileName := strings.TrimSpace(media.FileName)
	if fileName == "" {
		fileName = filepath.Base(media.LocalPath)
	}
	fmt.Fprintf(
		stderr,
		"Media progress: processed=%d/%d remaining=%d action=%s file=%q\n",
		processed,
		total,
		remaining,
		action,
		fileName,
	)
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
	opts mediaResolutionOptions,
) (map[string]*mediaCheckpointEntry, bool, error) {
	expected := make(map[string]validatedImportMedia, len(opts.Media))
	for _, item := range opts.Media {
		expected[item.LogicalID] = item
	}
	entries := make(map[string]*mediaCheckpointEntry, len(checkpoint.Entries))
	migrated := false
	for index := range checkpoint.Entries {
		entry := &checkpoint.Entries[index]
		item, ok := expected[entry.LogicalID]
		if !ok || item.MediaType != entry.MediaType {
			return nil, false, fmt.Errorf("canvas import media changed after checkpoint creation")
		}
		if !validRawMediaSHA256(entry.SHA256) || !validImportMediaContentFingerprint(item.ContentFingerprint) {
			return nil, false, fmt.Errorf("canvas import media checkpoint entry %q has an invalid fingerprint", entry.LogicalID)
		}
		if item.SHA256 != entry.SHA256 {
			if item.MediaType != "image" {
				return nil, false, fmt.Errorf("canvas import media changed after checkpoint creation")
			}
			if entry.ContentFingerprint == "" || entry.CanonicalByteSize == 0 {
				previousIdentity, err := findLegacyCheckpointImageIdentity(checkpoint, opts, *entry)
				if err != nil {
					return nil, false, fmt.Errorf("verify changed checkpoint image %q: %w", entry.LogicalID, err)
				}
				if !validImportMediaContentFingerprint(previousIdentity.ContentFingerprint) {
					return nil, false, fmt.Errorf("canvas import media checkpoint image %q has an invalid content fingerprint", entry.LogicalID)
				}
				if entry.ContentFingerprint != "" && entry.ContentFingerprint != previousIdentity.ContentFingerprint {
					return nil, false, fmt.Errorf("canvas import media checkpoint image %q content fingerprint does not match its retained bundle", entry.LogicalID)
				}
				if entry.CanonicalByteSize != 0 && entry.CanonicalByteSize != previousIdentity.ByteSize {
					return nil, false, fmt.Errorf("canvas import media checkpoint image %q byte size does not match its retained bundle", entry.LogicalID)
				}
				if entry.ContentFingerprint == "" {
					entry.ContentFingerprint = previousIdentity.ContentFingerprint
					migrated = true
				}
				if entry.CanonicalByteSize == 0 {
					entry.CanonicalByteSize = previousIdentity.ByteSize
					migrated = true
				}
			}
			if !validImportMediaContentFingerprint(entry.ContentFingerprint) || entry.ContentFingerprint != item.ContentFingerprint {
				return nil, false, fmt.Errorf("canvas import image content changed after checkpoint creation")
			}
		} else {
			if entry.ContentFingerprint != "" &&
				(!validImportMediaContentFingerprint(entry.ContentFingerprint) || entry.ContentFingerprint != item.ContentFingerprint) {
				return nil, false, fmt.Errorf("canvas import media content changed after checkpoint creation")
			}
			if entry.ContentFingerprint == "" {
				entry.ContentFingerprint = item.ContentFingerprint
				migrated = true
			}
			if entry.CanonicalByteSize != 0 && entry.CanonicalByteSize != item.ByteSize {
				return nil, false, fmt.Errorf("canvas import media byte size changed after checkpoint creation")
			}
			if entry.CanonicalByteSize == 0 {
				entry.CanonicalByteSize = item.ByteSize
				migrated = true
			}
		}
		if _, duplicate := entries[entry.LogicalID]; duplicate {
			return nil, false, fmt.Errorf("canvas import media checkpoint contains duplicate logical ID %q", entry.LogicalID)
		}
		if (entry.Status == mediaStatusReady || entry.Status == mediaStatusProcessing) &&
			(strings.TrimSpace(entry.AssetID) == "" || strings.TrimSpace(entry.PippitAssetID) == "") {
			return nil, false, fmt.Errorf("canvas import media checkpoint entry %q has no durable IDs", entry.LogicalID)
		}
		entries[entry.LogicalID] = entry
	}
	return entries, migrated, nil
}

func validRawMediaSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func canonicalizeImportPlanMedia(
	plan canvasplan.Plan,
	target string,
	journalPath string,
	checkpointPath string,
) (canvasplan.Plan, error) {
	lock, err := acquireImportMediaCheckpointLock(checkpointPath + ".lock")
	if err != nil {
		return canvasplan.Plan{}, err
	}
	defer func() { _ = lock.release() }()
	checkpoint, err := loadMediaCheckpoint(mediaResolutionOptions{
		Plan:              plan,
		Target:            target,
		CanvasJournalPath: journalPath,
		CheckpointPath:    checkpointPath,
	})
	if err != nil {
		return canvasplan.Plan{}, fmt.Errorf("load canonical canvas import media identities: %w", err)
	}
	entries := make(map[string]mediaCheckpointEntry, len(checkpoint.Entries))
	for _, entry := range checkpoint.Entries {
		if _, exists := entries[entry.LogicalID]; exists {
			return canvasplan.Plan{}, fmt.Errorf("canvas import media checkpoint contains duplicate logical ID %q", entry.LogicalID)
		}
		entries[entry.LogicalID] = entry
	}
	canonical := plan
	canonical.RequiredMedia = append([]canvasplan.MediaRequirement(nil), plan.RequiredMedia...)
	for index := range canonical.RequiredMedia {
		requirement := &canonical.RequiredMedia[index]
		entry, ok := entries[requirement.LogicalID]
		if !ok || entry.MediaType != requirement.MediaType || entry.Status != mediaStatusReady {
			return canvasplan.Plan{}, fmt.Errorf("canvas import media checkpoint has no ready canonical identity for %q", requirement.LogicalID)
		}
		if !validRawMediaSHA256(entry.SHA256) || entry.CanonicalByteSize <= 0 {
			return canvasplan.Plan{}, fmt.Errorf("canvas import media checkpoint has an invalid canonical identity for %q", requirement.LogicalID)
		}
		requirement.SHA256 = entry.SHA256
		byteSize := entry.CanonicalByteSize
		requirement.Metadata.ByteSize = &byteSize
	}
	return canonical, nil
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
