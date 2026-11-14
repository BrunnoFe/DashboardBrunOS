// Janelas (0.17.0, decisão 58): gerenciador genérico. Hoje só as Janelas de app usam; na 0.18.0 as
// janelas do sistema (Arquivos, Terminal...) vêm para cá também.
//
// - Estilo das janelas (Aparência): Flutuante (arrasta, redimensiona, encaixa, várias ao mesmo tempo) ou
//   Folha (centralizada, uma por vez, fundo escurecido). Tela estreita: sempre tela cheia.
// - Minimizar esconde sem descarregar (o app continua carregado); fechar descarta.
// - A dock fica sempre por cima; com janela maximizada ela fica visível, se esconde sozinha (volta com o
//   mouse na beirada de baixo) ou o app para onde ela começa ("Dock com app maximizado").
// - Posição, tamanho e "maximizada" ficam por app e por aparelho (localStorage).
// - O voltar do navegador/celular minimiza a janela da frente; F5 com janela aberta pergunta antes.
import { $, esc, svg, html, debounce } from './ui.js';
import { defaultRect, clampRect, snapZone, resizeRect, pickToClose, dockAction } from './winmath.js';

let cfg = { settings: () => ({}), confirm: async () => true, onChange: () => {} };
const wins = new Map(); // id → { id, title, el, z, min, used, state: normal|max|left|right, rect, cleanup, tools }
let host = null, zTop = 10, histPushed = false, skipUnload = false, peekT = 0, peekAt = 0, wasMaxed = false;
const narrowMQ = matchMedia('(max-width: 759px), (max-height: 499px)');
const EASE = 'cubic-bezier(0.23, 1, 0.32, 1)';
const KEY = 'brunos.win.';

export function initWindows(o) {
  cfg = { ...cfg, ...o };
  host = $('#win-host');
  host.innerHTML = '<div id="win-backdrop"></div><div id="win-snap"></div>';
  $('#win-backdrop').addEventListener('click', () => { const f = front(); if (f) setMin(f, true); });
  addEventListener('resize', debounce(layout, 80));
  narrowMQ.addEventListener?.('change', layout);
  // clique dentro do app (iframe): a página perde o foco para ele; traz aquela janela para a frente
  addEventListener('blur', () => setTimeout(() => {
    const el = document.activeElement?.closest?.('.win');
    if (el && wins.has(el.dataset.win)) focus(el.dataset.win);
    if (Date.now() - peekAt > 600) unpeek(); // tocou/clicou no app: a dock volta a se esconder
  }));
  addEventListener('popstate', () => {
    if (!histPushed) return;
    histPushed = false;
    const f = front(); if (!f) return;
    const more = visible().length > 1;
    setMin(f, true);
    if (more) pushHist();
  });
  addEventListener('beforeunload', (e) => {
    if (skipUnload || !wins.size) return;
    e.preventDefault(); e.returnValue = '';
  });
  // dock que se esconde sozinha: faixa na beirada de baixo (por cima dos apps) traz de volta
  const peek = $('#dock-peek'), dock = $('#dock');
  peek?.addEventListener('pointerenter', (e) => { if (e.pointerType === 'mouse') peekDock(0); });
  peek?.addEventListener('pointerdown', () => peekDock(3500));
  // no toque, o "clique" que vem depois cairia no app (a faixa some quando a dock aparece) e tiraria o foco
  peek?.addEventListener('touchstart', (e) => { e.preventDefault(); peekDock(3500); }, { passive: false });
  dock?.addEventListener('pointerenter', () => clearTimeout(peekT));
  dock?.addEventListener('pointerleave', (e) => { if (e.pointerType === 'mouse' && document.body.classList.contains('dock-peek')) peekDock(700); });
}

