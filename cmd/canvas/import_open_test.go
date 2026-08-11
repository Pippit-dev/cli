package canvas

import (
	"reflect"
	"testing"
)

func TestSanitizedCanvasOpenEnvRemovesAccessKeys(t *testing.T) {
	input := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"XYQ_ACCESS_KEY=xyq-secret",
		"PIPPIT_ACCESS_KEY=pippit-secret",
		"PIPPIT_AK=legacy-secret",
		"PIPPIT_CLI_PPE_ENV=ppe_cli_canvas_ak",
	}
	want := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"PIPPIT_CLI_PPE_ENV=ppe_cli_canvas_ak",
	}

	if got := sanitizedCanvasOpenEnv(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitizedCanvasOpenEnv() = %v, want %v", got, want)
	}
}
