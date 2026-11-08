// Gerenciador de arquivos do BrunOS (janela "Arquivos").
import { api } from './api.js';
import {
  $, $$, esc, html, clamp, fmtBytes, timeAgo, debounce, svg, folderSVG, fileSVG, extKind, KIND_LABEL,
  toast, toastError, contextMenu, closeMenu, menuOpen, dialogs, openDialog, confirmDialog, promptDialog,
} from './ui.js';

let F = null;                 // estado da janela aberta
let clipboard = null;         // { mode: 'copy'|'cut', root, paths: [] }
const uploads = [];           // fila de envios (sobrevive a fechar a janela)

const THUMB_EXT = /\.(jpe?g|png|webp|gif|avif|bmp)$/i;
const joinPath = (a, b) => (a ? `${a}/${b}` : b);
const parentOf = (p) => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '');
const baseOf = (p) => p.split('/').pop();

export function mountFiles(body, opts = {}) {
  F = {
    body, ctx: opts.ctx, root: null, path: '', entries: [], view: opts.ctx?.settings?.filesView || 'list',
    sort: { key: 'name', dir: 1 }, sel: new Set(), anchor: null, hist: [], fwd: [], showHidden: !!opts.ctx?.settings?.filesHidden,
    mode: 'browse', search: null, trash: [], loading: false,
  };
  body.innerHTML = `<div class="files">
    <aside class="f-side"><h2 class="sheet-title">Arquivos</h2><div class="f-side-list"></div></aside>
    <div class="f-main">
      <div class="f-toolbar">
        <button class="icon-btn f-menu-btn" data-f="side" aria-label="Locais" title="Locais">${svg('menu')}</button>
        <button class="icon-btn" data-f="back" aria-label="Voltar" title="Voltar (Alt+←)">${svg('chevL')}</button>
        <button class="icon-btn" data-f="fwd" aria-label="Avançar" title="Avançar (Alt+→)">${svg('chevR')}</button>
        <div class="crumbs"></div>
        <span class="f-spacer"></span>
        <label class="search-field">${svg('search')}<input data-f="q" placeholder="Procurar aqui" aria-label="Procurar"></label>
        <div class="f-actions"></div>
      </div>
      <div class="f-scroll" tabindex="-1"><div class="f-content"></div></div>
      <div class="f-status"></div>
    </div>
    <div class="f-side-shade" data-f="side-close"></div>
    <div class="f-drop">${svg('upload')}<span>Solte para enviar para esta pasta</span></div>
  </div>`;
  wire();
  (async () => {
    try { F.ctx.roots = await api.files.roots(); } catch (e) { toastError(e, 'Não foi possível listar as pastas'); }
    if (!F) return;
    const roots = F.ctx.roots;
    if (opts.search) { F.root = roots[0]?.id; F.path = ''; await go(F.root, '', false); $('[data-f="q"]', body).value = opts.search; runSearch(opts.search); return; }
    if (opts.root && roots.some((r) => r.id === opts.root)) await go(opts.root, opts.path || '', false);
    else if (roots.length) await go(roots[0].id, '', false);
    else { drawSide(); $('.f-content', body).innerHTML = `<div class="empty-note big">${svg('folder')}<b>Nenhuma pasta disponível</b>Monte pastas em <code>/files</code> no docker-compose (ex.: <code>/home:/files/home</code>).</div>`; }
  })();
  return () => { document.removeEventListener('keydown', onKey, true); document.removeEventListener('paste', onPaste); F = null; };
}

export function navigate(opts) { if (F && opts.root) go(opts.root, opts.path || ''); }

const rootById = (id) => F.ctx.roots.find((r) => r.id === id);
const rootRO = () => !!rootById(F.root)?.readOnly;

/* ── Navegação ─────────────────────────────────────────────────── */
async function go(root, path, push = true) {
  if (push && F.root && (F.root !== root || F.path !== path || F.mode !== 'browse')) { F.hist.push({ root: F.root, path: F.path, mode: F.mode }); F.fwd = []; }
  F.root = root; F.path = path; F.mode = 'browse'; F.search = null; F.sel.clear(); F.anchor = null;
  const q = $('[data-f="q"]', F.body); if (q) q.value = '';
  await reload();
}
async function reload({ keepSel = false } = {}) {
  if (!F) return;
  if (F.mode === 'trash') return openTrash(false);
  if (F.mode === 'search' && F.search) return runSearch(F.search.q);
  F.loading = true; draw();
  try {
    const r = await api.files.list(F.root, F.path);
    if (!F) return;
    F.entries = r.entries;
    if (!keepSel) F.sel.clear();
    else F.sel = new Set([...F.sel].filter((n) => F.entries.some((e) => e.name === n)));
  } catch (e) {
    toastError(e, 'Não foi possível abrir a pasta');
    if (F.path) { F.path = parentOf(F.path); return reload(); }
    F.entries = [];
  }
  F.loading = false;
  draw();
}
function back() { const h = F.hist.pop(); if (!h) return; F.fwd.push({ root: F.root, path: F.path, mode: F.mode }); restore(h); }
function forward() { const h = F.fwd.pop(); if (!h) return; F.hist.push({ root: F.root, path: F.path, mode: F.mode }); restore(h); }
function restore(h) { F.root = h.root; F.path = h.path; F.mode = h.mode === 'trash' ? 'trash' : 'browse'; F.search = null; F.sel.clear(); reload(); }
const up = () => { if (F.mode === 'browse' && F.path) go(F.root, parentOf(F.path)); };

/* ── Desenho ───────────────────────────────────────────────────── */
const isDisk = (r) => r.kind ? r.kind === 'disk' : !(r.id === 'home' || /home/i.test(r.name));
function drawSide() {
  const favs = F.ctx.favorites || [];
  const usage = (r) => r.total ? `<span class="f-mini-track"><i style="width:${clamp(((r.total - r.free) / r.total) * 100, 0, 100)}%"></i></span>` : '';
  const link = (r) => `<button class="f-link ${F.mode === 'browse' && F.root === r.id && !F.path ? 'on' : ''}" data-root="${esc(r.id)}" data-drop-root="${esc(r.id)}" data-drop-path="">
      ${svg(isDisk(r) ? 'disk' : 'home')}<span class="f-link-txt"><span>${esc(r.name)}${r.readOnly ? ' <small class="ro">leitura</small>' : ''}</span>${usage(r)}</span></button>`;
  const places = F.ctx.roots.filter((r) => !isDisk(r)), disks = F.ctx.roots.filter(isDisk);
  const open = F.ctx.settings?.filesDisksOpen !== false;
  $('.f-side-list', F.body).innerHTML = `
    <div class="f-sec">Locais</div>
    ${places.map(link).join('')}
    <div class="f-disks-head ${open ? 'open' : ''}">
      <button class="f-link f-disks-label" data-f="disks-menu" aria-haspopup="menu" title="Discos de /mnt">${svg('disks')}<span class="f-link-txt"><span>Discos</span></span></button>
      <button class="f-caret" data-f="disks-toggle" aria-expanded="${open}" aria-label="${open ? 'Recolher' : 'Mostrar'} discos">${svg('caret')}</button>
    </div>
    <div class="f-disks" ${open ? '' : 'hidden'}>
      ${disks.map(link).join('') || '<button class="f-disks-empty" data-f="map-disks">' + svg('plus') + 'Mapear discos</button>'}
    </div>
    ${favs.length ? `<div class="f-sec">Favoritos</div>${favs.map((f, i) => `<button class="f-link ${F.mode === 'browse' && F.root === f.root && F.path === f.path && f.path ? 'on' : ''}" data-fav="${i}" data-drop-root="${esc(f.root)}" data-drop-path="${esc(f.path)}">${folderSVG()}<span class="f-link-txt"><span>${esc(f.name)}</span></span></button>`).join('')}` : ''}
    <div class="f-sec">Outros</div>
    <button class="f-link ${F.mode === 'trash' ? 'on' : ''}" data-f="trash">${svg('trash')}<span class="f-link-txt"><span>Lixeira</span></span></button>`;
}

