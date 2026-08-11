# LibTV canvas adapter

This directory is a provider adapter, not a Pippit API client. It converts a
LibTV snapshot into the ID-neutral `pippit-canvas-plan/0.1` contract. It does
not read an access key, choose a Pippit environment, allocate Pippit asset IDs,
upload files, create a project, write assets, bind a canvas, or use team state.

Generate a plan:

```bash
node adapters/libtv/cli.mjs plan \
  --snapshot ./libtv-snapshot.json \
  --media-manifest ./bundle-media.json \
  --title "My imported canvas" \
  --output ./canvas-plan.json
```

`--media-manifest` is optional. It may provide `sourceNodeId` + `fileName` rows
for a local export bundle. Existing prototype manifests may also contain
Pippit IDs or authorization metadata; the adapter deliberately ignores those
fields and never copies them into the plan. If no manifest row exists, the
adapter uses the snapshot's HTTPS media reference and derives a file name.

The generated plan is written with mode `0600`. When the source export only
contains signed HTTPS media URLs, those URLs (including their query strings)
must remain in the local plan so a later executor can download the files. Treat
the plan as sensitive local state: do not print it into logs, commit it, or
share it. Prefer a local export bundle plus `--media-manifest` when available.

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
Supported source types are `group`, `video`, `audio`, and `video-clip`.

LibTV `video-clip` nodes do not carry a portable generated result. The plan
preserves their input references and records an explicit degradation to an
empty Pippit `video-composite` placeholder.
