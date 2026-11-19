// Menu de energia (0.25.0, decisão 64): o botão na ponta do canto (depois do sino) abre um menu pequeno
// com Bloquear, Sair, Reiniciar e Desligar. Reiniciar e Desligar pedem a senha do BrunOS; o servidor espera
// 1 minuto (contagem no botão e no menu, com Cancelar) e quem desliga é o próprio servidor.
import { api } from './api.js';
import { $, esc, html, svg, toast, toastError, openDialog, on } from './ui.js';
import { redrawNotifications } from './notifications.js';

let ctx = null, panel = null, state = { hostReady: null, manual: null }, tick = 0;

const LABEL = { desligar: 'Desligar', reiniciar: 'Reiniciar' };
const GOING = { desligar: 'Desligando', reiniciar: 'Reiniciando' };

/** Liga o botão. c = { lock(), logout(), openPower() } */
export function initPowerMenu(c) {
  ctx = c;
  $('#power-btn').addEventListener('click', (e) => { e.stopPropagation(); if (panel) closePowerMenu(); else openPowerMenu(); });
  // o aviso do pedido (e o de cancelado) chega pelo sino, também quando o pedido veio de outra aba
  on('notify', ({ detail: e }) => {
    if (e.kind === 'power-manual' && e.meta?.deadline) setManual({ action: e.meta.action, deadline: e.meta.deadline });
    else if (e.kind === 'power-manual-cancelled') setManual(null);
  });
}

export const powerMenuOpen = () => !!panel;

/** Relê do servidor se a conexão está pronta e se há pedido em andamento (ao conectar e ao abrir o menu). */
export const refreshPowerMenu = () => refresh();
async function refresh() {
  try {
    const r = await api.power.get();
    state.hostReady = !!r.hostReady;
    setManual(r.manual ? { action: r.manual.action, deadline: Math.floor(Date.now() / 1000) + r.manual.remaining } : null);
  } catch { /* sem resposta: o menu mostra o que já sabe */ }
}

const remaining = () => (state.manual ? Math.max(0, state.manual.deadline - Math.floor(Date.now() / 1000)) : 0);
const mmss = (s) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;

function setManual(m) {
  state.manual = m;
  clearInterval(tick); tick = 0;
  if (m) tick = setInterval(paint, 1000);
  paint();
  redrawNotifications();
}

// Botão (anel e contagem) e o menu aberto.
function paint() {
  const b = $('#power-btn'); if (!b) return;
  const m = state.manual, left = remaining(), badge = $('.bell-badge', b);
  b.classList.toggle('pending', !!m);
  badge.hidden = !m;
  badge.textContent = m ? (left ? mmss(left) : '…') : '';
  b.setAttribute('aria-label', m ? `${GOING[m.action]} o servidor em ${left} segundos` : 'Energia');
  if (m && left <= 0 && Date.now() / 1000 > m.deadline + 150) setManual(null); // o servidor não foi: some
  if (panel) draw();
}

export function closePowerMenu() {
  if (!panel) return;
  const p = panel; panel = null;
  p.classList.add('closing');
  setTimeout(() => p.remove(), 160);
  document.removeEventListener('mousedown', outside, true);
  $('#power-btn').classList.remove('open');
}
const outside = (e) => { if (panel && !panel.contains(e.target) && !e.target.closest('#power-btn') && !e.target.closest('.dlg-wrap')) closePowerMenu(); };

export function openPowerMenu() {
  panel = html('<div class="notif-panel pw-menu" role="menu" aria-label="Energia"></div>');
  document.body.append(panel);
  $('#power-btn').classList.add('open');
  draw();
  refresh();
  setTimeout(() => document.addEventListener('mousedown', outside, true));
  panel.addEventListener('click', (e) => {
    const b = e.target.closest('[data-pw]'); if (!b || b.disabled) return;
    const a = b.dataset.pw;
    if (a === 'cancel') { cancelManual(b); return; }
    closePowerMenu();
    if (a === 'lock') ctx.lock();
    else if (a === 'logout') ctx.logout();
    else powerAction(a);
  });
}

