package canvasplan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/canvas"
)

func TestMaterializeCanonicalDocumentWithoutTransientMediaLocations(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	mapping := map[string]string{
		"node:image":             "node-asset-image",
		"node:video":             "node-asset-video",
		"node:audio":             "node-asset-audio",
		"node:composite":         "node-asset-composite",
		"node:image-placeholder": "node-asset-image-placeholder",
		"node:video-placeholder": "node-asset-video-placeholder",
	}
	document, err := Materialize(plan, resolved, "root-asset", mapping)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if got, want := len(document.Assets), len(plan.Nodes)+1; got != want {
		t.Fatalf("asset count = %d, want %d", got, want)
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"https://source.example", "bundle/media"} {
		if bytes.Contains(payload, []byte(forbidden)) {
			t.Fatalf("materialized document persisted transient source %q: %s", forbidden, payload)
		}
	}

	root := decodeRawMap(t, document.Assets[document.RootCanvasID])
	content := asMap(t, root["content"])
	nodes := asMap(t, content["nodes"])
	group := asMap(t, nodes["group:scene"])
	children := asSlice(t, group["children"])
	if got := fmt.Sprint(children[0]); got != mapping["node:image"] {
		t.Fatalf("group first child = %q, want allocated ID", got)
	}
	edges := asMap(t, content["edges"])
	edge := asMap(t, edges["edge:reference"])
	if edge["source"] != mapping["node:video"] || edge["target"] != mapping["node:composite"] {
		t.Fatalf("edge endpoints were not remapped: %#v", edge)
	}

	image := companionContent(t, document, mapping["node:image"])
	if image["assetId"] != "upload-image" || image["pippitAssetId"] != "media-image" || image["sourceType"] != float64(4) {
		t.Fatalf("image content = %#v", image)
	}
	if image["naturalWidth"] != float64(1600) || image["naturalHeight"] != float64(900) {
		t.Fatalf("image dimensions = %#v", image)
	}
	video := companionContent(t, document, mapping["node:video"])
	if video["duration"] != float64(6) || video["frameWidth"] != float64(1280) || video["frameHeight"] != float64(720) {
		t.Fatalf("video content = %#v", video)
	}
	if _, invented := video["generation"]; invented {
		t.Fatalf("uploaded video content unexpectedly contains generation: %#v", video)
	}
	composite := companionContent(t, document, mapping["node:composite"])
	references := asSlice(t, asMap(t, composite["generation"])["references"])
	if reference := asMap(t, references[0]); reference["nodeAssetId"] != mapping["node:video"] {
		t.Fatalf("composite reference = %#v", reference)
	}
	imagePlaceholder := companionContent(t, document, mapping["node:image-placeholder"])
	if _, hasAssetID := imagePlaceholder["assetId"]; hasAssetID {
		t.Fatalf("placeholder persisted an asset ID: %#v", imagePlaceholder)
	}
}

func TestDecodeContractsAreStrictAndAllowDeduplicatedResolvedAssets(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	resolved.Media[1].AssetID = resolved.Media[0].AssetID
	resolved.Media[1].PippitAssetID = resolved.Media[0].PippitAssetID
	if _, err := NormalizeResolvedMedia(resolved); err != nil {
		t.Fatalf("NormalizeResolvedMedia() rejected deduplicated upload: %v", err)
	}

	planPayload, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	planPayload = bytes.Replace(planPayload, []byte(`"title":`), []byte(`"team_id":"forbidden","title":`), 1)
	if _, err := DecodePlan(bytes.NewReader(planPayload)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("DecodePlan() error = %v, want unknown team_id rejection", err)
	}

	resolvedPayload, err := json.Marshal(resolved)
	if err != nil {
		t.Fatal(err)
	}
	resolvedPayload = bytes.Replace(resolvedPayload, []byte(`"schema":`), []byte(`"team_id":"forbidden","schema":`), 1)
	if _, err := DecodeResolvedMedia(bytes.NewReader(resolvedPayload)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("DecodeResolvedMedia() error = %v, want unknown team_id rejection", err)
	}

	badSource := plan
	badSource.RequiredMedia = append([]MediaRequirement(nil), plan.RequiredMedia...)
	badSource.RequiredMedia[0].URL = "https://source.example/image.png"
	if _, err := NormalizePlan(badSource); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("NormalizePlan() error = %v, want URL/local_path XOR rejection", err)
	}
	badDigest := plan
	badDigest.RequiredMedia = append([]MediaRequirement(nil), plan.RequiredMedia...)
	badDigest.RequiredMedia[0].SHA256 = "sha256:bad"
	if _, err := NormalizePlan(badDigest); err == nil || !strings.Contains(err.Error(), "64 lowercase") {
		t.Fatalf("NormalizePlan() error = %v, want digest rejection", err)
	}
}

