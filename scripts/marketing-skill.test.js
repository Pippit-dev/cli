const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');
const { spawnSync } = require('child_process');
const { BASE, PATHS, validate, checkResponse, resolveCLI, invokeCLI, createClient, resolveHostSource, main } = require('../skills/xyq-marketing-skill/scripts/marketing');

async function test() {
  for (const key of ['PIPPIT_CLI_SOURCE', 'CODEX_THREAD_ID', 'CODEX_SESSION_ID', 'CLAUDECODE', 'CURSOR_AGENT', 'GEMINI_CLI', 'OPENCODE', 'QWEN_CODE', 'CLINE_ACTIVE', 'COPILOT_CLI', 'COPILOT_AGENT_SESSION_ID']) delete process.env[key];
  for (const [explicit, env, want] of [
    [undefined, {}, ''],
    [' workbuddy ', { CODEX_THREAD_ID: 'private-id' }, 'workbuddy'],
    ['', { PIPPIT_CLI_SOURCE: 'codex' }, 'codex'],
    ['  ', { OPENCODE: '1' }, 'opencode'],
    [undefined, { PIPPIT_CLI_SOURCE: ' doubao_office ', CODEX_THREAD_ID: 'private-id' }, 'doubao_office'],
    [undefined, { CODEX_THREAD_ID: 'private-id', CODEX_SESSION_ID: 'private-session' }, 'codex'],
    [undefined, { CLAUDECODE: '1' }, 'claude_code'],
    [undefined, { CURSOR_AGENT: '1' }, 'cursor'],
    [undefined, { GEMINI_CLI: '1' }, 'gemini_cli'],
    [undefined, { OPENCODE: '1' }, 'opencode'],
    [undefined, { QWEN_CODE: '1' }, 'qwen_code'],
    [undefined, { CLINE_ACTIVE: ' true ' }, 'cline'],
    [undefined, { COPILOT_CLI: '1' }, 'github_copilot_cli'],
    [undefined, { COPILOT_AGENT_SESSION_ID: 'private-id' }, 'github_copilot_cli'],
    [undefined, { COPILOT_CLI: '1', COPILOT_AGENT_SESSION_ID: 'private-id' }, 'github_copilot_cli'],
    [undefined, { CODEX_THREAD_ID: 'private-id', CURSOR_AGENT: '1' }, ''],
    [undefined, { OPENCODE: '1', CLINE_ACTIVE: 'true' }, ''],
    [undefined, { CLAUDECODE: '0', GEMINI_CLI: 'false', TERM_PROGRAM: 'vscode' }, ''],
    [undefined, { OPENCODE: '0', QWEN_CODE: ' FALSE ', CLINE_ACTIVE: '', COPILOT_CLI: 'false', COPILOT_AGENT_SESSION_ID: ' ' }, ''],
    [undefined, { AGENT: '1', OPENCODE_CONFIG: '/example/config.json', QWEN_API_KEY: 'private-id', COPILOT_HOME: '/example/copilot' }, ''],
  ]) assert.strictEqual(resolveHostSource(explicit, env), want);

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
  const calls = [];
  let cliResult;
  const client = createClient({ request, timeout: 1000, invoke: async (...args) => { calls.push(args); return args[0].includes('--help') ? '  --source string  Host identifier\n' : cliResult; } });
  const json = value => { respond = (_, res) => { res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(value)); }; };
  const ids = { thread_id: 'marketing-thread', run_id: 'marketing-run' };
  const response = (state, extra = {}) => ({ ret: '0', team_id: '12', space_context: 'fixed-account-origin-team', data: { ...ids, run_state: state, ...extra } });
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
    const previewFactory = () => ({ api: async (action, request, source, execute) => {
      assert.strictEqual(action, 'generate');
      assert.strictEqual(execute, false, 'preview must not submit');
      return { dry_run: true, url: BASE + PATHS.generate, body: source && source.trim() ? { ...request, platform: source.trim() } : request, team_id: '0' };
    } });
    const output = [];
    assert.strictEqual(await main(['generate', '--request', file], {
      env: {}, out: line => output.push(JSON.parse(line)), clientFactory: previewFactory,
    }), 0);
    assert.deepStrictEqual(output[0], { dry_run: true, url: BASE + PATHS.generate, body, team_id: '0' });
    await assert.rejects(main(['generate', '--request', file, '--execute', '--dry-run']), /不能同时/);
    await assert.rejects(main(['query', '--thread-id', ids.thread_id]), /run-id/);
    await assert.rejects(main(['query', '--thread-id', ids.thread_id, '--run-id', ids.run_id, '--timeout', '0']), /timeout/);
    for (const source of [' codex ', '', '  ']) {
      const preview = [];
      await main(['generate', '--request', file, '--source', source], { out: line => preview.push(JSON.parse(line)), clientFactory: previewFactory });
      assert.deepStrictEqual(preview[0].body, source.trim() ? { ...body, platform: source.trim() } : body);
    }
    await assert.rejects(main(['balance', '--source', 'codex']), /无效/);
    assert.strictEqual(received.length, 0);

    cliResult = { ret: 0, log_id: 'log-submit', data: { run: { ...ids, state: 1 } } };
    assert.strictEqual(await main(['generate', '--request', file, '--execute'], { out: () => {}, clientFactory: () => client }), 0);
    assert.deepStrictEqual(calls[0][0], ['marketing', 'generate', '--timeout', '1000ms', '--request', '-', '--execute']);
    assert.deepStrictEqual(JSON.parse(calls[0][1]), body);
    assert(!calls[0][0].includes(body.message), 'request must use stdin, not command arguments');
    assert.strictEqual(received.length, 0, 'Node must never make authenticated API requests');
    await main(['generate', '--request', file, '--source', ' codex ', '--execute'], { out: () => {}, clientFactory: () => client });
    assert.deepStrictEqual(calls[calls.length - 1][0], ['marketing', 'generate', '--timeout', '1000ms', '--request', '-', '--execute', '--source', 'codex']);
    assert.deepStrictEqual(JSON.parse(calls[calls.length - 1][1]), body);
    process.env.PIPPIT_CLI_SOURCE = ' workbuddy ';
    const inferredPreview = [];
    await main(['generate', '--request', file], { out: line => inferredPreview.push(JSON.parse(line)), clientFactory: previewFactory });
    assert.strictEqual(inferredPreview[0].body.platform, 'workbuddy');
    await main(['generate', '--request', file, '--execute'], { out: () => {}, clientFactory: () => client });
    assert.deepStrictEqual(calls[calls.length - 1][0].slice(-2), ['--source', 'workbuddy']);
    await main(['generate', '--request', file, '--source', '', '--execute'], { out: () => {}, clientFactory: () => client });
    assert.deepStrictEqual(calls[calls.length - 1][0].slice(-2), ['--source', 'workbuddy']);
    const blankPreview = [];
    await main(['generate', '--request', file, '--source', '  '], { out: line => blankPreview.push(JSON.parse(line)), clientFactory: previewFactory });
    assert.strictEqual(blankPreview[0].body.platform, 'workbuddy');
    delete process.env.PIPPIT_CLI_SOURCE;
    for (const [key, host] of [
      ['OPENCODE', 'opencode'], ['QWEN_CODE', 'qwen_code'], ['CLINE_ACTIVE', 'cline'],
      ['COPILOT_CLI', 'github_copilot_cli'], ['COPILOT_AGENT_SESSION_ID', 'github_copilot_cli'],
    ]) {
      process.env[key] = 'private-id';
      const preview = [];
      await main(['generate', '--request', file, '--source', ''], { out: line => preview.push(JSON.parse(line)), clientFactory: previewFactory });
      assert.strictEqual(preview[0].body.platform, host);
      await main(['generate', '--request', file, '--source', '', '--execute'], { out: () => {}, clientFactory: () => client });
      const submitted = calls[calls.length - 1];
      assert.deepStrictEqual(submitted[0].slice(-2), ['--source', host]);
      assert.deepStrictEqual(JSON.parse(submitted[1]), body);
      assert(!JSON.stringify(submitted).includes('private-id'));
      delete process.env[key];
    }

    const uploadFile = path.join(dir, '商品 with spaces.png');
    fs.writeFileSync(uploadFile, Buffer.from([0, 1, 2, 255]));
    cliResult = { ret: '0', data: { pippit_asset_id: 'asset-real' } };
    assert.strictEqual((await client.upload(uploadFile)).data.pippit_asset_id, 'asset-real');
    assert.deepStrictEqual(calls[calls.length - 1][0], ['marketing', 'upload', '--file', uploadFile, '--timeout', '1000ms']);
    await assert.rejects(client.upload(dir), /非空文件/);
    cliResult = { ret: '0', data: { total_remain_amount: '0' } };
    assert.strictEqual((await client.api('balance', {})).data.total_remain_amount, '0');
    cliResult = response('3', { video_urls: ['https://cdn.example/final.mp4'] });
    await client.api('query', ids);
    assert.deepStrictEqual(calls[calls.length - 1][0], ['marketing', 'query', '--timeout', '1000ms', '--thread-id', ids.thread_id, '--run-id', ids.run_id]);
    assert.throws(() => checkResponse('generate', { ret: '0', data: { run: {} } }), /禁止自动重提/);
    assert.throws(() => checkResponse('query', response('unknown'), ids), /run_state/);
    assert.throws(() => checkResponse('query', response(3, { run_id: 'other' }), ids), /不一致/);
    assert.throws(() => checkResponse('balance', { ret: false, data: {} }), /API 失败/);
    let failedCalls = 0;
    const failedClient = createClient({ invoke: async () => { failedCalls++; throw new Error('请先运行 pippit-tool-cli login'); } });
    await assert.rejects(failedClient.api('generate', body), /pippit-tool-cli login/);
    assert.strictEqual(failedCalls, 1, 'failed submission must never retry');

    // Installed npm layouts must work without cmd.exe/shell interpolation.
    for (const platform of ['linux', 'win32']) {
      const prefix = path.join(dir, platform);
      const packageRoot = path.join(prefix, 'node_modules/@pippit-dev/cli');
      const binary = path.join(packageRoot, 'bin', platform === 'win32' ? 'pippit-tool-cli.exe' : 'pippit-tool-cli');
      fs.mkdirSync(path.dirname(binary), { recursive: true });
      fs.writeFileSync(path.join(packageRoot, 'package.json'), JSON.stringify({ name: '@pippit-dev/cli' }));
      fs.writeFileSync(binary, 'fixture');
      assert.strictEqual(resolveCLI({ platform, packageRoot, searchPath: '' }).command, binary);
      if (platform === 'win32') {
        assert.strictEqual(resolveCLI({ platform, packageRoot: dir, searchPath: prefix }).command, binary);
      }
    }
    assert.throws(() => resolveCLI({ packageRoot: dir, searchPath: '' }), /pippit-tool-cli login/);

    // Real subprocess bridge: stdin, arguments, bounded wait and login guidance.
    const fixture = path.join(dir, 'fake cli.js');
    fs.writeFileSync(fixture, `let input = ''; process.stdin.on('data', c => input += c); process.stdin.on('end', () => console.log(JSON.stringify({args: process.argv.slice(2), input})));`);
    const invocation = { command: process.execPath, args: [fixture] };
    const bridge = await invokeCLI(['marketing', 'generate', '--request', '-'], JSON.stringify(body), 3000, invocation);
    assert.deepStrictEqual(bridge.args, ['marketing', 'generate', '--request', '-']);
    assert.deepStrictEqual(JSON.parse(bridge.input), body);
    fs.writeFileSync(fixture, `console.error('本次空间：团队空间（team_id=12）；积分账户：团队'); console.error('本地空间记录不可用，将使用显式 --team-id；本次不保存空间记录。'); console.error('请求已成功，但空间记录保存失败：test；不要重复提交。'); console.log(JSON.stringify({ret:'0',team_id:'12'}));`);
    const originalWrite = process.stderr.write;
    let warning = '';
    try {
      process.stderr.write = chunk => { warning += chunk; return true; };
      const success = await invokeCLI(['marketing', 'generate'], undefined, 3000, invocation);
      assert.strictEqual(success.ret, '0');
    } finally { process.stderr.write = originalWrite; }
    assert(warning.includes('不要重复提交'), 'successful submission must preserve record warnings');
    assert(warning.includes('本次空间：'), 'successful submission must preserve selected space');
    assert(warning.includes('本地空间记录不可用'), 'successful submission must preserve missing record notice');
    // Filter unrelated stderr while preserving the two CLI upgrade notices.
    const versionNotice = '[pippit-tool-cli] New version available: 1.0.41 -> 1.0.42';
    const loginNotice = '[pippit-tool-cli] If browser authorization fails, update the CLI and run pippit-tool-cli login again.';
    const stderrChunks = [
      `backend private log\n ${versionNotice}\n[pippit-tool-cli] New ver`,
      'sion available: 1.0.41 -> 1.0.42\r\n[pippit-tool-cli] If browser author',
      `ization fails, update the CLI and run pippit-tool-cli login again. backend detail\n${loginNotice}`,
    ];
    const rawResult = { ret: 0, data: { exact: '原样 🛍️', values: [false, null, 0] } };
    async function noticeFixture(mode, args = ['marketing', 'balance']) {
      fs.writeFileSync(fixture, `
        const chunks = ${JSON.stringify(stderrChunks)};
        let index = 0;
        function writeNext() {
          if (index < chunks.length) {
            process.stderr.write(chunks[index++]);
            setTimeout(writeNext, 10);
          } else if (${JSON.stringify(mode)} === 'timeout') {
            setInterval(() => {}, 1000);
          } else {
            process.stdout.write(${JSON.stringify(mode === 'help' ? '  --source string  Host identifier\n' : mode === 'invalid' ? 'invalid JSON' : JSON.stringify(rawResult))});
            process.exitCode = ${mode === 'failed' ? 1 : 0};
          }
        }
        writeNext();
      `);
      let stdout = '', stderr = '', result, error;
      const originalOut = process.stdout.write;
      const originalErr = process.stderr.write;
      process.stdout.write = chunk => { stdout += chunk; return true; };
      process.stderr.write = chunk => { stderr += chunk; return true; };
      try {
        try { result = await invokeCLI(args, undefined, mode === 'timeout' ? 300 : 3000, invocation); }
        catch (failure) { error = failure; }
      } finally {
        process.stdout.write = originalOut;
        process.stderr.write = originalErr;
      }
      assert.strictEqual(stdout, '', 'CLI bridge must never write notices or data to parent stdout');
      return { result, error, stderr };
    }
    const successNotice = await noticeFixture('json');
    assert.ifError(successNotice.error);
    assert.deepStrictEqual(successNotice.result, rawResult, 'stderr filtering must preserve JSON data');
    assert.strictEqual(successNotice.stderr, `${versionNotice}\n${loginNotice}\n`, 'mixed and split stderr must relay only complete notice lines, including EOF');
    const helpNotice = await noticeFixture('help', ['marketing', 'generate', '--help']);
    assert.ifError(helpNotice.error);
    assert.strictEqual(helpNotice.result, '  --source string  Host identifier\n');
    assert.strictEqual(helpNotice.stderr, successNotice.stderr);
    const failedNotice = await noticeFixture('failed');
    assert.strictEqual(failedNotice.stderr, '', 'failed CLI stderr must not be printed before returning its error');
    assert.strictEqual(failedNotice.error.message, `CLI 调用失败：${stderrChunks.join('').trim()}；请求未重试。若不支持 marketing 命令，请更新 CLI。`);
    for (const mode of ['invalid', 'timeout']) {
      const failed = await noticeFixture(mode);
      assert.match(failed.error.message, mode === 'timeout' ? /超时/ : /未返回有效 JSON/);
      assert.strictEqual(failed.stderr, '', `${mode} results must not relay upgrade notices`);
    }
    // Standalone Skill + old CLI: probe help, omit unsupported attribution, never replay.
    for (const support of ['new', 'old', 'help-failed']) {
      fs.writeFileSync(fixture, `
        if (process.argv.includes('--help')) {
          if (${JSON.stringify(support)} === 'help-failed') process.exit(1);
          console.log(${JSON.stringify(support === 'new' ? '  --source string  Host identifier' : '  --request string  Request JSON')});
        } else if (${JSON.stringify(support)} !== 'new' && process.argv.includes('--source')) {
          console.error('unknown flag: --source'); process.exitCode = 1;
        } else {
          process.stdin.resume();
          process.stdin.on('end', () => console.log(JSON.stringify({ret: 0, data: {run: ${JSON.stringify(ids)}}})));
        }
      `);
      process.env.CODEX_THREAD_ID = 'fixture-session';
      for (const sourceArgs of [[], ['--source', 'workbuddy'], ['--source', '']]) {
        const legacyCalls = [];
        const legacyClient = createClient({ invoke: async (...args) => {
          legacyCalls.push(args);
          return invokeCLI(...args, invocation);
        } });
        assert.strictEqual(await main(['generate', '--request', file, '--execute', ...sourceArgs], { out: () => {}, clientFactory: () => legacyClient }), 0);
        assert.strictEqual(legacyCalls.length, 2, 'one help probe and exactly one submission');
        assert.deepStrictEqual(legacyCalls[0][0], ['marketing', 'generate', '--help']);
        const submitted = legacyCalls[1];
        assert.strictEqual(submitted[0].includes('--source'), support === 'new');
        if (support === 'new') assert.deepStrictEqual(submitted[0].slice(-2), ['--source', sourceArgs[1] || 'codex']);
        assert.deepStrictEqual(JSON.parse(submitted[1]), body);
      }
      delete process.env.CODEX_THREAD_ID;
    }
    fs.writeFileSync(fixture, `console.error('请先运行 pippit-tool-cli login'); process.exitCode = 1;`);
    await assert.rejects(invokeCLI(['marketing', 'balance'], undefined, 3000, invocation), /pippit-tool-cli login/);
    fs.writeFileSync(fixture, `console.log('invalid JSON');`);
    await assert.rejects(invokeCLI(['marketing', 'balance'], undefined, 3000, invocation), /未返回有效 JSON/);
    fs.writeFileSync(fixture, `setInterval(() => {}, 1000);`);
    await assert.rejects(invokeCLI(['marketing', 'balance'], undefined, 50, invocation), /超时/);

    const queryArgs = ['query', '--thread-id', ids.thread_id, '--run-id', ids.run_id];
    const seenActions = [];
    let states = [1, '2', 7, '8', 3];
    let clock = 0;
    const outputs = [];
    const pollBindings = [];
    const runtime = {
      out: line => outputs.push(JSON.parse(line)), now: () => clock, pause: async ms => { clock += ms; },
      clientFactory: options => { pollBindings.push(options); return { api: async (action, input) => {
        seenActions.push(action); assert.deepStrictEqual(input, ids);
        return response(states.shift(), { video_urls: ['https://cdn.example/final.mp4'] });
      } }; },
    };
    assert.strictEqual(await main([...queryArgs, '--wait'], runtime), 0);
    assert.deepStrictEqual(seenActions, ['query', 'query', 'query', 'query', 'query']);
    assert.strictEqual(outputs[0].data.run_state, 3);
    assert.strictEqual(clock, 40000);
    assert(pollBindings.some(binding => binding.teamID === '12' && binding.spaceContext === 'fixed-account-origin-team'), 'subsequent polls must pin account, origin, and team');
    let changedPoll = 0;
    await assert.rejects(main([...queryArgs, '--wait'], {
      out: () => {}, pause: async () => {},
      clientFactory: () => ({ api: async () => ++changedPoll === 1 ? response(2) : { ...response(3), space_context: 'different-account' } }),
    }), /发生变化/);
    let fixedCall;
    const fixedClient = createClient({ teamID: '00012', spaceContext: 'scope-token', invoke: async args => { fixedCall = args; return response(3); } });
    await fixedClient.api('query', ids);
    assert.deepStrictEqual(fixedCall.slice(4, 8), ['--team-id', '12', '--space-context', 'scope-token']);
    await assert.rejects(main([...queryArgs, '--team-id', '9223372036854775808'], { clientFactory: () => { throw new Error('must reject before API'); } }), /team-id/);

    // An explicitly selected space can be polled without local account records.
    for (const teamID of ['0', '12', '34']) {
      const bindings = [];
      let polls = 0;
      const resultWithoutAccount = state => ({ ...response(state, { video_urls: ['https://cdn.example/final.mp4'] }), team_id: teamID, space_context: '' });
      assert.strictEqual(await main([...queryArgs, '--wait', '--team-id', teamID], {
        out: () => {}, pause: async () => {},
        clientFactory: options => { bindings.push(options); return { api: async () => resultWithoutAccount(++polls === 1 ? 2 : 3) }; },
      }), 0);
      assert.strictEqual(polls, 2);
      assert(bindings.every(binding => binding.teamID === teamID && !binding.spaceContext));
    }
    let unboundPolls = 0;
    await assert.rejects(main([...queryArgs, '--wait', '--team-id', '12'], {
      out: () => {}, pause: async () => {},
      clientFactory: () => ({ api: async () => ({ ...response(++unboundPolls === 1 ? 2 : 3), team_id: unboundPolls === 1 ? '12' : '34', space_context: '' }) }),
    }), /任务空间发生变化/);
    await assert.rejects(main([...queryArgs, '--wait'], {
      out: () => {}, pause: async () => {},
      clientFactory: () => ({ api: async () => ({ ...response(2), space_context: '' }) }),
    }), /原 --team-id/);

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
    let before = received.length;
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
    cliResult = response('3', { video_urls: ['https://cdn.example/v.mp4'], image_urls: ['https://cdn.example/i.png'] });
    respond = (_, res) => { res.setHeader('Content-Type', 'application/octet-stream'); res.end(media); };
    const delivery = [];
    assert.strictEqual(await main([...queryArgs, '--output-dir', path.join(dir, 'results')], { out: line => delivery.push(JSON.parse(line)), clientFactory: () => client }), 0);
    assert.strictEqual(delivery[0].data.run_id, ids.run_id);
    assert.strictEqual(delivery[delivery.length - 1].downloaded_files.length, 2);
    for (const file of delivery[delivery.length - 1].downloaded_files) assert.deepStrictEqual(fs.readFileSync(file.path), media);

    const executable = path.resolve(__dirname, '../skills/xyq-marketing-skill/scripts/marketing.js');
    const help = spawnSync(process.execPath, [executable, '--help'], { encoding: 'utf8', env: { ...process.env, XYQ_ACCESS_KEY: '' } });
    assert.strictEqual(help.status, 0);
    assert(help.stdout.includes('generate --request'));
    console.log('Marketing Skill: CLI login reuse, stdin transport, no replay, polling and media delivery passed');
  } finally {
    await new Promise(resolve => server.close(resolve));
    fs.rmSync(dir, { recursive: true, force: true });
  }
}
test().catch(error => { console.error(error); process.exitCode = 1; });
