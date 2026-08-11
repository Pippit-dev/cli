package canvasplan

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/canvas"
)

var journalURLPattern = regexp.MustCompile(`https?://[^\s"']+`)

func (executor *Executor) Execute(
	ctx context.Context,
	inputPlan Plan,
	inputResolved ResolvedMediaSet,
	opts ExecuteOptions,
) (*ExecutionResult, error) {
	return executor.execute(ctx, inputPlan, inputResolved, opts, true)
}

func (executor *Executor) Resume(
	ctx context.Context,
	inputPlan Plan,
	inputResolved ResolvedMediaSet,
	opts ExecuteOptions,
) (*ExecutionResult, error) {
	return executor.execute(ctx, inputPlan, inputResolved, opts, false)
}

func (executor *Executor) execute(
	ctx context.Context,
	inputPlan Plan,
	inputResolved ResolvedMediaSet,
	opts ExecuteOptions,
	allowCreateJournal bool,
) (result *ExecutionResult, returnErr error) {
	if executor == nil || executor.api == nil {
		return nil, fmt.Errorf("CanvasPlan executor API is missing")
	}
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
	if len(plan.Nodes) > canvas.MaxAllocateCount {
		return nil, fmt.Errorf("CanvasPlan contains %d business nodes; maximum is %d", len(plan.Nodes), canvas.MaxAllocateCount)
	}
	journalPath := strings.TrimSpace(opts.JournalPath)
	if journalPath == "" {
		return nil, fmt.Errorf("CanvasPlan journal path is required")
	}
	journalPath, err = filepath.Abs(journalPath)
	if err != nil {
		return nil, fmt.Errorf("resolve CanvasPlan journal path: %w", err)
	}
	journalPath = filepath.Clean(journalPath)
	planHash, err := hashJSON(plan)
	if err != nil {
		return nil, fmt.Errorf("hash CanvasPlan: %w", err)
	}
	resolvedHash, err := hashJSON(resolved)
	if err != nil {
		return nil, fmt.Errorf("hash resolved media: %w", err)
	}

	lock, err := acquireJournalLock(journalPath)
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

	journal, _, err := loadOrCreateJournal(journalPath, planHash, resolvedHash, allowCreateJournal)
	if err != nil {
		return nil, err
	}
	result = executionResult(journalPath, journal, plan)

	if err := executor.ensureRoot(ctx, journalPath, journal, plan, opts); err != nil {
		return executionResult(journalPath, journal, plan), err
	}
	if journal.State == StateCreatePending {
		return executionResult(journalPath, journal, plan), nil
	}
	if err := executor.ensureAllocation(ctx, journalPath, journal, plan); err != nil {
		return executionResult(journalPath, journal, plan), err
	}

	document, err := Materialize(plan, resolved, journal.Create.CanvasAssetID, journal.NodeAssetIDs)
	if err != nil {
		return failExecution(journalPath, journal, plan, StateMaterializationDrift, err)
	}
	documentHash, err := DocumentSHA256(document)
	if err != nil {
		return failExecution(journalPath, journal, plan, StateMaterializationDrift, err)
	}
	assetHashes, err := DocumentAssetSHA256(document)
	if err != nil {
		return failExecution(journalPath, journal, plan, StateMaterializationDrift, err)
	}
	if journal.DocumentSHA256 != "" && journal.DocumentSHA256 != documentHash {
		driftErr := fmt.Errorf("CanvasPlan materialization changed after it was journaled; refusing to apply")
		return failExecution(journalPath, journal, plan, StateMaterializationDrift, driftErr)
	}
	if len(journal.AssetSHA256) != 0 && !equalStringMaps(journal.AssetSHA256, assetHashes) {
		driftErr := fmt.Errorf("CanvasPlan asset materialization changed after it was journaled; refusing to apply")
		return failExecution(journalPath, journal, plan, StateMaterializationDrift, driftErr)
	}
	if journal.DocumentSHA256 == "" {
		journal.DocumentSHA256 = documentHash
		journal.AssetSHA256 = assetHashes
		journal.State = StateMaterialized
		journal.LastError = ""
		if err := saveJournal(journalPath, journal); err != nil {
			return executionResult(journalPath, journal, plan), err
		}
	}
	if journal.State == StateVerified {
		if journal.Verification == nil || !journal.Verification.Verified {
			return executionResult(journalPath, journal, plan), fmt.Errorf("verified CanvasPlan journal is missing successful verification")
		}
		return executor.reverifyCompleted(ctx, journalPath, journal, plan, document)
	}

	return executor.applyAndVerify(ctx, journalPath, journal, plan, document)
}

