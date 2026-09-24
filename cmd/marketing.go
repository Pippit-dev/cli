package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/spf13/cobra"
)

var marketingPaths = map[string]string{
	"generate": "/api/biz/v1/agent/submit_marketing_run",
	"query":    "/api/biz/v1/agent/query_generate_video_result",
	"upload":   config.UploadFilePath,
	"balance":  config.GetCreditBalancePath,
}

// Marketing uses the same AuthManager as login/status and all other commands.
// Only fixed public endpoints are exposed; credentials never leave the CLI.
func newMarketingCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	root := &cobra.Command{Use: "marketing", Short: "Marketing API using the shared CLI login"}
	root.SetOut(stdout)
	root.SetErr(stderr)
	for _, action := range []string{"generate", "query", "upload", "balance"} {
		root.AddCommand(newMarketingAction(action, stdout, stderr, runner))
	}
	return root
}

func newMarketingAction(action string, stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var requestFile, file, threadID, runID, source string
	var execute bool
	var timeout time.Duration
	command := &cobra.Command{Use: action, Args: cobra.NoArgs, Short: "Call marketing " + action}
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "request deadline (e.g. 60s)")
	switch action {
	case "generate":
		command.Flags().StringVar(&source, "source", "", "optional host agent/platform identifier for statistics only; filled silently by the host agent (e.g. doubao_office, workbuddy, codex)")
		command.Flags().StringVar(&requestFile, "request", "", "request JSON file, or - for stdin")
		command.Flags().BoolVar(&execute, "execute", false, "submit generation; otherwise preview only")
	case "query":
		command.Flags().StringVar(&threadID, "thread-id", "", "marketing thread ID")
		command.Flags().StringVar(&runID, "run-id", "", "marketing run ID")
	case "upload":
		command.Flags().StringVar(&file, "file", "", "local media file")
	}
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		if timeout <= 0 || timeout > 30*time.Minute {
			return fmt.Errorf("timeout 必须大于 0 且不超过 30m")
		}
		var body any = map[string]any{}
		switch action {
		case "generate":
			if requestFile == "" {
				return fmt.Errorf("缺少必填参数 --request")
			}
			reader := cmd.InOrStdin()
			if requestFile != "-" {
				f, err := os.Open(requestFile)
				if err != nil {
					return err
				}
				defer f.Close()
				reader = f
			}
			var value map[string]json.RawMessage
			decoder := json.NewDecoder(io.LimitReader(reader, 8*1024*1024+1))
			if err := decoder.Decode(&value); err != nil {
				return fmt.Errorf("请求 JSON 无效: %w", err)
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				return fmt.Errorf("请求只能包含一个 JSON 对象")
			}
			for key := range value {
				if key != "message" && key != "asset_ids" && key != "thread_id" && key != "general_agent_settings" {
					return fmt.Errorf("未支持的营销请求字段: %s", key)
				}
			}
			var message string
			var settings struct {
				VideoModel string `json:"video_model"`
			}
			if json.Unmarshal(value["message"], &message) != nil || strings.TrimSpace(message) == "" {
				return fmt.Errorf("message 必须为非空字符串")
			}
			if json.Unmarshal(value["general_agent_settings"], &settings) != nil || strings.TrimSpace(settings.VideoModel) == "" {
				return fmt.Errorf("general_agent_settings.video_model 必填")
			}
			if platform := strings.TrimSpace(source); platform != "" {
				value["platform"], _ = json.Marshal(platform)
			}
			body = value
			if !execute {
				return common.WriteJSON(stdout, map[string]any{"dry_run": true, "url": config.DefaultBaseURL + marketingPaths[action], "body": body})
			}
		case "query":
			if strings.TrimSpace(threadID) == "" || strings.TrimSpace(runID) == "" {
				return fmt.Errorf("query 需要 --thread-id 和 --run-id")
			}
			body = map[string]string{"thread_id": threadID, "run_id": runID}
		case "upload":
			if err := validateMediaUpload(file); err != nil {
				return err
			}
			info, err := os.Stat(file)
			if err != nil {
				return err
			}
			if info.Size() == 0 {
				return fmt.Errorf("上传需要非空文件")
			}
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		client := common.NewNonRedirectingHTTPClient(runner.Config.BaseURL, timeout, newRunnerAuthorizer(runner))
		var result map[string]json.RawMessage
		var err error
		if action == "upload" {
			contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(file)))
			err = client.SendMultipartRequest(ctx, marketingPaths[action], nil, common.MultipartFile{FieldName: "file", Path: file, ContentType: contentType}, &result)
		} else {
			err = client.SendRequest(ctx, marketingPaths[action], body, &result)
		}
		if err != nil {
			return err
		}
		ret := strings.TrimSpace(string(result["ret"]))
		if ret != `"0"` && ret != "0" {
			var message, logID string
			_ = json.Unmarshal(result["errmsg"], &message)
			_ = json.Unmarshal(result["log_id"], &logID)
			return common.NewLogIDError(fmt.Sprintf("营销 API 失败: ret=%s errmsg=%s", ret, message), logID)
		}
		return common.WriteJSON(stdout, result)
	}
	return command
}
