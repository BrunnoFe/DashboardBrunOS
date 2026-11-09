// Janela Widgets (etapa 7): Meus widgets · Modelos · Integrações, e o editor com prévia ao vivo.
// Carregada só quando abre. Termos em CONTEXT.md.
import { api } from './api.js';
import { $, $$, esc, svg, on, toast, toastError, confirmDialog, openDialog, timeAgo, clamp } from './ui.js';
import { SIZES, FORMATS, CHART_TYPES, RANGES, WINDOWS, ICONS, FOLDER_ICONS, FOLDER_HUES, ROTATE, modelGroups, normalize, widgetHTML, innerHTML, defaultColors, acceptsKind, fmtText } from './widgets.js';

const KIND_ICON = { adguard: 'adguard-home', speedtest: 'speedtest-tracker', plex: 'plex', immich: 'immich', watchyourlan: 'watchyourlan', tailscale: 'tailscale', transmission: 'transmission', syncthing: 'syncthing', nextcloud: 'nextcloud', vaultwarden: 'vaultwarden' };
const INTERVALS = [[10, '10 segundos'], [15, '15 segundos'], [30, '30 segundos'], [60, '1 minuto'], [120, '2 minutos'], [300, '5 minutos'], [600, '10 minutos'], [1800, '30 minutos'], [3600, '1 hora']];
const PREVIEW = { s: [240, 148], w: [508, 148], t: [240, 322], l: [508, 322], f: [508, 148] };

let st = null; // { body, ctx, view, page, draft, editId, integ }

const kindIcon = (k) => (KIND_ICON[k] ? `<img src="assets/icons/${KIND_ICON[k]}.webp" alt="">` : svg('link'));
const sw = (on_, attrs = '') => `<button type="button" class="switch ${on_ ? 'on' : ''}" role="switch" aria-checked="${!!on_}" ${attrs}></button>`;

export function mountWidgets(body, { ctx, edit, add }) {
  st = { body, ctx, view: 'mine', page: add ?? ctx.currentPage() };
  body.innerHTML = `<div class="wapp">
    <h2 class="sheet-title">Widgets</h2>
    <div class="wapp-grid">
      <nav class="wapp-nav">
        <button data-view="mine">${svg('grid')}<span>Meus widgets</span><small data-count="mine"></small></button>
        <button data-view="models">${svg('sparkles')}<span>Modelos</span></button>
        <button data-view="integrations">${svg('link')}<span>Integrações</span><small data-count="integ"></small></button>
        <p class="wapp-tip">Dica: na tela inicial, clique com o botão direito num widget para trocar o tamanho, mover de página ou organizar arrastando.</p>
      </nav>
      <section class="wapp-main"></section>
    </div></div>`;
  body.addEventListener('click', onClick);
  body.addEventListener('input', onInput);
  body.addEventListener('change', onInput);
  if (edit) openEditor(edit);
  else if (add != null) show('models');
  else show('mine');
  const repaint = () => paintPreviews();
  const offs = [on('stats', repaint), on('metrics', repaint), on('widgets:data', repaint)];
  return () => { offs.forEach((f) => f()); body.removeEventListener('click', onClick); st = null; };
}

export function focusWidgets(opts = {}) {
  if (!st) return;
  if (opts.edit) openEditor(opts.edit);
  else if (opts.add != null) { st.page = opts.add; show('models'); }
}

const main = () => $('.wapp-main', st.body);
function show(view) {
  st.view = view;
  $$('.wapp-nav [data-view]', st.body).forEach((b) => b.classList.toggle('on', b.dataset.view === view || (view === 'edit' && b.dataset.view === 'mine') || (view === 'integration' && b.dataset.view === 'integrations')));
  const w2 = st.ctx.w2;
  $('[data-count="mine"]', st.body).textContent = Object.keys(w2.items).length || '';
  $('[data-count="integ"]', st.body).textContent = st.ctx.integrations.length || '';
  ({ mine: drawMine, models: drawModels, integrations: drawIntegrations, edit: drawEditor, integration: drawIntegration })[view]();
  main().scrollTop = 0;
}

/* ── Prévias ───────────────────────────────────────────────────── */
function previewBox(item, id = '', scale = 0.62) {
  const [w, h] = PREVIEW[item.size] || PREVIEW.s;
  return `<div class="wapp-prev" style="width:${Math.round(w * scale)}px;height:${Math.round((h + 26) * scale)}px"><div class="wapp-prev-in" style="width:${w}px;height:${h + 26}px;transform:scale(${scale})">${widgetHTML(item, st.ctx.ctx(), { id })}</div></div>`;
}
function paintPreviews() {
  if (!st || document.hidden) return;
  const ctx = st.ctx.ctx();
  for (const wrap of $$('.widget-wrap[data-draft]', st.body)) {
    const box = wrap.firstElementChild, h = innerHTML(st.draft, ctx);
    if (box._h !== h) { box.innerHTML = h; box._h = h; }
  }
  for (const wrap of $$('.widget-wrap[data-model]', st.body)) {
    const m = st.models?.get(wrap.dataset.model); if (!m) continue;
    const box = wrap.firstElementChild, h = innerHTML(m, ctx);
    if (box._h !== h) { box.innerHTML = h; box._h = h; }
  }
}

/* ── Meus widgets ──────────────────────────────────────────────── */
function drawMine() {
  const w2 = st.ctx.w2;
  const pages = Object.keys(w2.areas).map(Number).sort((a, b) => a - b).filter((p) => w2.areas[p].some((id) => w2.items[id]));
  main().innerHTML = `<div class="wapp-head"><div><h3>Meus widgets</h3><p>Os widgets da tela inicial, por página. Clique para editar.</p></div>
    <span class="f-spacer"></span><button class="pill-btn" data-act="organize">${svg('sort')}Organizar na tela inicial</button><button class="pill-btn primary" data-act="new">${svg('plus')}Novo widget</button></div>
    ${pages.length ? pages.map((p) => `<div class="wapp-sec wapp-sec-row"><span>Página ${p + 1}</span><label class="wapp-inline">Colunas dos widgets <select class="text-input sm" data-pcols="${p}"><option value="0">automático</option>${[1, 2, 3, 4, 5, 6].map((n) => `<option value="${n}" ${w2.cols?.[p] === n ? 'selected' : ''}>${n}</option>`).join('')}</select></label></div><div class="wapp-cards">${w2.areas[p].filter((id) => w2.items[id]).map((id) => {
      const it = normalize(w2.items[id]);
      return `<div class="wapp-card" data-edit="${esc(id)}">${previewBox(it, id)}<div class="wapp-card-txt"><b>${esc(it.title || FORMATS[it.format].label)}</b><small>${FORMATS[it.format].label} · ${SIZES[it.size].label}</small></div>
        <div class="wapp-card-acts"><button class="icon-btn" data-dup="${esc(id)}" title="Duplicar">${svg('copy')}</button><button class="icon-btn danger" data-del="${esc(id)}" title="Remover">${svg('trash')}</button></div></div>`;
    }).join('')}</div>`).join('') : `<div class="wapp-empty">${svg('grid')}<p>Nenhum widget ainda.</p><button class="pill-btn primary" data-act="new">${svg('plus')}Escolher um modelo</button></div>`}`;
}

