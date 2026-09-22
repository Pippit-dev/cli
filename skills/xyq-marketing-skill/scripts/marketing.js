#!/usr/bin/env node
// Documented Xiaoyunque marketing API; Node.js >= 16, no dependencies.
const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const { Readable } = require('stream');
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

function createClient({ key = process.env.XYQ_ACCESS_KEY, request = https.request, timeout = 60000 } = {}) {
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

  async function api(action, body, headers = { 'Content-Type': 'application/json' }) {
    requireValue(nonempty(key) && !/[\r\n]/.test(key), '请在本机环境设置 XYQ_ACCESS_KEY，不要在聊天中发送密钥；CLI 登录不会设置此变量');
    const wire = headers['Content-Type'] === 'application/json' ? JSON.stringify(body) : body;
    const res = await open(BASE + PATHS[action], 'POST', { ...headers, Accept: 'application/json', Authorization: `Bearer ${key}` }, wire);
    // Never follow API redirects or automatically retry a POST.
    if (res.statusCode < 200 || res.statusCode >= 300) {
      res.resume();
      throw new Error(`HTTP ${res.statusCode}；请求未重试，生成结果可能不明确`);
    }
    const chunks = [];
    let size = 0;
    for await (const chunk of res) {
      size += chunk.length;
      requireValue(size <= 8 * 1024 * 1024, 'API 响应超出 8 MiB 限制');
      chunks.push(chunk);
    }
    let result;
    try { result = JSON.parse(Buffer.concat(chunks).toString('utf8')); }
    catch (_) { throw new Error('API 未返回有效 JSON；提交结果可能不明确，请勿自动重提'); }
    return checkResponse(action, result, body);
  }

  async function upload(file) {
    const stat = await fs.promises.stat(file);
    requireValue(stat.isFile() && stat.size > 0 && stat.size < 500000000, '上传需要非空文件且小于 500 MB');
    const boundary = 'xyq-' + crypto.randomBytes(16).toString('hex');
    const name = path.basename(file).replace(/["\r\n\\]/g, '_');
    const mime = { '.png': 'image/png', '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.webp': 'image/webp', '.mp4': 'video/mp4', '.mp3': 'audio/mpeg', '.wav': 'audio/wav' }[path.extname(file).toLowerCase()] || 'application/octet-stream';
    const head = Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${name}"\r\nContent-Type: ${mime}\r\n\r\n`);
    const tail = Buffer.from(`\r\n--${boundary}--\r\n`);
    const body = Readable.from((async function* () {
      yield head;
      for await (const chunk of fs.createReadStream(file)) yield chunk;
      yield tail;
    })());
    return api('upload', body, { 'Content-Type': `multipart/form-data; boundary=${boundary}`, 'Content-Length': head.length + stat.size + tail.length });
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
  node marketing.js generate --request request.json [--dry-run | --execute]
  node marketing.js upload --file product.png
  node marketing.js query --thread-id ID --run-id ID [--wait] [--max-wait 900] [--output-dir DIR]
  node marketing.js balance
所有 API 调用从环境读取 XYQ_ACCESS_KEY；generate 默认仅预览。
--timeout 秒数：单请求总时限，默认 60；--max-wait：轮询总时限，默认 900。
query 输出 API 原始响应；有 --output-dir 时成功结果附带 downloaded_files。
退出码：0 成功/单次查询进行中；1 输入或接口错误；2 生成失败/取消/无视频；3 等待超时；4 等待用户交互；5 未知或未指定状态。
`;

function parseArgs(argv) {
  const [action, ...rest] = argv;
  requireValue(Object.hasOwnProperty.call(PATHS, action), '操作必须是 generate/upload/query/balance');
  const options = {};
  const flags = ['execute', 'dry-run', 'wait'];
  const allowed = { generate: ['request', 'execute', 'dry-run'], upload: ['file'], query: ['thread-id', 'run-id', 'wait', 'max-wait', 'output-dir'], balance: [] }[action].concat('timeout');
  for (let i = 0; i < rest.length; i++) {
    const name = rest[i].replace(/^--/, '');
    requireValue(rest[i].startsWith('--') && allowed.includes(name) && !(name in options), `无效或重复参数：${rest[i]}`);
    if (flags.includes(name)) options[name] = true;
    else {
      requireValue(nonempty(rest[i + 1]) && !rest[i + 1].startsWith('--'), `${name} 缺少值`);
      options[name] = rest[++i];
    }
  }
  requireValue(!(options.execute && options['dry-run']), '--execute 和 --dry-run 不能同时使用');
  for (const [name, fallback, max] of [['timeout', 60, 1800], ['max-wait', 900, 86400]]) {
    const value = options[name] === undefined ? fallback : Number(options[name]);
    requireValue(Number.isInteger(value) && value > 0 && value <= max, `${name} 必须为 1 至 ${max} 的整数秒数`);
    options[name] = value;
  }
  return { action, options };
}

async function main(argv, { env = process.env, out = console.log, clientFactory = createClient, now = Date.now, pause = ms => new Promise(resolve => setTimeout(resolve, ms)) } = {}) {
  if (!argv.length || argv.includes('--help') || argv.includes('-h')) { out(HELP); return 0; }
  const { action, options } = parseArgs(argv);
  let body = {};
  if (action === 'generate') {
    requireValue(options.request, 'generate 需要 --request JSON 文件');
    body = validate(JSON.parse(await fs.promises.readFile(options.request, 'utf8')));
    if (!options.execute) { out(JSON.stringify({ dry_run: true, url: BASE + PATHS.generate, body })); return 0; }
  }
  if (action === 'query') {
    requireValue(nonempty(options['thread-id']) && nonempty(options['run-id']), 'query 需要 thread-id 和 run-id');
    body = { thread_id: options['thread-id'], run_id: options['run-id'] };
  }
  if (action === 'upload') requireValue(nonempty(options.file), 'upload 需要 --file');
  const client = clientFactory({ key: env.XYQ_ACCESS_KEY, timeout: options.timeout * 1000 });
  let result;
  const deadline = now() + options['max-wait'] * 1000;
  do {
    // A poll request cannot overrun the remaining wait budget.
    const pollClient = action === 'query' && options.wait
      ? clientFactory({ key: env.XYQ_ACCESS_KEY, timeout: Math.min(options.timeout * 1000, Math.max(1, deadline - now())) }) : client;
    result = action === 'upload' ? await client.upload(options.file) : await pollClient.api(action, body);
    if (action !== 'query' || !options.wait || !WAIT_STATES.includes(String(result.data.run_state))) break;
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
module.exports = { BASE, PATHS, validate, checkResponse, createClient, parseArgs, main };
