// Escudo do AdGuard na barra do topo (0.34.0, decisão 73). Só aparece quando há uma Integração AdGuard.
// Verde = protegendo; âmbar com contagem = pausado por um tempo; vermelho = desligado até ligar de novo.
// Um toque abre o menu: Pausar 5 min / 30 min / 1 h / Até eu ligar (sem confirmação, porque é
// reversível) ou Ligar agora. Com mais de um AdGuard, a Ação vale para todos.
import { api } from './api.js';
import { $, esc, html, svg, toastError, on } from './ui.js';

let ctx = null, btn = null, panel = null, tick = 0, busy = false, reloadAt = 0;
const PAUSES = [[5, '5 minutos'], [30, '30 minutos'], [60, '1 hora'], [0, 'Até eu ligar']];

/** c = { integrations(), metrics(), openUrl(href), reload() } */
export function initAdguard(c) {
  ctx = c;
  btn = $('#adg-btn');
  btn.addEventListener('click', (e) => { e.stopPropagation(); if (panel) closeAdguard(); else openAdguard(); });
  on('metrics', paint);
  paint();
}

export const adguardOpen = () => !!panel;

const now = () => Math.floor(Date.now() / 1000);
const list = () => (ctx?.integrations() || []).filter((x) => x.kind === 'adguard');

// Estado de um AdGuard: on (protegendo), until (unix em que volta sozinho; 0 = sem prazo), err.
function stateOf(it) {
  const v = ctx.metrics()[it.id];
  if (!v?.values?.protection_on) return { it, unknown: true, err: v?.err || '' };
  const on = v.values.protection_on.n === 1, left = v.values.pause_left?.n || 0;
  return { it, on, until: !on && left > 0 ? (v.at || now()) + left : 0, err: v.err || '' };
}

// Juntando todos: desligado sem prazo > pausado > protegendo.
function overall() {
  const sts = list().map(stateOf), known = sts.filter((s) => !s.unknown);
  if (!sts.length) return null;
  if (!known.length) return { sts, mode: sts.some((s) => s.err) ? 'err' : 'wait' };
  if (known.some((s) => !s.on && !s.until)) return { sts, mode: 'off' };
  const paused = known.filter((s) => !s.on);
  if (paused.length) return { sts, mode: 'paused', until: Math.max(...paused.map((s) => s.until)) };
  return { sts, mode: 'on' };
}

const mmss = (s) => (s >= 3600 ? `${Math.floor(s / 3600)}h${String(Math.floor(s % 3600 / 60)).padStart(2, '0')}` : `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`);
const ICON = { on: 'shieldCheck', paused: 'shieldPause', off: 'shieldOff', err: 'shield', wait: 'shield' };

function paint() {
  if (!btn) return;
  const o = overall();
  btn.hidden = !o;
  document.body.classList.toggle('has-adg', !!o);
  if (!o) { clearInterval(tick); tick = 0; if (panel) closeAdguard(); return; }
  const left = o.mode === 'paused' ? Math.max(0, o.until - now()) : 0;
  btn.className = `bell-btn adg-btn adg-${o.mode}${panel ? ' open' : ''}`;
  btn.querySelector('.svg').innerHTML = svg(ICON[o.mode]).replace(/^<svg[^>]*>|<\/svg>$/g, '');
  const badge = $('.bell-badge', btn);
  badge.hidden = o.mode !== 'paused';
  badge.textContent = o.mode === 'paused' ? mmss(left) : '';
  btn.setAttribute('aria-label', { on: 'AdGuard: protegendo', paused: `AdGuard pausado: volta em ${mmss(left)}`, off: 'AdGuard desligado', err: 'AdGuard sem responder', wait: 'AdGuard' }[o.mode]);
  btn.title = btn.getAttribute('aria-label');
  // a contagem anda sozinha; quando acaba, relê (o AdGuard já voltou, mas a próxima busca pode demorar 1 min)
  if (o.mode === 'paused' && !tick) tick = setInterval(paint, 1000);
  if (o.mode !== 'paused' && tick) { clearInterval(tick); tick = 0; }
  if (o.mode === 'paused' && left === 0 && reloadAt !== o.until) { reloadAt = o.until; setTimeout(() => ctx.reload(), 2500); }
  if (panel) draw();
}