/* ── Discos mapeados (pastas de /mnt e /media escolhidas pelo usuário) ── */
export async function mapDisksDialog(ctx, onDone) {
  let disks;
  try { disks = await api.files.disks(); } catch (e) { toastError(e, 'Não foi possível listar os discos'); return; }
  const row = (d) => `<label class="map-row ${d.mounted ? '' : 'unmounted'}">
      <input type="checkbox" name="d" value="${esc(d.name)}" ${d.mapped ? 'checked' : ''}>
      <span class="map-check">${svg('check')}</span>
      <span class="map-ico">${svg('disk')}</span>
      <span class="map-txt"><b>${esc(d.name)}</b><small>${d.mounted ? `${fmtBytes(d.total - d.free)} usados de ${fmtBytes(d.total)}` : 'Pasta vazia: nenhum disco montado aqui'}</small></span>
      ${d.mounted ? `<span class="map-track"><i style="width:${d.total ? clamp(((d.total - d.free) / d.total) * 100, 0, 100) : 0}%"></i></span>` : '<span class="map-tag">sem disco</span>'}
    </label>`;
  const d = openDialog(`<form class="map-form">
      <h2>Mapear discos</h2>
      <p>Escolha quais pastas de <code>/mnt</code> aparecem no gerenciador. Discos não mapeados ficam fora do alcance do BrunOS. A escolha fica salva no servidor.</p>
      <div class="map-list">${disks.length ? disks.map(row).join('') : '<div class="empty-note">Nenhuma pasta encontrada em <code>/mnt</code> ou <code>/media</code>.</div>'}</div>
      <div class="dlg-actions">${disks.some((x) => x.mounted) ? '<button type="button" class="pill-btn" data-c="mounted" style="margin-right:auto">Só os montados</button>' : ''}<button type="button" class="pill-btn" data-c="no">Cancelar</button><button type="submit" class="pill-btn primary">Salvar</button></div>
    </form>`, { closeBtn: false, width: 520 });
  const f = $('form', d.el);
  $('[data-c="no"]', d.el).onclick = () => d.close();
  const mounted = $('[data-c="mounted"]', d.el);
  if (mounted) mounted.onclick = () => $$('input[name="d"]', f).forEach((i) => { i.checked = disks.find((x) => x.name === i.value)?.mounted; });
  f.onsubmit = async (e) => {
    e.preventDefault();
    const mapped = $$('input[name="d"]:checked', f).map((i) => i.value);
    try {
      ctx.roots = await api.files.setDisks(mapped);
      d.close();
      toast('Discos atualizados', mapped.length ? `${mapped.length} disco(s) no gerenciador.` : 'Nenhum disco mapeado.');
      onDone?.();
    } catch (err) { toastError(err, 'Não deu para salvar'); }
  };
}
function disksMenu(el) {
  const r = el.getBoundingClientRect();
  contextMenu(r.left + 10, r.bottom + 4, [
    { icon: 'plus', label: 'Mapear', run: () => mapDisksDialog(F.ctx, afterMap) },
  ]);
}
function afterMap() {
  if (!F) return;
  F.ctx.settings && F.ctx.saveSetting('filesDisksOpen', true);
  if (F.mode === 'browse' && !rootById(F.root)) go(F.ctx.roots[0]?.id, '', false);
  else drawSide();
}

