package canvasplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

// ErrReconcileNotEligible indicates that normal Execute/Resume state handling
// must continue; no remote write was issued by Reconcile.
var ErrReconcileNotEligible = errors.New("CanvasPlan journal is not eligible for journal-only reconciliation")

// Reconcile verifies an existing execution journal from its persisted asset
// hashes. It never creates, allocates, or applies Canvas assets.
func Reconcile(ctx context.Context, journalPath string, runner *common.Runner) (*ExecutionResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("CanvasPlan runner client is missing")
	}
	return NewExecutor(runner).Reconcile(ctx, journalPath)
}

// ReconcileWithInputs verifies a journal only after binding it to the current
// normalized plan and resolved-media inputs. Import orchestrators should use
// this form so an explicit journal cannot be resumed for a different source.
func ReconcileWithInputs(
	ctx context.Context,
	journalPath string,
	plan Plan,
	resolved ResolvedMediaSet,
	runner *common.Runner,
) (*ExecutionResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("CanvasPlan runner client is missing")
	}
	return NewExecutor(runner).ReconcileWithInputs(ctx, journalPath, plan, resolved)
}

// Reconcile verifies an ambiguous or previously verified journal without the
// original plan or resolved-media inputs and without replaying Apply.
func (executor *Executor) Reconcile(
	ctx context.Context,
	journalPath string,
) (result *ExecutionResult, returnErr error) {
	return executor.reconcile(ctx, journalPath, "", "", nil)
}

// ReconcileWithInputs binds reconciliation to the exact normalized plan and
// resolved-media identity already recorded by the execution journal.
func (executor *Executor) ReconcileWithInputs(
	ctx context.Context,
	journalPath string,
	inputPlan Plan,
	inputResolved ResolvedMediaSet,
) (*ExecutionResult, error) {
	plan, err := NormalizePlan(inputPlan)
	if err != nil {
		return nil, err
	}
	resolved, err := NormalizeResolvedMedia(inputResolved)
	if err != nil {
		return nil, err
	}
	if err := ValidateResolution(plan, resolved); err != nil {
		return nil, err
	}
	planHash, err := hashJSON(plan)
	if err != nil {
		return nil, fmt.Errorf("hash CanvasPlan for reconciliation: %w", err)
	}
	resolvedHash, err := hashJSON(resolved)
	if err != nil {
		return nil, fmt.Errorf("hash resolved media for reconciliation: %w", err)
	}
	return executor.reconcile(ctx, journalPath, planHash, resolvedHash, &plan)
}

