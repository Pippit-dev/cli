#!/usr/bin/env node
// Marketing orchestration; authenticated API calls stay inside pippit-tool-cli.
const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const { spawn } = require('child_process');
const { pipeline } = require('stream/promises');

const BASE = 'https://xyq.jianying.com';
const PATHS = {
  generate: '/api/biz/v1/agent/submit_marketing_run',
  upload: '/api/biz/v1/skill/upload_file',
  query: '/api/biz/v1/agent/query_generate_video_result',
  balance: '/api/biz/v1/skill/get_credit_balance',
};
const object = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const nonempty = value => typeof value === 'string' && value.trim().length > 0;
const RUN_STATES = Object.freeze({ 0: 'Unspecified', 1: 'Submitted', 2: 'Working', 3: 'Completed', 4: 'Failed', 5: 'Canceled', 6: 'InputRequired', 7: 'Generating', 8: 'Interrupt', 9: 'HITL_Interrupt' });
const WAIT_STATES = ['1', '2', '7', '8'];
const INPUT_STATES = ['6', '9'];
function requireValue(condition, message) { if (!condition) throw new Error(message); }
function onlyKeys(value, keys) {
  requireValue(object(value), '请求字段必须是 JSON 对象');
  requireValue(Object.keys(value).every(key => keys.includes(key)), '存在未支持的字段，请核对接口文档');
}

function validate(body) {
  onlyKeys(body, ['message', 'asset_ids', 'thread_id', 'general_agent_settings']);
  requireValue(nonempty(body.message), 'message 必须为用户的非空创作指令');
  if ('thread_id' in body) requireValue(nonempty(body.thread_id), 'thread_id 必须为非空字符串');
  if ('asset_ids' in body) requireValue(Array.isArray(body.asset_ids) && body.asset_ids.every(nonempty), 'asset_ids 必须为素材 ID 字符串数组');
  const settings = body.general_agent_settings;
  onlyKeys(settings, ['ratio', 'duration_start', 'duration_end', 'show_subtitle', 'video_model', 'video_resolution']);
  requireValue(nonempty(settings.video_model), 'general_agent_settings.video_model 必填；请先取得用户模型选择或按已授权的选择范围配置模型');
  if ('ratio' in settings) requireValue([2, 3, 4, 5, 6].includes(settings.ratio), 'ratio 必须为 2/3/4/5/6 的整数枚举');
  for (const key of ['duration_start', 'duration_end']) {
    if (key in settings) requireValue(Number.isInteger(settings[key]) && settings[key] > 0 && settings[key] <= 2147483647, `${key} 必须为正整数秒数（int32）`);
  }
  if ('duration_start' in settings && 'duration_end' in settings) requireValue(settings.duration_start <= settings.duration_end, '时长下限不能大于上限');
  if ('show_subtitle' in settings) requireValue(typeof settings.show_subtitle === 'boolean', 'show_subtitle 必须为布尔值');
  for (const key of ['video_model', 'video_resolution']) {
    if (key in settings) requireValue(nonempty(settings[key]), `${key} 必须为非空字符串`);
  }
  return body;
}

function checkResponse(action, result, body) {
  requireValue(object(result) && (result.ret === '0' || result.ret === 0),
    `API 失败：ret=${result && result.ret} errmsg=${result && result.errmsg || ''} log_id=${result && result.log_id || ''}`);
  const data = result.data;
  requireValue(object(data), 'API 响应缺少 data');
  if (action === 'generate') requireValue(object(data.run) && nonempty(data.run.thread_id) && nonempty(data.run.run_id), '提交响应缺少任务 ID；结果不明确，禁止自动重提');
  if (action === 'upload') requireValue(nonempty(data.pippit_asset_id), '上传响应缺少 pippit_asset_id');
  if (action === 'balance') requireValue(typeof data.total_remain_amount === 'string' && /^\d+$/.test(data.total_remain_amount), '余额响应缺少字符串 total_remain_amount');
  if (action === 'query') {
    requireValue(data.thread_id === body.thread_id && data.run_id === body.run_id, '返回任务 ID 与请求不一致');
    requireValue((typeof data.run_state === 'number' || typeof data.run_state === 'string') && /^\d+$/.test(String(data.run_state)), 'run_state 缺失或格式异常');
    for (const key of ['video_urls', 'image_urls']) {
      if (key in data) requireValue(Array.isArray(data[key]) && data[key].every(nonempty), `${key} 格式异常`);
    }
  }
  return result;
}