function draw() {
  const m = state.manual, left = remaining(), noHost = state.hostReady === false;
  const item = (act, icon, label, sub = '', extra = '') =>
    `<button class="pw-item pw-${act}" role="menuitem" data-pw="${act}" ${extra}><span class="pw-ico">${svg(icon)}</span><span class="pw-txt"><b>${label}</b>${sub ? `<small>${esc(sub)}</small>` : ''}</span></button>`;
  const hostSub = noHost ? 'Configure em Energia' : '';
  panel.innerHTML = `${m ? `<div class="pw-pending"><span class="pw-ring" style="--p:${Math.min(1, left / 60)}"><b>${left ? mmss(left) : '…'}</b></span>
      <span class="pw-txt"><b>${left ? `${GOING[m.action]} em ${left} s` : `${GOING[m.action]}…`}</b><small>${left ? 'O servidor espera 1 minuto.' : 'O BrunOS volta sozinho quando o servidor ligar.'}</small></span>
      ${left ? '<button class="pill-btn sm" data-pw="cancel">Cancelar</button>' : ''}</div>` : ''}
    ${item('lock', 'lock', 'Bloquear', 'Ctrl+Shift+L')}
    ${item('logout', 'logout', 'Sair')}
    <hr>
    ${item('reiniciar', 'refresh', 'Reiniciar', hostSub, m ? 'disabled' : '')}
    ${item('desligar', 'power', 'Desligar', hostSub, m ? 'disabled' : '')}`;
}

async function cancelManual(btn) {
  btn.disabled = true; btn.textContent = 'Cancelando…';
  try { await api.power.cancelManual(); setManual(null); } // o aviso "cancelado" chega pelo sino
  catch (err) { toastError(err, 'Não deu para cancelar'); btn.disabled = false; btn.textContent = 'Cancelar'; refresh(); }
}

/** Desligar ou reiniciar (também chamado pelas Configurações e pelo painel de Atualizações). */
export async function powerAction(action) {
  if (state.hostReady === null) await refresh();
  if (state.manual) { openPowerMenu(); return; }
  if (state.hostReady === false) {
    toast('Configure a conexão com o servidor', 'Desligar e reiniciar usam a mesma conexão do desligamento seguro. Ela fica em Configurações → Energia.');
    ctx.openPower();
    return;
  }
  const reboot = action === 'reiniciar';
  const d = openDialog(`<h2>${svg(reboot ? 'refresh' : 'power')}${reboot ? 'Reiniciar' : 'Desligar'} o servidor?</h2>
    <p class="dlg-text">Os apps e o BrunOS vão parar. O servidor espera 1 minuto antes${reboot ? ' e volta sozinho em seguida' : ''}; até lá dá para cancelar pelo botão de energia.${reboot ? '' : ' <b>Para ligar de novo, é preciso apertar o botão do servidor.</b>'}</p>
    <form class="form-grid"><label>Senha do BrunOS<input class="text-input" type="password" name="password" autocomplete="current-password" required autofocus></label>
    <p class="form-error"></p>
    <div class="dlg-actions"><button type="button" class="pill-btn" data-c="no">Cancelar</button><button type="submit" class="pill-btn ${reboot ? 'primary' : 'danger-solid'}">${LABEL[action]}</button></div></form>`, { closeBtn: false, width: 440 });
  const f = $('form', d.el), err = $('.form-error', d.el), ok = $('[type=submit]', d.el);
  $('[data-c="no"]', d.el).onclick = () => d.close();
  f.onsubmit = async (e) => {
    e.preventDefault();
    ok.disabled = true; err.textContent = '';
    try {
      const r = await api.power.manual(action, f.password.value);
      setManual({ action, deadline: Math.floor(Date.now() / 1000) + (r.manual?.remaining ?? 60) });
      d.close(); // o aviso do servidor ("O servidor vai ... em 1 minuto") chega pelo sino em seguida
    } catch (x) {
      ok.disabled = false;
      if (x.data?.setup) { d.close(); state.hostReady = false; ctx.openPower(); toast('Configure a conexão com o servidor', x.message); return; }
      err.textContent = x.message;
      f.password.select();
    }
  };
}

/** Há desligamento/reinício agendado (o aviso do sino só mostra Cancelar enquanto houver). */
export const powerPending = () => !!state.manual && remaining() > 0;

/** Para o aviso do sino: cancela o pedido em andamento. */
export async function cancelPowerManual() {
  try { await api.power.cancelManual(); setManual(null); }
  catch (err) { toastError(err, 'Não deu para cancelar'); refresh(); }
}
