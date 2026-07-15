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

func TestImageFileNameUsesAssetID(t *testing.T) {
	got := imageFileName(queryImage{
		AssetID:  "7659311708893512254",
		Metadata: queryImageMeta{Format: "jpeg"},
	}, 1)
	want := "7659311708893512254.jpeg"
	if got != want {
		t.Fatalf("imageFileName() = %q, want %q", got, want)
	}
}

func TestImageFileNameAddsExtensionFromFormat(t *testing.T) {
	got := imageFileName(queryImage{
		AssetID:  "pic1",
		Metadata: queryImageMeta{Format: "png"},
	}, 1)
	if got != "pic1.png" {
		t.Fatalf("imageFileName() = %q, want pic1.png", got)
	}
}

func TestImageFileNameFallsBackToPngWhenFormatEmpty(t *testing.T) {
	got := imageFileName(queryImage{
		AssetID:  "pic1",
		Metadata: queryImageMeta{Format: ""},
	}, 1)
	if got != "pic1.png" {
		t.Fatalf("imageFileName() = %q, want pic1.png", got)
	}
}

func TestImageFileNameNormalizesMimeTypeFormat(t *testing.T) {
	cases := []struct {
		format string
		want   string
	}{
		{format: "image/jpeg", want: "pic1.jpeg"},
		{format: ".jpeg", want: "pic1.jpeg"},
		{format: "JPEG", want: "pic1.jpeg"},
		{format: "image/png", want: "pic1.png"},
		{format: "image/webp", want: "pic1.webp"},
		{format: "image/svg+xml", want: "pic1.png"}, // unsupported, falls back
		{format: "application/octet-stream", want: "pic1.png"},
	}
	for _, tt := range cases {
		t.Run(tt.format, func(t *testing.T) {
			got := imageFileName(queryImage{AssetID: "pic1", Metadata: queryImageMeta{Format: tt.format}}, 1)
			if got != tt.want {
				t.Fatalf("imageFileName(format=%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestImageFileNameUsesResultIndexWhenNoID(t *testing.T) {
	got := imageFileName(queryImage{
		Metadata: queryImageMeta{Format: "jpeg"},
	}, 2)
	if got != "result_2.jpeg" {
		t.Fatalf("imageFileName() = %q, want result_2.jpeg", got)
	}
}

func TestImageFileNameKeepsExistingImageExtension(t *testing.T) {
	got := imageFileName(queryImage{
		AssetID:  "cat_poster.png",
		Metadata: queryImageMeta{Format: "jpeg"},
	}, 1)
	if got != "cat_poster.png" {
		t.Fatalf("imageFileName() = %q, want cat_poster.png", got)
	}
}

func TestExtractQueryImagesFiltersBySubType(t *testing.T) {
	run := queryRun{
		EntryList: []queryEntry{
			{Artifact: queryArtifact{Content: []queryContent{
				{SubType: "biz/x_data_image", Data: queryContentData{Image: &queryImage{DownloadURL: "https://x/a.jpeg", AssetID: "p1"}}},
				{SubType: "biz/x_data_video", Data: queryContentData{Video: &queryVideo{DownloadURL: "https://x/v.mp4", VID: "v1"}}},
			}}},
			{Artifact: queryArtifact{Content: []queryContent{
				{SubType: "biz/x_data_image", Data: queryContentData{Image: &queryImage{DownloadURL: "https://x/b.png", AssetID: "p2"}}},
				{SubType: "text/plain", Data: queryContentData{}},
			}}},
		},
	}
	got := extractQueryImages(run)
	if len(got) != 2 {
		t.Fatalf("extractQueryImages() = %d images, want 2", len(got))
	}
	if got[0].AssetID != "p1" || got[1].AssetID != "p2" {
		t.Fatalf("extractQueryImages() = %q,%q, want p1,p2", got[0].AssetID, got[1].AssetID)
	}
}
