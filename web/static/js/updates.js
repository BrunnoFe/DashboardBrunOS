// Botão de atualizações ao lado do sino (0.12.0): bolinha com quantas há (apps + Linux), e um clique
// abre o painel e já confere (se a última conferência tiver mais de 10 min). No painel dá para
// atualizar cada app e o Linux direto pela interface: o servidor roda a atualização sozinho e o
// painel mostra o andamento (decisão 47).
import { api } from './api.js';
import { $, esc, html, svg, toast, toastError, timeAgo, confirmDialog, openDialog, fmtBytes } from './ui.js';

let ctx = null, panel = null, checking = false, pollTimer = 0, lastJobRunning = false, dismissed = 0, failure = '';
const STALE = 600; // segundos: conferência mais velha que isso é refeita ao abrir o painel
const EVERY = { '6h': 'a cada 6 horas', '12h': 'a cada 12 horas', '24h': 'uma vez por dia', '168h': 'uma vez por semana', manual: 'só quando você manda' };

const snap = () => ctx?.get() || null;
const jobRunning = (s = snap()) => !!s?.job?.running;
function counts(s = snap()) {
  const apps = s?.apps?.length || 0, os = s?.os && !s.os.err ? s.os.total || 0 : 0;
  return { apps, os, total: apps + os, security: s?.os?.security || 0 };
}

/** Liga o botão. c = { get, set, appIcon(urn), openApp(urn), osList(), scriptDialog(), openSettings(), reloadApps() } */
export function initUpdates(c) {
  ctx = c;
  $('#upd-btn').addEventListener('click', (e) => { e.stopPropagation(); if (panel) closeUpdates(); else openUpdates(); });
}

/** Repinta o botão (e o painel aberto) com os dados atuais; acompanha uma atualização em andamento. */
export function paintUpdates() {
  const s = snap(), b = $('#upd-btn'); if (!b) return;
  const n = counts(s), badge = $('.bell-badge', b), run = jobRunning(s);
  badge.textContent = n.total > 99 ? '99+' : String(n.total);
  badge.hidden = !n.total || run;
  b.classList.toggle('sec', n.security > 0);
  b.classList.toggle('busy', run || checking);
  b.setAttribute('aria-label', run ? `Atualizando ${s.job.name}` : n.total ? `Atualizações: ${n.total}` : 'Atualizações: tudo em dia');
  if (lastJobRunning && !run) ctx?.reloadApps?.(); // terminou: as setas dos ícones mudam
  lastJobRunning = run;
  schedulePoll();
  if (panel) drawPanel();
}

// Com atualização rodando: lê o andamento a cada 3 s com o painel aberto, 15 s com ele fechado.
function schedulePoll() {
  clearTimeout(pollTimer); pollTimer = 0;
  if (!jobRunning()) return;
  pollTimer = setTimeout(async () => {
    try { ctx.set(await api.updates.get()); } catch { /* tenta de novo */ }
    paintUpdates();
  }, panel ? 3000 : 15000);
}

export const updatesOpen = () => !!panel;
export function closeUpdates() {
  if (!panel) return;
  const p = panel; panel = null;
  p.classList.add('closing');
  setTimeout(() => p.remove(), 160);
  document.removeEventListener('mousedown', outside, true);
  $('#upd-btn').classList.remove('open');
  schedulePoll();
}
const outside = (e) => { if (panel && !panel.contains(e.target) && !e.target.closest('#upd-btn') && !e.target.closest('.dlg-wrap')) closeUpdates(); };

export function openUpdates() {
  panel = html(`<div class="notif-panel upd-panel" role="dialog" aria-label="Atualizações">
    <div class="np-head"><b>Atualizações</b><small class="upd-when"></small><span class="np-acts"><button class="np-btn" data-up="check">Conferir agora</button></span></div>
    <div class="upd-body"></div>
    <div class="upd-foot"><span></span><button class="np-btn" data-up="freq">Mudar</button></div></div>`);
  document.body.append(panel);
  $('#upd-btn').classList.add('open');
  drawPanel();
  setTimeout(() => document.addEventListener('mousedown', outside, true));
  panel.addEventListener('click', onClick);
  const s = snap();
  if (!jobRunning(s) && (!s?.checkedAt || Date.now() / 1000 - s.checkedAt > STALE)) check(); // um clique já confere
  schedulePoll();
}