function drawToolbar() {
  $('[data-f="back"]', F.body).disabled = !F.hist.length;
  $('[data-f="fwd"]', F.body).disabled = !F.fwd.length;
  const r = rootById(F.root);
  const crumbs = $('.crumbs', F.body);
  if (F.mode === 'trash') crumbs.innerHTML = `<button disabled>${svg('trash')} Lixeira</button>`;
  else if (F.mode === 'search') crumbs.innerHTML = `<button data-crumb="${esc(F.path)}">${esc(r?.name || '')}</button><span class="sep">${svg('chevR')}</span><button disabled>Resultados para “${esc(F.search?.q || '')}”</button>`;
  else {
    const parts = F.path ? F.path.split('/') : [];
    crumbs.innerHTML = [`<button data-crumb="" data-drop-root="${esc(F.root)}" data-drop-path="">${esc(r?.name || '')}</button>`,
      ...parts.map((p, i) => { const path = parts.slice(0, i + 1).join('/'); return `<span class="sep">${svg('chevR')}</span><button data-crumb="${esc(path)}" data-drop-root="${esc(F.root)}" data-drop-path="${esc(path)}">${esc(p)}</button>`; })].join('');
    crumbs.scrollLeft = crumbs.scrollWidth;
  }
  const ro = rootRO();
  $('.f-actions', F.body).innerHTML = F.mode === 'trash'
    ? `<button class="pill-btn danger" data-f="empty-trash" ${F.trash.length ? '' : 'disabled'}>${svg('trash')}Esvaziar lixeira</button>`
    : `<button class="pill-btn" data-f="mkdir" ${ro || F.mode !== 'browse' ? 'disabled' : ''} title="Nova pasta">${svg('folderPlus')}<span class="hide-sm">Pasta</span></button>
       <button class="pill-btn" data-f="upload" ${ro || F.mode !== 'browse' ? 'disabled' : ''} title="Enviar arquivos">${svg('upload')}<span class="hide-sm">Enviar</span></button>
       <button class="icon-btn" data-f="tools" ${F.mode !== 'browse' ? 'disabled' : ''} title="Mais: novo arquivo, mapa do espaço, duplicados" aria-label="Mais ferramentas">${svg('more')}</button>
       <button class="icon-btn ${F.showHidden ? 'on' : ''}" data-f="hidden" title="${F.showHidden ? 'Esconder' : 'Mostrar'} arquivos ocultos" aria-label="Arquivos ocultos">${svg(F.showHidden ? 'eye' : 'eyeOff')}</button>
       <div class="view-toggle"><button class="icon-btn ${F.view === 'list' ? 'on' : ''}" data-view="list" aria-label="Lista">${svg('list')}</button><button class="icon-btn ${F.view === 'grid' ? 'on' : ''}" data-view="grid" aria-label="Grade">${svg('grid')}</button></div>`;
}
function visibleEntries() {
  let list = F.mode === 'search' ? (F.search?.hits || []) : F.entries;
  if (!F.showHidden) list = list.filter((e) => !e.hidden);
  const { key, dir } = F.sort;
  const kind = (e) => (e.dir ? '' : KIND_LABEL[extKind(e.name)]);
  return [...list].sort((a, b) => {
    if (a.dir !== b.dir) return a.dir ? -1 : 1;
    let c = 0;
    if (key === 'size') c = (a.size || 0) - (b.size || 0);
    else if (key === 'mtime') c = (a.mtime || 0) - (b.mtime || 0);
    else if (key === 'kind') c = kind(a).localeCompare(kind(b), 'pt-BR');
    if (!c) c = a.name.localeCompare(b.name, 'pt-BR', { numeric: true, sensitivity: 'base' });
    return c * dir;
  });
}
const keyOf = (e) => (F.mode === 'search' ? e.path : e.name);
const pathOf = (e) => (F.mode === 'search' ? e.path : joinPath(F.path, e.name));
function iconFor(e, big = false) {
  if (e.dir) return folderSVG(big ? 'folder-ico big' : 'folder-ico');
  if (big && THUMB_EXT.test(e.name) && e.size < 20e6 && !api.demo) return `<span class="thumb"><img src="${esc(api.files.rawUrl(F.root, pathOf(e)))}" alt="" loading="lazy" decoding="async" onerror="this.parentElement.classList.add('err')"></span>`;
  return fileSVG(e.name, big ? 'file-ico big' : 'file-ico');
}
function draw() {
  if (!F) return;
  drawSide(); drawToolbar();
  const box = $('.f-content', F.body);
  if (F.loading && !F.entries.length) { box.innerHTML = '<div class="f-loading"><i></i><i></i><i></i></div>'; return; }
  if (F.mode === 'trash') return drawTrash(box);
  const items = visibleEntries();
  const hiddenCount = (F.mode === 'search' ? F.search?.hits || [] : F.entries).filter((e) => e.hidden).length;
  if (!items.length) {
    box.innerHTML = `<div class="empty-note big">${svg(F.mode === 'search' ? 'search' : 'folder')}<b>${F.mode === 'search' ? 'Nada encontrado' : 'Pasta vazia'}</b>${F.mode === 'search' ? 'Tente outro termo.' : rootRO() ? '' : 'Arraste arquivos para cá para enviar.'}${hiddenCount && !F.showHidden ? `<br><button class="pill-btn" data-f="hidden">Mostrar ${hiddenCount} oculto(s)</button>` : ''}</div>`;
  } else if (F.view === 'list') {
    const th = (k, l) => `<th data-sort="${k}" class="${F.sort.key === k ? 'sorted' : ''}">${l}${F.sort.key === k ? `<span class="sort-dir">${svg(F.sort.dir > 0 ? 'chevU' : 'chevD')}</span>` : ''}</th>`;
    box.innerHTML = `<table class="f-table"><thead><tr>${th('name', 'Nome')}${F.mode === 'search' ? '<th>Em</th>' : ''}${th('mtime', 'Modificado')}${th('size', 'Tamanho')}${th('kind', 'Tipo')}</tr></thead><tbody>
      ${items.map((e) => `<tr class="item ${F.sel.has(keyOf(e)) ? 'sel' : ''} ${e.hidden ? 'is-hidden' : ''} ${clipboard?.mode === 'cut' && clipboard.root === F.root && clipboard.paths.includes(pathOf(e)) ? 'is-cut' : ''}" data-key="${esc(keyOf(e))}" draggable="true" ${e.dir ? `data-drop-root="${esc(F.root)}" data-drop-path="${esc(pathOf(e))}"` : ''}>
        <td><span class="f-name">${iconFor(e)}<span class="f-name-txt">${esc(e.name)}</span>${e.link ? `<small class="f-tag">link</small>` : ''}</span></td>
        ${F.mode === 'search' ? `<td class="f-muted f-ellipsis">${esc(parentOf(e.path) || '/')}</td>` : ''}
        <td class="f-muted" title="${e.mtime ? new Date(e.mtime * 1000).toLocaleString('pt-BR') : ''}">${timeAgo(e.mtime)}</td>
        <td class="f-muted">${e.dir ? '—' : fmtBytes(e.size)}</td>
        <td class="f-muted">${e.dir ? 'Pasta' : KIND_LABEL[extKind(e.name)]}</td></tr>`).join('')}
    </tbody></table>`;
  } else {
    box.innerHTML = `<div class="f-grid">${items.map((e) => `<button class="f-tile ${F.sel.has(keyOf(e)) ? 'sel' : ''} ${e.hidden ? 'is-hidden' : ''}" data-key="${esc(keyOf(e))}" draggable="true" ${e.dir ? `data-drop-root="${esc(F.root)}" data-drop-path="${esc(pathOf(e))}"` : ''} title="${esc(e.name)}">${iconFor(e, true)}<span>${esc(e.name)}</span></button>`).join('')}</div>`;
  }
  drawStatus(items);
}
function drawStatus(items) {
  const r = rootById(F.root);
  const sel = items.filter((e) => F.sel.has(keyOf(e)));
  const selSize = sel.reduce((s, e) => s + (e.size || 0), 0);
  const parts = [`${items.length} ${items.length === 1 ? 'item' : 'itens'}`];
  if (sel.length) parts.push(`${sel.length} selecionado${sel.length > 1 ? 's' : ''}${selSize ? ` (${fmtBytes(selSize)})` : ''}`);
  if (F.search?.truncated) parts.push('busca limitada aos primeiros resultados');
  $('.f-status', F.body).innerHTML = `<span>${parts.join(' · ')}</span>${r?.total ? `<span>${fmtBytes(r.free)} livres em ${esc(r.name)}</span>` : ''}${clipboard ? `<span class="clip-note">${svg(clipboard.mode === 'cut' ? 'cut' : 'copy')} ${clipboard.paths.length} na área de transferência · Ctrl+V para colar</span>` : ''}`;
}

/* ── Lixeira ───────────────────────────────────────────────────── */
async function openTrash(push = true) {
  if (push && F.mode !== 'trash') { F.hist.push({ root: F.root, path: F.path, mode: F.mode }); F.fwd = []; }
  F.mode = 'trash'; F.sel.clear();
  try { F.trash = await api.files.trashList(); } catch (e) { toastError(e); F.trash = []; }
  draw();
}
function drawTrash(box) {
  if (!F.trash.length) { box.innerHTML = `<div class="empty-note big">${svg('trash')}<b>A lixeira está vazia</b>Itens excluídos ficam aqui até você esvaziar.</div>`; $('.f-status', F.body).innerHTML = ''; return; }
  const rn = (id) => rootById(id)?.name || id;
  box.innerHTML = `<table class="f-table"><thead><tr><th>Nome</th><th>Estava em</th><th>Excluído</th><th>Tamanho</th><th></th></tr></thead><tbody>
    ${F.trash.map((t) => `<tr class="item" data-trash="${esc(t.root)}|${esc(t.id)}"><td><span class="f-name">${t.dir ? folderSVG() : fileSVG(t.name)}<span class="f-name-txt">${esc(t.name)}</span></span></td>
      <td class="f-muted f-ellipsis">${esc(rn(t.root))}/${esc(parentOf(t.path))}</td><td class="f-muted">${timeAgo(new Date(t.deletedAt).getTime() / 1000)}</td><td class="f-muted">${t.dir ? '—' : fmtBytes(t.size)}</td>
      <td class="f-row-actions"><button class="pill-btn" data-t="restore">${svg('restore')}Restaurar</button><button class="icon-btn" data-t="delete" title="Excluir para sempre">${svg('trash')}</button></td></tr>`).join('')}
  </tbody></table>`;
  $('.f-status', F.body).innerHTML = `<span>${F.trash.length} ${F.trash.length === 1 ? 'item' : 'itens'} na lixeira</span>`;
}

