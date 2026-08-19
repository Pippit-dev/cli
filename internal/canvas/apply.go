package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

var decimalIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

type ApplyOptions struct {
	ProjectID                   string
	Request                     ApplyRequest
	AllowNonAcknowledgedResults bool
}

type ApplyRequest struct {
	BatchID           string             `json:"batch_id"`
	ClientID          string             `json:"client_id"`
	RootPippitAssetID string             `json:"root_pippit_asset_id,omitempty"`
	Transactions      []PatchTransaction `json:"transactions"`
	Base              map[string]any     `json:"Base"`
}

type PatchTransaction struct {
	TransactionID string       `json:"transaction_id"`
	Intent        string       `json:"intent,omitempty"`
	MergeKey      string       `json:"merge_key,omitempty"`
	Attempt       *int32       `json:"attempt,omitempty"`
	EnqueuedAt    *int64       `json:"enqueued_at,omitempty"`
	Patches       []PatchEntry `json:"patches"`
}

type PatchEntry struct {
	AssetID          string          `json:"asset_id"`
	BaseAssetVersion *int64          `json:"base_asset_version,omitempty"`
	AssetSourceType  *int32          `json:"asset_source_type,omitempty"`
	Op               string          `json:"op"`
	Path             string          `json:"path"`
	Value            json.RawMessage `json:"value,omitempty"`
}

type ApplyResult struct {
	BatchID string                   `json:"batch_id"`
	Results []PatchTransactionResult `json:"results"`
	LogID   string                   `json:"log_id,omitempty"`

	rawResults []json.RawMessage
}

type PatchTransactionResult struct {
	TransactionID        string           `json:"transaction_id"`
	Status               string           `json:"status"`
	AssetVersions        map[string]int64 `json:"asset_versions,omitempty"`
	BlockedByTransaction string           `json:"blocked_by,omitempty"`
	Error                string           `json:"error,omitempty"`
}

type applyData struct {
	Results []json.RawMessage `json:"results"`
}

type decodedPatchTransactionResult struct {
	result PatchTransactionResult
	raw    json.RawMessage
}

func (result ApplyResult) MarshalJSON() ([]byte, error) {
	type applyResultJSON struct {
		BatchID string `json:"batch_id"`
		Results any    `json:"results"`
		LogID   string `json:"log_id,omitempty"`
	}
	results := any(result.Results)
	if len(result.rawResults) > 0 {
		results = result.rawResults
	}
	return json.Marshal(applyResultJSON{
		BatchID: result.BatchID,
		Results: results,
		LogID:   result.LogID,
	})
}

func Apply(ctx context.Context, opts ApplyOptions, runner *common.Runner) (*ApplyResult, error) {
	client, err := runnerClient(runner, "canvas apply")
	if err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(opts.ProjectID)
	if projectID != "" && !decimalIDPattern.MatchString(projectID) {
		return nil, fmt.Errorf("canvas apply project_id must be a positive decimal JSON string")
	}
	request := opts.Request
	request.Base = base()
	if err := validateApplyRequest(&request); err != nil {
		return nil, err
	}

	var envelope responseEnvelope
	if err := client.SendRequest(ctx, pathWithProjectID(ApplyPath, projectID), request, &envelope); err != nil {
		return nil, fmt.Errorf("canvas apply request failed; outcome may be ambiguous, do not replay blindly: %w", err)
	}
	if err := envelope.validate("canvas apply"); err != nil {
		return nil, err
	}
	var data applyData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, common.NewLogIDError(
			fmt.Sprintf("canvas apply returned invalid data: %v; query affected assets before retrying because outcome cannot be confirmed", err),
			envelope.LogID,
		)
	}
	decodedResults, err := decodeApplyResults(data.Results)
	if err != nil {
		return nil, common.NewLogIDError(
			fmt.Sprintf("canvas apply returned invalid data: %v; query affected assets before retrying because outcome cannot be confirmed", err),
			envelope.LogID,
		)
	}
	ordered, rawResults, _, err := validateApplyResults(
		request.Transactions,
		decodedResults,
		opts.AllowNonAcknowledgedResults,
	)
	if err != nil {
		return nil, common.NewLogIDError(
			fmt.Sprintf("%s; query affected assets before retrying because outcome cannot be confirmed", err),
			envelope.LogID,
		)
	}
	result := &ApplyResult{
		BatchID: request.BatchID,
		Results: ordered,
		LogID:   strings.TrimSpace(envelope.LogID),
	}
	if opts.AllowNonAcknowledgedResults {
		result.rawResults = rawResults
	}
	return result, nil
}

