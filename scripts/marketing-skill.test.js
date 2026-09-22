const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');
const { spawnSync } = require('child_process');
const { BASE, PATHS, validate, createClient, main } = require('../skills/xyq-marketing-skill/scripts/marketing');

async function test() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'xyq-marketing-'));
  const received = [];
  let respond;
  const server = http.createServer(async (req, res) => {
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    received.push({ url: req.url, method: req.method, headers: req.headers, body: Buffer.concat(chunks) });
    respond(req, res);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  // Real HTTP streaming on loopback; public HTTPS destination is checked before mapping.
  const targets = [];
  const request = (url, opts, callback) => {
    targets.push(url.href);
    return http.request({ hostname: '127.0.0.1', port: server.address().port, path: url.pathname + url.search, ...opts }, callback);
  };
  const client = createClient({ key: 'test-secret', request, timeout: 1000 });
  const json = value => { respond = (_, res) => { res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(value)); }; };
  const ids = { thread_id: 'marketing-thread', run_id: 'marketing-run' };
  const response = (state, extra = {}) => ({ ret: '0', data: { ...ids, run_state: state, ...extra } });
  try {
    const body = { message: '按原文生成 🛍️', general_agent_settings: { video_model: 'seedance2.0_vision', ratio: 3, show_subtitle: false, duration_start: 15, duration_end: 15 } };
    assert.strictEqual(validate(body), body);
    for (const invalid of [
      { message: '', general_agent_settings: {} },
      { message: 'x' },
      { message: 'x', general_agent_settings: {} },
      { message: 'x', general_agent_settings: { video_model: ' ' } },
      { message: 'x', general_agent_settings: { video_model: 'future-model', ratio: '9:16' } },
      { message: 'x', general_agent_settings: { video_model: 'future-model', show_subtitle: 'false' } },
      { message: 'x', general_agent_settings: { video_model: 'future-model', duration_start: 20, duration_end: 15 } },
      { message: 'x', general_agent_settings: { video_model: 'future-model', duration_start: 1.5 } },
      { message: 'x', general_agent_settings: { video_model: 'future-model', duration_start: 2147483648 } },
      { message: 'x', general_agent_settings: {}, asset_ids: [123] },
      { message: 'x', general_agent_settings: {}, TeamID: 'invented' },
    ]) assert.throws(() => validate(invalid));
    // New server model values remain usable without a Skill update.
    validate({ message: 'x', general_agent_settings: { video_model: 'future-model' } });

    const file = path.join(dir, 'request.json');
    fs.writeFileSync(file, JSON.stringify(body));
    const output = [];
    assert.strictEqual(await main(['generate', '--request', file], {
      env: {}, out: line => output.push(JSON.parse(line)), clientFactory: () => { throw new Error('dry-run contacted API'); },
    }), 0);
    assert.deepStrictEqual(output[0], { dry_run: true, url: BASE + PATHS.generate, body });
    await assert.rejects(main(['generate', '--request', file, '--execute', '--dry-run']), /不能同时/);
    await assert.rejects(main(['query', '--thread-id', ids.thread_id]), /run-id/);
    await assert.rejects(main(['query', '--thread-id', ids.thread_id, '--run-id', ids.run_id, '--timeout', '0']), /timeout/);
    await assert.rejects(main(['generate', '--request', file, '--source', 'codex']), /无效/);
    await assert.rejects(createClient({ key: '', request }).api('balance', {}), /XYQ_ACCESS_KEY/);
    assert.strictEqual(received.length, 0);

    json({ ret: 0, log_id: 'log-submit', data: { run: { ...ids, state: 1 }, web_thread_link: 'https://xyq.jianying.com/task' } });
    assert.strictEqual(await main(['generate', '--request', file, '--execute'], { out: () => {}, clientFactory: () => client }), 0);
    assert.strictEqual(received.length, 1);
    assert.deepStrictEqual(JSON.parse(received[0].body), body);
    assert.strictEqual(received[0].url, PATHS.generate);
    assert.strictEqual(received[0].headers.authorization, 'Bearer test-secret');
    assert.strictEqual(targets[0], BASE + PATHS.generate);

    const uploadFile = path.join(dir, '商品.png');
    const imageBytes = Buffer.from([0, 1, 2, 255, 13, 10]);
    fs.writeFileSync(uploadFile, imageBytes);
    json({ ret: '0', data: { pippit_asset_id: 'asset-real' } });
    assert.strictEqual((await client.upload(uploadFile)).data.pippit_asset_id, 'asset-real');
    const upload = received[received.length - 1];
    assert.strictEqual(upload.url, PATHS.upload);
    assert(upload.body.includes(imageBytes));
    assert(upload.body.includes(Buffer.from('name="file"')));
    assert(upload.headers['content-type'].startsWith('multipart/form-data; boundary='));
    assert.strictEqual(Number(upload.headers['content-length']), upload.body.length);
    assert(!upload.body.includes(Buffer.from('test-secret')));
    await assert.rejects(client.upload(dir), /非空文件/);

    json({ ret: '0', data: { total_remain_amount: '0' } });
    assert.strictEqual((await client.api('balance', {})).data.total_remain_amount, '0');
    json({ ret: '12004', errmsg: 'permission denied', log_id: 'log-failure' });
    await assert.rejects(client.api('generate', body), /12004.*permission denied.*log-failure/);
    json({ ret: '0', data: { run: { thread_id: ids.thread_id } } });
    await assert.rejects(client.api('generate', body), /禁止自动重提/);
    json({ ret: false, data: {} });
    await assert.rejects(client.api('balance', {}), /API 失败/);
    json(response('3', { run_id: 'different' }));
    await assert.rejects(client.api('query', ids), /不一致/);
    json(response('unknown'));
    await assert.rejects(client.api('query', ids), /run_state 缺失或格式异常/);
    respond = (_, res) => { res.writeHead(302, { Location: 'https://other.example/secret' }); res.end(); };
    let before = received.length;
    await assert.rejects(client.api('generate', body), /HTTP 302/);
    assert.strictEqual(received.length - before, 1, 'API redirect must not be followed');
    respond = (_, res) => { res.writeHead(504); res.end(); };
    before = received.length;
    await assert.rejects(client.api('generate', body), /HTTP 504/);
    assert.strictEqual(received.length - before, 1, 'submission must not retry');
    respond = (_, res) => res.end('<html>not json</html>');
    await assert.rejects(client.api('generate', body), /有效 JSON/);
    respond = () => {};
    await assert.rejects(createClient({ key: 'test-secret', request, timeout: 20 }).api('generate', body), /超时/);

    const queryArgs = ['query', '--thread-id', ids.thread_id, '--run-id', ids.run_id];
    const seenActions = [];
    let states = [1, '2', 7, '8', 3];
    let clock = 0;
    const outputs = [];
    const runtime = {
      out: line => outputs.push(JSON.parse(line)), now: () => clock, pause: async ms => { clock += ms; },
      clientFactory: () => ({ api: async (action, input) => {
        seenActions.push(action); assert.deepStrictEqual(input, ids);
        return response(states.shift(), { video_urls: ['https://cdn.example/final.mp4'] });
      } }),
    };
    assert.strictEqual(await main([...queryArgs, '--wait'], runtime), 0);
    assert.deepStrictEqual(seenActions, ['query', 'query', 'query', 'query', 'query']);
    assert.strictEqual(outputs[0].data.run_state, 3);
    assert.strictEqual(clock, 40000);
    for (const state of [6, '6', 9, '9']) {
      states = [state, 3];
      const beforeClock = clock;
      assert.strictEqual(await main([...queryArgs, '--wait'], runtime), 4);
      assert.strictEqual(states.length, 1, 'input-required run must not be polled again');
      assert.strictEqual(clock, beforeClock, 'input-required run must not sleep');
      const result = outputs[outputs.length - 1];
      assert.strictEqual(result.action_required, true);
      assert.strictEqual(result.run_state_name, state == 9 ? 'HITL_Interrupt' : 'InputRequired');
      assert.deepStrictEqual(result.data, response(state, { video_urls: ['https://cdn.example/final.mp4'] }).data);
    }
    states = [2, 7, 9, 3];
    assert.strictEqual(await main([...queryArgs, '--wait'], runtime), 4);
    assert.strictEqual(states.length, 1);
    for (const state of [0, 99, '99']) {
      states = [state, 3];
      assert.strictEqual(await main([...queryArgs, '--wait'], runtime), 5);
      assert.strictEqual(states.length, 1);
      assert.strictEqual(outputs[outputs.length - 1].unknown_state, true);
      assert.strictEqual(outputs[outputs.length - 1].data.run_state, state);
    }
    for (const state of [4, '5']) {
      states = [state];
      assert.strictEqual(await main([...queryArgs, '--wait'], runtime), 2);
      assert.strictEqual(states.length, 0);
    }
    states = [8, 2];
    assert.strictEqual(await main([...queryArgs, '--wait', '--max-wait', '1'], runtime), 3);
    assert.strictEqual(states.length, 1, 'stop at deadline without a further query');
    assert.strictEqual(outputs[outputs.length - 1].wait_timed_out, true);
    states = [2];
    assert.strictEqual(await main(queryArgs, runtime), 0, 'single pending query is not a completed video');
    const emptyRuntime = { out: () => {}, clientFactory: () => ({ api: async () => response(3, { video_urls: [] }) }) };
    assert.strictEqual(await main([...queryArgs, '--wait'], emptyRuntime), 2);

    const media = Buffer.from('test-media-bytes');
    respond = (req, res) => {
      if (req.url === '/redirect') { res.writeHead(302, { Location: 'https://cdn.example/final.mp4' }); res.end(); }
      else { res.setHeader('Content-Type', 'video/mp4'); res.end(media); }
    };
    before = received.length;
    const dest = path.join(dir, 'video.mp4');
    await client.download('https://cdn.example/redirect', dest);
    assert.deepStrictEqual(fs.readFileSync(dest), media);
    for (const call of received.slice(before)) assert.strictEqual(call.headers.authorization, undefined);
    await assert.rejects(client.download('https://cdn.example/final.mp4', dest), /EEXIST/);
    assert.deepStrictEqual(fs.readFileSync(dest), media);
    await assert.rejects(client.download('http://cdn.example/final.mp4', path.join(dir, 'bad')), /HTTPS/);
    respond = (_, res) => { res.setHeader('Content-Type', 'text/html'); res.end('login'); };
    await assert.rejects(client.download('https://cdn.example/login', path.join(dir, 'login.mp4')), /未取得媒体/);
    assert(!fs.existsSync(path.join(dir, 'login.mp4')));
    respond = (_, res) => res.end();
    const emptyFile = path.join(dir, 'empty.mp4');
    await assert.rejects(client.download('https://cdn.example/empty', emptyFile), /为空/);
    assert(!fs.existsSync(emptyFile));

    // Exercise query + download orchestration and preserve raw IDs before delivery.
    respond = (req, res) => {
      if (req.url === PATHS.query) res.end(JSON.stringify(response('3', { video_urls: ['https://cdn.example/v.mp4'], image_urls: ['https://cdn.example/i.png'] })));
      else { res.setHeader('Content-Type', 'application/octet-stream'); res.end(media); }
    };
    const delivery = [];
    assert.strictEqual(await main([...queryArgs, '--output-dir', path.join(dir, 'results')], { out: line => delivery.push(JSON.parse(line)), clientFactory: () => client }), 0);
    assert.strictEqual(delivery[0].data.run_id, ids.run_id);
    assert.strictEqual(delivery[delivery.length - 1].downloaded_files.length, 2);
    for (const file of delivery[delivery.length - 1].downloaded_files) assert.deepStrictEqual(fs.readFileSync(file.path), media);

    const executable = path.resolve(__dirname, '../skills/xyq-marketing-skill/scripts/marketing.js');
    const help = spawnSync(process.execPath, [executable, '--help'], { encoding: 'utf8', env: { ...process.env, XYQ_ACCESS_KEY: '' } });
    assert.strictEqual(help.status, 0);
    assert(help.stdout.includes('generate --request'));
    console.log('Marketing Skill: validation, multipart, API errors, no replay, polling and media delivery passed');
  } finally {
    await new Promise(resolve => server.close(resolve));
    fs.rmSync(dir, { recursive: true, force: true });
  }
}
test().catch(error => { console.error(error); process.exitCode = 1; });