const st = () => cfg.settings() || {};
const reduced = () => st().reduceMotion || matchMedia('(prefers-reduced-motion: reduce)').matches;
export const mode = () => (narrowMQ.matches ? 'full' : st().winStyle === 'sheet' ? 'sheet' : 'float');
const visible = () => [...wins.values()].filter((w) => !w.min).sort((a, b) => a.z - b.z);
const front = () => visible().at(-1) || null;
const dockSpace = () => { const d = $('#dock'); return d ? d.offsetHeight + 20 : 94; };

export const has = (id) => wins.has(id);
export const count = () => wins.size;
export const list = () => [...wins.values()].map(({ id, title, min, z, used }) => ({ id, title, min, z, used }));
export const frontId = () => front()?.id ?? null;
// recarregamento feito pelo próprio BrunOS (já avisado): não pergunta "Sair do site?"
export const allowUnload = () => { skipUnload = true; };

function load(id) { try { return JSON.parse(localStorage.getItem(KEY + id)) || null; } catch { return null; } }
function save(w) { try { localStorage.setItem(KEY + w.id, JSON.stringify({ rect: w.rect, state: w.state === 'max' ? 'max' : 'normal' })); } catch { /* sem localStorage */ } }
function pushHist() { if (histPushed) return; try { history.pushState({ brunosWin: 1 }, ''); histPushed = true; } catch { /* ok */ } }
function changed() { cfg.onChange?.(); }

// Abre (ou traz de volta) uma janela. o = { id, title, icon (HTML), originEl, mount(body, w) → limpeza?, tools: [{icon, label, run}] }
export async function open(o) {
  const old = wins.get(o.id);
  if (old) { if (old.min) setMin(old, false); else focus(old.id); return true; }
  const limit = narrowMQ.matches ? +st().winLimitMobile || 5 : +st().winLimitPc || 8;
  if (wins.size >= limit) {
    const victim = wins.get(pickToClose(list()));
    if (victim) {
      const ok = await cfg.confirm(`Você já tem ${wins.size} ${wins.size === 1 ? 'app aberto' : 'apps abertos'}`,
        `Para abrir ${o.title}, o BrunOS precisa fechar ${victim.title}. O limite muda em Configurações → Sistema.`, `Fechar ${victim.title}`);
      if (!ok) return false;
      close(victim.id, false);
    }
  }
  const saved = load(o.id);
  const w = {
    id: o.id, title: o.title, z: ++zTop, min: false, used: Date.now(), cleanup: null, tools: o.tools || [],
    state: saved?.state === 'max' ? 'max' : 'normal',
    rect: saved?.rect && saved.rect.w > 0 ? saved.rect : defaultRect(innerWidth, innerHeight, dockSpace()),
  };
  w.el = html(`<section class="win" role="dialog" aria-label="${esc(o.title)}" data-win="${esc(o.id)}">
    <header class="win-bar"><span class="win-ico">${o.icon || ''}</span><span class="win-title">${esc(o.title)}</span><span class="win-sp"></span>
      <span class="win-tools">${w.tools.map((t, i) => `<button class="win-btn" data-tool="${i}" title="${esc(t.label)}" aria-label="${esc(t.label)}">${svg(t.icon)}</button>`).join('')}</span>
      <span class="win-ctl"><button class="win-btn" data-w="min" title="Minimizar" aria-label="Minimizar">${svg('minus')}</button><button class="win-btn win-maxbtn" data-w="max" title="Maximizar" aria-label="Maximizar">${svg('winMax')}</button><button class="win-btn win-close" data-w="close" title="Fechar" aria-label="Fechar">${svg('x')}</button></span>
    </header>
    <div class="win-body"></div>
    ${['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'].map((d) => `<i class="win-rz" data-rz="${d}"></i>`).join('')}
  </section>`);
  wins.set(w.id, w);
  if (mode() === 'sheet') visible().filter((x) => x !== w).forEach((x) => setMin(x, true));
  host.append(w.el);
  bind(w);
  try { w.cleanup = o.mount?.($('.win-body', w.el), w) || null; } catch (e) { console.error(e); }
  layout();
  animateOpen(w, o.originEl);
  pushHist();
  changed();
  return true;
}

