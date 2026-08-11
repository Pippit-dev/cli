#!/usr/bin/env node

import { chmod, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

import { convertSnapshotToCanvasPlan } from './plan.mjs';

function parseArgs(argv) {
  const [command, ...rest] = argv;
  const args = { command };
  for (let index = 0; index < rest.length; index += 1) {
    const key = rest[index];
    const value = rest[index + 1];
    if (!key?.startsWith('--') || !value || value.startsWith('--')) {
      throw new Error(`invalid argument near ${key ?? '<end>'}`);
    }
    args[key.slice(2)] = value;
    index += 1;
  }
  return args;
}

function required(args, key) {
  const value = args[key]?.trim();
  if (!value) throw new Error(`--${key} is required`);
  return value;
}

async function readJson(path) {
  const text = await readFile(resolve(path), 'utf8');
  try {
    return JSON.parse(text);
  } catch (error) {
    throw new Error(`${path} is not valid JSON: ${error instanceof Error ? error.message : String(error)}`);
  }
}

async function runPlan(args) {
  const snapshot = await readJson(required(args, 'snapshot'));
  const mediaManifest = args['media-manifest'] ? await readJson(args['media-manifest']) : undefined;
  const plan = convertSnapshotToCanvasPlan(snapshot, { mediaManifest, title: args.title });
  const serialized = `${JSON.stringify(plan, null, 2)}\n`;
  const output = required(args, 'output');
  if (output === '-') {
    process.stdout.write(serialized);
    return;
  }
  const outputPath = resolve(output);
  await writeFile(outputPath, serialized, { mode: 0o600 });
  await chmod(outputPath, 0o600);
  process.stdout.write(`${JSON.stringify({
    output: outputPath,
    schema: plan.schema,
    source: plan.source,
    media_count: plan.required_media.length,
    node_count: plan.nodes.length,
    group_count: plan.groups.length,
    edge_count: plan.edges.length,
    degradation_count: plan.degradations.length,
  })}\n`);
}

async function main(argv = process.argv.slice(2)) {
  const args = parseArgs(argv);
  if (args.command !== 'plan') {
    throw new Error(
      'usage: node adapters/libtv/cli.mjs plan --snapshot <snapshot.json> ' +
        '[--media-manifest <manifest.json>] [--title <title>] --output <plan.json|->',
    );
  }
  await runPlan(args);
}

export { main, parseArgs };

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  });
}