// part: 'apps' (atualiza as lojas do Runtipi e confere os apps), 'linux' ou '' (tudo)
async function check(part = '') {
  if (checking) return;
  checking = part || 'all'; paintUpdates();
  try { ctx.set(await api.updates.check(part)); if (part !== 'linux') ctx.reloadApps?.(); }
  catch (err) { toastError(err, 'Não deu para conferir as atualizações'); }
  checking = false; paintUpdates();
}

// Confirmação do app: caixinha do backup do Runtipi (sempre marcada) e o tamanho medido no servidor.
function confirmApp(u) {
  return new Promise((resolve) => {
    let done = false;
    const d = openDialog(`<h2>Atualizar ${esc(u.name)}?</h2>
      <p>De ${esc(u.installed || '?')} para ${esc(u.available || '?')}, pelo Runtipi. O app fica fora do ar por alguns instantes enquanto baixa a versão nova e reinicia.</p>
      <label class="upd-bk"><input type="checkbox" checked data-bk><span class="map-check">${svg('check')}</span>
        <span><b>Fazer backup antes (como o Runtipi)</b><small data-bk-size><span class="upd-spin"></span> Medindo o tamanho do backup…</small></span></label>
      <p class="upd-bk-note"><small>O backup é o mesmo do painel do Runtipi (pasta do app e dados dele) e dá para restaurar pela página do app no Runtipi. Se o backup falhar, nada é atualizado.</small></p>
      <div class="dlg-actions"><button class="pill-btn" data-c="no">Cancelar</button><button class="pill-btn primary" data-c="ok" autofocus>Atualizar</button></div>`,
    { closeBtn: false, width: 460, onClose: () => { if (!done) resolve(null); } });
    api.updates.backupSize(u.urn).then((r) => {
      const el = $('[data-bk-size]', d.el); if (!el) return;
      const tight = r.free && r.size > r.free * 0.9;
      el.innerHTML = `Backup de ~${esc(fmtBytes(r.size))}${r.free ? ` · ${esc(fmtBytes(r.free))} livres` : ''}${tight ? ' · <b class="os-reboot">pode faltar espaço</b>' : r.size > 5e9 ? ' · pode demorar alguns minutos' : ''}`;
    }).catch(() => { const el = $('[data-bk-size]', d.el); if (el) el.textContent = 'Não deu para medir o tamanho agora.'; });
    d.el.addEventListener('click', (e) => {
      const b = e.target.closest('[data-c]'); if (!b) return;
      done = true; resolve(b.dataset.c === 'ok' ? { backup: $('[data-bk]', d.el).checked } : null); d.close();
    });
  });
}

async function run(kind, urn) {
  const s = snap();
  let backup = false;
  if (kind === 'linux') {
    const o = s.os;
    if (!(await confirmDialog('Atualizar o Linux do servidor?', `${o.total} ${o.total === 1 ? 'pacote' : 'pacotes'} vão ser instalados sem perguntas (os arquivos de configuração que você mudou são mantidos). Pode levar alguns minutos. Se o Docker for atualizado, os apps e o BrunOS reiniciam sozinhos e voltam em seguida. Reiniciar o servidor, se precisar, fica com você.`, 'Atualizar'))) return;
  } else {
    const u = s.apps.find((x) => x.urn === urn); if (!u) return;
    const r = await confirmApp(u); if (!r) return;
    backup = r.backup;
  }
  try { ctx.set(await api.updates.run(kind, urn, backup)); }
  catch (err) { failure = err.message || String(err); toastError(err, 'Não deu para começar a atualização'); }
  paintUpdates();
}

async function upgradeScript() {
  try { ctx.set(await api.updates.script()); const sc = snap()?.script; if (sc && sc.have >= sc.want) toast('Script do servidor em dia', `Versão ${sc.have}.`); }
  catch (err) { toastError(err, 'Não deu para atualizar o script'); }
  paintUpdates();
}

