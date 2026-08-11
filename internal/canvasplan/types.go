package canvasplan

import (
	"encoding/json"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/canvas"
)

const (
	PlanSchema          = "pippit-canvas-plan/0.1"
	ResolvedMediaSchema = "pippit-canvas-resolved-media/0.1"
	JournalSchema       = "pippit-canvas-execution-journal/0.1"
)

const (
	StateInitialized          = "initialized"
	StateCreateRequested      = "create-requested"
	StateCreatePending        = "create-pending"
	StateCreateAmbiguous      = "create-ambiguous"
	StateCreateFailed         = "create-failed"
	StateRootReady            = "root-ready"
	StateAllocationRequested  = "allocation-requested"
	StateAllocated            = "allocated"
	StateMaterialized         = "materialized"
	StateApplyPrepared        = "apply-prepared"
	StateApplyRequested       = "apply-requested"
	StateApplyAcknowledged    = "apply-acknowledged"
	StateApplyAmbiguous       = "apply-ambiguous"
	StateVerificationFailed   = "verification-failed"
	StateUnsafePartial        = "unsafe-partial-apply"
	StateUnsafeRootChanged    = "unsafe-root-changed"
	StateMaterializationDrift = "unsafe-materialization-drift"
	StateVerified             = "verified"
)

type Plan struct {
	Schema        string             `json:"schema"`
	Title         string             `json:"title"`
	Source        Source             `json:"source"`
	RequiredMedia []MediaRequirement `json:"required_media"`
	Nodes         []Node             `json:"nodes"`
	Groups        []Group            `json:"groups"`
	Edges         []Edge             `json:"edges"`
	Degradations  []json.RawMessage  `json:"degradations,omitempty"`
}

type Source struct {
	Provider    string `json:"provider"`
	ProjectID   string `json:"project_id"`
	Fingerprint string `json:"fingerprint"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type MediaMetadata struct {
	ByteSize   *int64 `json:"byte_size,omitempty"`
	DurationMS *int64 `json:"duration_ms,omitempty"`
	Extension  string `json:"extension,omitempty"`
	Height     *int64 `json:"height,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	Width      *int64 `json:"width,omitempty"`
}

type MediaRequirement struct {
	LogicalID    string        `json:"logical_id"`
	SourceNodeID string        `json:"source_node_id"`
	FileName     string        `json:"file_name"`
	MediaType    string        `json:"media_type"`
	URL          string        `json:"url,omitempty"`
	LocalPath    string        `json:"local_path,omitempty"`
	SHA256       string        `json:"sha256,omitempty"`
	Metadata     MediaMetadata `json:"metadata,omitempty"`
}

type Node struct {
	LogicalID            string   `json:"logical_id"`
	SourceNodeID         string   `json:"source_node_id"`
	Title                string   `json:"title"`
	Position             Position `json:"position"`
	Size                 Size     `json:"size"`
	ParentGroupLogicalID string   `json:"parent_group_logical_id,omitempty"`
	Order                int      `json:"order"`
	Kind                 string   `json:"kind"`
	TargetType           string   `json:"target_type"`
	MediaLogicalID       string   `json:"media_logical_id,omitempty"`
	Variant              string   `json:"variant,omitempty"`
	InputNodeLogicalIDs  []string `json:"input_node_logical_ids,omitempty"`
}

type Group struct {
	LogicalID       string   `json:"logical_id"`
	SourceNodeID    string   `json:"source_node_id"`
	Title           string   `json:"title"`
	Position        Position `json:"position"`
	Size            Size     `json:"size"`
	Order           int      `json:"order"`
	ChildLogicalIDs []string `json:"child_logical_ids"`
}

type Edge struct {
	LogicalID           string `json:"logical_id"`
	SourceEdgeID        string `json:"source_edge_id"`
	Type                string `json:"type"`
	SourceNodeLogicalID string `json:"source_node_logical_id"`
	TargetNodeLogicalID string `json:"target_node_logical_id"`
	SourceHandle        string `json:"source_handle"`
	TargetHandle        string `json:"target_handle"`
}

