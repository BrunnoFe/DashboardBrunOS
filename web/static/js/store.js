// Janela Loja (0.18.0): os apps das lojas do Runtipi mostrados pelo próprio BrunOS (categorias, busca,
// página do app), com o que está instalado marcado. Parar, iniciar, desinstalar e mexer nas lojas vão
// pelo script do servidor (runtipi-cli); instalar abre a tela do Runtipi, que é quem tem o formulário.
// O desenho segue a largura da JANELA (container query), não a da tela: janela estreita e celular usam
// o mesmo arranjo. Carregado só quando a Loja abre (este arquivo e o css/store.css).
import { api } from './api.js';
import { $, esc, svg, toast, toastError, confirmDialog, openDialog, debounce, timeAgo } from './ui.js';

export const STORE_WIN = 'sys:store';
const CHUNK = 90; // linhas desenhadas por vez (o resto entra conforme a rolagem)
const CATS = {
  utilities: ['Utilitários', 'sliders'], network: ['Rede', 'network'], media: ['Mídia', 'film'], development: ['Desenvolvimento', 'code'],
  automation: ['Automação', 'bolt'], social: ['Social', 'user'], data: ['Dados e arquivos', 'folder'], security: ['Segurança', 'shield'],
  photography: ['Fotos', 'image'], books: ['Livros', 'doc'], music: ['Música', 'music'], finance: ['Finanças', 'chart'],
  gaming: ['Jogos', 'play'], ai: ['IA', 'sparkles'],
};
const catLabel = (c) => CATS[c]?.[0] || c.charAt(0).toUpperCase() + c.slice(1);
const norm = (s) => String(s || '').normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase();
const hue = (s) => { let h = 0; for (const c of String(s)) h = (h * 31 + c.charCodeAt(0)) % 360; return h; };

let S = null; // estado da janela aberta (uma só)
let cssReady = null;
function loadCss() {
  return (cssReady ||= new Promise((res) => {
    const l = document.createElement('link');
    l.rel = 'stylesheet'; l.href = 'css/store.css'; l.onload = l.onerror = res;
    document.head.append(l);
  }));
}

/** Janela para o gerenciador (windows.js).
 * ctx = { runtipiBase(), installed(urn) → app da tela inicial, openApp(urn), appDetails(urn), showUpdates(),
 *         reloadApps(), openPanel(url, title, frame), favs(), setFavs(lista), iconHTML } */
export function storeWindow(ctx, { originEl, urn } = {}) {
  return {
    id: STORE_WIN, title: 'Loja', icon: ctx.iconHTML, originEl,
    tools: [{ icon: 'refresh', label: 'Atualizar as lojas (baixa a lista nova de apps)', run: () => refreshStores() }],
    mount(body) {
      S = { ctx, body, cat: null, script: {}, self: '', panelFrame: '', tab: 'discover', catKey: '', store: '', q: '', view: null, detail: null,
        busy: '', watch: null, watchT: 0, io: null, loadedAt: 0, tstate: {}, tT: 0,
        inst: new Set((window.brunosInstalls || []).map((x) => x.urn)) };
      // 0.21.2: instalação acompanhada em segundo plano (storeforms.js): "Instalando…" no lugar de Instalar
      const onInst = (e) => { if (!S) return; S.inst = new Set(e.detail.list.map((x) => x.urn)); if (S.cat) { drawList(); if (S.view) drawDetail(); } };
      addEventListener('brunos:installs', onInst);
      body.innerHTML = `<div class="st"><div class="st-loading"><span class="upd-spin"></span>Abrindo a Loja…</div></div>`;
      const onFocus = () => { if (S && Date.now() - S.loadedAt > 30000) reload(); };
      addEventListener('focus', onFocus);
      loadCss().then(() => S && reload(true)).then(() => { if (S && urn) openDetail(urn); });
      return () => { removeEventListener('focus', onFocus); removeEventListener('brunos:installs', onInst); clearInterval(S?.watchT); clearTimeout(S?.tT); S?.io?.disconnect(); S = null; };
    },
  };
}
/** Com a Loja aberta: mostra a página de um app. */
export function showStoreApp(urn) { if (S?.cat) openDetail(urn); }

async function reload(first = false) {
  if (!S) return;
  try {
    const r = await api.store.get();
    if (!S) return;
    apply(r);
    if (first) shell();
    draw();
  } catch (err) {
    if (!S) return;
    if (first) $('.st', S.body).innerHTML = `<div class="empty-note big">${svg('bag')}<b>Não deu para abrir a Loja</b><span>${esc(err.message || String(err))}</span><button class="pill-btn" data-st="retry">Tentar de novo</button></div>`;
    $('[data-st="retry"]', S.body)?.addEventListener('click', () => reload(true));
  }
}
function apply(r) {
  S.cat = r.catalog; S.script = r.script || {}; S.self = r.self || ''; S.panelFrame = r.panelFrame || ''; S.loadedAt = Date.now();
  S.suggested = r.suggested || [];
}
const app = (urn) => S.cat.apps.find((a) => a.urn === urn);
// A loja oficial do Runtipi aparece com o nome dela (a pasta se chama "migrated" desde o Runtipi 4); as
// lojas do Garimpo (0.19.0), com o nome da loja de origem que vem no garimpo.json ("CasaOS").
const official = (s) => /github\.com\/runtipi\/runtipi-appstore$/i.test(s?.url || '');
const storeOf = (slug) => S.cat.stores.find((s) => s.slug === slug);
const storeName = (slug) => { const s = storeOf(slug); return official(s) ? 'Runtipi' : s?.name || slug; };
const via = (slug) => storeOf(slug)?.badge || ''; // selo dos apps convertidos: "via CasaOS"
const isFav = (a) => S.ctx.favs().includes(a.urn);

