// Janela Terminal (K1–K3, termos em CONTEXT.md): SSH até o servidor, desenhado com o xterm.js
// (carregado só quando abre).
//   - Abas: até 4 sessões SSH. Só a Aba da frente recebe a tela ao vivo (uma conexão só com o
//     BrunOS); ao trocar, o servidor manda o fim da tela da outra. As de trás mandam um sinal de
//     vida a cada 30 s para não contarem como "sem ninguém olhando".
//   - "–" minimiza: a janela some, as Abas continuam rodando no servidor (ícone na dock). "×" fecha
//     todas, conferindo antes se algo ainda roda.
//   - Conexões salvas: a principal conecta sozinha com a Chave do Terminal (trancada pela senha do
//     BrunOS; a chave nunca vem para o navegador).
//   - Comandos salvos: botões que digitam o comando (e apertam Enter só se "executar direto").
// Senha e chave colada vão direto para o BrunOS abrir a conexão e não ficam guardadas.
import { api } from './api.js';
import { $, $$, esc, svg, on, openDialog, confirmDialog, contextMenu, toast, toastError } from './ui.js';

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
const MAX_TABS = 4;
const portValue = (p) => (p && +p !== 22 ? String(p) : '');
const isPhone = () => matchMedia('(max-width: 640px)').matches;
const rid = () => Math.random().toString(36).slice(2, 10);
const shortAddr = (a) => String(a || '').replace(/:22$/, '');

let openWin = null;   // janela aberta: { focus, snippets(), send(s) }
let knownTabs = 0;    // quantas Abas existem (para a dock, mesmo com a janela fechada)

/** Quantas Abas estão abertas no servidor (a dock mostra o Terminal enquanto houver). */
export const tabCount = () => knownTabs;

/** Ações da busca (Ctrl+K): os Comandos salvos digitam na Aba da frente. */
export function paletteActions() {
  if (!openWin) return [];
  return openWin.snippets().map((s) => ({ icon: 'terminal', label: `Terminal: ${s.label}${s.run ? '' : ' (só digita)'}`, run: () => openWin?.send(s) }));
}

// Conexões salvas: migra a "lembrança" antiga (sshHost/sshUser) na primeira vez
function connections(st, save) {
  if (!Array.isArray(st.termConnections)) st.termConnections = [];
  if (!st.termConnections.length && st.sshUser) {
    st.termConnections = [{ id: rid(), name: 'Servidor', host: st.sshHost, port: st.sshPort || 22, user: st.sshUser, method: st.sshMethod === 'key' ? 'paste' : 'password', main: true }];
    save('termConnections', st.termConnections);
  }
  return st.termConnections;
}
const mainConn = (list) => list.find((c) => c.main) || list[0] || null;

/**
 * ctx: { settings, saveSetting(k, v), defaultHost, onTabs() }
 */
