// Tela de atualização (0.33.0, decisão 70): tela cheia enquanto o BrunOS está fora do ar por uma
// atualização ou um reinício. A logo faz a "órbita" dentro de um anel que mostra as etapas; quando o
// servidor volta, a página recarrega sozinha. Quem chama: updates.js (o próprio BrunOS e o Linux),
// powermenu.js (reiniciar/desligar o servidor) e disksapp.js (reiniciar o BrunOS).
import { api } from './api.js';
import { esc, toast } from './ui.js';
import { allowUnload } from './windows.js';

const KINDS = {
  self: { title: 'Atualizando o BrunOS', steps: ['Backup', 'Baixando', 'Reiniciando', 'Voltando'], down: 2 },
  linux: { title: 'Atualizando o Linux', sub: 'O Docker foi atualizado e os apps estão reiniciando.', steps: ['Instalando', 'Reiniciando', 'Voltando'], down: 1 },
  reboot: { title: 'Reiniciando o servidor', sub: 'Os apps e o BrunOS voltam sozinhos em alguns minutos.', steps: ['Desligando', 'Ligando', 'Voltando'], down: 1 },
  restart: { title: 'Reiniciando o BrunOS', steps: ['Reiniciando', 'Voltando'], down: 0 },
  shutdown: { title: 'Desligando o servidor', steps: ['Desligando', 'Desligado'], down: 1 },
};
const R = 68, C = 2 * Math.PI * R; // anel
const EVERY = 2000;                // de quanto em quanto tempo pergunta ao servidor
const SLOW = 8 * 60 * 1000;        // fora do ar por mais que isso: avisa que está demorando

let S = null;

export const updScreenOn = () => !!S;
export const updScreenKind = () => S?.kind || '';

/**
 * Mostra a tela. o = { kind: 'self'|'linux'|'reboot'|'restart'|'shutdown', from, to, backup (self),
 * step, expectDownMs (se o servidor não cair nesse tempo, a tela some: ele não foi) }.
 */
export function showUpdScreen(o) {
  if (S) { if (S.kind === o.kind) return; hideUpdScreen(); }
  const k = KINDS[o.kind]; if (!k) return;
  // sem backup, a atualização do BrunOS não tem a primeira etapa
  const steps = o.kind === 'self' && !o.backup ? k.steps.slice(1) : k.steps;
  const down = o.kind === 'self' && !o.backup ? k.down - 1 : k.down;
  const ver = o.from && o.to ? `<div class="us-ver">${esc(o.from)} → <b>${esc(o.to)}</b></div>` : '';
  const el = document.createElement('section');
  el.id = 'upd-screen'; el.setAttribute('role', 'alertdialog'); el.setAttribute('aria-modal', 'true'); el.setAttribute('aria-live', 'polite');
  el.innerHTML = `<div class="us-art" aria-hidden="true">
      <svg class="us-ring" viewBox="0 0 148 148"><circle cx="74" cy="74" r="${R}" class="us-track"/><circle cx="74" cy="74" r="${R}" class="us-prog" stroke-dasharray="${C}" stroke-dashoffset="${C}"/><circle cx="74" cy="74" r="${R}" class="us-comet" stroke-dasharray="60 ${C}"/></svg>
      <svg class="us-logo" viewBox="0 0 64 64" fill="none"><circle class="us-halo" cx="32" cy="34" r="8"/><path class="us-arc" d="M50.8 25.2A20 20 0 1 1 38.8 13.2" stroke="#fff" stroke-width="5.5" stroke-linecap="round"/><g class="us-orb"><circle cx="46.6" cy="17.4" r="5" fill="#fff"/></g><circle class="us-core" cx="32" cy="34" r="6.5" fill="#fff"/></svg>
    </div>
    <h2 class="us-title">${esc(k.title)}<span class="us-dots" aria-hidden="true"></span></h2>${ver}
    ${k.sub ? `<p class="us-sub">${esc(k.sub)}</p>` : ''}
    <div class="us-steps">${steps.map((s) => `<span class="us-step"><i></i>${esc(s)}</span>`).join('')}</div>
    <p class="us-hint">Pode deixar esta página aberta: ela volta sozinha quando terminar.</p>`;
  document.body.append(el);
  document.body.classList.add('upd-screen-on');
  document.activeElement?.blur?.();
  // 0.35.0: na atualização do próprio BrunOS, só recarrega quando responder a versão NOVA. O backup do
  // Runtipi para e religa a versão antiga antes de trocar a imagem: recarregar nessa volta deixava a
  // página na versão velha (o Bruno precisava apertar F5 depois).
  S = { el, kind: o.kind, steps, downStep: down, step: -1, fails: 0, downAt: 0, version0: '', startedAt: Date.now(),
    expectBy: o.expectDownMs ? Date.now() + o.expectDownMs : 0, timer: 0, to: o.kind === 'self' ? o.to || '' : '', needNew: o.kind === 'self' };
  setStep(o.step ?? 0);
  // a versão de agora: se mudar, é porque o BrunOS novo já está no ar
  api.public().then((j) => { if (S) S.version0 = j.version || ''; }).catch(() => {});
  S.timer = setTimeout(watch, EVERY);
}

