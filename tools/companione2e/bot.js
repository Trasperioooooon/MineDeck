// 两半联调用的假玩家（mineflayer）：进服后一直待着，从标准输入收指令，把收到的聊天和系统消息逐行写成 JSON。
//
// 用法：NODE_PATH=<node_modules> node bot.js --port <游戏端口> --name <名字> --version 1.21.1|1.20.1 --mode neoforge|forge
//   neoforge：配置阶段发 minecraft:register，声明有 minedeck_companion:hello 通道（假装客户端装了伙伴模组，只装它）；
//   forge   ：模拟 Forge 1.20.1 客户端的 FML3 登录握手，模组清单和通道照抄服务器发来的（假装 3 个模组都装了）。
// 标准输入一行一条：chat <一句话> / pos / quit。
// 标准输出一行一个 JSON：{"t":毫秒,"ev":"login|spawn|msg|pos|kicked|end|error|log", ...}；msg 的 text 是 mineflayer 的 messagestr。
const mineflayer = require('mineflayer');
const readline = require('readline');

const argv = process.argv.slice(2);
const opt = (k, d) => { const i = argv.indexOf('--' + k); return i >= 0 ? argv[i + 1] : d; };
const port = Number(opt('port', '25565'));
const name = opt('name', 'Steve');
const version = opt('version', '1.21.1');
const mode = opt('mode', 'neoforge');
const out = (o) => process.stdout.write(JSON.stringify(Object.assign({ t: Date.now() }, o)) + '\n');

const forge = mode === 'forge';
const bot = mineflayer.createBot({ host: '127.0.0.1', port, username: name, version, auth: 'offline',
  fakeHost: forge ? '127.0.0.1\0FML3\0' : undefined, hideErrors: true });

if (!forge) {
  bot._client.on('state', (s) => {
    if (s === 'configuration') {
      bot._client.write('custom_payload', { channel: 'minecraft:register', data: Buffer.from('minedeck_companion:hello') });
      out({ ev: 'log', text: 'sent minecraft:register minedeck_companion:hello' });
    }
  });
} else {
  // FML3：格式按 Forge 47.4.28 的 HandshakeMessages / IndexedMessageCodec（一字节的消息编号）
  const readVarInt = (buf, off) => { let n = 0, shift = 0, b; do { b = buf[off++]; n |= (b & 0x7f) << shift; shift += 7; } while (b & 0x80); return [n >>> 0, off]; };
  const writeVarInt = (n) => { const o = []; do { let b = n & 0x7f; n >>>= 7; if (n) b |= 0x80; o.push(b); } while (n); return Buffer.from(o); };
  const readString = (buf, off) => { const [len, o] = readVarInt(buf, off); return [buf.slice(o, o + len).toString('utf8'), o + len]; };
  const writeString = (s) => { const b = Buffer.from(s, 'utf8'); return Buffer.concat([writeVarInt(b.length), b]); };
  const wrap = (index, payload) => {
    const inner = Buffer.concat([Buffer.from([index]), payload]);
    return Buffer.concat([writeString('fml:handshake'), writeVarInt(inner.length), inner]);
  };
  bot._client.removeAllListeners('login_plugin_request');
  // Forge 发给客户端的命令树里有模组的参数类型，minecraft-protocol 认为它不合法、会自己断开；测试用不到命令
  const origEnd = bot._client.end.bind(bot._client);
  bot._client.end = (reason) => { if (reason === 'impossibleCommandTree') { out({ ev: 'log', text: 'ignore impossibleCommandTree' }); return; } origEnd(reason); };
  bot._client.on('login_plugin_request', (p) => {
    if (p.channel !== 'fml:loginwrapper' || !p.data) {
      bot._client.write('login_plugin_response', { messageId: p.messageId });
      return;
    }
    let [, o] = readString(p.data, 0);
    let len; [len, o] = readVarInt(p.data, o);
    const msg = p.data.slice(o, o + len);
    if (msg[0] === 1) { // S2CModList：照抄
      let off = 1, n;
      const mods = [];
      [n, off] = readVarInt(msg, off);
      for (let i = 0; i < n; i++) { let s; [s, off] = readString(msg, off); mods.push(s); }
      const channels = [];
      [n, off] = readVarInt(msg, off);
      for (let i = 0; i < n; i++) { let k, v; [k, off] = readString(msg, off); [v, off] = readString(msg, off); channels.push([k, v]); }
      out({ ev: 'log', text: 'S2CModList: ' + mods.join(',') });
      const parts = [writeVarInt(mods.length), ...mods.map(writeString), writeVarInt(channels.length)];
      for (const [k, v] of channels) parts.push(writeString(k), writeString(v));
      parts.push(writeVarInt(0));
      bot._client.write('login_plugin_response', { messageId: p.messageId, data: wrap(2, Buffer.concat(parts)) });
    } else { // 注册表、配置……一律回 C2SAcknowledge
      bot._client.write('login_plugin_response', { messageId: p.messageId, data: wrap(99, Buffer.alloc(0)) });
    }
  });
}

const pos = () => bot.entity ? { x: bot.entity.position.x, y: bot.entity.position.y, z: bot.entity.position.z } : null;
bot.on('login', () => out({ ev: 'login' }));
bot.on('spawn', () => out(Object.assign({ ev: 'spawn' }, pos())));
bot.on('messagestr', (text, position) => out({ ev: 'msg', text, position }));
bot.on('kicked', (reason) => out({ ev: 'kicked', reason: typeof reason === 'string' ? reason : JSON.stringify(reason) }));
bot.on('error', (e) => out({ ev: 'error', text: String(e && e.message) }));
bot.on('end', (r) => { out({ ev: 'end', reason: String(r) }); process.exit(0); });
process.on('uncaughtException', (e) => out({ ev: 'error', text: 'uncaught: ' + String(e && e.message) }));

readline.createInterface({ input: process.stdin }).on('line', (line) => {
  const [cmd, ...rest] = line.split(' ');
  const arg = rest.join(' ');
  try {
    if (cmd === 'chat') { bot.chat(arg); out({ ev: 'log', text: 'chat sent: ' + arg }); }
    else if (cmd === 'pos') out(Object.assign({ ev: 'pos' }, pos()));
    else if (cmd === 'quit') bot.quit('done');
  } catch (e) { out({ ev: 'error', text: String(e && e.message) }); }
}).on('close', () => bot.quit('stdin closed'));