function shell() {
  const root = $('.st', S.body);
  root.innerHTML = `<header class="st-top">
      <div class="tabs st-tabs" role="tablist"></div>
      <select class="st-store" aria-label="Loja" hidden></select>
      <label class="search-field st-search">${svg('search')}<input type="search" placeholder="Procurar apps" aria-label="Procurar apps" autocomplete="off"></label>
      <button class="pill-btn sm" data-st="stores">${svg('bag')}Lojas</button>
    </header>
    <div class="st-main"><nav class="st-cats" aria-label="Categorias"></nav><div class="st-content"></div></div>
    <div class="st-detail" hidden></div>`;
  root.addEventListener('click', onClick);
  $('.st-search input', root).addEventListener('input', debounce((e) => { S.q = e.target.value.trim(); drawCats(); drawList(); }, 140));
  $('.st-store', root).addEventListener('change', (e) => { S.store = e.target.value; S.catKey = ''; drawCats(); drawList(); });
  // logo que não carrega vira a letra do app
  root.addEventListener('error', (e) => {
    const box = e.target.tagName === 'IMG' && e.target.closest('.st-logo');
    if (box) { box.classList.remove('pad'); box.innerHTML = `<span class="st-letter">${esc(box.dataset.l || '?')}</span>`; }
  }, true);
}

/* ── Filtros ───────────────────────────────────────────────────── */
function scoped() { // a aba e a loja escolhidas (antes da categoria e da busca)
  let l = S.cat.apps;
  if (S.tab === 'installed') l = l.filter((a) => a.installed);
  else if (S.tab === 'updates') l = l.filter((a) => a.update);
  else l = l.filter((a) => !a.local);
  if (S.store) l = l.filter((a) => a.store === S.store);
  return l;
}
function filtered() {
  let l = scoped();
  if (S.catKey === '@fav') l = l.filter(isFav);
  else if (S.catKey === '@priv') l = l.filter((a) => a.priv);
  else if (S.catKey) l = l.filter((a) => a.categories.includes(S.catKey));
  const q = norm(S.q);
  if (q) l = l.filter((a) => norm(`${a.name} ${a.id} ${a.shortDesc} ${a.categories.map(catLabel).join(' ')}`).includes(q));
  return l;
}

/* ── Desenho ───────────────────────────────────────────────────── */
function draw() { drawTop(); drawCats(); drawList(); if (S.view) drawDetail(); }

function drawTop() {
  const root = $('.st', S.body), all = S.cat.apps;
  const n = { installed: all.filter((a) => a.installed).length, updates: all.filter((a) => a.update).length };
  $('.st-tabs', root).innerHTML = [['discover', 'Descobrir'], ['installed', `Instalados${n.installed ? ` <i>${n.installed}</i>` : ''}`], ['updates', `Atualizações${n.updates ? ` <i class="up">${n.updates}</i>` : ''}`]]
    .map(([k, t]) => `<button role="tab" data-tab="${k}" class="${S.tab === k ? 'on' : ''}" aria-selected="${S.tab === k}">${t}</button>`).join('');
  const sel = $('.st-store', root), stores = S.cat.stores;
  sel.hidden = stores.length < 2;
  if (!stores.some((s) => s.slug === S.store)) S.store = '';
  sel.innerHTML = `<option value="">Todas as lojas</option>${stores.map((s) => `<option value="${esc(s.slug)}" ${s.slug === S.store ? 'selected' : ''}>${esc(storeName(s.slug))} (${s.apps})</option>`).join('')}`;
}

function drawCats() {
  const l = scoped(), count = {};
  for (const a of l) for (const c of a.categories) if (c !== 'featured') count[c] = (count[c] || 0) + 1;
  const favs = l.filter(isFav).length, privs = l.filter((a) => a.priv).length;
  if (S.catKey && S.catKey[0] !== '@' && !count[S.catKey]) S.catKey = '';
  if ((S.catKey === '@fav' && !favs) || (S.catKey === '@priv' && !privs)) S.catKey = '';
  const item = (k, label, icon, n, cls = '') => `<button class="st-cat ${cls} ${S.catKey === k ? 'on' : ''}" data-cat="${esc(k)}">${svg(icon)}<span>${esc(label)}</span><i>${n}</i></button>`;
  // os apps que requerem privilégios também ficam juntos numa guia própria (decisão 62), no fim da lista
  $('.st-cats', S.body).innerHTML = item('', 'Todos', 'grid', l.length) + (favs ? item('@fav', 'Favoritos', 'star', favs) : '')
    + Object.keys(count).sort((a, b) => catLabel(a).localeCompare(catLabel(b), 'pt-BR')).map((c) => item(c, catLabel(c), CATS[c]?.[1] || 'box', count[c])).join('')
    + (privs ? item('@priv', 'Apps com Privilégios', 'shield', privs, 'priv') : '');
}

function logo(a, cls = '') {
  const src = api.store.logoUrl(a), l = (a.name || '?')[0].toUpperCase();
  return `<span class="st-logo ${cls} ${a.demoIcon ? 'pad' : ''}" style="--h:${hue(a.name)}" data-l="${esc(l)}">${src ? `<img src="${esc(src)}" alt="" loading="lazy" draggable="false">` : `<span class="st-letter">${esc(l)}</span>`}</span>`;
}
function badge(a) {
  if (S.inst?.has(a.urn)) return `<span class="st-badge inst"><span class="upd-spin"></span>Instalando</span>`;
  if (a.update) return `<span class="st-badge up">${svg('arrowUp')}Atualização</span>`;
  if (a.installed) return `<span class="st-badge ok">${svg('check')}Instalado</span>`;
  return `<span class="st-go">${svg('chevR')}</span>`;
}
function row(a) {
  const tags = [];
  if (via(a.store)) tags.push(`<i class="st-tag via">${esc(via(a.store))}</i>`);
  else if (S.cat.stores.length > 1 && !S.store) tags.push(`<i class="st-tag store">${esc(storeName(a.store))}</i>`);
  const c = a.categories.find((x) => x !== 'featured');
  if (c) tags.push(`<i class="st-tag">${esc(catLabel(c))}</i>`);
  if (a.local) tags.push('<i class="st-tag">fora das lojas</i>');
  if (a.priv) tags.unshift(privTag);
  return `<button class="st-app" data-urn="${esc(a.urn)}">${logo(a)}<span class="st-txt"><b>${esc(a.name)}</b><small>${esc(a.shortDesc || '')}</small><span class="st-tags">${tags.join('')}</span></span>${badge(a)}</button>`;
}
// selo dos apps cuja receita pede poderes sobre o servidor (decisão 62)
const privTag = `<i class="st-tag priv" title="A receita pede poderes sobre o servidor: veja quais na página do app">${svg('shield')}Requer privilégios</i>`;
const card = (a) => `<button class="st-card" data-urn="${esc(a.urn)}">${logo(a, 'lg')}<b>${esc(a.name)}</b><small>${esc(catLabel(a.categories.find((x) => x !== 'featured') || '') || storeName(a.store))}</small>${a.installed ? `<span class="st-dot" title="Instalado">${svg('check')}</span>` : ''}${a.priv ? `<span class="st-pdot" title="Requer privilégios">${svg('shield')}</span>` : ''}</button>`;
const shelf = (title, list) => (list.length ? `<h3 class="st-h">${esc(title)}</h3><div class="st-shelf">${list.map(card).join('')}</div>` : '');

