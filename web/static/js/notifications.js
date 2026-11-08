// Central de notificações: o sino no canto da tela inicial e o painel com os avisos do servidor
// (energia caiu/voltou, desligamento seguro, disco desmontado...). Os avisos novos chegam pelo
// stream de estatísticas (event: notify) e também aparecem como toast.
import { api } from './api.js';
import { $, esc, html, svg, toast, timeAgo, emit } from './ui.js';

const S = { events: [], unread: 0 };
let panel = null;

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
  toast(e.title, e.body || '', null, { error: e.level === 'critico', ms: e.level === 'critico' ? 12000 : 7000 });
  emit('notify', e);
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
  b.setAttribute('aria-label', S.unread ? `Notificações: ${S.unread} sem ler` : 'Notificações');
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
    const b = e.target.closest('[data-np]'); if (!b) return;
    try {
      if (b.dataset.np === 'read') { await api.events.read([]); S.events.forEach((x) => { x.read = true; }); S.unread = 0; }
      else { await api.events.clear(); S.events = []; S.unread = 0; }
    } catch (err) { toast('Não deu para atualizar', err.message, null, { error: true }); }
    paintBell(); drawPanel();
  });
}

function drawPanel() {
  if (!panel) return;
  const list = $('.np-list', panel);
  $$np(panel).forEach((b) => { b.disabled = b.dataset.np === 'read' ? !S.unread : !S.events.length; });
  list.innerHTML = S.events.length ? S.events.map((e) => `<div class="np-item lv-${esc(e.level)} ${e.read ? '' : 'unread'}">
      <span class="np-ico" title="${esc(LEVEL[e.level] || '')}">${svg(ICON(e))}</span>
      <span class="np-text"><b>${esc(e.title)}</b>${e.body ? `<small>${esc(e.body)}</small>` : ''}<time datetime="${new Date(e.time * 1000).toISOString()}" title="${esc(new Date(e.time * 1000).toLocaleString('pt-BR'))}">${esc(timeAgo(e.time))}</time></span>
    </div>`).join('') : `<div class="np-empty">${svg('bell')}<span>Nenhuma notificação.<br><small>Quedas de energia, problemas nos discos e acessos novos ao BrunOS aparecem aqui.</small></span></div>`;
}
const $$np = (p) => [...p.querySelectorAll('[data-np]')];