/* ── Busca ─────────────────────────────────────────────────────── */
async function runSearch(qq) {
  qq = qq.trim();
  if (!qq) { if (F.mode === 'search') { F.mode = 'browse'; F.search = null; reload(); } return; }
  if (F.mode === 'browse') { F.hist.push({ root: F.root, path: F.path, mode: 'browse' }); F.fwd = []; }
  F.mode = 'search'; F.sel.clear();
  F.search = { q: qq, hits: [], truncated: false };
  $('.f-content', F.body).innerHTML = '<div class="f-loading"><i></i><i></i><i></i></div>';
  try {
    const r = await api.files.search(F.root, F.path, qq);
    if (!F || F.search?.q !== qq) return;
    F.search.hits = r.hits; F.search.truncated = r.truncated;
  } catch (e) { toastError(e, 'Falha na busca'); }
  draw();
}

/* ── Seleção e ações ───────────────────────────────────────────── */
const selectedEntries = () => visibleEntries().filter((e) => F.sel.has(keyOf(e)));
const selectedPaths = () => selectedEntries().map(pathOf);
function selectClick(key, e) {
  const items = visibleEntries().map(keyOf);
  if (e.shiftKey && F.anchor) {
    const a = items.indexOf(F.anchor), b = items.indexOf(key);
    if (!(e.ctrlKey || e.metaKey)) F.sel.clear();
    items.slice(Math.min(a, b), Math.max(a, b) + 1).forEach((k) => F.sel.add(k));
  } else if (e.ctrlKey || e.metaKey) {
    F.sel.has(key) ? F.sel.delete(key) : F.sel.add(key);
    F.anchor = key;
  } else { F.sel = new Set([key]); F.anchor = key; }
  $$('[data-key]', F.body).forEach((el) => el.classList.toggle('sel', F.sel.has(el.dataset.key)));
  drawStatus(visibleEntries());
}
function openEntry(e) {
  if (e.dir) go(F.root, pathOf(e));
  else preview(e);
}
function download(entries) {
  if (api.demo) { toast('Download indisponível na demonstração'); return; }
  const a = document.createElement('a');
  if (entries.length === 1 && !entries[0].dir) a.href = api.files.rawUrl(F.root, pathOf(entries[0]), true);
  else a.href = api.files.zipUrl(F.root, entries.map(pathOf));
  a.click();
}
async function mkdir() {
  const name = await promptDialog('Nova pasta', { label: 'Nome', value: 'Nova pasta', okLabel: 'Criar' });
  if (!name) return;
  try { await api.files.mkdir(F.root, F.path, name); await reload(); F.sel = new Set([name]); draw(); }
  catch (e) { toastError(e, 'Não foi possível criar a pasta'); }
}
async function rename(e) {
  const name = await promptDialog('Renomear', { label: 'Novo nome', value: e.name, okLabel: 'Renomear' });
  if (!name || name === e.name) return;
  try { await api.files.rename(F.root, pathOf(e), name); await reload(); F.sel = new Set([F.mode === 'search' ? joinPath(parentOf(e.path), name) : name]); draw(); }
  catch (x) { toastError(x, 'Não foi possível renomear'); }
}
async function trash(entries, permanent = false) {
  if (!entries.length) return;
  const label = entries.length === 1 ? `“${entries[0].name}”` : `${entries.length} itens`;
  if (permanent && !(await confirmDialog(`Excluir ${label} para sempre?`, 'Isso não pode ser desfeito.', 'Excluir', { danger: true }))) return;
  try {
    if (permanent) await api.files.del(F.root, entries.map(pathOf));
    else await api.files.trash(F.root, entries.map(pathOf));
    toast(permanent ? 'Excluído' : `${label} na lixeira`, permanent ? '' : 'Dá para restaurar pela Lixeira.', permanent ? null : { label: 'Abrir lixeira', run: () => F && openTrash() });
    reload();
  } catch (e) {
    if (!permanent && /outro disco/.test(e.message)) { if (await confirmDialog('Não dá para mandar para a lixeira', `${e.message}.`, 'Excluir para sempre', { danger: true })) trash(entries, true); }
    else toastError(e, 'Não foi possível excluir');
  }
}
function setClipboard(mode) {
  const paths = selectedPaths(); if (!paths.length) return;
  clipboard = { mode, root: F.root, paths };
  toast(mode === 'cut' ? 'Recortado' : 'Copiado', `${paths.length} item(ns). Abra a pasta de destino e cole (Ctrl+V).`);
  draw();
}
async function paste(destRoot = F.root, destPath = F.path) {
  if (!clipboard) return;
  if (rootById(destRoot)?.readOnly) { toastError(new Error('esta pasta é somente leitura')); return; }
  const c = clipboard;
  try {
    toast(c.mode === 'cut' ? 'Movendo…' : 'Copiando…', `${c.paths.length} item(ns)`);
    await api.files.paste(c.root, c.paths, destRoot, destPath, c.mode === 'copy');
    if (c.mode === 'cut') clipboard = null;
    toast('Pronto', c.mode === 'cut' ? 'Itens movidos.' : 'Itens copiados.');
    reload();
  } catch (e) { toastError(e, 'Não foi possível colar'); }
}
async function moveTo(paths, srcRoot, destRoot, destPath, copy) {
  if (!paths.length || (srcRoot === destRoot && paths.every((p) => parentOf(p) === destPath) && !copy)) return;
  try { await api.files.paste(srcRoot, paths, destRoot, destPath, copy); toast(copy ? 'Copiado' : 'Movido', `${paths.length} item(ns) para ${baseOf(destPath) || rootById(destRoot)?.name}`); reload(); }
  catch (e) { toastError(e, copy ? 'Não foi possível copiar' : 'Não foi possível mover'); }
}
function toggleFavorite(e) {
  const favs = [...(F.ctx.favorites || [])];
  const path = e._path ?? pathOf(e);
  const i = favs.findIndex((f) => f.root === F.root && f.path === path);
  if (i >= 0) favs.splice(i, 1); else favs.push({ root: F.root, path, name: e.name });
  F.ctx.setFavorites(favs); drawSide();
  toast(i >= 0 ? 'Removido dos favoritos' : 'Adicionado aos favoritos', i >= 0 ? '' : 'Aparece na barra lateral e no widget Arquivos.');
}
async function useAsWallpaper(e) {
  try {
    const res = await fetch(api.files.rawUrl(F.root, pathOf(e)), { credentials: 'same-origin' });
    const blob = await res.blob();
    await F.ctx.setWallpaperFromFile(new File([blob], e.name, { type: blob.type || 'image/jpeg' }));
  } catch (x) { toastError(x); }
}

