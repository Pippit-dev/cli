package common

import "github.com/Pippit-dev/pippit-cli/internal/config"

// SubmitRunResponse is the shared response envelope returned by submit_run.
type SubmitRunResponse struct {
	Ret    string                `json:"ret"`
	Errmsg string                `json:"errmsg"`
	LogID  string                `json:"log_id"`
	Data   SubmitRunResponseData `json:"data"`
}

type SubmitRunResponseData struct {
	WebThreadLink string               `json:"web_thread_link"`
	Run           SubmitRunResponseRun `json:"run"`
}

type SubmitRunResponseRun struct {
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id"`
}

// SubmitRunPath returns the configured submit_run endpoint path.
func SubmitRunPath(runner *Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.SubmitRun != "" {
		return runner.Config.Paths.SubmitRun
	}
	return config.SubmitRunPath
}