export function focus(id) {
  const w = wins.get(id); if (!w) return;
  w.used = Date.now();
  if (front() === w) return;
  w.z = ++zTop;
  w.el.style.zIndex = w.z;
  changed();
}

export const minimize = (id) => { const w = wins.get(id); if (w) setMin(w, true); };
export const restore = (id) => { const w = wins.get(id); if (w) setMin(w, false); };
export const toggleMax = (id) => { const w = wins.get(id); if (w) setMax(w, w.state === 'normal' ? 'max' : 'normal'); };

// Clique no ícone da dock: volta se minimizada, vem para a frente se atrás, minimiza se já é a da frente.
export function dockClick(id) {
  const w = wins.get(id); if (!w) return;
  const a = dockAction(w, front()?.id);
  if (a === 'restore') setMin(w, false);
  else if (a === 'minimize') setMin(w, true);
  else focus(id);
}

export function close(id, anim = true) {
  const w = wins.get(id); if (!w) return;
  wins.delete(id);
  try { w.cleanup?.(); } catch { /* ok */ }
  const el = w.el;
  const done = () => el.remove();
  if (anim && !w.min) animate(el, [{ transform: 'none', opacity: 1 }, { transform: 'scale(.95)', opacity: 0 }], reduced() ? 120 : 160, 'forwards').then(done);
  else done();
  layout();
  changed();
}
export function closeAll() { for (const id of [...wins.keys()]) close(id, false); }

async function setMin(w, on) {
  if (w.min === on) return;
  w.min = on; w.used = Date.now();
  if (on) {
    changed();
    layout();
    await animateMin(w);
  } else {
    w.z = ++zTop;
    if (mode() === 'sheet') visible().filter((x) => x !== w).forEach((x) => setMin(x, true));
    w.el.classList.remove('min');
    layout();
    animateRestore(w);
    pushHist();
    changed();
  }
}

function setMax(w, state) {
  if (mode() === 'full' || w.state === state) return;
  const before = w.el.getBoundingClientRect();
  w.state = state;
  layout();
  flip(w, before);
  save(w);
}

// Desenha tudo conforme o estilo, a tela e as preferências. Chamado também pelo app.js quando as
// preferências mudam.
export function layout() {
  if (!host) return;
  const m = mode(), dm = st().dockWithMax || 'auto';
  host.dataset.mode = m;
  host.style.setProperty('--dock-space', dockSpace() + 'px');
  if (m === 'sheet') visible().slice(0, -1).forEach((w) => setMin(w, true)); // Folha: uma por vez
  for (const w of wins.values()) place(w, m);
  const vis = visible();
  host.classList.toggle('has-visible', vis.length > 0);
  const maxed = vis.some((w) => m === 'full' || w.state === 'max');
  const b = document.body.classList;
  b.toggle('has-wins', wins.size > 0);
  b.toggle('dock-auto', maxed && dm === 'auto');
  b.toggle('dock-above', maxed && dm === 'above');
  if (maxed && dm === 'auto' && !wasMaxed) peekDock(1200); // mostra a dock um instante e esconde
  if (!maxed) unpeek();
  wasMaxed = maxed;
}

function place(w, m = mode()) {
  const el = w.el;
  el.style.zIndex = w.z;
  el.classList.toggle('max', m !== 'full' && w.state === 'max');
  el.classList.toggle('snap-left', m === 'float' && w.state === 'left');
  el.classList.toggle('snap-right', m === 'float' && w.state === 'right');
  const btn = $('.win-maxbtn', el);
  if (btn) {
    const mx = w.state !== 'normal';
    if (btn.dataset.mx !== String(mx)) { btn.dataset.mx = mx; btn.innerHTML = svg(mx ? 'winRestore' : 'winMax'); btn.title = mx ? 'Restaurar' : 'Maximizar'; btn.setAttribute('aria-label', btn.title); }
  }
  if (m === 'float' && w.state === 'normal') {
    const r = (w.rect = clampRect(w.rect, innerWidth, innerHeight));
    el.style.left = r.x + 'px'; el.style.top = r.y + 'px'; el.style.width = r.w + 'px'; el.style.height = r.h + 'px';
  } else el.style.left = el.style.top = el.style.width = el.style.height = '';
}