/* ── Modelos ───────────────────────────────────────────────────── */
function drawModels() {
  st.ctx.refreshInternet(true); // o modelo Internet precisa da medição para a prévia
  const groups = modelGroups(st.ctx.integrations);
  st.models = new Map();
  const total = st.ctx.pages();
  main().innerHTML = `<div class="wapp-head"><div><h3>Modelos</h3><p>Escolha um ponto de partida; dá para mudar tudo depois.</p></div><span class="f-spacer"></span>
    <label class="wapp-inline">Colocar na <select class="text-input sm" data-f="page">${Array.from({ length: total + 1 }, (_, i) => `<option value="${i}" ${i === st.page ? 'selected' : ''}>${i < total ? `página ${i + 1}` : 'página nova'}</option>`).join('')}</select></label></div>
    ${st.ctx.integrations.length ? '' : `<div class="wapp-note">${svg('link')}<span>Cadastre uma integração (AdGuard, Speedtest…) para ver os modelos com dados dos seus apps.</span><button class="pill-btn" data-view="integrations">Integrações</button></div>`}
    ${groups.map((g) => `<div class="wapp-sec">${esc(g.group)}</div><div class="wapp-cards">${g.models.map((m) => {
      const it = normalize(m.item);
      st.models.set(m.id, it);
      return `<div class="wapp-card model" data-model-use="${esc(m.id)}">${previewBox(it).replace('data-wid=""', `data-wid="" data-model="${esc(m.id)}"`)}<div class="wapp-card-txt"><b>${esc(m.name)}</b><small>${esc(m.desc)}</small></div></div>`;
    }).join('')}</div>`).join('')}`;
}

/* ── Editor ────────────────────────────────────────────────────── */
function openEditor(id, draft) {
  const w2 = st.ctx.w2;
  st.editId = id && w2.items[id] ? id : null;
  st.draft = normalize(draft || (st.editId ? w2.items[id] : {}));
  if (st.editId) st.page = +(Object.entries(w2.areas).find(([, ids]) => ids.includes(id))?.[0] ?? 0);
  show('edit');
}
const metricOptions = (format, cur) => {
  const groups = new Map();
  for (const d of st.ctx.defs().values()) {
    if (d.local?.startsWith('_') || !acceptsKind(format, d.kind, d)) continue;
    if (!groups.has(d.source)) groups.set(d.source, []);
    groups.get(d.source).push(d);
  }
  let out = '';
  if (cur && !st.ctx.defs().has(cur)) out += `<option value="${esc(cur)}" selected>(métrica indisponível)</option>`;
  for (const [src, list] of groups) out += `<optgroup label="${esc(src)}">${list.map((d) => `<option value="${esc(d.id)}" ${d.id === cur ? 'selected' : ''}>${esc(d.name.replace(/^.* › /, ''))}</option>`).join('')}</optgroup>`;
  return out;
};
function slotRole(i) {
  const d = st.draft;
  if (d.format === 'stat') return ['Número grande', 'Linha de apoio', 'Valor pequeno 1', 'Valor pequeno 2', 'Valor pequeno 3'][i];
  if (d.format === 'chart') return d.chart.type === 'donut' ? `Parte ${i + 1}` : `Série ${i + 1}`;
  if (d.format === 'list') return st.ctx.defs().get(d.slots[0]?.metric)?.kind === 'list' && i === 0 ? 'Lista' : `Linha ${i + 1}`;
  return `Valor ${i + 1}`;
}
function firstMetric(format) {
  for (const d of st.ctx.defs().values()) if (!d.local?.startsWith('_') && acceptsKind(format, d.kind, d)) return d.id;
  return null;
}
function colorsMode(slot, def) { return slot.colors === undefined ? (defaultColors(def) ? 'default' : 'off') : slot.colors === null ? 'off' : 'custom'; }

