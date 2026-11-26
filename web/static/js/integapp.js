// Janela Integrações (0.39.0, decisão 74): os apps de onde o BrunOS tira dados e com que ele conversa.
// Antes era uma parte da janela Widgets; virou uma janela do sistema porque as Integrações vão além dos
// widgets (pasta do Nextcloud no Arquivos, escudo do AdGuard, Avisos prontos, Ações). Três telas:
// Conectadas (as suas, com o que cada uma libera), Apps compatíveis (todos os Tipos, com os que estão
// instalados no Runtipi em destaque) e o formulário de uma Integração. Carregada só quando abre.
import { api } from './api.js';
import { $, $$, esc, svg, toast, toastError, confirmDialog, timeAgo } from './ui.js';
import { kindIcon, kindModelCount, fmtText } from './widgets.js';

const INTERVALS = [[10, '10 segundos'], [15, '15 segundos'], [30, '30 segundos'], [60, '1 minuto'], [120, '2 minutos'], [300, '5 minutos'], [600, '10 minutos'], [900, '15 minutos'], [1800, '30 minutos'], [3600, '1 hora']];
const ACTIONS_LABEL = { adguard: 'Pausar a proteção', transmission: 'Pausar e retomar torrents' };
const sw = (on_, attrs = '') => `<button type="button" class="switch ${on_ ? 'on' : ''}" role="switch" aria-checked="${!!on_}" ${attrs}></button>`;

let st = null; // { body, ctx, view, integ, testResult, sugg, places }

/** O que um Tipo (ou uma Integração dele) libera no BrunOS: [[ícone, texto]]. */
export function unlocks(k, it) {
  const out = [], n = kindModelCount(k.id);
  if (n) out.push(['grid', `${n} ${n === 1 ? 'modelo' : 'modelos'} de widget`]);
  if (k.id === 'adguard') out.push(['shield', 'Escudo na barra do topo']);
  if (k.id === 'nextcloud') out.push(['folder', it ? (it.hasSecret2 ? 'Pasta no Arquivos' : 'Pasta no Arquivos (falta a senha de app)') : 'Pasta no Arquivos (com senha de app)']);
  if (k.id === 'plex') out.push(['play', 'Abrir vídeos no Plex']);
  if (k.actions?.length) out.push(['bolt', ACTIONS_LABEL[k.id] || `${k.actions.length} ações`]);
  if (k.alerts?.length) out.push(['bell', `${k.alerts.length} ${k.alerts.length === 1 ? 'aviso' : 'avisos'} no sino`]);
  return out;
}
const chips = (list) => (list.length ? `<span class="ichips">${list.map(([i, t]) => `<span class="ichip">${svg(i)}${esc(t)}</span>`).join('')}</span>` : '');

export function mountIntegrations(body, opts = {}) {
  st = { body, ctx: opts.ctx, view: 'list', sugg: [] };
  body.innerHTML = `<div class="wapp iapp">
    <h2 class="sheet-title">Integrações</h2>
    <div class="wapp-grid">
      <nav class="wapp-nav">
        <button data-view="list">${svg('link')}<span>Conectadas</span><small data-count="list"></small></button>
        <button data-view="catalog">${svg('plus')}<span>Apps compatíveis</span><small data-count="catalog"></small></button>
        <p class="wapp-tip">Cada integração liga o BrunOS a um app seu: widgets com os dados dele, avisos no sino e, em alguns, ações como pausar. A senha fica cifrada no servidor.</p>
      </nav>
      <section class="wapp-main"></section>
    </div></div>`;
  body.addEventListener('click', onClick);
  body.addEventListener('input', onInput);
  body.addEventListener('change', onInput);
  loadSugg();
  focusIntegrations(opts);
  return () => { body.removeEventListener('click', onClick); st = null; };
}

