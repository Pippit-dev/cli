#!/usr/bin/env node

import { chmod, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

import { exportLibTVURL, preflightLibTVAuth } from './exporter.mjs';
import { convertSnapshotToCanvasPlan } from './plan.mjs';

const BOOLEAN_FLAGS = new Set(['non-interactive']);

function parseArgs(argv) {
  const [command, ...rest] = argv;
  const args = { command };
  for (let index = 0; index < rest.length; index += 1) {
    const key = rest[index];
    if (key?.startsWith('--') && BOOLEAN_FLAGS.has(key.slice(2))) {
      args[key.slice(2)] = true;
      continue;
    }
    const value = rest[index + 1];
    if (!key?.startsWith('--') || !value || value.startsWith('--')) {
      throw new Error(`参数格式无效，位置：${key ?? '参数末尾'}`);
    }
    args[key.slice(2)] = value;
    index += 1;
  }
  return args;
}

async function runExport(args) {
  const result = await exportLibTVURL({
    url: required(args, 'url'),
    outputDir: required(args, 'output-dir'),
    binary: args['libtv-cli'],
    nonInteractive: Boolean(args['non-interactive']),
    title: args.title,
    env: process.env,
    onProgress: (message) => process.stderr.write(`${message}\n`),
  });
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

async function runAuth(args) {
  const result = await preflightLibTVAuth({
    binary: args['libtv-cli'],
    nonInteractive: Boolean(args['non-interactive']),
    env: process.env,
    onProgress: (message) => process.stderr.write(`${message}\n`),
  });
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

function required(args, key) {
  const value = args[key]?.trim();
  if (!value) throw new Error(`缺少必填参数 --${key}`);
  return value;
}

async function readJson(path) {
  const text = await readFile(resolve(path), 'utf8');
  try {
    return JSON.parse(text);
  } catch (error) {
    throw new Error(`${path} 不是有效的 JSON：${error instanceof Error ? error.message : String(error)}`);
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
  if (args.command === 'plan') {
    await runPlan(args);
    return;
  }
  if (args.command === 'export') {
    await runExport(args);
    return;
  }
  if (args.command === 'auth') {
    await runAuth(args);
    return;
  }
  throw new Error(
    '用法：\n' +
      '  node adapters/libtv/cli.mjs auth [--libtv-cli <路径>] [--non-interactive]\n' +
      '  node adapters/libtv/cli.mjs export --url <LibTV 画布链接> --output-dir <新目录> ' +
      '[--libtv-cli <路径>] [--non-interactive] [--title <标题>]\n' +
      '  node adapters/libtv/cli.mjs plan --snapshot <snapshot.json> ' +
      '[--media-manifest <manifest.json>] [--title <标题>] --output <plan.json|->',
  );
}

export { main, parseArgs };

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  });
}