function slotHTML(s, i) {
  const d = st.draft, def = st.ctx.defs().get(s.metric);
  const isNum = def?.kind === 'num';
  const cm = colorsMode(s, def), cur = s.colors || defaultColors(def) || { warn: '', crit: '' };
  return `<div class="wslot" data-slot="${i}">
    <div class="wslot-top"><span class="wslot-role">${esc(slotRole(i))}</span>
      <select class="text-input sm" data-s="metric">${metricOptions(d.format, s.metric)}</select>
      <button class="icon-btn" data-sact="up" title="Subir" ${i === 0 ? 'disabled' : ''}>${svg('arrowUp')}</button>
      <button class="icon-btn" data-sact="down" title="Descer" ${i === d.slots.length - 1 ? 'disabled' : ''}>${svg('arrowDown')}</button>
      <button class="icon-btn danger" data-sact="del" title="Tirar">${svg('x')}</button></div>
    ${def?.kind === 'list' || def?.kind === 'series' ? '' : `<details class="wslot-more" ${st.open?.has(i) ? 'open' : ''}><summary>Ajustes: rótulo, unidade, ícone, cores</summary><div class="wslot-grid">
      <label>Rótulo<input class="text-input sm" data-s="label" value="${esc(s.label ?? '')}" placeholder="${esc((def?.name || '').replace(/^.* › /, ''))}"></label>
      ${isNum ? `<label>Unidade<input class="text-input sm" data-s="unit" value="${esc(s.unit ?? '')}" placeholder="${esc(def?.unit === 'B' ? 'automática (KB, MB, GB)' : def?.unit === 'B/s' ? 'automática' : def?.unit === 'ts' ? 'tempo relativo' : def?.unit || 'nenhuma')}"></label>
      <label>Casas decimais<select class="text-input sm" data-s="decimals"><option value="">automático</option>${[0, 1, 2].map((n) => `<option value="${n}" ${s.decimals === n ? 'selected' : ''}>${n}</option>`).join('')}</select></label>
      <label class="wsw">Número compacto (12,4 mil)${sw(s.compact, 'data-s="compact"')}</label>` : ''}
      ${d.format !== 'chart' ? `<label>Ícone<span class="wicon-row"><span class="wicon-prev">${svg(s.icon || def?.icon || 'info')}</span><select class="text-input sm" data-s="icon"><option value="">do app/métrica</option>${ICONS.map((n) => `<option value="${n}" ${s.icon === n ? 'selected' : ''}>${n}</option>`).join('')}</select></span></label>` : ''}
      ${isNum && def?.chartable && d.format !== 'chart' && (d.format !== 'stat' || i === 0) ? `<label class="wsw">Mini-gráfico${sw(s.spark, 'data-s="spark"')}</label>` : ''}
      ${isNum && (d.format === 'stat' && i === 0 || d.format === 'chart') ? `<label>Máximo (barra/anel)<input class="text-input sm" type="number" data-s="max" value="${esc(s.max ?? '')}" placeholder="${esc(def?.max ?? 'automático')}"></label>` : ''}
      ${isNum ? `<label class="wcolors">Cor por faixa<select class="text-input sm" data-s="colors"><option value="default" ${cm === 'default' ? 'selected' : ''}>${defaultColors(def) ? 'padrão do app' : 'sem cor (padrão)'}</option><option value="off" ${cm === 'off' ? 'selected' : ''}>sem cor</option><option value="custom" ${cm === 'custom' ? 'selected' : ''}>personalizada</option></select></label>
        ${cm === 'custom' ? `<label>Amarelo a partir de<input class="text-input sm" type="number" step="any" data-s="warn" value="${esc(cur.warn)}"></label><label>Vermelho a partir de<input class="text-input sm" type="number" step="any" data-s="crit" value="${esc(cur.crit)}"></label>
        <small class="hint wfull">Vermelho maior que amarelo: quanto maior, pior (ex.: temperatura). Vermelho menor que amarelo: quanto menor, pior (ex.: download).</small>` : ''}` : ''}
    </div></details>`}
  </div>`;
}