// Pedido vindo de fora (menu do app, Widgets, sino): abre uma Integração, um Tipo novo ou a lista
export function focusIntegrations(opts = {}) {
  if (!st) return;
  if (opts.edit && st.ctx.integrations.some((x) => x.id === opts.edit)) openIntegration(opts.edit);
  else if (opts.kind) openIntegration(null, st.sugg.find((s) => s.kind === opts.kind && (!opts.app || s.app === opts.app)) || { kind: opts.kind });
  else show(opts.view || 'list');
}

async function loadSugg() {
  try {
    st.sugg = await api.integrations.suggest();
    if (st && (st.view === 'list' || st.view === 'catalog')) show(st.view);
  } catch { /* sem sugestões */ }
}
// sugestões do Runtipi que ainda não viraram Integração
const pending = () => (st?.sugg || []).filter((s) => !st.ctx.integrations.some((it) => it.kind === s.kind && (it.url === s.url || !s.url)));

const main = () => $('.wapp-main', st.body);
function show(view) {
  st.view = view;
  $$('.wapp-nav [data-view]', st.body).forEach((b) => b.classList.toggle('on', b.dataset.view === view || (view === 'integration' && b.dataset.view === (st.integ?.id ? 'list' : 'catalog'))));
  $('[data-count="list"]', st.body).textContent = st.ctx.integrations.length || '';
  $('[data-count="catalog"]', st.body).textContent = st.ctx.kinds.length || '';
  ({ list: drawList, catalog: drawCatalog, integration: drawIntegration })[view]?.();
  main().scrollTop = 0;
}

/* ── Conectadas ───────────────────────────────────────────────── */
function drawList() {
  const list = st.ctx.integrations, kinds = st.ctx.kinds, metrics = st.ctx.metrics, sugg = pending();
  main().innerHTML = `<div class="wapp-head"><div><h3>Conectadas</h3><p>Os seus apps ligados ao BrunOS e o que cada um libera. Toque para editar.</p></div>
    <span class="f-spacer"></span><button class="pill-btn" data-act="tuya-assist">${svg('plug')}Assistente de tomadas</button><button class="pill-btn primary" data-view="catalog">${svg('plus')}Conectar app</button></div>
    ${sugg.length ? `<div class="wapp-sec">Instalados no Runtipi e ainda não conectados</div><div class="winteg-list">${sugg.map((s) => `<div class="winteg sugg"><span class="winteg-ico">${kindIcon(s.kind)}</span><span class="winteg-txt"><b>${esc(s.name)}</b><small>${esc(s.url)}</small>${chips(unlocks(kinds.find((k) => k.id === s.kind) || { id: s.kind }))}</span><button class="pill-btn primary" data-sugg="${esc(s.kind)}|${esc(s.app)}">${svg('plus')}Conectar</button></div>`).join('')}</div>` : ''}
    ${list.length ? `<div class="wapp-sec">Suas integrações</div><div class="icards">${list.map((it) => {
      const k = kinds.find((x) => x.id === it.kind) || { id: it.kind }, v = metrics[it.id];
      const status = v?.err ? `<span class="wst bad">${svg('alert')}${esc(v.err)}</span>` : v?.at ? `<span class="wst ok">atualizado ${esc(timeAgo(v.at))}</span>` : '<span class="wst">busca quando algum widget usa e a tela está aberta</span>';
      const nc = it.kind === 'nextcloud' && st.ctx.roots().some((r) => r.id === `nc-${it.id}`);
      return `<div class="icard" data-integ="${esc(it.id)}" role="button" tabindex="0">
        <div class="icard-top"><span class="winteg-ico">${kindIcon(it.kind)}</span><span class="winteg-txt"><b>${esc(it.name)}</b><small>${esc(it.url || it.options?.city || k.name || '')}</small></span><span class="chev">${svg('chevR')}</span></div>
        ${status}${chips(unlocks(k, it))}
        <div class="icard-acts">${kindModelCount(it.kind) ? `<button class="pill-btn sm" data-iwidget="${esc(it.id)}">${svg('grid')}Criar widget</button>` : ''}${nc ? `<button class="pill-btn sm" data-ifiles="${esc(it.id)}">${svg('folder')}Abrir no Arquivos</button>` : ''}</div>
      </div>`;
    }).join('')}</div>` : `<div class="wapp-empty">${svg('link')}<p>Nenhum app conectado ainda.</p><button class="pill-btn primary" data-view="catalog">${svg('plus')}Ver os apps compatíveis</button></div>`}`;
}

