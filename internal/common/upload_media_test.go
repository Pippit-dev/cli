package common

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/space"
)

type mediaUploadClient struct {
	Client
	upload func(context.Context, MultipartFile, any) error
}

func (c mediaUploadClient) SendMultipartRequest(ctx context.Context, _ string, _ map[string]string, file MultipartFile, out any) error {
	return c.upload(ctx, file, out)
}

func TestUploadMediaGroupsConcurrentAndOrdered(t *testing.T) {
	const wantConcurrency = 10
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	groups := []UploadMediaGroup{{Label: "张参考图片"}, {Label: "个参考视频"}, {Label: "段参考音频"}}
	want := [][]string{
		{"a.jpg", "b.jpg", "c.jpg", "d.jpg"},
		{"a.mp4", "b.mp4", "c.mp4", "d.mp4"},
		{"a.wav", "b.wav", "c.wav", "d.wav"},
	}
	release := make(map[string]chan struct{})
	dir := t.TempDir()
	for g, names := range want {
		for _, name := range names {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte("media"), 0600); err != nil {
				t.Fatal(err)
			}
			groups[g].Paths = append(groups[g].Paths, path)
			release[name] = make(chan struct{})
		}
	}
	started := make(chan string, len(release))
	var active, peak atomic.Int32
	client := mediaUploadClient{upload: func(ctx context.Context, file MultipartFile, out any) error {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- file.FileName
		select {
		case <-release[file.FileName]:
		case <-ctx.Done():
			return ctx.Err()
		}
		response := out.(*uploadFileResponse)
		response.Ret = "0"
		response.Data.PippitAssetID = file.FileName
		return nil
	}}
	type outcome struct {
		ids [][]string
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		ids, err := UploadMediaGroups(ctx, &Runner{Client: client}, groups...)
		done <- outcome{ids, err}
	}()
	var first []string
	for i := 0; i < wantConcurrency; i++ {
		select {
		case name := <-started:
			first = append(first, name)
		case <-ctx.Done():
			t.Fatal("uploads did not start concurrently")
		}
	}
	// Release the first requests in reverse order; IDs must still match inputs.
	for i := len(first) - 1; i >= 0; i-- {
		close(release[first[i]])
	}
	for i := len(first); i < len(release); i++ {
		select {
		case name := <-started:
			close(release[name])
		case <-ctx.Done():
			t.Fatal("remaining uploads did not start")
		}
	}
	select {
	case got := <-done:
		if got.err != nil || !reflect.DeepEqual(got.ids, want) {
			t.Fatalf("UploadMediaGroups() = %v, %v; want %v", got.ids, got.err, want)
		}
	case <-ctx.Done():
		t.Fatal("uploads did not finish")
	}
	if got := peak.Load(); got != wantConcurrency {
		t.Fatalf("peak uploads = %d, want %d", got, wantConcurrency)
	}
}

func TestUploadMediaGroupsPreservesSpace(t *testing.T) {
	for _, teamID := range []string{"0", "12"} {
		t.Run(teamID, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantHeader := teamID
				if teamID == "0" {
					wantHeader = ""
				}
				if r.Header.Get("X-Team-Id") != wantHeader || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("upload lost selected space or authentication")
				}
				if r.URL.Path != config.UploadFilePath {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				file, header, err := r.FormFile("file")
				if err != nil {
					t.Errorf("FormFile(): %v", err)
					http.Error(w, "invalid upload", http.StatusBadRequest)
					return
				}
				file.Close()
				defer r.MultipartForm.RemoveAll()
				fmt.Fprintf(w, `{"ret":"0","data":{"pippit_asset_id":%q}}`, header.Filename)
			}))
			defer api.Close()
			store := &space.Store{Dir: t.TempDir(), Account: "account", Origin: api.URL}
			var stderr bytes.Buffer
			session := &space.Session{Store: store, Explicit: &teamID, Stderr: &stderr}
			ctx := WithSpace(context.Background(), session)
			var paths []string
			dir := t.TempDir()
			for i := 0; i < 7; i++ {
				path := filepath.Join(dir, fmt.Sprintf("image-%d.jpg", i))
				if err := os.WriteFile(path, []byte("image"), 0600); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, path)
			}
			runner := &Runner{Client: NewHTTPClient(api.URL, 5*time.Second, NewAccessKeyAuthorizer("test-key"))}
			ids, err := UploadMediaGroups(ctx, runner, UploadMediaGroup{Label: "张参考图片", Paths: paths})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(stderr.String(), "本次空间：") != 1 || session.TeamID() != teamID {
				t.Fatalf("space selected more than once or changed: %s", stderr.String())
			}
			binding, err := space.DecodeBinding(session.Token())
			if err != nil || binding.TeamID != teamID {
				t.Fatalf("space token changed: %+v, %v", binding, err)
			}
			for _, id := range ids[0] {
				ref := space.References{AssetIDs: []string{id}}
				selected, err := (&space.Session{Store: store, Explicit: &teamID}).Resolve(ref)
				if err != nil || selected.TeamID != teamID {
					t.Fatalf("asset %s lost its space: %+v, %v", id, selected, err)
				}
				other := "34"
				if _, err := (&space.Session{Store: store, Explicit: &other}).Resolve(ref); err == nil {
					t.Fatalf("asset %s accepted in another team", id)
				}
			}
		})
	}
}

func TestUploadMediaGroupsCanceledBeforeUpload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := mediaUploadClient{upload: func(context.Context, MultipartFile, any) error {
		t.Error("canceled batch sent an upload request")
		return nil
	}}
	ids, err := UploadMediaGroups(ctx, &Runner{Client: client}, UploadMediaGroup{Label: "张参考图片", Paths: []string{"a.jpg", "b.jpg"}})
	if ids != nil || err == nil || strings.Count(err.Error(), "上传文件已取消") != 2 {
		t.Fatalf("canceled batch = %v, %v", ids, err)
	}
}