export function terminalWindow(ctx) {
  // ctx.typeCmd: abre com um comando já digitado na Aba da frente (sem Enter), ex.: atualizar o Linux
  if (openWin) { openWin.focus(); if (ctx.typeCmd) openWin.send({ cmd: ctx.typeCmd, run: false }); return; }
  let pendingType = ctx.typeCmd || '';
  const st = ctx.settings;
  const conns = () => connections(st, ctx.saveSetting);
  if (!Array.isArray(st.termSnippets)) st.termSnippets = [];
  let tabs = [], active = -1, view = 'loading';
  let term = null, fit = null, stopStream = null, ro = null, sendTimer = 0, pending = '', keyInfo = null, offUnlock = null, pingTimer = 0;
  let fontSize = st.termFontSize || (isPhone() ? 11 : 13);

  const d = openDialog(`<div class="term-win">
      <div class="term-head">
        <div class="term-tabs" role="tablist"></div>
        <span class="term-add">
          <button class="icon-btn" data-t="new" title="Nova aba" aria-label="Nova aba">${svg('plus')}</button>
          <button class="icon-btn" data-t="new-pick" title="Nova aba em…" aria-label="Escolher onde abrir">${svg('chevD')}</button>
        </span>
        <span class="f-spacer"></span>
        <span class="term-tools">
          <button class="icon-btn" data-t="snipbar" title="Comandos salvos" aria-label="Mostrar ou esconder os comandos salvos">${svg('code')}</button>
          <button class="icon-btn" data-t="smaller" title="Diminuir fonte" aria-label="Diminuir fonte"><b class="t-a sm">A</b></button>
          <button class="icon-btn" data-t="bigger" title="Aumentar fonte" aria-label="Aumentar fonte"><b class="t-a">A</b></button>
          <button class="icon-btn" data-t="config" title="Chave, conexões e comandos" aria-label="Configurar o terminal">${svg('sliders')}</button>
        </span>
        <span class="term-win-btns">
          <button class="icon-btn" data-t="minimize" title="Minimizar (as abas continuam rodando)" aria-label="Minimizar">${svg('minus')}</button>
          <button class="icon-btn" data-t="closeall" title="Fechar todas as abas" aria-label="Fechar">${svg('x')}</button>
        </span>
      </div>
      <div class="term-snips" hidden></div>
      <div class="term-body"></div>
    </div>`, { width: 1000, cls: 'term-dlg', backdropClose: false, closeBtn: false, onClose: () => detachAll() });
  const body = $('.term-body', d.el);

  openWin = {
    focus: () => term?.focus(),
    snippets: () => (tabs[active] && !tabs[active].ended ? st.termSnippets.filter((s) => s.cmd) : []),
    send: (s) => sendSnippet(s),
  };

  function setCount() { knownTabs = tabs.filter((t) => !t.ended).length; ctx.onTabs?.(); }

  // Fechar a janela (Esc, "–") só desliga a tela; as Abas continuam no servidor
  function detachAll() {
    openWin = null;
    stopStream?.(); stopStream = null;
    ro?.disconnect(); clearTimeout(sendTimer); clearInterval(pingTimer);
    offUnlock?.();
    term?.dispose(); term = null;
    setCount();
  }
  function dropScreen() { stopStream?.(); stopStream = null; ro?.disconnect(); term?.dispose(); term = null; }

  // ── Abas ─────────────────────────────────────────────────────────
  const connOf = (t) => conns().find((x) => x.host && `${x.host}:${x.port || 22}` === t.addr && x.user === t.user);
  const baseTitle = (t) => { const c = connOf(t); return c?.name && conns().length > 1 ? c.name : `${t.user}@${shortAddr(t.addr)}`; };
  // duas Abas no mesmo lugar ganham número: "ptbruno@servidor (2)"
  const tabTitle = (t) => {
    const same = tabs.filter((x) => baseTitle(x) === baseTitle(t));
    return same.length > 1 ? `${baseTitle(t)} (${same.indexOf(t) + 1})` : baseTitle(t);
  };
  function drawTabs() {
    $('.term-tabs', d.el).innerHTML = tabs.map((t, i) => `<span class="term-tab ${i === active && view === 'term' ? 'on' : ''} ${t.ended ? 'ended' : ''}" data-tab="${i}" role="tab" tabindex="0" aria-selected="${i === active}" title="${esc(`${t.user}@${shortAddr(t.addr)}${t.running?.length ? ` · rodando: ${t.running.join(', ')}` : ''}`)}">
        ${t.running?.length ? '<i class="term-run" aria-label="rodando"></i>' : svg('terminal')}<span class="term-tab-name">${esc(tabTitle(t))}</span><button class="term-tab-x" data-tabclose="${i}" aria-label="Fechar aba">${svg('x')}</button></span>`).join('');
    $('[data-t="new"]', d.el).disabled = tabs.length >= MAX_TABS;
    $('[data-t="new-pick"]', d.el).disabled = tabs.length >= MAX_TABS;
    $('.term-tools', d.el).classList.toggle('dim', view !== 'term');
    drawSnips();
  }
  function drawSnips() {
    const box = $('.term-snips', d.el);
    const list = st.termSnippets.filter((s) => s.cmd);
    const show = view === 'term' && st.termSnippetBar !== false && list.length && tabs[active] && !tabs[active].ended;
    box.hidden = !show;
    $('[data-t="snipbar"]', d.el).classList.toggle('on', st.termSnippetBar !== false && list.length > 0);
    if (show) box.innerHTML = st.termSnippets.map((s, i) => s.cmd ? `<button class="term-snip ${s.run ? 'run' : ''}" data-snip="${i}" title="${esc(s.cmd)}${s.run ? ' (executa direto)' : ' (só digita: confira e aperte Enter)'}">${s.run ? svg('chevR') : svg('code')}${esc(s.label || s.cmd)}</button>` : '').join('');
  }

  async function ensureTerm() {
    if (term) return;
    const { Terminal, FitAddon } = await loadXterm();
    if (!openWin || term) return;
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
    // teclas: junta o que for digitado em poucos ms num único envio
    term.onData((data) => {
      if (!tabs[active] || tabs[active].ended) return;
      pending += data;
      if (!sendTimer) sendTimer = setTimeout(flush, 8);
    });
    term.onResize(({ cols, rows }) => { const t = tabs[active]; if (t && !t.ended) api.term.resize(t.id, cols, rows).catch(() => {}); });
    ro = new ResizeObserver(() => { try { fit.fit(); } catch { /* janela fechando */ } });
    ro.observe($('.term-screen', body));
  }
  function flush() {
    sendTimer = 0;
    const data = pending; pending = '';
    const t = tabs[active];
    if (data && t && !t.ended) api.term.input(t.id, data).catch(() => ended(t, 'a sessão não existe mais'));
  }

  async function showTab(i) {
    if (!tabs[i]) return;
    active = i; view = 'term';
    stopStream?.(); stopStream = null;
    if (!$('.term-screen', body)) { ro?.disconnect(); term?.dispose(); term = null; }
    await ensureTerm();
    if (!term || !openWin || active !== i) return;
    const t = tabs[i];
    $('.term-ended', body).hidden = true;
    term.reset();
    drawTabs();
    if (t.ended) { showEnded(t); return; }
    api.term.resize(t.id, term.cols, term.rows).catch(() => {});
    stopStream = api.term.stream(t.id, {
      replay: true,
      onData: (u8) => { if (tabs[active] === t) term?.write(u8); },
      onExit: (msg) => ended(t, msg),
      onReplaced: () => ended(t, 'esta aba foi aberta em outra janela', true),
      onError: () => ended(t, 'conexão com o BrunOS perdida', true),
      onLocked: () => term?.write('\r\n\x1b[2m[tela bloqueada: a aba continua rodando no servidor]\x1b[0m\r\n'),
    });
    setTimeout(() => term?.focus(), 30);
    if (pendingType) { const cmd = pendingType; pendingType = ''; setTimeout(() => sendSnippet({ cmd, run: false }), 900); }
  }
  function ended(t, msg, reattach = false) {
    if (!reattach) { t.ended = msg; setCount(); }
    if (tabs[active] !== t || !term) { drawTabs(); return; }
    stopStream?.(); stopStream = null;
    term.write(`\r\n\x1b[2m[${msg}]\x1b[0m\r\n`);
    showEnded(t, msg, reattach);
    drawTabs();
  }
  function showEnded(t, msg = t.ended, reattach = false) {
    const box = $('.term-ended', body); if (!box) return;
    box.hidden = false;
    box.innerHTML = `<span>${esc(msg[0].toUpperCase() + msg.slice(1))}</span>${reattach ? `<button class="pill-btn primary" data-t="reattach">${svg('refresh')}Mostrar de novo</button>`
      : `<button class="pill-btn" data-t="drop">${svg('x')}Fechar aba</button><button class="pill-btn primary" data-t="again">${svg('refresh')}Conectar de novo</button>`}`;
  }
  async function closeTab(i, ask = true) {
    const t = tabs[i]; if (!t) return;
    if (ask && !t.ended && !(await okToClose([t]))) return;
    if (!t.ended) api.term.close(t.id).catch(() => {});
    tabs.splice(i, 1); setCount();
    if (!tabs.length) { active = -1; dropScreen(); startEmpty(); return; }
    showTab(Math.min(i, tabs.length - 1));
  }

  // "Ainda está rodando?" antes de fechar: pergunta ao servidor (ps por um canal SSH extra)
  async function okToClose(list) {
    const busy = [];
    for (const t of list) {
      try { const b = await api.term.busy(t.id); if (!b.known) busy.push({ t, what: null }); else if (b.names.length) busy.push({ t, what: b.names.join(', ') }); }
      catch { /* a aba já acabou */ }
    }
    if (!busy.length) return true;
    const lines = busy.map(({ t, what }) => `A aba ${tabs.indexOf(t) + 1} (${tabTitle(t)}) ${what ? `ainda está rodando ${what}` : 'pode ter algo rodando (não deu para conferir)'}.`);
    return new Promise((resolve) => {
      let done = false;
      const dd = openDialog(`<h2>Fechar mesmo assim?</h2><p>${lines.map(esc).join('<br>')}</p><p class="hint">Fechar interrompe o que está rodando. Minimizando, a aba continua e o BrunOS avisa no sino quando o comando terminar.</p>
        <div class="dlg-actions"><button class="pill-btn" data-c="no">Cancelar</button><button class="pill-btn" data-c="min">${svg('minus')}Minimizar</button><button class="pill-btn danger-solid" data-c="ok">Fechar mesmo assim</button></div>`,
      { closeBtn: false, width: 470, onClose: () => { if (!done) resolve(false); } });
      dd.el.addEventListener('click', (e) => {
        const b = e.target.closest('[data-c]'); if (!b) return;
        done = true; dd.close();
        if (b.dataset.c === 'min') { resolve(false); setTimeout(() => d.close(), 60); return; }
        resolve(b.dataset.c === 'ok');
      });
    });
  }

  function sendSnippet(s) {
    const t = tabs[active]; if (!t || t.ended || !s?.cmd) return;
    api.term.input(t.id, s.cmd + (s.run ? '\r' : '')).catch(() => ended(t, 'a sessão não existe mais'));
    term?.focus();
  }

  // ── Abrir uma Aba ────────────────────────────────────────────────
  function guessSize() {
    if (term) return { cols: term.cols, rows: term.rows };
    const w = Math.max(320, (body.clientWidth || 900) - 24), h = Math.max(200, (innerHeight * 0.62) - 24);
    return { cols: Math.floor(w / (fontSize * 0.61)), rows: Math.floor(h / (fontSize * 1.32)) };
  }
  async function getKey(force = false) {
    if (!keyInfo || force) { try { keyInfo = await api.term.key.get(); } catch { keyInfo = { exists: false }; } }
    return keyInfo;
  }

  async function openTab(req, btn) {
    if (tabs.length >= MAX_TABS) { toast('Já são 4 abas', 'Feche uma antes de abrir outra.'); return false; }
    if (btn) { btn.disabled = true; btn.classList.add('busy'); }
    if (!btn) { view = 'loading'; dropScreen(); body.innerHTML = `<div class="term-loading">${svg('terminal')}Conectando em ${esc(`${req.user}@${req.host}`)}…</div>`; drawTabs(); }
    const size = guessSize();
    try {
      const r = await api.term.open({ host: req.host, port: req.port, user: req.user, useKey: !!req.useKey, password: req.password || '', key: req.key || '', passphrase: req.passphrase || '', trust: req.trust || '', cols: size.cols, rows: size.rows });
      req.password = req.key = req.passphrase = '';
      if (!openWin) { api.term.close(r.id).catch(() => {}); return false; }
      tabs.push({ id: r.id, addr: r.addr, user: r.user, created: Date.now() });
      setCount();
      await showTab(tabs.length - 1);
      return true;
    } catch (err) {
      if (btn) { btn.disabled = false; btn.classList.remove('busy'); }
      const data = err.data || {};
      if (err.status === 409 && data.unknownHost) return confirmHost(req, data);
      if (err.status === 409 && data.hostKeyChanged) return keyChanged(req, data);
      if (err.status === 401 || err.status === 423) return false; // sessão do BrunOS acabou ou tela bloqueada
      if (data.keyLocked) {
        if (await unlockKey()) return openTab(req, btn);
        showForm('A Chave do Terminal continua trancada: digite a senha do BrunOS para usar, ou entre com a senha do servidor.', { ...req, method: 'password' });
        return false;
      }
      if (data.noKey) { showForm('A Chave do Terminal ainda não foi criada. Crie em ⚙ (Configurar) e autorize no servidor.', { ...req, method: 'password' }); return false; }
      showForm(err.message, req);
      return false;
    }
  }
  // Conecta numa Conexão salva: com a Chave do Terminal direto; com senha, pede a senha
  function openConn(c) {
    if (!c) { showForm(); return; }
    const base = { host: c.host, port: c.port || 22, user: c.user, save: true, method: c.method };
    if (c.method === 'key') return openTab({ ...base, useKey: true });
    showForm('', base);
  }
  // senha do BrunOS → abre a Chave do Terminal para esta Sessão (a chave fica no servidor)
  function unlockKey() {
    return new Promise((resolve) => {
      let done = false;
      const dd = openDialog(`<h2>${svg('key')} Senha do BrunOS</h2><p>A Chave do Terminal fica trancada pela sua senha do BrunOS. Ela é pedida de novo depois de o BrunOS reiniciar ou de a tela ser bloqueada.</p>
        <form class="form-grid"><label>Senha do BrunOS<input class="text-input" type="password" name="pw" autocomplete="current-password" autofocus></label><p class="term-err" role="alert"></p>
        <div class="dlg-actions"><button type="button" class="pill-btn" data-c="no">Cancelar</button><button class="pill-btn primary">${svg('check')}Usar a chave</button></div></form>`,
      { closeBtn: false, width: 420, onClose: () => { if (!done) resolve(false); } });
      const f = $('form', dd.el);
      $('[data-c="no"]', dd.el).onclick = () => dd.close();
      f.onsubmit = async (e) => {
        e.preventDefault();
        try { keyInfo = await api.term.key.unlock(f.pw.value); done = true; dd.close(); resolve(true); }
        catch (x) { $('.term-err', f).textContent = x.message; f.pw.select(); }
      };
    });
  }

  // ── Formulário (outro servidor, ou conexão com senha) ────────────
  async function showForm(msg = '', prev = null) {
    view = 'form'; dropScreen(); drawTabs();
    const k = await getKey();
    if (!openWin || view !== 'form') return;
    const main = mainConn(conns());
    const method = prev?.method && (prev.method !== 'key' || k.exists) ? prev.method : (k.exists ? 'key' : 'password');
    body.innerHTML = `<form class="term-form" autocomplete="off">
      <p class="term-lead">Abre um terminal por SSH. ${k.exists ? 'Com a <b>Chave do Terminal</b> entra direto, sem senha.' : 'A senha (ou a chave colada) só é usada para conectar: o BrunOS não guarda.'}</p>
      ${msg ? `<div class="term-msg">${svg('alert')}<span>${esc(msg)}</span></div>` : ''}
      <div class="term-grid">
        <label class="fld fld-host"><span>Servidor</span><input class="text-input" name="host" required value="${esc(prev?.host || main?.host || ctx.defaultHost || '')}" placeholder="192.168.1.50"></label>
        <label class="fld fld-port"><span>Porta</span><input class="text-input" name="port" type="number" min="1" max="65535" value="${esc(portValue(prev?.port || main?.port))}" placeholder="22"></label>
        <label class="fld fld-user"><span>Usuário</span><input class="text-input" name="user" required value="${esc(prev?.user || main?.user || '')}" placeholder="ptbruno" autocapitalize="off" spellcheck="false"></label>
      </div>
      <div class="seg term-method">${k.exists ? `<button type="button" data-m="key" class="${method === 'key' ? 'on' : ''}">Chave do Terminal</button>` : ''}<button type="button" data-m="password" class="${method === 'password' ? 'on' : ''}">Senha</button><button type="button" data-m="paste" class="${method === 'paste' ? 'on' : ''}">Colar chave</button></div>
      <div data-auth="key" ${method === 'key' ? '' : 'hidden'}><small class="hint">A chave precisa estar autorizada neste servidor (⚙ Configurar → Chave do Terminal mostra o comando).</small></div>
      <div data-auth="password" ${method === 'password' ? '' : 'hidden'}>
        <label class="fld"><span>Senha do usuário no servidor</span><input class="text-input" name="password" type="password" autocomplete="off"></label>
      </div>
      <div data-auth="paste" ${method === 'paste' ? '' : 'hidden'}>
        <label class="fld"><span>Chave privada (conteúdo do arquivo, ex.: ~/.ssh/id_ed25519)</span><textarea class="text-input term-key" name="key" spellcheck="false" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"></textarea></label>
        <label class="fld"><span>Senha da chave (se tiver)</span><input class="text-input" name="passphrase" type="password" autocomplete="off"></label>
      </div>
      <label class="term-remember"><input type="checkbox" name="save" ${(prev ? prev.save : !conns().length) ? 'checked' : ''}><span class="map-check">${svg('check')}</span>Salvar nas Conexões salvas (servidor, usuário e forma de entrar; a senha nunca)</label>
      <div class="dlg-actions">${tabs.length ? '<button type="button" class="pill-btn" data-t="back">Voltar</button>' : ''}<button type="submit" class="pill-btn primary">${svg('terminal')}Conectar</button></div>
    </form>`;
    const f = $('form', body);
    f.querySelectorAll('[data-m]').forEach((b) => b.onclick = () => {
      f.querySelectorAll('[data-m]').forEach((x) => x.classList.toggle('on', x === b));
      f.querySelectorAll('[data-auth]').forEach((x) => { x.hidden = x.dataset.auth !== b.dataset.m; });
    });
    setTimeout(() => (f.user.value ? ({ key: $('button[type=submit]', f), password: f.password, paste: f.key }[method]) : f.user)?.focus(), 50);
    f.onsubmit = async (e) => {
      e.preventDefault();
      const m = f.querySelector('[data-m].on').dataset.m;
      const req = { host: f.host.value.trim(), port: +f.port.value || 22, user: f.user.value.trim(), useKey: m === 'key', password: m === 'password' ? f.password.value : '', key: m === 'paste' ? f.key.value : '', passphrase: m === 'paste' ? f.passphrase.value : '', method: m, save: f.save.checked };
      if (m === 'password' && !req.password) { f.password.focus(); return; }
      if (m === 'paste' && !req.key.trim()) { f.key.focus(); return; }
      if (await openTab(req, $('button[type=submit]', f)) && req.save) remember(req);
    };
  }
  function remember(req) {
    const list = conns();
    let c = list.find((x) => x.host === req.host && +(x.port || 22) === +req.port && x.user === req.user);
    if (!c) { c = { id: rid(), name: list.length ? `${req.user}@${req.host}` : 'Servidor', host: req.host, port: req.port, user: req.user, main: !list.some((x) => x.main) }; list.push(c); }
    c.method = req.method;
    ctx.saveSetting('termConnections', list);
  }
  function confirmHost(req, data) {
    view = 'form'; dropScreen(); drawTabs();
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
    $('[data-c="ok"]', body).onclick = async (e) => { if (await openTab({ ...req, trust: data.fingerprint }, e.currentTarget) && req.save) remember(req); };
    return false;
  }
  function keyChanged(req, data) {
    view = 'form'; dropScreen(); drawTabs();
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
    return false;
  }

  // ── ⚙ Chave do Terminal, Conexões salvas e Comandos salvos ───────
  async function showConfig() {
    view = 'config'; dropScreen(); drawTabs();
    const k = await getKey(true);
    if (!openWin || view !== 'config') return;
    const list = conns();
    const add = k.publicKey ? `mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo '${k.publicKey}' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys` : '';
    const live = tabs[active] && !tabs[active].ended;
    body.innerHTML = `<div class="term-config">
      <section class="tc-sec"><h3>${svg('key')}Chave do Terminal</h3>
        ${k.exists ? `<p>Uma chave SSH só do Terminal, criada pelo BrunOS e trancada pela sua senha do BrunOS. Para entrar direto num computador, cole este comando nele uma vez (por exemplo numa aba aberta com a senha):</p>
          <pre class="pw-cmds tc-cmd" tabindex="0">${esc(add)}</pre>
          <div class="tc-row"><button class="pill-btn" data-c="copy">${svg('copy')}Copiar</button>${live ? `<button class="pill-btn" data-c="type">${svg('terminal')}Digitar na aba ${active + 1}</button>` : ''}<span class="f-spacer"></span><small class="hint">${esc(k.fingerprint || '')}</small></div>
          <p class="hint">Para tirar o acesso: apague a chave aqui e, no servidor, rode <code>sed -i '/brunos-terminal$/d' ~/.ssh/authorized_keys</code>. Suas outras chaves e a senha continuam funcionando.</p>
          <div class="tc-row"><span class="v1-note ${k.unlocked ? 'ok' : 'off'}">${k.unlocked ? 'destrancada nesta sessão' : 'trancada: pede a senha do BrunOS'}</span><span class="f-spacer"></span><button class="pill-btn danger" data-c="delkey">${svg('trash')}Apagar a chave</button></div>`
        : `<p>Com a Chave do Terminal, o Terminal entra direto no servidor, sem pedir a senha do SSH. Ela fica guardada no BrunOS <b>cifrada pela sua senha do BrunOS</b>: quem copiar os dados do BrunOS não consegue usá-la.</p>
          <div class="tc-row"><button class="pill-btn primary" data-c="newkey">${svg('key')}Criar a Chave do Terminal</button></div>`}
      </section>
      <section class="tc-sec"><h3>${svg('server')}Conexões salvas</h3>
        <p class="hint">A principal (estrela) abre sozinha ao abrir o Terminal e no “+”. A setinha ao lado do “+” deixa escolher outra.</p>
        <div class="tc-list">${list.length ? list.map((c, i) => `<div class="tc-item" data-ci="${i}">
          <button class="tc-star ${c.main ? 'on' : ''}" data-c="main" title="${c.main ? 'Principal' : 'Tornar principal'}" aria-label="Principal">${svg('star')}</button>
          <input class="text-input sm tc-name" data-cf="name" value="${esc(c.name || '')}" placeholder="Nome" aria-label="Nome">
          <input class="text-input sm tc-user" data-cf="user" value="${esc(c.user)}" placeholder="usuário" aria-label="Usuário">
          <span class="tc-at">@</span><input class="text-input sm tc-host" data-cf="host" value="${esc(c.host)}" placeholder="192.168.1.50" aria-label="Servidor">
          <input class="text-input sm tc-port" data-cf="port" type="number" value="${esc(portValue(c.port))}" placeholder="22" aria-label="Porta">
          <select class="text-input sm" data-cf="method" aria-label="Forma de entrar"><option value="key" ${c.method === 'key' ? 'selected' : ''}>Chave do Terminal</option><option value="password" ${c.method === 'password' ? 'selected' : ''}>Senha</option><option value="paste" ${c.method === 'paste' ? 'selected' : ''}>Colar chave</option></select>
          <button class="icon-btn danger" data-c="delconn" title="Apagar" aria-label="Apagar">${svg('trash')}</button></div>`).join('') : '<small class="hint">Nenhuma ainda: conecte uma vez marcando “Salvar nas Conexões salvas”.</small>'}</div>
        <div class="tc-row"><button class="pill-btn" data-c="addconn">${svg('plus')}Adicionar</button></div>
      </section>
      <section class="tc-sec"><h3>${svg('code')}Comandos salvos</h3>
        <p class="hint">Viram botões em cima do terminal e aparecem na busca (Ctrl+K). Sem “executar direto”, o comando só é digitado: você confere e aperta Enter.</p>
        <div class="tc-list">${st.termSnippets.length ? st.termSnippets.map((s, i) => `<div class="tc-item snip" data-si="${i}">
          <input class="text-input sm tc-name" data-sf="label" value="${esc(s.label)}" placeholder="Nome do botão" aria-label="Nome do botão" maxlength="30">
          <input class="text-input sm tc-cmdin" data-sf="cmd" value="${esc(s.cmd)}" placeholder="comando, ex.: docker ps" aria-label="Comando" spellcheck="false">
          <label class="wsw tc-run">Executar direto<button type="button" class="switch ${s.run ? 'on' : ''}" data-c="snrun" role="switch" aria-checked="${!!s.run}"></button></label>
          <button class="icon-btn" data-c="snup" title="Subir" aria-label="Subir" ${i === 0 ? 'disabled' : ''}>${svg('arrowUp')}</button>
          <button class="icon-btn danger" data-c="sndel" title="Apagar" aria-label="Apagar">${svg('trash')}</button></div>`).join('') : '<small class="hint">Nenhum ainda. Exemplos: <code>docker ps</code>, <code>df -h</code>, <code>sudo apt update && sudo apt upgrade</code>.</small>'}</div>
        <div class="tc-row"><button class="pill-btn" data-c="addsnip">${svg('plus')}Adicionar comando</button></div>
      </section>
      <div class="dlg-actions">${tabs.length ? `<button class="pill-btn primary" data-t="back">${svg('terminal')}Voltar ao terminal</button>` : `<button class="pill-btn primary" data-t="connect">${svg('terminal')}Conectar</button>`}</div>
    </div>`;
  }
  const saveConns = () => ctx.saveSetting('termConnections', conns());
  const saveSnips = () => ctx.saveSetting('termSnippets', st.termSnippets);
  async function configClick(e) {
    const b = e.target.closest('[data-c]'); if (!b || view !== 'config') return;
    const c = b.dataset.c, list = conns();
    const ci = +b.closest('[data-ci]')?.dataset.ci, si = +b.closest('[data-si]')?.dataset.si;
    if (c === 'copy') { try { await navigator.clipboard.writeText($('.tc-cmd', body).textContent); toast('Comando copiado', 'Cole no terminal do servidor e aperte Enter.'); } catch { toast('Não deu para copiar', 'Selecione o texto e copie com Ctrl+C.'); } }
    else if (c === 'type') { const t = tabs[active]; if (t && !t.ended) { const cmd = $('.tc-cmd', body).textContent; await showTab(active); api.term.input(t.id, cmd).catch(() => {}); toast('Comando digitado', 'Confira e aperte Enter.'); } }
    else if (c === 'delkey') {
      if (!(await confirmDialog('Apagar a Chave do Terminal?', 'As Conexões salvas com a Chave do Terminal vão pedir senha até você criar outra. Tire também a linha brunos-terminal do authorized_keys do servidor.', 'Apagar', { danger: true }))) return;
      try { await api.term.key.del(); keyInfo = null; showConfig(); } catch (x) { toastError(x); }
    } else if (c === 'newkey') {
      const dd = openDialog(`<h2>${svg('key')} Criar a Chave do Terminal</h2><p>A chave vai ser trancada com a sua senha do BrunOS. Se um dia você esquecer essa senha, é só criar outra chave.</p>
        <form class="form-grid"><label>Senha do BrunOS<input class="text-input" type="password" name="pw" autocomplete="current-password" autofocus></label><p class="term-err" role="alert"></p>
        <div class="dlg-actions"><button type="button" class="pill-btn" data-x="no">Cancelar</button><button class="pill-btn primary">${svg('check')}Criar</button></div></form>`, { closeBtn: false, width: 420 });
      const f = $('form', dd.el);
      $('[data-x="no"]', dd.el).onclick = () => dd.close();
      f.onsubmit = async (ev) => {
        ev.preventDefault();
        try { keyInfo = await api.term.key.create(f.pw.value); dd.close(); toast('Chave do Terminal criada', 'Agora autorize no servidor com o comando mostrado.'); showConfig(); }
        catch (x) { $('.term-err', f).textContent = x.message; f.pw.select(); }
      };
    }
    else if (c === 'main') { list.forEach((x, i) => { x.main = i === ci; }); saveConns(); showConfig(); }
    else if (c === 'delconn') { const was = list[ci]?.main; list.splice(ci, 1); if (was && list[0]) list[0].main = true; saveConns(); showConfig(); }
    else if (c === 'addconn') { const m = mainConn(list); list.push({ id: rid(), name: '', host: m?.host || ctx.defaultHost || '', port: 22, user: m?.user || '', method: keyInfo?.exists ? 'key' : 'password', main: !list.length }); saveConns(); showConfig(); }
    else if (c === 'addsnip') { st.termSnippets.push({ id: rid(), label: '', cmd: '', run: false }); saveSnips(); await showConfig(); $$('[data-sf="label"]', body).pop()?.focus(); }
    else if (c === 'sndel') { st.termSnippets.splice(si, 1); saveSnips(); showConfig(); }
    else if (c === 'snup' && si > 0) { [st.termSnippets[si - 1], st.termSnippets[si]] = [st.termSnippets[si], st.termSnippets[si - 1]]; saveSnips(); showConfig(); }
    else if (c === 'snrun') { const s = st.termSnippets[si]; s.run = !s.run; b.classList.toggle('on', s.run); b.setAttribute('aria-checked', s.run); saveSnips(); }
  }
  function configInput(e) {
    const t = e.target;
    if (t.dataset.cf) {
      const c = conns()[+t.closest('[data-ci]').dataset.ci]; if (!c) return;
      c[t.dataset.cf] = t.dataset.cf === 'port' ? (+t.value || 22) : t.value.trim();
      saveConns();
    } else if (t.dataset.sf) {
      const s = st.termSnippets[+t.closest('[data-si]').dataset.si]; if (!s) return;
      s[t.dataset.sf] = t.dataset.sf === 'label' ? t.value.slice(0, 30) : t.value;
      saveSnips();
    }
  }

  // escolher onde abrir a nova Aba (Conexões salvas ou outro servidor)
  function pickMenu(anchor) {
    const r = anchor.getBoundingClientRect();
    contextMenu(r.left, r.bottom + 6, [
      ...conns().map((c) => ({ icon: c.main ? 'star' : 'server', label: `${c.name || `${c.user}@${c.host}`}${c.method === 'key' ? '' : ' (pede senha)'}`, run: () => openConn(c) })),
      ...(conns().length ? ['-'] : []),
      { icon: 'plus', label: 'Outro servidor…', run: () => showForm() },
    ]);
  }

  function startEmpty() {
    const main = mainConn(conns());
    getKey().then((k) => {
      if (!openWin) return;
      if (main && main.method === 'key' && k.exists) openConn(main);
      else showForm('', main ? { host: main.host, port: main.port, user: main.user, method: main.method, save: true } : null);
    });
  }

  // ── Eventos ──────────────────────────────────────────────────────
  d.el.addEventListener('click', async (e) => {
    const x = e.target.closest('[data-tabclose]'); if (x) { e.stopPropagation(); closeTab(+x.dataset.tabclose); return; }
    const tb = e.target.closest('[data-tab]'); if (tb) { showTab(+tb.dataset.tab); return; }
    const sn = e.target.closest('[data-snip]'); if (sn) { sendSnippet(st.termSnippets[+sn.dataset.snip]); return; }
    if (view === 'config' && e.target.closest('[data-c]')) { configClick(e); return; }
    const b = e.target.closest('[data-t]'); if (!b) return;
    const k = b.dataset.t;
    if (k === 'new') openConn(mainConn(conns()));
    else if (k === 'new-pick') pickMenu(b);
    else if (k === 'config') showConfig();
    else if (k === 'back') { if (tabs.length) showTab(Math.max(0, Math.min(active, tabs.length - 1))); else startEmpty(); }
    else if (k === 'connect') startEmpty();
    else if (k === 'minimize') d.close();
    else if (k === 'closeall') {
      const live = tabs.filter((t) => !t.ended);
      if (live.length && !(await okToClose(live))) return;
      for (const t of live) api.term.close(t.id).catch(() => {});
      tabs = []; d.close();
    } else if (k === 'snipbar') {
      if (!st.termSnippets.some((s) => s.cmd)) { showConfig(); return; }
      ctx.saveSetting('termSnippetBar', st.termSnippetBar === false); drawSnips();
    } else if (k === 'again') {
      const t = tabs[active]; if (!t) return;
      const c = connOf(t);
      tabs.splice(active, 1); setCount();
      if (c) openConn(c); else showForm('', { host: t.addr.replace(/:\d+$/, ''), port: +t.addr.split(':').pop() || 22, user: t.user });
    }
    else if (k === 'drop') closeTab(active, false);
    else if (k === 'reattach') showTab(active);
    else if (k === 'bigger' || k === 'smaller') {
      fontSize = Math.min(22, Math.max(10, fontSize + (k === 'bigger' ? 1 : -1)));
      ctx.saveSetting('termFontSize', fontSize);
      if (term) { term.options.fontSize = fontSize; fit.fit(); }
    }
  });
  d.el.addEventListener('keydown', (e) => { const tb = e.target.closest?.('[data-tab]'); if (tb && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); showTab(+tb.dataset.tab); } });
  d.el.addEventListener('input', (e) => { if (view === 'config') configInput(e); });

  // desbloqueou a tela: volta a mostrar a Aba da frente
  offUnlock = on('unlocked', () => { if (openWin && view === 'term' && tabs[active] && !tabs[active].ended) showTab(active); });
  // a cada 30 s: o que roda em cada Aba (pontinho), Abas que acabaram e sinal de vida das de trás
  pingTimer = setInterval(async () => {
    if (!openWin || document.hidden) return;
    try {
      const list = await api.term.list();
      for (const t of tabs) {
        const s = list.find((y) => y.id === t.id);
        if (!s && !t.ended) t.ended = 'sessão encerrada';
        else if (s) { t.running = s.running; if (tabs[active] !== t) api.term.resize(t.id, term?.cols || 100, term?.rows || 30).catch(() => {}); }
      }
      setCount(); drawTabs();
    } catch { /* tela bloqueada ou sem rede: tenta depois */ }
  }, 30000);

  // ── Início: Abas que já existem (minimizadas) ou a Conexão principal ──
  (async () => {
    body.innerHTML = `<div class="term-loading">${svg('terminal')}Abrindo…</div>`;
    let list = [];
    try { list = await api.term.list(); } catch { /* sem terminal no servidor */ }
    if (!openWin) return;
    tabs = list.map((s) => ({ id: s.id, addr: s.addr, user: s.user, created: s.created, running: s.running }));
    setCount();
    if (tabs.length) showTab(tabs.length - 1);
    else startEmpty();
  })();
}