function resolveCLI({ platform = process.platform, searchPath = process.env.PATH || '', packageRoot = path.resolve(__dirname, '../../..') } = {}) {
  const binaryName = platform === 'win32' ? 'pippit-tool-cli.exe' : 'pippit-tool-cli';
  function fromPackage(root) {
    const manifest = path.join(root, 'package.json');
    if (!fs.existsSync(manifest) || JSON.parse(fs.readFileSync(manifest, 'utf8')).name !== '@pippit-dev/cli') return null;
    const command = path.join(root, 'bin', binaryName);
    requireValue(fs.existsSync(command), 'CLI 原生程序缺失，请重新安装 @pippit-dev/cli；无需提供 access_token');
    return { command, args: [] };
  }
  // Run the native binary directly so deadlines also stop the API process.
  const bundled = fromPackage(packageRoot);
  if (bundled) return bundled;
  for (const dir of searchPath.split(path.delimiter).filter(Boolean)) {
    const binary = path.join(dir, binaryName);
    if (fs.existsSync(binary)) {
      const resolved = fs.realpathSync(binary);
      const npmPackage = fromPackage(path.resolve(path.dirname(resolved), '..'));
      return npmPackage || { command: binary, args: [] };
    }
    if (platform === 'win32') {
      const npmPackage = fromPackage(path.join(dir, 'node_modules/@pippit-dev/cli'));
      if (npmPackage) return npmPackage;
    }
  }
  throw new Error('请先安装或更新 @pippit-dev/cli，并运行 pippit-tool-cli login；无需提供 access_token');
}

function invokeCLI(args, input, timeout, invocation = resolveCLI()) {
  return new Promise((resolve, reject) => {
    const child = spawn(invocation.command, [...invocation.args, ...args], {
      shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'],
    });
    let stdout = '', stderr = '', failure;
    const stop = message => { failure = new Error(message); child.kill(); };
    const timer = setTimeout(() => stop('CLI 请求超时；提交结果可能不明确，请勿自动重提'), timeout);
    child.stdout.setEncoding('utf8'); child.stderr.setEncoding('utf8');
    child.stdout.on('data', chunk => {
      stdout += chunk;
      if (Buffer.byteLength(stdout) > 8 * 1024 * 1024) stop('CLI 响应超出 8 MiB 限制');
    });
    child.stderr.on('data', chunk => {
      stderr += chunk;
      if (Buffer.byteLength(stderr) > 1024 * 1024) stop('CLI 错误输出过大');
    });
    child.once('error', () => { clearTimeout(timer); reject(new Error('无法启动 pippit-tool-cli，请检查安装；无需提供 access_token')); });
    child.stdin.on('error', error => { if (error.code !== 'EPIPE') failure = new Error('无法写入 CLI 请求'); });
    child.once('close', code => {
      clearTimeout(timer);
      if (failure) return reject(failure);
      if (code !== 0) return reject(new Error(`CLI 调用失败：${stderr.trim() || `退出码 ${code}`}；请求未重试。若不支持 marketing 命令，请更新 CLI。`));
      let result;
      try { result = args.includes('--help') ? stdout : JSON.parse(stdout); }
      catch (_) { return reject(new Error('CLI 未返回有效 JSON；提交结果可能不明确，请勿自动重提')); }
      // Relay successful update and space notices; backend logs stay buffered.
      for (const notice of stderr.split(/\r?\n/)) {
        if (notice.startsWith('[pippit-tool-cli] New version available:') ||
            notice === '[pippit-tool-cli] If browser authorization fails, update the CLI and run pippit-tool-cli login again.' ||
            notice.startsWith('本次空间：') ||
            notice.startsWith('请求已成功，但空间记录保存失败：') ||
            notice === '本地空间记录不可用，将使用显式 --team-id；本次不保存空间记录。') {
          process.stderr.write(`${notice}\n`);
        }
      }
      resolve(result);
    });
    child.stdin.end(input);
  });
}