function peekDock(ms) {
  peekAt = Date.now();
  document.body.classList.add('dock-peek');
  clearTimeout(peekT);
  if (ms) peekT = setTimeout(() => { if (!$('#dock')?.matches(':hover')) unpeek(); else peekDock(700); }, ms);
}
function unpeek() { clearTimeout(peekT); document.body.classList.remove('dock-peek'); }

/* ── Eventos de uma janela ─────────────────────────────────────── */
function bind(w) {
  const el = w.el, bar = $('.win-bar', el);
  el.addEventListener('pointerdown', () => focus(w.id), true);
  el.addEventListener('click', (e) => {
    const b = e.target.closest('[data-w],[data-tool]'); if (!b) return;
    if (b.dataset.tool) { w.tools[+b.dataset.tool]?.run(w); return; }
    const k = b.dataset.w;
    if (k === 'min') setMin(w, true);
    else if (k === 'max') setMax(w, w.state === 'normal' ? 'max' : 'normal');
    else if (k === 'close') close(w.id);
  });
  // dois cliques na barra: maximiza (ou restaura, se já estiver maximizada), como no Windows
  bar.addEventListener('dblclick', (e) => { if (!e.target.closest('button')) setMax(w, w.state === 'max' ? 'normal' : 'max'); });
  bar.addEventListener('pointerdown', (e) => startDrag(w, e));
  el.querySelectorAll('.win-rz').forEach((h) => h.addEventListener('pointerdown', (e) => startResize(w, e, h.dataset.rz)));
}

function gesture(target, e, move, end) {
  e.preventDefault();
  try { target.setPointerCapture(e.pointerId); } catch { /* ok */ }
  host.classList.add('busy'); // os iframes não podem engolir o mouse durante o gesto
  const up = () => {
    target.removeEventListener('pointermove', move);
    target.removeEventListener('pointerup', up);
    target.removeEventListener('pointercancel', up);
    host.classList.remove('busy');
    end();
  };
  target.addEventListener('pointermove', move);
  target.addEventListener('pointerup', up);
  target.addEventListener('pointercancel', up);
}

function startDrag(w, e) {
  if (e.button !== 0 || e.target.closest('button') || mode() !== 'float') return;
  const bar = e.currentTarget, sx = e.clientX, sy = e.clientY;
  let offX = sx - w.rect.x, offY = sy - w.rect.y, moved = false, zone = null, before = null;
  gesture(bar, e, (ev) => {
    if (!moved) {
      if (Math.hypot(ev.clientX - sx, ev.clientY - sy) < 5) return;
      moved = true;
      if (w.state !== 'normal') { // sai do maximizado/encaixe: volta ao tamanho normal debaixo do mouse
        const cur = w.el.getBoundingClientRect(), fx = (sx - cur.left) / cur.width;
        w.state = 'normal';
        offX = Math.round(w.rect.w * fx); offY = sy - cur.top;
        layout();
      }
      before = { ...w.rect }; // se encaixar, o tamanho normal continua sendo o de antes do arraste
    }
    w.rect = { ...w.rect, x: ev.clientX - offX, y: ev.clientY - offY };
    place(w);
    zone = snapZone(ev.clientX, ev.clientY, innerWidth);
    showSnap(zone);
  }, () => {
    showSnap(null);
    if (!moved) return;
    if (zone) { const at = w.el.getBoundingClientRect(); w.rect = before; w.state = zone; layout(); flip(w, at); save(w); }
    else { layout(); save(w); }
  });
}

