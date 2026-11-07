// Janela Terminal: SSH até o servidor, desenhado com o xterm.js (carregado só quando abre).
// Senha e chave vão direto para o BrunOS abrir a conexão e não ficam guardadas em lugar nenhum.
import { api } from './api.js';
import { $, esc, svg, openDialog, toast, toastError } from './ui.js';

let xtermMod = null;
async function loadXterm() {
  if (xtermMod) return xtermMod;
  if (!document.querySelector('link[data-xterm]')) {
    const l = document.createElement('link');
    l.rel = 'stylesheet'; l.href = 'vendor/xterm/xterm.css'; l.dataset.xterm = '1';
    document.head.append(l);
  }
  const [{ Terminal }, { FitAddon }] = await Promise.all([import('../vendor/xterm/xterm.mjs'), import('../vendor/xterm/addon-fit.mjs')]);
  xtermMod = { Terminal, FitAddon };
  return xtermMod;
}

const THEME = {
  background: 'rgba(0,0,0,0)', foreground: '#e8eaf0', cursor: '#ffffff', cursorAccent: '#101218',
  selectionBackground: 'rgba(255,255,255,.22)',
  black: '#1d2029', red: '#ff6b6b', green: '#5fd38a', yellow: '#f5c542', blue: '#6aa8ff', magenta: '#c792ea', cyan: '#5fd0e6', white: '#d8dbe3',
  brightBlack: '#5c6370', brightRed: '#ff8a8a', brightGreen: '#8ff0b1', brightYellow: '#ffd76a', brightBlue: '#9cc4ff', brightMagenta: '#dcb2ff', brightCyan: '#8fe6f5', brightWhite: '#ffffff',
};

let openWin = null;
/**
 * ctx: { settings, saveSetting(k, v), defaultHost }
 */