func (executor *Executor) reverifyCompleted(
	ctx context.Context,
	journalPath string,
	journal *Journal,
	plan Plan,
	document *Document,
) (*ExecutionResult, error) {
	assetIDs := DocumentAssetIDs(document)
	queried, err := executor.api.Get(ctx, assetIDs)
	if err != nil {
		result := executionResult(journalPath, journal, plan)
		result.Warning = fmt.Sprintf("query previously verified Canvas with current authentication: %v", err)
		return result, fmt.Errorf("%s", result.Warning)
	}
	if queried == nil {
		result := executionResult(journalPath, journal, plan)
		result.Warning = "query previously verified Canvas with current authentication returned no result"
		return result, fmt.Errorf("%s", result.Warning)
	}
	verification := verifyJournalAssetHashes(journal.AssetSHA256, queried.Assets)
	verification.LogID = queried.LogID
	verification.RecoveredFromQuery = true
	if len(verification.MissingAssetIDs) != 0 || len(verification.UnverifiableAssetIDs) != 0 {
		result := executionResult(journalPath, journal, plan)
		result.Warning = fmt.Sprintf(
			"previously verified Canvas is not fully accessible with current authentication: missing=%d unverifiable=%d",
			len(verification.MissingAssetIDs),
			len(verification.UnverifiableAssetIDs),
		)
		return result, fmt.Errorf("%s", result.Warning)
	}
	changed := append([]string(nil), verification.MismatchedAssetIDs...)
	result := executionResult(journalPath, journal, plan)
	if len(changed) != 0 {
		result.Warning = fmt.Sprintf(
			"%d Canvas asset(s) changed after the import was originally verified; current access is valid and apply was not replayed",
			len(changed),
		)
	}
	return result, nil
}

func (executor *Executor) ensureRoot(
	ctx context.Context,
	journalPath string,
	journal *Journal,
	plan Plan,
	opts ExecuteOptions,
) error {
	switch journal.State {
	case StateInitialized:
		journal.State = StateCreateRequested
		journal.LastError = ""
		if err := saveJournal(journalPath, journal); err != nil {
			return err
		}
		created, err := executor.api.Create(ctx, canvas.CreateOptions{
			Title:        plan.Title,
			RequestID:    journal.RequestID,
			Wait:         true,
			PollInterval: opts.PollInterval,
			WaitTimeout:  opts.WaitTimeout,
		})
		return finishCreate(journalPath, journal, created, err)
	case StateCreateRequested:
		return recordJournalError(
			journalPath,
			journal,
			StateCreateAmbiguous,
			fmt.Errorf("canvas create may have been sent before interruption; do not create again blindly"),
		)
	case StateCreatePending:
		if journal.Create == nil {
			return recordJournalError(journalPath, journal, StateCreateAmbiguous, fmt.Errorf("pending canvas create has no accepted IDs"))
		}
		created, err := executor.api.ResumeCreate(ctx, journal.Create, canvas.ResumeCreateOptions{
			PollInterval: opts.PollInterval,
			WaitTimeout:  opts.WaitTimeout,
		})
		return finishCreate(journalPath, journal, created, err)
	case StateCreateAmbiguous, StateCreateFailed:
		return fmt.Errorf("CanvasPlan create is in terminal safety state %q; inspect journal and accepted IDs before retrying", journal.State)
	default:
		if journal.Create == nil || journal.Create.State != canvas.StateReady {
			return fmt.Errorf("CanvasPlan journal state %q has no ready personal novel canvas", journal.State)
		}
		return nil
	}
}

