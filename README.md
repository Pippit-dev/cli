# pippit-cli

Minimal demo CLI for Pippit workflows.

## Novel Run Demo

Install from npm after the package is published:

```bash
npx @pippit-dev/cli@latest install
pippit-cli novel +submit-run --prompt "写一个赛博朋克小说开头" --title "霓虹黎明"
pippit-cli novel +upload-file --path ./story.md
pippit-cli novel +get-thread --thread-id thread_mock_123456
```

NPM package names must be lowercase, so the publishable package name is
`@pippit-dev/cli` rather than `@Pippit-dev/cli`.

Submit a mocked Run task for the novel scene:

```bash
go run . novel +submit-run --prompt "写一个赛博朋克小说开头" --title "霓虹黎明"
go run . novel +upload-file --path ./story.md
go run . novel +get-thread --thread-id thread_mock_123456
```

The command prints a JSON envelope with `scene`, `thread_id`, `run_id`, `status`,
and the submitted request. The current submitter is intentionally mocked; replace
`internal/novel.MockClient` with a real client when the Pippit API contract is
ready.

## HTTP Client

Command modules should share `internal/client.Client` for service calls. It
supports JSON `GET` and `POST`, a common base URL, and centralized HTTP error
handling. Set `PIPPIT_CLI_BASE_URL` when wiring a real service endpoint.