/* ── Editor, extrair e ferramentas (0.6.0) ─────────────────────── */
const ARCHIVE = /\.(zip|tar|tgz|tar\.gz)$/i;
const EDITABLE_NAME = /\.(txt|md|markdown|log|json|webmanifest|ya?ml|toml|ini|conf|cfg|properties|env|csv|tsv|xml|html?|svg|js|mjs|cjs|ts|css|py|go|rs|sh|bash|zsh|c|h|cpp|java|rb|php|sql|r|m|tex|bib|srt|vtt|nfo|diff|patch|nginx|dockerfile)$|^(dockerfile|makefile|\.env(\..+)?|\.gitignore|\.bashrc|\.profile)$/i;
const canEdit = (e) => e && !e.dir && EDITABLE_NAME.test(e.name);
async function editFile(path, name) {
  const root = F.root;
  try {
    const m = await import('./editor.js');
    m.openEditor({ root, path, name, onSaved: () => { if (F && F.root === root) reload({ keepSel: true }); } });
  } catch (x) { toastError(x, 'Não foi possível abrir o editor'); }
}
async function newTextFile() {
  const name = await promptDialog('Novo arquivo de texto', { label: 'Nome', value: 'novo.txt', okLabel: 'Criar' });
  if (!name) return;
  try {
    const r = await api.files.newFile(F.root, F.path, name);
    await reload(); F.sel = new Set([name]); draw();
    editFile(r.path, name);
  } catch (x) { toastError(x, 'Não foi possível criar o arquivo'); }
}
async function extractArchive(e) {
  const root = F.root;
  toast('Extraindo…', e.name);
  try {
    const r = await api.files.extract(root, pathOf(e));
    toast('Extraído', `Pasta “${baseOf(r.path)}”`, { label: 'Abrir', run: () => F && go(root, r.path) });
    if (F && F.root === root) reload({ keepSel: true });
  } catch (x) { toastError(x, 'Não foi possível extrair'); }
}
function toolsItems() {
  const ro = rootRO(), root = F.root, path = F.path, rootName = rootById(root)?.name || '';
  return [
    { icon: 'filePlus', label: 'Novo arquivo de texto…', hidden: ro, run: newTextFile },
    { icon: 'treemap', label: 'Mapa do espaço desta pasta', run: () => import('./filetools.js').then((m) => m.spaceDialog({ root, path, rootName, onOpen: (p) => F && go(root, p) })) },
    { icon: 'dupes', label: 'Encontrar arquivos duplicados', run: () => import('./filetools.js').then((m) => m.duplicatesDialog({ root, path, rootName, readOnly: ro, onChanged: () => F && F.root === root && reload({ keepSel: true }) })) },
  ];
}

function itemMenu(e, x, y) {
  if (!F.sel.has(keyOf(e))) { F.sel = new Set([keyOf(e)]); F.anchor = keyOf(e); draw(); }
  const sel = selectedEntries(), one = sel.length === 1 ? sel[0] : null, ro = rootRO();
  const isFav = one?.dir && (F.ctx.favorites || []).some((f) => f.root === F.root && f.path === pathOf(one));
  contextMenu(x, y, [
    { icon: one?.dir ? 'folder' : 'eye', label: one?.dir ? 'Abrir' : 'Visualizar', hidden: !one, run: () => openEntry(one), kbd: 'Enter' },
    { icon: 'edit', label: ro ? 'Abrir no editor (só leitura)' : 'Editar', hidden: !canEdit(one), run: () => editFile(pathOf(one), one.name) },
    { icon: 'unzip', label: 'Extrair aqui', hidden: !(one && !one.dir && ARCHIVE.test(one.name)) || ro, run: () => extractArchive(one) },
    { icon: 'folder', label: 'Mostrar na pasta', hidden: !(one && F.mode === 'search'), run: () => go(F.root, parentOf(one.path)).then(() => { F.sel = new Set([one.name]); draw(); }) },
    { icon: 'download', label: sel.length > 1 || one?.dir ? 'Baixar como .zip' : 'Baixar', run: () => download(sel) },
    { icon: 'image', label: 'Usar como papel de parede', hidden: !(one && THUMB_EXT.test(one.name)) || api.demo, run: () => useAsWallpaper(one) },
    { icon: 'star', label: isFav ? 'Remover dos favoritos' : 'Adicionar aos favoritos', hidden: !(one?.dir), run: () => toggleFavorite(one) },
    '-',
    { icon: 'edit', label: 'Renomear', hidden: !one || ro, run: () => rename(one), kbd: 'F2' },
    { icon: 'copy', label: 'Copiar', run: () => setClipboard('copy'), kbd: 'Ctrl+C' },
    { icon: 'cut', label: 'Recortar', hidden: ro, run: () => setClipboard('cut'), kbd: 'Ctrl+X' },
    { icon: 'paste', label: 'Colar dentro', hidden: !(clipboard && one?.dir) || ro, run: () => paste(F.root, pathOf(one)) },
    '-',
    { icon: 'trash', label: 'Mover para a lixeira', hidden: ro, run: () => trash(sel), kbd: 'Del' },
    { icon: 'x', label: 'Excluir para sempre', hidden: ro, danger: true, run: () => trash(sel, true), kbd: 'Shift+Del' },
  ]);
}
function blankMenu(x, y) {
  const ro = rootRO() || F.mode !== 'browse';
  contextMenu(x, y, [
    { icon: 'folderPlus', label: 'Nova pasta', hidden: ro, run: mkdir },
    { icon: 'filePlus', label: 'Novo arquivo de texto…', hidden: ro, run: newTextFile },
    { icon: 'upload', label: 'Enviar arquivos…', hidden: ro, run: () => pickUpload(false) },
    { icon: 'folder', label: 'Enviar pasta…', hidden: ro, run: () => pickUpload(true) },
    { icon: 'paste', label: `Colar${clipboard ? ` (${clipboard.paths.length})` : ''}`, disabled: !clipboard, hidden: ro, run: () => paste() },
    '-',
    { icon: 'download', label: 'Baixar esta pasta (.zip)', hidden: F.mode !== 'browse', run: () => { const a = document.createElement('a'); a.href = api.files.zipUrl(F.root, [F.path || '.']); a.click(); } },
    { icon: 'star', label: 'Adicionar esta pasta aos favoritos', hidden: !F.path || F.mode !== 'browse', run: () => toggleFavorite({ name: baseOf(F.path), dir: true, _path: F.path }) },
    { icon: 'treemap', label: 'Mapa do espaço desta pasta', hidden: F.mode !== 'browse', run: toolsItems()[1].run },
    { icon: 'dupes', label: 'Encontrar arquivos duplicados', hidden: F.mode !== 'browse', run: toolsItems()[2].run },
    { icon: F.showHidden ? 'eyeOff' : 'eye', label: F.showHidden ? 'Esconder ocultos' : 'Mostrar ocultos', run: toggleHidden },
    { icon: 'refresh', label: 'Atualizar', run: () => reload({ keepSel: true }) },
  ]);
}
function toggleHidden() { F.showHidden = !F.showHidden; F.ctx.saveSetting('filesHidden', F.showHidden); draw(); }

