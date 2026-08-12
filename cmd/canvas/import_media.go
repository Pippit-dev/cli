package canvas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

var errCanvasImportMediaStillProcessing = errors.New("小云雀素材仍在处理中")

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
		return nil, fmt.Errorf("上传前再次验证画布导入素材失败：%w", err)
	}
	defer file.Close()
	if identity.RawSHA256 != media.SHA256 || identity.ContentFingerprint != media.ContentFingerprint ||
		identity.ByteSize != media.ByteSize {
		return nil, fmt.Errorf("画布导入素材在发起上传前发生了变化")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("上传前重置已验证素材的读取位置失败：%w", err)
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
	if api.runner == nil {
		return fmt.Errorf("画布素材上传器尚未配置")
	}
	accessKey := ""
	if api.runner.Auth != nil {
		resolved, err := api.runner.Auth.ResolveAccessKey(ctx)
		if err != nil {
			return fmt.Errorf("未找到可用的小云雀 CLI 登录凭证；请先运行 pippit-tool-cli login: %w", err)
		}
		accessKey = resolved
	} else if api.runner.Config != nil {
		accessKey = api.runner.Config.AccessKey
	}
	if strings.TrimSpace(accessKey) == "" {
		return fmt.Errorf("未登录小云雀 CLI；请先运行 pippit-tool-cli login")
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
			return nil, fmt.Errorf("LibTV 画布计划中的素材 %q 必须使用本地导出包路径", requirement.LogicalID)
		}
		localPath := filepath.Join(bundleDir, filepath.FromSlash(requirement.LocalPath))
		if err := requireFileWithinBundle(localPath, bundleDir); err != nil {
			return nil, fmt.Errorf("LibTV 素材 %q 无效：%w", requirement.LogicalID, err)
		}
		identity, err := inspectImportMediaFile(localPath)
		if err != nil {
			return nil, fmt.Errorf("检查 LibTV 素材 %q 失败：%w", requirement.LogicalID, err)
		}
		if requirement.Metadata.ByteSize == nil || identity.ByteSize != *requirement.Metadata.ByteSize {
			return nil, fmt.Errorf("LibTV 素材 %q 的文件大小与画布计划不一致", requirement.LogicalID)
		}
		if identity.RawSHA256 != requirement.SHA256 {
			return nil, fmt.Errorf("LibTV 素材 %q 的 SHA-256 与画布计划不一致", requirement.LogicalID)
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
			fmt.Fprintf(stderr, "提示：无法释放画布导入素材断点锁：%v\n", err)
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
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf("保存迁移后的画布导入素材断点记录失败：%w", err)
		}
	}
	if len(opts.Media) == 0 {
		reportImportMediaProgress(stderr, 0, 0, "complete", validatedImportMedia{FileName: "（无）"})
	}
	queriedReadyAssetIDs := make(map[string]struct{})
	for index, media := range opts.Media {
		if existing := entries[media.LogicalID]; existing != nil {
			action := "reused"
			switch existing.Status {
			case mediaStatusBlocked:
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"素材 %q 的上传结果未知，当前处于 blocked 状态；请检查 %s，不要直接重复上传",
					media.LogicalID, opts.CheckpointPath,
				)
			case mediaStatusUploadRequested:
				existing.Status = mediaStatusBlockedInterruption
				existing.LastError = "上次进程在记录 upload-requested 后、持久化上传结果前中断"
				if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, *existing); err != nil {
					return canvasplan.ResolvedMediaSet{}, err
				}
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"素材 %q 的上传曾被中断且结果未知；已在 %s 中记录为 blocked-on-interruption，后续不会自动重复上传",
					media.LogicalID, opts.CheckpointPath,
				)
			case mediaStatusBlockedInterruption:
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"素材 %q 的上传结果未知，当前处于 blocked-on-interruption 状态；请检查 %s，不要直接重复上传",
					media.LogicalID, opts.CheckpointPath,
				)
			case mediaStatusProcessing:
				if err := waitForImportMediaReady(ctx, opts, api, stderr, index, media, existing.PippitAssetID); err != nil {
					return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
						"等待之前上传的素材 %q 就绪失败；持久化素材 ID 仍保存在 %s：%w",
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
							"当前小云雀账号无法读取之前上传的素材 %q，因此不会复用断点记录：%w",
							media.LogicalID, err,
						)
					}
					if !ready {
						return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
							"当前小云雀账号不可见之前上传的素材 %q，因此不会复用断点记录",
							media.LogicalID,
						)
					}
					queriedReadyAssetIDs[existing.PippitAssetID] = struct{}{}
				}
			default:
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf("素材 %q 的断点记录状态 %q 无效", media.LogicalID, existing.Status)
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
						"当前小云雀账号无法读取已去重素材 %q，因此不会复用断点记录：%w",
						media.LogicalID, err,
					)
				}
				if !ready {
					return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
						"当前小云雀账号不可见已去重素材 %q，因此不会复用断点记录",
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
					"请求上传素材 %q 前，小云雀授权检查失败：%w",
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
			LastError:          "即将发起上传请求；若此时中断，需要人工确认上传结果",
		}
		if err := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); err != nil {
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
				"保存素材 %q 的 upload-requested 断点记录失败：%w",
				media.LogicalID, err,
			)
		}
		entries[media.LogicalID] = &entry
		reportImportMediaProgress(stderr, index, len(opts.Media), "uploading", media)
		uploaded, uploadErr := api.Upload(ctx, media)
		if uploadErr != nil && isProvenPrewriteCredentialFailure(uploadErr) {
			if checkpointErr := removeAndSaveMediaEntry(opts.CheckpointPath, checkpoint, media.LogicalID); checkpointErr != nil {
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"发送素材 %q 的上传请求前，小云雀授权失败，且无法清理 upload-requested 断点记录；请勿直接重试：%w",
					media.LogicalID, checkpointErr,
				)
			}
			delete(entries, media.LogicalID)
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
				"%w：发送素材 %q 的上传请求被小云雀明确拒绝，未产生远端写入",
				errCanvasImportReauthenticationRequired, media.LogicalID,
			)
		}
		entry.LastError = ""
		if uploaded != nil {
			entry.AssetID = strings.TrimSpace(uploaded.AssetID)
			entry.PippitAssetID = strings.TrimSpace(uploaded.PippitAssetID)
		}
		if uploadErr != nil || uploaded == nil {
			entry.Status = mediaStatusBlocked
			entry.LastError = errorText(uploadErr, "上传未返回结果")
			if checkpointErr := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); checkpointErr != nil {
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"素材 %q 的上传结果未知，且无法保存 blocked 断点记录；请勿直接重复上传：%w",
					media.LogicalID, checkpointErr,
				)
			}
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
				"素材 %q 的上传结果未知；已在 %s 中记录为 blocked：%s",
				media.LogicalID, opts.CheckpointPath, errorText(uploadErr, "上传未返回结果"),
			)
		}
		if entry.AssetID == "" || entry.PippitAssetID == "" {
			entry.Status = mediaStatusBlocked
			entry.LastError = "上传响应缺少可持久化的素材 ID"
			if checkpointErr := replaceAndSaveMediaEntry(opts.CheckpointPath, checkpoint, entry); checkpointErr != nil {
				return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
					"素材 %q 的上传响应缺少可持久化 ID，且无法保存 blocked 断点记录；请勿直接重复上传：%w",
					media.LogicalID, checkpointErr,
				)
			}
			return canvasplan.ResolvedMediaSet{}, fmt.Errorf(
				"素材 %q 的上传响应缺少可持久化 ID；已在 %s 中记录为 blocked",
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
					"等待素材 %q 上传就绪失败；持久化素材 ID 仍保存在 %s：%w",
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
				"查询处理中小云雀素材 %q 失败；这是读取或授权错误，不代表素材仍在处理中：%w",
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
		return fmt.Errorf("%w：素材 %q 在 %s 内仍不可见；只会继续查询，不会重复上传", errCanvasImportMediaStillProcessing, pippitAssetID, timeout)
	}
	return fmt.Errorf("等待小云雀素材 %q 就绪已取消；不会重复上传：%w", pippitAssetID, err)
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
		"素材进度：已处理=%d/%d，剩余=%d，状态=%s，文件=%q\n",
		processed,
		total,
		remaining,
		importMediaProgressAction(action),
		fileName,
	)
}