func finishCreate(journalPath string, journal *Journal, created *canvas.CreateResult, createErr error) error {
	if created != nil {
		journal.Create = created
	}
	if createErr != nil {
		if canvas.IsCredentialUnavailable(createErr) {
			if created == nil {
				// The shared authorizer proves that no HTTP request was issued.
				// Roll back only this local marker so the import may authenticate
				// and make its one allowed Create call.
				return recordJournalError(journalPath, journal, StateInitialized, createErr)
			}
			// Create already returned durable IDs. Persist them as pending and
			// let the next attempt call ResumeCreate only; never send Create again.
			journal.State = StateCreatePending
			journal.LastError = sanitizeJournalError(createErr.Error())
			if strings.TrimSpace(created.Warning) == "" {
				created.Warning = journal.LastError
			}
			if err := saveJournal(journalPath, journal); err != nil {
				return fmt.Errorf("%w; additionally failed to save CanvasPlan journal: %v", createErr, err)
			}
			return createErr
		}
		state := StateCreateAmbiguous
		if created != nil {
			state = StateCreateFailed
		}
		return recordJournalError(journalPath, journal, state, createErr)
	}
	if created == nil {
		return recordJournalError(journalPath, journal, StateCreateAmbiguous, fmt.Errorf("canvas create returned no result; do not create again blindly"))
	}
	if created.State != canvas.StateReady {
		journal.State = StateCreatePending
		journal.LastError = sanitizeJournalError(created.Warning)
		return saveJournal(journalPath, journal)
	}
	journal.State = StateRootReady
	journal.LastError = ""
	return saveJournal(journalPath, journal)
}

func (executor *Executor) ensureAllocation(ctx context.Context, journalPath string, journal *Journal, plan Plan) error {
	if len(journal.NodeAssetIDs) == len(plan.Nodes) {
		return nil
	}
	if len(journal.NodeAssetIDs) != 0 {
		return recordJournalError(
			journalPath,
			journal,
			StateMaterializationDrift,
			fmt.Errorf("CanvasPlan journal has %d allocated node IDs, want %d", len(journal.NodeAssetIDs), len(plan.Nodes)),
		)
	}
	if journal.State != StateRootReady && journal.State != StateAllocationRequested {
		return fmt.Errorf("CanvasPlan journal state %q is missing allocated node IDs", journal.State)
	}
	if journal.State == StateRootReady {
		journal.State = StateAllocationRequested
		journal.LastError = ""
		if err := saveJournal(journalPath, journal); err != nil {
			return err
		}
	}
	allocation, err := executor.api.Allocate(ctx, len(plan.Nodes))
	if err != nil {
		return recordJournalError(journalPath, journal, StateAllocationRequested, err)
	}
	if allocation == nil || len(allocation.AssetIDs) != len(plan.Nodes) {
		return recordJournalError(journalPath, journal, StateAllocationRequested, fmt.Errorf("canvas allocation result is incomplete"))
	}
	journal.NodeAssetIDs = make(map[string]string, len(plan.Nodes))
	for index, node := range plan.Nodes {
		journal.NodeAssetIDs[node.LogicalID] = allocation.AssetIDs[index]
	}
	journal.AllocationLogID = allocation.LogID
	journal.State = StateAllocated
	journal.LastError = ""
	return saveJournal(journalPath, journal)
}

