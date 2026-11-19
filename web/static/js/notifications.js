// Central de notificações: o sino no canto da tela inicial e o painel com os avisos do servidor
// (energia caiu/voltou, desligamento seguro, disco desmontado...). Os avisos novos chegam pelo
// stream de estatísticas (event: notify) e também aparecem como toast.
import { api } from './api.js';
import { $, esc, html, svg, toast, timeAgo, emit } from './ui.js';
import { canExpand, notifDetail } from './notifdetails.js';

const S = { events: [], unread: 0, installs: [] };
let panel = null, actions = {};
const opened = new Set(); // avisos abertos (0.25.0: detalhes dos importantes)

/** Botões de atalho dos detalhes (app.js): { app(urn), disks(), power(), internet(), updates(), password(), store(), 'cancel-power'() } */
export const setNotifActions = (a) => { actions = a; };
/** Redesenha o painel aberto (o menu de energia chama quando o pedido muda: o Cancelar aparece/some). */
export const redrawNotifications = () => drawPanel();

// 0.21.2: instalações acompanhadas em segundo plano (storeforms.js): o sino ganha um anel girando e o
// painel mostra "Em andamento" com uma barra animada, até o app ligar.
addEventListener('brunos:installs', (e) => {
  S.installs = e.detail.list || [];
  paintBell();
  if (panel) drawPanel();
});

const LEVEL = { info: 'Informação', aviso: 'Aviso', critico: 'Importante' };
const ICON = (e) => e.kind.startsWith('power') ? 'bolt' : e.kind.startsWith('disk') ? 'disks' : e.kind.startsWith('login') ? 'lock' : 'bell';

$('#bell').addEventListener('click', (e) => { e.stopPropagation(); if (panel) closePanel(); else openPanel(); });
export const toggleNotifications = () => (panel ? closePanel() : openPanel());
export const notificationsOpen = () => !!panel;

// Relê a lista do servidor (ao entrar e sempre que o stream reconecta, para não perder avisos).
export async function refreshNotifications() {
  try {
    const r = await api.events.list();
    S.events = r.events || []; S.unread = r.unread || 0;
  } catch { return; }
  paintBell();
  if (panel) drawPanel();
}

// Chamado quando chega um aviso pelo stream.
export function onNotify(e) {
  if (S.events.some((x) => x.id === e.id)) return;
  S.events.unshift(e); S.unread++;
  paintBell(true);
  if (panel) drawPanel();
  // aviso importante: clicar no balão abre o sino já com este aviso aberto
  toast(e.title, e.body || '', null, { error: e.level === 'critico', ms: e.level === 'critico' ? 12000 : 7000, onClick: canExpand(e) ? () => showNotification(e.id) : undefined });
  emit('notify', e);
}

/** Abre o painel com um aviso aberto (balão flutuante, busca...). */
export function showNotification(id) {
  if (id) opened.add(id);
  if (!panel) openPanel(); else drawPanel();
  if (id) markRead(id);
  requestAnimationFrame(() => panel?.querySelector(`[data-id="${CSS.escape(id || '')}"]`)?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }));
}

async function markRead(id) {
  const e = S.events.find((x) => x.id === id); if (!e || e.read) return;
  e.read = true; S.unread = Math.max(0, S.unread - 1); paintBell(); drawPanel();
  try { const r = await api.events.read([id]); if (typeof r?.unread === 'number') { S.unread = r.unread; paintBell(); } } catch { /* fica lido só aqui */ }
}

export function closePanel() {
  if (!panel) return;
  const p = panel; panel = null;
  p.classList.add('closing');
  setTimeout(() => p.remove(), 160);
  document.removeEventListener('mousedown', outside, true);
  $('#bell').classList.remove('open');
}

function paintBell(ring = false) {
  const b = $('#bell'), badge = $('.bell-badge', b);
  badge.textContent = S.unread > 99 ? '99+' : String(S.unread);
  badge.hidden = !S.unread;
  const crit = S.events.some((e) => !e.read && e.level === 'critico');
  b.classList.toggle('crit', crit);
  const busy = S.installs.length;
  b.classList.toggle('working', !!busy);
  b.title = busy ? `Instalando: ${S.installs.map((x) => x.name).join(', ')}` : '';
  b.setAttribute('aria-label', `${busy ? `Instalando ${busy} ${busy === 1 ? 'app' : 'apps'}. ` : ''}${S.unread ? `Notificações: ${S.unread} sem ler` : 'Notificações'}`);
  if (ring) { b.classList.remove('ring'); void b.offsetWidth; b.classList.add('ring'); }
}

const outside = (e) => { if (panel && !panel.contains(e.target) && !e.target.closest('#bell')) closePanel(); };