function drawEditor() {
  const d = st.draft, f = FORMATS[d.format];
  const total = st.ctx.pages();
  const ig = st.ctx.integrations;
  main().innerHTML = `<div class="wapp-head"><div><h3>${st.editId ? 'Editar widget' : 'Novo widget'}</h3><p>As mudanças aparecem na prévia ao lado. Nada muda na tela inicial até salvar.</p></div></div>
  <div class="wed">
    <form class="wed-form" onsubmit="return false">
      <div class="wed-sec"><label>Título<input class="text-input" data-f="title" value="${esc(d.title)}" placeholder="ex.: Minha rede" maxlength="40"></label>
        <label class="wsw">Mostrar o título dentro do widget${sw(d.caption, 'data-f="caption"')}</label></div>
      <div class="wed-sec"><span class="wed-lbl">Formato</span><div class="wed-chips">${Object.entries(FORMATS).map(([k, x]) => `<button type="button" class="wchip ${d.format === k ? 'on' : ''}" data-format="${k}" title="${esc(x.desc)}">${svg(x.icon)}${x.label}</button>`).join('')}</div><small class="hint">${esc(f.desc)}</small></div>
      <div class="wed-sec"><span class="wed-lbl">Tamanho</span><div class="wed-chips">${Object.entries(SIZES).map(([k, x]) => `<button type="button" class="wchip ${d.size === k ? 'on' : ''}" data-size="${k}"><i class="wsz-ico z-${k}"></i>${x.label}</button>`).join('')}</div></div>
      <div class="wed-sec"><label>Página<select class="text-input" data-f="page">${Array.from({ length: total + (st.editId ? 0 : 1) }, (_, i) => `<option value="${i}" ${i === st.page ? 'selected' : ''}>${i < total ? `Página ${i + 1}` : 'Página nova'}</option>`).join('')}${st.editId ? `<option value="${total}">Página nova</option>` : ''}</select></label></div>
      ${d.format === 'shortcuts' ? foldersHTML() : `
      <div class="wed-sec"><span class="wed-lbl">Métricas <small>${d.slots.length}/${f.max}</small></span>
        <div class="wslots">${d.slots.map(slotHTML).join('') || '<small class="hint">Nenhuma métrica ainda.</small>'}</div>
        ${d.slots.length < f.max ? `<button type="button" class="pill-btn" data-act="add-slot">${svg('plus')}Adicionar métrica</button>` : ''}
        ${ig.length ? '' : `<small class="hint">Só aparecem métricas do servidor. Cadastre integrações para usar dados dos apps.</small>`}</div>`}
      ${d.format === 'grid' ? `<div class="wed-sec"><span class="wed-lbl">Disposição</span><div class="seg">${[['auto', 'Automática'], ['cols', 'Em colunas'], ['rows', 'Em linhas']].map(([k, l]) => `<button type="button" class="${d.layout === k ? 'on' : ''}" data-layout="${k}">${l}</button>`).join('')}</div></div>` : ''}
      ${d.format === 'list' ? `<div class="wed-sec"><label>Itens no máximo<select class="text-input" data-f="listmax"><option value="0">conforme o tamanho</option>${[3, 5, 8].map((n) => `<option value="${n}" ${d.list.max === n ? 'selected' : ''}>${n}</option>`).join('')}</select></label><label class="wsw">Barrinhas proporcionais${sw(d.list.bars !== false, 'data-f="listbars"')}</label></div>` : ''}
      ${d.format === 'stat' ? `<div class="wed-sec"><label class="wsw">Barra de progresso (quando há máximo)${sw(d.bar !== false, 'data-f="bar"')}</label></div>` : ''}
      ${d.format === 'media' ? `<div class="wed-sec"><span class="wed-lbl">Mídia</span>
        ${st.ctx.defs().get(d.slots[0]?.metric)?.look === 'poster' ? '<small class="hint">Mostra a capa do que está tocando; com mais de uma sessão, a primeira e um “+N”.</small>'
          : `<label>Trocar a foto a cada<select class="text-input" data-f="rotate">${ROTATE.map(([m, l]) => `<option value="${m}" ${d.media.rotate === m ? 'selected' : ''}>${l}</option>`).join('')}</select>
          <small class="hint">A troca é feita no navegador, percorrendo as fotos do dia; cada foto nova baixa uma imagem pequena (prévia) uma vez.</small></label>`}
        <label class="wsw">Mostrar a legenda (data e lugar)${sw(d.media.caption !== false, 'data-f="mcaption"')}</label></div>` : ''}
      ${d.format === 'chart' ? `<div class="wed-sec"><span class="wed-lbl">Gráfico</span><div class="seg">${Object.entries(CHART_TYPES).map(([k, l]) => `<button type="button" class="${d.chart.type === k ? 'on' : ''}" data-ctype="${k}">${l}</button>`).join('')}</div>
        ${d.chart.type !== 'donut' ? `<span class="wed-lbl">Período</span><div class="seg">${Object.entries(RANGES).map(([k, l]) => `<button type="button" class="${d.chart.range === k ? 'on' : ''}" data-range="${k}">${l}</button>`).join('')}</div>
        <small class="hint">Métricas do servidor usam o histórico do BrunOS; as dos apps usam o histórico do próprio app (ex.: AdGuard 24 h, Speedtest 7 dias) ou, se ele não tiver, o BrunOS passa a guardar um ponto a cada 5 minutos.</small>
        <label class="wsw">Mínimo, máximo e média${sw(d.chart.stats, 'data-f="cstats"')}</label>` : ''}</div>` : ''}
      <div class="wed-sec"><span class="wed-lbl">Ao clicar</span><select class="text-input" data-f="click">${[['auto', 'Automático'], ['app', 'Abrir o app da integração'], ['window', 'Abrir uma janela do BrunOS'], ['url', 'Abrir um endereço'], ['none', 'Nada']].map(([k, l]) => `<option value="${k}" ${d.click.kind === k ? 'selected' : ''}>${l}</option>`).join('')}</select>
        ${d.click.kind === 'window' ? `<select class="text-input" data-f="clicktarget">${Object.entries(WINDOWS).map(([k, l]) => `<option value="${k}" ${d.click.target === k ? 'selected' : ''}>${l}</option>`).join('')}</select>` : ''}
        ${d.click.kind === 'app' ? `<select class="text-input" data-f="clicktarget">${ig.map((x) => `<option value="${esc(x.id)}" ${d.click.target === x.id ? 'selected' : ''}>${esc(x.name)}</option>`).join('') || '<option value="">nenhuma integração</option>'}</select>` : ''}
        ${d.click.kind === 'url' ? `<input class="text-input" data-f="clicktarget" value="${esc(d.click.target || '')}" placeholder="http://192.168.1.1">` : ''}</div>
      <div class="wed-sec"><span class="wed-lbl">Aparência</span><label class="wsw">Seguir o tema do BrunOS${sw(d.look.follow !== false, 'data-f="follow"')}</label>
        ${d.look.follow === false ? `<label>Transparência do fundo <input type="range" min="0" max="100" value="${d.look.opacity ?? 50}" data-f="opacity" style="--p:${d.look.opacity ?? 50}%"></label>
        <label class="wsw">Cor própria${sw(d.look.hue != null, 'data-f="ownhue"')}</label>
        ${d.look.hue != null ? `<label>Matiz <input type="range" min="0" max="359" value="${d.look.hue}" data-f="hue" style="--p:${d.look.hue / 3.59}%"></label>` : ''}
        <label class="wsw">Esconder o nome embaixo${sw(d.look.hideLabel, 'data-f="hidelabel"')}</label>` : '<small class="hint">Igual aos outros widgets: vidro, cor de destaque do papel de parede e nome embaixo.</small>'}</div>
    </form>
    <aside class="wed-side"><div class="wed-preview">${previewEditor()}</div>
      <div class="wed-actions">${st.editId ? `<button class="pill-btn danger" data-act="remove">${svg('trash')}Remover</button>` : ''}<span class="f-spacer"></span><button class="pill-btn" data-act="cancel">Cancelar</button><button class="pill-btn primary" data-act="save">${svg('check')}Salvar</button></div></aside>
  </div>`;
}
/* Formato Atalhos: pastas escolhidas, grade ou lista, ícone, cor e espaço livre */
function foldersHTML() {
  const d = st.draft, roots = st.ctx.roots();
  const rootName = (id) => roots.find((r) => r.id === id)?.name || id;
  return `<div class="wed-sec"><span class="wed-lbl">Pastas <small>${d.folders.length || 'favoritas'}</small></span>
    <div class="wslots">${d.folders.length ? d.folders.map((f, i) => `<div class="wslot wfold" data-fold="${i}">
      <div class="wslot-top"><span class="wfold-ico" style="--fh:${f.color ?? 'var(--bh)'}">${svg(f.icon || 'folder')}</span>
        <input class="text-input sm" data-fo="name" value="${esc(f.name || '')}" placeholder="${esc(f.path.split('/').pop() || rootName(f.root))}" aria-label="Nome do atalho">
        <button type="button" class="icon-btn" data-foact="up" title="Subir" ${i === 0 ? 'disabled' : ''}>${svg('arrowUp')}</button>
        <button type="button" class="icon-btn" data-foact="down" title="Descer" ${i === d.folders.length - 1 ? 'disabled' : ''}>${svg('arrowDown')}</button>
        <button type="button" class="icon-btn danger" data-foact="del" title="Tirar">${svg('x')}</button></div>
      <small class="hint wfold-path">${esc(rootName(f.root))}${f.path ? ` / ${esc(f.path)}` : ''}</small>
      <div class="wfold-opts"><select class="text-input sm" data-fo="icon" aria-label="Ícone">${FOLDER_ICONS.map((n) => `<option value="${n}" ${(f.icon || 'folder') === n ? 'selected' : ''}>${{ folder: 'pasta', film: 'filmes', music: 'música', image: 'fotos', doc: 'documentos', download: 'downloads', box: 'backup', disks: 'disco', star: 'favorita', home: 'casa', user: 'pessoal', code: 'código' }[n]}</option>`).join('')}</select>
        <span class="wfold-hues"><button type="button" class="swatch ${f.color == null ? 'on' : ''}" data-fohue="" title="Cor de destaque" style="background:var(--brand)"></button>${FOLDER_HUES.map((h) => `<button type="button" class="swatch ${f.color === h ? 'on' : ''}" data-fohue="${h}" style="background:hsl(${h} 75% 52%)" aria-label="Matiz ${h}"></button>`).join('')}</span></div>
    </div>`).join('') : '<small class="hint">Sem pastas escolhidas, o widget mostra as favoritas do Arquivos (estrela na barra lateral).</small>'}</div>
    <button type="button" class="pill-btn" data-act="add-folder">${svg('folderPlus')}Adicionar pasta</button></div>
  <div class="wed-sec"><span class="wed-lbl">Disposição</span><div class="seg">${[['grid', 'Grade'], ['list', 'Lista']].map(([k, l]) => `<button type="button" class="${d.files.layout === k ? 'on' : ''}" data-flayout="${k}">${l}</button>`).join('')}</div>
    <label class="wsw">Espaço livre do disco${sw(d.files.showFree, 'data-f="showfree"')}</label>
    <small class="hint">O tamanho do widget decide quantas pastas aparecem.</small></div>`;
}
// Navegador de pastas (usa a mesma API do Arquivos) → { root, path }
function pickFolder() {
  return new Promise((resolve) => {
    const roots = st.ctx.roots();
    if (!roots.length) { toast('Nenhuma pasta disponível', 'Mapeie um disco ou monte /files.'); resolve(null); return; }
    let root = roots[0].id, path = '', done = false;
    const d = openDialog(`<h2>${svg('folder')}Escolher pasta</h2><div class="wpick"><div class="seg wpick-roots">${roots.map((r) => `<button type="button" data-root="${esc(r.id)}">${esc(r.name)}</button>`).join('')}</div>
      <div class="wpick-crumbs"></div><div class="wpick-list"></div>
      <div class="dlg-actions"><button type="button" class="pill-btn" data-c="no">Cancelar</button><button type="button" class="pill-btn primary" data-c="ok">${svg('check')}Escolher esta pasta</button></div></div>`,
    { width: 480, onClose: () => { if (!done) resolve(null); } });
    const draw = async () => {
      $$('[data-root]', d.el).forEach((b) => b.classList.toggle('on', b.dataset.root === root));
      const parts = path.split('/').filter(Boolean);
      $('.wpick-crumbs', d.el).innerHTML = `<button type="button" data-go="">${esc(roots.find((r) => r.id === root)?.name || root)}</button>${parts.map((p, i) => ` / <button type="button" data-go="${esc(parts.slice(0, i + 1).join('/'))}">${esc(p)}</button>`).join('')}`;
      const box = $('.wpick-list', d.el);
      box.innerHTML = '<div class="empty-note">Carregando…</div>';
      try {
        const dirs = (await api.files.list(root, path)).entries.filter((x) => x.dir && !x.hidden);
        box.innerHTML = dirs.length ? dirs.map((x) => `<button type="button" class="wpick-dir" data-go="${esc(path ? `${path}/${x.name}` : x.name)}">${svg('folder')}<span>${esc(x.name)}</span>${svg('chevR')}</button>`).join('') : '<div class="empty-note">Sem subpastas</div>';
      } catch (e) { box.innerHTML = `<div class="empty-note">${esc(e.message)}</div>`; }
    };
    d.el.addEventListener('click', (e) => {
      const r = e.target.closest('[data-root]'); if (r) { root = r.dataset.root; path = ''; draw(); return; }
      const g = e.target.closest('[data-go]'); if (g) { path = g.dataset.go; draw(); return; }
      const c = e.target.closest('[data-c]'); if (!c) return;
      done = true; d.close(); resolve(c.dataset.c === 'ok' ? { root, path } : null);
    });
    draw();
  });
}
function previewEditor() {
  const [w, h] = PREVIEW[st.draft.size] || PREVIEW.s;
  const scale = Math.min(1, Math.min(380, innerWidth - 70) / w);
  return `<div class="wapp-prev big" style="width:${Math.round(w * scale)}px;height:${Math.round((h + 26) * scale)}px"><div class="wapp-prev-in" style="width:${w}px;height:${h + 26}px;transform:scale(${scale})">${widgetHTML(st.draft, st.ctx.ctx()).replace('data-wid=""', 'data-wid="" data-draft="1"')}</div></div>`;
}
const redrawPreview = () => { const p = $('.wed-preview', st.body); if (p) p.innerHTML = previewEditor(); };
function rememberOpen() { st.open = new Set($$('.wslot-more[open]', st.body).map((el) => +el.closest('[data-slot]').dataset.slot)); }
function redrawEditor() { rememberOpen(); const y = main().scrollTop; drawEditor(); main().scrollTop = y; }