/* ── Apps compatíveis ─────────────────────────────────────────── */
function drawCatalog() {
  const kinds = st.ctx.kinds, list = st.ctx.integrations, inst = new Set((st.sugg || []).map((s) => s.kind));
  const sorted = [...kinds].sort((a, b) => (inst.has(b.id) - inst.has(a.id)) || a.name.localeCompare(b.name, 'pt-BR'));
  main().innerHTML = `<div class="wapp-head"><div><h3>Apps compatíveis</h3><p>Apps que o BrunOS sabe ler. Os que estão instalados no seu Runtipi aparecem primeiro, já com o endereço preenchido.</p></div></div>
    <div class="icards">${sorted.map((k) => {
      const n = list.filter((it) => it.kind === k.id).length;
      return `<div class="icard" data-kind="${esc(k.id)}" role="button" tabindex="0">
        <div class="icard-top"><span class="winteg-ico">${kindIcon(k.id)}</span><span class="winteg-txt"><b>${esc(k.name)}</b><small class="wrap">${esc(k.desc)}</small></span></div>
        <span class="icard-tags">${inst.has(k.id) ? `<span class="itag ok">${svg('check')}Instalado no Runtipi</span>` : ''}${n ? `<span class="itag">${n === 1 ? 'Já conectado' : `${n} conectados`}</span>` : ''}</span>
        ${chips(unlocks(k))}
      </div>`;
    }).join('')}</div>
    <p class="hint">Quase todos só leem dados. O AdGuard e o Transmission também aceitam pausar, e o Nextcloud mexe nos seus arquivos se você der uma senha de app para isso.</p>`;
}

