// Ferramentas do gerenciador de arquivos: mapa do espaço (treemap) e arquivos duplicados.
import { api } from './api.js';
import { $, $$, esc, fmtBytes, timeAgo, svg, fileSVG, extKind, toast, toastError, openDialog, confirmDialog } from './ui.js';

const FILE_COLORS = { img: '#34b46a', vid: '#e8505b', doc: '#3d7ff0', pdf: '#e0453a', zip: '#d8962b', aud: '#d24fa3', code: '#7a8396', txt: '#8b93a6', sheet: '#1f9d55', slide: '#e57a2e' };
const pct = (a, b) => (b ? `${((a / b) * 100).toLocaleString('pt-BR', { maximumFractionDigits: a / b < 0.1 ? 1 : 0 })}%` : '');

/* ── Mapa do espaço ─────────────────────────────────────────────── */

// Treemap "squarified": retângulos o mais quadrados possível, área proporcional ao tamanho.
function squarify(items, x, y, w, h) {
  const out = [];
  const total = items.reduce((s, i) => s + i.size, 0);
  if (!total || w <= 0 || h <= 0) return out;
  const scale = (w * h) / total;
  let rest = items.map((i) => ({ ...i, v: i.size * scale }));
  const worst = (row, side) => {
    const sum = row.reduce((s, r) => s + r.v, 0), mx = Math.max(...row.map((r) => r.v)), mn = Math.min(...row.map((r) => r.v));
    return Math.max((side * side * mx) / (sum * sum), (sum * sum) / (side * side * mn));
  };
  while (rest.length) {
    const side = Math.min(w, h);
    const row = [rest[0]];
    let i = 1;
    while (i < rest.length && worst([...row, rest[i]], side) <= worst(row, side)) row.push(rest[i++]);
    const sum = row.reduce((s, r) => s + r.v, 0);
    if (w >= h) {
      const rw = sum / h; let yy = y;
      for (const r of row) { const rh = r.v / rw; out.push({ ...r, x, y: yy, w: rw, h: rh }); yy += rh; }
      x += rw; w -= rw;
    } else {
      const rh = sum / w; let xx = x;
      for (const r of row) { const rw = r.v / rh; out.push({ ...r, x: xx, y, w: rw, h: rh }); xx += rw; }
      y += rh; h -= rh;
    }
    rest = rest.slice(i);
  }
  return out;
}