func (executor *Executor) reconcile(
	ctx context.Context,
	journalPath string,
	expectedPlanHash string,
	expectedResolvedHash string,
	plan *Plan,
) (result *ExecutionResult, returnErr error) {
	if executor == nil || executor.api == nil {
		return nil, fmt.Errorf("CanvasPlan executor API is missing")
	}
	journalPath = strings.TrimSpace(journalPath)
	if journalPath == "" {
		return nil, fmt.Errorf("CanvasPlan journal path is required")
	}
	absolute, err := filepath.Abs(journalPath)
	if err != nil {
		return nil, fmt.Errorf("resolve CanvasPlan journal path: %w", err)
	}
	absolute = filepath.Clean(absolute)

	lock, err := acquireJournalLock(absolute)
	if err != nil {
		return nil, err
	}
	defer func() {
		if releaseErr := lock.release(); releaseErr != nil {
			if returnErr == nil {
				returnErr = fmt.Errorf("release CanvasPlan journal lock: %w", releaseErr)
			} else {
				returnErr = fmt.Errorf("%w; release CanvasPlan journal lock: %v", returnErr, releaseErr)
			}
		}
	}()

	journal, err := loadReconcileJournal(absolute)
	if err != nil {
		return nil, err
	}
	result = reconciliationResult(absolute, journal, plan)
	if err := validateReconcileJournal(journal); err != nil {
		return result, err
	}
	if expectedPlanHash != "" &&
		(journal.PlanSHA256 != expectedPlanHash || journal.ResolvedMediaSHA256 != expectedResolvedHash) {
		return result, fmt.Errorf("CanvasPlan or resolved media changed after journal creation")
	}

	assetIDs := make([]string, 0, len(journal.AssetSHA256))
	for assetID := range journal.AssetSHA256 {
		assetIDs = append(assetIDs, assetID)
	}
	sort.Strings(assetIDs)
	wasVerified := journalWasVerified(journal)
	queried, err := executor.api.Get(ctx, assetIDs)
	if err != nil {
		result.Warning = fmt.Sprintf("query CanvasPlan assets with current authentication: %v", err)
		return result, fmt.Errorf("%s", result.Warning)
	}
	if queried == nil {
		result.Warning = "query CanvasPlan assets with current authentication returned no result"
		return result, fmt.Errorf("%s", result.Warning)
	}

	verification := verifyJournalAssetHashes(journal.AssetSHA256, queried.Assets)
	verification.LogID = queried.LogID
	verification.RecoveredFromQuery = true
	if wasVerified {
		if len(verification.MissingAssetIDs) != 0 || len(verification.UnverifiableAssetIDs) != 0 {
			result.Warning = fmt.Sprintf(
				"previously verified Canvas is not fully accessible with current authentication: missing=%d unverifiable=%d",
				len(verification.MissingAssetIDs),
				len(verification.UnverifiableAssetIDs),
			)
			return result, fmt.Errorf("%s", result.Warning)
		}
		changed := append([]string(nil), verification.MismatchedAssetIDs...)
		if journal.State == StateVerified {
			result = reconciliationResult(absolute, journal, plan)
			if len(changed) != 0 {
				result.Warning = fmt.Sprintf(
					"%d Canvas asset(s) changed after the import was originally verified; current access is valid and apply was not replayed",
					len(changed),
				)
			}
			return result, nil
		}
		// This state is only used to recover a completed import that was
		// incorrectly demoted by a later current-account query. Keep the
		// original strict verification as the audit record; the current query
		// establishes accessibility, not content immutability.
		if journal.Verification == nil || !journal.Verification.Verified {
			// Older clients replaced the original strict verification with a
			// later failed current-account check. Apply.Status="verified" is only
			// persisted after strict query-back succeeds, so rebuild the lost
			// completion marker without treating current content edits as drift.
			journal.Verification = &Verification{
				ExpectedAssetCount: len(assetIDs),
				ReturnedAssetCount: len(queried.Assets),
				Verified:           true,
				RecoveredFromQuery: true,
				LogID:              queried.LogID,
			}
		}
		journal.State = StateVerified
		journal.LastError = ""
		if err := saveJournal(absolute, journal); err != nil {
			return reconciliationResult(absolute, journal, plan), err
		}
		result = reconciliationResult(absolute, journal, plan)
		if len(changed) != 0 {
			result.Warning = fmt.Sprintf(
				"%d Canvas asset(s) changed after the import was originally verified; current access is valid and apply was not replayed",
				len(changed),
			)
		}
		return result, nil
	}
	if !verification.Verified {
		result.Verification = &verification
		result.Warning = fmt.Sprintf(
			"CanvasPlan journal reconciliation failed: missing=%d unverifiable=%d mismatched=%d; apply was not replayed",
			len(verification.MissingAssetIDs),
			len(verification.UnverifiableAssetIDs),
			len(verification.MismatchedAssetIDs),
		)
		return result, fmt.Errorf("%s", result.Warning)
	}

	journal.Verification = &verification
	journal.State = StateVerified
	journal.LastError = ""
	if journal.Apply != nil {
		journal.Apply.Status = "verified"
	}
	if err := saveJournal(absolute, journal); err != nil {
		return reconciliationResult(absolute, journal, plan), err
	}
	return reconciliationResult(absolute, journal, plan), nil
}

func journalWasVerified(journal *Journal) bool {
	if journal == nil {
		return false
	}
	if journal.State == StateVerified {
		return journal.Verification != nil && journal.Verification.Verified
	}
	return journal.State == StateVerificationFailed && journal.Apply != nil && journal.Apply.Status == "verified"
}

func loadReconcileJournal(path string) (*Journal, error) {
	directory, err := ensureSecureJournalDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	file, err := openRegularFileNoFollow(path, os.O_RDWR, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("CanvasPlan journal does not exist: %s", path)
		}
		return nil, fmt.Errorf("open CanvasPlan journal: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("secure CanvasPlan journal permissions: %w", err)
	}
	journal, err := decodeJournal(io.LimitReader(file, maxJournalBytes+1))
	if err != nil {
		return nil, err
	}
	if journal.Schema != JournalSchema {
		return nil, fmt.Errorf("unsupported CanvasPlan journal schema %q", journal.Schema)
	}
	if strings.TrimSpace(journal.OperationID) == "" || strings.TrimSpace(journal.RequestID) == "" || strings.TrimSpace(journal.State) == "" {
		return nil, fmt.Errorf("CanvasPlan journal identity or state is incomplete")
	}
	if err := directory.validateStable(); err != nil {
		return nil, err
	}
	return journal, nil
}