/* ── Pré-visualização ──────────────────────────────────────────── */
// O navegador não toca todo formato (ex.: .mkv com áudio AC3/DTS, .mov HEVC): em vez de uma tela
// preta, explica e oferece o download.
function mediaFallback(el, f) {
  el.addEventListener('error', () => {
    const stage = el.closest('.pv-stage'); if (!stage) return;
    stage.innerHTML = `<div class="pv-none">${fileSVG(f.name, 'file-ico big')}<p>O navegador não consegue tocar este arquivo (formato ou codec não suportado).<br>Baixe para assistir num player como o VLC.</p><button class="pill-btn" data-pv="dl">${svg('download')}Baixar</button></div>`;
  }, { once: true });
}
function preview(e) {
  const files = visibleEntries().filter((x) => !x.dir);
  let idx = Math.max(0, files.findIndex((x) => keyOf(x) === keyOf(e)));
  const d = openDialog('<div class="pv"><div class="pv-head"><b class="pv-name"></b><span class="pv-meta"></span><span class="f-spacer"></span><button class="pill-btn" data-pv="edit" hidden>' + svg('edit') + 'Editar</button><button class="pill-btn" data-pv="dl">' + svg('download') + 'Baixar</button></div><div class="pv-stage"></div><button class="pv-nav prev" data-pv="prev" aria-label="Anterior">' + svg('chevL') + '</button><button class="pv-nav next" data-pv="next" aria-label="Próximo">' + svg('chevR') + '</button></div>', { cls: 'pv-dialog', width: 1100 });
  const show = async () => {
    const f = files[idx], stage = $('.pv-stage', d.el), url = api.files.rawUrl(F.root, pathOf(f));
    $('.pv-name', d.el).textContent = f.name;
    $('.pv-meta', d.el).textContent = `${fmtBytes(f.size)} · ${timeAgo(f.mtime)} · ${idx + 1} de ${files.length}`;
    $('[data-pv="prev"]', d.el).hidden = idx === 0; $('[data-pv="next"]', d.el).hidden = idx === files.length - 1;
    $('[data-pv="edit"]', d.el).hidden = !canEdit(f) || f.size > 5 * 1024 * 1024;
    const k = extKind(f.name), ext = f.name.split('.').pop().toLowerCase();
    if (api.demo) { stage.innerHTML = `<div class="pv-none">${fileSVG(f.name, 'file-ico big')}<p>A pré-visualização funciona com o servidor rodando.</p></div>`; return; }
    if (k === 'img' && ext !== 'heic' && ext !== 'tiff' && ext !== 'tif') stage.innerHTML = `<img src="${esc(url)}" alt="">`;
    else if (k === 'vid' && /^(mp4|webm|mov|m4v|ogv|mkv)$/.test(ext)) {
      stage.innerHTML = `<video src="${esc(url)}" controls autoplay playsinline preload="metadata"></video>`;
      mediaFallback($('video', stage), f);
    } else if (k === 'aud') {
      stage.innerHTML = `<div class="pv-audio">${fileSVG(f.name, 'file-ico big')}<audio src="${esc(url)}" controls autoplay preload="metadata"></audio></div>`;
      mediaFallback($('audio', stage), f);
    }
    else if (k === 'pdf') stage.innerHTML = `<iframe src="${esc(url)}" title="${esc(f.name)}"></iframe>`;
    else if (k === 'code' || k === 'txt' || ext === 'csv' || ext === 'tsv') {
      if (f.size > 0 && !/^(txt|md|log|json|ya?ml|toml|ini|conf|cfg|csv|tsv|xml|html?|svg|js|ts|css|py|go|rs|sh|env|c|h|cpp|java|rb|php|sql|r|m|tex|bib|srt|vtt|nfo|properties)$/.test(ext)) { stage.innerHTML = `<div class="pv-none">${fileSVG(f.name, 'file-ico big')}<p>Sem pré-visualização para este tipo de arquivo.</p></div>`; return; }
      stage.innerHTML = '<div class="f-loading"><i></i><i></i><i></i></div>';
      try {
        const res = await fetch(url, { headers: { Range: 'bytes=0-524287' }, credentials: 'same-origin' });
        const text = await res.text();
        stage.innerHTML = `<pre class="pv-text"></pre>${f.size > 524288 ? '<p class="pv-trunc">Mostrando só o começo do arquivo (512 KB).</p>' : ''}`;
        $('.pv-text', stage).textContent = text;
      } catch { stage.innerHTML = '<div class="pv-none"><p>Não foi possível ler o arquivo.</p></div>'; }
    } else stage.innerHTML = `<div class="pv-none">${fileSVG(f.name, 'file-ico big')}<p>Sem pré-visualização para este tipo de arquivo.</p></div>`;
  };
  d.el.addEventListener('click', (ev) => {
    const b = ev.target.closest('[data-pv]'); if (!b) return;
    if (b.dataset.pv === 'prev' && idx > 0) { idx--; show(); }
    if (b.dataset.pv === 'next' && idx < files.length - 1) { idx++; show(); }
    if (b.dataset.pv === 'dl') download([files[idx]]);
    if (b.dataset.pv === 'edit') { const f = files[idx]; d.close(); editFile(pathOf(f), f.name); }
  });
  d.el._keys = (ev) => {
    if (ev.key === 'ArrowLeft' && idx > 0) { idx--; show(); ev.preventDefault(); }
    if (ev.key === 'ArrowRight' && idx < files.length - 1) { idx++; show(); ev.preventDefault(); }
  };
  show();
}

