// 仅适配既有本地测试 binary 和证据落盘，不复制营销轮询/鉴权/下载逻辑。
const fs = require('fs');
const path = require('path');
const { main, createClient, invokeCLI } = require('../../skills/xyq-marketing-skill/scripts/marketing');

process.umask(0o077);
const [directory, maxWait = '900'] = process.argv.slice(2);
const out = path.resolve(directory);
const run = JSON.parse(fs.readFileSync(path.join(out, 'submit.json'), 'utf8')).data.run;
const invocation = { command: path.resolve(__dirname, '../../.local/tests/video/pippit-cli-ppe'), args: [] };
const journal = path.join(out, 'queries.jsonl');
function clientFactory(options) {
  return createClient({ ...options, invoke: async (args, input, timeout) => {
    const result = await invokeCLI(args, input, timeout, invocation);
    fs.appendFileSync(journal, JSON.stringify({ time: new Date().toISOString(), result }) + '\n');
    fs.writeFileSync(path.join(out, 'query-result.json'), JSON.stringify(result, null, 2) + '\n');
    console.log(JSON.stringify({ run_id: run.run_id, run_state: result.data?.run_state, log_id: result.log_id }));
    return result;
  }});
}
main(['query', '--thread-id', run.thread_id, '--run-id', run.run_id, '--wait', '--max-wait', maxWait,
  '--output-dir', path.join(out, 'media')], {
  clientFactory,
  out: line => {
    fs.appendFileSync(path.join(out, 'query-output.jsonl'), line + '\n');
    const result = JSON.parse(line);
    if (result.action_required || result.wait_timed_out || result.unknown_state) console.log(line);
    if (result.downloaded_files) console.log(JSON.stringify({ downloaded_files: result.downloaded_files.map(item => item.path) }));
  },
}).then(code => { process.exitCode = code; }).catch(error => {
  fs.appendFileSync(path.join(out, 'query-errors.txt'), error.message + '\n');
  console.error('查询或下载失败，见证据目录 query-errors.txt；保留原 Run，不重提。');
  process.exitCode = 1;
});
