package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

func TestGenerationCollectsUploadFailuresBeforeReturning(t *testing.T) {
	for _, command := range []string{"generate-image", "generate-video"} {
		t.Run(command, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("LocalAppData", t.TempDir())
			type material struct {
				flag, name, failure string
				missing             bool
			}
			materials := []material{
				{flag: "image", name: "rejected-a.jpg", failure: `第1张参考图片 "rejected-a.jpg" 上传失败: 上传文件请求返回失败: ret=1 errmsg=图片不符合社区规范 log_id=log-rejected-a.jpg`},
				{flag: "image", name: "accepted.jpg"},
				{flag: "image", name: "rejected-b.jpg", failure: `第3张参考图片 "rejected-b.jpg" 上传失败: 上传文件请求返回失败: ret=1 errmsg=图片不符合社区规范 log_id=log-rejected-b.jpg`},
			}
			if command == "generate-video" {
				materials = append(materials,
					material{flag: "video", name: "rejected.mp4", failure: `第1个参考视频 "rejected.mp4" 上传失败: 上传文件请求返回失败: ret=1 errmsg=视频不符合社区规范 log_id=log-rejected.mp4`},
					material{flag: "video", name: "accepted.mp4"},
					material{flag: "audio", name: "missing.wav", missing: true, failure: `第1段参考音频 "missing.wav" 上传失败: 上传文件不存在:`},
					material{flag: "audio", name: "unavailable.wav", failure: `第2段参考音频 "unavailable.wav" 上传失败: 上传文件请求返回失败: ret=1 errmsg=服务暂不可用 log_id=log-unavailable.wav`},
				)
			}
			args := []string{command, "--prompt", "test"}
			if command == "generate-image" {
				args = append(args, "--model", "Seedream 5.0 Pro")
			}
			dir := t.TempDir()
			for _, item := range materials {
				path := filepath.Join(dir, item.name)
				if !item.missing {
					if err := os.WriteFile(path, []byte("media"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				args = append(args, "--"+item.flag, path)
			}
			var mu sync.Mutex
			seen := make(map[string]int)
			var submits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case config.GetAvailableModelListPath:
					_, _ = w.Write([]byte(`{"ret":"0","data":{"scene":"web_image_agent","config_key":"image-key","config":{"models":[{"key":"seedream_5.0_pro","name":"Seedream 5.0 Pro","kind":"image"}]}}}`))
				case config.UploadFilePath:
					file, header, err := r.FormFile("file")
					if err != nil {
						t.Errorf("FormFile(): %v", err)
						http.Error(w, "invalid upload", http.StatusBadRequest)
						return
					}
					file.Close()
					defer r.MultipartForm.RemoveAll()
					mu.Lock()
					seen[header.Filename]++
					mu.Unlock()
					switch {
					case strings.HasPrefix(header.Filename, "accepted"):
						fmt.Fprintf(w, `{"ret":"0","data":{"pippit_asset_id":%q}}`, header.Filename)
					case header.Filename == "unavailable.wav":
						fmt.Fprintf(w, `{"ret":"1","errmsg":"服务暂不可用","log_id":%q}`, "log-"+header.Filename)
					default:
						reason := "图片不符合社区规范"
						if strings.HasSuffix(header.Filename, ".mp4") {
							reason = "视频不符合社区规范"
						}
						fmt.Fprintf(w, `{"ret":"1","errmsg":%q,"log_id":%q}`, reason, "log-"+header.Filename)
					}
				case config.SubmitRunPath:
					submits.Add(1)
					http.Error(w, "must not submit", http.StatusInternalServerError)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			root.SetArgs(args)
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "未提交生成") {
				t.Fatalf("error = %v, want upload failure summary", err)
			}
			var logErr *common.LogIDError
			if !errors.As(err, &logErr) || logErr.LogID() != "log-rejected-a.jpg" {
				t.Errorf("aggregated error lost the original LogID error chain: %v", err)
			}
			lastPosition := -1
			mu.Lock()
			defer mu.Unlock()
			for _, item := range materials {
				wantRequests := 1
				if item.missing {
					wantRequests = 0
				}
				if seen[item.name] != wantRequests {
					t.Errorf("%s uploaded %d times, want %d", item.name, seen[item.name], wantRequests)
				}
				if item.failure != "" {
					position := strings.Index(err.Error(), item.failure)
					if position <= lastPosition {
						t.Errorf("failure missing or out of order: %s; error: %v", item.failure, err)
					}
					lastPosition = position
				}
			}
			if submits.Load() != 0 || stdout.Len() != 0 {
				t.Fatalf("failed uploads created a task: submits=%d stdout=%s", submits.Load(), stdout.String())
			}
		})
	}
}
