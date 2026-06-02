# pippit-tool-cli

Minimal demo CLI for Pippit workflows.

## Short Drama Run Demo

Install from npm after the package is published. The installer downloads the
matching prebuilt binary for macOS, Linux, or Windows:

```bash
npx @pippit-dev/cli@latest install
export XYQ_ACCESS_KEY="<access-key>"
pippit-tool-cli --version
pippit-tool-cli short-drama +submit-run --message "写一个赛博朋克短剧开头"
pippit-tool-cli short-drama +upload-file --path ./reference.doc
pippit-tool-cli short-drama +get-thread --thread-id thread_123 --run-id run_456
pippit-tool-cli short-drama +download-result --output-path ./thread_123/results/result.mp4 --url URL
```

NPM package names must be lowercase, so the publishable package name is
`@pippit-dev/cli` rather than `@Pippit-dev/cli`.

Submit a Run task for the short drama scene:

```bash
export XYQ_ACCESS_KEY="<access-key>"
go run . --version
go run . short-drama +submit-run --message "写一个赛博朋克短剧开头"
go run . short-drama +upload-file --path ./reference.doc
go run . short-drama +get-thread --thread-id thread_123 --run-id run_456
go run . short-drama +download-result --output-path ./thread_123/results/result.mp4 --url URL
```

`+submit-run` calls `/api/biz/v1/skill/submit_run` and prints `thread_id`,
`run_id`, and `web_thread_link`; `--message` is required.
`+get-thread` calls
`/api/biz/v1/skill/get_thread` with `version=v2` and prints `readable_text`.
`+upload-file` calls `/api/biz/v1/skill/upload_file` with
`multipart/form-data` and prints the returned `asset_id`.
Only `.doc`, `.docx`, and `.txt` file extensions are supported.
`+download-result` downloads the result URL to
the `--output-path` file path.

Short drama command errors are appended to a daily local log file under
`~/.pippit_tool_cli/logs/yyyy-mm-dd.log`. The path is built with the current
user home directory and the platform path separator, so it works on macOS,
Linux, and Windows.

## HTTP Client

Command modules should receive `common.Runner` for service calls. Runtime
settings such as base URL, HTTP timeout, and API paths are loaded by
`internal/config` and paired with `common.Client` in the runner.

## Auth

`short-drama +submit-run`, `short-drama +get-thread`, and `short-drama +upload-file` authenticate with
`Authorization: Bearer <XYQ_ACCESS_KEY>`. OAuth command code remains available,
but runtime short drama requests do not use it.
