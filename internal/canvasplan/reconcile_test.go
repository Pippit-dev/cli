package canvasplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/canvas"
)

type reconcileTestAPI struct {
	*fakeCanvasAPI
	assets    []json.RawMessage
	requested []string
}

func (api *reconcileTestAPI) Get(_ context.Context, assetIDs []string) (*canvas.GetResult, error) {
	api.getCalls++
	api.requested = append([]string(nil), assetIDs...)
	if api.getErr != nil {
		return nil, api.getErr
	}
	return &canvas.GetResult{RequestedAssetIDs: assetIDs, Assets: api.assets, LogID: "reconcile-log"}, nil
}

func TestReconcileVerifiesJournalWithoutApplying(t *testing.T) {
	contents := map[string]json.RawMessage{
		"asset-b": json.RawMessage(`{"value":2}`),
		"asset-a": json.RawMessage(`{"value":1}`),
	}
	journalPath := writeReconcileTestJournal(t, StateApplyAmbiguous, "ambiguous", contents)
	api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
	api.assets = []json.RawMessage{
		queriedAsset(testedAsset{ID: "asset-b", Version: 2, Content: contents["asset-b"]}),
		queriedAsset(testedAsset{ID: "asset-a", Version: 2, Content: contents["asset-a"]}),
	}

	result, err := (&Executor{api: api}).Reconcile(context.Background(), journalPath)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.State != StateVerified || result.Verification == nil || !result.Verification.Verified || !result.Verification.RecoveredFromQuery {
		t.Fatalf("Reconcile() result = %#v, want verified query recovery", result)
	}
	if !reflect.DeepEqual(api.requested, []string{"asset-a", "asset-b"}) {
		t.Fatalf("Get() asset IDs = %#v, want sorted journal keys", api.requested)
	}
	if api.applyCalls != 0 {
		t.Fatalf("Reconcile() replayed Apply %d times", api.applyCalls)
	}
	if journal := readJournal(t, journalPath); journal.State != StateVerified || journal.Apply.Status != "verified" {
		t.Fatalf("saved journal = %#v, want verified", journal)
	}
}