export function terminalWindow(ctx) {
  if (openWin) { openWin.focus(); return; }
  const st = ctx.settings;
  let lastReq = null, session = null, term = null, fit = null, stopStream = null, ro = null, sendTimer = 0, pending = '', fontSize = st.termFontSize || 13;
  const d = openDialog(`<div class="term-win">
      <div class="term-head">
        <span class="term-title">${svg('terminal')}<b data-t="title">Terminal</b><small data-t="sub">SSH até o servidor</small></span>
        <span class="term-tools" hidden>
          <button class="icon-btn" data-t="smaller" title="Diminuir fonte" aria-label="Diminuir fonte">${svg('minus')}</button>
          <button class="icon-btn" data-t="bigger" title="Aumentar fonte" aria-label="Aumentar fonte">${svg('plus')}</button>
          <button class="pill-btn" data-t="disconnect">${svg('logout')}Desconectar</button>
        </span>
      </div>
      <div class="term-body"></div>
    </div>`, { width: 980, cls: 'term-dlg', backdropClose: false, onClose: () => cleanup() });
  openWin = { focus: () => term?.focus() };
  const body = $('.term-body', d.el);
  const setHead = (title, sub) => { $('[data-t="title"]', d.el).textContent = title; $('[data-t="sub"]', d.el).textContent = sub; };

  function cleanup() {
    openWin = null;
    stopStream?.(); ro?.disconnect(); clearTimeout(sendTimer);
    if (session) api.term.close(session.id).catch(() => {});
    session = null;
    term?.dispose(); term = null;
  }

  // ── 1. formulário de conexão ─────────────────────────────────────
  function showForm(msg = '', prev = null) {
    $('.term-tools', d.el).hidden = true;
    setHead('Terminal', 'SSH até o servidor');
    const method = st.sshMethod || 'password';
    body.innerHTML = `<form class="term-form" autocomplete="off">
      <p class="term-lead">Abre um terminal no servidor por SSH, com o seu usuário. A senha (ou a chave) só é usada para conectar: o BrunOS não guarda.</p>
      ${msg ? `<div class="term-msg">${svg('alert')}<span>${esc(msg)}</span></div>` : ''}
      <div class="term-grid">
        <label class="fld fld-host"><span>Servidor</span><input class="text-input" name="host" required value="${esc(prev?.host || st.sshHost || ctx.defaultHost || '')}" placeholder="192.168.1.50"></label>
        <label class="fld fld-port"><span>Porta</span><input class="text-input" name="port" type="number" min="1" max="65535" value="${esc(String(prev?.port || st.sshPort || 22))}"></label>
        <label class="fld fld-user"><span>Usuário</span><input class="text-input" name="user" required value="${esc(prev?.user || st.sshUser || '')}" placeholder="ptbruno" autocapitalize="off" spellcheck="false"></label>
      </div>
      <div class="seg term-method"><button type="button" data-m="password" class="${method === 'password' ? 'on' : ''}">Senha</button><button type="button" data-m="key" class="${method === 'key' ? 'on' : ''}">Chave privada</button></div>
      <div data-auth="password" ${method === 'password' ? '' : 'hidden'}>
        <label class="fld"><span>Senha do usuário no servidor</span><input class="text-input" name="password" type="password" autocomplete="off"></label>
      </div>
      <div data-auth="key" ${method === 'key' ? '' : 'hidden'}>
        <label class="fld"><span>Chave privada (conteúdo do arquivo, ex.: ~/.ssh/id_ed25519)</span><textarea class="text-input term-key" name="key" spellcheck="false" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"></textarea></label>
        <label class="fld"><span>Senha da chave (se tiver)</span><input class="text-input" name="passphrase" type="password" autocomplete="off"></label>
      </div>
      <label class="term-remember"><input type="checkbox" name="remember" ${(prev ? prev.remember : st.sshUser) ? 'checked' : ''}><span class="map-check">${svg('check')}</span>Lembrar servidor e usuário neste BrunOS</label>
      <div class="dlg-actions"><button type="submit" class="pill-btn primary">${svg('terminal')}Conectar</button></div>
    </form>`;
    const f = $('form', body);
    f.querySelectorAll('[data-m]').forEach((b) => b.onclick = () => {
      f.querySelectorAll('[data-m]').forEach((x) => x.classList.toggle('on', x === b));
      f.querySelectorAll('[data-auth]').forEach((x) => { x.hidden = x.dataset.auth !== b.dataset.m; });
      ctx.saveSetting('sshMethod', b.dataset.m);
    });
    setTimeout(() => (f.user.value ? (method === 'key' ? f.key : f.password) : f.user).focus(), 50);
    f.onsubmit = (e) => {
      e.preventDefault();
      const isKey = f.querySelector('[data-m="key"]').classList.contains('on');
      const req = { host: f.host.value.trim(), port: +f.port.value || 22, user: f.user.value.trim(), password: isKey ? '' : f.password.value, key: isKey ? f.key.value : '', passphrase: isKey ? f.passphrase.value : '' };
      if (!req.password && !req.key) { (isKey ? f.key : f.password).focus(); return; }
      req.remember = f.remember.checked;
      if (f.remember.checked) { ctx.saveSetting('sshHost', req.host); ctx.saveSetting('sshPort', req.port); ctx.saveSetting('sshUser', req.user); }
      else { ctx.saveSetting('sshHost', ''); ctx.saveSetting('sshUser', ''); }
      connect(req, $('button[type=submit]', f));
    };
  }

  // ── 2. conexão (e confirmação da chave do servidor) ──────────────
  async function connect(req, btn) {
    if (btn) { btn.disabled = true; btn.classList.add('busy'); }
    const size = guessSize();
    try {
      const r = await api.term.open({ ...req, cols: size.cols, rows: size.rows });
      req.password = req.key = req.passphrase = '';
      lastReq = { host: req.host, port: req.port, user: req.user, remember: req.remember };
      await startTerminal(r);
    } catch (err) {
      if (btn) { btn.disabled = false; btn.classList.remove('busy'); }
      const data = err.data || {};
      if (err.status === 409 && data.unknownHost) return confirmHost(req, data);
      if (err.status === 409 && data.hostKeyChanged) return keyChanged(req, data);
      if (err.status === 401) return; // sessão do BrunOS expirou: a tela de login aparece sozinha
      showForm(err.message, req);
    }
  }
  function confirmHost(req, data) {
    const kt = (data.keyType || '').replace('ssh-', '').toUpperCase();
    const file = { 'ssh-ed25519': 'ed25519', 'ssh-rsa': 'rsa' }[data.keyType] || 'ecdsa';
    body.innerHTML = `<div class="term-trust">
      <span class="term-trust-ico">${svg('shield')}</span>
      <h3>Primeira conexão com ${esc(data.addr)}</h3>
      <p>Para ter certeza de que é mesmo o seu servidor, confira a impressão digital da chave ${esc(kt)} dele:</p>
      <code class="term-fp">${esc(data.fingerprint)}</code>
      <p class="term-hint">No servidor (ou no terminal que você já usa), rode<br><code>ssh-keygen -lf /etc/ssh/ssh_host_${file}_key.pub</code><br>e veja se aparece o mesmo código. O BrunOS guarda essa chave e avisa se um dia ela mudar.</p>
      <div class="dlg-actions"><button class="pill-btn" data-c="no">Voltar</button><button class="pill-btn primary" data-c="ok">${svg('check')}É o meu servidor, conectar</button></div>
    </div>`;
    $('[data-c="no"]', body).onclick = () => showForm('', req);
    $('[data-c="ok"]', body).onclick = (e) => connect({ ...req, trust: data.fingerprint }, e.currentTarget);
  }
  function keyChanged(req, data) {
    body.innerHTML = `<div class="term-trust danger">
      <span class="term-trust-ico">${svg('alert')}</span>
      <h3>A chave de ${esc(data.addr)} mudou</h3>
      <p>Isso acontece quando o sistema do servidor foi reinstalado ou o SSH foi reconfigurado. Se não foi o caso, alguém pode estar se passando pelo servidor na sua rede. A conexão foi recusada.</p>
      <div class="term-fps"><div><small>Chave guardada</small><code class="term-fp">${esc(data.known)}</code></div><div><small>Chave apresentada agora</small><code class="term-fp">${esc(data.fingerprint)}</code></div></div>
      <div class="dlg-actions"><button class="pill-btn" data-c="no">Voltar</button><button class="pill-btn danger" data-c="forget">Esquecer a chave antiga</button></div>
    </div>`;
    $('[data-c="no"]', body).onclick = () => showForm('', req);
    $('[data-c="forget"]', body).onclick = async () => {
      try { await api.term.forget(req.host, req.port); toast('Chave esquecida', 'Confirme a nova impressão digital para conectar.'); showForm('', req); }
      catch (err) { toastError(err); }
    };
  }

  // ── 3. terminal ──────────────────────────────────────────────────
  function guessSize() {
    const w = Math.max(320, body.clientWidth - 24), h = Math.max(200, (innerHeight * 0.62) - 24);
    return { cols: Math.floor(w / (fontSize * 0.61)), rows: Math.floor(h / (fontSize * 1.32)) };
  }
  async function startTerminal(r) {
    session = r;
    const { Terminal, FitAddon } = await loadXterm();
    if (!openWin) { api.term.close(r.id).catch(() => {}); return; }
    setHead(`${r.user}@${r.addr.replace(/:22$/, '')}`, 'conectado por SSH');
    $('.term-tools', d.el).hidden = false;
    body.innerHTML = '<div class="term-screen"></div><div class="term-ended" hidden></div>';
    term = new Terminal({
      fontFamily: 'ui-monospace, SFMono-Regular, "JetBrains Mono", Menlo, Consolas, "Liberation Mono", monospace',
      fontSize, lineHeight: 1.2, cursorBlink: true, allowTransparency: true, scrollback: 5000, theme: THEME,
      macOptionIsMeta: true, rightClickSelectsWord: true,
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    term.open($('.term-screen', body));
    fit.fit();
    api.term.resize(r.id, term.cols, term.rows).catch(() => {});
    term.focus();
    // teclas: junta o que for digitado em poucos ms num único envio
    term.onData((data) => {
      if (!session) return;
      pending += data;
      if (!sendTimer) sendTimer = setTimeout(flush, 8);
    });
    term.onResize(({ cols, rows }) => { if (session) api.term.resize(session.id, cols, rows).catch(() => {}); });
    ro = new ResizeObserver(() => { try { fit.fit(); } catch { /* janela fechando */ } });
    ro.observe($('.term-screen', body));
    stopStream = api.term.stream(r.id, {
      onData: (u8) => term?.write(u8),
      onExit: (msg) => ended(msg),
      onReplaced: () => ended('esta sessão foi aberta em outra janela'),
      onError: () => ended('conexão com o BrunOS perdida'),
    });
  }
  function flush() {
    sendTimer = 0;
    const data = pending; pending = '';
    if (data && session) api.term.input(session.id, data).catch(() => ended('a sessão não existe mais'));
  }
  function ended(msg) {
    if (!term) return;
    session = null; stopStream?.(); stopStream = null;
    term.write(`\r\n\x1b[2m[${msg}]\x1b[0m\r\n`);
    const box = $('.term-ended', body);
    box.hidden = false;
    box.innerHTML = `<span>${esc(msg[0].toUpperCase() + msg.slice(1))}</span><button class="pill-btn primary" data-t="again">${svg('refresh')}Conectar de novo</button>`;
    $('[data-t="sub"]', d.el).textContent = 'desconectado';
  }

  d.el.addEventListener('click', (e) => {
    const b = e.target.closest('[data-t]'); if (!b) return;
    const k = b.dataset.t;
    if (k === 'disconnect') { if (session) { api.term.close(session.id).catch(() => {}); ended('sessão encerrada'); } }
    else if (k === 'again') { stopStream?.(); ro?.disconnect(); term?.dispose(); term = null; showForm('', lastReq); }
    else if (k === 'bigger' || k === 'smaller') {
      fontSize = Math.min(22, Math.max(10, fontSize + (k === 'bigger' ? 1 : -1)));
      ctx.saveSetting('termFontSize', fontSize);
      if (term) { term.options.fontSize = fontSize; fit.fit(); }
    }
  });
  showForm();
}