function setFormat(k) {
  const d = st.draft;
  d.format = k;
  const defs = st.ctx.defs();
  d.slots = d.slots.filter((s) => acceptsKind(k, defs.get(s.metric)?.kind, defs.get(s.metric))).slice(0, FORMATS[k].max);
  if (!d.slots.length && FORMATS[k].max) { const m = firstMetric(k); if (m) d.slots.push({ metric: m }); }
  if (k === 'chart' && d.size === 's' && d.chart.type !== 'donut') d.size = 'w';
}

function saveDraft() {
  const w2 = st.ctx.w2, d = st.draft;
  const id = st.editId || st.ctx.newId();
  const samePage = st.editId && (w2.areas[st.page] || []).includes(id);
  const item = structuredClone(d);
  if (samePage) w2.items[id] = item;
  else st.ctx.put(id, item, st.page);
  st.ctx.save();
  toast('Widget salvo', d.title || FORMATS[d.format].label);
  st.editId = null; st.draft = null;
  show('mine');
}

/* ── Integrações ───────────────────────────────────────────────── */
async function drawIntegrations() {
  const list = st.ctx.integrations;
  const metrics = st.ctx.metrics;
  main().innerHTML = `<div class="wapp-head"><div><h3>Integrações</h3><p>Os apps de onde os widgets tiram dados. A senha ou o token fica guardado cifrado no servidor e nunca vem para o navegador.</p></div>
    <span class="f-spacer"></span><button class="pill-btn primary" data-act="new-integ">${svg('plus')}Nova integração</button></div>
    <div class="wapp-suggest"></div>
    <div class="winteg-list">${list.length ? list.map((it) => {
      const v = metrics[it.id];
      const status = v?.err ? `<span class="wst bad">${svg('alert')}${esc(v.err)}</span>` : v?.at ? `<span class="wst ok">atualizado ${esc(timeAgo(v.at))}</span>` : '<span class="wst">só busca quando algum widget usa e a tela está aberta</span>';
      return `<button class="winteg" data-integ="${esc(it.id)}"><span class="winteg-ico">${kindIcon(it.kind)}</span><span class="winteg-txt"><b>${esc(it.name)}</b><small>${esc(it.url)}</small>${status}</span><span class="chev">${svg('chevR')}</span></button>`;
    }).join('') : `<div class="wapp-empty">${svg('link')}<p>Nenhuma integração ainda.</p></div>`}</div>`;
  try {
    const sugg = (await api.integrations.suggest()).filter((s) => !list.some((it) => it.kind === s.kind && it.url === s.url));
    const box = $('.wapp-suggest', st.body);
    if (box && sugg.length && st.view === 'integrations') {
      box.innerHTML = `<div class="wapp-sec">Encontrei no Runtipi</div><div class="winteg-list">${sugg.map((s, i) => `<div class="winteg sugg"><span class="winteg-ico">${kindIcon(s.kind)}</span><span class="winteg-txt"><b>${esc(s.name)}</b><small>${esc(s.url)}</small></span><button class="pill-btn" data-sugg="${i}">${svg('plus')}Adicionar</button></div>`).join('')}</div>`;
      st.sugg = sugg;
    }
  } catch { /* sem sugestões */ }
}