/* ── Formulário de uma Integração ─────────────────────────────── */
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
  for (const a of k.alerts || []) st.integ.alerts[a.id] ||= { on: false, value: a.default };
}
// Clima: procura a cidade pelo nome (o servidor pergunta ao Open-Meteo) e mostra a lista para escolher
let cityTimer = 0, citySeq = 0;
function searchCity(input) {
  clearTimeout(cityTimer);
  const box = input.closest('label').querySelector('.wcity-list');
  const q = input.value.trim();
  if (q.length < 2) { box.hidden = true; return; }
  cityTimer = setTimeout(async () => {
    const seq = ++citySeq;
    box.hidden = false; box.innerHTML = '<span class="hint">procurando…</span>';
    try {
      const list = await api.geocode(q);
      if (seq !== citySeq || !st) return;
      st.places = list;
      box.innerHTML = list.length ? list.map((p, i) => `<button type="button" class="wcity-opt" data-place="${i}">${svg('home')}<b>${esc(p.name)}</b><small>${esc([p.region, p.country].filter(Boolean).join(' · '))}</small></button>`).join('') : '<span class="hint">Nenhuma cidade com esse nome.</span>';
    } catch (e) { if (seq === citySeq) box.innerHTML = `<span class="hint">${esc(e.message)}</span>`; }
  }, 350);
}
function drawIntegration() {
  const g = st.integ, k = st.ctx.kinds.find((x) => x.id === g.kind);
  const back = g.id ? 'list' : 'catalog';
  if (!k) { show('catalog'); return; }
  const field = (f) => {
    if (f.type === 'hidden') return '';
    if (f.type === 'city') { // Clima: procura a cidade pelo nome e guarda nome + coordenadas
      const has = g.options?.lat && g.options?.lon;
      return `<label>${esc(f.label)}<span class="wcity"><input class="text-input" data-city value="${esc(g.options?.city || '')}" placeholder="${esc(f.placeholder || '')}" autocomplete="off">${has ? `<small class="wcity-ok">${svg('check')}${esc((+g.options.lat).toFixed(2))}, ${esc((+g.options.lon).toFixed(2))}</small>` : ''}</span>
        <span class="wcity-list" hidden></span>${f.help ? `<small class="hint">${esc(f.help)}</small>` : ''}</label>`;
    }
    // 0.37.0: segunda credencial (senha de app dos arquivos do Nextcloud), guardada cifrada como a primeira
    if (f.id === 'secret2') return `<label>${esc(f.label)}<input class="text-input" type="password" autocomplete="new-password" data-g="secret2" value="${esc(g.secret2 || '')}" placeholder="${g.hasSecret2 ? '•••••••• guardada (deixe vazio para manter)' : ''}">${f.help ? `<small class="hint">${esc(f.help)}</small>` : ''}</label>`;
    if (f.id !== 'url' && f.id !== 'user' && f.id !== 'secret') { // campo extra (ex.: álbum do Immich)
      return `<label>${esc(f.label)}<input class="text-input" data-gopt="${esc(f.id)}" value="${esc(g.options?.[f.id] || '')}" placeholder="${esc(f.placeholder || '')}">${f.help ? `<small class="hint">${esc(f.help)}</small>` : ''}</label>`;
    }
    if (f.id === 'secret') return `<label>${esc(f.label)}${f.required ? '' : ' <small>(opcional)</small>'}<input class="text-input" type="password" autocomplete="new-password" data-g="secret" value="${esc(g.secret)}" placeholder="${g.hasSecret ? '•••••••• guardado (deixe vazio para manter)' : ''}">${f.help ? `<small class="hint">${esc(f.help)}</small>` : ''}</label>`;
    return `<label>${esc(f.label)}<input class="text-input" data-g="${f.id}" value="${esc(g[f.id] || '')}" placeholder="${esc(f.placeholder || '')}" ${f.id === 'url' ? 'inputmode="url"' : ''}>${f.help ? `<small class="hint">${esc(f.help)}</small>` : ''}</label>`;
  };
  const defs = new Map((k.metrics || []).map((m) => [m.id, m]));
  const tr = st.testResult, lib = unlocks(k, g.id ? g : null);
  main().innerHTML = `<div class="wapp-head"><span class="winteg-ico big">${kindIcon(k.id)}</span><div><h3>${g.id ? esc(g.name) : `Conectar ${esc(k.name)}`}</h3><p>${esc(k.desc)}</p></div><span class="f-spacer"></span><button class="pill-btn" data-view="${back}">Voltar</button></div>
    ${lib.length ? `<div class="ilib"><span class="wed-lbl">O que libera</span>${chips(lib)}${g.id && kindModelCount(k.id) ? `<button type="button" class="pill-btn sm" data-iwidget="${esc(g.id)}">${svg('grid')}Criar widget</button>` : ''}</div>` : ''}
    <form class="form-grid winteg-form" onsubmit="return false">
      <label>Nome<input class="text-input" data-g="name" value="${esc(g.name)}" maxlength="60"><small class="hint">Aparece nas métricas, ex.: “${esc(g.name || k.name)} › ${esc(k.metrics?.[0]?.name || '')}”.</small></label>
      ${k.id === 'tuya' ? `<div class="wapp-note">${svg('plug')}<span>Mais fácil: o <b>Assistente de tomadas</b> acha o IP, o ID, a versão e a chave local sozinho, pela conta do app Smart Life.</span><button type="button" class="pill-btn sm" data-act="tuya-assist">Abrir</button></div>`
        : k.note ? `<div class="wapp-note">${svg('info')}<span>${esc(k.note)}</span></div>` : ''}
      ${k.id === 'tailscale' ? `<div class="wts-cmds"><button type="button" class="pill-btn" data-act="ts-cmds">${svg('terminal')}Mostrar os comandos de atualização</button><pre class="pw-cmds" hidden tabindex="0"></pre></div>` : ''}
      ${(k.fields || []).map(field).join('')}
      <label>Buscar a cada<select class="text-input" data-g="interval">${INTERVALS.map(([s, l]) => `<option value="${s}" ${g.interval === s ? 'selected' : ''}>${l}${s === k.interval ? ' (recomendado)' : ''}</option>`).join('')}</select>
        <small class="hint">Só enquanto alguém está com o BrunOS aberto e algum widget usa esta integração. Avisos ligados fazem uma busca a cada 5 minutos mesmo com a tela fechada.</small></label>
      ${(k.alerts || []).length ? `<div class="wed-lbl">Avisos no sino</div>
      <div class="walerts">${k.alerts.map((a) => { const s = g.alerts[a.id] || { on: false, value: a.default }; return `<div class="walert"><span class="walert-txt"><b>${esc(a.name)}</b><small>${esc(a.desc || '')}</small></span>
        ${a.fixed ? '' : `<span class="walert-val"><input class="text-input sm" type="number" step="any" data-alert-val="${a.id}" value="${esc(s.value || a.default)}"><small>${esc(a.unit || '')}</small></span>`}${sw(s.on, `data-alert="${a.id}"`)}</div>`; }).join('')}</div>` : ''}
      ${tr ? `<div class="wtest ${tr.ok ? 'ok' : 'bad'}">${tr.ok ? `${svg('check')}<div><b>Funcionou!</b><small>${Object.entries(tr.values || {}).filter(([id]) => defs.get(id) && ['num', 'text'].includes(defs.get(id).kind)).slice(0, 5).map(([id, v]) => `${esc(defs.get(id).name)}: ${esc(fmtText(v, defs.get(id), {}))}`).join(' · ')}</small></div>` : `${svg('alert')}<div><b>Não funcionou</b><small>${esc(tr.error)}</small></div>`}</div>` : ''}
      <div class="dlg-actions">${g.id ? `<button type="button" class="pill-btn danger" data-act="del-integ" style="margin-right:auto">${svg('trash')}Apagar</button>` : ''}
        <button type="button" class="pill-btn" data-act="test-integ">${svg('refresh')}Testar</button><button type="button" class="pill-btn primary" data-act="save-integ">${svg('check')}Salvar</button></div>
    </form>`;
}
function integBody() {
  const g = st.integ;
  const b = { kind: g.kind, name: g.name, url: g.url, user: g.user, interval: +g.interval, alerts: g.alerts, options: g.options || {} };
  if (g.secret) b.secret = g.secret;
  if (g.secret2) b.secret2 = g.secret2;
  return b;
}
async function testInteg(btn) {
  btn.disabled = true; btn.classList.add('busy');
  try { st.testResult = await api.integrations.test({ ...integBody(), ...(st.integ.id ? { id: st.integ.id } : {}) }); }
  catch (e) { st.testResult = { ok: false, error: e.message }; }
  if (st) drawIntegration();
}
async function saveInteg(btn) {
  btn.disabled = true;
  try {
    const g = st.integ;
    if (g.id) await api.integrations.update(g.id, integBody()); else await api.integrations.add(integBody());
    await st.ctx.reloadIntegrations();
    toast('Integração salva', g.name);
    if (st) show('list');
  } catch (e) { btn.disabled = false; toastError(e, 'Não foi possível salvar'); }
}
async function deleteInteg() {
  const g = st.integ;
  if (!(await confirmDialog(`Apagar ${g.name}?`, 'Os widgets que usam esta integração vão ficar sem dados (você pode editá-los depois). A senha ou o token guardado é apagado.', 'Apagar', { danger: true }))) return;
  try { await api.integrations.del(g.id); await st.ctx.reloadIntegrations(); toast('Integração apagada', g.name); if (st) show('list'); }
  catch (e) { toastError(e, 'Não foi possível apagar'); }
}