type ResolvedMediaSet struct {
	Schema string          `json:"schema"`
	Media  []ResolvedMedia `json:"media"`
}

type ResolvedMedia struct {
	LogicalID     string `json:"logical_id"`
	MediaType     string `json:"media_type"`
	AssetID       string `json:"asset_id"`
	PippitAssetID string `json:"pippit_asset_id"`
}

type Document struct {
	Revision     int                        `json:"revision"`
	RootCanvasID string                     `json:"rootCanvasId"`
	Assets       map[string]json.RawMessage `json:"assets"`
}

type Verification struct {
	ExpectedAssetCount   int      `json:"expected_asset_count"`
	ReturnedAssetCount   int      `json:"returned_asset_count"`
	MissingAssetIDs      []string `json:"missing_asset_ids,omitempty"`
	UnverifiableAssetIDs []string `json:"unverifiable_asset_ids,omitempty"`
	MismatchedAssetIDs   []string `json:"mismatched_asset_ids,omitempty"`
	Verified             bool     `json:"verified"`
	RecoveredFromQuery   bool     `json:"recovered_from_query,omitempty"`
	LogID                string   `json:"log_id,omitempty"`
}

type ExecuteOptions struct {
	JournalPath  string
	PollInterval time.Duration
	WaitTimeout  time.Duration
}

type ExecutionResult struct {
	State                 string        `json:"state"`
	JournalPath           string        `json:"journal_path"`
	OperationID           string        `json:"operation_id"`
	ProjectID             string        `json:"project_id,omitempty"`
	RootCanvasID          string        `json:"root_canvas_id,omitempty"`
	OverviewPippitAssetID string        `json:"overview_pippit_asset_id,omitempty"`
	WebURL                string        `json:"web_url,omitempty"`
	DocumentSHA256        string        `json:"document_sha256,omitempty"`
	AssetCount            int           `json:"asset_count,omitempty"`
	NodeCount             int           `json:"node_count,omitempty"`
	EdgeCount             int           `json:"edge_count,omitempty"`
	DegradationCount      int           `json:"degradation_count,omitempty"`
	TransactionID         string        `json:"transaction_id,omitempty"`
	Verification          *Verification `json:"verification,omitempty"`
	Warning               string        `json:"warning,omitempty"`
}

type Journal struct {
	Schema              string               `json:"schema"`
	OperationID         string               `json:"operation_id"`
	RequestID           string               `json:"request_id"`
	PlanSHA256          string               `json:"plan_sha256"`
	ResolvedMediaSHA256 string               `json:"resolved_media_sha256"`
	State               string               `json:"state"`
	CreatedAt           string               `json:"created_at"`
	UpdatedAt           string               `json:"updated_at"`
	Create              *canvas.CreateResult `json:"create,omitempty"`
	NodeAssetIDs        map[string]string    `json:"node_asset_ids,omitempty"`
	AllocationLogID     string               `json:"allocation_log_id,omitempty"`
	DocumentSHA256      string               `json:"document_sha256,omitempty"`
	AssetSHA256         map[string]string    `json:"asset_sha256,omitempty"`
	Apply               *ApplyJournal        `json:"apply,omitempty"`
	Verification        *Verification        `json:"verification,omitempty"`
	LastError           string               `json:"last_error,omitempty"`
}

type ApplyJournal struct {
	TransactionID   string           `json:"transaction_id"`
	BatchID         string           `json:"batch_id"`
	ClientID        string           `json:"client_id"`
	BaseRootVersion int64            `json:"base_root_version"`
	RequestSHA256   string           `json:"request_sha256"`
	Status          string           `json:"status"`
	AssetVersions   map[string]int64 `json:"asset_versions,omitempty"`
	LogID           string           `json:"log_id,omitempty"`
}