function createClient({ request = https.request, timeout = 60000, invoke = invokeCLI, teamID, spaceContext } = {}) {
  const scopeArgs = [];
  if (teamID !== undefined) scopeArgs.push('--team-id', normalizeTeamID(teamID));
  if (spaceContext) scopeArgs.push('--space-context', spaceContext);
  function open(url, method, headers, body) {
    return new Promise((resolve, reject) => {
      const target = new URL(url);
      requireValue(target.protocol === 'https:' && !target.username && !target.password, '仅支持无内嵌凭据的 HTTPS URL');
      const req = request(target, { method, headers }, res => {
        // Keep the total deadline active through response consumption.
        res.once('end', () => clearTimeout(timer));
        res.once('close', () => clearTimeout(timer));
        resolve(res);
      });
      const timer = setTimeout(() => req.destroy(new Error('请求超时；提交结果可能不明确，请勿自动重提')), timeout);
      req.once('error', error => { clearTimeout(timer); reject(error); });
      if (body && typeof body.pipe === 'function') {
        body.once('error', error => req.destroy(error));
        req.once('close', () => body.destroy());
        body.pipe(req);
      } else req.end(body);
    });
  }

  async function api(action, body, source, execute = true) {
    requireValue(['generate', 'query', 'balance'].includes(action), '无效营销 API 操作');
    const args = ['marketing', action, '--timeout', `${timeout}ms`, ...scopeArgs];
    let input;
    if (action === 'generate') {
      validate(body);
      args.push('--request', '-');
      if (execute) args.push('--execute');
      if (source !== undefined) {
        // Probe before submitting; optional attribution must not break older CLIs.
        let help = '';
        try { help = await invoke(['marketing', 'generate', '--help'], undefined, timeout); } catch (_) {}
        if (typeof help === 'string' && /(?:^|\s)--source(?:[=\s,]|$)/m.test(help)) args.push('--source', source.trim());
      }
      input = JSON.stringify(body);
    }
    if (action === 'query') args.push('--thread-id', body.thread_id, '--run-id', body.run_id);
    const result = await invoke(args, input, timeout);
    return action === 'generate' && !execute ? result : checkResponse(action, result, body);
  }

  async function upload(file) {
    const stat = await fs.promises.stat(file);
    requireValue(stat.isFile() && stat.size > 0 && stat.size < 500000000, '上传需要非空文件且小于 500 MB');
    return checkResponse('upload', await invoke(['marketing', 'upload', '--file', path.resolve(file), '--timeout', `${timeout}ms`, ...scopeArgs], undefined, timeout));
  }

  async function download(url, destination) {
    // Media requests never receive the API key, including across redirects.
    let res;
    for (let redirects = 0; ; redirects++) {
      res = await open(url, 'GET', {}, undefined);
      if (![301, 302, 303, 307, 308].includes(res.statusCode)) break;
      res.resume();
      requireValue(redirects < 5 && res.headers.location, '媒体重定向次数过多或缺少 Location');
      url = new URL(res.headers.location, url).href;
    }
    if (res.statusCode !== 200) { res.resume(); throw new Error(`媒体下载 HTTP ${res.statusCode}`); }
    if (/text\/html|application\/json/i.test(res.headers['content-type'] || '')) {
      res.resume(); throw new Error('下载返回网页或 JSON，未取得媒体');
    }
    // Exclusive creation preserves existing user files and permits safe recovery.
    let output;
    try { output = await fs.promises.open(destination, 'wx'); }
    catch (error) { res.destroy(); throw error; }
    try {
      await pipeline(res, fs.createWriteStream(destination, { fd: output.fd, autoClose: false }));
      await output.close();
      requireValue((await fs.promises.stat(destination)).size > 0, '下载文件为空');
    } catch (error) {
      await output.close().catch(() => {});
      await fs.promises.unlink(destination).catch(() => {});
      throw error;
    }
    return path.resolve(destination);
  }
  return { api, upload, download };
}

const HELP = `小云雀营销 Skill（Node.js >= 16）
  node marketing.js generate --request request.json [--source HOST] [--dry-run | --execute]
  node marketing.js upload --file product.png
  node marketing.js query --thread-id ID --run-id ID [--wait] [--max-wait 900] [--output-dir DIR]
  node marketing.js balance
所有 API 调用复用 pippit-tool-cli 登录态；未登录先运行 pippit-tool-cli login，无需提供 access_token。generate 默认仅预览。
--source HOST：仅生成提交使用，映射为请求体 platform；未传或空值时尝试环境识别，来源未知则省略。
--team-id ID：本次使用团队；0 为个人，不改变 space use 保存的默认空间。轮询固定使用原任务空间。
--timeout 秒数：单请求总时限，默认 60；--max-wait：轮询总时限，默认 900。
query 输出 API 原始响应；有 --output-dir 时成功结果附带 downloaded_files。
退出码：0 成功/单次查询进行中；1 输入或接口错误；2 生成失败/取消/无视频；3 等待超时；4 等待用户交互；5 未知或未指定状态。
`;