function openIntegration(id, preset) {
  const it = id ? st.ctx.integrations.find((x) => x.id === id) : null;
  st.integ = it ? { ...structuredClone(it), secret: '', options: { ...(it.options || {}) } } : { kind: preset?.kind || '', name: preset?.name || '', url: preset?.url || '', user: '', secret: '', interval: 0, alerts: {}, options: {}, hasSecret: false };
  if (!it && st.integ.kind) applyKindDefaults();
  st.testResult = null;
  show('integration');
}
function applyKindDefaults() {
  const k = st.ctx.kinds.find((x) => x.id === st.integ.kind); if (!k) return;
  st.integ.interval ||= k.interval;
  st.integ.name ||= k.name;
  for (const a of k.alerts) st.integ.alerts[a.id] ||= { on: false, value: a.default };
}
function drawIntegration() {
  const g = st.integ, kinds = st.ctx.kinds, k = kinds.find((x) => x.id === g.kind);
  if (!k) {
    main().innerHTML = `<div class="wapp-head"><div><h3>Nova integração</h3><p>Qual app?</p></div><span class="f-spacer"></span><button class="pill-btn" data-view="integrations">Voltar</button></div>
      <div class="winteg-list">${kinds.map((x) => `<button class="winteg" data-kind="${esc(x.id)}"><span class="winteg-ico">${kindIcon(x.id)}</span><span class="winteg-txt"><b>${esc(x.name)}</b><small>${esc(x.desc)}</small></span><span class="chev">${svg('chevR')}</span></button>`).join('')}</div>
      <p class="hint">Todos só leem dados: o BrunOS nunca muda nada nos seus apps.</p>`;
    return;
  }
  const field = (f) => {
    if (f.id !== 'url' && f.id !== 'user' && f.id !== 'secret') { // campo extra (ex.: álbum do Immich)
      return `<label>${esc(f.label)}<input class="text-input" data-gopt="${esc(f.id)}" value="${esc(g.options?.[f.id] || '')}" placeholder="${esc(f.placeholder || '')}">${f.help ? `<small class="hint">${esc(f.help)}</small>` : ''}</label>`;
    }
    if (f.id === 'secret') return `<label>${esc(f.label)}${f.required ? '' : ' <small>(opcional)</small>'}<input class="text-input" type="password" autocomplete="new-password" data-g="secret" value="${esc(g.secret)}" placeholder="${g.hasSecret ? '•••••••• guardado (deixe vazio para manter)' : ''}">${f.help ? `<small class="hint">${esc(f.help)}</small>` : ''}</label>`;
    return `<label>${esc(f.label)}<input class="text-input" data-g="${f.id}" value="${esc(g[f.id] || '')}" placeholder="${esc(f.placeholder || '')}" ${f.id === 'url' ? 'inputmode="url"' : ''}>${f.help ? `<small class="hint">${esc(f.help)}</small>` : ''}</label>`;
  };
  const defs = new Map(k.metrics.map((m) => [m.id, m]));
  const tr = st.testResult;
  main().innerHTML = `<div class="wapp-head"><span class="winteg-ico big">${kindIcon(k.id)}</span><div><h3>${g.id ? esc(g.name) : `Nova integração: ${esc(k.name)}`}</h3><p>${esc(k.desc)}</p></div><span class="f-spacer"></span><button class="pill-btn" data-view="integrations">Voltar</button></div>
    <form class="form-grid winteg-form" onsubmit="return false">
      <label>Nome<input class="text-input" data-g="name" value="${esc(g.name)}" maxlength="60"><small class="hint">Aparece nas métricas, ex.: “${esc(g.name || k.name)} › ${esc(k.metrics[0]?.name || '')}”.</small></label>
      ${k.note ? `<div class="wapp-note">${svg('info')}<span>${esc(k.note)}</span></div>` : ''}
      ${k.id === 'tailscale' ? `<div class="wts-cmds"><button type="button" class="pill-btn" data-act="ts-cmds">${svg('terminal')}Mostrar os comandos de atualização</button><pre class="pw-cmds" hidden tabindex="0"></pre></div>` : ''}
      ${k.fields.map(field).join('')}
      <label>Buscar a cada<select class="text-input" data-g="interval">${INTERVALS.map(([s, l]) => `<option value="${s}" ${g.interval === s ? 'selected' : ''}>${l}${s === k.interval ? ' (recomendado)' : ''}</option>`).join('')}</select>
        <small class="hint">Só enquanto alguém está com o BrunOS aberto e algum widget usa esta integração. Avisos ligados fazem uma busca a cada 5 minutos mesmo com a tela fechada.</small></label>
      <div class="wed-lbl">Avisos no sino</div>
      <div class="walerts">${k.alerts.map((a) => { const s = g.alerts[a.id] || { on: false, value: a.default }; return `<div class="walert"><span class="walert-txt"><b>${esc(a.name)}</b><small>${esc(a.desc || '')}</small></span>
        ${a.fixed ? '' : `<span class="walert-val"><input class="text-input sm" type="number" step="any" data-alert-val="${a.id}" value="${esc(s.value || a.default)}"><small>${esc(a.unit || '')}</small></span>`}${sw(s.on, `data-alert="${a.id}"`)}</div>`; }).join('')}</div>
      ${tr ? `<div class="wtest ${tr.ok ? 'ok' : 'bad'}">${tr.ok ? `${svg('check')}<div><b>Funcionou!</b><small>${Object.entries(tr.values || {}).filter(([id]) => defs.get(id) && ['num', 'text'].includes(defs.get(id).kind)).slice(0, 5).map(([id, v]) => `${esc(defs.get(id).name)}: ${esc(fmtText(v, defs.get(id), {}))}`).join(' · ')}</small></div>` : `${svg('alert')}<div><b>Não funcionou</b><small>${esc(tr.error)}</small></div>`}</div>` : ''}
      <div class="dlg-actions">${g.id ? `<button type="button" class="pill-btn danger" data-act="del-integ" style="margin-right:auto">${svg('trash')}Apagar</button>` : ''}
        <button type="button" class="pill-btn" data-act="test-integ">${svg('refresh')}Testar</button><button type="button" class="pill-btn primary" data-act="save-integ">${svg('check')}Salvar</button></div>
    </form>`;
}
function integBody() {
  const g = st.integ;
  const b = { kind: g.kind, name: g.name, url: g.url, user: g.user, interval: +g.interval, alerts: g.alerts, options: g.options || {} };
  if (g.secret) b.secret = g.secret;
  return b;
}
async function testInteg(btn) {
  btn.disabled = true; btn.classList.add('busy');
  try { st.testResult = await api.integrations.test({ ...integBody(), ...(st.integ.id ? { id: st.integ.id } : {}) }); }
  catch (e) { st.testResult = { ok: false, error: e.message }; }
  drawIntegration();
}
async function saveInteg(btn) {
  btn.disabled = true;
  try {
    const g = st.integ;
    if (g.id) await api.integrations.update(g.id, integBody()); else await api.integrations.add(integBody());
    await st.ctx.reloadIntegrations();
    toast('Integração salva', g.name);
    show('integrations');
  } catch (e) { btn.disabled = false; toastError(e, 'Não foi possível salvar'); }
}
async function deleteInteg() {
  const g = st.integ;
  if (!(await confirmDialog(`Apagar ${g.name}?`, 'Os widgets que usam esta integração vão ficar sem dados (você pode editá-los depois). A senha ou o token guardado é apagado.', 'Apagar', { danger: true }))) return;
  try { await api.integrations.del(g.id); await st.ctx.reloadIntegrations(); toast('Integração apagada', g.name); show('integrations'); }
  catch (e) { toastError(e, 'Não foi possível apagar'); }
}

