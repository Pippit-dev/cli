package common

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"time"
)

// UploadFileOptions is the stable command-facing request shape for file upload.
type UploadFileOptions struct {
	Path     string `json:"path"`
	FileName string `json:"file_name"`
	Purpose  string `json:"purpose"`
	Mock     bool   `json:"mock"`
}

// UploadFileResult is the JSON envelope printed by `pippit-cli short-drama +upload-file`.
type UploadFileResult struct {
	Scene    string            `json:"scene"`
	FileID   string            `json:"file_id"`
	Status   string            `json:"status"`
	Uploaded string            `json:"uploaded_at"`
	Request  UploadFileOptions `json:"request"`
}

func UploadFile(ctx context.Context, opts UploadFileOptions, _ *Runner) (*UploadFileResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := opts.Purpose + "\x00" + opts.FileName + "\x00" + opts.Path
	id := stableID(key, 10)
	return &UploadFileResult{
		Scene:    "short-drama",
		FileID:   "file_mock_" + id,
		Status:   "uploaded",
		Uploaded: time.Now().UTC().Format(time.RFC3339),
		Request:  opts,
	}, nil
}

func stableID(key string, n int) string {
	sum := sha1.Sum([]byte(key))
	id := hex.EncodeToString(sum[:])
	if n > len(id) {
		return id
	}
	return id[:n]
}
