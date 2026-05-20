# pippit-cli

Minimal demo CLI for Pippit workflows.

## Novel Run Demo

Install from npm after the package is published:

```bash
npx @pippit-dev/cli@latest install
export XYQ_ACCESS_KEY="<access-key>"
pippit-cli novel +submit-run --message "写一个赛博朋克小说开头"
pippit-cli novel +upload-file --path ./story.md
pippit-cli novel +get-thread --thread-id thread_123 --run-id run_456 --after-seq 0
```

NPM package names must be lowercase, so the publishable package name is
`@pippit-dev/cli` rather than `@Pippit-dev/cli`.

Submit a Run task for the novel scene:

```bash
export XYQ_ACCESS_KEY="<access-key>"
go run . novel +submit-run --message "写一个赛博朋克小说开头"
go run . novel +upload-file --path ./story.md
go run . novel +get-thread --thread-id thread_123 --run-id run_456 --after-seq 0
```

`+submit-run` calls `/api/biz/v1/skill/submit_run` and prints `thread_id`,
`run_id`, and `web_thread_link`. `+get-thread` calls
`/api/biz/v1/skill/get_thread` and prints extracted `messages`. `+upload-file`
is still mocked while its real service contract is wired.

## HTTP Client

Command modules should receive `common.Runner` for service calls. Runtime
settings such as base URL, HTTP timeout, and API paths are loaded by
`internal/config` and paired with `common.Client` in the runner.

## Auth

`novel +submit-run` and `novel +get-thread` authenticate with
`Authorization: Bearer <XYQ_ACCESS_KEY>`. OAuth command code remains available,
but runtime novel requests do not use it.