func (executor *Executor) applyAndVerify(
	ctx context.Context,
	journalPath string,
	journal *Journal,
	plan Plan,
	document *Document,
) (*ExecutionResult, error) {
	assetIDs := DocumentAssetIDs(document)
	existing, err := executor.api.GetExisting(ctx, assetIDs)
	if err != nil {
		return failExecution(journalPath, journal, plan, journal.State, fmt.Errorf("query CanvasPlan assets before apply: %w", err))
	}
	indexed, err := indexQueriedAssets(existing.Assets)
	if err != nil {
		return failExecution(journalPath, journal, plan, journal.State, fmt.Errorf("index CanvasPlan preflight assets: %w", err))
	}
	verification := VerifyDocument(document, existing.Assets)
	verification.LogID = existing.LogID
	if verification.Verified {
		verification.RecoveredFromQuery = true
		journal.Verification = &verification
		journal.State = StateVerified
		journal.LastError = ""
		if journal.Apply != nil {
			journal.Apply.Status = "verified"
		}
		if err := saveJournal(journalPath, journal); err != nil {
			return executionResult(journalPath, journal, plan), err
		}
		return executionResult(journalPath, journal, plan), nil
	}
	if companionIDs := existingCompanionIDs(document, indexed); len(companionIDs) != 0 {
		partialErr := fmt.Errorf("query found %d companion assets without an exact full document match; refusing partial apply replay", len(companionIDs))
		journal.Verification = &verification
		return failExecution(journalPath, journal, plan, StateUnsafePartial, partialErr)
	}
	rootRaw, rootExists := indexed[document.RootCanvasID]
	if !rootExists {
		missingErr := fmt.Errorf("created Canvas root %q is not queryable", document.RootCanvasID)
		if journal.Apply != nil && journal.Apply.Status != "prepared" {
			journal.Verification = &verification
			return failExecution(journalPath, journal, plan, StateVerificationFailed, missingErr)
		}
		return failExecution(journalPath, journal, plan, journal.State, missingErr)
	}
	rootVersion, err := queriedAssetVersion(rootRaw)
	if err != nil {
		return failExecution(journalPath, journal, plan, journal.State, fmt.Errorf("read created Canvas root version: %w", err))
	}

	request, err := prepareApplyRequest(journal, document, rootVersion)
	if err != nil {
		state := journal.State
		if strings.Contains(err.Error(), "root changed") {
			state = StateUnsafeRootChanged
		}
		return failExecution(journalPath, journal, plan, state, err)
	}
	if journal.Apply != nil && journal.Apply.Status != "prepared" {
		return executionResult(journalPath, journal, plan), fmt.Errorf(
			"CanvasPlan apply is journaled as %q but query-back did not verify; refusing to replay blindly",
			journal.Apply.Status,
		)
	}
	if journal.Apply == nil {
		requestHash, hashErr := hashJSON(request)
		if hashErr != nil {
			return executionResult(journalPath, journal, plan), hashErr
		}
		journal.Apply = &ApplyJournal{
			TransactionID:   request.Transactions[0].TransactionID,
			BatchID:         request.BatchID,
			ClientID:        request.ClientID,
			BaseRootVersion: rootVersion,
			RequestSHA256:   requestHash,
			Status:          "prepared",
		}
		journal.State = StateApplyPrepared
		journal.LastError = ""
		if err := saveJournal(journalPath, journal); err != nil {
			return executionResult(journalPath, journal, plan), err
		}
	}

	journal.State = StateApplyRequested
	journal.Apply.Status = "requested"
	journal.LastError = ""
	if err := saveJournal(journalPath, journal); err != nil {
		return executionResult(journalPath, journal, plan), err
	}
	applyResult, err := executor.api.Apply(ctx, canvas.ApplyOptions{ProjectID: journal.Create.ProjectID, Request: request})
	if err != nil {
		return executor.recoverAmbiguousApplyByQuery(ctx, journalPath, journal, plan, document, assetIDs, err)
	}
	if applyResult == nil || len(applyResult.Results) != 1 {
		return executor.recoverAmbiguousApplyByQuery(
			ctx, journalPath, journal, plan, document, assetIDs,
			fmt.Errorf("canvas apply acknowledgement is incomplete; query affected assets and do not replay blindly"),
		)
	}
	journal.Apply.Status = "acknowledged"
	journal.Apply.AssetVersions = applyResult.Results[0].AssetVersions
	journal.Apply.LogID = applyResult.LogID
	journal.State = StateApplyAcknowledged
	journal.LastError = ""
	if err := saveJournal(journalPath, journal); err != nil {
		return executionResult(journalPath, journal, plan), err
	}

	queried, err := executor.api.Get(ctx, assetIDs)
	if err != nil {
		journal.Apply.Status = "verification-failed"
		return failExecution(journalPath, journal, plan, StateVerificationFailed, fmt.Errorf("query CanvasPlan assets after apply: %w", err))
	}
	verification = VerifyDocument(document, queried.Assets)
	verification.LogID = queried.LogID
	journal.Verification = &verification
	if !verification.Verified {
		journal.Apply.Status = "verification-failed"
		verifyErr := fmt.Errorf(
			"CanvasPlan query-back verification failed: missing=%d unverifiable=%d mismatched=%d; do not replay blindly",
			len(verification.MissingAssetIDs),
			len(verification.UnverifiableAssetIDs),
			len(verification.MismatchedAssetIDs),
		)
		return failExecution(journalPath, journal, plan, StateVerificationFailed, verifyErr)
	}
	journal.Apply.Status = "verified"
	journal.State = StateVerified
	journal.LastError = ""
	if err := saveJournal(journalPath, journal); err != nil {
		return executionResult(journalPath, journal, plan), err
	}
	return executionResult(journalPath, journal, plan), nil
}

