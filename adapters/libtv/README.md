# LibTV canvas adapter

This directory is a provider adapter, not a Pippit API client. It converts a
LibTV snapshot into the ID-neutral `pippit-canvas-plan/0.1` contract. It does
not read an access key, choose a Pippit environment, allocate Pippit asset IDs,
upload files, create a project, write assets, bind a canvas, or use team state.

Export a LibTV URL with the official LibTV CLI, then generate a plan:

```bash
node adapters/libtv/cli.mjs export \
  --url 'https://www.liblib.tv/canvas?projectId=<project-id>' \
  --output-dir ./libtv-bundle
```

Before an import starts reading a project or downloading media, callers can
run the independent authentication preflight:

```bash
node adapters/libtv/cli.mjs auth
```

This command only prepares the verified LibTV CLI and runs `account info`. If
the account is not authenticated, it runs `login web --open` and verifies the
account again. It never receives or reads a project URL, node, or media file.
Progress and browser-login output go to stderr; successful stdout remains one
JSON object using `pippit-libtv-auth-result/0.1`. `--non-interactive` reports
`AUTH_REQUIRED` without trying to read any project data, so an orchestration
layer can retry after arranging authentication.

An explicit `--libtv-cli`, `LIBTV_CLI_BINARY`, or `LIBTV_CLI_PATH` opts into a
user-managed binary after a version check. Without an explicit override, the
exporter never executes `libtv` from `PATH` or `~/.libtv`; it goes directly to
the verified cache/bootstrap path. This prevents an ambient binary from
bypassing the pin. Bootstrap installs the pinned official 1.1.3 ZIP into the
private Pippit tool cache and supports
Darwin, Linux, and Windows on arm64/x64; it verifies both ZIP and binary against
embedded SHA-256 values, uses `0700` directories/binary, and never executes a
remote installer or script. Windows extraction uses the built-in `tar.exe`
(Windows 10+); absence of a safe local extractor fails closed. Set
`PIPPIT_CLI_LIBTV_CACHE_DIR` to override the cache root.

After locating a verified CLI, the exporter probes existing official CLI
credentials with `libtv account info`. If none are available, interactive use
runs `libtv login web --open`; `--non-interactive` instead fails with an
actionable login message. Login child output is redirected to stderr so stdout
remains one machine-readable JSON object. Project, node-detail, media-download,
and final count progress also goes to stderr. Phase lines precede CLI setup,
authentication, and project fetch; every media download emits a start line
before the potentially long transfer. The adapter never reads browser
cookies or the LibTV credential file. The LibTV child receives only a small
runtime/login environment allowlist. Unknown variables, SSH agent sockets, and
all unlisted key/token/password values are omitted. HTTP/HTTPS/SOCKS proxy URLs
are passed only when they contain no user information.

The output directory must not already exist. Export is staged privately and
renamed atomically, so cancellation, permission denial, or a partial media
download leaves no final bundle. The successful stdout object uses
`pippit-libtv-export-result/0.1` and returns `plan_path`, `snapshot_path`,
`media_manifest_path`, and absolute local paths for each media item.

The bundle contains:

- a URL- and credential-sanitized `snapshot.json`;
- `media-manifest.json` (`pippit-libtv-media-manifest/0.1`) with bundle-relative
  paths, byte sizes, and bare lowercase SHA-256 digests;
- local files downloaded through official `libtv download`, preserving LibTV's
  source-account permission and watermark behavior;
- `plan.json` (`pippit-canvas-plan/0.1`).

To convert an existing snapshot instead:

```bash
node adapters/libtv/cli.mjs plan \
  --snapshot ./libtv-snapshot.json \
  --media-manifest ./bundle-media.json \
  --title "My imported canvas" \
  --output ./canvas-plan.json
```

`--media-manifest` is optional. It may provide `sourceNodeId` + `fileName` rows
for an older export, or `source_node_id`, `relative_path`, `sha256`, and
`media_type` rows for a local bundle. Existing prototype manifests may also
contain Pippit IDs or authorization metadata; the adapter deliberately ignores
those fields and never copies them into the plan.

The generated plan is written with mode `0600`. Official URL export always
uses bundle-relative `local_path` + `sha256` and omits source URLs. Legacy
snapshot-only conversion may still accept an absolute HTTPS media URL, but its
fingerprint strips query strings and authentication fields.

The generic canvas executor owns the remaining steps:

1. resolve/download and upload each `required_media` item;
2. create a personal novel canvas;
3. allocate Pippit IDs and materialize the logical nodes/edges/groups;
4. apply a provider-neutral `canvas.write` transaction;
5. query the assets back and verify them.

Plan IDs (`node:*`, `group:*`, `edge:*`, and `media:*`) are logical and stable
within the source snapshot. They are never Pippit asset IDs. The executor must
allocate new personal asset IDs and keep the logical-to-Pippit mapping in its
resume journal. A plan deliberately has no creation timestamp, so the same
snapshot and media mapping produce byte-for-byte stable JSON; the executor
should hash the complete plan when deriving its operation identity.

The v0.1 adapter fails closed for unsupported node types or dangling edges.
Supported source types are `group`, `image`, `video`, `audio`, and
`video-clip`. Empty LibTV image/video generation nodes are preserved as
`image-placeholder` / `video-placeholder` with an explicit degradation; they
are not mistaken for partially downloaded media.

LibTV `video-clip` nodes do not carry a portable generated result. The plan
preserves their input references and records an explicit degradation to an
empty Pippit `video-composite` placeholder.