func TestHashRawJSONPreservesIntegerPrecision(t *testing.T) {
	left, err := hashRawJSON(json.RawMessage(`{"value":9007199254740992}`))
	if err != nil {
		t.Fatal(err)
	}
	right, err := hashRawJSON(json.RawMessage(`{"value":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatal("hashRawJSON() collapsed distinct large integers")
	}
}

func TestExternalPlanFixtureWhenConfigured(t *testing.T) {
	planPath := strings.TrimSpace(os.Getenv("PIPPIT_CANVASPLAN_FIXTURE"))
	if planPath == "" {
		t.Skip("set PIPPIT_CANVASPLAN_FIXTURE to run a local exported-plan smoke test")
	}
	file, err := os.Open(planPath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := DecodePlan(file)
	_ = file.Close()
	if err != nil {
		t.Fatalf("DecodePlan(%s) error = %v", planPath, err)
	}
	resolved := ResolvedMediaSet{Schema: ResolvedMediaSchema, Media: make([]ResolvedMedia, 0, len(plan.RequiredMedia))}
	for index, media := range plan.RequiredMedia {
		localPath := filepath.Join(filepath.Dir(planPath), filepath.FromSlash(media.LocalPath))
		payload, err := os.ReadFile(localPath)
		if err != nil {
			t.Fatalf("read media %q: %v", media.LogicalID, err)
		}
		if media.Metadata.ByteSize == nil || int64(len(payload)) != *media.Metadata.ByteSize {
			t.Fatalf("media %q byte size mismatch", media.LogicalID)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(payload)); got != media.SHA256 {
			t.Fatalf("media %q SHA-256 mismatch", media.LogicalID)
		}
		resolved.Media = append(resolved.Media, ResolvedMedia{
			LogicalID: media.LogicalID, MediaType: media.MediaType,
			AssetID: fmt.Sprintf("fixture-upload-%d", index+1), PippitAssetID: fmt.Sprintf("fixture-media-%d", index+1),
		})
	}
	mapping := make(map[string]string, len(plan.Nodes))
	for index, node := range plan.Nodes {
		mapping[node.LogicalID] = fmt.Sprintf("fixture-node-%d", index+1)
	}
	document, err := Materialize(plan, resolved, "fixture-root", mapping)
	if err != nil {
		t.Fatalf("Materialize(real plan) error = %v", err)
	}
	if got, want := len(document.Assets), len(plan.Nodes)+1; got != want {
		t.Fatalf("materialized asset count = %d, want %d", got, want)
	}
	t.Logf(
		"materialized title=%q media=%d nodes=%d groups=%d edges=%d degradations=%d assets=%d",
		plan.Title, len(plan.RequiredMedia), len(plan.Nodes), len(plan.Groups), len(plan.Edges), len(plan.Degradations), len(document.Assets),
	)
}

func TestExecutorAppliesOnceVerifiesAndReusesJournal(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	executor := &Executor{api: api}
	journalPath := filepath.Join(t.TempDir(), "canvas-plan.json")

	result, err := executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.State != StateVerified || result.Verification == nil || !result.Verification.Verified {
		t.Fatalf("Execute() result = %#v, want verified", result)
	}
	if api.applyCalls != 1 || api.createCalls != 1 || api.allocateCalls != 1 {
		t.Fatalf("remote calls create=%d allocate=%d apply=%d", api.createCalls, api.allocateCalls, api.applyCalls)
	}
	request := api.lastApply.Request
	if len(request.Transactions) != 1 || request.Transactions[0].Intent != "canvas.write" {
		t.Fatalf("apply request = %#v, want one canvas.write transaction", request)
	}
	if got, want := len(request.Transactions[0].Patches), len(plan.Nodes)+1; got != want {
		t.Fatalf("patch count = %d, want %d", got, want)
	}
	if request.Transactions[0].Patches[0].Op != "replace" || request.Transactions[0].Patches[0].BaseAssetVersion == nil || *request.Transactions[0].Patches[0].BaseAssetVersion != 7 {
		t.Fatalf("root patch = %#v", request.Transactions[0].Patches[0])
	}
	info, err := os.Stat(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("journal mode = %o, want 600", got)
	}

	callsBefore := api.totalCalls()
	getCallsBefore := api.getCalls
	resumed, err := executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if resumed.State != StateVerified || api.totalCalls() != callsBefore+1 || api.getCalls != getCallsBefore+1 {
		t.Fatalf("second Execute() result=%#v total calls=%d get calls=%d, want one current-auth query", resumed, api.totalCalls()-callsBefore, api.getCalls-getCallsBefore)
	}
	if api.applyCalls != 1 {
		t.Fatalf("verified resume replayed apply: calls=%d", api.applyCalls)
	}
}

func TestExecutorVerifiedResumeFailsOnCurrentAccountMismatch(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	executor := &Executor{api: api}
	journalPath := filepath.Join(t.TempDir(), "current-account-mismatch.json")

	result, err := executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err != nil || result.State != StateVerified {
		t.Fatalf("first Execute() result=%#v error=%v, want verified", result, err)
	}
	api.getErr = errors.New("assets are not visible to current account")
	result, err = executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err == nil || result == nil || result.State != StateVerificationFailed {
		t.Fatalf("verified resume result=%#v error=%v, want current-auth verification failure", result, err)
	}
	if result.Verification == nil || result.Verification.Verified {
		t.Fatalf("verification = %#v, want fresh failed verification", result.Verification)
	}
	if api.applyCalls != 1 {
		t.Fatalf("verified resume replayed apply: calls=%d", api.applyCalls)
	}
}

func TestExecutorVerifiedResumeFailsOnRemoteDrift(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	executor := &Executor{api: api}
	journalPath := filepath.Join(t.TempDir(), "remote-drift.json")

	result, err := executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err != nil || result.State != StateVerified {
		t.Fatalf("first Execute() result=%#v error=%v, want verified", result, err)
	}
	api.corruptGet = true
	result, err = executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err == nil || result == nil || result.State != StateVerificationFailed {
		t.Fatalf("verified resume result=%#v error=%v, want remote-drift failure", result, err)
	}
	if result.Verification == nil || result.Verification.Verified || len(result.Verification.MismatchedAssetIDs) != 1 {
		t.Fatalf("verification = %#v, want one mismatched asset", result.Verification)
	}
	if api.applyCalls != 1 {
		t.Fatalf("verified resume replayed apply: calls=%d", api.applyCalls)
	}
}

func TestExecutorNeverReplaysAmbiguousApply(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	api.applyErr = errors.New("connection lost after request")
	executor := &Executor{api: api}
	journalPath := filepath.Join(t.TempDir(), "ambiguous.json")

	result, err := executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err == nil || result == nil || result.State != StateApplyAmbiguous {
		t.Fatalf("Execute() result=%#v error=%v, want apply ambiguity", result, err)
	}
	if api.applyCalls != 1 {
		t.Fatalf("apply calls = %d, want 1", api.applyCalls)
	}
	result, err = executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err == nil || result == nil || !strings.Contains(err.Error(), "refusing to replay") {
		t.Fatalf("resume result=%#v error=%v, want replay refusal", result, err)
	}
	if api.applyCalls != 1 {
		t.Fatalf("apply was replayed: calls=%d", api.applyCalls)
	}
	journal := readJournal(t, journalPath)
	if journal.Apply == nil || journal.Apply.Status != "ambiguous" {
		t.Fatalf("journal apply = %#v", journal.Apply)
	}
}

func TestExecutorRecoversCommittedAmbiguousApplyByQuery(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	api.applyErr = errors.New("connection lost after commit")
	api.commitBeforeApplyError = true
	executor := &Executor{api: api}
	journalPath := filepath.Join(t.TempDir(), "committed-ambiguous.json")

	result, err := executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err == nil || result.State != StateApplyAmbiguous {
		t.Fatalf("first Execute() result=%#v error=%v, want ambiguity", result, err)
	}
	result, err = executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err != nil || result.State != StateVerified || result.Verification == nil || !result.Verification.RecoveredFromQuery {
		t.Fatalf("resume result=%#v error=%v, want query recovery", result, err)
	}
	if api.applyCalls != 1 {
		t.Fatalf("committed apply was replayed: calls=%d", api.applyCalls)
	}
}

func TestExecutorNeverReplaysAmbiguousCreate(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	api.createErr = errors.New("connection lost after create request")
	executor := &Executor{api: api}
	journalPath := filepath.Join(t.TempDir(), "ambiguous-create.json")

	result, err := executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err == nil || result == nil || result.State != StateCreateAmbiguous {
		t.Fatalf("Execute() result=%#v error=%v, want create ambiguity", result, err)
	}
	result, err = executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err == nil || result == nil || !strings.Contains(err.Error(), "terminal safety state") {
		t.Fatalf("resume result=%#v error=%v, want create replay refusal", result, err)
	}
	if api.createCalls != 1 {
		t.Fatalf("create was replayed: calls=%d", api.createCalls)
	}
}

func TestExecutorReportsQueryBackMismatchWithoutReplay(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	api.corruptGet = true
	executor := &Executor{api: api}
	journalPath := filepath.Join(t.TempDir(), "query-mismatch.json")

	result, err := executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err == nil || result == nil || result.State != StateVerificationFailed {
		t.Fatalf("Execute() result=%#v error=%v, want verification failure", result, err)
	}
	if result.Verification == nil || len(result.Verification.MismatchedAssetIDs) != 1 {
		t.Fatalf("verification = %#v", result.Verification)
	}
	if api.applyCalls != 1 {
		t.Fatalf("apply calls = %d, want 1", api.applyCalls)
	}
}

func TestExecutorResumesAcceptedCreateWithoutCreatingAgain(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	api.createPending = true
	executor := &Executor{api: api}
	journalPath := filepath.Join(t.TempDir(), "pending-create.json")

	result, err := executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err != nil || result.State != StateCreatePending || result.ProjectID == "" {
		t.Fatalf("first Execute() result=%#v error=%v", result, err)
	}
	if api.createCalls != 1 || api.resumeCreateCalls != 0 {
		t.Fatalf("first call create=%d resume=%d", api.createCalls, api.resumeCreateCalls)
	}
	result, err = executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath})
	if err != nil || result.State != StateVerified {
		t.Fatalf("second Execute() result=%#v error=%v", result, err)
	}
	if api.createCalls != 1 || api.resumeCreateCalls != 1 {
		t.Fatalf("create was duplicated: create=%d resume=%d", api.createCalls, api.resumeCreateCalls)
	}
}

func TestExecutorRejectsJournalInputDriftAndResumeWithoutJournal(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	api.createPending = true
	executor := &Executor{api: api}
	journalPath := filepath.Join(t.TempDir(), "drift.json")
	if _, err := executor.Resume(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Resume() error = %v, want missing journal rejection", err)
	}
	if _, err := executor.Execute(context.Background(), plan, resolved, ExecuteOptions{JournalPath: journalPath}); err != nil {
		t.Fatal(err)
	}
	changed := plan
	changed.Title = "Changed title"
	if _, err := executor.Execute(context.Background(), changed, resolved, ExecuteOptions{JournalPath: journalPath}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("Execute(changed plan) error = %v, want drift rejection", err)
	}
}

func testPlanAndResolved() (Plan, ResolvedMediaSet) {
	byteSizeImage, byteSizeVideo, byteSizeAudio := int64(1234), int64(5678), int64(234)
	durationVideo, durationAudio := int64(6000), int64(9000)
	imageWidth, imageHeight := int64(1600), int64(900)
	videoWidth, videoHeight := int64(1280), int64(720)
	plan := Plan{
		Schema: PlanSchema,
		Title:  "Provider-neutral import",
		Source: Source{Provider: "fixture", ProjectID: "source-project", Fingerprint: "fingerprint-1"},
		RequiredMedia: []MediaRequirement{
			{
				LogicalID: "media:image", SourceNodeID: "source-image", FileName: "image.png", MediaType: "image",
				LocalPath: "bundle/media/image.png", SHA256: strings.Repeat("a", 64),
				Metadata: MediaMetadata{ByteSize: &byteSizeImage, Extension: "png", Height: &imageHeight, MimeType: "image/png", Width: &imageWidth},
			},
			{
				LogicalID: "media:video", SourceNodeID: "source-video", FileName: "video.mp4", MediaType: "video",
				LocalPath: "bundle/media/video.mp4", SHA256: strings.Repeat("b", 64),
				Metadata: MediaMetadata{ByteSize: &byteSizeVideo, DurationMS: &durationVideo, Extension: "mp4", Height: &videoHeight, MimeType: "video/mp4", Width: &videoWidth},
			},
			{
				LogicalID: "media:audio", SourceNodeID: "source-audio", FileName: "audio.mp3", MediaType: "audio",
				URL:      "https://source.example/audio.mp3",
				Metadata: MediaMetadata{ByteSize: &byteSizeAudio, DurationMS: &durationAudio, Extension: "mp3", MimeType: "audio/mpeg"},
			},
		},
		Nodes: []Node{
			{LogicalID: "node:image", SourceNodeID: "source-image", Title: "Image", Position: Position{X: 10, Y: 20}, Size: Size{Width: 320, Height: 180}, ParentGroupLogicalID: "group:scene", Order: 0, Kind: "image", TargetType: "biz/image", MediaLogicalID: "media:image"},
			{LogicalID: "node:video", SourceNodeID: "source-video", Title: "Video", Position: Position{X: 350, Y: 20}, Size: Size{Width: 320, Height: 180}, ParentGroupLogicalID: "group:scene", Order: 1, Kind: "video", TargetType: "biz/video", MediaLogicalID: "media:video"},
			{LogicalID: "node:audio", SourceNodeID: "source-audio", Title: "Audio", Position: Position{X: 10, Y: 220}, Size: Size{Width: 320, Height: 100}, ParentGroupLogicalID: "group:scene", Order: 2, Kind: "audio", TargetType: "biz/audio", MediaLogicalID: "media:audio"},
			{LogicalID: "node:composite", SourceNodeID: "source-composite", Title: "Composite", Position: Position{X: 350, Y: 220}, Size: Size{Width: 320, Height: 180}, ParentGroupLogicalID: "group:scene", Order: 3, Kind: "video-composite", TargetType: "biz/video", Variant: "video-composite", InputNodeLogicalIDs: []string{"node:video"}},
			{LogicalID: "node:image-placeholder", SourceNodeID: "source-image-placeholder", Title: "Pending image", Position: Position{X: 10, Y: 420}, Size: Size{Width: 320, Height: 180}, ParentGroupLogicalID: "group:scene", Order: 4, Kind: "image-placeholder", TargetType: "biz/image"},
			{LogicalID: "node:video-placeholder", SourceNodeID: "source-video-placeholder", Title: "Pending video", Position: Position{X: 350, Y: 420}, Size: Size{Width: 320, Height: 180}, ParentGroupLogicalID: "group:scene", Order: 5, Kind: "video-placeholder", TargetType: "biz/video"},
		},
		Groups: []Group{{
			LogicalID: "group:scene", SourceNodeID: "source-group", Title: "Scene", Position: Position{X: 0, Y: 0}, Size: Size{Width: 700, Height: 640}, Order: 0,
			ChildLogicalIDs: []string{"node:image", "node:video", "node:audio", "node:composite", "node:image-placeholder", "node:video-placeholder"},
		}},
		Edges:        []Edge{{LogicalID: "edge:reference", SourceEdgeID: "source-edge", Type: "reference", SourceNodeLogicalID: "node:video", TargetNodeLogicalID: "node:composite", SourceHandle: "right", TargetHandle: "left"}},
		Degradations: []json.RawMessage{json.RawMessage(`{"reason":"source media is not ready","node_logical_ids":["node:image-placeholder","node:video-placeholder"]}`)},
	}
	resolved := ResolvedMediaSet{
		Schema: ResolvedMediaSchema,
		Media: []ResolvedMedia{
			{LogicalID: "media:image", MediaType: "image", AssetID: "upload-image", PippitAssetID: "media-image"},
			{LogicalID: "media:video", MediaType: "video", AssetID: "upload-video", PippitAssetID: "media-video"},
			{LogicalID: "media:audio", MediaType: "audio", AssetID: "upload-audio", PippitAssetID: "media-audio"},
		},
	}
	return plan, resolved
}

type fakeCanvasAPI struct {
	nodeCount              int
	createCalls            int
	resumeCreateCalls      int
	allocateCalls          int
	getCalls               int
	getExistingCalls       int
	applyCalls             int
	createPending          bool
	createErr              error
	getErr                 error
	applyErr               error
	commitBeforeApplyError bool
	corruptGet             bool
	lastApply              canvas.ApplyOptions
	stored                 map[string]json.RawMessage
}

func newFakeCanvasAPI(nodeCount int) *fakeCanvasAPI {
	return &fakeCanvasAPI{nodeCount: nodeCount, stored: make(map[string]json.RawMessage)}
}

func (api *fakeCanvasAPI) Create(context.Context, canvas.CreateOptions) (*canvas.CreateResult, error) {
	api.createCalls++
	if api.createErr != nil {
		return nil, api.createErr
	}
	state := canvas.StateReady
	warning := ""
	if api.createPending {
		state = canvas.StateCreating
		warning = "still creating"
	}
	return &canvas.CreateResult{
		RequestID: "request-1", State: state, ProjectID: "123", ThreadID: "thread-1", RunID: "run-1",
		CanvasAssetID: "root-asset", OverviewPippitAssetID: "overview-asset", WebURL: "/novel/detail/canvas?projectId=123", Warning: warning,
	}, nil
}

func (api *fakeCanvasAPI) ResumeCreate(context.Context, *canvas.CreateResult, canvas.ResumeCreateOptions) (*canvas.CreateResult, error) {
	api.resumeCreateCalls++
	api.createPending = false
	return &canvas.CreateResult{
		RequestID: "request-1", State: canvas.StateReady, ProjectID: "123", ThreadID: "thread-1", RunID: "run-1",
		CanvasAssetID: "root-asset", OverviewPippitAssetID: "overview-asset", WebURL: "/novel/detail/canvas?projectId=123",
	}, nil
}

func (api *fakeCanvasAPI) Allocate(_ context.Context, count int) (*canvas.AllocateResult, error) {
	api.allocateCalls++
	if count != api.nodeCount {
		return nil, fmt.Errorf("count=%d, want %d", count, api.nodeCount)
	}
	ids := make([]string, count)
	for index := range ids {
		ids[index] = fmt.Sprintf("node-asset-%d", index+1)
	}
	return &canvas.AllocateResult{AssetIDs: ids, LogID: "allocate-log"}, nil
}

func (api *fakeCanvasAPI) Get(_ context.Context, assetIDs []string) (*canvas.GetResult, error) {
	api.getCalls++
	if api.getErr != nil {
		return nil, api.getErr
	}
	assets := make([]json.RawMessage, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		content, ok := api.stored[assetID]
		if !ok {
			return nil, fmt.Errorf("missing stored asset %s", assetID)
		}
		if api.corruptGet && assetID == "root-asset" {
			content = json.RawMessage(`{"pippitAssetId":"root-asset","type":"canvas","content":{"corrupted":true},"extra":{}}`)
		}
		assets = append(assets, queriedAsset(testedAsset{ID: assetID, Version: 8, Content: content}))
	}
	return &canvas.GetResult{RequestedAssetIDs: assetIDs, Assets: assets, LogID: "query-after-log"}, nil
}

func (api *fakeCanvasAPI) GetExisting(_ context.Context, assetIDs []string) (*canvas.GetResult, error) {
	api.getExistingCalls++
	assets := make([]json.RawMessage, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		if content, ok := api.stored[assetID]; ok {
			assets = append(assets, queriedAsset(testedAsset{ID: assetID, Version: 8, Content: content}))
			continue
		}
		if assetID == "root-asset" {
			assets = append(assets, queriedAsset(testedAsset{ID: assetID, Version: 7, Content: json.RawMessage(`{"pippitAssetId":"root-asset","type":"canvas","content":{},"extra":{}}`)}))
		}
	}
	return &canvas.GetResult{RequestedAssetIDs: assetIDs, Assets: assets, LogID: "preflight-log"}, nil
}

func (api *fakeCanvasAPI) Apply(_ context.Context, opts canvas.ApplyOptions) (*canvas.ApplyResult, error) {
	api.applyCalls++
	api.lastApply = opts
	if api.applyErr != nil && !api.commitBeforeApplyError {
		return nil, api.applyErr
	}
	versions := make(map[string]int64)
	for _, patch := range opts.Request.Transactions[0].Patches {
		api.stored[patch.AssetID] = append(json.RawMessage(nil), patch.Value...)
		versions[patch.AssetID] = 8
	}
	if api.applyErr != nil {
		return nil, api.applyErr
	}
	return &canvas.ApplyResult{
		BatchID: opts.Request.BatchID,
		Results: []canvas.PatchTransactionResult{{TransactionID: opts.Request.Transactions[0].TransactionID, Status: "ack", AssetVersions: versions}},
		LogID:   "apply-log",
	}, nil
}

func (api *fakeCanvasAPI) totalCalls() int {
	return api.createCalls + api.resumeCreateCalls + api.allocateCalls + api.getCalls + api.getExistingCalls + api.applyCalls
}

type testedAsset struct {
	ID      string
	Version int64
	Content json.RawMessage
}

func queriedAsset(asset testedAsset) json.RawMessage {
	content, _ := json.Marshal(string(asset.Content))
	return json.RawMessage(fmt.Sprintf(
		`{"PippitAssetID":%q,"Version":%d,"TextInfo":{"Content":%s}}`,
		asset.ID,
		asset.Version,
		content,
	))
}

func readJournal(t *testing.T, path string) *Journal {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	journal, err := decodeJournal(file)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func decodeRawMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value %#v is not an object", value)
	}
	return result
}

func asSlice(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("value %#v is not an array", value)
	}
	return result
}

func companionContent(t *testing.T, document *Document, assetID string) map[string]any {
	t.Helper()
	asset := decodeRawMap(t, document.Assets[assetID])
	return asMap(t, asset["content"])
}