/* ── Eventos ───────────────────────────────────────────────────── */
async function onClick(e) {
  if (!st) return;
  const q = (s) => e.target.closest(s);
  let el;
  if ((el = q('[data-view]'))) { show(el.dataset.view); return; }
  if ((el = q('[data-dup]'))) { const w2 = st.ctx.w2, id = el.dataset.dup, n = st.ctx.newId(); st.ctx.put(n, structuredClone(w2.items[id]), +(Object.entries(w2.areas).find(([, ids]) => ids.includes(id))?.[0] ?? 0)); st.ctx.save(); drawMine(); return; }
  if ((el = q('[data-del]'))) { const id = el.dataset.del; if (await confirmDialog('Remover este widget?', 'Ele sai da tela inicial. Dá para criar de novo a partir dos modelos.', 'Remover', { danger: true })) { st.ctx.remove(id); drawMine(); } return; }
  if ((el = q('[data-edit]'))) { openEditor(el.dataset.edit); return; }
  if ((el = q('[data-model-use]'))) { const m = st.models.get(el.dataset.modelUse); st.editId = null; openEditor(null, { ...m, model: el.dataset.modelUse }); return; }
  if ((el = q('[data-integ]'))) { openIntegration(el.dataset.integ); return; }
  if ((el = q('[data-kind]'))) { st.integ.kind = el.dataset.kind; applyKindDefaults(); drawIntegration(); return; }
  if ((el = q('[data-sugg]'))) { openIntegration(null, st.sugg[+el.dataset.sugg]); return; }
  if ((el = q('[data-alert]'))) { const id = el.dataset.alert; const a = st.integ.alerts[id] ||= { on: false }; a.on = !a.on; el.classList.toggle('on', a.on); el.setAttribute('aria-checked', a.on); return; }
  const d = st.draft;
  if (st.view === 'edit' && d) {
    if ((el = q('[data-foact]'))) {
      const i = +el.closest('[data-fold]').dataset.fold, a = el.dataset.foact;
      if (a === 'del') d.folders.splice(i, 1);
      else { const j = a === 'up' ? i - 1 : i + 1; if (j >= 0 && j < d.folders.length) [d.folders[i], d.folders[j]] = [d.folders[j], d.folders[i]]; }
      redrawEditor(); return;
    }
    if ((el = q('[data-fohue]'))) { const f = d.folders[+el.closest('[data-fold]').dataset.fold]; f.color = el.dataset.fohue === '' ? undefined : +el.dataset.fohue; redrawEditor(); return; }
    if ((el = q('[data-flayout]'))) { d.files.layout = el.dataset.flayout; redrawEditor(); return; }
    if ((el = q('[data-format]'))) { setFormat(el.dataset.format); redrawEditor(); return; }
    if ((el = q('[data-size]'))) { d.size = el.dataset.size; $$('[data-size]', st.body).forEach((b) => b.classList.toggle('on', b === el)); redrawPreview(); return; }
    if ((el = q('[data-layout]'))) { d.layout = el.dataset.layout; redrawEditor(); return; }
    if ((el = q('[data-ctype]'))) { d.chart.type = el.dataset.ctype; redrawEditor(); return; }
    if ((el = q('[data-range]'))) { d.chart.range = el.dataset.range; redrawEditor(); return; }
    if ((el = q('[data-sact]'))) {
      const i = +el.closest('[data-slot]').dataset.slot, a = el.dataset.sact;
      if (a === 'del') d.slots.splice(i, 1);
      else { const j = a === 'up' ? i - 1 : i + 1; if (j >= 0 && j < d.slots.length) [d.slots[i], d.slots[j]] = [d.slots[j], d.slots[i]]; }
      st.open = new Set(); redrawEditor(); return;
    }
    if ((el = q('.switch[data-s]'))) { const s = d.slots[+el.closest('[data-slot]').dataset.slot]; const k = el.dataset.s; s[k] = !s[k]; el.classList.toggle('on', s[k]); redrawPreview(); return; }
    if ((el = q('.switch[data-f]'))) {
      const k = el.dataset.f;
      if (k === 'caption') d.caption = !d.caption;
      else if (k === 'bar') d.bar = d.bar === false;
      else if (k === 'listbars') d.list.bars = d.list.bars === false;
      else if (k === 'cstats') d.chart.stats = !d.chart.stats;
      else if (k === 'follow') { d.look.follow = d.look.follow === false; redrawEditor(); return; }
      else if (k === 'ownhue') { d.look.hue = d.look.hue == null ? 200 : null; redrawEditor(); return; }
      else if (k === 'hidelabel') d.look.hideLabel = !d.look.hideLabel;
      else if (k === 'showfree') d.files.showFree = !d.files.showFree;
      else if (k === 'mcaption') d.media.caption = d.media.caption === false;
      el.classList.toggle('on'); redrawPreview(); return;
    }
  }
  if ((el = q('[data-act]'))) {
    const a = el.dataset.act;
    if (a === 'new') { show('models'); }
    else if (a === 'organize') st.ctx.organize();
    else if (a === 'add-folder') { const f = await pickFolder(); if (f && st?.draft) { st.draft.folders.push(f); redrawEditor(); } }
    else if (a === 'add-slot') { const m = firstMetric(d.format); if (m) { d.slots.push({ metric: m }); redrawEditor(); } else toast('Nenhuma métrica disponível', 'Cadastre uma integração ou escolha outro formato.'); }
    else if (a === 'cancel') { st.draft = null; show(st.editId ? 'mine' : 'models'); }
    else if (a === 'save') saveDraft();
    else if (a === 'remove') { if (await confirmDialog('Remover este widget?', 'Ele sai da tela inicial.', 'Remover', { danger: true })) { st.ctx.remove(st.editId); st.editId = null; show('mine'); } }
    else if (a === 'new-integ') openIntegration(null);
    else if (a === 'ts-cmds') {
      const pre = el.parentElement.querySelector('pre');
      try { pre.textContent = (await api.power.setup()).updateCommands; pre.hidden = false; el.hidden = true; }
      catch (e) { toastError(e, 'Não deu para gerar os comandos'); }
    }
    else if (a === 'test-integ') testInteg(el);
    else if (a === 'save-integ') saveInteg(el);
    else if (a === 'del-integ') deleteInteg();
  }
}