/** A atualização do BrunOS terminou bem no servidor: a próxima resposta, de qualquer versão, recarrega. */
export function updScreenJobDone() { if (S) S.needNew = false; }

/** Avança para a etapa i (não volta para trás). */
export function updScreenStep(i) { if (S && i > S.step) setStep(i); }

export function hideUpdScreen() {
  if (!S) return;
  clearTimeout(S.timer);
  const el = S.el; S = null;
  el.classList.add('out');
  document.body.classList.remove('upd-screen-on');
  setTimeout(() => el.remove(), 400);
}

function setStep(i) {
  i = Math.max(0, Math.min(S.steps.length - 1, i));
  S.step = i;
  S.el.querySelectorAll('.us-step').forEach((s, j) => { s.className = `us-step ${j < i ? 'done' : j === i ? 'on' : ''}`; });
  const p = S.el.querySelector('.us-prog');
  if (p) p.style.strokeDashoffset = String(C * (1 - (i + 0.5) / S.steps.length));
}

// Pergunta ao servidor (rota pública, sem sessão) até ele cair e voltar.
async function watch() {
  if (!S) return;
  const me = S;
  let v = null;
  try { v = (await api.public()).version || ''; } catch { /* fora do ar */ }
  if (S !== me) return;
  if (v === null) { // não respondeu
    if (++S.fails >= 2) { // duas seguidas (4 s): caiu de verdade, não foi um soluço da rede
      if (!S.downAt) S.downAt = Date.now();
      setStep(S.downStep);
      if (S.kind === 'shutdown' || Date.now() - S.downAt > SLOW) slowNote();
    }
  } else {
    const changed = S.version0 && v && v !== S.version0;
    const norm = (x) => String(x || '').trim().replace(/^v/i, '');
    const isNew = S.to ? norm(v) === norm(S.to) : changed; // versão de destino conhecida: tem que ser ela
    if (isNew || (S.downAt && !S.needNew)) { back(); return; }
    if (S.downAt) { S.downAt = 0; setStep(Math.max(0, S.downStep - 1)); } // voltou a antiga (backup): espera a troca
    S.fails = 0;
    if (S.expectBy && Date.now() > S.expectBy) { // o servidor não caiu: o pedido não foi adiante
      const kind = S.kind; hideUpdScreen();
      toast(kind === 'shutdown' ? 'O servidor não desligou' : 'O servidor não reiniciou', 'O pedido não foi adiante. Confira os avisos no sino.', null, { error: true });
      return;
    }
  }
  S.timer = setTimeout(watch, EVERY);
}

// Voltou: última etapa e recarrega a página (o desligado só recarrega quando o servidor liga de novo).
function back() {
  setStep(S.steps.length - 1);
  S.el.classList.add('back');
  setTimeout(() => { allowUnload(); location.reload(); }, 1400);
}

function slowNote() {
  if (S.el.querySelector('.us-slow')) return;
  const p = document.createElement('p');
  p.className = 'us-slow';
  p.innerHTML = S.kind === 'shutdown'
    ? 'O servidor está desligado. Quando ele ligar de novo, esta página volta sozinha.'
    : 'Está demorando mais que o normal. Confira se o servidor ligou. <button type="button" class="pill-btn sm">Recarregar agora</button>';
  p.querySelector('button')?.addEventListener('click', () => { allowUnload(); location.reload(); });
  S.el.querySelector('.us-hint')?.before(p);
}
