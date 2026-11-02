package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

func imageBindingFixture(node string, patches []PatchEntry) (ApplyOptions, *common.Runner, *int) {
	opts, _, writes := applyIdentityFixture(patches, imageMaterial())
	client := &fakeClient{send: func(_ context.Context, path string, body, out any) error {
		switch path {
		case QueryPath:
			request := body.(queryRequest)
			var assets []json.RawMessage
			for _, id := range request.PippitAssetIDs {
				if id == testNodeID {
					content, _ := json.Marshal(node)
					assets = append(assets, json.RawMessage(fmt.Sprintf(`{"PippitAssetID":"%s","TextInfo":{"content":%s},"version":5}`, id, content)))
				} else if id == testPippitID {
					assets = append(assets, json.RawMessage(imageMaterial()))
				}
			}
			payload, _ := json.Marshal(map[string]any{"ret": "0", "data": map[string]any{"Assets": assets}})
			return decodeInto(out, string(payload))
		case ApplyPath:
			*writes++
			return decodeInto(out, fmt.Sprintf(`{"ret":"0","data":{"results":[{"transaction_id":"tx","status":"ack","asset_versions":{"%s":6}}]}}`, testNodeID))
		default:
			return fmt.Errorf("unexpected request %s", path)
		}
	}}
	return opts, runnerWithClient(client), writes
}

func bindingPatch(op, path, value string) PatchEntry {
	return PatchEntry{AssetID: testNodeID, Op: op, Path: path, Value: json.RawMessage(value)}
}

func TestApplyMediaIdentityRejectsRoleFieldsOnImageBeforeWrite(t *testing.T) {
	blank := `{"type":"biz/image","content":{"caption":"draft","generation":{}}}`
	for _, test := range []struct {
		name, node string
		patches    []PatchEntry
	}{
		{"original-upload", blank, []PatchEntry{
			bindingPatch("add", "/content/appearanceInfo", `{"pippitAssetId":"image-material"}`),
			bindingPatch("add", "/content/generation", `{"source":"uploaded"}`),
			bindingPatch("add", "/content/sourceInfos", `{"image-material":{"source":"uploaded"}}`),
		}},
		{"source-map", blank, []PatchEntry{bindingPatch("add", "/content/sourceInfos", `{"image-material":{}}`)}},
		{"source-array", blank, []PatchEntry{bindingPatch("add", "/content/sourceInfos", `[{"pippitAssetId":"image-material"}]`)}},
		{"root-create", "", []PatchEntry{bindingPatch("add", "", `{"type":"biz/image","content":{"appearanceInfo":{"pippitAssetId":"image-material"}}}`)}},
		{"root-replace", blank, []PatchEntry{bindingPatch("replace", "", `{"type":"biz/image","content":{"sourceInfos":{"image-material":{}}}}`)}},
		{"content-replace", blank, []PatchEntry{bindingPatch("replace", "/content", `{"appearanceInfo":{"candidateId":"candidate"}}`)}},
		{"nested-appearance", `{"type":"biz/image","content":{"appearanceInfo":{}}}`, []PatchEntry{bindingPatch("add", "/content/appearanceInfo/pippitAssetId", `"image-material"`)}},
		{"nested-source-map", `{"type":"biz/image","content":{"sourceInfos":{}}}`, []PatchEntry{bindingPatch("add", "/content/sourceInfos/material", `{"pippitAssetId":"image-material"}`)}},
		{"nested-source-array", `{"type":"biz/image","content":{"sourceInfos":[]}}`, []PatchEntry{bindingPatch("add", "/content/sourceInfos/-", `{"pippitAssetId":"image-material"}`)}},
		{"changed-legacy-field", `{"type":"biz/image","content":{"appearanceInfo":{"pippitAssetId":"old"}}}`, []PatchEntry{bindingPatch("replace", "/content/appearanceInfo/pippitAssetId", `"new"`)}},
		{"remove-one-legacy-source", `{"type":"biz/image","content":{"sourceInfos":{"first":{},"second":{}}}}`, []PatchEntry{bindingPatch("remove", "/content/sourceInfos/first", "")}},
		{"unchanged-direct-identities", string(imageNode(testMediaID)), []PatchEntry{bindingPatch("add", "/content/sourceInfos", `{"material":{}}`)}},
		{"type-change", `{"type":"biz/role","content":{"appearanceInfo":{"pippitAssetId":"material"}}}`, []PatchEntry{bindingPatch("replace", "/type", `"biz/image"`)}},
		{"case-alias", blank, []PatchEntry{bindingPatch("add", "/content/AppearanceInfo", `{"pippitAssetId":"material"}`)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts, runner, writes := imageBindingFixture(test.node, test.patches)
			_, err := Apply(context.Background(), opts, runner)
			if err == nil || !strings.Contains(err.Error(), "INVALID_MEDIA_IDENTITY") || !strings.Contains(err.Error(), "/content/") || !strings.Contains(err.Error(), "content.pippitAssetId") {
				t.Fatalf("expected corrective image binding error, got %v", err)
			}
			if *writes != 0 {
				t.Fatalf("invalid image binding was sent %d times", *writes)
			}
		})
	}
}