export function closeAdguard() {
  if (!panel) return;
  const p = panel; panel = null;
  p.classList.add('closing');
  setTimeout(() => p.remove(), 160);
  document.removeEventListener('mousedown', outside, true);
  document.removeEventListener('keydown', onKey, true);
  btn?.classList.remove('open');
}
const outside = (e) => { if (panel && !panel.contains(e.target) && !e.target.closest('#adg-btn')) closeAdguard(); };
const onKey = (e) => { if (e.key === 'Escape' && panel) { e.stopPropagation(); closeAdguard(); btn.focus(); } };

export function openAdguard() {
  if (!overall()) return;
  panel = html('<div class="notif-panel pw-menu adg-menu" role="menu" aria-label="AdGuard"></div>');
  document.body.append(panel);
  btn.classList.add('open');
  draw();
  setTimeout(() => { document.addEventListener('mousedown', outside, true); document.addEventListener('keydown', onKey, true); });
  panel.addEventListener('click', (e) => {
    const b = e.target.closest('[data-adg]'); if (!b || b.disabled) return;
    const a = b.dataset.adg;
    if (a === 'open') { closeAdguard(); const it = list()[0]; if (it?.url) ctx.openUrl(it.url); return; }
    act(a === 'resume' ? { action: 'resume' } : { action: 'pause', minutes: +b.dataset.min });
  });
}

function draw() {
  const o = overall(); if (!o) return;
  const left = o.mode === 'paused' ? Math.max(0, o.until - now()) : 0;
  const head = {
    on: ['Protegendo', 'Anúncios e rastreadores bloqueados'],
    paused: ['Pausado', left ? `Volta sozinho em ${mmss(left)}` : 'Voltando…'],
    off: ['Desligado', 'Fica assim até você ligar de novo'],
    err: ['Sem resposta', o.sts.find((s) => s.err)?.err || 'O AdGuard não está respondendo'],
    wait: ['Carregando…', 'Buscando o estado da proteção'],
  }[o.mode];
  const item = (attrs, icon, label, sub = '', cls = '') => `<button class="pw-item ${cls}" role="menuitem" ${attrs}${busy ? ' disabled' : ''}><span class="pw-ico">${svg(icon)}</span><span class="pw-txt"><b>${label}</b>${sub ? `<small>${esc(sub)}</small>` : ''}</span></button>`;
  const can = o.mode !== 'err' && o.mode !== 'wait';
  const off = o.mode === 'paused' || o.mode === 'off';
  panel.innerHTML = `<div class="adg-head adg-${o.mode}"><span class="adg-badge">${svg(ICON[o.mode])}</span><span class="pw-txt"><b>${esc(head[0])}</b><small>${esc(head[1])}</small></span></div>
    ${can && off ? item('data-adg="resume"', 'shieldCheck', 'Ligar agora', '', 'adg-resume') : ''}
    ${can ? `<p class="adg-sec">${off ? 'Pausar de novo por' : 'Pausar a proteção por'}</p>
      ${PAUSES.map(([m, l]) => item(`data-adg="pause" data-min="${m}"`, m ? 'clock' : 'shieldOff', l, m ? '' : 'Avisa no sino enquanto estiver desligado', m ? 'adg-pause' : 'adg-forever')).join('')}` : ''}
    <hr>${item('data-adg="open"', 'globe', 'Abrir o AdGuard', list().length > 1 ? `${list().length} AdGuards: as ações valem para todos` : '', 'adg-open')}`;
}

async function act(req) {
  if (busy) return;
  busy = true; draw();
  try {
    await Promise.all(list().map((it) => api.integrations.action(it.id, req)));
    // mostra já o estado novo; o valor de verdade chega pela conexão ao vivo em seguida
    for (const it of list()) {
      const v = ctx.metrics()[it.id] ||= { id: it.id, values: {} };
      v.values = { ...v.values, protection_on: { n: req.action === 'resume' ? 1 : 0 } };
      if (req.action === 'pause' && req.minutes) v.values.pause_left = { n: req.minutes * 60 }; else delete v.values.pause_left;
      v.at = now();
    }
    busy = false;
    closeAdguard();
    paint();
  } catch (err) {
    busy = false;
    toastError(err, 'O AdGuard não aceitou o pedido');
    if (panel) draw();
  }
}
