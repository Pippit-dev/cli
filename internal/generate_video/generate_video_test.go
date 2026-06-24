package generate_video

import "testing"

func TestValidateOptionsDoesNotRejectReferenceMediaCounts(t *testing.T) {
	opts := &Options{
		Prompt:     "x",
		ImagePaths: mediaPaths("image", ".jpg", 10),
		VideoPaths: mediaPaths("video", ".mp4", 4),
		AudioPaths: mediaPaths("audio", ".mp3", 4),
	}

	if err := ValidateOptions(opts); err != nil {
		t.Fatalf("ValidateOptions() error = %v, want nil", err)
	}
}

func mediaPaths(prefix string, ext string, count int) []string {
	paths := make([]string, 0, count)
	for i := 0; i < count; i++ {
		paths = append(paths, prefix+string(rune('a'+i))+ext)
	}
	return paths
}
