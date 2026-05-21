package novel

// SubmitRunOptions is the stable command-facing request shape for novel run submission.
type SubmitRunOptions struct {
	Message   string   `json:"message"`
	ThreadID  string   `json:"thread_id,omitempty"`
	AssetIDs  []string `json:"asset_ids,omitempty"`
	AgentName string   `json:"agent_name,omitempty"`
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

// DownloadResultsOptions is the command-facing shape for downloading result URLs.
type DownloadResultsOptions struct {
	URLs      []string `json:"urls"`
	OutputDir string   `json:"output_dir"`
	Workers   int      `json:"workers"`
}

type DownloadResultsError struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// DownloadResultsResult is the JSON envelope printed by `pippit-cli novel +download-results`.
type DownloadResultsResult struct {
	OutputDir  string                  `json:"output_dir"`
	Downloaded []string                `json:"downloaded"`
	Total      int                     `json:"total"`
	Errors     []*DownloadResultsError `json:"errors,omitempty"`
}

// GetThreadOptions is the stable command-facing request shape for thread lookup.
type GetThreadOptions struct {
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id,omitempty"`
	AfterSeq int    `json:"after_seq"`
}

// ThreadEntry is a compact message or artifact entry inside a thread run.
type ThreadEntry struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

// GetThreadResult is the JSON envelope printed by `pippit-cli novel +get-thread`.
type GetThreadResult struct {
	Messages []*ThreadEntry `json:"messages"`
}
