package canvas

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

type generatedImportMediaReader struct {
	remaining      int64
	maxReadRequest int
}

type verifiedReaderUploadClient struct {
	path       string
	wantBytes  []byte
	uploadRead bool
}

func (client *verifiedReaderUploadClient) SendRequest(_ context.Context, _ string, _ any, out any) error {
	return json.Unmarshal([]byte(`{"ret":"0","data":{"Assets":[{"PippitAssetID":"pippit-1"}]}}`), out)
}

func (client *verifiedReaderUploadClient) SendRequestWithHeaders(
	ctx context.Context,
	path string,
	body any,
	_ map[string]string,
	out any,
) error {
	return client.SendRequest(ctx, path, body, out)
}

func (client *verifiedReaderUploadClient) SendMultipartRequest(
	_ context.Context,
	_ string,
	_ map[string]string,
	file common.MultipartFile,
	out any,
) error {
	if file.Reader == nil || file.Path != "" {
		return fmt.Errorf("multipart must use the caller-verified reader without a fallback path")
	}
	if err := os.Rename(client.path, client.path+".replaced"); err != nil {
		return err
	}
	if err := os.WriteFile(client.path, []byte("attacker replacement"), 0o600); err != nil {
		return err
	}
	payload, err := io.ReadAll(file.Reader)
	if err != nil {
		return err
	}
	if !bytes.Equal(payload, client.wantBytes) {
		return fmt.Errorf("uploaded payload %q does not match verified inode", payload)
	}
	client.uploadRead = true
	return json.Unmarshal([]byte(`{"ret":"0","data":{"asset_id":"asset-1","pippit_asset_id":"pippit-1"}}`), out)
}

func (reader *generatedImportMediaReader) Read(payload []byte) (int, error) {
	if len(payload) > reader.maxReadRequest {
		reader.maxReadRequest = len(payload)
	}
	if reader.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(payload)) > reader.remaining {
		payload = payload[:reader.remaining]
	}
	for index := range payload {
		payload[index] = 0x5a
	}
	reader.remaining -= int64(len(payload))
	return len(payload), nil
}

func TestInspectImportMediaContentStreamsNonPNG(t *testing.T) {
	const byteSize = int64(8 << 20)
	reader := &generatedImportMediaReader{remaining: byteSize}
	identity, err := inspectImportMediaContent(reader)
	if err != nil {
		t.Fatalf("inspectImportMediaContent() error = %v", err)
	}
	if identity.ByteSize != byteSize || !strings.HasPrefix(identity.ContentFingerprint, rawMediaFingerprintPrefix) ||
		identity.ContentFingerprint != rawMediaFingerprintPrefix+identity.RawSHA256 {
		t.Fatalf("identity = %#v, want streamed raw identity", identity)
	}
	if reader.maxReadRequest > 64<<10 {
		t.Fatalf("largest Read buffer = %d, want bounded streaming instead of whole-file allocation", reader.maxReadRequest)
	}
}

func TestLibTVPNGAIGCFingerprintFallsBackForUnrecognizedSchema(t *testing.T) {
	known := testLibTVAIGCMetadata("libtv" + strings.Repeat("a", 32))
	base := testPNGWithITXt(t, testFingerprintPixel, known)
	secondAIGC := append([]byte("AIGC\x00\x00\x00\x00\x00"), []byte(known)...)
	oversizedAIGC := append(
		[]byte("AIGC\x00\x00\x00\x00\x00"),
		bytes.Repeat([]byte{'x'}, maxCanonicalITXtChunkBytes+1)...,
	)
	cases := map[string][]byte{
		"missing ID": testPNGWithITXt(t, testFingerprintPixel, `{"ProduceID":"one"}`),
		"non-string ID": testPNGWithITXt(
			t, testFingerprintPixel, `{"ProduceID":1,"PropagateID":"two"}`,
		),
		"unknown extra field": testPNGWithITXt(
			t, testFingerprintPixel, strings.TrimSuffix(known, "}")+`,"Prompt":"future"}`,
		),
		"future compressed schema": rewriteTestITXtHeader(t, base, func(header []byte) {
			header[len("AIGC")+1] = 1
		}),
		"future language schema": rewriteTestITXtHeader(t, base, func(header []byte) {
			header[len("AIGC")+3] = 'e'
		}),
		"multiple AIGC chunks": testInsertPNGChunkBeforeIEND(t, base, "iTXt", secondAIGC),
		"oversized second AIGC chunk": testInsertPNGChunkBeforeIEND(
			t, base, "iTXt", oversizedAIGC,
		),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			fingerprint, err := canonicalLibTVPNGFingerprint(payload)
			if err != nil {
				t.Fatalf("canonicalLibTVPNGFingerprint() error = %v", err)
			}
			digest := sha256.Sum256(payload)
			want := rawMediaFingerprintPrefix + hex.EncodeToString(digest[:])
			if fingerprint != want {
				t.Fatalf("fingerprint = %q, want raw fallback %q", fingerprint, want)
			}
		})
	}
}

func TestInspectImportMediaFileRejectsFinalSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.bin")
	link := filepath.Join(directory, "link.bin")
	if err := os.WriteFile(target, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	if _, err := inspectImportMediaFile(link); err == nil || !strings.Contains(err.Error(), "non-symbolic") {
		t.Fatalf("inspectImportMediaFile() error = %v, want no-follow rejection", err)
	}
}

func TestRunnerImportMediaUploadStreamsVerifiedInode(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "clip.mp4")
	original := []byte("verified original media")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := inspectImportMediaFile(path)
	if err != nil {
		t.Fatal(err)
	}
	client := &verifiedReaderUploadClient{path: path, wantBytes: original}
	api := runnerImportMediaAPI{runner: &common.Runner{Client: client}}
	result, err := api.Upload(context.Background(), validatedImportMedia{
		LogicalID:          "media:video-1",
		MediaType:          "video",
		FileName:           "clip.mp4",
		LocalPath:          path,
		SHA256:             identity.RawSHA256,
		ContentFingerprint: identity.ContentFingerprint,
		ByteSize:           identity.ByteSize,
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if !client.uploadRead || result.PippitAssetID != "pippit-1" {
		t.Fatalf("uploadRead/result = %v/%#v, want verified descriptor upload", client.uploadRead, result)
	}
}

func TestLegacyPNGCheckpointBindsSourceNodeRequirement(t *testing.T) {
	oldPNG := testPNGWithITXt(t, testFingerprintPixel, testLibTVAIGCMetadata("libtv"+strings.Repeat("a", 32)))
	currentPNG := testPNGWithITXt(t, testFingerprintPixel, testLibTVAIGCMetadata("libtv"+strings.Repeat("b", 32)))
	opts, _ := testPNGCheckpointMigrationOptions(t, oldPNG, currentPNG)
	checkpoint := readTestMediaCheckpoint(t, opts.CheckpointPath)
	planPath := filepath.Join(checkpoint.BundleDirs[0], "plan.json")
	plan, err := readCanvasPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	plan.RequiredMedia[0].SourceNodeID = "different-source-node"
	if err := writeTestJSON(planPath, plan); err != nil {
		t.Fatal(err)
	}
	api := &fakeImportMediaAPI{}
	_, err = resolveImportMedia(context.Background(), opts, api, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "source node and media contract") {
		t.Fatalf("resolveImportMedia() error = %v, want source requirement rejection", err)
	}
	if api.uploads != 0 || api.queries != 0 {
		t.Fatalf("uploads/queries = %d/%d, want no remote calls", api.uploads, api.queries)
	}
}

func TestLegacyPNGCheckpointRejectsMultipleMatchingBundles(t *testing.T) {
	oldPNG := testPNGWithITXt(t, testFingerprintPixel, testLibTVAIGCMetadata("libtv"+strings.Repeat("a", 32)))
	currentPNG := testPNGWithITXt(t, testFingerprintPixel, testLibTVAIGCMetadata("libtv"+strings.Repeat("b", 32)))
	opts, _ := testPNGCheckpointMigrationOptions(t, oldPNG, currentPNG)
	checkpoint := readTestMediaCheckpoint(t, opts.CheckpointPath)
	oldPlan, err := readCanvasPlan(filepath.Join(checkpoint.BundleDirs[0], "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	duplicateBundle := filepath.Join(opts.BundleRoot, "export-old-duplicate")
	exporter := &fakeImportExporter{
		plan: oldPlan,
		mediaBytes: map[string][]byte{
			oldPlan.RequiredMedia[0].LocalPath: oldPNG,
		},
	}
	if _, err := exporter.Export(context.Background(), testLibTVURL, duplicateBundle, io.Discard); err != nil {
		t.Fatal(err)
	}
	checkpoint.BundleDirs = append(checkpoint.BundleDirs, duplicateBundle)
	if err := saveMediaCheckpoint(opts.CheckpointPath, &checkpoint); err != nil {
		t.Fatal(err)
	}
	api := &fakeImportMediaAPI{}
	_, err = resolveImportMedia(context.Background(), opts, api, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "ambiguously match") {
		t.Fatalf("resolveImportMedia() error = %v, want ambiguous legacy bundle rejection", err)
	}
	if api.uploads != 0 || api.queries != 0 {
		t.Fatalf("uploads/queries = %d/%d, want no remote calls", api.uploads, api.queries)
	}
}

var testFingerprintPixel = color.NRGBA{R: 20, G: 40, B: 60, A: 255}

func rewriteTestITXtHeader(t *testing.T, payload []byte, rewrite func([]byte)) []byte {
	t.Helper()
	return testRewriteFirstPNGChunk(t, payload, "iTXt", rewrite)
}
