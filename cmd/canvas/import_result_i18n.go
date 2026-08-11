package canvas

import "github.com/Pippit-dev/pippit-cli/internal/canvasplan"

// localizeCanvasImportResult keeps the provider-neutral executor contract
// unchanged while making the import command's user-facing warning Chinese.
func localizeCanvasImportResult(result *canvasplan.ExecutionResult) {
	if result == nil || result.Warning == "" {
		return
	}
	if result.Verification != nil && result.Verification.Verified && result.Verification.RecoveredFromQuery {
		result.Warning = "画布写入响应状态不明确，但已通过精确回读找回并校验全部资产；CLI 未重复提交写入。"
		return
	}
	switch result.State {
	case canvasplan.StateVerified:
		result.Warning = "画布已通过历史验证；当前授权仍可访问，CLI 未覆盖用户后续编辑，也未重复提交写入。"
	case canvasplan.StateCreatePending:
		result.Warning = "漫剧画布创建请求已受理，服务端仍在处理中；断点已保存，续跑不会重复创建项目。"
	case canvasplan.StateCreateAmbiguous:
		result.Warning = "漫剧画布创建结果暂不明确；已保存安全断点，请使用同一断点恢复，切勿重新创建。"
	case canvasplan.StateApplyAmbiguous:
		result.Warning = "画布写入结果暂不明确；已保存安全断点，恢复时只会回读核对，不会盲目重放写入。"
	case canvasplan.StateVerificationFailed:
		result.Warning = "画布写入后的回读验证尚未完成；已保存安全断点，CLI 不会盲目重放写入。"
	default:
		result.Warning = "画布导入已保存安全断点；请排除当前错误后使用同一断点继续。"
	}
}