func importMediaProgressAction(action string) string {
	switch action {
	case "complete":
		return "完成"
	case "reused":
		return "已复用"
	case "queried":
		return "已确认"
	case "checking":
		return "正在检查"
	case "uploading":
		return "正在上传"
	case "uploaded":
		return "已上传"
	case "processing":
		return "正在处理"
	case "waiting":
		return "等待处理中"
	default:
		return action
	}
}

func loadMediaCheckpoint(opts mediaResolutionOptions) (*mediaCheckpoint, error) {
	if info, lstatErr := os.Lstat(opts.CheckpointPath); lstatErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("画布导入素材断点记录不能是符号链接")
		}
		if info.Size() > maxMediaCheckpointBytes {
			return nil, fmt.Errorf("画布导入素材断点记录超过 %d 字节", maxMediaCheckpointBytes)
		}
	} else if !os.IsNotExist(lstatErr) {
		return nil, fmt.Errorf("检查画布导入素材断点记录失败：%w", lstatErr)
	}
	file, err := os.Open(opts.CheckpointPath)
	if os.IsNotExist(err) {
		if _, journalErr := os.Stat(opts.CanvasJournalPath); journalErr == nil {
			return nil, fmt.Errorf("画布断点记录已存在，但素材断点记录缺失；为避免重复上传，已停止处理：%s", opts.CheckpointPath)
		} else if !os.IsNotExist(journalErr) {
			return nil, fmt.Errorf("上传素材前检查画布导入断点记录失败：%w", journalErr)
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
		return nil, fmt.Errorf("打开画布导入素材断点记录失败：%w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxMediaCheckpointBytes+1))
	decoder.DisallowUnknownFields()
	var checkpoint mediaCheckpoint
	if err := decoder.Decode(&checkpoint); err != nil {
		return nil, fmt.Errorf("解析画布导入素材断点记录失败：%w", err)
	}
	if err := ensureImportJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("解析画布导入素材断点记录失败：%w", err)
	}
	if checkpoint.Schema != mediaCheckpointSchema || checkpoint.Source != opts.Plan.Source || checkpoint.Target != opts.Target {
		return nil, fmt.Errorf("画布导入素材断点记录与当前来源或目标不匹配")
	}
	if err := os.Chmod(opts.CheckpointPath, 0o600); err != nil {
		return nil, fmt.Errorf("设置画布导入素材断点记录权限失败：%w", err)
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
			return nil, false, fmt.Errorf("画布导入素材在断点记录创建后发生了变化")
		}
		if !validRawMediaSHA256(entry.SHA256) || !validImportMediaContentFingerprint(item.ContentFingerprint) {
			return nil, false, fmt.Errorf("画布导入素材 %q 的断点指纹无效", entry.LogicalID)
		}
		if item.SHA256 != entry.SHA256 {
			if item.MediaType != "image" {
				return nil, false, fmt.Errorf("画布导入素材在断点记录创建后发生了变化")
			}
			if entry.ContentFingerprint == "" || entry.CanonicalByteSize == 0 {
				previousIdentity, err := findLegacyCheckpointImageIdentity(checkpoint, opts, *entry)
				if err != nil {
					return nil, false, fmt.Errorf("验证断点记录中已变化的图片 %q 失败：%w", entry.LogicalID, err)
				}
				if !validImportMediaContentFingerprint(previousIdentity.ContentFingerprint) {
					return nil, false, fmt.Errorf("画布导入图片 %q 的断点内容指纹无效", entry.LogicalID)
				}
				if entry.ContentFingerprint != "" && entry.ContentFingerprint != previousIdentity.ContentFingerprint {
					return nil, false, fmt.Errorf("画布导入图片 %q 的断点内容指纹与保留的导出包不一致", entry.LogicalID)
				}
				if entry.CanonicalByteSize != 0 && entry.CanonicalByteSize != previousIdentity.ByteSize {
					return nil, false, fmt.Errorf("画布导入图片 %q 的断点文件大小与保留的导出包不一致", entry.LogicalID)
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
				return nil, false, fmt.Errorf("画布导入图片内容在断点记录创建后发生了变化")
			}
		} else {
			if entry.ContentFingerprint != "" &&
				(!validImportMediaContentFingerprint(entry.ContentFingerprint) || entry.ContentFingerprint != item.ContentFingerprint) {
				return nil, false, fmt.Errorf("画布导入素材内容在断点记录创建后发生了变化")
			}
			if entry.ContentFingerprint == "" {
				entry.ContentFingerprint = item.ContentFingerprint
				migrated = true
			}
			if entry.CanonicalByteSize != 0 && entry.CanonicalByteSize != item.ByteSize {
				return nil, false, fmt.Errorf("画布导入素材大小在断点记录创建后发生了变化")
			}
			if entry.CanonicalByteSize == 0 {
				entry.CanonicalByteSize = item.ByteSize
				migrated = true
			}
		}
		if _, duplicate := entries[entry.LogicalID]; duplicate {
			return nil, false, fmt.Errorf("画布导入素材断点记录包含重复的逻辑 ID %q", entry.LogicalID)
		}
		if (entry.Status == mediaStatusReady || entry.Status == mediaStatusProcessing) &&
			(strings.TrimSpace(entry.AssetID) == "" || strings.TrimSpace(entry.PippitAssetID) == "") {
			return nil, false, fmt.Errorf("画布导入素材 %q 的断点记录缺少可持久化 ID", entry.LogicalID)
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
		return canvasplan.Plan{}, fmt.Errorf("加载画布导入素材的规范标识失败：%w", err)
	}
	entries := make(map[string]mediaCheckpointEntry, len(checkpoint.Entries))
	for _, entry := range checkpoint.Entries {
		if _, exists := entries[entry.LogicalID]; exists {
			return canvasplan.Plan{}, fmt.Errorf("画布导入素材断点记录包含重复的逻辑 ID %q", entry.LogicalID)
		}
		entries[entry.LogicalID] = entry
	}
	canonical := plan
	canonical.RequiredMedia = append([]canvasplan.MediaRequirement(nil), plan.RequiredMedia...)
	for index := range canonical.RequiredMedia {
		requirement := &canonical.RequiredMedia[index]
		entry, ok := entries[requirement.LogicalID]
		if !ok || entry.MediaType != requirement.MediaType || entry.Status != mediaStatusReady {
			return canvasplan.Plan{}, fmt.Errorf("画布导入素材断点记录中没有 %q 的可用规范标识", requirement.LogicalID)
		}
		if !validRawMediaSHA256(entry.SHA256) || entry.CanonicalByteSize <= 0 {
			return canvasplan.Plan{}, fmt.Errorf("画布导入素材 %q 的规范标识无效", requirement.LogicalID)
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
		return fmt.Errorf("编码画布导入素材断点记录失败：%w", err)
	}
	payload = append(payload, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("创建画布导入素材断点记录目录失败：%w", err)
	}
	temporary, err := os.CreateTemp(directory, ".canvas-import-media-*")
	if err != nil {
		return fmt.Errorf("创建临时画布导入素材断点记录失败：%w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("设置临时画布导入素材断点记录权限失败：%w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		return fmt.Errorf("写入画布导入素材断点记录失败：%w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("同步画布导入素材断点记录失败：%w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭画布导入素材断点记录失败：%w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("替换画布导入素材断点记录失败：%w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("设置画布导入素材断点记录权限失败：%w", err)
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
			fmt.Fprintf(stderr, "提示：无法删除本地 LibTV 导出包 %s：%v\n", bundleDir, err)
			remaining = append(remaining, bundleDir)
		}
	}
	checkpoint.BundleDirs = remaining
	if err := saveMediaCheckpoint(opts.CheckpointPath, checkpoint); err != nil {
		fmt.Fprintf(stderr, "提示：无法更新素材断点记录的清理状态：%v\n", err)
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