// Um cartão por loja (e o "Todas"), como pediu o Bruno: clicar mostra só os apps daquela loja. As lojas
// sugeridas (Garimpo) aparecem no fim, com o botão de adicionar.
function storesShelf() {
  const stores = S.cat.stores, sug = S.suggested || [];
  if (stores.length < 2 && !sug.length) return '';
  const total = S.cat.apps.filter((a) => !a.local).length;
  const sub = (s) => (official(s) ? 'Loja oficial' : s.badge ? 'Convertida pelo Garimpo' : 'Loja extra');
  const one = (k, name, small, n, cls = '') => `<button class="st-scard ${cls}" data-store="${esc(k)}" style="--h:${hue(name)}"><span class="st-sicon">${svg('bag')}</span><b>${esc(name)}</b><small>${esc(small)}</small><i>${n} ${n === 1 ? 'app' : 'apps'}</i></button>`;
  return `<h3 class="st-h">Lojas</h3><div class="st-shelf st-srow-cards">${one('', 'Todas', 'Todas as lojas juntas', total, 'all')}${stores.map((s) => one(s.slug, storeName(s.slug), sub(s), s.apps)).join('')}
    ${sug.map((g) => `<button class="st-scard sug" data-sug="${esc(g.slug)}" ${S.busy ? 'disabled' : ''}><span class="st-sicon">${svg('plus')}</span><b>${esc(g.name)}</b><small>Sugerida (Garimpo)</small><i>Adicionar</i></button>`).join('')}</div>`;
}
// Faixa com o que é a loja escolhida (as do Garimpo dizem de onde vêm e a licença).
function storeInfo(s) {
  if (!s) return '';
  const host = (u) => u.replace(/^https?:\/\/(www\.)?/, '');
  const txt = official(s) ? 'A loja oficial do Runtipi.' : s.about || 'Uma loja extra adicionada ao Runtipi.';
  return `<div class="st-sinfo"><span class="st-sicon" style="--h:${hue(storeName(s.slug))}">${svg('bag')}</span><span><span class="st-sname"><b>${esc(storeName(s.slug))}</b>${s.badge ? '<i class="st-tag via">convertida pelo Garimpo</i>' : ''}</span><small>${esc(txt)}${s.origin ? ` Origem: <a href="${esc(s.origin)}" target="_blank" rel="noopener noreferrer">${esc(host(s.origin))}</a>${s.license ? ` (licença ${esc(s.license)})` : ''}.` : ''}</small></span>
    <button class="pill-btn sm" data-store="">Todas as lojas</button></div>`;
}

function drawList() {
  const box = $('.st-content', S.body);
  S.io?.disconnect(); S.io = null;
  if (!S.cat.found && !S.cat.apps.length) {
    box.innerHTML = `<div class="empty-note big">${svg('bag')}<b>Não achei as lojas do Runtipi</b><span>O BrunOS lê as lojas na pasta do Runtipi. Confira a pasta em Configurações do app no Runtipi e atualize as lojas.</span></div>`;
    return;
  }
  const list = filtered(), home = S.tab === 'discover' && !S.catKey && !S.q;
  let top = '';
  if (home) {
    const base = scoped();
    const fresh = base.filter((a) => a.created).sort((a, b) => b.created - a.created).slice(0, 12);
    const seen = new Set(fresh.map((a) => a.urn));
    top = (S.store ? storeInfo(storeOf(S.store)) : storesShelf()) + shelf('Favoritos', base.filter(isFav)) + shelf('Novos na loja', fresh)
      + shelf('Atualizados recentemente', base.filter((a) => a.updated && !seen.has(a.urn)).sort((a, b) => b.updated - a.updated).slice(0, 12));
  }
  const title = S.q ? `Resultados para “${S.q}”` : S.catKey === '@fav' ? 'Favoritos' : S.catKey === '@priv' ? 'Apps com Privilégios' : S.catKey ? catLabel(S.catKey) : S.tab === 'installed' ? 'Instalados' : S.tab === 'updates' ? 'Com atualização' : 'Todos os apps';
  const empty = S.q ? 'Nenhum app com esse nome. Tente outra palavra ou outra categoria.' : S.tab === 'updates' ? 'Todos os apps estão em dia.' : S.tab === 'installed' ? 'Nenhum app instalado ainda.' : 'Nenhum app aqui.';
  box.innerHTML = `${top}<h3 class="st-h">${esc(title)} <small>${list.length}</small>${S.tab === 'updates' && list.length ? '<button class="pill-btn sm" data-st="updates">Abrir atualizações</button>' : ''}</h3>
    ${list.length ? '<div class="st-grid"></div><div class="st-more"></div>' : `<div class="empty-note">${esc(empty)}</div>`}`;
  box.scrollTop = 0;
  if (!list.length) return;
  const grid = $('.st-grid', box), more = $('.st-more', box);
  let i = 0;
  const add = () => { grid.insertAdjacentHTML('beforeend', list.slice(i, i + CHUNK).map(row).join('')); i += CHUNK; if (i >= list.length) { S.io?.disconnect(); more.remove(); } };
  add();
  if (i < list.length && 'IntersectionObserver' in window) {
    S.io = new IntersectionObserver((es) => { if (es.some((e) => e.isIntersecting)) add(); }, { root: box, rootMargin: '600px' });
    S.io.observe(more);
  } else while (i < list.length) add();
}