func validateApplyRequest(request *ApplyRequest) error {
	if request == nil {
		return fmt.Errorf("canvas apply request is required")
	}
	request.BatchID = strings.TrimSpace(request.BatchID)
	request.ClientID = strings.TrimSpace(request.ClientID)
	request.RootPippitAssetID = strings.TrimSpace(request.RootPippitAssetID)
	if request.BatchID == "" {
		return fmt.Errorf("canvas apply batch_id is required")
	}
	if request.ClientID == "" {
		return fmt.Errorf("canvas apply client_id is required")
	}
	if len(request.Transactions) != 1 {
		return fmt.Errorf("canvas apply requires exactly one transaction in this beta; put related patches in that transaction")
	}
	transactionIDs := make(map[string]struct{}, len(request.Transactions))
	for txIndex := range request.Transactions {
		tx := &request.Transactions[txIndex]
		tx.TransactionID = strings.TrimSpace(tx.TransactionID)
		if tx.TransactionID == "" {
			return fmt.Errorf("canvas apply transactions[%d].transaction_id is required", txIndex)
		}
		if _, duplicate := transactionIDs[tx.TransactionID]; duplicate {
			return fmt.Errorf("canvas apply transaction_id %q is duplicated", tx.TransactionID)
		}
		transactionIDs[tx.TransactionID] = struct{}{}
		if len(tx.Patches) == 0 {
			return fmt.Errorf("canvas apply transaction %q has no patches", tx.TransactionID)
		}
		for patchIndex := range tx.Patches {
			patch := &tx.Patches[patchIndex]
			patch.AssetID = strings.TrimSpace(patch.AssetID)
			patch.Op = strings.ToLower(strings.TrimSpace(patch.Op))
			if patch.AssetID == "" {
				return fmt.Errorf("canvas apply transaction %q patch[%d].asset_id is required", tx.TransactionID, patchIndex)
			}
			switch patch.Op {
			case "add", "replace":
				if len(patch.Value) == 0 || !json.Valid(patch.Value) {
					return fmt.Errorf("canvas apply transaction %q patch[%d].value must be valid JSON", tx.TransactionID, patchIndex)
				}
			case "remove":
				if len(patch.Value) != 0 && !json.Valid(patch.Value) {
					return fmt.Errorf("canvas apply transaction %q patch[%d].value must be valid JSON", tx.TransactionID, patchIndex)
				}
			default:
				return fmt.Errorf("canvas apply transaction %q patch[%d].op must be add, replace, or remove", tx.TransactionID, patchIndex)
			}
			if patch.BaseAssetVersion != nil && *patch.BaseAssetVersion < 0 {
				return fmt.Errorf("canvas apply transaction %q patch[%d].base_asset_version must not be negative", tx.TransactionID, patchIndex)
			}
		}
	}
	return nil
}

func decodeApplyResults(rawResults []json.RawMessage) ([]decodedPatchTransactionResult, error) {
	results := make([]decodedPatchTransactionResult, 0, len(rawResults))
	for index, rawResult := range rawResults {
		var result PatchTransactionResult
		if err := json.Unmarshal(rawResult, &result); err != nil {
			return nil, fmt.Errorf("decode transaction result[%d]: %w", index, err)
		}
		results = append(results, decodedPatchTransactionResult{result: result, raw: rawResult})
	}
	return results, nil
}

func validateApplyResults(
	expected []PatchTransaction,
	actual []decodedPatchTransactionResult,
	allowNonAcknowledgedResults bool,
) ([]PatchTransactionResult, []json.RawMessage, bool, error) {
	byID := make(map[string]decodedPatchTransactionResult, len(actual))
	for _, decoded := range actual {
		result := decoded.result
		result.TransactionID = strings.TrimSpace(result.TransactionID)
		if result.TransactionID == "" {
			return nil, nil, false, fmt.Errorf("canvas apply returned a result without transaction_id")
		}
		if _, duplicate := byID[result.TransactionID]; duplicate {
			return nil, nil, false, fmt.Errorf("canvas apply returned duplicate result for transaction %q", result.TransactionID)
		}
		decoded.result = result
		byID[result.TransactionID] = decoded
	}
	if len(byID) != len(expected) {
		return nil, nil, false, fmt.Errorf("canvas apply returned %d transaction results, want %d", len(byID), len(expected))
	}
	ordered := make([]PatchTransactionResult, 0, len(expected))
	orderedRaw := make([]json.RawMessage, 0, len(expected))
	hasNonAcknowledgedResult := false
	for _, transaction := range expected {
		decoded, ok := byID[transaction.TransactionID]
		if !ok {
			return nil, nil, false, fmt.Errorf("canvas apply omitted transaction result %q", transaction.TransactionID)
		}
		result := decoded.result
		status := strings.ToLower(strings.TrimSpace(result.Status))
		switch status {
		case "ack":
			for _, patch := range transaction.Patches {
				version, ok := result.AssetVersions[patch.AssetID]
				if !ok {
					if allowNonAcknowledgedResults && patch.Op == "add" && patch.Path == "" {
						continue
					}
					return nil, nil, false, fmt.Errorf("canvas apply transaction %q omitted version for asset %q", transaction.TransactionID, patch.AssetID)
				}
				if version < 0 {
					return nil, nil, false, fmt.Errorf("canvas apply transaction %q returned negative version for asset %q", transaction.TransactionID, patch.AssetID)
				}
			}
		case "reject", "blocked", "skipped":
			if !allowNonAcknowledgedResults {
				return nil, nil, false, fmt.Errorf(
					"canvas apply transaction %q was not acknowledged: status=%q blocked_by=%q error=%q",
					transaction.TransactionID, result.Status, result.BlockedByTransaction, result.Error,
				)
			}
			hasNonAcknowledgedResult = true
		default:
			return nil, nil, false, fmt.Errorf(
				"canvas apply transaction %q returned invalid status %q",
				transaction.TransactionID, result.Status,
			)
		}
		ordered = append(ordered, result)
		orderedRaw = append(orderedRaw, decoded.raw)
	}
	return ordered, orderedRaw, hasNonAcknowledgedResult, nil
}