/* ── Envio de arquivos ─────────────────────────────────────────── */
function pickUpload(folder) {
  const input = document.createElement('input');
  input.type = 'file'; input.multiple = true;
  if (folder) input.webkitdirectory = true;
  input.onchange = () => enqueue([...input.files].map((f) => ({ file: f, rel: folder ? f.webkitRelativePath || f.name : f.name })), F.root, F.path);
  input.click();
}
async function filesFromDrop(dt) {
  const out = [];
  const items = [...(dt.items || [])].map((i) => i.webkitGetAsEntry?.()).filter(Boolean);
  if (!items.length) return [...dt.files].map((f) => ({ file: f, rel: f.name }));
  const readAll = (reader) => new Promise((res) => { const all = []; const next = () => reader.readEntries((ents) => { if (!ents.length) res(all); else { all.push(...ents); next(); } }, () => res(all)); next(); });
  const walk = async (entry, prefix) => {
    if (entry.isFile) await new Promise((res) => entry.file((f) => { out.push({ file: f, rel: prefix + f.name }); res(); }, res));
    else if (entry.isDirectory) for (const ch of await readAll(entry.createReader())) await walk(ch, `${prefix}${entry.name}/`);
  };
  for (const it of items) await walk(it, '');
  return out;
}
function enqueue(list, root, path) {
  if (!list.length) return;
  for (const { file, rel } of list) uploads.push({ file, rel, root, path, progress: 0, state: 'wait', ctl: null, err: '' });
  drawUploads(); pump();
}
let active = 0;
function pump() {
  while (active < 2) {
    const u = uploads.find((x) => x.state === 'wait');
    if (!u) break;
    active++; u.state = 'up';
    const ctl = api.files.upload(u.root, u.path, u.rel, u.file, (p) => { u.progress = p; drawUploads(); });
    u.ctl = ctl;
    ctl.promise.then(() => { u.state = 'done'; u.progress = 1; })
      .catch((e) => { u.state = e.message === 'Cancelado' ? 'cancel' : 'error'; u.err = e.message; })
      .finally(() => {
        active--; drawUploads(); pump();
        if (F && F.root === u.root && F.path === u.path && F.mode === 'browse' && !uploads.some((x) => x.state === 'up' || x.state === 'wait')) reload({ keepSel: true });
      });
  }
}
let autoHide;
const drawUploads = debounce(() => {
  const host = $('#upload-host');
  if (!uploads.length) { host.innerHTML = ''; return; }
  const done = uploads.filter((u) => u.state === 'done').length, running = uploads.some((u) => u.state === 'up' || u.state === 'wait');
  clearTimeout(autoHide);
  // tudo enviado sem erro: o painel some sozinho depois de alguns segundos
  if (!running && !uploads.some((u) => u.state === 'error')) autoHide = setTimeout(() => { uploads.length = 0; drawUploads(); }, 5000);
  const total = uploads.reduce((s, u) => s + u.file.size, 0), sent = uploads.reduce((s, u) => s + u.file.size * u.progress, 0);
  host.innerHTML = `<div class="up-panel">
    <div class="up-head"><b>${running ? `Enviando ${done + 1} de ${uploads.length}` : `${done} de ${uploads.length} enviados`}</b><small>${fmtBytes(sent)} / ${fmtBytes(total)}</small><span class="f-spacer"></span>
    ${running ? '' : `<button class="icon-btn" data-up="clear" aria-label="Fechar">${svg('x')}</button>`}</div>
    <div class="track"><i style="width:${total ? (sent / total) * 100 : 0}%"></i></div>
    <div class="up-list">${uploads.slice(-50).map((u, i) => `<div class="up-row ${u.state}">${fileSVG(u.file.name)}<span class="nm" title="${esc(u.rel)}">${esc(u.rel)}</span>
      <span class="st">${u.state === 'done' ? svg('check') : u.state === 'error' ? `<span title="${esc(u.err)}">erro</span>` : u.state === 'cancel' ? 'cancelado' : u.state === 'up' ? `${Math.round(u.progress * 100)}%` : 'na fila'}</span>
      ${u.state === 'up' || u.state === 'wait' ? `<button class="icon-btn" data-up-cancel="${uploads.length - Math.min(50, uploads.length) + i}" aria-label="Cancelar">${svg('x')}</button>` : ''}</div>`).join('')}</div></div>`;
}, 80);
document.addEventListener('click', (e) => {
  if (e.target.closest('[data-up="clear"]')) { uploads.length = 0; drawUploads(); }
  const c = e.target.closest('[data-up-cancel]');
  if (c) { const u = uploads[+c.dataset.upCancel]; if (u?.state === 'up') u.ctl.abort(); else if (u) u.state = 'cancel'; drawUploads(); }
});

