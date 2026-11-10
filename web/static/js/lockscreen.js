// Tela de bloqueio (J1, termos em CONTEXT.md). Dois modos, escolhidos nas Configurações:
//   - "senha" (padrão): o servidor marca a Sessão como bloqueada e recusa toda a API até a senha
//     (decisão 39); a tela só mostra relógio, data e saudação, nada que venha da API.
//   - "visual": só cobre a tela; qualquer clique ou tecla volta. Mostra também clima e avisos.
// Bloqueia pelo botão, pela busca (Ctrl+K), por Ctrl+Shift+L e por inatividade (só mouse e teclado).
import { api } from './api.js';
import { $, esc, svg, html, on } from './ui.js';

let C = null;          // contexto do app.js
let shown = null;      // 'senha' | 'visual' | null
let lastActive = Date.now();
let clockTimer = 0;

export const isScreenLocked = () => !!shown;
const mode = () => (C.settings().lockMode === 'visual' ? 'visual' : 'senha');

/**
 * ctx: { settings(), displayName(), greeting(), extra() → { weather: {temp, icon, text}|null, unread },
 *        active() → a área de trabalho está à vista, onLocked(mode), onUnlocked(mode), onEnded() }
 */
export function initLock(ctx) {
  C = ctx;
  const mark = () => { lastActive = Date.now(); };
  for (const ev of ['pointerdown', 'pointermove', 'keydown', 'wheel', 'touchstart']) addEventListener(ev, mark, { passive: true, capture: true });
  setInterval(() => {
    const min = C.settings().lockIdleMin ?? 15;
    if (!shown && min > 0 && C.active() && Date.now() - lastActive > min * 60000) lockNow();
  }, 15000);
  // outra aba deste navegador bloqueou (a API respondeu 423): mostra a tela aqui também
  on('locked', () => { if (!shown && C.active()) showLock('senha'); });
}

/** Bloqueia agora, no modo das Configurações. */
export async function lockNow() {
  if (shown) return;
  const m = mode();
  if (m === 'senha') {
    try { await api.lock(); } catch (e) { if (e.status === 401) { C.onEnded(); return; } }
  }
  showLock(m);
}

/** Mostra a Tela de bloqueio (também ao abrir o BrunOS com a Sessão já bloqueada no servidor). */
export function showLock(m = 'senha') {
  if (shown) return;
  shown = m;
  document.activeElement?.blur?.();
  const el = html(`<section id="lockscreen" class="ls-${m}" aria-label="Tela bloqueada" role="dialog" aria-modal="true">
    <div class="ls-clock"><div class="ls-date"></div><div class="ls-time"></div><div class="ls-greet"></div><div class="ls-extra"></div></div>
    ${m === 'senha' ? `<form class="ls-form" autocomplete="off">
        <span class="ls-lock">${svg('lock')}</span>
        <div class="pw-field"><input name="password" type="password" placeholder="Senha do BrunOS" autocomplete="current-password" aria-label="Senha do BrunOS"><button type="submit" class="pw-eye ls-go" aria-label="Desbloquear">${svg('chevR')}</button></div>
        <p class="ls-err" role="alert"></p>
      </form>` : `<p class="ls-hint">${svg('lock')}Clique ou aperte qualquer tecla para continuar</p>`}
  </section>`);
  document.body.append(el);
  document.body.classList.add('screen-locked');
  requestAnimationFrame(() => el.classList.add('in'));
  tick();
  clockTimer = setInterval(tick, 1000);
  C.onLocked(m);
  if (m === 'senha') {
    const f = $('form', el);
    setTimeout(() => f.password.focus(), 350);
    f.addEventListener('submit', (e) => { e.preventDefault(); unlock(f); });
  } else {
    const go = (e) => { if (e.type === 'keydown' && ['Shift', 'Control', 'Alt', 'Meta'].includes(e.key)) return; hideLock(); };
    const t = setTimeout(() => { el.addEventListener('pointerdown', go); addEventListener('keydown', go, true); }, 400);
    visualOff = () => { clearTimeout(t); removeEventListener('keydown', go, true); };
  }
}
let visualOff = null;

async function unlock(f) {
  const err = $('.ls-err', f), btn = $('.ls-go', f), pw = f.password.value;
  if (!pw) { f.password.focus(); return; }
  err.textContent = ''; btn.disabled = true;
  try {
    await api.unlock(pw);
    hideLock();
  } catch (e) {
    if (e.status === 401) { hideLock(true); C.onEnded(e.message); return; }
    err.textContent = e.status === 429 ? e.message : 'Senha incorreta';
    f.classList.remove('shake'); void f.offsetWidth; f.classList.add('shake');
    f.password.select();
  } finally { btn.disabled = false; }
}

/** Some com a tela. ended = a Sessão acabou (vai para o login, sem avisar que desbloqueou). */
export function hideLock(ended = false) {
  const el = $('#lockscreen'); if (!el) return;
  const m = shown;
  shown = null; lastActive = Date.now();
  clearInterval(clockTimer);
  visualOff?.(); visualOff = null;
  document.body.classList.remove('screen-locked');
  el.classList.remove('in'); el.classList.add('out');
  setTimeout(() => el.remove(), 450);
  if (!ended) C.onUnlocked(m);
}

function tick() {
  const el = $('#lockscreen'); if (!el) return;
  const d = new Date();
  $('.ls-time', el).textContent = d.toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' });
  const date = d.toLocaleDateString('pt-BR', { weekday: 'long', day: 'numeric', month: 'long' });
  $('.ls-date', el).textContent = date.charAt(0).toUpperCase() + date.slice(1);
  $('.ls-greet', el).textContent = C.greeting();
  if (shown !== 'visual') return;
  const x = C.extra(), box = $('.ls-extra', el);
  const parts = [];
  if (x.weather) parts.push(`<span>${svg(x.weather.icon || 'wx-cloud', 'wx')}${esc(x.weather.temp)}${x.weather.text ? ` · ${esc(x.weather.text)}` : ''}</span>`);
  if (x.unread) parts.push(`<span>${svg('bell')}${x.unread} ${x.unread === 1 ? 'aviso' : 'avisos'}</span>`);
  const h = parts.join('');
  if (box.innerHTML !== h) box.innerHTML = h;
}