/* ── Eventos ───────────────────────────────────────────────────── */
async function onClick(e) {
  if (!st) return;
  const q = (s) => e.target.closest(s);
  let el;
  if ((el = q('[data-iwidget]'))) { st.ctx.openWidgets({ add: st.ctx.currentPage(), modelTab: el.dataset.iwidget }); return; }
  if ((el = q('[data-ifiles]'))) { st.ctx.openFiles(`nc-${el.dataset.ifiles}`); return; }
  if ((el = q('[data-view]'))) { show(el.dataset.view); return; }
  if ((el = q('[data-sugg]'))) { const [kind, app] = el.dataset.sugg.split('|'); openIntegration(null, st.sugg.find((s) => s.kind === kind && s.app === app)); return; }
  if ((el = q('[data-integ]')) && !q('button, a, input')) { openIntegration(el.dataset.integ); return; }
  if ((el = q('[data-kind]'))) { openIntegration(null, st.sugg.find((s) => s.kind === el.dataset.kind && !st.ctx.integrations.some((it) => it.kind === s.kind && it.url === s.url)) || { kind: el.dataset.kind }); return; }
  if ((el = q('[data-place]'))) {
    const p = st.places?.[+el.dataset.place]; if (!p) return;
    const name = [p.name, p.region].filter(Boolean).join(', ');
    st.integ.options = { ...(st.integ.options || {}), city: name, lat: String(p.lat), lon: String(p.lon) };
    if (!st.integ.id && (!st.integ.name || st.integ.name === 'Clima')) st.integ.name = `Clima de ${p.name}`;
    drawIntegration(); return;
  }
  if ((el = q('[data-alert]'))) { const id = el.dataset.alert; const a = st.integ.alerts[id] ||= { on: false }; a.on = !a.on; el.classList.toggle('on', a.on); el.setAttribute('aria-checked', a.on); return; }
  if ((el = q('[data-act]'))) {
    const a = el.dataset.act;
    if (a === 'tuya-assist') { // 0.31.0: carrega sob demanda, como as outras janelas
      const { tuyaAssistant } = await import('./tuya.js');
      tuyaAssistant({ onDone: async () => { await st?.ctx.reloadIntegrations(); if (st?.view === 'list') show('list'); } });
    } else if (a === 'ts-cmds') {
      const pre = el.parentElement.querySelector('pre');
      try { pre.textContent = (await api.power.setup()).updateCommands; pre.hidden = false; el.hidden = true; }
      catch (x) { toastError(x, 'Não deu para gerar os comandos'); }
    } else if (a === 'test-integ') testInteg(el);
    else if (a === 'save-integ') saveInteg(el);
    else if (a === 'del-integ') deleteInteg();
  }
}
function onInput(e) {
  if (!st || st.view !== 'integration' || !st.integ) return;
  const t = e.target;
  if (t.dataset.city != null && e.type === 'input') { // digitou outra cidade: precisa escolher na lista de novo
    st.integ.options = { ...(st.integ.options || {}), city: t.value, lat: '', lon: '' };
    t.parentElement.querySelector('.wcity-ok')?.remove();
    st.testResult = null; searchCity(t);
    return;
  }
  if (t.dataset.g) { st.integ[t.dataset.g] = t.dataset.g === 'interval' ? +t.value : t.value; st.testResult = null; }
  if (t.dataset.gopt) { st.integ.options = { ...(st.integ.options || {}), [t.dataset.gopt]: t.value }; st.testResult = null; }
  if (t.dataset.alertVal) { const a = st.integ.alerts[t.dataset.alertVal] ||= { on: false }; a.value = parseFloat(t.value) || 0; }
}
// teclado nos cartões (são div com role=button)
document.addEventListener('keydown', (e) => {
  if (!st || (e.key !== 'Enter' && e.key !== ' ')) return;
  const card = e.target.closest?.('.iapp .icard');
  if (card && e.target === card) { e.preventDefault(); card.click(); }
});
