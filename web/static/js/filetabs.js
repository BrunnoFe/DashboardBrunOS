// Guias do Arquivos (0.23.1): cada janela do Arquivos tem uma barra de guias, como um navegador. Cada guia é
// um Arquivos completo (files.js) com o próprio estado. Destacar uma guia (arrastar para fora da barra ou
// "Mover para nova janela") leva a página dela inteira para uma janela nova, sem recarregar: a pasta, o
// voltar/avançar e a seleção continuam. As guias da janela principal voltam sozinhas neste aparelho.
//
// Atalhos: Alt+T nova guia, Alt+W fecha a guia, Alt+1…9 vai para a guia (o Chrome não deixa sites usarem
// Ctrl+T, Ctrl+W nem Ctrl+Tab, que são das abas dele). Clique do meio numa guia fecha.
import { $, esc, svg, html, contextMenu } from './ui.js';
import { mountFiles } from './files.js';
import { mode as winMode } from './windows.js';

const KEY = 'brunos.files.tabs';
const MAX = 12;
let seq = 0;
let main = null; // controlador da janela principal (para "abrir pasta" vindo de fora)

const load = () => { try { return JSON.parse(localStorage.getItem(KEY)) || null; } catch { return null; } };
const store = (v) => { try { localStorage.setItem(KEY, JSON.stringify(v)); } catch { /* sem localStorage */ } };

/** o = { ctx, opts (pasta pedida), adopt (guia vinda de outra janela), persist (janela principal),
 *        closeWin(), onTitle(título da Guia ativa) → nome da janela,
 *        detach(tab, { at, fromRect } | { slide }) → abre uma janela nova com a guia } */