/* ── Eventos ───────────────────────────────────────────────────── */
function onPaste(e) {
  if (!F || dialogs.length || e.target.closest?.('input,textarea')) return;
  const files = [...(e.clipboardData?.files || [])];
  if (files.length && !rootRO() && F.mode === 'browse') { e.preventDefault(); enqueue(files.map((f) => ({ file: f, rel: f.name || `colado-${Date.now()}.png` })), F.root, F.path); }
}
function onKey(e) {
  if (!F || menuOpen()) return;
  if (dialogs.length) { const top = $$('.dialog').pop(); top?._keys?.(e); return; }
  const inInput = e.target.closest?.('input,textarea,select');
  if (inInput) { if (e.key === 'Escape' && e.target.matches('[data-f="q"]') && e.target.value) { e.target.value = ''; runSearch(''); e.stopPropagation(); } return; }
  const sel = selectedEntries(), mod = e.ctrlKey || e.metaKey, ro = rootRO();
  const handled = () => { e.preventDefault(); e.stopPropagation(); };
  if (e.key === 'Escape' && F.sel.size) { F.sel.clear(); draw(); handled(); }
  else if (e.key === 'Enter' && sel.length === 1) { openEntry(sel[0]); handled(); }
  else if ((e.key === 'Delete' || (e.key === 'Backspace' && mod)) && sel.length && !ro && F.mode !== 'trash') { trash(sel, e.shiftKey); handled(); }
  else if (e.key === 'F2' && sel.length === 1 && !ro) { rename(sel[0]); handled(); }
  else if (mod && e.key.toLowerCase() === 'a') { visibleEntries().forEach((x) => F.sel.add(keyOf(x))); draw(); handled(); }
  else if (mod && e.key.toLowerCase() === 'c' && sel.length) { setClipboard('copy'); handled(); }
  else if (mod && e.key.toLowerCase() === 'x' && sel.length && !ro) { setClipboard('cut'); handled(); }
  else if (mod && e.key.toLowerCase() === 'v' && clipboard && F.mode === 'browse') { paste(); handled(); }
  else if ((e.key === 'Backspace' && !mod) || (e.altKey && e.key === 'ArrowUp')) { up(); handled(); }
  else if (e.altKey && e.key === 'ArrowLeft') { back(); handled(); }
  else if (e.altKey && e.key === 'ArrowRight') { forward(); handled(); }
  else if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && !mod) {
    const items = visibleEntries(); if (!items.length) return;
    let i = items.findIndex((x) => keyOf(x) === F.anchor);
    i = clamp(i + (e.key === 'ArrowDown' ? 1 : -1), 0, items.length - 1);
    selectClick(keyOf(items[i]), { shiftKey: e.shiftKey });
    $(`[data-key="${CSS.escape(keyOf(items[i]))}"]`, F.body)?.scrollIntoView({ block: 'nearest' });
    handled();
  }
}
function wire() {
  const body = F.body;
  document.addEventListener('keydown', onKey, true);
  document.addEventListener('paste', onPaste);
  $('[data-f="q"]', body).addEventListener('input', debounce((e) => runSearch(e.target.value), 350));
  body.addEventListener('click', async (e) => {
    const t = e.target;
    let el;
    // celular: a barra lateral vira uma gaveta; fecha ao escolher um local ou tocar fora
    const files = $('.files', body);
    if (t.closest('.f-side [data-root], .f-side [data-fav], .f-side [data-f="trash"], [data-f="side-close"]')) files.classList.remove('side-open');
    if (t.closest('[data-f="side"]')) { files.classList.toggle('side-open'); return; }
    if ((el = t.closest('[data-f="disks-toggle"]'))) {
      const open = !(F.ctx.settings?.filesDisksOpen !== false);
      F.ctx.saveSetting?.('filesDisksOpen', open); drawSide();
    }
    else if ((el = t.closest('[data-f="disks-menu"]'))) disksMenu(el);
    else if (t.closest('[data-f="map-disks"]')) mapDisksDialog(F.ctx, afterMap);
    else if ((el = t.closest('[data-root]'))) go(el.dataset.root, '');
    else if ((el = t.closest('[data-fav]'))) { const f = F.ctx.favorites[+el.dataset.fav]; if (f) go(f.root, f.path); }
    else if (t.closest('[data-f="trash"]')) openTrash();
    else if (t.closest('[data-f="back"]')) back();
    else if (t.closest('[data-f="fwd"]')) forward();
    else if (t.closest('[data-f="mkdir"]')) mkdir();
    else if ((el = t.closest('[data-f="upload"]'))) { const r = el.getBoundingClientRect(); contextMenu(r.left, r.bottom + 6, [{ icon: 'upload', label: 'Arquivos…', run: () => pickUpload(false) }, { icon: 'folder', label: 'Pasta inteira…', run: () => pickUpload(true) }]); }
    else if ((el = t.closest('[data-f="tools"]')) && !el.disabled) { const r = el.getBoundingClientRect(); contextMenu(r.left, r.bottom + 6, toolsItems()); }
    else if (t.closest('[data-f="hidden"]')) toggleHidden();
    else if (t.closest('[data-f="empty-trash"]')) {
      if (await confirmDialog('Esvaziar a lixeira?', `${F.trash.length} item(ns) serão apagados para sempre.`, 'Esvaziar', { danger: true })) {
        try { await api.files.trashEmpty(); toast('Lixeira vazia'); openTrash(false); } catch (x) { toastError(x); }
      }
    }
    else if ((el = t.closest('[data-view]'))) { F.view = el.dataset.view; F.ctx.saveSetting('filesView', F.view); draw(); }
    else if ((el = t.closest('[data-crumb]')) && !el.disabled) { if (F.mode === 'search') { F.mode = 'browse'; F.search = null; $('[data-f="q"]', body).value = ''; } go(F.root, el.dataset.crumb); }
    else if ((el = t.closest('[data-sort]'))) { const k = el.dataset.sort; F.sort = { key: k, dir: F.sort.key === k ? -F.sort.dir : 1 }; draw(); }
    else if ((el = t.closest('[data-t]'))) {
      const [root, id] = el.closest('[data-trash]').dataset.trash.split('|');
      try {
        if (el.dataset.t === 'restore') { const r = await api.files.restore(root, id); toast('Restaurado', r.path); }
        else if (await confirmDialog('Excluir para sempre?', 'Isso não pode ser desfeito.', 'Excluir', { danger: true })) await api.files.trashDelete(root, id);
        openTrash(false);
      } catch (x) { toastError(x); }
    }
    else if ((el = t.closest('[data-key]'))) selectClick(el.dataset.key, e);
    else if (t.closest('.f-scroll') && !t.closest('button')) { F.sel.clear(); draw(); }
  });
  body.addEventListener('dblclick', (e) => {
    const el = e.target.closest('[data-key]'); if (!el) return;
    const ent = visibleEntries().find((x) => keyOf(x) === el.dataset.key);
    if (ent) openEntry(ent);
  });
  body.addEventListener('contextmenu', (e) => {
    if (e.target.closest('input')) return;
    const el = e.target.closest('[data-key]');
    if (el) { e.preventDefault(); const ent = visibleEntries().find((x) => keyOf(x) === el.dataset.key); if (ent) itemMenu(ent, e.clientX, e.clientY); }
    else if (e.target.closest('.f-scroll')) { e.preventDefault(); blankMenu(e.clientX, e.clientY); }
    else if (e.target.closest('[data-fav]')) {
      e.preventDefault();
      const i = +e.target.closest('[data-fav]').dataset.fav;
      contextMenu(e.clientX, e.clientY, [{ icon: 'star', label: 'Remover dos favoritos', run: () => { const f = [...F.ctx.favorites]; f.splice(i, 1); F.ctx.setFavorites(f); drawSide(); } }]);
    }
  });

  // Arrastar: itens internos (mover/copiar) e arquivos do computador (enviar)
  let dragKeys = null, depth = 0;
  const dropTarget = (el) => el?.closest('[data-drop-root]');
  body.addEventListener('dragstart', (e) => {
    const el = e.target.closest('[data-key]'); if (!el) return;
    if (!F.sel.has(el.dataset.key)) selectClick(el.dataset.key, {});
    dragKeys = { root: F.root, paths: selectedPaths() };
    e.dataTransfer.effectAllowed = 'copyMove';
    e.dataTransfer.setData('text/x-brunos-files', JSON.stringify(dragKeys));
  });
  body.addEventListener('dragend', () => { dragKeys = null; $$('.drop-on', body).forEach((x) => x.classList.remove('drop-on')); });
  body.addEventListener('dragenter', (e) => {
    if ([...e.dataTransfer.types].includes('Files') && !rootRO() && F.mode === 'browse') { depth++; body.querySelector('.files').classList.add('dropping'); }
  });
  body.addEventListener('dragleave', (e) => {
    if ([...e.dataTransfer.types].includes('Files') && --depth <= 0) { depth = 0; body.querySelector('.files')?.classList.remove('dropping'); }
  });
  body.addEventListener('dragover', (e) => {
    const isFiles = [...e.dataTransfer.types].includes('Files');
    const tgt = dropTarget(e.target);
    $$('.drop-on', body).forEach((x) => x !== tgt && x.classList.remove('drop-on'));
    if (dragKeys && tgt && !(tgt.dataset.dropRoot === dragKeys.root && dragKeys.paths.includes(tgt.dataset.dropPath))) {
      e.preventDefault(); tgt.classList.add('drop-on'); e.dataTransfer.dropEffect = e.ctrlKey || e.altKey || tgt.dataset.dropRoot !== dragKeys.root ? 'copy' : 'move';
    } else if (isFiles && !rootRO() && F.mode === 'browse') { e.preventDefault(); if (tgt) tgt.classList.add('drop-on'); }
  });
  body.addEventListener('drop', async (e) => {
    const tgt = dropTarget(e.target);
    $$('.drop-on', body).forEach((x) => x.classList.remove('drop-on'));
    body.querySelector('.files')?.classList.remove('dropping'); depth = 0;
    if (dragKeys) {
      e.preventDefault();
      if (tgt) { const k = dragKeys; dragKeys = null; moveTo(k.paths, k.root, tgt.dataset.dropRoot, tgt.dataset.dropPath, e.ctrlKey || e.altKey || tgt.dataset.dropRoot !== k.root); }
      return;
    }
    if ([...e.dataTransfer.types].includes('Files') && !rootRO() && F.mode === 'browse') {
      e.preventDefault(); e.stopPropagation();
      const list = await filesFromDrop(e.dataTransfer);
      const root = tgt?.dataset.dropRoot || F.root, path = tgt ? tgt.dataset.dropPath : F.path;
      if (rootById(root)?.readOnly) { toastError(new Error('esta pasta é somente leitura')); return; }
      enqueue(list, root, path);
    }
  });
}
