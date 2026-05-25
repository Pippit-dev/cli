# pippit-cli

Minimal demo CLI for Pippit workflows.

## Short Drama Run Demo

Install from npm after the package is published:

```bash
npx @pippit-dev/cli@latest install
export XYQ_ACCESS_KEY="<access-key>"
pippit-cli short-drama +submit-run --message "写一个赛博朋克短剧开头"
pippit-cli short-drama +upload-file --path ./story.md
pippit-cli short-drama +get-thread --thread-id thread_123 --run-id run_456 --after-seq 0
pippit-cli short-drama +download-result --output-dir ./thread_123/results/result.mp4 --url URL
```

NPM package names must be lowercase, so the publishable package name is
`@pippit-dev/cli` rather than `@Pippit-dev/cli`.

Submit a Run task for the short drama scene:

```bash
export XYQ_ACCESS_KEY="<access-key>"
go run . short-drama +submit-run --message "写一个赛博朋克短剧开头"
go run . short-drama +upload-file --path ./story.md
go run . short-drama +get-thread --thread-id thread_123 --run-id run_456 --after-seq 0
go run . short-drama +download-result --output-dir ./thread_123/results/result.mp4 --url URL
```

`+submit-run` calls `/api/biz/v1/skill/submit_run` and prints `thread_id`,
`run_id`, and `web_thread_link`; `--message` is required.
`+get-thread` calls
`/api/biz/v1/skill/get_thread` and prints extracted `messages`.
`+download-result` downloads the result URL to
the `--output-dir` file path. `+upload-file` is still mocked while
its real service contract is wired.

## HTTP Client

Command modules should receive `common.Runner` for service calls. Runtime
settings such as base URL, HTTP timeout, and API paths are loaded by
`internal/config` and paired with `common.Client` in the runner.

## Auth

`short-drama +submit-run` and `short-drama +get-thread` authenticate with
`Authorization: Bearer <XYQ_ACCESS_KEY>`. OAuth command code remains available,
but runtime short drama requests do not use it.
