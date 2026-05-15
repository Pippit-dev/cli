package novel

// SubmitRunOptions is the stable command-facing request shape for novel run submission.
type SubmitRunOptions struct {
	Message  string   `json:"message"`
	ThreadID string   `json:"thread_id,omitempty"`
	AssetIDs []string `json:"asset_ids,omitempty"`
}

// SubmitRunResult is the JSON envelope printed by `pippit-cli novel +submit-run`.
type SubmitRunResult struct {
	ThreadID      string `json:"thread_id"`
	RunID         string `json:"run_id"`
	WebThreadLink string `json:"web_thread_link"`
}

// UploadFileOptions is the stable command-facing request shape for file upload.
type UploadFileOptions struct {
	Path     string `json:"path"`
	FileName string `json:"file_name"`
	Purpose  string `json:"purpose"`
	Mock     bool   `json:"mock"`
}

// UploadFileResult is the JSON envelope printed by `pippit-cli novel +upload-file`.
type UploadFileResult struct {
	Scene    string            `json:"scene"`
	FileID   string            `json:"file_id"`
	Status   string            `json:"status"`
	Uploaded string            `json:"uploaded_at"`
	Request  UploadFileOptions `json:"request"`
}

// GetThreadOptions is the stable command-facing request shape for thread lookup.
type GetThreadOptions struct {
	ThreadID string `json:"thread_id"`
	Mock     bool   `json:"mock"`
}

// ThreadRun is a compact run summary inside a thread.
type ThreadRun struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
	Scene  string `json:"scene"`
}

// GetThreadResult is the JSON envelope printed by `pippit-cli novel +get-thread`.
type GetThreadResult struct {
	Scene    string           `json:"scene"`
	ThreadID string           `json:"thread_id"`
	Status   string           `json:"status"`
	Runs     []ThreadRun      `json:"runs"`
	Request  GetThreadOptions `json:"request"`
}