/* ── Página do app ─────────────────────────────────────────────── */
async function openDetail(urn) {
  const a = app(urn); if (!a) return;
  S.view = urn; S.detail = null;
  drawDetail();
  $('.st-detail', S.body).scrollTop = 0;
  try { const d = await api.store.app(urn); if (S?.view === urn) { S.detail = d; drawDetail(); } }
  catch { if (S?.view === urn) { S.detail = { description: '', fields: [] }; drawDetail(); } }
  if (a.installed && !a.local && canForms()) checkTipi(urn);
}

// 0.21.1: enquanto o Runtipi está instalando ou atualizando o app, ligar, parar ou reconfigurar recriaria o
// container no meio do caminho (foi o que estragou o banco de um app logo depois de instalar, 17/11/2026).
// A página pergunta ao Runtipi em que pé ele está e troca os botões por um aviso até ele terminar.
const TIPI_BUSY = { installing: 'instalando', updating: 'atualizando', uninstalling: 'desinstalando', starting: 'ligando', stopping: 'parando',
  restarting: 'reiniciando', backing_up: 'fazendo backup de', restoring: 'restaurando um backup de', resetting: 'reiniciando do zero' };
async function checkTipi(urn) {
  let st;
  try { st = (await api.store.status(urn)).status; } catch { return; } // script antigo ou servidor fora: fica como está
  if (!S) return;
  const was = TIPI_BUSY[S.tstate[urn]];
  S.tstate[urn] = st;
  if (S.view === urn) drawDetail();
  clearTimeout(S.tT);
  if (TIPI_BUSY[st]) S.tT = setTimeout(() => { if (S?.view === urn) checkTipi(urn); }, 4000);
  else if (was) { try { apply(await api.store.get()); await S.ctx.reloadApps(); if (S) draw(); } catch { /* confere de novo ao voltar */ } }
}
function closeDetail() {
  S.view = null; S.detail = null;
  const root = $('.st', S.body);
  root.classList.remove('viewing');
  $('.st-detail', root).hidden = true;
}

// O que dá para fazer com o app agora (o estado ligado/parado vem dos apps da tela inicial).
function actionsHTML(a) {
  if (S.busy === a.urn) return `<span class="st-busy"><span class="upd-spin"></span>${esc(S.busyText)} Pode levar um minuto.</span>`;
  const tb = a.installed && TIPI_BUSY[S.tstate[a.urn]];
  if (tb) return `<span class="st-busy"><span class="upd-spin"></span>O Runtipi está ${tb} este app. Os botões voltam quando ele terminar (mexer agora recriaria o app no meio do caminho).</span>`;
  const dis = S.busy ? 'disabled' : '';
  if (S.inst?.has(a.urn)) return `<span class="st-busy"><span class="upd-spin"></span>Instalando… Pode fechar a Loja: o sino mostra o andamento e avisa quando terminar.</span>`;
  if (!a.installed) return `<button class="pill-btn primary" data-st="install" ${dis}>${svg('download')}Instalar</button>`;
  const inst = S.ctx.installed(a.urn), stopped = inst?.state === 'stopped', self = a.id === S.self;
  return [
    a.noGui || stopped ? '' : `<button class="pill-btn primary" data-st="open">${svg('external')}Abrir</button>`,
    a.update ? `<button class="pill-btn ${a.noGui || stopped ? 'primary' : ''}" data-st="updates">${svg('arrowUp')}Atualizar</button>` : '',
    self ? '' : `<button class="pill-btn" data-st="${stopped ? 'start' : 'stop'}" ${dis}>${svg('power')}${stopped ? 'Iniciar' : 'Parar'}</button>`,
    self || !canForms() ? '' : `<button class="pill-btn" data-st="${a.local ? 'settings' : 'reconfig'}" ${dis}>${svg('sliders')}Configurar</button>`,
    `<button class="pill-btn" data-st="info">${svg('info')}Detalhes e logs</button>`,
    self ? '' : `<button class="pill-btn danger" data-st="uninstall" ${dis}>${svg('trash')}Desinstalar</button>`,
  ].join('');
}