async function onClick(e) {
  const b = e.target.closest('[data-up]'); if (!b) return;
  const a = b.dataset.up;
  if (a === 'check') check();
  else if (a === 'check-apps') check('apps');
  else if (a === 'check-linux') check('linux');
  else if (a === 'freq') { closeUpdates(); ctx.openSettings(); }
  else if (a === 'linux') run('linux');
  else if (a === 'app') run('app', b.dataset.urn);
  else if (a === 'open-app') { closeUpdates(); ctx.openApp(b.dataset.urn); }
  else if (a === 'os-list') ctx.osList();
  else if (a === 'script') ctx.scriptDialog();
  else if (a === 'script-term') { closeUpdates(); ctx.scriptInTerminal(); }
  else if (a === 'script-auto') { b.disabled = true; upgradeScript(); }
  else if (a === 'dismiss') { dismissed = snap()?.job?.startedAt || 0; drawPanel(); }
  else if (a === 'dismiss-fail') { failure = ''; drawPanel(); }
}

// Faixa do script do servidor desatualizado: troca sozinho (7+, versão assinada) ou pelo Terminal.
function scriptHTML(sc) {
  if (!sc) return '';
  if (!sc.have) return sc.err ? `<div class="upd-note"><span>${svg('info')}</span><small>${esc(sc.err)}</small></div>` : '';
  if (sc.have >= sc.want) return sc.err ? `<div class="upd-note warn"><span>${svg('info')}</span><small>${esc(sc.err)}</small></div>` : '';
  const self = sc.auto && sc.have >= 7;
  return `<div class="upd-note warn"><span>${svg('info')}</span><span><b>Script do servidor desatualizado</b><small>Está na versão ${sc.have}; esta versão do BrunOS precisa da ${sc.want}${self ? '. Ele se atualiza sozinho com a versão assinada.' : sc.have < 7 ? '. Desta vez é pelo Terminal (uma vez só): a partir da versão 7 ele se atualiza sozinho.' : ''}${sc.err ? ` ${esc(sc.err)}` : ''}</small>
    <span class="upd-note-acts">${self ? '<button class="pill-btn sm" data-up="script-auto">Atualizar agora</button>' : ''}<button class="pill-btn sm ${self ? '' : 'primary'}" data-up="script-term">${svg('terminal')}Pelo Terminal</button><button class="np-btn" data-up="script">Ver comandos</button></span></span></div>`;
}

function jobHTML(j) {
  if (!j || (!j.running && (dismissed === j.startedAt || Date.now() / 1000 - (j.finishedAt || 0) > 1800))) return '';
  const log = (j.lines || []).slice(-4);
  const logHTML = log.length ? `<pre class="upd-log">${esc(log.join('\n'))}</pre>` : '';
  if (j.running) {
    const what = j.phase === 'backup' ? `Fazendo backup de ${esc(j.name)}…` : `Atualizando ${esc(j.name)}…`;
    const step = j.backup ? `<small class="upd-steps"><span class="${j.phase === 'atualizando' ? 'done' : 'on'}">1. Backup</span> → <span class="${j.phase === 'atualizando' ? 'on' : ''}">2. Atualizar</span></small>` : '';
    return `<div class="upd-job"><div class="upd-job-head"><span class="upd-spin"></span><b>${what}</b></div>${step}${logHTML || '<pre class="upd-log">Começando no servidor…</pre>'}<small>Pode fechar: a atualização continua no servidor e o sino avisa quando terminar.</small></div>`;
  }
  const ok = j.ok, warn = ok && j.warn;
  return `<div class="upd-job ${warn ? 'warn' : ok ? 'ok' : 'bad'}"><div class="upd-job-head">${svg(ok && !warn ? 'check' : 'info')}<b>${ok ? `${esc(j.name)} atualizado` : `Não deu para atualizar ${esc(j.name)}`}</b><button class="np-btn" data-up="dismiss" aria-label="Dispensar">${svg('x')}</button></div>${warn ? `<small>Aviso: ${esc(j.warn)}.</small>` : ''}${ok ? '' : `<small>${esc(j.err || '')}</small>${logHTML}`}</div>`;
}