// Keep runtime marker semantics aligned with internal/common/host_source.go.
function resolveHostSource(explicit, env = process.env) {
  if (explicit !== undefined && explicit.trim()) return explicit.trim();
  if ((env.PIPPIT_CLI_SOURCE || '').trim()) return env.PIPPIT_CLI_SOURCE.trim();
  const hosts = new Set();
  for (const [key, host] of [
    ['CODEX_THREAD_ID', 'codex'], ['CODEX_SESSION_ID', 'codex'],
    ['CLAUDECODE', 'claude_code'], ['CURSOR_AGENT', 'cursor'], ['GEMINI_CLI', 'gemini_cli'],
    ['OPENCODE', 'opencode'], ['QWEN_CODE', 'qwen_code'], ['CLINE_ACTIVE', 'cline'],
    ['COPILOT_CLI', 'github_copilot_cli'], ['COPILOT_AGENT_SESSION_ID', 'github_copilot_cli'],
  ]) {
    const value = (env[key] || '').trim().toLowerCase();
    if (value && value !== '0' && value !== 'false') hosts.add(host);
  }
  return hosts.size === 1 ? [...hosts][0] : '';
}

function normalizeTeamID(value) {
  requireValue(typeof value === 'string' && /^\d+$/.test(value), 'team-id 必须为 0 或正整数');
  requireValue(BigInt(value) <= 9223372036854775807n, 'team-id 超出 int64 范围');
  return BigInt(value).toString();
}

function parseArgs(argv) {
  const [action, ...rest] = argv;
  requireValue(Object.hasOwnProperty.call(PATHS, action), '操作必须是 generate/upload/query/balance');
  const options = {};
  const flags = ['execute', 'dry-run', 'wait'];
  const allowed = { generate: ['request', 'execute', 'dry-run', 'source'], upload: ['file'], query: ['thread-id', 'run-id', 'wait', 'max-wait', 'output-dir'], balance: [] }[action].concat('timeout', 'team-id');
  for (let i = 0; i < rest.length; i++) {
    const name = rest[i].replace(/^--/, '');
    requireValue(rest[i].startsWith('--') && allowed.includes(name) && !(name in options), `无效或重复参数：${rest[i]}`);
    if (flags.includes(name)) options[name] = true;
    else {
      requireValue(typeof rest[i + 1] === 'string' && (name === 'source' || nonempty(rest[i + 1])) && !rest[i + 1].startsWith('--'), `${name} 缺少值`);
      options[name] = rest[++i];
    }
  }
  if (options['team-id'] !== undefined) options['team-id'] = normalizeTeamID(options['team-id']);
  requireValue(!(options.execute && options['dry-run']), '--execute 和 --dry-run 不能同时使用');
  for (const [name, fallback, max] of [['timeout', 60, 1800], ['max-wait', 900, 86400]]) {
    const value = options[name] === undefined ? fallback : Number(options[name]);
    requireValue(Number.isInteger(value) && value > 0 && value <= max, `${name} 必须为 1 至 ${max} 的整数秒数`);
    options[name] = value;
  }
  return { action, options };
}