export function spaceDialog({ root, path = '', rootName, onOpen }) {
  let cur = path, data = null, ro = null;
  const d = openDialog(`<div class="sp">
      <div class="sp-head">${svg('treemap')}<b>Mapa do espaço</b><span class="sp-crumbs"></span><span class="f-spacer"></span>
        <button class="pill-btn" data-sp="refresh" title="Varrer de novo">${svg('refresh')}<span class="hide-sm">Atualizar</span></button>
        <button class="pill-btn" data-sp="open" title="Abrir esta pasta no gerenciador">${svg('folder')}<span class="hide-sm">Abrir</span></button></div>
      <div class="sp-sum"></div>
      <div class="sp-map"></div>
      <div class="sp-list"></div>
    </div>`, { cls: 'sp-dialog', width: 1000, onClose: () => ro?.disconnect() });
  const map = $('.sp-map', d.el);

  const crumbs = () => {
    const parts = cur ? cur.split('/') : [];
    $('.sp-crumbs', d.el).innerHTML = [`<button data-sp-go="">${esc(rootName)}</button>`, ...parts.map((p, i) => `<button data-sp-go="${esc(parts.slice(0, i + 1).join('/'))}">${esc(p)}</button>`)].join('<span>›</span>');
  };
  const draw = () => {
    if (!data) return;
    const items = data.items.filter((i) => i.size > 0);
    // itens muito pequenos viram um bloco "outros" (não dá para ler nem clicar)
    const min = data.size * 0.004;
    const big = items.filter((i) => i.size >= min).slice(0, 80);
    const others = data.size - big.reduce((s, i) => s + i.size, 0);
    const boxes = others > 0 ? [...big, { name: 'outros', size: others, other: true }] : big;
    const { width, height } = map.getBoundingClientRect();
    map.innerHTML = squarify(boxes, 0, 0, width, height).map((b) => {
      const color = b.other ? 'rgba(255,255,255,.08)' : b.dir ? `hsl(var(--bh) 55% ${32 + (b.size / data.size) * 25}%)` : FILE_COLORS[extKind(b.name)];
      const label = b.w > 70 && b.h > 34;
      return `<div class="sp-box ${b.dir ? 'dir' : ''} ${b.other ? 'other' : ''}" style="left:${b.x}px;top:${b.y}px;width:${b.w}px;height:${b.h}px;--c:${color}" ${b.dir ? `data-sp-go="${esc(cur ? `${cur}/${b.name}` : b.name)}"` : ''} title="${esc(b.name)} · ${fmtBytes(b.size)} (${pct(b.size, data.size)})">
        ${label ? `<span class="sp-lbl">${b.dir ? svg('folder') : ''}${esc(b.name)}</span><small>${fmtBytes(b.size)}</small>` : ''}</div>`;
    }).join('') || '<div class="empty-note">Pasta vazia</div>';
  };
  const drawList = () => {
    $('.sp-sum', d.el).innerHTML = `<b>${fmtBytes(data.size)}</b> em ${data.count.toLocaleString('pt-BR')} arquivos${data.truncated ? ' · <span class="sp-warn">varredura parcial (pasta grande demais; os números são mínimos)</span>' : ''} · medido ${esc(timeAgo(data.scannedAt))}`;
    $('.sp-list', d.el).innerHTML = data.items.slice(0, 40).map((i) => `<button class="sp-row" ${i.dir ? `data-sp-go="${esc(cur ? `${cur}/${i.name}` : i.name)}"` : 'disabled'}>
        ${i.dir ? svg('folder') : fileSVG(i.name)}<span class="nm">${esc(i.name)}</span>
        <span class="track"><i style="width:${data.size ? (i.size / data.size) * 100 : 0}%"></i></span>
        <span class="pc">${pct(i.size, data.size)}</span><span class="sz">${fmtBytes(i.size)}</span></button>`).join('');
  };
  const load = async (p, refresh = false) => {
    cur = p; crumbs();
    map.innerHTML = `<div class="sp-loading"><div class="f-loading"><i></i><i></i><i></i></div><small>Somando o tamanho das pastas… numa pasta grande isso leva alguns segundos.</small></div>`;
    try {
      data = await api.files.usage(root, p, refresh);
      if (cur !== p) return;
      draw(); drawList();
    } catch (e) { map.innerHTML = `<div class="empty-note">${esc(e.message)}</div>`; }
  };
  d.el.addEventListener('click', (e) => {
    const g = e.target.closest('[data-sp-go]'); if (g) { load(g.dataset.spGo); return; }
    const b = e.target.closest('[data-sp]'); if (!b) return;
    if (b.dataset.sp === 'refresh') load(cur, true);
    if (b.dataset.sp === 'open') { d.close(); onOpen?.(cur); }
  });
  ro = new ResizeObserver(() => draw());
  ro.observe(map);
  d.el._keys = (e) => { if (e.key === 'Backspace' && cur) { e.preventDefault(); load(cur.includes('/') ? cur.slice(0, cur.lastIndexOf('/')) : ''); } };
  load(cur);
}

/* ── Duplicados ─────────────────────────────────────────────────── */