function drawDetail() {
  const a = app(S.view), root = $('.st', S.body), box = $('.st-detail', root);
  if (!a) { closeDetail(); return; }
  root.classList.add('viewing'); box.hidden = false;
  const d = S.detail, fav = isFav(a), keep = box.scrollTop;
  const kv = (k, v) => (v ? `<div class="kv"><span>${k}</span><span>${v}</span></div>` : '');
  const link = (u, t) => (u ? `<a href="${esc(u)}" target="_blank" rel="noopener noreferrer">${esc(t)} ${svg('external')}</a>` : '');
  const host = (u) => { try { return new URL(u).hostname.replace(/^www\./, ''); } catch { return ''; } };
  const date = (t) => (t ? new Date(t * 1000).toLocaleDateString('pt-BR', { day: 'numeric', month: 'short', year: 'numeric' }) : '');
  const old = S.script.have > 0 && S.script.have < S.script.want;
  const st = storeOf(a.store);
  const note = (a.installed && old ? `<p class="st-note">${svg('info')}Para parar, iniciar e desinstalar por aqui, o script do servidor precisa ser atualizado: abra as atualizações, ao lado do sino.</p>`
    : !a.installed && !a.local ? `<p class="st-note">${svg('info')}${canForms() ? 'Instalar abre as perguntas do app aqui mesmo; o Runtipi instala e o BrunOS acompanha até o app ligar.' : `A instalação é feita na tela do Runtipi, que abre ${S.panelFrame === 'no' ? 'em nova aba' : 'numa janela'}. Quando terminar, o app aparece aqui como instalado.`}</p>` : '')
    + (st?.badge ? `<p class="st-note via">${svg('shield')}Convertido automaticamente da loja ${esc(st.name || st.slug)} pelo Garimpo do BrunOS. Se este app não funcionar, a receita original pode precisar de ajuste.</p>` : '')
    + (d?.privileges?.length ? `<div class="st-priv"><div class="st-privh">${svg('alert')}<b>Requer privilégios</b></div><p>A receita deste app pede poderes sobre o servidor que os apps comuns não têm. Com eles, um app com defeito ou malicioso pode mexer no servidor inteiro${a.installed ? '' : ', por isso instalar pede “Entendo os riscos” e a Senha de configuração'}.</p>
      <ul>${d.privileges.map((p) => `<li>${esc(p)}</li>`).join('')}</ul></div>` : '');
  const fields = d?.fields?.length ? `<div class="card st-side"><h4>A instalação pergunta</h4>${d.fields.map((f) => `<div class="st-field"><b>${esc(f.label)}${f.required ? ' <i>obrigatório</i>' : ''}</b>${f.hint ? `<small>${esc(f.hint)}</small>` : ''}</div>`).join('')}</div>` : '';
  box.innerHTML = `<div class="st-dwrap">
    <div class="st-dbar"><button class="pill-btn sm" data-st="back">${svg('chevR', 'flip')}Loja</button>
      <button class="pill-btn sm st-fav ${fav ? 'on' : ''}" data-st="fav" aria-pressed="${fav}">${svg('star')}${fav ? 'Nos favoritos' : 'Favoritar'}</button></div>
    <div class="st-hero">${logo(a, 'xl')}
      <div class="st-htxt"><h2>${esc(a.name)}</h2><p>${esc(a.shortDesc || '')}</p>
        <div class="st-tags">${a.priv ? privTag : ''}${via(a.store) ? `<i class="st-tag via">${esc(via(a.store))}</i>` : ''}${a.installed ? `<i class="st-tag ok">${svg('check')}Instalado${a.have ? ` · ${esc(a.have)}` : ''}</i>` : ''}${a.update ? `<i class="st-tag up">Nova versão: ${esc(a.version)}</i>` : ''}${a.categories.filter((c) => c !== 'featured').map((c) => `<i class="st-tag">${esc(catLabel(c))}</i>`).join('')}${a.deprecated ? '<i class="st-tag warn">Descontinuado</i>' : ''}</div></div>
      <div class="st-acts">${actionsHTML(a)}</div></div>
    ${note}
    <div class="st-cols">
      <article class="st-md">${d ? (d.description ? md(d.description) : `<p class="st-dim">${a.local ? 'Este app foi instalado fora das lojas (criado à mão, ou a loja dele foi removida).' : 'Este app não tem descrição na loja.'}</p>`) : '<div class="st-loading"><span class="upd-spin"></span>Carregando a descrição…</div>'}</article>
      <aside><div class="card st-side">${kv('Versão', esc(a.version || '—'))}${kv('Loja', esc(storeName(a.store) || '—'))}${kv('Autor', esc(a.author || ''))}${kv('Porta', a.port ? String(a.port) : '')}
        ${kv('Funciona em', esc((a.archs || []).join(', ')))}${kv('Na loja desde', date(a.created))}${kv('Atualizado', a.updated ? esc(timeAgo(a.updated)) : '')}
        ${kv('Site', link(a.website, host(a.website)))}${kv('Código', link(a.source, host(a.source)))}${st?.origin ? kv('Receita de', link(st.origin, host(st.origin))) : ''}</div>${fields}</aside>
    </div></div>`;
  box.scrollTop = keep;
}