function drawPanel() {
  if (!panel) return;
  const s = snap() || {}, n = counts(s), run = jobRunning(s), o = s.os;
  $('.upd-when', panel).textContent = s.checkedAt ? `conferido ${timeAgo(s.checkedAt)}` : '';
  const chk = $('[data-up="check"]', panel); chk.disabled = !!checking || run; chk.textContent = checking === 'all' ? 'Conferindo…' : 'Conferir tudo';
  // atualizar apps pede o script 7 (backup); o Linux funciona desde o 6
  const sc = s.script || {}, known = sc.have > 0;
  const disApp = run || (known && sc.have < 7) ? 'disabled' : '', disOS = run || (known && sc.have < 6) ? 'disabled' : '';
  const apps = (s.apps || []).map((u) => `<div class="upd-row">
      <button class="upd-app" data-up="open-app" data-urn="${esc(u.urn)}" title="Detalhes do app">${ctx.appIcon(u.urn)}<span class="upd-txt"><b>${esc(u.name)}</b><small>${esc(u.installed || '?')} → ${esc(u.available || '?')}</small></span></button>
      <button class="pill-btn sm" data-up="app" data-urn="${esc(u.urn)}" ${disApp}>Atualizar</button></div>`).join('');
  const script = /script do servidor/.test(o?.err || '');
  // Linux: o que dá para instalar agora, os segurados pela liberação gradual do Ubuntu e o reinício
  const phased = o?.phased ? `<span title="O Ubuntu libera algumas atualizações aos poucos, para uma parte dos computadores de cada vez. Elas entram sozinhas numa próxima atualização.">${o.phased} aguardando a liberação gradual do Ubuntu</span>` : '';
  const osText = !o ? 'Ainda não conferido' : [
    o.total ? `${o.total} ${o.total === 1 ? 'atualização' : 'atualizações'}${o.security ? ` · <span class="upd-sec">${o.security} de segurança</span>` : ''}` : 'Tudo instalado',
    phased, o.reboot ? '<b class="os-reboot">reinício necessário</b>' : '',
  ].filter(Boolean).join(' · ');
  const linux = o?.err
    ? `<div class="upd-row"><span class="upd-app"><span class="mini sys">${svg('cpu')}</span><span class="upd-txt"><b>Linux</b><small>${esc(o.err)}</small></span></span>${script ? '<button class="pill-btn sm" data-up="script">Mostrar comandos</button>' : ''}</div>`
    : `<div class="upd-row"><span class="upd-app"><span class="mini sys">${svg('cpu')}</span><span class="upd-txt"><b>Linux</b><small>${osText}</small></span></span>
      ${o?.total || o?.phased ? '<button class="np-btn" data-up="os-list">Ver lista</button>' : ''}${o?.total ? `<button class="pill-btn sm" data-up="linux" ${disOS}>Atualizar</button>` : ''}</div>`;
  // cada seção com o seu "Conferir" (Apps também atualiza as lojas do Runtipi)
  const busy = (p) => checking === p || checking === 'all';
  const secBtn = (p) => busy(p) ? '<span class="upd-sec-busy"><span class="upd-spin"></span>conferindo…</span>' : `<button class="np-btn" data-up="check-${p}" ${checking || run ? 'disabled' : ''}>Conferir</button>`;
  const stores = s.storesErr ? `<div class="upd-note warn"><span>${svg('info')}</span><small>${esc(s.storesErr)}</small></div>`
    : s.storesAt ? `<small class="upd-stores">Lojas do Runtipi atualizadas ${esc(timeAgo(s.storesAt))}</small>` : '';
  const body = `<div class="upd-sec-title">Apps <span>${n.apps || ''}</span>${secBtn('apps')}</div>
    ${apps || `<div class="upd-none">${svg('check')}<small>${s.appsAt || s.checkedAt ? 'Todos os apps em dia' : 'Ainda não conferido'}</small></div>`}${stores}
    <div class="upd-sec-title">Sistema${secBtn('linux')}</div>${linux}`;
  const checkingHTML = '';
  // erro ao começar uma atualização: fica no painel (não só numa mensagem que some)
  const failHTML = failure ? `<div class="upd-job bad"><div class="upd-job-head">${svg('info')}<b>Não deu para começar</b><button class="np-btn" data-up="dismiss-fail" aria-label="Dispensar">${svg('x')}</button></div><small>${esc(failure)}</small>${/script do servidor/.test(failure) ? '<span class="upd-note-acts"><button class="pill-btn sm" data-up="script-term">Atualizar o script pelo Terminal</button></span>' : ''}</div>` : '';
  $('.upd-body', panel).innerHTML = checkingHTML + scriptHTML(sc) + failHTML + jobHTML(s.job) + body;
  $('.upd-foot span', panel).textContent = `Procura sozinho ${EVERY[s.every] || EVERY['24h']}.`;
}

/** Para a busca (Ctrl+K) e outros lugares: abre o painel. */
export function showUpdates() { if (!panel) openUpdates(); }
