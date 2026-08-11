package canvas

import (
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/canvasplan"
)

func TestLocalizeCanvasImportResultRecoveredApply(t *testing.T) {
	result := &canvasplan.ExecutionResult{
		State:   canvasplan.StateVerified,
		Warning: "Canvas apply response was ambiguous",
		Verification: &canvasplan.Verification{
			Verified:           true,
			RecoveredFromQuery: true,
		},
	}

	localizeCanvasImportResult(result)

	want := "画布写入响应状态不明确，但已通过精确回读找回并校验全部资产；CLI 未重复提交写入。"
	if result.Warning != want {
		t.Fatalf("warning = %q, want %q", result.Warning, want)
	}
}

func TestLocalizeCanvasImportResultRecoverableStates(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  string
	}{
		{name: "create pending", state: canvasplan.StateCreatePending, want: "漫剧画布创建请求已受理，服务端仍在处理中；断点已保存，续跑不会重复创建项目。"},
		{name: "create ambiguous", state: canvasplan.StateCreateAmbiguous, want: "漫剧画布创建结果暂不明确；已保存安全断点，请使用同一断点恢复，切勿重新创建。"},
		{name: "apply ambiguous", state: canvasplan.StateApplyAmbiguous, want: "画布写入结果暂不明确；已保存安全断点，恢复时只会回读核对，不会盲目重放写入。"},
		{name: "verification failed", state: canvasplan.StateVerificationFailed, want: "画布写入后的回读验证尚未完成；已保存安全断点，CLI 不会盲目重放写入。"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := &canvasplan.ExecutionResult{State: test.state, Warning: "internal warning"}
			localizeCanvasImportResult(result)
			if result.Warning != test.want {
				t.Fatalf("warning = %q, want %q", result.Warning, test.want)
			}
		})
	}
}

func TestLocalizeCanvasImportResultLeavesEmptyWarningAlone(t *testing.T) {
	result := &canvasplan.ExecutionResult{State: canvasplan.StateVerified}
	localizeCanvasImportResult(result)
	if result.Warning != "" {
		t.Fatalf("warning = %q, want empty", result.Warning)
	}
}
