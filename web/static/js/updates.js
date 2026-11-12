// Botão de atualizações ao lado do sino (0.12.0): bolinha com quantas há (apps + Linux), e um clique
// abre o painel e já confere (se a última conferência tiver mais de 10 min). No painel dá para
// atualizar cada app e o Linux direto pela interface: o servidor roda a atualização sozinho e o
// painel mostra o andamento (decisão 47).
import { api } from './api.js';
import { $, esc, html, svg, toastError, timeAgo, confirmDialog } from './ui.js';

let ctx = null, panel = null, checking = false, pollTimer = 0, lastJobRunning = false, dismissed = 0;
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

async function check() {
  if (checking) return;
  checking = true; paintUpdates();
  try { ctx.set(await api.updates.check()); ctx.reloadApps?.(); }
  catch (err) { toastError(err, 'Não deu para conferir as atualizações'); }
  checking = false; paintUpdates();
}

async function run(kind, urn) {
  const s = snap();
  if (kind === 'linux') {
    const o = s.os;
    if (!(await confirmDialog('Atualizar o Linux do servidor?', `${o.total} ${o.total === 1 ? 'pacote' : 'pacotes'} vão ser instalados sem perguntas (os arquivos de configuração que você mudou são mantidos). Pode levar alguns minutos. Se o Docker for atualizado, os apps e o BrunOS reiniciam sozinhos e voltam em seguida. Reiniciar o servidor, se precisar, fica com você.`, 'Atualizar'))) return;
  } else {
    const u = s.apps.find((x) => x.urn === urn); if (!u) return;
    if (!(await confirmDialog(`Atualizar ${u.name}?`, `De ${u.installed || '?'} para ${u.available || '?'}, pelo Runtipi. O app fica fora do ar por alguns instantes enquanto baixa a versão nova e reinicia. Versões grandes às vezes mudam coisas: se o app guarda dados importantes, vale ter backup.`, 'Atualizar'))) return;
  }
  try { ctx.set(await api.updates.run(kind, urn)); }
  catch (err) { toastError(err, 'Não deu para começar a atualização'); }
  paintUpdates();
}

async function onClick(e) {
  const b = e.target.closest('[data-up]'); if (!b) return;
  const a = b.dataset.up;
  if (a === 'check') check();
  else if (a === 'freq') { closeUpdates(); ctx.openSettings(); }
  else if (a === 'linux') run('linux');
  else if (a === 'app') run('app', b.dataset.urn);
  else if (a === 'open-app') { closeUpdates(); ctx.openApp(b.dataset.urn); }
  else if (a === 'os-list') ctx.osList();
  else if (a === 'script') ctx.scriptDialog();
  else if (a === 'dismiss') { dismissed = snap()?.job?.startedAt || 0; drawPanel(); }
}

function jobHTML(j) {
  if (!j || (!j.running && (dismissed === j.startedAt || Date.now() / 1000 - (j.finishedAt || 0) > 1800))) return '';
  const log = (j.lines || []).slice(-4);
  const logHTML = log.length ? `<pre class="upd-log">${esc(log.join('\n'))}</pre>` : '';
  if (j.running) return `<div class="upd-job"><div class="upd-job-head"><span class="upd-spin"></span><b>Atualizando ${esc(j.name)}…</b></div>${logHTML || '<pre class="upd-log">Começando no servidor…</pre>'}<small>Pode fechar: a atualização continua no servidor e o sino avisa quando terminar.</small></div>`;
  const ok = j.ok;
  return `<div class="upd-job ${ok ? 'ok' : 'bad'}"><div class="upd-job-head">${svg(ok ? 'check' : 'info')}<b>${ok ? `${esc(j.name)} atualizado` : `Não deu para atualizar ${esc(j.name)}`}</b><button class="np-btn" data-up="dismiss" aria-label="Dispensar">${svg('x')}</button></div>${ok ? '' : `<small>${esc(j.err || '')}</small>${logHTML}`}</div>`;
}

function drawPanel() {
  if (!panel) return;
  const s = snap() || {}, n = counts(s), run = jobRunning(s), o = s.os;
  $('.upd-when', panel).textContent = s.checkedAt ? `conferido ${timeAgo(s.checkedAt)}` : '';
  const chk = $('[data-up="check"]', panel); chk.disabled = checking || run; chk.textContent = checking ? 'Conferindo…' : 'Conferir agora';
  const dis = run ? 'disabled' : '';
  const apps = (s.apps || []).map((u) => `<div class="upd-row">
      <button class="upd-app" data-up="open-app" data-urn="${esc(u.urn)}" title="Detalhes do app">${ctx.appIcon(u.urn)}<span class="upd-txt"><b>${esc(u.name)}</b><small>${esc(u.installed || '?')} → ${esc(u.available || '?')}</small></span></button>
      <button class="pill-btn sm" data-up="app" data-urn="${esc(u.urn)}" ${dis}>Atualizar</button></div>`).join('');
  const script = /script do servidor/.test(o?.err || '');
  const linux = !o ? '' : o.err
    ? `<div class="upd-row"><span class="upd-app"><span class="mini sys">${svg('cpu')}</span><span class="upd-txt"><b>Linux</b><small>${esc(o.err)}</small></span></span>${script ? '<button class="pill-btn sm" data-up="script">Mostrar comandos</button>' : ''}</div>`
    : o.total || o.reboot ? `<div class="upd-row"><span class="upd-app"><span class="mini sys">${svg('cpu')}</span><span class="upd-txt"><b>Linux</b><small>${o.total ? `${o.total} ${o.total === 1 ? 'atualização' : 'atualizações'}${o.security ? ` · <span class="upd-sec">${o.security} de segurança</span>` : ''}` : 'tudo instalado'}${o.reboot ? ' · <b class="os-reboot">reinício necessário</b>' : ''}</small></span></span>
      ${o.total ? `<button class="np-btn" data-up="os-list">Ver lista</button><button class="pill-btn sm" data-up="linux" ${dis}>Atualizar</button>` : ''}</div>` : '';
  const checkingHTML = checking ? '<div class="upd-checking"><span class="upd-spin"></span>Conferindo os apps e o Linux…</div>' : '';
  const body = !n.total && !linux
    ? `<div class="np-empty">${svg('check')}<span>Tudo em dia.<br><small>${checking ? 'Conferindo de novo…' : 'Nenhum app nem pacote do Linux para atualizar.'}</small></span></div>`
    : `${apps ? `<div class="upd-sec-title">Apps <span>${n.apps}</span></div>${apps}` : ''}${linux ? `<div class="upd-sec-title">Sistema</div>${linux}` : ''}`;
  $('.upd-body', panel).innerHTML = checkingHTML + jobHTML(s.job) + body;
  $('.upd-foot span', panel).textContent = `Procura sozinho ${EVERY[s.every] || EVERY['24h']}.`;
}

/** Para a busca (Ctrl+K) e outros lugares: abre o painel. */
export function showUpdates() { if (!panel) openUpdates(); }