func TestApplyMediaIdentityPreservesImageDraftAndLegacyEdits(t *testing.T) {
	legacy := `{"type":"biz/image","content":{"caption":"old","appearanceInfo":{"pippitAssetId":"material","candidateId":"candidate"},"sourceInfos":{"material":{}}}}`
	for _, test := range []struct {
		name, node string
		patches    []PatchEntry
	}{
		{"blank-create", "", []PatchEntry{bindingPatch("add", "", `{"type":"biz/image","content":{"caption":"draft"}}`)}},
		{"legacy-caption", legacy, []PatchEntry{bindingPatch("replace", "/content/caption", `"edited"`)}},
		{"unchanged-content", legacy, []PatchEntry{bindingPatch("replace", "/content", `{"sourceInfos":{"material":{}},"appearanceInfo":{"candidateId":"candidate","pippitAssetId":"material"},"caption":"edited"}`)}},
		{"unchanged-root", legacy, []PatchEntry{bindingPatch("replace", "", `{"type":"biz/image","content":{"sourceInfos":{"material":{}},"appearanceInfo":{"candidateId":"candidate","pippitAssetId":"material"},"caption":"edited"}}`)}},
		{"unchanged-nested-field", legacy, []PatchEntry{bindingPatch("replace", "/content/appearanceInfo/pippitAssetId", `"material"`)}},
		{"remove-field", legacy, []PatchEntry{bindingPatch("remove", "/content/appearanceInfo", "")}},
		{"clear-object", legacy, []PatchEntry{bindingPatch("replace", "/content/appearanceInfo", `{}`)}},
		{"clear-array", legacy, []PatchEntry{bindingPatch("replace", "/content/sourceInfos", `[]`)}},
		{"clear-null", legacy, []PatchEntry{bindingPatch("replace", "/content/sourceInfos", `null`)}},
		{"clear-blank", legacy, []PatchEntry{bindingPatch("replace", "/content/appearanceInfo", `" "`)}},
		{"clear-last-object-key", `{"type":"biz/image","content":{"sourceInfos":{"a/b~c":{}}}}`, []PatchEntry{bindingPatch("remove", "/content/sourceInfos/a~1b~0c", "")}},
		{"clear-last-array-item", `{"type":"biz/image","content":{"sourceInfos":[{"pippitAssetId":"material"}]}}`, []PatchEntry{bindingPatch("remove", "/content/sourceInfos/0", "")}},
		{"final-role-type", `{"type":"biz/image","content":{}}`, []PatchEntry{
			bindingPatch("add", "/content/appearanceInfo", `{"pippitAssetId":"material"}`),
			bindingPatch("replace", "/type", `"biz/role"`),
		}},
		{"final-empty-binding", `{"type":"biz/image","content":{}}`, []PatchEntry{
			bindingPatch("add", "/content/appearanceInfo", `{"pippitAssetId":"material"}`),
			bindingPatch("remove", "/content/appearanceInfo", ""),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts, runner, writes := imageBindingFixture(test.node, test.patches)
			if _, err := Apply(context.Background(), opts, runner); err != nil {
				t.Fatal(err)
			}
			if *writes != 1 {
				t.Fatalf("writes = %d", *writes)
			}
		})
	}
}

func TestApplyMediaIdentityLeavesOtherNodeBindingsUnchanged(t *testing.T) {
	for _, kind := range []string{"role", "scene", "audio", "video"} {
		t.Run(kind, func(t *testing.T) {
			node := fmt.Sprintf(`{"type":"biz/%s","content":{"appearanceInfo":{},"sourceInfos":[]}}`, kind)
			opts, runner, writes := imageBindingFixture(node, []PatchEntry{
				bindingPatch("add", "/content/appearanceInfo/pippitAssetId", `"material"`),
				bindingPatch("add", "/content/sourceInfos/-", `{"pippitAssetId":"material"}`),
			})
			if _, err := Apply(context.Background(), opts, runner); err != nil {
				t.Fatal(err)
			}
			if *writes != 1 {
				t.Fatalf("writes = %d", *writes)
			}
		})
	}
}
