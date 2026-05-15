# pippit-cli

Minimal demo CLI for Pippit workflows.

## Novel Run Demo

Install from npm after the package is published:

```bash
npx @pippit-dev/cli@latest install
pippit-cli novel +submit-run --message "写一个赛博朋克小说开头"
pippit-cli novel +upload-file --path ./story.md
pippit-cli novel +get-thread --thread-id thread_mock_123456
```

NPM package names must be lowercase, so the publishable package name is
`@pippit-dev/cli` rather than `@Pippit-dev/cli`.

Submit a Run task for the novel scene:

```bash
go run . novel +submit-run --message "写一个赛博朋克小说开头"
go run . novel +upload-file --path ./story.md
go run . novel +get-thread --thread-id thread_mock_123456
```

`+submit-run` calls `/api/biz/v1/skill/submit_run` and prints `thread_id`,
`run_id`, and `web_thread_link`. `+upload-file` and `+get-thread` are still
mocked while their real service contracts are wired.

## HTTP Client

Command modules should share `internal.Client` for service calls. It supports
JSON `GET` and `POST`, a common base URL, and centralized HTTP error handling.
Set `PIPPIT_CLI_BASE_URL` to override the default `https://xyq.jianying.com`.