func TestReconcilePartialOrMismatchFailsClosed(t *testing.T) {
	contents := map[string]json.RawMessage{
		"asset-a": json.RawMessage(`{"value":1}`),
		"asset-b": json.RawMessage(`{"value":2}`),
	}
	journalPath := writeReconcileTestJournal(t, StateApplyAmbiguous, "ambiguous", contents)
	beforeBytes, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
	api.assets = []json.RawMessage{
		queriedAsset(testedAsset{ID: "asset-a", Version: 2, Content: json.RawMessage(`{"value":99}`)}),
	}

	result, err := (&Executor{api: api}).Reconcile(context.Background(), journalPath)
	if err == nil || result == nil || result.State == StateVerified {
		t.Fatalf("Reconcile() result=%#v error=%v, want nonverified failure", result, err)
	}
	if result.Verification == nil || len(result.Verification.MissingAssetIDs) != 1 || len(result.Verification.MismatchedAssetIDs) != 1 {
		t.Fatalf("verification = %#v, want one missing and one mismatch", result.Verification)
	}
	if api.applyCalls != 0 {
		t.Fatalf("Reconcile() replayed Apply %d times", api.applyCalls)
	}
	afterBytes, readErr := os.ReadFile(journalPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(afterBytes, beforeBytes) {
		t.Fatal("ambiguous journal bytes changed after non-exact query result")
	}
}

func TestReconcileRejectsInvalidStateWithoutQuery(t *testing.T) {
	journalPath := writeReconcileTestJournal(
		t,
		StateApplyAcknowledged,
		"acknowledged",
		map[string]json.RawMessage{"asset-a": json.RawMessage(`{"value":1}`)},
	)
	api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
	result, err := (&Executor{api: api}).Reconcile(context.Background(), journalPath)
	if err == nil || result == nil || !strings.Contains(err.Error(), "not eligible") {
		t.Fatalf("Reconcile() result=%#v error=%v, want state rejection", result, err)
	}
	if api.getCalls != 0 || api.applyCalls != 0 {
		t.Fatalf("invalid journal made remote calls: get=%d apply=%d", api.getCalls, api.applyCalls)
	}
}

func TestReconcilePreviouslyVerifiedCanvasAllowsRemoteEditsWithoutOverwritingHistory(t *testing.T) {
	contents := map[string]json.RawMessage{"asset-a": json.RawMessage(`{"value":1}`)}
	journalPath := writeReconcileTestJournal(t, StateVerified, "verified", contents)
	before := readJournal(t, journalPath)
	api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
	api.assets = []json.RawMessage{
		queriedAsset(testedAsset{ID: "asset-a", Version: 3, Content: json.RawMessage(`{"value":2}`)}),
	}

	result, err := (&Executor{api: api}).Reconcile(context.Background(), journalPath)
	if err != nil || result == nil || result.State != StateVerified || !strings.Contains(result.Warning, "changed after") {
		t.Fatalf("Reconcile() result=%#v error=%v, want verified history with edit warning", result, err)
	}
	after := readJournal(t, journalPath)
	if !reflect.DeepEqual(after.Verification, before.Verification) || after.State != StateVerified {
		t.Fatalf("saved journal changed historical verification: before=%#v after=%#v", before, after)
	}
	if api.applyCalls != 0 {
		t.Fatalf("Reconcile() replayed Apply %d times", api.applyCalls)
	}
}

func TestReconcilePreviouslyVerifiedCanvasAccessFailureDoesNotOverwriteHistory(t *testing.T) {
	contents := map[string]json.RawMessage{"asset-a": json.RawMessage(`{"value":1}`)}
	journalPath := writeReconcileTestJournal(t, StateVerified, "verified", contents)
	before := readJournal(t, journalPath)
	beforeBytes, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
	api.getErr = fmt.Errorf("not visible to current account")

	result, err := (&Executor{api: api}).Reconcile(context.Background(), journalPath)
	if err == nil || result == nil || result.State != StateVerified || !strings.Contains(result.Warning, "current authentication") {
		t.Fatalf("Reconcile() result=%#v error=%v, want access failure with verified history", result, err)
	}
	after := readJournal(t, journalPath)
	if !reflect.DeepEqual(after.Verification, before.Verification) || after.State != StateVerified {
		t.Fatalf("saved journal changed after access failure: before=%#v after=%#v", before, after)
	}
	afterBytes, readErr := os.ReadFile(journalPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(afterBytes, beforeBytes) {
		t.Fatal("journal bytes changed after current-auth query failure")
	}
}

func TestReconcileRejectsVerifiedJournalWithoutHistoricalVerification(t *testing.T) {
	journalPath := writeReconcileTestJournal(
		t,
		StateVerified,
		"verified",
		map[string]json.RawMessage{"asset-a": json.RawMessage(`{"value":1}`)},
	)
	journal := readJournal(t, journalPath)
	journal.Verification = &Verification{ExpectedAssetCount: 1, ReturnedAssetCount: 1}
	if err := saveJournal(journalPath, journal); err != nil {
		t.Fatal(err)
	}
	api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
	result, err := (&Executor{api: api}).Reconcile(context.Background(), journalPath)
	if err == nil || result == nil || !strings.Contains(err.Error(), "historical verification") {
		t.Fatalf("Reconcile() result=%#v error=%v, want corrupt verified journal rejection", result, err)
	}
	if api.getCalls != 0 || api.applyCalls != 0 {
		t.Fatalf("corrupt verified journal made remote calls: get=%d apply=%d", api.getCalls, api.applyCalls)
	}
}

func TestReconcileRejectsInvalidInputHashesBeforeQuery(t *testing.T) {
	for _, field := range []string{"plan", "resolved"} {
		t.Run(field, func(t *testing.T) {
			journalPath := writeReconcileTestJournal(
				t,
				StateVerified,
				"verified",
				map[string]json.RawMessage{"asset-a": json.RawMessage(`{"value":1}`)},
			)
			journal := readJournal(t, journalPath)
			if field == "plan" {
				journal.PlanSHA256 = "bad"
			} else {
				journal.ResolvedMediaSHA256 = "bad"
			}
			if err := saveJournal(journalPath, journal); err != nil {
				t.Fatal(err)
			}
			api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
			result, err := (&Executor{api: api}).Reconcile(context.Background(), journalPath)
			if err == nil || result == nil || !strings.Contains(err.Error(), "invalid plan or resolved-media") {
				t.Fatalf("Reconcile() result=%#v error=%v, want invalid hash rejection", result, err)
			}
			if api.getCalls != 0 {
				t.Fatalf("invalid hash made %d query call(s)", api.getCalls)
			}
		})
	}
}

func TestReconcileRecoversLegacyDemotionAndPreservesStrictHistoryWhenPresent(t *testing.T) {
	contents := map[string]json.RawMessage{"asset-a": json.RawMessage(`{"value":1}`)}
	for _, historicalVerification := range []bool{false, true} {
		t.Run(fmt.Sprintf("historical-verification-%t", historicalVerification), func(t *testing.T) {
			journalPath := writeReconcileTestJournal(t, StateVerificationFailed, "verified", contents)
			journal := readJournal(t, journalPath)
			journal.LastError = "a later current-account query failed"
			journal.Verification = &Verification{
				ExpectedAssetCount: 1,
				ReturnedAssetCount: 1,
				MismatchedAssetIDs: []string{"asset-a"},
				Verified:           historicalVerification,
				RecoveredFromQuery: true,
				LogID:              "old-query-log",
			}
			if historicalVerification {
				journal.Verification.MismatchedAssetIDs = nil
			}
			if err := saveJournal(journalPath, journal); err != nil {
				t.Fatal(err)
			}
			before := readJournal(t, journalPath)
			api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
			api.assets = []json.RawMessage{
				queriedAsset(testedAsset{ID: "asset-a", Version: 3, Content: json.RawMessage(`{"value":2}`)}),
			}

			result, err := (&Executor{api: api}).Reconcile(context.Background(), journalPath)
			if err != nil || result == nil || result.State != StateVerified ||
				result.Verification == nil || !result.Verification.Verified ||
				!strings.Contains(result.Warning, "changed after") {
				t.Fatalf("Reconcile() result=%#v error=%v, want accessible legacy recovery", result, err)
			}
			after := readJournal(t, journalPath)
			if after.State != StateVerified || after.LastError != "" || !after.Verification.Verified {
				t.Fatalf("saved journal = %#v, want repaired verified state", after)
			}
			if historicalVerification && !reflect.DeepEqual(after.Verification, before.Verification) {
				t.Fatalf("historical strict verification was overwritten: before=%#v after=%#v", before.Verification, after.Verification)
			}
			if !historicalVerification && (!after.Verification.RecoveredFromQuery || after.Verification.LogID != "reconcile-log" || len(after.Verification.MismatchedAssetIDs) != 0) {
				t.Fatalf("rebuilt completion marker = %#v, want auditable legacy recovery", after.Verification)
			}
			if api.applyCalls != 0 {
				t.Fatalf("legacy recovery replayed Apply %d times", api.applyCalls)
			}
		})
	}
}

func TestReconcileLegacyDemotionAccessFailureDoesNotRepairOrPersist(t *testing.T) {
	journalPath := writeReconcileTestJournal(
		t,
		StateVerificationFailed,
		"verified",
		map[string]json.RawMessage{"asset-a": json.RawMessage(`{"value":1}`)},
	)
	journal := readJournal(t, journalPath)
	journal.Verification = &Verification{
		ExpectedAssetCount: 1,
		MissingAssetIDs:    []string{"asset-a"},
		Verified:           false,
		RecoveredFromQuery: true,
	}
	if err := saveJournal(journalPath, journal); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
	api.getErr = errors.New("access key cannot see the imported assets")

	result, err := (&Executor{api: api}).Reconcile(context.Background(), journalPath)
	if err == nil || result == nil || result.State != StateVerificationFailed {
		t.Fatalf("Reconcile() result=%#v error=%v, want legacy access failure", result, err)
	}
	after, readErr := os.ReadFile(journalPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("legacy journal changed despite current-account access failure")
	}
}

func TestReconcileNormalVerificationFailureFallsBackWithoutQuery(t *testing.T) {
	journalPath := writeReconcileTestJournal(
		t,
		StateVerificationFailed,
		"verification-failed",
		map[string]json.RawMessage{"asset-a": json.RawMessage(`{"value":1}`)},
	)
	api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
	result, err := (&Executor{api: api}).Reconcile(context.Background(), journalPath)
	if !errors.Is(err, ErrReconcileNotEligible) || result == nil {
		t.Fatalf("Reconcile() result=%#v error=%v, want normal Execute fallback signal", result, err)
	}
	if api.getCalls != 0 || api.applyCalls != 0 {
		t.Fatalf("noneligible journal made remote calls: get=%d apply=%d", api.getCalls, api.applyCalls)
	}
}

func TestReconcileWithInputsRejectsDifferentPlanOrResolvedMediaBeforeQuery(t *testing.T) {
	for _, change := range []string{"source", "resolved"} {
		t.Run(change, func(t *testing.T) {
			plan, resolved := testPlanAndResolved()
			journalPath := writeReconcileTestJournal(
				t,
				StateVerified,
				"verified",
				map[string]json.RawMessage{"asset-a": json.RawMessage(`{"value":1}`)},
			)
			bindReconcileTestJournal(t, journalPath, plan, resolved)
			if change == "source" {
				plan.Source.ProjectID = "different-source-project"
			} else {
				resolved.Media[0].AssetID = "different-upload"
			}
			api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
			result, err := (&Executor{api: api}).ReconcileWithInputs(context.Background(), journalPath, plan, resolved)
			if err == nil || result == nil || !strings.Contains(err.Error(), "changed after journal creation") {
				t.Fatalf("ReconcileWithInputs() result=%#v error=%v, want input binding rejection", result, err)
			}
			if api.getCalls != 0 || api.applyCalls != 0 {
				t.Fatalf("input mismatch made remote calls: get=%d apply=%d", api.getCalls, api.applyCalls)
			}
		})
	}
}

func TestReconcileWithInputsReturnsPlanCounts(t *testing.T) {
	plan, resolved := testPlanAndResolved()
	contents := map[string]json.RawMessage{"asset-a": json.RawMessage(`{"value":1}`)}
	journalPath := writeReconcileTestJournal(t, StateApplyAmbiguous, "ambiguous", contents)
	bindReconcileTestJournal(t, journalPath, plan, resolved)
	api := &reconcileTestAPI{fakeCanvasAPI: newFakeCanvasAPI(0)}
	api.assets = []json.RawMessage{
		queriedAsset(testedAsset{ID: "asset-a", Version: 2, Content: contents["asset-a"]}),
	}

	result, err := (&Executor{api: api}).ReconcileWithInputs(context.Background(), journalPath, plan, resolved)
	if err != nil {
		t.Fatalf("ReconcileWithInputs() error = %v", err)
	}
	if result.State != StateVerified || result.NodeCount != len(plan.Nodes)+len(plan.Groups) ||
		result.EdgeCount != len(plan.Edges) || result.DegradationCount != len(plan.Degradations) {
		t.Fatalf("ReconcileWithInputs() result = %#v, want plan-derived counts", result)
	}
}

func bindReconcileTestJournal(t *testing.T, journalPath string, plan Plan, resolved ResolvedMediaSet) {
	t.Helper()
	normalizedPlan, err := NormalizePlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	normalizedResolved, err := NormalizeResolvedMedia(resolved)
	if err != nil {
		t.Fatal(err)
	}
	journal := readJournal(t, journalPath)
	journal.PlanSHA256, err = hashJSON(normalizedPlan)
	if err != nil {
		t.Fatal(err)
	}
	journal.ResolvedMediaSHA256, err = hashJSON(normalizedResolved)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveJournal(journalPath, journal); err != nil {
		t.Fatal(err)
	}
}

func writeReconcileTestJournal(t *testing.T, state, applyStatus string, contents map[string]json.RawMessage) string {
	t.Helper()
	hashes := make(map[string]string, len(contents))
	for assetID, content := range contents {
		digest, err := hashRawJSON(content)
		if err != nil {
			t.Fatal(err)
		}
		hashes[assetID] = digest
	}
	path := filepath.Join(t.TempDir(), "reconcile.json")
	journal := &Journal{
		Schema:              JournalSchema,
		OperationID:         "operation-reconcile",
		RequestID:           "request-reconcile",
		PlanSHA256:          strings.Repeat("a", 64),
		ResolvedMediaSHA256: strings.Repeat("b", 64),
		State:               state,
		Create:              &canvas.CreateResult{ProjectID: "123", CanvasAssetID: "asset-a", OverviewPippitAssetID: "overview", WebURL: "/novel/detail/canvas?projectId=123"},
		DocumentSHA256:      fmt.Sprintf("%064x", 1),
		AssetSHA256:         hashes,
		Apply:               &ApplyJournal{TransactionID: "transaction-1", Status: applyStatus},
	}
	if state == StateVerified {
		journal.Verification = &Verification{
			ExpectedAssetCount: len(contents),
			ReturnedAssetCount: len(contents),
			Verified:           true,
		}
	}
	if err := saveJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	return path
}