function openPanel() {
  panel = html(`<div class="notif-panel" role="dialog" aria-label="Notificações">
    <div class="np-head"><b>Notificações</b><span class="np-acts"><button class="np-btn" data-np="read">Marcar como lidas</button><button class="np-btn" data-np="clear">Limpar</button></span></div>
    <div class="np-list"></div></div>`);
  document.body.append(panel);
  $('#bell').classList.add('open');
  drawPanel();
  setTimeout(() => document.addEventListener('mousedown', outside, true));
  panel.addEventListener('click', async (e) => {
    const na = e.target.closest('[data-na]');
    if (na) { const run = actions[na.dataset.na]; if (run) { if (na.dataset.na !== 'cancel-power') closePanel(); run(na.dataset.arg || ''); } return; }
    const item = e.target.closest('.np-can');
    if (item && !e.target.closest('.np-detail') && !e.target.closest('[data-np]')) { toggle(item.dataset.id); return; }
    const b = e.target.closest('[data-np]'); if (!b) return;
    try {
      if (b.dataset.np === 'read') { await api.events.read([]); S.events.forEach((x) => { x.read = true; }); S.unread = 0; }
      else { await api.events.clear(); S.events = []; S.unread = 0; }
    } catch (err) { toast('Não deu para atualizar', err.message, null, { error: true }); }
    paintBell(); drawPanel();
  });
  panel.addEventListener('keydown', (e) => {
    const item = e.target.closest?.('.np-can');
    if (item && e.target === item && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); toggle(item.dataset.id); }
  });
}

function toggle(id) {
  if (opened.has(id)) opened.delete(id); else { opened.add(id); markRead(id); }
  drawPanel();
  panel?.querySelector(`[data-id="${CSS.escape(id)}"]`)?.focus({ preventScroll: true });
}

// Detalhes de um aviso importante aberto: explicação, o que fazer, dados e o botão de atalho.
function detailHTML(e) {
  const d = notifDetail(e); if (!d) return '';
  if (d.action?.act === 'cancel-power' && actions.powerPending && !actions.powerPending()) d.action = null; // já cancelado
  return `<div class="np-detail">
    <p>${esc(d.why)}</p>
    <p class="np-todo"><b>O que fazer</b>${esc(d.todo)}</p>
    ${d.facts.length ? `<dl class="np-facts">${d.facts.map(([k, v]) => `<dt>${esc(k)}</dt><dd>${esc(v)}</dd>`).join('')}</dl>` : ''}
    ${d.action ? `<button class="pill-btn sm ${d.action.act === 'cancel-power' ? 'danger-solid' : ''}" data-na="${esc(d.action.act)}" data-arg="${esc(e.meta?.urn || '')}">${esc(d.action.label)}</button>` : ''}
  </div>`;
}

function drawPanel() {
  if (!panel) return;
  const list = $('.np-list', panel);
  $$np(panel).forEach((b) => { b.disabled = b.dataset.np === 'read' ? !S.unread : !S.events.length; });
  const working = S.installs.length ? `<div class="np-sec">Em andamento</div>${S.installs.map((x) => `<div class="np-item np-work">
      <span class="np-ico">${svg('download')}</span>
      <span class="np-text"><b>Instalando ${esc(x.name)}</b><small>O Runtipi está baixando e ligando o app. O sino avisa quando terminar.</small><span class="np-bar" role="progressbar" aria-label="Instalando ${esc(x.name)}"><i></i></span><time>${esc(timeAgo(Math.floor(x.since / 1000)))}</time></span>
    </div>`).join('')}${S.events.length ? '<div class="np-sec">Avisos</div>' : ''}` : '';
  list.innerHTML = working + (S.events.length ? S.events.map((e) => {
    const can = canExpand(e), open = can && opened.has(e.id);
    return `<div class="np-item lv-${esc(e.level)} ${e.read ? '' : 'unread'} ${can ? 'np-can' : ''} ${open ? 'open' : ''}" data-id="${esc(e.id)}"${can ? ` role="button" tabindex="0" aria-expanded="${open}"` : ''}>
      <span class="np-ico" title="${esc(LEVEL[e.level] || '')}">${svg(ICON(e))}</span>
      <span class="np-text"><b>${esc(e.title)}</b>${e.body ? `<small>${esc(e.body)}</small>` : ''}<time datetime="${new Date(e.time * 1000).toISOString()}" title="${esc(new Date(e.time * 1000).toLocaleString('pt-BR'))}">${esc(timeAgo(e.time))}</time>${open ? detailHTML(e) : ''}</span>
      ${can ? `<span class="np-chev" aria-hidden="true">${svg('chevD')}</span>` : ''}
    </div>`;
  }).join('') : `<div class="np-empty">${svg('bell')}<span>Nenhuma notificação.<br><small>Quedas de energia, problemas nos discos e acessos novos ao BrunOS aparecem aqui.</small></span></div>`)
    .replace(/^(<div class="np-empty">.*)$/s, S.installs.length ? '' : '$1');
}
const $$np = (p) => [...p.querySelectorAll('[data-np]')];
