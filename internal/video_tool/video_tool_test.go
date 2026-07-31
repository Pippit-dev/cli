package video_tool

import (
	"strings"
	"testing"
)

func TestValidateSuperResolutionOptions(t *testing.T) {
	tests := []struct {
		name         string
		opts         *SuperResolutionOptions
		wantContains string
	}{
		{name: "valid", opts: &SuperResolutionOptions{VideoPath: "source.mp4", OutputResolution: "1080p"}},
		{name: "missing video", opts: &SuperResolutionOptions{OutputResolution: "1080p"}, wantContains: "--video"},
		{name: "missing output resolution", opts: &SuperResolutionOptions{VideoPath: "source.mp4"}, wantContains: "--output-resolution"},
		{name: "unsupported extension", opts: &SuperResolutionOptions{VideoPath: "source.txt", OutputResolution: "1080p"}, wantContains: "不支持的视频文件后缀"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSuperResolutionOptions(tt.opts)
			if tt.wantContains == "" {
				if err != nil {
					t.Fatalf("ValidateSuperResolutionOptions() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("ValidateSuperResolutionOptions() error = %v, want containing %q", err, tt.wantContains)
			}
		})
	}
}

func TestValidateEraseSubtitleOptions(t *testing.T) {
	if err := ValidateEraseSubtitleOptions(&EraseSubtitleOptions{VideoPath: "source.webm"}); err != nil {
		t.Fatalf("ValidateEraseSubtitleOptions() error = %v, want nil", err)
	}
	err := ValidateEraseSubtitleOptions(&EraseSubtitleOptions{})
	if err == nil || !strings.Contains(err.Error(), "--video") {
		t.Fatalf("ValidateEraseSubtitleOptions() error = %v, want --video validation", err)
	}
}

func TestSuperResolutionSemanticValuesRemainServiceOwned(t *testing.T) {
	if err := ValidateSuperResolutionOptions(&SuperResolutionOptions{
		VideoPath:        "source.mp4",
		ToolVersion:      "future_version",
		OutputResolution: "future_resolution",
	}); err != nil {
		t.Fatalf("ValidateSuperResolutionOptions() error = %v, want service-owned enum validation", err)
	}
}