export function mountTabs(body, o) {
  body.innerHTML = `<div class="ftabs"><div class="ftabs-bar"><div class="ftabs-list" role="tablist"></div>
    <button class="ftabs-add" data-tb="add" title="Nova guia (Alt+T)" aria-label="Nova guia">${svg('plus')}</button></div>
    <div class="ftabs-pages"></div></div>`;
  const list = $('.ftabs-list', body), pages = $('.ftabs-pages', body), bar = $('.ftabs-bar', body);
  const T = { tabs: [], active: null, newTab, adopt, update };

  function newTab(fopts = {}, focus = true) {
    if (T.tabs.length >= MAX) return null;
    const tab = { id: ++seq, el: document.createElement('div'), title: 'Arquivos', loc: null, owner: T };
    tab.el.className = 'ftab-page';
    pages.append(tab.el);
    tab.dispose = mountFiles(tab.el, { ...fopts, ctx: o.ctx,
      openTab: (root, path) => tab.owner.newTab({ root, path }, false),
      onTitle: (title, loc) => { tab.title = title; tab.loc = loc; tab.owner.update(tab); } });
    T.tabs.push(tab);
    if (focus || !T.active) activate(tab); else { tab.el.hidden = true; draw(); }
    return tab;
  }
  // guia que veio de outra janela: a página (e o estado do files.js dentro dela) muda de lugar, sem recarregar
  function adopt(tab) {
    tab.owner = T;
    pages.append(tab.el);
    T.tabs.push(tab);
    activate(tab);
  }
  function update(tab) { if (T.tabs.includes(tab)) { draw(); save(); } }
  function activate(tab) {
    T.active = tab;
    for (const t of T.tabs) t.el.hidden = t !== tab;
    draw(); save();
  }
  function closeTab(tab) {
    const i = T.tabs.indexOf(tab); if (i < 0) return;
    T.tabs.splice(i, 1);
    try { tab.dispose?.(); } catch { /* ok */ }
    tab.el.remove();
    if (!T.tabs.length) { save(); o.closeWin(); return; }
    if (T.active === tab) activate(T.tabs[Math.min(i, T.tabs.length - 1)]); else { draw(); save(); }
  }
  function detach(tab, drop) {
    if (T.tabs.length < 2) return; // a única guia já é a janela inteira
    const i = T.tabs.indexOf(tab);
    T.tabs.splice(i, 1);
    tab.el.remove(); // sai do DOM sem descartar: a janela nova adota a mesma página
    if (T.active === tab) activate(T.tabs[Math.min(i, T.tabs.length - 1)]); else { draw(); save(); }
    o.detach(tab, drop || (winMode() === 'full' ? { slide: true } : {})); // tela cheia: a janela nova entra deslizando
  }
  function draw() {
    list.innerHTML = T.tabs.map((t, i) => `<div class="ftab ${t === T.active ? 'on' : ''}" role="tab" tabindex="0" aria-selected="${t === T.active}" data-tab="${t.id}" title="${esc(t.title)}${i < 9 ? ` (Alt+${i + 1})` : ''}">
      <span class="ftab-ico">${svg(t.loc?.mode === 'trash' ? 'trash' : 'folder')}</span><span class="ftab-txt">${esc(t.title)}</span>
      <button class="ftab-x" data-tb="close" aria-label="Fechar a guia ${esc(t.title)}" title="Fechar a guia (Alt+W)">${svg('x')}</button></div>`).join('');
    bar.classList.toggle('single', T.tabs.length === 1);
    $('[data-tb="add"]', body).disabled = T.tabs.length >= MAX;
    o.onTitle?.(T.active?.title);
  }
  function save() {
    if (!o.persist) return;
    store({ tabs: T.tabs.filter((t) => t.loc?.root).map((t) => ({ root: t.loc.root, path: t.loc.mode === 'browse' ? t.loc.path : '' })), active: Math.max(0, T.tabs.indexOf(T.active)) });
  }
  const byId = (el) => T.tabs.find((t) => t.id === +el?.closest('[data-tab]')?.dataset.tab);
  const here = () => (T.active?.loc ? { root: T.active.loc.root, path: T.active.loc.mode === 'browse' ? T.active.loc.path : '' } : {});

  /* ── Eventos ── */
  body.addEventListener('click', (e) => {
    const b = e.target.closest('[data-tb]'), tab = byId(e.target);
    if (b?.dataset.tb === 'add') newTab(here());
    else if (b?.dataset.tb === 'close' && tab) closeTab(tab);
    else if (tab && e.target.closest('.ftabs-list')) activate(tab);
  });
  list.addEventListener('auxclick', (e) => { const tab = byId(e.target); if (e.button === 1 && tab) { e.preventDefault(); closeTab(tab); } });
  list.addEventListener('mousedown', (e) => { if (e.button === 1) e.preventDefault(); });
  list.addEventListener('keydown', (e) => { const tab = byId(e.target); if (tab && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); activate(tab); } });
  list.addEventListener('contextmenu', (e) => {
    const tab = byId(e.target); if (!tab) return;
    e.preventDefault();
    contextMenu(e.clientX, e.clientY, [
      { icon: 'plus', label: 'Nova guia', run: () => newTab(here()), kbd: 'Alt+T' },
      { icon: 'copy', label: 'Duplicar guia', run: () => newTab(tab.loc ? { root: tab.loc.root, path: tab.loc.path } : {}) },
      { icon: 'external', label: 'Mover para nova janela', hidden: T.tabs.length < 2, run: () => detach(tab) },
      '-',
      { icon: 'x', label: 'Fechar guia', run: () => closeTab(tab), kbd: 'Alt+W' },
      { icon: 'x', label: 'Fechar as outras guias', hidden: T.tabs.length < 2, run: () => { for (const t of [...T.tabs]) if (t !== tab) closeTab(t); } },
    ]);
  });
  // arrastar uma guia para fora da barra: vira uma janela nova (dentro da barra, só reordena). 0.38.0: fora
  // da barra, uma miniatura da janela segue o mouse e a janela nova cresce dela, onde soltou. Em tela
  // cheia (celular) não sai da barra: lá fica o "Mover para nova janela" do menu.
  list.addEventListener('pointerdown', (e) => {
    const tab = byId(e.target);
    if (!tab || e.button !== 0 || e.target.closest('[data-tb]')) return;
    const el = e.target.closest('.ftab'), sx = e.clientX, sy = e.clientY, canOut = winMode() !== 'full';
    let out = false, moved = false, ghost = null, last = { x: sx, y: sy };
    const showGhost = (on, ev) => {
      if (on && !ghost) {
        ghost = html(`<div class="ftab-ghost" aria-hidden="true"><div class="ftab-ghost-bar"><span class="ftab-ico">${svg(tab.loc?.mode === 'trash' ? 'trash' : 'folder')}</span><b>${esc(tab.title)}</b></div><div class="ftab-ghost-body"><i></i><i></i><i></i><i></i></div></div>`);
        document.body.append(ghost);
      } else if (!on && ghost) { ghost.remove(); ghost = null; }
      if (ghost && ev) ghost.style.transform = `translate(${ev.clientX - 70}px, ${ev.clientY - 14}px)`;
    };
    const move = (ev) => {
      const r = bar.getBoundingClientRect();
      moved ||= Math.hypot(ev.clientX - sx, ev.clientY - sy) > 6;
      out = canOut && T.tabs.length > 1 && (ev.clientY < r.top - 30 || ev.clientY > r.bottom + 40);
      last = { x: ev.clientX, y: ev.clientY };
      showGhost(out, ev);
      el.classList.toggle('dragging', moved); el.classList.toggle('out', out);
      if (moved && !out) { // reordena pela posição do mouse
        const over = byId(document.elementFromPoint(ev.clientX, r.top + r.height / 2));
        if (over && over !== tab) { const a = T.tabs.indexOf(tab), b = T.tabs.indexOf(over); T.tabs.splice(a, 1); T.tabs.splice(b, 0, tab); draw(); save(); }
      }
    };
    const up = () => {
      removeEventListener('pointermove', move); removeEventListener('pointerup', up); removeEventListener('pointercancel', up);
      el.classList.remove('dragging', 'out');
      const fromRect = ghost?.getBoundingClientRect();
      showGhost(false);
      if (out) detach(tab, { at: last, fromRect });
    };
    addEventListener('pointermove', move); addEventListener('pointerup', up); addEventListener('pointercancel', up);
  });
  // atalhos (só com o foco nesta janela)
  body.addEventListener('keydown', (e) => {
    if (!e.altKey || e.ctrlKey || e.metaKey) return;
    const k = e.key.toLowerCase();
    if (k === 't') { e.preventDefault(); newTab(here()); }
    else if (k === 'w' && T.active) { e.preventDefault(); closeTab(T.active); }
    else if (/^[1-9]$/.test(k) && T.tabs[+k - 1]) { e.preventDefault(); activate(T.tabs[+k - 1]); }
  }, true);

  /* ── Começo ── */
  if (o.adopt) adopt(o.adopt);
  else {
    const saved = o.persist ? load() : null;
    if (saved?.tabs?.length && !o.opts?.search) {
      saved.tabs.slice(0, MAX).forEach((t, i) => newTab({ root: t.root, path: t.path }, i === (saved.active || 0)));
      if (o.opts?.root) newTab({ root: o.opts.root, path: o.opts.path || '' });
    } else newTab(o.opts || {});
  }
  if (o.persist) main = T;
  return () => { if (main === T) main = null; for (const t of T.tabs) { try { t.dispose?.(); } catch { /* ok */ } } T.tabs = []; };
}

// Abrir uma pasta pedida de fora (Ctrl+K, widget, Discos) com o Arquivos já aberto: numa guia nova da janela
// principal, como um navegador. Devolve false se a janela principal não está aberta.
export function openInMain(opts) {
  if (!main || !opts.root) return !!main;
  main.newTab({ root: opts.root, path: opts.path || '', search: opts.search });
  return true;
}