func (executor *Executor) recoverAmbiguousApplyByQuery(
	ctx context.Context,
	journalPath string,
	journal *Journal,
	plan Plan,
	document *Document,
	assetIDs []string,
	applyCause error,
) (*ExecutionResult, error) {
	journal.Apply.Status = "ambiguous"
	journal.State = StateApplyAmbiguous
	journal.LastError = sanitizeJournalError(applyCause.Error())
	if err := saveJournal(journalPath, journal); err != nil {
		return executionResult(journalPath, journal, plan), fmt.Errorf(
			"%w; additionally failed to persist ambiguous Canvas apply: %v",
			applyCause,
			err,
		)
	}

	queried, queryErr := executor.api.Get(ctx, assetIDs)
	if queryErr != nil {
		return executionResult(journalPath, journal, plan), fmt.Errorf(
			"%w; exact query-back after the ambiguous response also failed: %v",
			applyCause,
			queryErr,
		)
	}
	if queried == nil {
		return executionResult(journalPath, journal, plan), fmt.Errorf(
			"%w; exact query-back after the ambiguous response returned no result",
			applyCause,
		)
	}
	verification := VerifyDocument(document, queried.Assets)
	verification.LogID = queried.LogID
	verification.RecoveredFromQuery = true
	if !verification.Verified {
		result := executionResult(journalPath, journal, plan)
		result.Verification = &verification
		return result, fmt.Errorf(
			"%w; exact query-back after the ambiguous response did not verify: missing=%d unverifiable=%d mismatched=%d; apply was not replayed",
			applyCause,
			len(verification.MissingAssetIDs),
			len(verification.UnverifiableAssetIDs),
			len(verification.MismatchedAssetIDs),
		)
	}

	journal.Verification = &verification
	journal.Apply.Status = "verified"
	journal.State = StateVerified
	journal.LastError = ""
	if err := saveJournal(journalPath, journal); err != nil {
		return executionResult(journalPath, journal, plan), err
	}
	result := executionResult(journalPath, journal, plan)
	result.Warning = "Canvas apply response was ambiguous; all expected assets were recovered by exact query-back and apply was not replayed"
	return result, nil
}