/* ── Markdown da descrição (só o básico; nada de HTML da loja) ─── */
function mdInline(s) {
  let t = esc(s.replace(/<[^>]*>/g, ''));
  t = t.replace(/!\[[^\]]*\]\([^)]*\)/g, ''); // imagens de fora não são carregadas
  t = t.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>');
  t = t.replace(/`([^`]+)`/g, '<code>$1</code>').replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>').replace(/__([^_]+)__/g, '<b>$1</b>').replace(/(^|[\s(])\*([^*\s][^*]*)\*/g, '$1<i>$2</i>');
  return t;
}
export function md(src) {
  const out = [];
  let para = [], list = null, code = null;
  const flushP = () => { if (para.length) { const t = mdInline(para.join(' ')).trim(); if (t) out.push(`<p>${t}</p>`); para = []; } };
  const flushL = () => { if (list) { out.push(`<${list.t}>${list.items.map((i) => `<li>${mdInline(i)}</li>`).join('')}</${list.t}>`); list = null; } };
  for (const raw of String(src).replace(/\r/g, '').split('\n').slice(0, 2000)) {
    if (code) { if (/^\s*```/.test(raw)) { out.push(`<pre>${esc(code.join('\n'))}</pre>`); code = null; } else code.push(raw); continue; }
    const ln = raw.trim();
    let m;
    if (/^```/.test(ln)) { flushP(); flushL(); code = []; }
    else if (!ln) { flushP(); flushL(); }
    else if ((m = /^(#{1,6})\s+(.*)$/.exec(ln))) { flushP(); flushL(); const h = Math.min(m[1].length + 2, 5); out.push(`<h${h}>${mdInline(m[2].replace(/#+\s*$/, ''))}</h${h}>`); }
    else if ((m = /^(?:[-*+]|(\d+)[.)])\s+(.*)$/.exec(ln))) { flushP(); const t = m[1] ? 'ol' : 'ul'; if (list && list.t !== t) flushL(); (list ||= { t, items: [] }).items.push(m[2]); }
    else if (/^([-*_])\1{2,}$/.test(ln.replace(/\s/g, '')) || /^\|?[\s:|-]+\|[\s:|-]*$/.test(ln)) { flushP(); flushL(); }
    else if (ln.startsWith('|')) { flushP(); flushL(); out.push(`<p>${mdInline(ln.replace(/^\||\|$/g, '').split('|').map((c) => c.trim()).filter(Boolean).join(' · '))}</p>`); }
    else if (list && /^\s{2,}\S/.test(raw)) list.items[list.items.length - 1] += ` ${ln}`;
    else { flushL(); para.push(ln.replace(/^>\s?/, '')); }
  }
  if (code) out.push(`<pre>${esc(code.join('\n'))}</pre>`);
  flushP(); flushL();
  return out.join('');
}

/* ── Ações ─────────────────────────────────────────────────────── */
async function onClick(e) {
  const tab = e.target.closest('[data-tab]');
  if (tab) { S.tab = tab.dataset.tab; S.catKey = ''; drawTop(); drawCats(); drawList(); return; }
  const cat = e.target.closest('[data-cat]');
  if (cat) { S.catKey = cat.dataset.cat; drawCats(); drawList(); return; }
  const store = e.target.closest('[data-store]');
  if (store) { S.store = store.dataset.store; S.catKey = ''; drawTop(); drawCats(); drawList(); return; }
  const sug = e.target.closest('[data-sug]');
  if (sug) { addSuggested(sug.dataset.sug); return; }
  const act = e.target.closest('[data-st]');
  if (act) { runAction(act.dataset.st); return; }
  const item = e.target.closest('[data-urn]');
  if (item) openDetail(item.dataset.urn);
}

async function runAction(k) {
  const a = S.view ? app(S.view) : null;
  if (k === 'back') closeDetail();
  else if (k === 'stores') storesDialog();
  else if (k === 'updates') S.ctx.showUpdates();
  else if (!a) return;
  else if (k === 'fav') { const f = S.ctx.favs(); S.ctx.setFavs(f.includes(a.urn) ? f.filter((u) => u !== a.urn) : [...f, a.urn]); drawDetail(); drawCats(); drawList(); }
  else if (k === 'open') S.ctx.openApp(a.urn);
  else if (k === 'info') S.ctx.appDetails(a.urn);
  else if (k === 'install') install(a);
  else if (k === 'reconfig') reconfig(a);
  else if (k === 'settings') appSettings(a);
  else if (k === 'start' || k === 'stop') startStop(a, k);
  else if (k === 'uninstall') uninstall(a);
}

// 0.20.0: com o script do servidor 14, instalar, editar a instalação e os Ajustes acontecem aqui mesmo
// (storeforms.js, carregado só agora); com script antigo, instalar abre a tela do Runtipi como antes.
const canForms = () => (S?.script?.have || 0) >= 14;
function formsCtx(a) {
  return {
    runtipi: () => installInRuntipi(a),
    openTerminal: S.ctx.openTerminal,
    done: async () => { try { if (S) { apply(await api.store.get()); draw(); } await S?.ctx.reloadApps(); } catch { /* a Loja confere de novo ao voltar */ } },
  };
}
function install(a) {
  if (!canForms()) { installInRuntipi(a); return; }
  import('./storeforms.js').then((m) => m.installDialog(a, formsCtx(a))).catch(() => installInRuntipi(a));
}
function reconfig(a) { import('./storeforms.js').then((m) => m.installDialog(a, formsCtx(a), { edit: true })); }
function appSettings(a) { import('./storeforms.js').then((m) => m.settingsDialog(a, formsCtx(a))); }
function installInRuntipi(a) {
  const url = `${S.ctx.runtipiBase()}/app-store/${encodeURIComponent(a.store)}/${encodeURIComponent(a.id)}`;
  S.ctx.openPanel(url, `Instalar ${a.name}`, S.panelFrame);
  // enquanto a instalação acontece no Runtipi, confere de tempos em tempos se o app já apareceu
  clearInterval(S.watchT);
  S.watch = { urn: a.urn, until: Date.now() + 20 * 60000 };
  S.watchT = setInterval(async () => {
    if (!S || !S.watch || Date.now() > S.watch.until) { clearInterval(S?.watchT); return; }
    try { apply(await api.store.get()); } catch { return; }
    if (!S) return;
    const now = app(S.watch.urn);
    if (now?.installed) {
      clearInterval(S.watchT); S.watch = null;
      toast(`${now.name} instalado`, 'Já está na tela inicial.');
      S.ctx.reloadApps(); draw();
    }
  }, 12000);
}

async function act(a, action, text, done) {
  S.busy = a.urn; S.busyText = text; drawDetail();
  try {
    const r = await api.store.appAction(a.urn, action);
    if (!S) return;
    S.cat = r.catalog; S.loadedAt = Date.now();
    toast(done);
    await S.ctx.reloadApps(); // o ligado/parado vem dos apps da tela inicial
  } catch (err) { toastError(err, 'Não deu certo'); }
  if (!S) return;
  S.busy = '';
  if (S.view && !app(S.view)) closeDetail();
  draw();
  // o Docker pode levar um instante para mostrar o estado novo dos containers: confere de novo
  setTimeout(async () => { if (!S) return; try { await S.ctx.reloadApps(); } catch { /* fica como está */ } if (S && !S.busy) drawDetailIfOpen(); }, 4000);
}
function drawDetailIfOpen() { if (S.view) drawDetail(); }
async function startStop(a, k) {
  const stop = k === 'stop';
  if (stop && !(await confirmDialog(`Parar ${a.name}?`, 'O Runtipi para o app e ele continua parado mesmo se o servidor reiniciar, até você iniciar de novo. Os dados ficam guardados.', 'Parar'))) return;
  act(a, k, stop ? `Parando ${a.name}…` : `Iniciando ${a.name}…`, `${a.name} ${stop ? 'parado' : 'iniciado'}`);
}

// Desinstalar apaga os dados do app: pede a Senha de configuração (a mesma de formatar discos), que o
// servidor confere. O diálogo fica aberto enquanto desinstala e mostra o erro (senha errada, trava).
// Antes de pedir a senha, confere no servidor se ela já foi criada e se não está bloqueada.
async function uninstall(a) {
  let info;
  try { info = await api.disks.formatInfo(); } catch (err) { toastError(err, 'Não deu para falar com o servidor'); return; }
  if (!S) return;
  if (!info.hasPassword) {
    const term = S.ctx.openTerminal;
    const d = openDialog(`<h2>${svg('key')}Falta a Senha de configuração</h2>
      <p>Desinstalar apaga os dados do app. Por isso o servidor pede a Senha de configuração: uma senha só para as ações sensíveis do servidor (formatar discos e desinstalar apps), diferente da senha do BrunOS. Ela é criada uma vez, <b>direto no servidor</b>, para que ninguém que invada o BrunOS consiga criar uma.</p>
      <p class="st-dlg-gap">No Terminal, rode o comando abaixo (o sudo pede a senha do seu usuário do servidor), escolha a senha e depois volte aqui:</p>
      <pre class="pw-cmds">sudo brunos-host senha-config</pre>
      <div class="dlg-actions"><button class="pill-btn" data-c="close">Fechar</button>${term ? `<button class="pill-btn primary" data-c="term">${svg('terminal')}Abrir no Terminal</button>` : ''}</div>`, { closeBtn: false, width: 500 });
    $('[data-c="close"]', d.el).addEventListener('click', () => d.close());
    $('[data-c="term"]', d.el)?.addEventListener('click', () => { d.close(); term({ typeCmd: 'sudo brunos-host senha-config' }); toast('Comando digitado no Terminal', 'Confira e aperte Enter.'); });
    return;
  }
  if (info.lockedUntil > Date.now() / 1000) {
    const d = openDialog(`<h2>${svg('lock')}Desinstalar está bloqueado</h2><p>A Senha de configuração foi digitada errada 5 vezes. Por segurança, formatar e desinstalar voltam a funcionar às ${esc(new Date(info.lockedUntil * 1000).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' }))}.</p>
      <div class="dlg-actions"><button class="pill-btn primary" data-c="close">Entendi</button></div>`, { closeBtn: false, width: 440 });
    $('[data-c="close"]', d.el).addEventListener('click', () => d.close());
    return;
  }
  uninstallDialog(a);
}
function uninstallDialog(a) {
  let running = false;
  const d = openDialog(`<h2>Desinstalar ${esc(a.name)}?</h2>
    <p>O Runtipi remove o app <b>e apaga os dados dele</b> (configurações, contas, banco de dados e o que estiver guardado na pasta do app). Não dá para desfazer. O que fica em outras pastas do servidor, como fotos e filmes em outros discos, continua lá.</p>
    <p class="st-dlg-gap">Se quiser guardar os dados, faça antes um backup pela página do app no Runtipi.</p>
    <form class="form-grid st-dlg-gap"><label>Senha de configuração (a mesma de formatar discos: protege as ações sensíveis do servidor)<input class="text-input st-wide" type="password" autocomplete="off" maxlength="200" autofocus></label>
      <p class="st-err" hidden></p>
      <div class="dlg-actions"><span class="st-busy" hidden><span class="upd-spin"></span>Desinstalando… pode levar um minuto.</span><button type="button" class="pill-btn" data-c="no">Cancelar</button><button type="submit" class="pill-btn danger-solid" disabled>Desinstalar</button></div></form>`,
  { closeBtn: false, width: 460, backdropClose: false, beforeClose: () => !running });
  const input = $('input', d.el), btn = $('[type="submit"]', d.el), err = $('.st-err', d.el), busy = $('.st-busy', d.el);
  input.addEventListener('input', () => { btn.disabled = !input.value || running; });
  $('[data-c="no"]', d.el).addEventListener('click', () => d.close());
  $('form', d.el).addEventListener('submit', async (e) => {
    e.preventDefault();
    if (running || !input.value || !S) return;
    running = true; btn.disabled = true; input.disabled = true; err.hidden = true; busy.hidden = false;
    S.busy = a.urn; S.busyText = `Desinstalando ${a.name}…`; drawDetail();
    let done = false;
    try {
      const r = await api.store.appAction(a.urn, 'uninstall', input.value);
      done = true;
      if (S) { S.cat = r.catalog; S.loadedAt = Date.now(); }
      toast(`${a.name} desinstalado`);
      await S?.ctx.reloadApps();
    } catch (ex) {
      if (!done) {
        const m = ex.message || String(ex);
        // o script do servidor antigo (até a versão 12) ainda chamava de Senha de formatação
        err.textContent = /ainda não existe Senha de (configuração|formatação)/.test(m) ? 'Ainda não existe Senha de configuração. Crie uma vez no Terminal do servidor com: sudo brunos-host senha-config' : m;
        err.hidden = false;
      }
    }
    running = false; busy.hidden = true; input.disabled = false; input.value = '';
    if (S) { S.busy = ''; if (S.view && !app(S.view)) closeDetail(); draw(); }
    if (done) d.close(); else input.focus();
  });
}

/* ── Lojas ─────────────────────────────────────────────────────── */
// Adiciona uma loja sugerida (as do Garimpo) com um clique, depois de explicar o que ela é.
async function addSuggested(slug, dlgDraw) {
  const g = (S?.suggested || []).find((x) => x.slug === slug);
  if (!g || S.busy) return;
  if (!(await confirmDialog(`Adicionar a loja ${g.name}?`, `${g.about} A conversão é feita pelo Garimpo, do BrunOS, e não é oficial. Endereço: ${g.url.replace(/^https:\/\//, '')}`, 'Adicionar'))) return;
  if (!S || S.busy) return;
  S.busy = '@stores'; draw(); dlgDraw?.();
  toast(`Adicionando ${g.name}…`, 'O Runtipi baixa a loja; pode levar um minuto.');
  try {
    const r = await api.store.stores('add', g.slug, g.url);
    if (!S) return;
    S.cat = r.catalog; S.suggested = r.suggested || []; S.loadedAt = Date.now();
    toast(`Loja ${g.name} adicionada`, `${S.cat.stores.find((s) => s.slug === g.slug)?.apps ?? 0} apps novos na Loja.`);
  } catch (err) { toastError(err, `Não deu para adicionar ${g.name}`); }
  if (!S) return;
  S.busy = ''; draw(); dlgDraw?.();
}

async function refreshStores(dlgDraw) {
  if (!S || S.busy) return;
  S.busy = '@stores'; dlgDraw?.();
  toast('Atualizando as lojas…', 'Baixando a lista nova de apps do Runtipi.');
  try {
    const r = await api.store.stores('refresh');
    if (!S) return;
    S.cat = r.catalog; S.loadedAt = Date.now();
    if (r.warn) toast('As lojas não foram atualizadas', r.warn, null, { error: true }); else toast('Lojas atualizadas', `${S.cat.apps.filter((a) => !a.local).length} apps disponíveis.`);
  } catch (err) { toastError(err, 'Não deu para atualizar as lojas'); }
  if (!S) return;
  S.busy = ''; draw(); dlgDraw?.();
}

function storesDialog() {
  const d = openDialog('<div class="st-stores"></div>', { width: 520 });
  const box = $('.st-stores', d.el);
  let err = '';
  const paint = () => {
    if (!S) { d.close(); return; }
    const busy = !!S.busy, keep = { name: $('[name="name"]', box)?.value || '', url: $('[name="url"]', box)?.value || '' };
    box.innerHTML = `<h2>Lojas</h2>
      <p>As lojas são repositórios com as receitas dos apps. A oficial do Runtipi já vem pronta; aqui você adiciona outras, suas ou de outras pessoas.</p>
      <div class="st-slist">${S.cat.stores.map((s) => `<div class="st-srow"><span class="mini sys">${svg('bag')}</span><span class="st-stxt"><b>${esc(storeName(s.slug))}${official(s) ? ' <i class="st-tag">oficial</i>' : s.badge ? ' <i class="st-tag via">Garimpo</i>' : ''}</b><small>${s.url ? esc(s.url.replace(/^https?:\/\//, '')) : 'endereço não informado'} · ${s.apps} ${s.apps === 1 ? 'app' : 'apps'}${s.license ? ` · licença ${esc(s.license)}` : ''}</small></span>
        ${official(s) ? '' : `<button class="pill-btn sm danger" data-rm="${esc(s.slug)}" ${busy ? 'disabled' : ''}>Remover</button>`}</div>`).join('') || '<div class="empty-note">Nenhuma loja encontrada.</div>'}</div>
      ${S.suggested?.length ? `<h4 class="st-sh">Sugeridas</h4><div class="st-slist">${S.suggested.map((g) => `<div class="st-srow"><span class="mini sys">${svg('plus')}</span><span class="st-stxt"><b>${esc(g.name)} <i class="st-tag via">Garimpo</i></b><small>${esc(g.about)}</small></span>
        <button class="pill-btn sm primary" data-sug="${esc(g.slug)}" ${busy ? 'disabled' : ''}>Adicionar</button></div>`).join('')}</div>` : ''}
      <form class="form-grid st-add"><h4>Adicionar loja</h4>
        <label>Nome (letras minúsculas, números e traço)<input class="text-input st-wide" name="name" placeholder="minha-loja" maxlength="32" autocomplete="off" value="${esc(keep.name)}"></label>
        <label>Endereço do repositório<input class="text-input st-wide" name="url" placeholder="https://github.com/usuario/loja" maxlength="200" autocomplete="off" inputmode="url" value="${esc(keep.url)}"></label>
        ${err ? `<p class="st-err">${esc(err)}</p>` : ''}
        <p class="st-warn">${svg('shield')}Só adicione lojas de quem você confia: os apps de uma loja rodam no seu servidor.</p>
        <div class="dlg-actions">${busy ? '<span class="st-busy"><span class="upd-spin"></span>Falando com o Runtipi…</span>' : ''}<button type="button" class="pill-btn" data-refresh ${busy ? 'disabled' : ''}>${svg('refresh')}Atualizar lojas</button><button type="submit" class="pill-btn primary" ${busy ? 'disabled' : ''}>Adicionar</button></div></form>`;
  };
  const run = async (action, name, url, okMsg) => {
    err = ''; S.busy = '@stores'; paint();
    try {
      const r = await api.store.stores(action, name, url);
      if (!S) return;
      S.cat = r.catalog; S.suggested = r.suggested || S.suggested; S.loadedAt = Date.now();
      toast(okMsg);
      if (action === 'add') { $('[name="name"]', box).value = ''; $('[name="url"]', box).value = ''; }
    } catch (e) { err = e.message || String(e); }
    if (!S) return;
    S.busy = ''; draw(); paint();
  };
  box.addEventListener('click', async (e) => {
    const rm = e.target.closest('[data-rm]');
    if (rm) {
      const name = rm.dataset.rm;
      if (await confirmDialog(`Remover a loja ${name}?`, 'Os apps dela saem da Loja. Apps já instalados precisam ser desinstalados antes.', 'Remover', { danger: true })) run('remove', name, '', `Loja ${name} removida`);
    } else if (e.target.closest('[data-refresh]')) refreshStores(paint);
    else if (e.target.closest('[data-sug]')) addSuggested(e.target.closest('[data-sug]').dataset.sug, paint);
  });
  box.addEventListener('submit', (e) => {
    e.preventDefault();
    const name = $('[name="name"]', box).value.trim().toLowerCase(), url = $('[name="url"]', box).value.trim().replace(/\/+$/, '').replace(/\.git$/, '');
    if (!name || !url) { err = 'Preencha o nome e o endereço.'; paint(); return; }
    run('add', name, url, `Loja ${name} adicionada`);
  });
  paint();
}