function startResize(w, e, dir) {
  if (e.button !== 0 || mode() !== 'float' || w.state !== 'normal') return;
  e.stopPropagation();
  const start = { ...w.rect }, sx = e.clientX, sy = e.clientY;
  gesture(e.currentTarget, e, (ev) => {
    w.rect = resizeRect(start, dir, ev.clientX - sx, ev.clientY - sy, innerWidth, innerHeight);
    place(w);
  }, () => save(w));
}

function showSnap(zone) {
  const s = $('#win-snap'); if (!s) return;
  s.className = zone ? `show ${zone}` : '';
}

/* ── Animações (Web Animations; só transform e opacity) ────────── */
function animate(el, frames, ms, fill) {
  try { const a = el.animate(frames, { duration: ms, easing: EASE, fill }); return a.finished.then(() => a, () => a); }
  catch { return Promise.resolve(null); }
}
// transform que leva o retângulo r até o retângulo t (origem no centro)
function toward(r, t) {
  const dx = t.left + t.width / 2 - (r.left + r.width / 2), dy = t.top + t.height / 2 - (r.top + r.height / 2);
  return `translate(${dx}px, ${dy}px) scale(${Math.max(t.width / r.width, 0.02)}, ${Math.max(t.height / r.height, 0.02)})`;
}
function dockTarget(w) {
  const it = document.querySelector(`.dock-item[data-app="${CSS.escape(w.id)}"]`) || $('#dock');
  return it?.getBoundingClientRect();
}
function animateOpen(w, originEl) {
  const el = w.el;
  if (reduced()) return animate(el, [{ opacity: 0 }, { opacity: 1 }], 120);
  if (mode() === 'full') return animate(el, [{ transform: 'translateY(35%)', opacity: 0 }, { transform: 'none', opacity: 1 }], 260);
  const o = originEl?.isConnected && originEl.getBoundingClientRect();
  if (o && o.width) {
    const r = el.getBoundingClientRect();
    return animate(el, [{ transform: toward(r, o), opacity: 0 }, { opacity: 1, offset: 0.4 }, { transform: 'none', opacity: 1 }], 240);
  }
  return animate(el, [{ transform: 'scale(.94)', opacity: 0 }, { transform: 'none', opacity: 1 }], 200);
}
async function animateMin(w) {
  const el = w.el;
  let frames;
  if (reduced()) frames = [{ opacity: 1 }, { opacity: 0 }];
  else if (mode() === 'full') frames = [{ transform: 'none', opacity: 1 }, { transform: 'translateY(45%)', opacity: 0 }];
  else { const t = dockTarget(w); frames = t ? [{ transform: 'none', opacity: 1 }, { opacity: 0.9, offset: 0.6 }, { transform: toward(el.getBoundingClientRect(), t), opacity: 0 }] : [{ opacity: 1 }, { opacity: 0 }]; }
  const a = await animate(el, frames, reduced() ? 120 : 230, 'forwards');
  if (w.min) el.classList.add('min'); // pode ter voltado no meio da animação
  a?.cancel();
}
function animateRestore(w) {
  const el = w.el;
  el.getAnimations?.().forEach((a) => a.cancel());
  if (reduced()) return animate(el, [{ opacity: 0 }, { opacity: 1 }], 120);
  if (mode() === 'full') return animate(el, [{ transform: 'translateY(45%)', opacity: 0 }, { transform: 'none', opacity: 1 }], 240);
  const t = dockTarget(w);
  if (!t) return animate(el, [{ opacity: 0 }, { opacity: 1 }], 160);
  return animate(el, [{ transform: toward(el.getBoundingClientRect(), t), opacity: 0 }, { opacity: 0.9, offset: 0.4 }, { transform: 'none', opacity: 1 }], 240);
}
// maximizar/restaurar/encaixar: desliza do retângulo antigo para o novo
function flip(w, before) {
  if (reduced() || !before.width) return;
  const after = w.el.getBoundingClientRect();
  if (!after.width) return;
  animate(w.el, [{ transform: toward(after, before) }, { transform: 'none' }], 220);
}