func prepareApplyRequest(journal *Journal, document *Document, rootVersion int64) (canvas.ApplyRequest, error) {
	if journal.Create == nil || !isPositiveDecimal(journal.Create.ProjectID) {
		return canvas.ApplyRequest{}, fmt.Errorf("created personal novel project_id must be a positive decimal JSON string")
	}
	stable := hashBytes([]byte(journal.OperationID + ":" + journal.DocumentSHA256))[:24]
	transactionID := "canvas_plan_" + stable
	request := canvas.ApplyRequest{
		BatchID:           "batch_" + transactionID,
		ClientID:          "pippit_cli_canvas_plan_" + stable,
		RootPippitAssetID: document.RootCanvasID,
		Base:              map[string]any{},
		Transactions: []canvas.PatchTransaction{{
			TransactionID: transactionID,
			Intent:        "canvas.write",
		}},
	}
	rootVersionCopy := rootVersion
	request.Transactions[0].Patches = append(request.Transactions[0].Patches, canvas.PatchEntry{
		AssetID:          document.RootCanvasID,
		BaseAssetVersion: &rootVersionCopy,
		Op:               "replace",
		Path:             "",
		Value:            document.Assets[document.RootCanvasID],
	})
	for _, assetID := range DocumentAssetIDs(document)[1:] {
		zero := int64(0)
		request.Transactions[0].Patches = append(request.Transactions[0].Patches, canvas.PatchEntry{
			AssetID:          assetID,
			BaseAssetVersion: &zero,
			Op:               "add",
			Path:             "",
			Value:            document.Assets[assetID],
		})
	}
	requestHash, err := hashJSON(request)
	if err != nil {
		return canvas.ApplyRequest{}, err
	}
	if journal.Apply != nil {
		if journal.Apply.BaseRootVersion != rootVersion {
			return canvas.ApplyRequest{}, fmt.Errorf("created Canvas root changed after apply was prepared; refusing to overwrite it")
		}
		if journal.Apply.TransactionID != transactionID || journal.Apply.BatchID != request.BatchID ||
			journal.Apply.ClientID != request.ClientID || journal.Apply.RequestSHA256 != requestHash {
			return canvas.ApplyRequest{}, fmt.Errorf("journaled CanvasPlan apply request differs from materialized request; refusing to replay")
		}
	}
	return request, nil
}

func executionResult(journalPath string, journal *Journal, plan Plan) *ExecutionResult {
	if journal == nil {
		return nil
	}
	result := &ExecutionResult{
		State:            journal.State,
		JournalPath:      journalPath,
		OperationID:      journal.OperationID,
		DocumentSHA256:   journal.DocumentSHA256,
		NodeCount:        len(plan.Nodes) + len(plan.Groups),
		EdgeCount:        len(plan.Edges),
		DegradationCount: len(plan.Degradations),
		Verification:     journal.Verification,
	}
	if journal.DocumentSHA256 != "" {
		result.AssetCount = len(journal.AssetSHA256)
	}
	if journal.Create != nil {
		result.ProjectID = journal.Create.ProjectID
		result.RootCanvasID = journal.Create.CanvasAssetID
		result.OverviewPippitAssetID = journal.Create.OverviewPippitAssetID
		result.WebURL = journal.Create.WebURL
		if journal.State == StateCreatePending {
			result.Warning = journal.Create.Warning
		}
	}
	if journal.Apply != nil {
		result.TransactionID = journal.Apply.TransactionID
	}
	if result.Warning == "" && strings.Contains(journal.State, "ambiguous") {
		result.Warning = journal.LastError
	}
	return result
}

func recordJournalError(journalPath string, journal *Journal, state string, cause error) error {
	journal.State = state
	journal.LastError = sanitizeJournalError(cause.Error())
	if err := saveJournal(journalPath, journal); err != nil {
		return fmt.Errorf("%w; additionally failed to save CanvasPlan journal: %v", cause, err)
	}
	return cause
}

func failExecution(journalPath string, journal *Journal, plan Plan, state string, cause error) (*ExecutionResult, error) {
	err := recordJournalError(journalPath, journal, state, cause)
	return executionResult(journalPath, journal, plan), err
}

func sanitizeJournalError(value string) string {
	value = journalURLPattern.ReplaceAllString(strings.TrimSpace(value), "[redacted-url]")
	const limit = 1024
	if len(value) > limit {
		return value[:limit] + "..."
	}
	return value
}

func equalStringMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func isPositiveDecimal(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value[0] == '0' {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