export function duplicatesDialog({ root, path = '', rootName, readOnly, onChanged }) {
  let res = null;
  const where = path ? `${rootName}/${path}` : rootName;
  const d = openDialog(`<div class="dp">
      <div class="dp-head">${svg('dupes')}<b>Arquivos duplicados</b><small>em ${esc(where)}</small></div>
      <div class="dp-opts">Procurar arquivos iguais com pelo menos
        <select class="text-input dp-min"><option value="102400">100 KB</option><option value="1048576" selected>1 MB</option><option value="10485760">10 MB</option><option value="104857600">100 MB</option></select>
        <button class="pill-btn primary" data-dp="run">${svg('search')}Procurar</button></div>
      <div class="dp-body"><p class="dp-hint">O BrunOS compara o conteúdo dos arquivos (não só o nome). Em pastas grandes pode levar até um minuto e mexe bastante no disco.</p></div>
      <div class="dlg-actions dp-actions" hidden><span class="dp-selinfo"></span><span class="f-spacer"></span><button class="pill-btn danger" data-dp="trash" ${readOnly ? 'disabled' : ''}>${svg('trash')}Mover selecionados para a lixeira</button></div>
    </div>`, { cls: 'dp-dialog', width: 820 });
  const body = $('.dp-body', d.el);

  const selected = () => $$('input[data-dp-path]:checked', d.el).map((i) => i.dataset.dpPath);
  const updateSel = () => {
    const sel = $$('input[data-dp-path]:checked', d.el);
    const bytes = sel.reduce((s, i) => s + +i.dataset.size, 0);
    $('.dp-selinfo', d.el).textContent = sel.length ? `${sel.length} selecionado(s) · libera ${fmtBytes(bytes)}` : 'Nada selecionado';
    $('[data-dp="trash"]', d.el).disabled = readOnly || !sel.length;
  };
  const draw = () => {
    if (!res.groups.length) {
      body.innerHTML = `<div class="empty-note big">${svg('check')}<b>Nenhum duplicado encontrado</b>Foram olhados ${res.scanned.toLocaleString('pt-BR')} arquivos.</div>`;
      $('.dp-actions', d.el).hidden = true; return;
    }
    body.innerHTML = `<p class="dp-sum"><b>${res.groups.length}</b> grupo(s) de arquivos iguais · dá para liberar <b>${fmtBytes(res.wasted)}</b>${res.truncated ? ' · <span class="sp-warn">busca parcial (pasta grande demais)</span>' : ''}<br><small>Já deixei marcadas todas as cópias menos a mais antiga de cada grupo. Confira antes de apagar.</small></p>
      ${res.groups.map((g) => `<div class="dp-group"><div class="dp-ghead">${fileSVG(g.files[0].path)}<b>${esc(g.files[0].path.split('/').pop())}</b><small>${fmtBytes(g.size)} cada · ${g.files.length} cópias</small></div>
        ${g.files.map((f, i) => `<label class="dp-file"><input type="checkbox" data-dp-path="${esc(f.path)}" data-size="${g.size}" ${i > 0 ? 'checked' : ''}><span class="nm" title="${esc(f.path)}">${esc(f.path)}</span><small>${esc(timeAgo(f.mtime))}${i === 0 ? ' · mais antigo' : ''}</small></label>`).join('')}</div>`).join('')}`;
    $('.dp-actions', d.el).hidden = false;
    updateSel();
  };
  d.el.addEventListener('change', (e) => { if (e.target.matches('[data-dp-path]')) updateSel(); });
  d.el.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-dp]'); if (!b || b.disabled) return;
    if (b.dataset.dp === 'run') {
      b.disabled = true;
      body.innerHTML = '<div class="sp-loading"><div class="f-loading"><i></i><i></i><i></i></div><small>Comparando arquivos…</small></div>';
      try { res = await api.files.duplicates(root, path, $('.dp-min', d.el).value); draw(); }
      catch (x) { body.innerHTML = `<div class="empty-note">${esc(x.message)}</div>`; }
      b.disabled = false;
    } else if (b.dataset.dp === 'trash') {
      const paths = selected();
      // nunca deixa um grupo sem nenhuma cópia por engano
      const lost = res.groups.filter((g) => g.files.every((f) => paths.includes(f.path)));
      if (lost.length && !(await confirmDialog('Apagar todas as cópias?', `Em ${lost.length} grupo(s) você marcou todas as cópias, então nenhuma vai sobrar. Continuar?`, 'Apagar todas', { danger: true }))) return;
      try {
        await api.files.trash(root, paths);
        toast('Movido para a lixeira', `${paths.length} arquivo(s). Dá para restaurar pela Lixeira.`);
        res.groups = res.groups.map((g) => ({ ...g, files: g.files.filter((f) => !paths.includes(f.path)) })).filter((g) => g.files.length > 1);
        res.wasted = res.groups.reduce((s, g) => s + g.size * (g.files.length - 1), 0);
        draw(); onChanged?.();
      } catch (x) { toastError(x, 'Não foi possível mover para a lixeira'); }
    }
  });
}
