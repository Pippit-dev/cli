package canvas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/space"
)

func TestApplyQueryPreservesFinalHTTPURL(t *testing.T) {
	for _, command := range []string{"", "xyq.role.updateDescription", "fixture.command +/&=?"} {
		t.Run(command, func(t *testing.T) {
			original := ApplyPath + "?project_id=123&trace=a%2Fb&repeat=1&repeat=2"
			path, err := pathWithApplyQuery(original, "", command)
			if err != nil {
				t.Fatal(err)
			}
			if command == "" && path != original {
				t.Fatal("no metadata must preserve the original request path")
			}
			body := map[string]any{"intent": "caller supplied intent", "value": "unchanged +/&=?"}
			calls := make(chan struct{}, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls <- struct{}{}
				query := r.URL.Query()
				if query.Get("command") != command || query.Has("command") != (command != "") ||
					query.Get("project_id") != "123" || query.Get("trace") != "a/b" ||
					!reflect.DeepEqual(query["repeat"], []string{"1", "2"}) {
					t.Errorf("query = %#v", query)
				}
				if command != "" && !strings.Contains("&"+r.URL.RawQuery+"&", "&command="+url.QueryEscape(command)+"&") {
					t.Errorf("command encoding changed: %s", r.URL.RawQuery)
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil || !reflect.DeepEqual(got, body) {
					t.Errorf("body changed: %#v, err=%v", got, err)
				}
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("X-Team-Id") != "12" {
					t.Error("method, authentication or selected space changed")
				}
				t.Logf("captured final HTTP URL: %s", r.URL.RequestURI())
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			teamID := "12"
			ctx := common.WithSpace(context.Background(), &space.Session{Explicit: &teamID})
			client := common.NewHTTPClient(server.URL, time.Second, common.NewAccessKeyAuthorizer("fixture-key"))
			if err := client.SendRequest(ctx, path, body, nil); err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 {
				t.Fatalf("requests=%d, want one", len(calls))
			}
		})
	}
}