function onInput(e) {
  if (!st) return;
  const t = e.target;
  if (st.view === 'integration' && st.integ) {
    if (t.dataset.g) { st.integ[t.dataset.g] = t.dataset.g === 'interval' ? +t.value : t.value; st.testResult = null; }
    if (t.dataset.gopt) { st.integ.options = { ...(st.integ.options || {}), [t.dataset.gopt]: t.value }; st.testResult = null; }
    if (t.dataset.alertVal) { const a = st.integ.alerts[t.dataset.alertVal] ||= { on: false }; a.value = parseFloat(t.value) || 0; }
    return;
  }
  if (st.view === 'models' && t.dataset.f === 'page') { st.page = +t.value; return; }
  if (t.dataset.pcols != null) { if (e.type === 'change') st.ctx.setCols(+t.dataset.pcols, +t.value); return; }
  const d = st.draft; if (st.view !== 'edit' || !d) return;
  if (t.dataset.fo) {
    const f = d.folders[+t.closest('[data-fold]').dataset.fold], k = t.dataset.fo;
    if (k === 'name') f.name = t.value.trim() ? t.value.slice(0, 40) : undefined;
    else if (k === 'icon') { if (e.type !== 'change') return; f.icon = t.value === 'folder' ? undefined : t.value; redrawEditor(); return; }
    redrawPreview(); return;
  }
  if (t.dataset.f) {
    const k = t.dataset.f;
    if (k === 'title') d.title = t.value;
    else if (k === 'page') st.page = +t.value;
    else if (k === 'listmax') d.list.max = +t.value;
    else if (k === 'rotate') d.media.rotate = +t.value;
    else if (k === 'click') { d.click = { kind: t.value }; if (t.value === 'window') d.click.target = 'live'; if (t.value === 'app') d.click.target = st.ctx.integrations[0]?.id; if (e.type === 'change') redrawEditor(); return; }
    else if (k === 'clicktarget') d.click.target = t.value;
    else if (k === 'opacity') { d.look.opacity = +t.value; t.style.setProperty('--p', `${t.value}%`); }
    else if (k === 'hue') { d.look.hue = +t.value; t.style.setProperty('--p', `${t.value / 3.59}%`); }
    redrawPreview();
    return;
  }
  if (t.dataset.s) {
    const i = +t.closest('[data-slot]').dataset.slot, s = d.slots[i], k = t.dataset.s;
    if (k === 'metric') { if (e.type !== 'change') return; d.slots[i] = { metric: t.value }; redrawEditor(); return; }
    if (k === 'label') s.label = t.value === '' ? undefined : t.value;
    else if (k === 'unit') s.unit = t.value.trim() || undefined;
    else if (k === 'decimals') s.decimals = t.value === '' ? undefined : +t.value;
    else if (k === 'icon') { s.icon = t.value || undefined; const p = t.parentElement.querySelector('.wicon-prev'); if (p) p.innerHTML = svg(s.icon || st.ctx.defs().get(s.metric)?.icon || 'info'); }
    else if (k === 'max') s.max = t.value === '' ? undefined : clamp(parseFloat(t.value), -1e15, 1e15);
    else if (k === 'colors') {
      if (e.type !== 'change') return;
      const def = st.ctx.defs().get(s.metric);
      if (t.value === 'default') delete s.colors;
      else if (t.value === 'off') s.colors = null;
      else s.colors = { ...(defaultColors(def) || { warn: 60, crit: 80 }) };
      redrawEditor(); return;
    } else if (k === 'warn' || k === 'crit') { s.colors = { ...(s.colors || {}), [k]: parseFloat(t.value) }; }
    redrawPreview();
  }
}
