package generate_video

import "testing"

func TestVideoFileNameUsesVIDBeforeTimestampTitle(t *testing.T) {
	got := videoFileName(queryVideo{
		Title: "v03c76g10004d8jp38iljhtepa11k25g_2026-06-09T120814.516",
		VID:   "v03c76g10004d8jp38iljhtepa11k25g",
	}, 1)
	want := "v03c76g10004d8jp38iljhtepa11k25g.mp4"
	if got != want {
		t.Fatalf("videoFileName() = %q, want %q", got, want)
	}
}

func TestVideoFileNameAddsMP4ForTimestampTitleWithoutVID(t *testing.T) {
	got := videoFileName(queryVideo{
		Title: "v03c76g10004d8jp38iljhtepa11k25g_2026-06-09T120814.516",
	}, 1)
	want := "v03c76g10004d8jp38iljhtepa11k25g_2026-06-09T120814.516.mp4"
	if got != want {
		t.Fatalf("videoFileName() = %q, want %q", got, want)
	}
}

func TestVideoFileNameKeepsVideoExtension(t *testing.T) {
	got := videoFileName(queryVideo{Title: "cat_video.mp4"}, 1)
	if got != "cat_video.mp4" {
		t.Fatalf("videoFileName() = %q, want cat_video.mp4", got)
	}
}
