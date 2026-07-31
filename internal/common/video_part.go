package common

// MediaAsset references one uploaded Pippit media asset in submit_run requests.
type MediaAsset struct {
	PippitAssetID string `json:"pippit_asset_id"`
}

// VideoPartToolParam is the shared request payload for video-part capabilities.
type VideoPartToolParam struct {
	Images        []*MediaAsset       `json:"images,omitempty"`
	Prompt        *string             `json:"prompt,omitempty"`
	DurationSec   *int                `json:"duration_sec,omitempty"`
	Ratio         string              `json:"ratio,omitempty"`
	Videos        []*MediaAsset       `json:"videos,omitempty"`
	Audios        []*MediaAsset       `json:"audios,omitempty"`
	Model         string              `json:"model,omitempty"`
	Resolution    string              `json:"resolution,omitempty"`
	GenerateType  *int64              `json:"generate_type,omitempty"`
	MiniToolParam *VideoMiniToolParam `json:"mini_tool_param,omitempty"`
}

// VideoMiniToolParam identifies a video mini tool and carries its request payload.
type VideoMiniToolParam struct {
	ToolName  string                `json:"tool_name"`
	ToolParam *VideoMiniToolPayload `json:"tool_param"`
}

// VideoMiniToolPayload contains the request payload for one selected video mini tool.
type VideoMiniToolPayload struct {
	VideoSuperResolutionToolParam *VideoSuperResolutionToolParam `json:"video_super_resolution_tool_param,omitempty"`
	EraseVideoSubtitleToolParam   *EraseVideoSubtitleToolParam   `json:"erase_video_subtitle_tool_param,omitempty"`
}

// VideoSuperResolutionToolParam is the request payload for video super-resolution.
type VideoSuperResolutionToolParam struct {
	ToolVersion      string      `json:"tool_version,omitempty"`
	Video            *MediaAsset `json:"video"`
	OutputResolution string      `json:"output_resolution"`
}

// EraseVideoSubtitleToolParam is the request payload for subtitle removal.
type EraseVideoSubtitleToolParam struct {
	Video *MediaAsset `json:"video"`
}