func validateReconcileJournal(journal *Journal) error {
	if !sha256Pattern.MatchString(journal.PlanSHA256) || !sha256Pattern.MatchString(journal.ResolvedMediaSHA256) {
		return fmt.Errorf("CanvasPlan journal has invalid plan or resolved-media SHA-256")
	}
	switch journal.State {
	case StateApplyAmbiguous:
		if journal.Apply == nil || journal.Apply.Status != "ambiguous" {
			return fmt.Errorf("CanvasPlan apply-ambiguous journal has no ambiguous apply record")
		}
	case StateVerified:
		if journal.Verification == nil || !journal.Verification.Verified {
			return fmt.Errorf("verified CanvasPlan journal is missing successful historical verification")
		}
	case StateVerificationFailed:
		if journal.Apply == nil || journal.Apply.Status != "verified" {
			return fmt.Errorf("%w: state %q has no prior verified apply", ErrReconcileNotEligible, journal.State)
		}
	default:
		return fmt.Errorf("%w: state %q", ErrReconcileNotEligible, journal.State)
	}
	if journal.Create == nil {
		return fmt.Errorf("CanvasPlan journal is missing its create result")
	}
	if !sha256Pattern.MatchString(journal.DocumentSHA256) {
		return fmt.Errorf("CanvasPlan journal has no valid document SHA-256")
	}
	if len(journal.AssetSHA256) == 0 {
		return fmt.Errorf("CanvasPlan journal has no asset SHA-256 entries")
	}
	for assetID, digest := range journal.AssetSHA256 {
		if strings.TrimSpace(assetID) == "" || assetID != strings.TrimSpace(assetID) || !sha256Pattern.MatchString(digest) {
			return fmt.Errorf("CanvasPlan journal contains an invalid asset SHA-256 entry")
		}
	}
	return nil
}

func verifyJournalAssetHashes(expected map[string]string, assets []json.RawMessage) Verification {
	verification := Verification{ExpectedAssetCount: len(expected), ReturnedAssetCount: len(assets)}
	seen := make(map[string]struct{}, len(assets))
	for index, asset := range assets {
		assetID, err := queriedAssetID(asset)
		if err != nil {
			verification.UnverifiableAssetIDs = append(verification.UnverifiableAssetIDs, fmt.Sprintf("response[%d]", index))
			continue
		}
		expectedHash, requested := expected[assetID]
		if !requested {
			verification.UnverifiableAssetIDs = append(verification.UnverifiableAssetIDs, assetID)
			continue
		}
		if _, duplicate := seen[assetID]; duplicate {
			verification.UnverifiableAssetIDs = append(verification.UnverifiableAssetIDs, assetID)
			continue
		}
		seen[assetID] = struct{}{}
		content, err := queriedAssetContent(asset)
		if err != nil {
			verification.UnverifiableAssetIDs = append(verification.UnverifiableAssetIDs, assetID)
			continue
		}
		storedHash, err := hashRawJSON(content)
		if err != nil {
			verification.UnverifiableAssetIDs = append(verification.UnverifiableAssetIDs, assetID)
			continue
		}
		if storedHash != expectedHash {
			verification.MismatchedAssetIDs = append(verification.MismatchedAssetIDs, assetID)
		}
	}
	for assetID := range expected {
		if _, exists := seen[assetID]; !exists {
			verification.MissingAssetIDs = append(verification.MissingAssetIDs, assetID)
		}
	}
	sort.Strings(verification.MissingAssetIDs)
	sort.Strings(verification.UnverifiableAssetIDs)
	sort.Strings(verification.MismatchedAssetIDs)
	verification.Verified = len(verification.MissingAssetIDs) == 0 &&
		len(verification.UnverifiableAssetIDs) == 0 &&
		len(verification.MismatchedAssetIDs) == 0
	return verification
}

func reconciliationResult(journalPath string, journal *Journal, plan *Plan) *ExecutionResult {
	if journal == nil {
		return nil
	}
	result := &ExecutionResult{
		State:          journal.State,
		JournalPath:    journalPath,
		OperationID:    journal.OperationID,
		DocumentSHA256: journal.DocumentSHA256,
		AssetCount:     len(journal.AssetSHA256),
		Verification:   journal.Verification,
	}
	if plan != nil {
		result.NodeCount = len(plan.Nodes) + len(plan.Groups)
		result.EdgeCount = len(plan.Edges)
		result.DegradationCount = len(plan.Degradations)
	}
	if journal.Create != nil {
		result.ProjectID = journal.Create.ProjectID
		result.RootCanvasID = journal.Create.CanvasAssetID
		result.OverviewPippitAssetID = journal.Create.OverviewPippitAssetID
		result.WebURL = journal.Create.WebURL
	}
	if journal.Apply != nil {
		result.TransactionID = journal.Apply.TransactionID
	}
	if journal.State != StateVerified {
		result.Warning = journal.LastError
	}
	return result
}