async function main(argv, { out = console.log, clientFactory = createClient, now = Date.now, pause = ms => new Promise(resolve => setTimeout(resolve, ms)) } = {}) {
  if (!argv.length || argv.includes('--help') || argv.includes('-h')) { out(HELP); return 0; }
  const { action, options } = parseArgs(argv);
  let body = {};
  if (action === 'generate') {
    const source = resolveHostSource(options.source);
    if (options.source !== undefined || source) options.source = source;
    requireValue(options.request, 'generate 需要 --request JSON 文件');
    body = validate(JSON.parse(await fs.promises.readFile(options.request, 'utf8')));
  }
  if (action === 'query') {
    requireValue(nonempty(options['thread-id']) && nonempty(options['run-id']), 'query 需要 thread-id 和 run-id');
    body = { thread_id: options['thread-id'], run_id: options['run-id'] };
  }
  if (action === 'upload') requireValue(nonempty(options.file), 'upload 需要 --file');
  const client = clientFactory({ timeout: options.timeout * 1000, teamID: options['team-id'] });
  if (action === 'generate' && !options.execute) {
    out(JSON.stringify(await client.api(action, body, options.source, false)));
    return 0;
  }
  let fixedScope;
  let fixedTeamID = options['team-id'];
  let result;
  const deadline = now() + options['max-wait'] * 1000;
  do {
    // A poll request cannot overrun the remaining wait budget.
    const pollClient = action === 'query' && options.wait
      ? clientFactory({ timeout: Math.min(options.timeout * 1000, Math.max(1, deadline - now())), teamID: fixedTeamID, spaceContext: fixedScope }) : client;
    result = action === 'upload' ? await client.upload(options.file) : await pollClient.api(action, body, options.source);
    if (action === 'query' && options.wait && fixedTeamID !== undefined) requireValue(result.team_id === fixedTeamID, '任务空间发生变化，已停止轮询');
    if (action === 'query' && options.wait && fixedScope) requireValue(result.space_context === fixedScope, '任务空间或账号发生变化，已停止轮询');
    if (action !== 'query' || !options.wait || !WAIT_STATES.includes(String(result.data.run_state))) break;
    requireValue(nonempty(result.team_id) && (nonempty(result.space_context) || fixedTeamID !== undefined), 'CLI 未返回固定空间信息，已停止轮询；请使用原 --team-id 查询同一任务，不要重提');
    fixedScope = result.space_context;
    fixedTeamID = result.team_id;
    if (now() >= deadline) {
      out(JSON.stringify({ ...result, wait_timed_out: true })); return 3;
    }
    await pause(Math.min(10000, deadline - now()));
    if (now() >= deadline) { out(JSON.stringify({ ...result, wait_timed_out: true })); return 3; }
  } while (true);
  if (action === 'query') {
    const state = String(result.data.run_state);
    if (INPUT_STATES.includes(state)) {
      out(JSON.stringify({ ...result, run_state_name: RUN_STATES[state], action_required: true,
        next_step: '查看同一会话的确认或问卷；沿用已有授权，缺少必要选择时再询问用户。确认后取得同一 thread 的最新 run_id 再查询，旧 Run 可保持此状态。不要重复生成。' }));
      return 4;
    }
    if (!(state in RUN_STATES) || state === '0') {
      out(JSON.stringify({ ...result, run_state_name: RUN_STATES[state] || 'Unknown', unknown_state: true,
        next_step: '停止自动轮询，保留任务 ID 和原始响应，核实服务端状态含义；不要重新提交生成。' }));
      return 5;
    }
  }
  // Print IDs and URLs before downloads so a partial download failure remains resumable.
  out(JSON.stringify(result));
  if (action === 'query') {
    const state = String(result.data.run_state);
    if (['4', '5'].includes(state)) return 2;
    if (state === '3') {
      if (!(result.data.video_urls || []).length) return 2;
      if (options['output-dir']) {
        await fs.promises.mkdir(options['output-dir'], { recursive: true });
        const downloaded = [];
        for (const [field, ext] of [['video_urls', '.mp4'], ['image_urls', '.jpg']]) {
          for (const [index, url] of (result.data[field] || []).entries()) {
            const suffix = path.extname(new URL(url).pathname);
            const mediaExt = /^\.(mp4|mov|webm|mkv|jpg|jpeg|png|webp|gif)$/i.test(suffix) ? suffix : ext;
            const file = path.join(options['output-dir'], `${field}-${index + 1}-${crypto.randomBytes(4).toString('hex')}${mediaExt}`);
            downloaded.push({ url, path: await client.download(url, file) });
            out(JSON.stringify({ downloaded_file: downloaded[downloaded.length - 1] }));
          }
        }
        out(JSON.stringify({ ...result, downloaded_files: downloaded }));
      }
    }
  }
  return 0;
}

if (require.main === module) {
  main(process.argv.slice(2)).then(code => { process.exitCode = code; }).catch(error => {
    const key = process.env.XYQ_ACCESS_KEY;
    const message = key ? error.message.split(key).join('[REDACTED]') : error.message;
    console.error(JSON.stringify({ error: message, note: '请求未自动重试；已有任务请按原 thread_id/run_id 继续查询。' }));
    process.exitCode = 1;
  });
}
module.exports = { BASE, PATHS, validate, checkResponse, resolveCLI, invokeCLI, createClient, resolveHostSource, parseArgs, main };
