package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const testPippitID = "10155947700492"
const testMediaID = "7693046454219440665"
const testNodeID = "10901596019212"

func imageNode(mediaID string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"biz/image","pippitAssetId":"%s","content":{"pippitAssetId":"%s","assetId":"%s","caption":"keep"}}`, testNodeID, testPippitID, mediaID))
}

func applyIdentityFixture(patches []PatchEntry, mediaRaw string) (ApplyOptions, *common.Runner, *int) {
	writes := 0
	client := &fakeClient{send: func(_ context.Context, path string, body, out any) error {
		switch path {
		case QueryPath:
			request := body.(queryRequest)
			var assets []json.RawMessage
			for _, id := range request.PippitAssetIDs {
				if id == testNodeID {
					content, _ := json.Marshal(string(imageNode(testMediaID)))
					assets = append(assets, json.RawMessage(fmt.Sprintf(`{"PippitAssetID":"%s","TextInfo":{"content":%s},"version":5}`, id, content)))
				} else if id == testPippitID {
					assets = append(assets, json.RawMessage(mediaRaw))
				}
			}
			payload, _ := json.Marshal(map[string]any{"ret": "0", "data": map[string]any{"Assets": assets}})
			return decodeInto(out, string(payload))
		case ApplyPath:
			writes++
			return decodeInto(out, fmt.Sprintf(`{"ret":"0","data":{"results":[{"transaction_id":"tx","status":"ack","asset_versions":{"%s":6}}]}}`, testNodeID))
		default:
			return fmt.Errorf("unexpected request %s", path)
		}
	}}
	return ApplyOptions{Request: ApplyRequest{
		BatchID: "batch", ClientID: "client", Transactions: []PatchTransaction{{TransactionID: "tx", Patches: patches}},
	}}, runnerWithClient(client), &writes
}

func imageMaterial() string {
	return fmt.Sprintf(`{"PippitAssetID":"%s","AssetType":1,"SourceID":"workspace-source-id","Image":{"asset_id":"%s"}}`, testPippitID, testMediaID)
}

func TestApplyMediaIdentityRejectsBadIDsBeforeWrite(t *testing.T) {
	for _, wrong := range []string{testPippitID, testNodeID, "workspace-source-id", "7693046454219440666"} {
		t.Run(wrong, func(t *testing.T) {
			opts, runner, writes := applyIdentityFixture([]PatchEntry{{AssetID: testNodeID, Op: "add", Path: "", Value: imageNode(wrong)}}, imageMaterial())
			_, err := Apply(context.Background(), opts, runner)
			if err == nil || !strings.Contains(err.Error(), "INVALID_MEDIA_IDENTITY") {
				t.Fatalf("error = %v", err)
			}
			if *writes != 0 {
				t.Fatalf("invalid data was written %d times", *writes)
			}
		})
	}
}

func TestApplyMediaIdentityChecksFinalTransactionAndAllowsValidPair(t *testing.T) {
	patches := []PatchEntry{
		{AssetID: testNodeID, Op: "add", Path: "", Value: imageNode(testPippitID)},
		{AssetID: testNodeID, Op: "replace", Path: "/content/assetId", Value: json.RawMessage(`"` + testMediaID + `"`)},
	}
	opts, runner, writes := applyIdentityFixture(patches, imageMaterial())
	if _, err := Apply(context.Background(), opts, runner); err != nil {
		t.Fatal(err)
	}
	if *writes != 1 {
		t.Fatalf("writes = %d", *writes)
	}
}

func TestApplyMediaIdentityRejectsPartialPatchBypass(t *testing.T) {
	opts, runner, writes := applyIdentityFixture([]PatchEntry{{AssetID: testNodeID, Op: "replace", Path: "/content/assetId", Value: json.RawMessage(`"` + testPippitID + `"`)}}, imageMaterial())
	if _, err := Apply(context.Background(), opts, runner); err == nil {
		t.Fatal("expected identity error")
	}
	if *writes != 0 {
		t.Fatal("partial invalid patch was written")
	}
}

func TestApplyMediaIdentityRejectsNumbersWrongTypeMissingOrUnreadyMaterial(t *testing.T) {
	for name, raw := range map[string]string{
		"numeric":    fmt.Sprintf(`{"PippitAssetID":"%s","AssetType":1,"Image":{"asset_id":7693046454219440665}}`, testPippitID),
		"wrong-type": fmt.Sprintf(`{"PippitAssetID":"%s","AssetType":2,"Image":{"asset_id":"%s"},"Video":{"asset_id":"video-id"}}`, testPippitID, testMediaID),
		"unready":    fmt.Sprintf(`{"PippitAssetID":"%s","AssetType":1,"Image":{}}`, testPippitID),
		"missing":    "null",
	} {
		t.Run(name, func(t *testing.T) {
			opts, runner, writes := applyIdentityFixture([]PatchEntry{{AssetID: testNodeID, Op: "add", Path: "", Value: imageNode(testMediaID)}}, raw)
			if _, err := Apply(context.Background(), opts, runner); err == nil {
				t.Fatal("expected identity preflight error")
			}
			if *writes != 0 {
				t.Fatal("invalid material was written")
			}
		})
	}
}

func TestMediaUploadLocatorDistinguishesSourceAndTypedMedia(t *testing.T) {
	for _, kind := range []string{"image", "video", "audio"} {
		nativeType := map[string]int{"image": 1, "video": 2, "audio": 3}[kind]
		raw := json.RawMessage(fmt.Sprintf(`{"PippitAssetID":"%s","AssetType":%d,"asset_id":"wrong-root-id","SourceID":"workspace-source-id","%s":{"asset_id":"%s"}}`, testPippitID, nativeType, strings.ToUpper(kind[:1])+kind[1:], testMediaID))
		locator := locatorFromAsset(raw, testPippitID)
		if locator.PippitAssetID != testPippitID || locator.MediaAssetID != testMediaID || locator.SourceID != "workspace-source-id" {
			t.Fatalf("locator = %#v", locator)
		}
	}
}

func TestUploadExposesDistinctIdentifiersAndKeepsUnreadyMediaProcessing(t *testing.T) {
	for _, ready := range []bool{true, false} {
		t.Run(fmt.Sprint(ready), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "reference.png")
			if err := os.WriteFile(file, []byte("image"), 0600); err != nil {
				t.Fatal(err)
			}
			raw := imageMaterial()
			if !ready {
				raw = fmt.Sprintf(`{"PippitAssetID":"%s","AssetType":1,"Image":{}}`, testPippitID)
			}
			client := &fakeClient{
				multipart: func(_ context.Context, _ string, _ map[string]string, _ common.MultipartFile, out any) error {
					return decodeInto(out, fmt.Sprintf(`{"ret":"0","data":{"asset_id":"workspace-source-id","pippit_asset_id":"%s"}}`, testPippitID))
				},
				send: func(_ context.Context, _ string, _ any, out any) error {
					return decodeInto(out, `{"ret":"0","data":{"Assets":[`+raw+`]}}`)
				},
			}
			result, err := Upload(context.Background(), UploadOptions{Path: file}, runnerWithClient(client))
			if err != nil {
				t.Fatal(err)
			}
			if result.PippitAssetID != testPippitID || result.SourceID != "workspace-source-id" || result.AssetID != result.SourceID {
				t.Fatalf("result = %#v", result)
			}
			if ready {
				if result.MediaAssetID != testMediaID || result.Locator.MediaAssetID != testMediaID || result.State != StateReady {
					t.Fatalf("ready result = %#v", result)
				}
			} else if result.MediaAssetID != "" || result.State != StateProcessing || result.Warning == "" {
				t.Fatalf("processing result = %#v", result)
			}
		})
	}
}
