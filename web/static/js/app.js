// BrunOS — casca da interface: login, área de trabalho, dock, widgets, configurações.
import { paintBattery, batteryDialog } from './battery.js';
import { SIZES, BRUNOS_METRICS, brunosValue, normalize, migrate, innerHTML, widgetHTML, autoClick, newId, spanStyle, sizeFor } from './widgets.js';
import { api, initApi } from './api.js';
import { refreshNotifications, onNotify, closePanel, notificationsOpen, toggleNotifications } from './notifications.js';
import { powerDialog, refreshPowerBanner, paintBanner, outagesHTML } from './power.js';
import { diskRowsHTML, driveTempHelp, smartDialog } from './disks.js';
import { internetDialog } from './internet.js';
import { initLock, lockNow, showLock, hideLock, isScreenLocked } from './lockscreen.js';
import { mountNotes, noteFocus, restoreNoteFocus, noteChanged, deleteNote, refreshNotes } from './notes.js';
import {
  $, $$, esc, html, clamp, fmt, fmtBytes, fmtRate, fmtDuration, debounce, on, emit, svg, folderSVG,
  toast, toastError, contextMenu, closeMenu, menuOpen, dialogs, openDialog, confirmDialog, promptDialog,
} from './ui.js';

/* ── Estado ────────────────────────────────────────────────────── */
const BUILTIN_WALLPAPERS = ['24', '2', '23', '19', '16', '14', '4', '1', '12', '6', '13', '10'];
const BUILTIN_ICONS = ['adguard-home', 'cloudflare', 'dashdot', 'filebrowser', 'homarr', 'immich', 'nextcloud', 'plex', 'qbittorrent', 'speedtest-tracker', 'syncthing', 'tailscale', 'tipi', 'transmission', 'vaultwarden', 'watchyourlan'];
const DEFAULTS = {
  wallpaper: 'builtin:24', wallpaperMode: 'single', wallpaperSchedule: [], accentMode: 'auto', manualHue: 209, glass: true, blur: 24, iconSize: 64,
  widgets: ['files', 'storage', 'system'], widgets2: null, appPlace: {}, showStatus: true, reduceMotion: false, diskForecast: false, appMonitor: true, netMonitor: true, appPages: true, folders: [], openIn: 'tab',
  urlMode: 'auto', serverHost: '', runtipiUrl: '', dock: [], order: [], apps: {}, links: [], favorites: [], welcomed: false,
  // 0.10.0: Bloqueio (J1) e Terminal (K1–K3)
  lockMode: 'senha', lockIdleMin: 15, termIdleMin: 15, termConnections: [], termSnippets: [], termSnippetBar: true,
  appUpdateNotify: true, // 0.11.0: aviso no sino de versão nova de app
};
// Endereço do painel do Runtipi: o que o usuário configurou ou http://<servidor>[:porta].
function runtipiBase() {
  const custom = (set().runtipiUrl || '').trim().replace(/\/+$/, '');
  if (custom) return /^https?:\/\//.test(custom) ? custom : `http://${custom}`;
  return runtipiAuto();
}
function runtipiAuto() {
  const host = set().serverHost || S.tipi.internalIp || location.hostname;
  const port = S.tipi.port && S.tipi.port !== 80 ? `:${S.tipi.port}` : '';
  return `http://${host}${port}`;
}
let termMod = null; // carregado na primeira vez que o Terminal abre (a busca usa os Comandos salvos dele)
function openTerminal(opts = {}) {
  import('./terminal.js').then((m) => {
    termMod = m;
    m.terminalWindow({ settings: set(), saveSetting: (k, v) => { set()[k] = v; save(); }, saveNow, defaultHost: S.tipi.internalIp || location.hostname, onTabs: () => renderDock(), ...opts });
  }).catch((e) => toastError(e, 'Não deu para abrir o terminal'));
}
const openPower = () => powerDialog({ openTerminal: S.terminal === false ? null : openTerminal });
function openBattery() {
  batteryDialog(() => S.snapshot?.battery, (fn) => on('stats', fn), {
    loadOutages: async () => outagesHTML((await api.power.get()).outages, 5), openPower,
  });
}
function openUrl(href) { const l = document.createElement('a'); l.href = href; l.target = '_blank'; l.rel = 'noopener'; l.click(); }
const S = {
  pub: {}, me: null, settings: structuredClone(DEFAULTS), apps: [], tipi: {}, docker: false, dockerActions: false,
  system: null, snapshot: null, history: [], roots: [], wallpapers: [], localWallpaper: null, stopStream: null,
  kinds: [], integrations: [], metrics: {}, internet: null,
};
const set = () => S.settings;
// Só salva depois de as preferências do servidor chegarem: antes disso S.settings é o padrão, e
// gravar o padrão apagaria widgets, páginas e tudo o mais (bug da 0.11.0)
const saveNow = async () => {
  if (!S.settingsLoaded) return;
  try { await api.saveSettings(S.settings); } catch (e) { toastError(e, 'Não foi possível salvar as preferências'); }
};
const save = debounce(saveNow, 400);

/* ── Tema a partir do papel de parede ──────────────────────────── */
function extractBrand(img, mode) {
  const W = 64, H = 36, c = document.createElement('canvas');
  c.width = W; c.height = H;
  const ctx = c.getContext('2d', { willReadFrequently: true });
  ctx.drawImage(img, 0, 0, W, H);
  let data;
  try { data = ctx.getImageData(0, 0, W, H).data; } catch { return null; }
  const bins = new Float64Array(36), cx = new Float64Array(36), cy = new Float64Array(36), cs = new Float64Array(36);
  for (let i = 0; i < data.length; i += 4) {
    const r = data[i] / 255, g = data[i + 1] / 255, b = data[i + 2] / 255;
    const max = Math.max(r, g, b), min = Math.min(r, g, b), l = (max + min) / 2, d = max - min;
    if (d === 0) continue;
    const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    if (l < 0.08 || l > 0.95 || s < 0.12) continue;
    let h = max === r ? (g - b) / d + (g < b ? 6 : 0) : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
    h /= 6;
    const w = mode === 'vibrant' ? s ** 4 * Math.max(0, 1 - Math.abs(l - 0.55) * 1.6) : s * s * Math.max(0, 1 - Math.abs(l - 0.5) * 1.4);
    if (w <= 0) continue;
    const k = Math.floor(h * 36) % 36, a = h * Math.PI * 2;
    bins[k] += w; cx[k] += Math.cos(a) * w; cy[k] += Math.sin(a) * w; cs[k] += s * w;
  }
  let best = -1, bestV = 0;
  for (let i = 0; i < 36; i++) { const v = bins[(i + 35) % 36] + bins[i] * 2 + bins[(i + 1) % 36]; if (v > bestV) { bestV = v; best = i; } }
  if (best < 0) return { h: 220, s: 10, l: 50 };
  let X = 0, Y = 0, Sm = 0, Wt = 0;
  for (const j of [-1, 0, 1]) { const k = (best + j + 36) % 36; X += cx[k]; Y += cy[k]; Sm += cs[k]; Wt += bins[k]; }
  return { h: Math.round((Math.atan2(Y, X) * 180 / Math.PI + 360) % 360), s: Math.round(clamp((Sm / Wt) * 135, 55, 100)), l: mode === 'vibrant' ? 50 : 45 };
}
let extracted = { auto: null, vibrant: null }, brandNow = { h: 209, s: 85, l: 45 };
function applyBrand() {
  const st = set();
  const b = st.accentMode === 'manual' ? { h: st.manualHue, s: 80, l: 48 } : (extracted[st.accentMode] || extracted.auto || brandNow);
  brandNow = b;
  const r = document.documentElement.style;
  r.setProperty('--bh', b.h); r.setProperty('--bs', b.s + '%'); r.setProperty('--bl', b.l + '%');
  $('meta[name=theme-color]')?.setAttribute('content', `hsl(${b.h} 30% 12%)`);
}
try {
  CSS.registerProperty({ name: '--bh', syntax: '<number>', inherits: true, initialValue: '209' });
  CSS.registerProperty({ name: '--bs', syntax: '<percentage>', inherits: true, initialValue: '85%' });
  CSS.registerProperty({ name: '--bl', syntax: '<percentage>', inherits: true, initialValue: '45%' });
  document.documentElement.style.transition = '--bh 900ms ease, --bs 900ms ease, --bl 900ms ease';
} catch { /* sem @property: troca instantânea */ }

/* ── Papel de parede ───────────────────────────────────────────── */
function wallpaperUrl(id, { thumb = false, pub = false } = {}) {
  if (!id) id = DEFAULTS.wallpaper;
  if (id === 'local') return S.localWallpaper;
  const [kind, name] = id.split(':');
  if (kind === 'builtin') return `assets/${thumb ? 'thumbs' : 'wallpapers'}/${name}.avif`;
  if (kind === 'upload') return pub ? api.publicWallpaperUrl() : api.wallpaperUrl(name);
  return `assets/wallpapers/24.avif`;
}
function setWallpaper(id, { instant = false, pub = false } = {}) {
  const url = wallpaperUrl(id, { pub });
  if (!url) return;
  const img = new Image();
  img.decoding = 'async';
  img.onload = () => {
    extracted = { auto: extractBrand(img, 'auto'), vibrant: extractBrand(img, 'vibrant') };
    applyBrand();
    const sharp = $('.wp-sharp'), blur = $('.wp-blur'), css = `url("${url}")`;
    blur.style.backgroundImage = css;
    if (instant || !sharp.style.backgroundImage) sharp.style.backgroundImage = css;
    else {
      const layer = html('<div class="wp-layer" style="opacity:0"></div>');
      layer.style.backgroundImage = css;
      sharp.after(layer);
      requestAnimationFrame(() => requestAnimationFrame(() => { layer.style.opacity = '1'; }));
      setTimeout(() => { sharp.style.backgroundImage = css; layer.remove(); }, 750);
    }
    document.body.classList.remove('is-booting');
  };
  img.onerror = () => { if (id !== DEFAULTS.wallpaper) setWallpaper(DEFAULTS.wallpaper, { instant }); else document.body.classList.remove('is-booting'); };
  img.src = url;
}
/* Papel de parede por horário (J2, lista livre): cada um vale da hora marcada até a próxima */
let wpsPick = null; // linha da lista escolhendo papel de parede
const toMin = (hhmm) => { const [h, m] = String(hhmm || '0:0').split(':').map(Number); return (h || 0) * 60 + (m || 0); };
function scheduledWallpaper() {
  const st = set(), list = (st.wallpaperSchedule || []).filter((e) => e && e.wp);
  if (st.wallpaperMode !== 'schedule' || !list.length) return null;
  const sorted = [...list].sort((a, b) => toMin(a.at) - toMin(b.at));
  const d = new Date(), now = d.getHours() * 60 + d.getMinutes();
  return (sorted.filter((e) => toMin(e.at) <= now).pop() || sorted[sorted.length - 1]).wp; // antes do 1º: o último de ontem
}
function applySchedule() {
  const id = scheduledWallpaper();
  if (!id || id === set().wallpaper) return;
  set().wallpaper = id; save(); // a tela de login também passa a mostrar este
  setWallpaper(id);
}
function scheduleHTML(wps) {
  const list = set().wallpaperSchedule || [];
  const thumb = (id) => esc(wallpaperUrl(id, { thumb: true }) || '');
  return `<div class="wps">${list.map((e, i) => `<div class="wps-row ${wpsPick === i ? 'picking' : ''}"><input type="time" class="text-input sm" data-wps-at="${i}" value="${esc(e.at)}" aria-label="Horário">
      <button class="wp-thumb sm ${wpsPick === i ? 'on' : ''}" data-wps-pick="${i}" style="background-image:url('${thumb(e.wp)}')" aria-label="Trocar o papel de parede deste horário"></button>
      <button class="icon-btn danger" data-wps-del="${i}" aria-label="Tirar horário">${svg('x')}</button></div>`).join('')}
    <button class="pill-btn sm" data-wps-add>${svg('plus')}Adicionar horário</button>
    ${wpsPick != null && list[wpsPick] ? `<div class="wp-thumbs wps-pick"><small class="hint">Escolha o papel de parede das ${esc(list[wpsPick].at)}:</small>${wps.map((id) => `<button class="wp-thumb ${list[wpsPick].wp === id ? 'on' : ''}" data-wps-set="${esc(id)}" style="background-image:url('${thumb(id)}')" aria-label="Papel de parede"></button>`).join('')}</div>` : ''}</div>`;
}
async function uploadWallpaper(file) {
  if (!file || !file.type.startsWith('image/')) return;
  if (api.demo) {
    const r = new FileReader();
    r.onload = () => { S.localWallpaper = r.result; set().wallpaper = 'local'; setWallpaper('local'); refreshSettings(); };
    r.readAsDataURL(file);
    return;
  }
  try {
    toast('Enviando papel de parede…');
    const { name } = await api.uploadWallpaper(file);
    S.wallpapers = await api.wallpapers();
    set().wallpaper = `upload:${name}`; save();
    setWallpaper(set().wallpaper);
    refreshSettings();
  } catch (e) { toastError(e, 'Não foi possível usar a imagem'); }
}

/* ── Apps: Runtipi + atalhos próprios, com personalizações ─────── */
function allApps() {
  const st = set();
  const list = [
    ...S.apps.map((a) => ({ ...a, id: a.urn, kind: 'runtipi' })),
    ...st.links.map((l) => ({ id: `link:${l.id}`, kind: 'link', name: l.name, url: l.url, icon: l.icon, tile: l.tile, state: 'unknown' })),
  ].map((a) => {
    const o = st.apps[a.id] || {};
    return { ...a, name: o.name || a.name, o };
  });
  const idx = new Map(st.order.map((id, i) => [id, i]));
  return list.sort((a, b) => (idx.has(a.id) ? idx.get(a.id) : 1e6) - (idx.has(b.id) ? idx.get(b.id) : 1e6) || a.name.localeCompare(b.name, 'pt-BR'));
}
const appById = (id) => allApps().find((a) => a.id === id);
const serverHost = () => set().serverHost || location.hostname || S.tipi.internalIp || 'localhost';
function appUrl(a, forceMode) {
  if (a.kind === 'link') return a.url || '';
  if (!forceMode && a.o.url) return a.o.url;
  const mode = forceMode || a.o.urlMode || set().urlMode;
  const domainUrl = a.exposedDomain ? `https://${a.exposedDomain}` : a.localDomain ? `${a.protocol === 'http' ? 'http' : 'https'}://${a.localDomain}` : '';
  const portUrl = a.port ? `http://${serverHost()}:${a.port}` : '';
  if (mode === 'port') return portUrl || domainUrl;
  if (mode === 'domain') return domainUrl || portUrl;
  return domainUrl || portUrl; // automático: domínio local quando o Runtipi expõe, senão IP:porta
}
function iconInfo(a) {
  const pref = a.o.icon || a.icon || 'auto';
  if (pref.startsWith('di:')) return { src: `https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/${encodeURIComponent(pref.slice(3))}.webp` };
  if (pref.startsWith('builtin:')) return { src: `assets/icons/${pref.slice(8)}.webp` };
  if (pref === 'letter') return {};
  if (a.hasLogo) return { src: api.logoUrl(a.urn), full: true };
  if (a.builtinIcon) return { src: `assets/icons/${a.builtinIcon}.webp` };
  return {};
}
function tileCss(a, info) {
  const t = a.o.tile || a.tile;
  if (t === 'brand') return 'var(--brand)';
  if (t) return t;
  if (!info.src) return 'linear-gradient(160deg, var(--brand-lightest), var(--brand))'; // sem ícone: inicial sobre a cor de destaque
  return info.full ? '#2a2a30' : '#ffffff';
}
function appIconHTML(a, cls = 'app-icon', extra = '') {
  const info = iconInfo(a);
  const inner = info.src
    ? `<img src="${esc(info.src)}" alt="" loading="lazy" draggable="false" class="${info.full ? 'full' : ''}" onerror="this.replaceWith(Object.assign(document.createElement('span'),{className:'initial',textContent:${esc(JSON.stringify((a.name || '?')[0]))}}))">`
    : `<span class="initial">${esc((a.name || '?')[0])}</span>`;
  return `<span class="${cls}" style="--tile:${tileCss(a, info)}">${inner}${extra}</span>`;
}
function openApp(a) {
  if (!a) return;
  const url = appUrl(a);
  if (!url) { toast(`${a.name} não tem endereço`, 'Clique com o botão direito no ícone → Editar.'); return; }
  const link = document.createElement('a');
  link.href = url; link.rel = 'noopener noreferrer';
  if (set().openIn === 'tab') link.target = '_blank';
  link.click();
}

/* ── Saudação ──────────────────────────────────────────────────── */
const displayName = () => (S.me?.displayName || S.pub.displayName || '').trim();
function greeting() {
  const h = new Date().getHours();
  const part = h < 5 ? 'Boa noite' : h < 12 ? 'Bom dia' : h < 18 ? 'Boa tarde' : 'Boa noite';
  const n = displayName().split(' ')[0];
  return n ? `${part}, ${n}.` : `${part}.`;
}
const renderGreeting = () => { $('#greeting').textContent = greeting(); };

/* ── Widgets (etapa 7; termos em CONTEXT.md) ───────────────────── */
function favorites() {
  const fav = set().favorites.filter((f) => S.roots.some((r) => r.id === f.root));
  if (fav.length) return fav;
  return S.roots.map((r) => ({ root: r.id, path: '', name: r.name }));
}
function storageTotals() {
  const disks = S.snapshot?.disks || [];
  return disks.reduce((acc, d) => ({ used: acc.used + d.used, total: acc.total + d.total }), { used: 0, total: 0 });
}
// preferência widgets2 = { v, areas: { página: [ids] }, items: { id: widget } }; a antiga `widgets` vira Modelos
function W2() {
  const st = set();
  if (!st.widgets2 || typeof st.widgets2 !== 'object' || !st.widgets2.items) { st.widgets2 = migrate(st.widgets); save(); }
  st.widgets2.areas ||= {};
  return st.widgets2;
}
const widgetById = (id) => { const it = W2().items[id]; return it ? normalize(it) : null; };
function widgetsOn(page) {
  const w = W2();
  return (w.areas[page] || []).filter((id) => w.items[id]).map((id) => [id, normalize(w.items[id])]);
}
const widgetPages = () => Object.keys(W2().areas).map(Number).filter((p) => widgetsOn(p).length).sort((a, b) => a - b);
function pageOfWidget(id) { for (const [p, ids] of Object.entries(W2().areas)) if (ids.includes(id)) return +p; return 0; }
function removeFromAreas(id) { const w = W2(); for (const p of Object.keys(w.areas)) { w.areas[p] = w.areas[p].filter((x) => x !== id); if (!w.areas[p].length) delete w.areas[p]; } }
function putWidget(id, item, page, beforeId) {
  const w = W2();
  w.items[id] = item;
  removeFromAreas(id);
  const list = (w.areas[page] ||= []);
  const at = beforeId ? list.indexOf(beforeId) : -1;
  list.splice(at < 0 ? list.length : at, 0, id);
}
function deleteWidget(id) {
  if (W2().items[id]?.format === 'notes') deleteNote(id); // o texto da nota sai do servidor junto
  removeFromAreas(id); delete W2().items[id]; save(); renderApps();
}

// Definições das Métricas: fonte BrunOS + cada Integração (nomes "Integração › Métrica")
let defsCache = null;
function metricDefs() {
  if (defsCache) return defsCache;
  const m = new Map(BRUNOS_METRICS.map((d) => [d.id, { ...d, chartable: !!d.hist }]));
  for (const it of S.integrations || []) {
    const k = (S.kinds || []).find((x) => x.id === it.kind);
    for (const d of k?.metrics || []) {
      m.set(`${it.id}.${d.id}`, { ...d, id: `${it.id}.${d.id}`, local: d.id, name: `${it.name} › ${d.name}`, source: it.name, iid: it.id, kindId: it.kind, chartable: d.kind === 'num' });
    }
  }
  return (defsCache = m);
}
const resetDefs = () => { defsCache = null; };

// Séries para gráficos: BrunOS (histórico do servidor) e coleta em segundo plano das integrações
const seriesCache = new Map(); // chave → { pts, at, busy }
function cachedSeries(key, maxAge, load) {
  const c = seriesCache.get(key);
  if (!c || (!c.busy && Date.now() - c.at > maxAge)) {
    const next = { pts: c?.pts || null, at: Date.now(), busy: true };
    seriesCache.set(key, next);
    load().then((pts) => { next.pts = pts; }).catch(() => { /* tenta de novo depois */ }).finally(() => { next.busy = false; next.at = Date.now(); paintWidgets(); emit('widgets:data'); });
  }
  return seriesCache.get(key);
}
const RANGE_S = { live: 300, '1h': 3600, '24h': 86400, '7d': 7 * 86400 };
function downsample(pts, max = 240) {
  if (pts.length <= max) return pts;
  const out = [], step = pts.length / max;
  for (let i = 0; i < max; i++) {
    const part = pts.slice(Math.floor(i * step), Math.floor((i + 1) * step)), ok = part.filter((p) => p.v != null);
    out.push({ t: part[0].t, v: ok.length ? ok.reduce((s, p) => s + p.v, 0) / ok.length : null });
  }
  return out;
}
const seriesPts = (sd) => (sd?.points || []).map((v, i) => ({ t: sd.start + i * sd.step, v })).filter((p) => p.v != null);
const integValue = (id) => { const i = id.indexOf('.'); return S.metrics[id.slice(0, i)]?.values?.[id.slice(i + 1)]; };
function widgetCtx() {
  const defs = metricDefs();
  const S2 = { snapshot: S.snapshot, internet: S.internet, apps: S.apps, energy: S.energy, updates: S.updates };
  const energyPts = () => cachedSeries('energy', 60e3, async () => ((await api.energy.history()).minutes || []).filter((p) => p.v != null)).pts;
  const netPts = () => (S.internet?.points || []).map((p) => ({ t: p.t, v: p.ms >= 0 ? p.ms : null }));
  const ctx = {
    def: (id) => defs.get(id),
    value: (id) => (id.startsWith('brunos.') ? brunosValue(id, S2) : integValue(id)),
    err: (id) => (id.startsWith('brunos.') ? '' : S.metrics[id.split('.')[0]]?.err || ''),
    favorites,
    roots: () => S.roots,
    imageUrl: (iid, ref) => api.integrationImage(iid, ref),
    maxGap: (id) => (id === 'brunos.net_latency' ? 180 : undefined),
    spark(id) {
      const d = defs.get(id); if (!d) return null;
      if (d.hist === 'net') return netPts().slice(-120);
      if (d.hist === 'energy') return (energyPts() || []).slice(-180);
      if (d.hist) return S.history.map((p) => ({ t: p.t, v: p[d.hist] ?? null }));
      if (d.kind === 'series') return seriesPts(ctx.value(id)?.s).slice(-48);
      if (d.history) return seriesPts(integValue(`${d.iid}.${d.history}`)?.s).slice(-48);
      if (d.iid) return cachedSeries(`bg:${id}:24h`, 300e3, async () => seriesPts((await api.metricSeries(id, '24h')).series)).pts;
      return null;
    },
    series(id, range) {
      const d = defs.get(id); if (!d) return null;
      const since = Date.now() / 1000 - (RANGE_S[range] || 86400);
      const crop = (pts) => downsample((pts || []).filter((p) => p.t >= since));
      if (d.hist === 'net') return crop(netPts());
      if (d.hist === 'energy') { const p = energyPts(); return p ? crop(p) : null; }
      if (d.hist) {
        if (range === 'live') return S.history.map((p) => ({ t: p.t, v: p[d.hist] ?? null }));
        const c = cachedSeries(`stats:${range}`, 60e3, async () => (await api.statsHistory(range)).points || []);
        return c.pts ? crop(c.pts.map((p) => ({ t: p.t, v: d.hist === 'temp' && !p.temp ? null : p[d.hist] }))) : null;
      }
      if (d.kind === 'series') return crop(seriesPts(ctx.value(id)?.s));
      if (d.history) return crop(seriesPts(integValue(`${d.iid}.${d.history}`)?.s));
      if (d.iid) { const r = range === 'live' ? '1h' : range; return cachedSeries(`bg:${id}:${r}`, 300e3, async () => seriesPts((await api.metricSeries(id, r)).series)).pts; }
      return null;
    },
    loading: (item) => item.slots.some((s) => [...seriesCache.entries()].some(([k, c]) => c.busy && k.includes(s.metric.startsWith('brunos.') ? 'stats:' : s.metric))),
  };
  return ctx;
}

// Área de widgets: colunas escolhidas para a página (widgets2.cols) ou automáticas (centraliza o que há).
// Em tela estreita nunca passa do que cabe; a Faixa ocupa a largura toda.
function areaFit() {
  const mobile = innerWidth < 640, wu = mobile ? Math.min(240, innerWidth * 0.44) : 240, gap = mobile ? 10 : 28;
  return clamp(Math.floor((Math.min(innerWidth - 32, 1640) + gap) / (wu + gap)), 1, 6);
}
function areaCols(list, page = 0) {
  const fit = areaFit(), fixed = W2().cols?.[page];
  if (fixed) return clamp(fixed, 1, fit);
  const need = list.reduce((s, [, it]) => s + (SIZES[it.size].c || fit), 0);
  const wide = list.some(([, it]) => SIZES[it.size].c === 2);
  return clamp(Math.max(need, wide ? 2 : 1), 1, fit);
}
let wEditing = false;
function areaHTML(page, list, cls = 'widget-area') {
  const ctx = widgetCtx(), cols = areaCols(list, page);
  return `<div class="${cls} ${wEditing ? 'editing' : ''}" data-area="${page}" style="--cols:${cols}">${list.map(([id, it], i) => widgetHTML(it, ctx, { id, i, editing: wEditing, cols })).join('')}${wEditing ? `<button class="w-add" data-wadd="${page}">${svg('plus')}<span>Adicionar widget</span></button>` : ''}</div>`;
}
// Modo Organizar: "− N colunas +" em cima da Área de cada página
function areaBarHTML(p) {
  if (!wEditing) return '';
  const n = W2().cols?.[p];
  return `<div class="w-area-bar"><span>Página ${p + 1} · colunas dos widgets</span><button data-wcols="-1" data-wpage="${p}" aria-label="Menos colunas">−</button><span class="w-cols-n">${n || 'automático'}</span><button data-wcols="1" data-wpage="${p}" aria-label="Mais colunas">+</button><button class="${n ? '' : 'on'}" data-wcols="auto" data-wpage="${p}">Automático</button></div>`;
}
function setAreaCols(p, how) {
  const w = W2();
  w.cols ||= {};
  if (how === 'auto') delete w.cols[p];
  else {
    const list = widgetsOn(p), cur = w.cols[p] || areaCols(list, p);
    w.cols[p] = clamp(cur + how, 1, 6);
  }
  save(); renderApps();
}

/* Páginas montadas pelo Bruno (0.9.1): página "real" = índice salvo. Cada uma tem a sua Área de widgets
   (widgets2.areas) e os apps colocados nela (appPlace). Apps "soltos" (sem lugar, ex.: recém-instalados)
   vão para a última página que tem algum app colocado, ou a primeira. */
const appPlace = () => (set().appPlace ||= {});
const placed = (k) => Number.isInteger(appPlace()[k]);
function appPagesPlan(items) {
  const place = appPlace();
  let last = -1;
  for (const it of items) if (placed(it.key)) last = Math.max(last, place[it.key]);
  const loose = Math.max(0, last), by = new Map();
  const push = (p, it) => { if (!by.has(p)) by.set(p, []); by.get(p).push(it); };
  for (const it of items) if (placed(it.key)) push(place[it.key], it);
  for (const it of items) if (!placed(it.key)) push(loose, it);
  return { by, last: items.length ? Math.max(last, loose) : -1 };
}
function realPageCount() {
  const w = widgetPages();
  return Math.max(appPagesPlan(homeItems()).last, w.length ? w[w.length - 1] : -1, 0) + 1;
}
const realOf = (i) => +($$('#app-grid .app-page')[i]?.dataset.real ?? 0);
const currentReal = () => ($('#app-grid').classList.contains('paged') ? realOf(page) : 0);
function goReal(p) {
  const i = $$('#app-grid .app-page').findIndex((el) => +el.dataset.real === p);
  if (i >= 0) goPage(i);
}
// Antes de mover algo entre páginas, quem estava solto ganha o lugar onde está agora (senão, colocar um
// app na página 2 levaria todos os soltos junto)
function fixLoosePlaces() {
  const place = appPlace(), paged = $('#app-grid').classList.contains('paged');
  for (const el of $$('#app-grid .app[data-key]')) {
    const k = el.dataset.key;
    if (!placed(k)) place[k] = paged ? +(el.closest('.app-page')?.dataset.real ?? 0) : 0;
  }
}
// Fecha buracos na numeração (página sem nada some e as seguintes andam uma casa)
function compactPages() {
  const w = W2(), place = appPlace();
  const keys = new Set(homeItems().map((it) => it.key));
  for (const k of Object.keys(place)) if (!keys.has(k)) delete place[k];
  const used = [...new Set([...widgetPages(), ...Object.values(place)])].sort((a, b) => a - b);
  const map = new Map(used.map((p, i) => [p, i]));
  if (used.every((p, i) => p === i)) return;
  const areas = {}, cols = {};
  for (const [p, ids] of Object.entries(w.areas)) if (map.has(+p)) areas[map.get(+p)] = ids;
  for (const [p, n] of Object.entries(w.cols || {})) if (map.has(+p)) cols[map.get(+p)] = n;
  w.areas = areas; w.cols = cols;
  for (const k of Object.keys(place)) place[k] = map.get(place[k]) ?? 0;
}
function placeItem(key, real) { fixLoosePlaces(); appPlace()[key] = real; }
function pageDialog(title, cur, onPick) {
  const total = realPageCount();
  const d = openDialog(`<h2>${svg('layers')}${esc(title)}</h2><div class="w-pages">${Array.from({ length: total }, (_, i) => `<button class="pill-btn ${i === cur ? 'primary' : ''}" data-pg="${i}">Página ${i + 1}</button>`).join('')}<button class="pill-btn" data-pg="${total}">${svg('plus')}Nova página</button></div>`, { width: 420 });
  d.el.addEventListener('click', (e) => {
    const b = e.target.closest('[data-pg]'); if (!b) return;
    d.close(); onPick(+b.dataset.pg);
  });
}
function moveItemToPage(key) {
  pageDialog('Mover para a página', appPlace()[key] ?? currentReal(), (p) => {
    placeItem(key, p); compactPages(); save(); renderApps(); goReal(Math.min(p, realPageCount() - 1));
  });
}
const renderWidgets = () => renderApps(); // os widgets moram nas páginas da tela inicial
// Atualiza só o miolo de cada widget, e só quando o HTML muda (aba escondida: nada)
function paintWidgets(root = document) {
  if (document.hidden) return;
  const wraps = $$('.widget-wrap[data-wid]', root);
  if (!wraps.length) return;
  const ctx = widgetCtx();
  for (const wrap of wraps) {
    const it = widgetById(wrap.dataset.wid); if (!it) continue;
    const box = wrap.firstElementChild, h = innerHTML(it, ctx, wrap.dataset.wid);
    if (box._h !== h) { box.innerHTML = h; box._h = h; }
  }
  mountNotes(root);
}
// Relógios: redesenha a cada segundo só os widgets de Relógio (o HTML só muda quando a hora muda)
setInterval(() => {
  if (document.hidden || document.body.classList.contains('is-locked')) return;
  for (const wrap of $$('.widget-wrap[data-wid]')) {
    const it = widgetById(wrap.dataset.wid); if (it?.format !== 'clock') continue;
    const box = wrap.firstElementChild, h = innerHTML(it, null, wrap.dataset.wid);
    if (box._h !== h) { box.innerHTML = h; box._h = h; }
  }
}, 1000);
// consumo e atualizações (0.11.0): o servidor mede/confere; aqui só busca quando algum widget usa
const usesMetric = (re) => Object.values(W2().items).some((it) => (it.slots || []).some((s) => re.test(s.metric || '')));
async function refreshEnergy(force = false) {
  if (!force && !usesMetric(/^brunos\.(power_w|energy_|cost_)/)) return;
  try { S.energy = (await api.energy.get()).now; } catch { return; }
  paintWidgets();
}
async function refreshUpdates() {
  try { S.updates = await api.updates.get(); } catch { return; }
  paintWidgets(); refreshSettings();
}
setInterval(() => { if (!document.hidden && S.desktopReady && !document.body.classList.contains('is-locked')) refreshEnergy(); }, 60000);
setInterval(() => { if (S.desktopReady && !document.body.classList.contains('is-locked')) refreshUpdates(); }, 3600e3);
// widgets com métricas de internet: o servidor mede a cada minuto; aqui só busca o resultado
const usesInternet = () => Object.values(W2().items).some((it) => (it.slots || []).some((s) => s.metric?.startsWith('brunos.net_')));
async function refreshInternet(force = false) {
  if (!force && !usesInternet()) return;
  if (force && S.internet) return;
  try { S.internet = await api.internet(); } catch { return; }
  paintWidgets(); emit('widgets:data');
}
function openWindow(t) {
  if (t === 'live') liveUsageDialog();
  else if (t === 'battery') openBattery();
  else if (t === 'internet') internetDialog();
  else if (t === 'power') openPower();
  else if (t === 'files') openSheet('files', $('[data-sys="files"]'));
  else if (t === 'settings') openSheet('settings', $('[data-sys="settings"]'));
  else if (t === 'widgets') openWidgets();
}
function widgetClick(id) {
  const it = widgetById(id); if (!it) return;
  const c = !it.click?.kind || it.click.kind === 'auto' ? autoClick(it) : it.click;
  if (c.kind === 'window') openWindow(c.target);
  else if (c.kind === 'app') { const ig = (S.integrations || []).find((x) => x.id === c.target); if (ig?.url) openUrl(ig.url); }
  else if (c.kind === 'url' && c.target) openUrl(normalizeUrl(c.target));
}
function widgetMenu(id, x, y) {
  const it = widgetById(id); if (!it) return;
  contextMenu(x, y, [
    { icon: 'edit', label: 'Editar…', run: () => openWidgets({ edit: id }) },
    '-',
    ...Object.entries(SIZES).map(([k, s]) => ({ icon: it.size === k ? 'check' : 'grid', label: `Tamanho: ${s.label}`, run: () => { W2().items[id].size = k; save(); renderApps(); } })),
    '-',
    { icon: 'layers', label: 'Mover para outra página…', run: () => moveWidgetDialog(id) },
    { icon: 'sort', label: 'Organizar widgets', run: () => setWidgetEditing(true) },
    { icon: 'copy', label: 'Duplicar', run: () => { const n = newId(); putWidget(n, structuredClone(W2().items[id]), pageOfWidget(id)); save(); renderApps(); } },
    { icon: 'trash', label: 'Remover', danger: true, run: () => deleteWidget(id) },
  ]);
}
function moveWidgetDialog(id) {
  pageDialog('Mover para a página', pageOfWidget(id), (p) => {
    fixLoosePlaces(); putWidget(id, W2().items[id], p); compactPages(); save(); renderApps(); goReal(Math.min(p, realPageCount() - 1));
  });
}
function setWidgetEditing(on) {
  wEditing = on;
  document.body.classList.toggle('w-editing', on);
  $('#w-done')?.remove();
  if (on) {
    const b = html(`<button id="w-done" class="pill-btn primary w-done">${svg('check')}Concluir</button>`);
    b.onclick = () => setWidgetEditing(false);
    document.body.append(b);
  }
  renderApps();
}
function openWidgets(opts = {}) {
  if (currentSheet === 'widgets') { import('./widgetsapp.js').then((m) => m.focusWidgets?.(opts)); return; }
  openSheet('widgets', $('[data-sys="widgets"]'), opts);
}
// O que a janela Widgets precisa do resto da interface
const widgetsCtx = {
  get w2() { return W2(); },
  get integrations() { return S.integrations || []; },
  set integrations(v) { S.integrations = v; resetDefs(); },
  get kinds() { return S.kinds || []; },
  get metrics() { return S.metrics; },
  roots: () => S.roots,
  defs: () => metricDefs(),
  ctx: () => widgetCtx(),
  pages: () => realPageCount(),
  currentPage: () => currentReal(),
  setCols: (p, n) => { const w = W2(); w.cols ||= {}; if (n) w.cols[p] = clamp(n, 1, 6); else delete w.cols[p]; save(); renderApps(); },
  save: () => { save(); renderApps(); },
  saveNow: () => saveNow(),
  put: putWidget,
  remove: (id) => deleteWidget(id),
  newId,
  refreshInternet,
  reloadIntegrations: () => loadIntegrations(),
  organize: () => { closeSheet(); setTimeout(() => setWidgetEditing(true), 250); },
};
async function loadIntegrations() {
  try {
    const [kinds, list] = await Promise.all([api.integrations.kinds(), api.integrations.list()]);
    S.kinds = kinds; S.integrations = list.integrations || [];
    for (const v of list.views || []) S.metrics[v.id] = v;
    resetDefs(); paintWidgets();
  } catch { /* sem integrações: só os widgets do BrunOS */ }
}

/* Valores ao vivo: qualquer elemento com data-bind / data-bind-w é atualizado a cada amostra */
function statValues() {
  const sn = S.snapshot;
  if (!sn) return { vals: {}, widths: {} };
  const st = storageTotals();
  const vals = {
    cpu: `${fmt(sn.cpu.percent, 0)}%`, cores: `${sn.cpu.cores} ${sn.cpu.cores === 1 ? 'núcleo' : 'núcleos'}`,
    mem: fmtBytes(sn.mem.used), memShort: fmtBytes(sn.mem.used), memTotal: `/ ${fmtBytes(sn.mem.total)}`, memFree: `${fmtBytes(sn.mem.available)} livres`,
    temp: sn.temp != null ? `${fmt(sn.temp, 0)} °C` : '—', fan: sn.fanRpm ? `${sn.fanRpm} RPM` : 'sem ventoinha',
    rx: fmtRate(sn.net.rx), tx: fmtRate(sn.net.tx), net: `↓ ${fmtRate(sn.net.rx)}  ↑ ${fmtRate(sn.net.tx)}`,
    storageUsed: fmtBytes(st.used), storageTotal: ` / ${fmtBytes(st.total)}`, storageFree: `${fmtBytes(st.total - st.used)} livres`,
    uptime: fmtDuration(sn.uptime), battery: sn.battery ? `${sn.battery.percent}% · ${batteryStatus(sn.battery.status)}` : '',
    load: sn.cpu.load.map((v) => fmt(v, 2)).join(' · '),
  };
  const widths = {
    cpu: sn.cpu.percent, mem: (sn.mem.used / sn.mem.total) * 100, temp: sn.temp != null ? ((sn.temp - 25) / 75) * 100 : 0,
    storage: st.total ? (st.used / st.total) * 100 : 0,
  };
  return { vals, widths };
}
const batteryStatus = (s) => ({ Full: 'carregada', Charging: 'carregando', Discharging: 'na bateria', 'Not charging': 'na tomada' }[s] || s || '');
function bindStats(root = document) {
  const { vals, widths } = statValues();
  $$('[data-bind]', root).forEach((el) => { const v = vals[el.dataset.bind]; if (v != null && el.textContent !== v) el.textContent = v; });
  $$('[data-bind-w]', root).forEach((el) => { const v = widths[el.dataset.bindW]; if (v != null) el.style.width = clamp(v, 0, 100) + '%'; });
  if (S.snapshot) paintBattery(root, S.snapshot.battery);
}

/* ── Grade de apps ─────────────────────────────────────────────── */
// Redesenhar a tela inicial não pode tirar o cursor de uma Nota que está sendo escrita
function renderApps() {
  const f = noteFocus();
  drawHome();
  restoreNoteFocus(f);
}
function drawHome() {
  const grid = $('#app-grid');
  const appW = Math.min(Math.round(set().iconSize * 1.8), Math.floor((innerWidth - 40) / 4));
  document.documentElement.style.setProperty('--app-w', appW + 'px');
  document.documentElement.style.setProperty('--app-icon', Math.min(set().iconSize, Math.round(appW * 0.68)) + 'px');
  const items = homeItems();
  grid.classList.remove('paged');
  $$('#desktop > .w-area-bar').forEach((x) => x.remove());
  // Área de widgets fora das páginas (uma página só ou páginas desligadas): todos os widgets no topo
  const showRow = (list) => {
    $('#widgets').outerHTML = list.length || wEditing ? areaHTML(0, list, 'widget-row').replace('class="widget-row', 'id="widgets" class="widget-row') : '<div id="widgets" class="widget-row" style="display:none"></div>';
    if (wEditing) $('#widgets').insertAdjacentHTML('beforebegin', areaBarHTML(0));
  };
  const empty = `<div class="grid-empty">${S.tipi.found === false ? 'Não encontrei a pasta do Runtipi. Confira a montagem <code>/runtipi</code> no docker-compose.' : 'Nenhum app ainda.'}<br><button class="pill-btn" data-action="add-link">${svg('plus')}Adicionar atalho</button></div>`;
  if (!set().appPages) {
    showRow(widgetPages().flatMap((p) => widgetsOn(p)));
    grid.innerHTML = items.length ? items.map(itemHTML).join('') : empty;
    paintWidgets();
    return;
  }
  // Páginas reais: as que têm widgets ou apps colocados (+ uma página nova vazia no modo Organizar)
  const plan = appPagesPlan(items);
  const wPages = widgetPages(), lastReal = Math.max(plan.last, wPages.length ? wPages[wPages.length - 1] : -1, 0);
  if (lastReal === 0 && !wEditing) {
    showRow(widgetsOn(0));
    if (items.length <= perPage(grid, appW)) { // tudo cabe numa tela só: sem páginas
      grid.innerHTML = items.length ? items.map(itemHTML).join('') : empty;
      paintWidgets();
      return;
    }
  }
  $('#widgets').outerHTML = '<div id="widgets" class="widget-row" style="display:none"></div>';
  $$('#desktop > .w-area-bar').forEach((x) => x.remove());
  grid.classList.add('paged');
  grid.innerHTML = '<div class="pages-track"></div><div class="page-dots"></div>';
  const track = $('.pages-track', grid);
  let n = 0;
  for (let real = 0; real <= lastReal + (wEditing ? 1 : 0); real++) {
    const list = widgetsOn(real), apps = plan.by.get(real) || [], isNew = real > lastReal;
    if (!list.length && !apps.length && !isNew) continue; // página sem nada não aparece
    const withArea = list.length > 0 || wEditing;
    let k = 0, first = true;
    do { // quando os apps não cabem, páginas de continuação logo depois (sem widgets)
      const pg = html(`<div class="app-page" data-real="${real}" ${first ? '' : 'data-cont="1"'}>${first && withArea ? areaBarHTML(real) + areaHTML(real, list) : ''}<div class="page-apps" role="list"></div></div>`);
      track.append(pg);
      const box = $('.page-apps', pg);
      if (isNew) { box.innerHTML = '<div class="page-new-hint">Página nova: arraste widgets ou apps para cá</div>'; break; }
      const per = pagePer(pg, appW, first && withArea);
      const slice = apps.slice(k, k + per);
      box.innerHTML = slice.map((it, i) => itemHTML(it, n + i)).join('');
      k += slice.length; n += slice.length; first = false;
    } while (k < apps.length);
  }
  const total = $$('.app-page', track).length;
  $$('.app-page', track).forEach((el, i) => el.setAttribute('aria-label', `Página ${i + 1} de ${total}`));
  page = clamp(page, 0, total - 1);
  $('.page-dots', grid).innerHTML = Array.from({ length: total }, (_, i) => `<button class="${i === page ? 'on' : ''}" data-page="${i}" aria-label="Página ${i + 1}"></button>`).join('');
  paintWidgets();
  track.scrollLeft = page * track.clientWidth;
  track.addEventListener('scroll', debounce(() => {
    page = Math.round(track.scrollLeft / track.clientWidth);
    $$('.page-dots button', grid).forEach((b, i) => b.classList.toggle('on', i === page));
  }, 60), { passive: true });
}

/* Pastas (E3) e páginas (E4) */
let page = 0;
const folders = () => set().folders || [];
const folderById = (id) => folders().find((f) => f.id === id);
// Itens da tela inicial: apps que não estão em pastas + pastas (com pelo menos um app visível), na ordem salva.
function homeItems() {
  const apps = allApps().filter((a) => !a.o.hidden);
  const byId = new Map(apps.map((a) => [a.id, a]));
  const inFolder = new Set(folders().flatMap((f) => f.apps));
  const items = [
    ...apps.filter((a) => !inFolder.has(a.id)).map((a) => ({ key: a.id, name: a.name, app: a })),
    ...folders().map((f) => ({ key: `folder:${f.id}`, name: f.name, folder: f, apps: f.apps.map((id) => byId.get(id)).filter(Boolean) })).filter((f) => f.apps.length),
  ];
  const idx = new Map(set().order.map((id, i) => [id, i]));
  const at = (it) => (idx.has(it.key) ? idx.get(it.key) : 1e6);
  return items.sort((a, b) => at(a) - at(b) || a.name.localeCompare(b.name, 'pt-BR'));
}
function statusDot(a) {
  if (!set().showStatus || a.state === 'unknown' || a.kind !== 'runtipi') return '';
  const down = a.health === 'down' && a.state !== 'stopped';
  const label = down ? 'Fora do ar' : { running: 'Rodando', stopped: 'Parado', partial: 'Parcialmente rodando' }[a.state] || '';
  return `<i class="status-dot ${a.state} ${down ? 'down' : ''}" title="${label}"></i>`;
}
function appTileHTML(a, i, extra = '') {
  const url = appUrl(a);
  return `<a class="app" role="listitem" href="${esc(url || '#')}" ${set().openIn === 'tab' ? 'target="_blank"' : ''} rel="noopener noreferrer" data-app="${esc(a.id)}" data-key="${esc(a.id)}" draggable="true" style="--i:${i}" title="${esc(a.name)}${url ? '\n' + esc(url) : ''}" ${extra}>
    ${appIconHTML(a, 'app-icon', statusDot(a) + (a.update ? `<i class="app-upd" title="Atualização disponível: ${esc(a.update.available || '')}">${svg('arrowUp')}</i>` : ''))}<span class="app-name">${esc(a.name)}</span></a>`;
}
function itemHTML(it, i) {
  if (it.app) return appTileHTML(it.app, i);
  const down = it.apps.some((a) => a.health === 'down' && a.state !== 'stopped');
  return `<button class="app folder" role="listitem" data-folder="${esc(it.folder.id)}" data-key="${esc(it.key)}" draggable="true" style="--i:${i}" title="${esc(it.name)} (${it.apps.length} apps)">
    <span class="app-icon folder-icon">${it.apps.slice(0, 9).map((a) => appIconHTML(a, 'fi-mini')).join('')}${down && set().showStatus ? '<i class="status-dot down" title="Algum app fora do ar"></i>' : ''}</span><span class="app-name">${esc(it.name)}</span></button>`;
}
function perPage(grid, appW) {
  const cols = Math.max(1, Math.floor(Math.min(innerWidth - 32, appW * 8) / appW));
  const icon = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--app-icon')) || 64;
  const rowH = icon + 60; // ícone + nome + espaçamentos
  const desk = $('#desktop');
  const top = grid.getBoundingClientRect().top + desk.scrollTop - desk.getBoundingClientRect().top;
  // com widgets em cima, aceita uma fileira só (o resto vai para a próxima página)
  const rows = Math.max(widgetsOn(0).length ? 1 : 2, Math.floor((innerHeight - top - 150 - 26) / rowH)); // 150 = dock e pesquisa; 26 = pontinhos
  return cols * rows;
}
// Quantos ícones cabem numa página que já tem a sua área de widgets desenhada
function pagePer(pg, appW, hasArea) {
  const cols = Math.max(1, Math.floor(Math.min(innerWidth - 32, appW * 8) / appW));
  const icon = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--app-icon')) || 64;
  const desk = $('#desktop'), apps = $('.page-apps', pg);
  const top = apps.getBoundingClientRect().top + desk.scrollTop - desk.getBoundingClientRect().top;
  const rows = Math.floor((innerHeight - top - 150 - 26) / (icon + 60));
  return cols * Math.max(hasArea ? 0 : 2, rows);
}
function goPage(n) {
  const track = $('#app-grid .pages-track'); if (!track) return false;
  const max = $$('.app-page', track).length - 1;
  const to = clamp(n, 0, max);
  if (to === page && Math.abs(track.scrollLeft - to * track.clientWidth) < 4) return false;
  page = to;
  track.scrollTo({ left: to * track.clientWidth, behavior: set().reduceMotion ? 'auto' : 'smooth' });
  return true;
}
// guarda a ordem atual da tela inicial, com `key` movido para a posição de `beforeKey` (ou o fim)
function reorder(key, beforeKey) {
  const keys = homeItems().map((it) => it.key).filter((k) => k !== key);
  const to = beforeKey ? keys.indexOf(beforeKey) : -1;
  keys.splice(to < 0 ? keys.length : to, 0, key);
  set().order = keys;
}
function removeFromFolders(id) {
  const st = set();
  st.folders = folders().map((f) => ({ ...f, apps: f.apps.filter((x) => x !== id) }));
}
// pasta com 0 ou 1 app deixa de existir (o app que sobrou volta para o lugar da pasta)
// os apps que saem de uma pasta ficam na página dela
function inheritPage(folderKey, appKeys) {
  const place = appPlace();
  if (!Number.isInteger(place[folderKey])) return;
  for (const k of appKeys) place[k] = place[folderKey];
}
function tidyFolders() {
  const st = set();
  for (const f of folders()) {
    if (f.apps.length > 1) continue;
    const key = `folder:${f.id}`, i = st.order.indexOf(key);
    if (i >= 0) st.order.splice(i, 1, ...f.apps);
    inheritPage(key, f.apps);
  }
  st.folders = folders().filter((f) => f.apps.length > 1);
}
function dropInto(dragKey, targetKey) {
  const st = set();
  if (dragKey.startsWith('folder:')) return false; // pasta dentro de pasta: não
  if (targetKey.startsWith('folder:')) {
    const f = folderById(targetKey.slice(7)); if (!f) return false;
    removeFromFolders(dragKey);
    folderById(f.id).apps.push(dragKey);
    tidyFolders(); save(); renderApps();
    toast(`Na pasta ${f.name}`, appById(dragKey)?.name || '');
    return true;
  }
  // app sobre app: cria uma pasta no lugar do alvo
  const id = Math.random().toString(36).slice(2, 9);
  removeFromFolders(dragKey); removeFromFolders(targetKey);
  if (placed(targetKey)) appPlace()[`folder:${id}`] = appPlace()[targetKey]; // a pasta nasce na página do alvo
  reorder(`folder:${id}`, targetKey);
  st.order = st.order.filter((k) => k !== targetKey && k !== dragKey);
  st.folders = [...folders(), { id, name: 'Pasta', apps: [targetKey, dragKey] }];
  tidyFolders(); save(); renderApps();
  toast('Pasta criada', 'Abra a pasta para dar um nome.', { label: 'Renomear', run: () => renameFolder(id) });
  return true;
}
async function renameFolder(id) {
  const f = folderById(id); if (!f) return;
  const name = await promptDialog('Nome da pasta', { label: 'Nome', value: f.name, okLabel: 'Salvar' });
  if (!name) return;
  f.name = name.slice(0, 40); save(); renderApps();
  $(`.folder-dialog[data-fid="${CSS.escape(id)}"] .fd-name`)?.replaceChildren(document.createTextNode(f.name));
}
function ungroupFolder(id) {
  const st = set(), key = `folder:${id}`, f = folderById(id); if (!f) return;
  const i = st.order.indexOf(key);
  if (i >= 0) st.order.splice(i, 1, ...f.apps);
  inheritPage(key, f.apps);
  st.folders = folders().filter((x) => x.id !== id);
  save(); renderApps();
}
function openFolder(id) {
  const f = folderById(id); if (!f) return;
  const draw = (el) => {
    const apps = folderById(id)?.apps.map(appById).filter((a) => a && !a.o.hidden) || [];
    $('.fd-grid', el).innerHTML = apps.map((a, i) => appTileHTML(a, i, 'data-in-folder="1"')).join('');
  };
  const d = openDialog(`<div class="folder-dialog" data-fid="${esc(id)}"><button class="fd-name" data-fd="rename" title="Renomear">${esc(f.name)}</button><div class="fd-grid app-grid"></div>
    <p class="fd-hint">Clique com o botão direito num app para tirá-lo da pasta.</p></div>`, { cls: 'fd-dialog', width: 560 });
  draw(d.el);
  d.el.addEventListener('click', (e) => {
    if (e.target.closest('[data-fd="rename"]')) { renameFolder(id); return; }
    const el = e.target.closest('.app'); if (!el) return;
    const a = appById(el.dataset.app);
    if (!appUrl(a)) { e.preventDefault(); openApp(a); }
    else setTimeout(() => d.close(), 50);
  });
  d.el.addEventListener('contextmenu', (e) => {
    const el = e.target.closest('.app'); if (!el) return;
    e.preventDefault();
    const a = appById(el.dataset.app);
    contextMenu(e.clientX, e.clientY, [
      { icon: 'external', label: 'Abrir', run: () => openApp(a) },
      { icon: 'info', label: 'Detalhes, logs e uso…', hidden: a.kind !== 'runtipi', run: () => openAppDetails(a) },
      { icon: 'arrowUp', label: 'Tirar da pasta', run: () => {
        removeFromFolders(a.id); inheritPage(`folder:${id}`, [a.id]); reorder(a.id, `folder:${id}`); tidyFolders(); save(); renderApps();
        if (folderById(id)) draw(d.el); else d.close();
      } },
    ]);
  });
}
function folderMenu(id, x, y) {
  contextMenu(x, y, [
    { icon: 'folder', label: 'Abrir', run: () => openFolder(id) },
    { icon: 'edit', label: 'Renomear…', run: () => renameFolder(id) },
    { icon: 'layers', label: 'Mover para a página…', hidden: !set().appPages, run: () => moveItemToPage(`folder:${id}`) },
    '-',
    { icon: 'grid', label: 'Desfazer a pasta', run: () => ungroupFolder(id) },
  ]);
}
(() => {
  const grid = $('#app-grid');
  grid.addEventListener('click', (e) => {
    if (e.target.closest('[data-action="add-link"]')) { linkDialog(); return; }
    const dot = e.target.closest('[data-page]'); if (dot) { goPage(+dot.dataset.page); return; }
    const fo = e.target.closest('[data-folder]'); if (fo) { openFolder(fo.dataset.folder); return; }
    const el = e.target.closest('.app'); if (!el) return;
    const a = appById(el.dataset.app);
    if (!appUrl(a)) { e.preventDefault(); openApp(a); }
  });
  grid.addEventListener('contextmenu', (e) => {
    const fo = e.target.closest('[data-folder]');
    if (fo) { e.preventDefault(); folderMenu(fo.dataset.folder, e.clientX, e.clientY); return; }
    const el = e.target.closest('.app'); if (!el) return;
    e.preventDefault();
    appMenu(appById(el.dataset.app), e.clientX, e.clientY, el);
  });
  // roda do mouse (vertical) sobre as páginas também troca de página
  let wheelAt = 0;
  grid.addEventListener('wheel', (e) => {
    if (!grid.classList.contains('paged') || Math.abs(e.deltaY) < Math.abs(e.deltaX) || Math.abs(e.deltaY) < 20) return;
    e.preventDefault();
    if (Date.now() - wheelAt < 450) return;
    wheelAt = Date.now(); goPage(page + (e.deltaY > 0 ? 1 : -1));
  }, { passive: false });

  // Arrastar: no meio de um ícone = juntar numa pasta; na borda = mudar de lugar.
  // Perto da borda da tela, troca de página.
  let dragKey = null, edgeTimer = 0;
  const clear = () => $$('.drop-before,.drop-into', grid).forEach((x) => x.classList.remove('drop-before', 'drop-into'));
  const zone = (e, el) => { const r = el.getBoundingClientRect(), fx = (e.clientX - r.left) / r.width; return fx > 0.25 && fx < 0.75 ? 'into' : 'before'; };
  grid.addEventListener('dragstart', (e) => {
    const el = e.target.closest('.app'); if (!el) return;
    dragKey = el.dataset.key; el.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/x-brunos-app', dragKey);
  });
  grid.addEventListener('dragover', (e) => {
    if (!dragKey) return;
    e.preventDefault();
    clear();
    const el = e.target.closest('.app');
    if (el && el.dataset.key !== dragKey) el.classList.add(zone(e, el) === 'into' && !dragKey.startsWith('folder:') ? 'drop-into' : 'drop-before');
    if (grid.classList.contains('paged')) {
      const r = grid.getBoundingClientRect(), edge = e.clientX < r.left + 40 ? -1 : e.clientX > r.right - 40 ? 1 : 0;
      if (edge && !edgeTimer) edgeTimer = setTimeout(() => { edgeTimer = 0; goPage(page + edge); }, 650);
      if (!edge) { clearTimeout(edgeTimer); edgeTimer = 0; }
    }
  });
  grid.addEventListener('drop', (e) => {
    if (!dragKey) return;
    e.preventDefault(); e.stopPropagation();
    const el = e.target.closest('.app'), key = dragKey, pg = e.target.closest('.app-page');
    dragKey = null; clear();
    // soltar numa página (ou na continuação dela) coloca o app lá
    if (pg && grid.classList.contains('paged')) placeItem(key, +pg.dataset.real);
    if (el && el.dataset.key !== key && zone(e, el) === 'into' && dropInto(key, el.dataset.key)) return;
    reorder(key, el && el.dataset.key !== key ? el.dataset.key : null);
    compactPages(); save(); renderApps();
  });
  grid.addEventListener('dragend', () => { dragKey = null; clearTimeout(edgeTimer); edgeTimer = 0; $$('.dragging', grid).forEach((x) => x.classList.remove('dragging')); clear(); });
  // setas do teclado trocam de página (fora de campos de texto, janelas e menus)
  document.addEventListener('keydown', (e) => {
    if ((e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') || e.altKey || e.ctrlKey || e.metaKey) return;
    if (dialogs.length || currentSheet || menuOpen() || document.body.classList.contains('is-locked') || e.target.closest?.('input,textarea,select,.xterm,.cm-editor')) return;
    if (goPage(page + (e.key === 'ArrowRight' ? 1 : -1))) e.preventDefault();
  });
})();
function appMenu(a, x, y, el) {
  const st = set();
  const pinned = st.dock.includes(a.id);
  const alt = a.kind === 'runtipi' && a.port && a.localDomain;
  el?.classList.add('menu-open');
  contextMenu(x, y, [
    { icon: 'external', label: 'Abrir', run: () => openApp(a) },
    { icon: 'info', label: 'Detalhes, logs e uso…', hidden: a.kind !== 'runtipi', run: () => openAppDetails(a) },
    { icon: 'globe', label: 'Abrir pelo domínio local', hidden: !alt, run: () => window.open(appUrl(a, 'domain'), '_blank', 'noopener') },
    { icon: 'network', label: `Abrir por IP:porta (${a.port})`, hidden: !alt, run: () => window.open(appUrl(a, 'port'), '_blank', 'noopener') },
    { icon: 'copy', label: 'Copiar endereço', hidden: !appUrl(a), run: async () => { try { await navigator.clipboard.writeText(appUrl(a)); toast('Endereço copiado', appUrl(a)); } catch { toast('Não deu para copiar', appUrl(a)); } } },
    '-',
    { icon: 'edit', label: 'Personalizar…', run: () => editAppDialog(a) },
    { icon: 'pin', label: pinned ? 'Remover do dock' : 'Fixar no dock', run: () => { st.dock = pinned ? st.dock.filter((i) => i !== a.id) : [...st.dock, a.id]; save(); renderDock(); } },
    { icon: 'layers', label: 'Mover para a página…', hidden: !st.appPages || !!el?.dataset.inFolder || !el, run: () => moveItemToPage(a.id) },
    { icon: 'refresh', label: 'Reiniciar app', hidden: !(S.dockerActions && a.kind === 'runtipi' && a.state !== 'stopped'), run: () => restartApp(a) },
    { icon: 'power', label: a.state === 'stopped' ? 'Ligar app' : 'Parar app', hidden: !(S.dockerActions && a.kind === 'runtipi' && a.state !== 'unknown'), run: () => startStopApp(a) },
    '-',
    { icon: 'eyeOff', label: 'Ocultar da tela inicial', run: () => { st.apps[a.id] = { ...(st.apps[a.id] || {}), hidden: true }; save(); renderApps(); toast(`${a.name} oculto`, 'Mostre de novo em Configurações → Apps.'); } },
    { icon: 'trash', label: 'Remover atalho', danger: true, hidden: a.kind !== 'link', run: () => { st.links = st.links.filter((l) => `link:${l.id}` !== a.id); st.dock = st.dock.filter((i) => i !== a.id); save(); renderApps(); renderDock(); } },
  ], () => el?.classList.remove('menu-open'));
}
async function startStopApp(a) {
  const stop = a.state !== 'stopped';
  if (!(await confirmDialog(`${stop ? 'Parar' : 'Ligar'} ${a.name}?`, stop ? 'Pelo BrunOS o Runtipi não fica sabendo: se o servidor ou o Runtipi reiniciar, o app volta a ligar. Para parar de vez, pare pelo painel do Runtipi.' : 'Os containers do app serão ligados.', stop ? 'Parar' : 'Ligar', { danger: stop }))) return;
  try { await (stop ? api.app.stop(a.urn) : api.app.start(a.urn)); toast(`${a.name} ${stop ? 'parado' : 'ligado'}`); setTimeout(loadApps, 1500); }
  catch (e) { toastError(e, `Não foi possível ${stop ? 'parar' : 'ligar'}`); }
}
// Detalhes do app (logs, uso, disponibilidade, ações): módulo carregado só quando abre
function openAppDetails(a) {
  if (!a) return;
  import('./appinfo.js').then((m) => m.appDetails(a, {
    iconHTML: (x) => appIconHTML(x, 'app-icon ai-icon'), appUrl, openApp, runtipiBase, dockerActions: S.dockerActions,
    monitored: (x) => !set().apps[x.id]?.noMonitor,
    setMonitored: (x, on) => { const st = set(); st.apps[x.id] = { ...(st.apps[x.id] || {}), noMonitor: !on }; save(); },
    reload: () => setTimeout(loadApps, 1500),
  })).catch((e) => toastError(e, 'Não deu para abrir os detalhes'));
}
async function restartApp(a) {
  if (!(await confirmDialog(`Reiniciar ${a.name}?`, 'Os containers do app serão reiniciados. Leva alguns segundos.', 'Reiniciar'))) return;
  try { const r = await api.restart(a.urn); toast(`${a.name} reiniciado`, `${r.restarted} container(s)`); setTimeout(loadApps, 3000); }
  catch (e) { toastError(e, 'Não foi possível reiniciar'); }
}

/* ── Dock (ampliação igual à do Umbrel: 50 → 80 px em ±150 px) ─── */
const SYS = {
  files: { label: 'Arquivos', glyph: 'folder', tile: 'linear-gradient(160deg,#ffb199 0%,#ff6a88 45%,#c04fd0 100%)', run: (el) => openSheet('files', el) },
  runtipi: { label: 'Runtipi', icon: 'tipi', tile: '#ffffff', badge: () => 0, run: () => openUrl(runtipiBase()) },
  store: { label: 'Loja de apps', glyph: 'bag', tile: 'linear-gradient(160deg,#5fd0ff 0%,#3a7bff 48%,#5b3df0 100%)', run: () => openUrl(`${runtipiBase()}/app-store`) },
  live: { label: 'Uso ao vivo', glyph: 'activity', tile: 'linear-gradient(160deg,#2e5e46 0%,#123324 100%)', run: () => liveUsageDialog() },
  widgets: { label: 'Widgets', glyph: 'grid', tile: 'linear-gradient(160deg,#ffd36e 0%,#ff9f43 50%,#f0643c 100%)', run: (el) => openSheet('widgets', el) },
  settings: { label: 'Configurações', glyph: 'sliders', tile: 'linear-gradient(160deg,#a5adbd 0%,#5d6577 100%)', run: (el) => openSheet('settings', el) },
  // só aparece enquanto há Abas do Terminal abertas (minimizadas ou não), com o pontinho
  terminal: { label: 'Terminal', glyph: 'terminal', tile: 'linear-gradient(160deg,#454c5e 0%,#191c24 100%)', run: () => openTerminal() },
};
const DOCK = { base: 50, zoom: 80, range: 150 };
let dockMouseX = null, dockRaf = 0, tipFor = null;
function renderDock() {
  const box = $('.dock-items');
  const tabs = termMod?.tabCount?.() ?? S.termTabs ?? 0;
  const sysHTML = Object.entries(SYS).filter(([k]) => k !== 'terminal' || (S.terminal !== false && tabs > 0)).map(([k, it]) => `<div class="dock-item" data-sys="${k}" data-label="${esc(k === 'terminal' ? `Terminal · ${tabs} ${tabs === 1 ? 'aba' : 'abas'}` : it.label)}">
    <button style="--tile:${it.tile}" aria-label="${esc(it.label)}">${it.icon ? `<img src="assets/icons/${it.icon}.webp" alt="">` : svg(it.glyph, 'glyph')}</button><span class="open-dot"></span></div>`).join('');
  const pins = set().dock.map(appById).filter(Boolean);
  const pinHTML = pins.map((a) => {
    const info = iconInfo(a);
    return `<div class="dock-item" data-app="${esc(a.id)}" data-label="${esc(a.name)}"><button style="--tile:${tileCss(a, info)}" aria-label="${esc(a.name)}">${info.src ? `<img src="${esc(info.src)}" alt="" class="${info.full ? 'full' : ''}">` : `<span class="initial">${esc(a.name[0])}</span>`}</button><span class="open-dot"></span></div>`;
  }).join('');
  box.innerHTML = sysHTML + (pins.length ? '<div class="dock-divider"></div>' + pinHTML : '');
  sizeDock(); markDockOpen();
}
function sizeDock() {
  const items = $$('.dock-item'), n = items.length || 1, mobile = innerWidth < 640, gap = mobile ? 6 : 10, pad = mobile ? 8 : 12;
  DOCK.base = clamp(Math.floor((innerWidth - 16 - pad * 2 - gap * (n - 1) - 12) / n), 34, 50);
  DOCK.zoom = Math.round(DOCK.base * 1.6);
  items.forEach((el) => { el._size = DOCK.base; el.style.width = el.style.height = DOCK.base + 'px'; });
  $('#dock').style.height = mobile ? DOCK.base + 16 + 'px' : '';
}
function dockFrame() {
  let moving = false;
  for (const el of $$('.dock-item')) {
    const r = el.getBoundingClientRect();
    const d = dockMouseX == null ? Infinity : Math.abs(dockMouseX - (r.left + r.width / 2));
    const f = d >= DOCK.range ? 0 : (Math.cos((d / DOCK.range) * Math.PI) + 1) / 2;
    const target = DOCK.base + (DOCK.zoom - DOCK.base) * f;
    const cur = el._size ?? DOCK.base, next = cur + (target - cur) * 0.28;
    el._size = Math.abs(next - target) < 0.15 ? target : next;
    if (el._size !== target) moving = true;
    el.style.width = el.style.height = el._size + 'px';
  }
  dockRaf = moving ? requestAnimationFrame(dockFrame) : 0;
  positionTip();
}
const kickDock = () => { if (!dockRaf) dockRaf = requestAnimationFrame(dockFrame); };
function positionTip() {
  const tip = $('#dock-tip');
  if (!tipFor || !tipFor.isConnected) { tip.classList.remove('show'); return; }
  const r = tipFor.getBoundingClientRect();
  tip.textContent = tipFor.dataset.label;
  tip.style.left = r.left + r.width / 2 + 'px';
  tip.style.top = r.top - 36 + 'px';
  tip.classList.add('show');
}
(() => {
  const dock = $('#dock'), fine = matchMedia('(pointer: fine)').matches;
  dock.addEventListener('mousemove', (e) => {
    if (fine && !set().reduceMotion) { dockMouseX = e.clientX; kickDock(); }
    const item = e.target.closest('.dock-item');
    if (item !== tipFor) { tipFor = item; positionTip(); }
  });
  dock.addEventListener('mouseleave', () => { dockMouseX = null; tipFor = null; positionTip(); kickDock(); });
  dock.addEventListener('click', (e) => {
    const item = e.target.closest('.dock-item'); if (!item) return;
    item.classList.remove('bounce'); void item.offsetWidth; item.classList.add('bounce');
    if (item.dataset.sys) { const k = item.dataset.sys; if (currentSheet === k) closeSheet(); else SYS[k].run(item); }
    else openApp(appById(item.dataset.app));
  });
  dock.addEventListener('contextmenu', (e) => {
    const item = e.target.closest('.dock-item[data-app]'); if (!item) return;
    e.preventDefault();
    appMenu(appById(item.dataset.app), e.clientX, e.clientY);
  });
})();
const markDockOpen = () => $$('.dock-item').forEach((el) => el.classList.toggle('open', el.dataset.sys === 'terminal' || (!!el.dataset.sys && el.dataset.sys === currentSheet)));

/* ── Sheets ────────────────────────────────────────────────────── */
let currentSheet = null, sheetCleanup = null;
export function openSheet(kind, originEl, opts = {}) {
  const host = $('#sheet-host');
  if (currentSheet === kind) {
    if (kind === 'files' && opts.root) import('./files.js').then((m) => m.navigate(opts));
    if (kind === 'widgets') import('./widgetsapp.js').then((m) => m.focusWidgets(opts));
    return;
  }
  sheetCleanup?.(); sheetCleanup = null;
  host.innerHTML = '';
  const backdrop = html('<div class="sheet-backdrop"></div>');
  const sheet = html(`<section class="sheet sheet-${kind}" role="dialog" aria-modal="true" aria-label="${{ files: 'Arquivos', widgets: 'Widgets' }[kind] || 'Configurações'}"><button class="sheet-close" aria-label="Fechar">${svg('x')}</button><div class="sheet-body"></div></section>`);
  if (originEl) {
    const r = originEl.getBoundingClientRect(), w = Math.min(1100, innerWidth - 32), left = (innerWidth - w) / 2;
    sheet.style.setProperty('--origin', `${r.left + r.width / 2 - left}px ${innerHeight - 20}px`);
  }
  backdrop.addEventListener('click', closeSheet);
  $('.sheet-close', sheet).addEventListener('click', closeSheet);
  host.append(backdrop, sheet);
  const body = $('.sheet-body', sheet);
  currentSheet = kind;
  document.body.classList.add('has-sheet');
  markDockOpen();
  if (kind === 'files') {
    body.innerHTML = '<div class="sheet-loading"></div>';
    import('./files.js').then((m) => { if (currentSheet === 'files') sheetCleanup = m.mountFiles(body, { ...opts, ctx: filesCtx }); });
  } else if (kind === 'widgets') {
    body.innerHTML = '<div class="sheet-loading"></div>';
    import('./widgetsapp.js').then((m) => { if (currentSheet === 'widgets') sheetCleanup = m.mountWidgets(body, { ...opts, ctx: widgetsCtx }); })
      .catch((e) => toastError(e, 'Não deu para abrir os widgets'));
  } else renderSettings(body);
}
export function closeSheet() {
  pickClose?.();
  const sheet = $('#sheet-host .sheet');
  if (!sheet) return;
  sheetCleanup?.(); sheetCleanup = null;
  currentSheet = null;
  document.body.classList.remove('has-sheet');
  markDockOpen();
  sheet.classList.add('closing');
  setTimeout(() => { if (!currentSheet) $('#sheet-host').innerHTML = ''; }, 220);
}
// O gerenciador de arquivos usa estes ganchos para conversar com o resto da interface.
const filesCtx = {
  get roots() { return S.roots; },
  set roots(v) { S.roots = v; },
  get favorites() { return set().favorites; },
  setFavorites(f) { set().favorites = f; save(); renderWidgets(); },
  get settings() { return set(); },
  saveSetting(k, v) { set()[k] = v; save(); },
  setWallpaperFromFile: async (blob) => uploadWallpaper(blob),
};

/* ── Configurações ─────────────────────────────────────────────── */
const SWATCH_HUES = [209, 259, 330, 6, 24, 45, 145, 174];
let settingsTab = 'all', settingsQuery = '';
// Apps no dock: botão com prévia dos ícones que abre uma lista suspensa com caixas de seleção.
function dockPickerBtn() {
  const pins = set().dock.map(appById).filter(Boolean);
  return `<button class="pick-btn" data-act="dock-pick" aria-haspopup="listbox">
    <span class="pick-icons">${pins.slice(0, 5).map((a) => appIconHTML(a, 'mini')).join('')}${pins.length > 5 ? `<span class="pick-more">+${pins.length - 5}</span>` : ''}</span>
    <span>${pins.length ? `${pins.length} ${pins.length === 1 ? 'app' : 'apps'}` : 'Escolher apps'}</span>${svg('chevD')}</button>`;
}
let pickClose = null;
function dockPicker(anchor, onChange) {
  closeMenu();
  if (pickClose) { pickClose(); return; }
  const apps = allApps();
  const pop = html(`<div class="ctx pick-pop" role="listbox" aria-multiselectable="true">
    ${apps.length > 8 ? `<label class="search-field pick-search">${svg('search')}<input placeholder="Procurar app" aria-label="Procurar app"></label>` : ''}
    <div class="pick-list">${apps.map((a) => `<label class="pick-row" data-name="${esc(a.name.toLowerCase())}">
      <input type="checkbox" value="${esc(a.id)}" ${set().dock.includes(a.id) ? 'checked' : ''}><span class="map-check">${svg('check')}</span>
      ${appIconHTML(a, 'mini')}<span class="pick-name">${esc(a.name)}</span></label>`).join('') || '<div class="empty-note">Nenhum app</div>'}</div>
    <div class="pick-foot"><button class="pick-clear" type="button">Limpar</button><span data-count></span></div></div>`);
  $('#menu-host').append(pop);
  const r = anchor.getBoundingClientRect(), pr = pop.getBoundingClientRect();
  pop.style.left = clamp(r.right - pr.width, 8, innerWidth - pr.width - 8) + 'px';
  pop.style.top = (r.bottom + 6 + pr.height < innerHeight - 8 ? r.bottom + 6 : Math.max(8, r.top - pr.height - 6)) + 'px';
  const count = () => { $('[data-count]', pop).textContent = `${set().dock.length} no dock`; };
  count();
  pop.addEventListener('change', (e) => {
    const id = e.target.value, st = set();
    st.dock = e.target.checked ? [...st.dock.filter((x) => x !== id), id] : st.dock.filter((x) => x !== id);
    save(); renderDock(); count(); onChange();
  });
  $('.pick-clear', pop).onclick = () => { set().dock = []; $$('input[type="checkbox"]', pop).forEach((i) => { i.checked = false; }); save(); renderDock(); count(); onChange(); };
  const search = $('.pick-search input', pop);
  if (search) { search.oninput = () => { const v = search.value.trim().toLowerCase(); $$('.pick-row', pop).forEach((row) => { row.hidden = v && !row.dataset.name.includes(v); }); }; setTimeout(() => search.focus(), 30); }
  const close = () => { pop.remove(); pickClose = null; document.removeEventListener('mousedown', outside, true); document.removeEventListener('keydown', esc_, true); };
  pickClose = close;
  const outside = (e) => { if (!pop.contains(e.target) && !e.target.closest('[data-act="dock-pick"]')) close(); };
  const esc_ = (e) => { if (e.key === 'Escape') { e.stopPropagation(); close(); } };
  setTimeout(() => { document.addEventListener('mousedown', outside, true); document.addEventListener('keydown', esc_, true); });
}
function settingsRows() {
  const st = set();
  const range = (key, min, max, unit = '') => `<input type="range" min="${min}" max="${max}" value="${st[key]}" data-range="${key}" data-unit="${unit}" style="--p:${((st[key] - min) / (max - min)) * 100}%" aria-label="${key}"><span class="range-val">${st[key]}${unit}</span>`;
  const sw = (key, label) => `<button class="switch ${st[key] ? 'on' : ''}" data-toggle="${key}" role="switch" aria-checked="${!!st[key]}" aria-label="${label || key}"></button>`;
  const wps = [...BUILTIN_WALLPAPERS.map((id) => `builtin:${id}`), ...S.wallpapers.map((w) => `upload:${w.name}`), ...(S.localWallpaper ? ['local'] : [])];
  const hidden = allApps().filter((a) => a.o.hidden);
  const info = S.system?.info || {};
  return [
    { group: 'Aparência', cat: 'look', icon: 'image', title: 'Papel de parede', desc: st.wallpaperMode === 'schedule' ? 'Por horário: cada papel de parede vale a partir da hora marcada, até a próxima (troca sozinho, com a cor de destaque junto)' : 'Escolha uma imagem ou envie a sua. Também dá para soltar o arquivo em qualquer lugar da tela inicial',
      ctrl: `<div class="wp-ctrl"><div class="seg wp-mode"><button class="${st.wallpaperMode !== 'schedule' ? 'on' : ''}" data-wpmode="single">Um só</button><button class="${st.wallpaperMode === 'schedule' ? 'on' : ''}" data-wpmode="schedule">Por horário</button></div>${st.wallpaperMode === 'schedule' ? scheduleHTML(wps) : `<div class="wp-thumbs">${wps.map((id) => `<span class="wp-thumb-wrap"><button class="wp-thumb ${st.wallpaper === id ? 'on' : ''}" data-wp="${esc(id)}" style="background-image:url('${esc(wallpaperUrl(id, { thumb: true }))}')" aria-label="Papel de parede"></button>${id.startsWith('upload:') ? `<button class="wp-del" data-wp-del="${esc(id.slice(7))}" aria-label="Apagar">${svg('x')}</button>` : ''}</span>`).join('')}<button class="wp-thumb add" data-wp-add aria-label="Enviar imagem">${svg('plus')}</button></div>`}</div>` },
    { group: 'Aparência', cat: 'look', icon: 'palette', title: 'Cor de destaque', desc: 'Automática tira a cor dominante do papel de parede; Vibrante pega a mais saturada', ctrl: `<span class="brand-chip" title="Cor atual"><i></i></span><div class="seg">${[['auto', 'Automática'], ['vibrant', 'Vibrante'], ['manual', 'Manual']].map(([k, l]) => `<button class="${st.accentMode === k ? 'on' : ''}" data-accent="${k}">${l}</button>`).join('')}</div>${st.accentMode === 'manual' ? `<div class="swatches">${SWATCH_HUES.map((h) => `<button class="swatch ${st.manualHue === h ? 'on' : ''}" data-hue="${h}" style="background:hsl(${h} 80% 48%)" aria-label="Matiz ${h}"></button>`).join('')}<input type="range" min="0" max="359" value="${st.manualHue}" data-hue-range aria-label="Matiz" style="--p:${st.manualHue / 3.59}%;width:110px"></div>` : ''}` },
    { group: 'Aparência', cat: 'look', icon: 'layers', title: 'Efeito de vidro', desc: 'Desfoque atrás dos widgets, do dock e das janelas. Desligar alivia computadores mais fracos', ctrl: `${range('blur', 0, 40, 'px')}${sw('glass', 'Vidro')}` },
    { group: 'Aparência', cat: 'look', icon: 'apps', title: 'Tamanho dos ícones', desc: 'Ícones da tela inicial', ctrl: range('iconSize', 48, 88, 'px') },
    { group: 'Aparência', cat: 'look', icon: 'grid', title: 'Widgets', desc: 'Crie, edite e organize os widgets e as integrações com os apps na janela Widgets', ctrl: `<button class="pill-btn" data-act="widgets">${svg('grid')}Abrir Widgets</button>` },
    { group: 'Aparência', cat: 'look', icon: 'sparkles', title: 'Reduzir animações', desc: 'Desliga a ampliação do dock e as transições', ctrl: sw('reduceMotion') },
    { group: 'Apps', cat: 'apps', icon: 'globe', title: 'Endereço dos apps', desc: 'Automático usa o domínio local do Runtipi quando o app está exposto, e IP:porta nos outros', ctrl: `<div class="seg">${[['auto', 'Automático'], ['domain', 'Domínio local'], ['port', 'IP:porta']].map(([k, l]) => `<button class="${st.urlMode === k ? 'on' : ''}" data-urlmode="${k}">${l}</button>`).join('')}</div>` },
    { group: 'Apps', cat: 'apps', icon: 'box', title: 'Endereço do Runtipi', desc: `Usado pelos ícones Runtipi e Loja de apps do dock. Vazio = ${esc(runtipiAuto())}`, ctrl: `<input class="text-input" data-input="runtipiUrl" value="${esc(st.runtipiUrl || '')}" placeholder="${esc(runtipiAuto().replace(/^https?:\/\//, ''))}" aria-label="Endereço do Runtipi">` },
    { group: 'Apps', cat: 'apps', icon: 'network', title: 'Servidor para IP:porta', desc: `Vazio = o mesmo endereço que você usou para abrir o BrunOS (${esc(location.hostname || '—')})`, ctrl: `<input class="text-input" data-input="serverHost" value="${esc(st.serverHost)}" placeholder="${esc(S.tipi.internalIp || '192.168.1.50')}" aria-label="Servidor">` },
    { group: 'Apps', cat: 'apps', icon: 'pin', title: 'Apps no dock', desc: 'Aparecem depois do divisor, na ordem em que você marca', ctrl: dockPickerBtn() },
    { group: 'Apps', cat: 'apps', icon: 'link', title: 'Atalhos personalizados', desc: 'Sites e serviços que não estão no Runtipi (roteador, impressora, outros servidores)', ctrl: `<button class="pill-btn" data-act="add-link">${svg('plus')}Adicionar</button>` },
    { group: 'Apps', cat: 'apps', icon: 'activity', title: 'Status nos ícones', desc: 'Verde rodando, cinza parado, amarelo parcialmente rodando, vermelho fora do ar', ctrl: sw('showStatus') },
    { group: 'Apps', cat: 'apps', icon: 'bell', title: 'Avisar quando um app cair', desc: 'A cada minuto o BrunOS confere se cada app responde e avisa no sino se algum parar. Dá para desligar por app em Detalhes do app', ctrl: sw('appMonitor', 'Avisar quando um app cair') },
    { group: 'Apps', cat: 'apps', icon: 'arrowUp', title: 'Avisar atualização de apps', desc: (() => { const n = S.updates?.apps.length || 0; return `Uma vez por dia o BrunOS compara cada app com a loja do Runtipi. ${n ? `Agora: ${n} ${n === 1 ? 'app com atualização' : 'apps com atualização'} (seta ↑ no ícone)` : 'Agora: tudo em dia'}`; })(), ctrl: `<button class="pill-btn" data-act="check-updates">${svg('refresh')}Conferir agora</button>${sw('appUpdateNotify', 'Avisar atualização de apps')}` },
    { group: 'Apps', cat: 'apps', icon: 'grid', title: 'Páginas na tela inicial', desc: 'Com muitos apps, divide em páginas (deslize para o lado, use as setas ou os pontinhos). Desligado, a tela rola para baixo', ctrl: sw('appPages', 'Páginas na tela inicial') },
    { group: 'Apps', cat: 'apps', icon: 'external', title: 'Abrir apps em', desc: 'Nova aba ou na mesma aba', ctrl: `<div class="seg"><button class="${st.openIn === 'tab' ? 'on' : ''}" data-openin="tab">Nova aba</button><button class="${st.openIn === 'same' ? 'on' : ''}" data-openin="same">Mesma aba</button></div>` },
    { group: 'Apps', cat: 'apps', icon: 'eyeOff', title: 'Apps ocultos', desc: hidden.length ? hidden.map((a) => a.name).join(', ') : 'Nenhum app oculto', ctrl: hidden.length ? `<button class="pill-btn" data-act="unhide">Mostrar todos</button>` : '' },
    { group: 'Apps', cat: 'apps', icon: 'sort', title: 'Ordem dos apps', desc: 'Arraste os ícones na tela inicial para reordenar', ctrl: st.order.length ? `<button class="pill-btn" data-act="reset-order">Ordem alfabética</button>` : '' },
    { group: 'Conta', cat: 'account', icon: 'user', title: 'Nome de exibição', desc: 'Usado na saudação e na tela de login', ctrl: `<input class="text-input" data-input="displayName" value="${esc(displayName())}" aria-label="Nome de exibição">` },
    { group: 'Conta', cat: 'account', icon: 'key', title: 'Trocar senha', desc: `Usuário: ${esc(S.me?.username || '—')}. Ao trocar, as outras sessões abertas são encerradas`, ctrl: `<button class="pill-btn" data-act="password">Trocar</button>` },
    { group: 'Conta', cat: 'account', icon: 'lock', title: 'Tela de bloqueio', desc: st.lockMode === 'visual' ? 'Só visual: cobre a tela com o relógio; qualquer clique ou tecla volta (não protege nada)' : 'Com senha: até digitar a senha, nada funciona neste aparelho, nem Arquivos nem Terminal. Atalho: Ctrl+Shift+L',
      ctrl: `<div class="seg">${[['senha', 'Com senha'], ['visual', 'Só visual']].map(([k, l]) => `<button class="${(st.lockMode || 'senha') === k ? 'on' : ''}" data-lockmode="${k}">${l}</button>`).join('')}</div><button class="pill-btn" data-act="lock-now">${svg('lock')}Bloquear agora</button>` },
    { group: 'Conta', cat: 'account', icon: 'clock', title: 'Bloquear sozinho', desc: 'Depois de quanto tempo sem mexer o mouse nem digitar', ctrl: `<select class="text-input sm" data-select="lockIdleMin" aria-label="Bloquear sozinho">${[[5, '5 minutos'], [15, '15 minutos'], [30, '30 minutos'], [60, '1 hora'], [0, 'Nunca']].map(([v, l]) => `<option value="${v}" ${(st.lockIdleMin ?? 15) === v ? 'selected' : ''}>${l}</option>`).join('')}</select>` },
    { group: 'Conta', cat: 'account', icon: 'logout', title: 'Sair', desc: 'Encerra esta sessão neste navegador', ctrl: `<button class="pill-btn" data-act="logout">Sair</button>` },
    { group: 'Sistema', cat: 'system', icon: 'power', title: 'Energia e desligamento seguro', desc: S.snapshot?.battery ? 'Desliga o servidor com segurança quando a bateria acabar e registra as quedas de energia' : 'Registra as quedas de energia e desliga o servidor com segurança (precisa de bateria)', ctrl: `<span class="chev">${svg('chevR')}</span>`, act: 'power' },
    { group: 'Sistema', cat: 'system', icon: 'info', title: 'Informações do dispositivo', desc: `${esc(info.os || 'Linux')} · ${esc(info.cpuModel || '')}`, ctrl: `<span class="chev">${svg('chevR')}</span>`, act: 'device' },
    { group: 'Sistema', cat: 'system', icon: 'box', title: 'Runtipi', desc: S.tipi.found ? `${S.apps.length} apps instalados · domínio local ${esc(S.tipi.localDomain || '—')}` : 'Pasta do Runtipi não encontrada (monte em /runtipi)', ctrl: `<button class="pill-btn" data-act="open-runtipi">Abrir ${svg('external')}</button>` },
    { group: 'Sistema', cat: 'system', icon: 'layers', title: 'Docker', desc: S.docker ? (S.dockerActions ? 'Conectado · reiniciar apps permitido' : 'Conectado em modo leitura (status e uso dos apps)') : 'Sem acesso ao Docker: status e uso por app ficam desligados', ctrl: `<span class="v1-note ${S.docker ? 'ok' : 'off'}">${S.docker ? 'ativo' : 'desligado'}</span>` },
    { group: 'Sistema', cat: 'system', icon: 'terminal', title: 'Terminal', desc: S.terminal === false ? 'Desligado no docker-compose (BRUNOS_TERMINAL=false)' : 'Terminal no servidor por SSH, com abas, comandos salvos e a Chave do Terminal (trancada pela sua senha do BrunOS)', ctrl: S.terminal === false ? '' : `<button class="pill-btn" data-act="terminal">${svg('terminal')}Abrir terminal</button>` },
    ...(S.terminal === false ? [] : [{ group: 'Sistema', cat: 'system', icon: 'clock', title: 'Encerrar aba do Terminal sem ninguém olhando', desc: 'Aba minimizada ou janela fechada. Se algo ainda estiver rodando nela (ex.: apt), ela espera o comando terminar', ctrl: `<select class="text-input sm" data-select="termIdleMin" aria-label="Encerrar aba sem uso">${[[3, '3 minutos'], [5, '5 minutos'], [10, '10 minutos'], [15, '15 minutos'], [20, '20 minutos'], [30, '30 minutos'], [0, 'Nunca']].map(([v, l]) => `<option value="${v}" ${(st.termIdleMin ?? 15) === v ? 'selected' : ''}>${l}</option>`).join('')}</select>` }]),
    (() => {
      const o = S.updates?.os;
      const desc = !o ? 'Conferido uma vez por dia pelo script do servidor' : o.err ? esc(o.err)
        : `${o.total ? `${o.total} ${o.total === 1 ? 'atualização' : 'atualizações'}${o.security ? ` (${o.security} de segurança)` : ''}` : 'Tudo em dia'}${o.reboot ? ' · <b class="os-reboot">reinício necessário</b>' : ''}`;
      return { group: 'Sistema', cat: 'system', icon: 'refresh', title: 'Atualizações do Linux', desc,
        ctrl: `${o?.total ? `<button class="pill-btn" data-act="os-list">Ver lista</button>` : ''}${S.terminal !== false ? `<button class="pill-btn" data-act="os-update">${svg('terminal')}Atualizar no Terminal</button>` : ''}${/script do servidor/.test(o?.err || '') ? `<button class="pill-btn" data-act="script">Mostrar comandos</button>` : ''}` };
    })(),
    { group: 'Sistema', cat: 'system', icon: 'folder', title: 'Pastas do gerenciador', desc: S.roots.length ? S.roots.map((r) => r.name + (r.readOnly ? ' (só leitura)' : '')).join(' · ') : 'Nenhuma pasta montada em /files', ctrl: `<button class="pill-btn" data-act="map-disks">${svg('disks')}Mapear discos</button>` },
    { group: 'Sistema', cat: 'system', icon: 'globe', title: 'Monitor de internet', desc: 'A cada minuto mede a latência até a Cloudflare, Google e Quad9 e avisa no sino quando a internet cai e volta. Mostre no widget Internet', ctrl: sw('netMonitor', 'Monitor de internet') },
    { group: 'Sistema', cat: 'system', icon: 'disks', title: 'Previsão de disco cheio', desc: 'Mostra em Uso ao vivo quando cada disco deve encher, pelo ritmo das últimas semanas, e avisa no sino se faltar menos de 30 dias', ctrl: sw('diskForecast', 'Previsão de disco cheio') },
    { group: 'Sistema', cat: 'system', icon: 'clock', title: 'Cópias das preferências', desc: 'O servidor guarda uma cópia por dia (últimos 14 dias) de widgets, páginas, dock e aparência. Volte para uma delas se algo se perder', ctrl: `<button class="pill-btn" data-act="backups">Ver cópias</button>` },
    { group: 'Sistema', cat: 'system', icon: 'copy', title: 'Exportar preferências', desc: 'Copia o JSON com apps, dock e aparência', ctrl: `<button class="pill-btn" data-act="copy-config">Copiar</button>` },
    { group: 'Sistema', cat: 'system', icon: 'refresh', title: 'Restaurar aparência padrão', desc: 'Volta papel de parede, cores, widgets e ordem dos apps ao padrão (não mexe na conta)', ctrl: `<button class="pill-btn danger" data-act="reset">Restaurar</button>` },
  ];
}
let settingsBody = null;
function refreshSettings() { if (settingsBody?.isConnected) drawSettingsRows(settingsBody); }
function renderSettings(body) {
  settingsBody = body;
  const info = S.system?.info || {};
  body.innerHTML = `<div class="settings">
    <h2 class="sheet-title">Configurações</h2>
    <div class="settings-grid">
      <div class="s-left">
        <div class="wp-preview" style="background-image:url('${esc(wallpaperUrl(set().wallpaper, { thumb: true }))}')"><span class="avatar">${esc((displayName() || 'B')[0].toUpperCase())}</span></div>
        <div class="device-name">BrunOS <span>· ${esc(info.hostname || location.hostname)}</span></div>
        <div class="card">
          <div class="kv"><span>Sistema</span><span>${esc(info.os || '—')}</span></div>
          <div class="kv"><span>Processador</span><span title="${esc(info.cpuModel || '')}">${esc((info.cpuModel || '—').replace(/\(R\)|\(TM\)|CPU|@.*$/g, '').replace(/\s+/g, ' ').trim())}</span></div>
          <div class="kv"><span>IP local</span><span>${esc(S.tipi.internalIp || location.hostname)}</span></div>
          <div class="kv"><span>Tempo ativo</span><span data-bind="uptime">—</span></div>
          <div class="kv"><span>BrunOS</span><span>${esc(S.system?.version || S.pub.version || '')}</span></div>
        </div>
        <div class="btn-row">
          <button class="pill-btn" data-act="logout">${svg('logout')}Sair</button>
          <button class="pill-btn" data-act="live">${svg('activity')}Uso ao vivo</button>
        </div>
        <div class="card meter-card"><div class="row"><span>Armazenamento</span><span><b data-bind="storageUsed"></b> <small data-bind="storageTotal"></small></span></div><div class="track"><i data-bind-w="storage"></i></div></div>
        <div class="card meter-card"><div class="row"><span>Memória</span><span><b data-bind="mem"></b> <small data-bind="memTotal"></small></span></div><div class="track"><i data-bind-w="mem"></i></div></div>
        <div class="card meter-card"><div class="row"><span>CPU</span><span><b data-bind="cpu"></b> <small data-bind="cores"></small></span></div><div class="track"><i data-bind-w="cpu"></i></div></div>
        <div class="card meter-card"><div class="row"><span>Temperatura</span><span><b data-bind="temp"></b> <small data-bind="fan"></small></span></div><div class="track"><i data-bind-w="temp"></i></div></div>
      </div>
      <div class="s-right">
        <div class="s-toolbar">
          <div class="tabs">${[['all', 'Tudo'], ['look', 'Aparência'], ['apps', 'Apps'], ['account', 'Conta'], ['system', 'Sistema']].map(([k, l]) => `<button data-tab="${k}" class="${settingsTab === k ? 'on' : ''}">${l}</button>`).join('')}</div>
          <label class="search-field">${svg('search')}<input data-s-search placeholder="Pesquisar" value="${esc(settingsQuery)}" aria-label="Pesquisar configurações"></label>
        </div>
        <div class="s-groups"></div>
      </div>
    </div>
  </div>`;
  drawSettingsRows(body);
  bindStats(body);
  wireSettings(body);
}
function drawSettingsRows(body) {
  const q = settingsQuery.trim().toLowerCase();
  const rows = settingsRows().filter((r) => (settingsTab === 'all' || r.cat === settingsTab) && (!q || (r.title + ' ' + r.desc + ' ' + r.group).toLowerCase().includes(q)));
  const groups = {};
  rows.forEach((r) => (groups[r.group] ||= []).push(r));
  $('.s-groups', body).innerHTML = rows.length ? Object.entries(groups).map(([g, rs]) => `<div class="s-group-title">${g}</div><div class="s-group">${rs.map((r) => `
    <div class="s-row ${r.act ? 'clickable' : ''}" ${r.act ? `data-act="${r.act}"` : ''}>
      <span class="s-ico">${svg(r.icon)}</span>
      <span class="s-text"><b>${r.title}</b><small>${r.desc}</small></span>
      <span class="s-ctrl">${r.ctrl}</span>
    </div>`).join('')}</div>`).join('') : '<div class="empty-note">Nenhuma configuração encontrada.</div>';
  const pv = $('.wp-preview', body); if (pv) pv.style.backgroundImage = `url('${wallpaperUrl(set().wallpaper, { thumb: true })}')`;
}
function wireSettings(body) {
  const redraw = () => drawSettingsRows(body);
  const st = set();
  body.onclick = async (e) => {
    const q = (sel) => e.target.closest(sel);
    let el;
    if ((el = q('[data-tab]'))) { settingsTab = el.dataset.tab; $$('[data-tab]', body).forEach((b) => b.classList.toggle('on', b === el)); redraw(); }
    else if ((el = q('[data-wpmode]'))) {
      st.wallpaperMode = el.dataset.wpmode;
      if (st.wallpaperMode === 'schedule' && !(st.wallpaperSchedule || []).length) {
        const other = `builtin:${BUILTIN_WALLPAPERS.find((x) => `builtin:${x}` !== st.wallpaper) || '2'}`;
        st.wallpaperSchedule = [{ at: '06:00', wp: st.wallpaper }, { at: '18:00', wp: other }];
      }
      wpsPick = null; save(); applySchedule(); redraw();
    }
    else if ((el = q('[data-wps-pick]'))) { const i = +el.dataset.wpsPick; wpsPick = wpsPick === i ? null : i; redraw(); }
    else if ((el = q('[data-wps-set]'))) { if (st.wallpaperSchedule[wpsPick]) st.wallpaperSchedule[wpsPick].wp = el.dataset.wpsSet; wpsPick = null; save(); applySchedule(); redraw(); }
    else if ((el = q('[data-wps-del]'))) { st.wallpaperSchedule.splice(+el.dataset.wpsDel, 1); wpsPick = null; save(); applySchedule(); redraw(); }
    else if (q('[data-wps-add]')) { (st.wallpaperSchedule ||= []).push({ at: '12:00', wp: st.wallpaper }); wpsPick = st.wallpaperSchedule.length - 1; save(); redraw(); }
    else if ((el = q('[data-wp-del]'))) {
      const name = el.dataset.wpDel;
      if (!(await confirmDialog('Apagar este papel de parede?', 'A imagem enviada será removida do servidor.', 'Apagar', { danger: true }))) return;
      try { await api.deleteWallpaper(name); S.wallpapers = S.wallpapers.filter((w) => w.name !== name); if (st.wallpaper === `upload:${name}`) { st.wallpaper = DEFAULTS.wallpaper; setWallpaper(st.wallpaper); save(); } redraw(); }
      catch (err) { toastError(err); }
    }
    else if ((el = q('[data-wp]'))) { st.wallpaper = el.dataset.wp; save(); setWallpaper(st.wallpaper); redraw(); }
    else if (q('[data-wp-add]')) $('#wp-file').click();
    else if ((el = q('[data-accent]'))) { st.accentMode = el.dataset.accent; save(); applyBrand(); redraw(); }
    else if ((el = q('[data-hue]'))) { st.manualHue = +el.dataset.hue; save(); applyBrand(); redraw(); }
    else if ((el = q('[data-toggle]'))) { const k = el.dataset.toggle; st[k] = !st[k]; save(); applyPrefs(); redraw(); }
    else if ((el = q('[data-act="dock-pick"]'))) dockPicker(el, () => { const b = $('[data-act="dock-pick"]', body); if (b) b.outerHTML = dockPickerBtn(); });
    else if ((el = q('[data-openin]'))) { st.openIn = el.dataset.openin; save(); renderApps(); redraw(); }
    else if ((el = q('[data-urlmode]'))) { st.urlMode = el.dataset.urlmode; save(); renderApps(); redraw(); }
    else if ((el = q('[data-lockmode]'))) { st.lockMode = el.dataset.lockmode; save(); redraw(); }
    else if ((el = q('[data-act]'))) {
      const a = el.dataset.act;
      if (a === 'live') liveUsageDialog();
      else if (a === 'widgets') openWidgets();
      else if (a === 'logout') logout();
      else if (a === 'device') deviceDialog();
      else if (a === 'power') openPower();
      else if (a === 'password') passwordDialog();
      else if (a === 'add-link') linkDialog();
      else if (a === 'open-runtipi') SYS.runtipi.run();
      else if (a === 'terminal') openTerminal();
      else if (a === 'lock-now') { closeSheet(); setTimeout(lockNow, 200); }
      else if (a === 'check-updates') { el.disabled = true; try { S.updates = await api.updates.check(); await loadApps(); toast('Atualizações conferidas', S.updates.apps.length ? `${S.updates.apps.length} app(s) com atualização.` : 'Tudo em dia.'); } catch (err) { toastError(err); } redraw(); }
      else if (a === 'os-update') { closeSheet(); openTerminal({ typeCmd: 'sudo apt update && sudo apt upgrade' }); }
      else if (a === 'os-list') osUpdatesDialog();
      else if (a === 'backups') backupsDialog();
      else if (a === 'script') import('./energy.js').then((m) => m.scriptDialog());
      else if (a === 'map-disks') import('./files.js').then((m) => m.mapDisksDialog(filesCtx, () => { renderWidgets(); redraw(); }));
      else if (a === 'unhide') { for (const k of Object.keys(st.apps)) if (st.apps[k].hidden) st.apps[k].hidden = false; save(); renderApps(); redraw(); }
      else if (a === 'reset-order') { st.order = []; save(); renderApps(); redraw(); }
      else if (a === 'copy-config') {
        try { await navigator.clipboard.writeText(JSON.stringify(st, null, 2)); toast('Preferências copiadas', 'JSON na área de transferência.'); }
        catch { toast('Não deu para copiar', 'O navegador bloqueou a área de transferência.'); }
      } else if (a === 'reset') {
        if (!(await confirmDialog('Restaurar a aparência padrão?', 'Papel de parede, cores, widgets, dock e ordem dos apps voltam ao padrão. Atalhos e personalizações de apps são mantidos.', 'Restaurar', { danger: true }))) return;
        const keep = { apps: st.apps, links: st.links, favorites: st.favorites, welcomed: true };
        S.settings = { ...structuredClone(DEFAULTS), ...keep };
        save(); applyPrefs(); renderDock(); setWallpaper(set().wallpaper); renderSettings(body);
      }
    }
  };
  body.oninput = debounce(async (e) => {
    const t = e.target;
    if (t.matches('[data-wps-at]')) {
      const e = set().wallpaperSchedule?.[+t.dataset.wpsAt];
      if (e && /^\d{2}:\d{2}$/.test(t.value)) { e.at = t.value; save(); applySchedule(); }
    } else if (t.matches('[data-range]')) {
      const k = t.dataset.range; set()[k] = +t.value; save();
      t.style.setProperty('--p', ((t.value - t.min) / (t.max - t.min)) * 100 + '%');
      t.nextElementSibling.textContent = t.value + (t.dataset.unit || '');
      applyPrefs();
    } else if (t.matches('[data-select]')) {
      set()[t.dataset.select] = +t.value; save(); // termIdleMin é lido pelo servidor (settings.json)
    } else if (t.matches('[data-hue-range]')) {
      set().manualHue = +t.value; t.style.setProperty('--p', t.value / 3.59 + '%'); save(); applyBrand();
    } else if (t.matches('[data-input="runtipiUrl"]')) { set().runtipiUrl = t.value.trim(); save(); }
    else if (t.matches('[data-input="serverHost"]')) { set().serverHost = t.value.trim().replace(/^https?:\/\//, '').replace(/\/.*$/, ''); save(); renderApps(); }
    else if (t.matches('[data-input="displayName"]')) {
      const v = t.value.trim(); if (!v) return;
      try { await api.updateMe({ displayName: v }); S.me.displayName = v; renderGreeting(); } catch (err) { toastError(err); }
    } else if (t.matches('[data-s-search]')) { settingsQuery = t.value; redraw(); }
  }, 60);
}

/* ── Diálogos ──────────────────────────────────────────────────── */
// Cópias das preferências (0.11.1): uma por dia no servidor; restaurar recarrega a página
async function backupsDialog() {
  let list;
  try { list = await api.settingsBackups(); } catch (e) { toastError(e); return; }
  const day = (s) => new Date(s + 'T12:00:00').toLocaleDateString('pt-BR', { weekday: 'short', day: '2-digit', month: 'short' });
  const d = openDialog(`<h2>${svg('clock')}Cópias das preferências</h2>
    <p class="dlg-text">Cada cópia é como as preferências estavam no começo daquele dia (antes da primeira mudança). Restaurar troca as atuais pela cópia; as atuais viram a cópia de hoje, se ainda não houver uma.</p>
    ${list.length ? `<div class="s-group">${list.map((b) => `<div class="s-row"><span class="s-ico">${svg('clock')}</span><span class="s-text"><b>${esc(day(b.date))}</b><small>${b.widgets} ${b.widgets === 1 ? 'widget' : 'widgets'}${b.pages ? ` em ${b.pages} ${b.pages === 1 ? 'página' : 'páginas'}` : ''}</small></span><span class="s-ctrl"><button class="pill-btn" data-restore="${esc(b.date)}">Restaurar</button></span></div>`).join('')}</div>`
    : '<p class="dlg-text">Ainda não há cópias: a primeira é feita na próxima vez que você mudar alguma preferência.</p>'}
    <div class="dlg-actions"><button class="pill-btn primary" data-c="ok">Fechar</button></div>`, { width: 480 });
  d.el.addEventListener('click', async (e) => {
    if (e.target.closest('[data-c]')) { d.close(); return; }
    const b = e.target.closest('[data-restore]'); if (!b) return;
    if (!(await confirmDialog('Restaurar esta cópia?', `As preferências voltam a ser como estavam em ${day(b.dataset.restore)}. A página vai recarregar.`, 'Restaurar'))) return;
    try { await api.settingsRestore(b.dataset.restore); S.settingsLoaded = false; location.reload(); } catch (err) { toastError(err); }
  });
}
// Atualizações do Linux (0.11.0): lista de pacotes e o botão que digita o comando no Terminal
function osUpdatesDialog() {
  const o = S.updates?.os; if (!o) return;
  const d = openDialog(`<h2>${svg('refresh')}Atualizações do Linux</h2>
    <p class="dlg-text">${o.total} ${o.total === 1 ? 'pacote' : 'pacotes'} para atualizar${o.security ? `, ${o.security} de segurança` : ''}.${o.reboot ? ' <b>O servidor precisa reiniciar</b> para valer uma atualização já instalada.' : ''}</p>
    <pre class="pw-cmds os-pkgs">${esc((o.packages || []).join('\n') || '—')}</pre>
    <p class="dlg-text"><small>O BrunOS não atualiza sozinho: o botão abre o Terminal com o comando digitado, e você confere e aperta Enter.</small></p>
    <div class="dlg-actions"><button class="pill-btn" data-c="no">Fechar</button>${S.terminal !== false ? `<button class="pill-btn primary" data-c="term">${svg('terminal')}Atualizar no Terminal</button>` : ''}</div>`, { width: 520 });
  d.el.addEventListener('click', (e) => {
    const c = e.target.closest('[data-c]')?.dataset.c; if (!c) return;
    d.close();
    if (c === 'term') { closeSheet(); openTerminal({ typeCmd: 'sudo apt update && sudo apt upgrade' }); }
  });
}
// Apps com atualização (busca Ctrl+K)
function appUpdatesDialog() {
  const list = S.updates?.apps || [];
  const d = openDialog(`<h2>${svg('arrowUp')}Apps com atualização</h2>
    ${list.length ? `<div class="s-group">${list.map((u) => { const a = appById(u.urn); return `<button class="s-row clickable" data-upd="${esc(u.urn)}">${a ? appIconHTML(a, 'mini') : svg('box')}<span class="s-text"><b>${esc(u.name)}</b><small>${esc(u.installed || '?')} → ${esc(u.available || '?')}</small></span><span class="chev">${svg('chevR')}</span></button>`; }).join('')}</div>`
    : '<p class="dlg-text">Tudo em dia.</p>'}
    <div class="dlg-actions"><button class="pill-btn primary" data-c="ok">Fechar</button></div>`, { width: 460 });
  d.el.addEventListener('click', (e) => {
    const b = e.target.closest('[data-upd]');
    if (b) { const a = appById(b.dataset.upd); d.close(); if (a) openAppDetails(a); return; }
    if (e.target.closest('[data-c]')) d.close();
  });
}
function deviceDialog() {
  const i = S.system?.info || {}, sn = S.snapshot;
  const rows = [['Hostname', i.hostname], ['Sistema', i.os], ['Kernel', i.kernel], ['Processador', i.cpuModel], ['Núcleos', i.cores], ['Arquitetura', i.arch],
    ['Memória', sn ? fmtBytes(sn.mem.total) : ''], ['Swap', sn?.mem.swapTotal ? `${fmtBytes(sn.mem.swapUsed)} de ${fmtBytes(sn.mem.swapTotal)}` : ''],
    ['Tempo ativo', sn ? fmtDuration(sn.uptime) : ''], ['Bateria', sn?.battery ? `${sn.battery.percent}% · ${batteryStatus(sn.battery.status)}` : ''],
    ['Ventoinha', sn?.fanRpm ? `${sn.fanRpm} RPM` : ''], ['Rede', sn?.net.ifaces?.join(', ')], ['IP (Runtipi)', S.tipi.internalIp], ['Domínio local', S.tipi.localDomain]].filter(([, v]) => v);
  const sensors = (sn?.sensors || []).map((s) => `<div class="kv"><span>${esc(s.chip)} · ${esc(s.label)}</span><span>${fmt(s.value, 1)} °C</span></div>`).join('');
  openDialog(`<h2>${svg('info')}Informações do dispositivo</h2><div class="card">${rows.map(([k, v]) => `<div class="kv"><span>${k}</span><span>${esc(v)}</span></div>`).join('')}</div>
    ${sensors ? `<div class="s-group-title" style="margin-top:14px">Sensores</div><div class="card">${sensors}</div>` : ''}`, { width: 500 });
}
function passwordDialog() {
  const d = openDialog(`<h2>${svg('key')}Trocar senha</h2><form class="form-grid">
    <label>Senha atual<input class="text-input" type="password" name="current" autocomplete="current-password" required autofocus></label>
    <label>Nova senha<input class="text-input" type="password" name="next" autocomplete="new-password" minlength="8" required></label>
    <label>Repita a nova senha<input class="text-input" type="password" name="again" autocomplete="new-password" minlength="8" required></label>
    <p class="form-error"></p>
    <div class="dlg-actions"><button type="button" class="pill-btn" data-c="no">Cancelar</button><button type="submit" class="pill-btn primary">Trocar senha</button></div></form>`, { closeBtn: false, width: 420 });
  const f = $('form', d.el), err = $('.form-error', d.el);
  $('[data-c="no"]', d.el).onclick = () => d.close();
  f.onsubmit = async (e) => {
    e.preventDefault();
    if (f.next.value !== f.again.value) { err.textContent = 'As senhas novas não são iguais.'; return; }
    try { await api.password({ current: f.current.value, next: f.next.value }); d.close(); toast('Senha trocada', 'As outras sessões foram encerradas.'); }
    catch (x) { err.textContent = x.message; }
  };
}
function iconPicker(current) {
  return `<div class="icon-pick">
    <label class="radio"><input type="radio" name="iconMode" value="auto" ${!current || current === 'auto' ? 'checked' : ''}> Automático</label>
    <label class="radio"><input type="radio" name="iconMode" value="di" ${current?.startsWith('di:') || current?.startsWith('builtin:') ? 'checked' : ''}> Dashboard Icons</label>
    <label class="radio"><input type="radio" name="iconMode" value="letter" ${current === 'letter' ? 'checked' : ''}> Letra</label>
    <input class="text-input" name="slug" placeholder="ex.: jellyfin, home-assistant" value="${esc(current?.startsWith('di:') ? current.slice(3) : current?.startsWith('builtin:') ? current.slice(8) : '')}" list="di-list">
    <datalist id="di-list">${BUILTIN_ICONS.map((i) => `<option value="${i}">`).join('')}</datalist>
    <small class="hint">Nomes em <a href="https://dashboardicons.com" target="_blank" rel="noopener">dashboardicons.com</a>. Os ícones vêm do CDN jsDelivr.</small>
  </div>`;
}
function readIcon(form) {
  const mode = form.iconMode.value, slug = form.slug.value.trim().toLowerCase().replace(/[^a-z0-9-]/g, '');
  if (mode === 'letter') return 'letter';
  if (mode === 'di' && slug) return BUILTIN_ICONS.includes(slug) ? `builtin:${slug}` : `di:${slug}`;
  return 'auto';
}
const TILES = ['', '#ffffff', '#f2f2f7', '#1d1d22', '#0f172a', 'brand'];
const tileOpts = (cur) => `<span class="tile-opts">${TILES.map((t) => `<button type="button" class="tile-opt ${(cur || '') === t ? 'on' : ''}" data-tile="${t}" style="background:${t === 'brand' ? 'var(--brand)' : t || 'conic-gradient(#fff 0 25%,#2a2a30 0 50%,#fff 0 75%,#2a2a30 0)'}" title="${t ? t : 'Automático'}"></button>`).join('')}</span>`;
function editAppDialog(a) {
  const o = set().apps[a.id] || {};
  const isLink = a.kind === 'link';
  const d = openDialog(`<h2>${appIconHTML(a, 'mini')} Personalizar ${esc(a.name)}</h2><form class="form-grid">
    <label>Nome<input class="text-input" name="name" value="${esc(a.name)}" autofocus></label>
    ${isLink ? `<label>Endereço<input class="text-input" name="url" value="${esc(a.url || '')}" placeholder="https://…" required></label>` : `
    <label>Como abrir<span class="seg" data-seg="urlMode">${[['', 'Padrão'], ['domain', 'Domínio local'], ['port', 'IP:porta']].map(([k, l]) => `<button type="button" class="${(o.urlMode || '') === k ? 'on' : ''}" data-v="${k}">${l}</button>`).join('')}</span></label>
    <label>Endereço personalizado (opcional)<input class="text-input" name="url" value="${esc(o.url || '')}" placeholder="${esc(appUrl({ ...a, o: { ...o, url: '' } }) || 'https://app.server.lan')}"></label>
    <small class="hint">${a.localDomain ? `Domínio do Runtipi: ${esc(a.localDomain)}` : 'Este app não está exposto no domínio local do Runtipi.'}${a.port ? ` · Porta ${a.port}` : ''}</small>`}
    <label>Ícone${iconPicker(o.icon || a.icon)}</label>
    <label>Fundo do ícone${tileOpts(o.tile || a.tile)}</label>
    <div class="dlg-actions">${!isLink && Object.keys(o).length ? '<button type="button" class="pill-btn" data-c="reset" style="margin-right:auto">Restaurar</button>' : ''}<button type="button" class="pill-btn" data-c="no">Cancelar</button><button type="submit" class="pill-btn primary">Salvar</button></div></form>`, { closeBtn: false, width: 480 });
  const f = $('form', d.el);
  let tile = o.tile || a.tile || '', urlMode = o.urlMode || '';
  d.el.addEventListener('click', (e) => {
    const t = e.target.closest('[data-tile]');
    if (t) { tile = t.dataset.tile; $$('[data-tile]', d.el).forEach((x) => x.classList.toggle('on', x === t)); }
    const sg = e.target.closest('[data-seg="urlMode"] [data-v]');
    if (sg) { urlMode = sg.dataset.v; $$('[data-seg="urlMode"] [data-v]', d.el).forEach((x) => x.classList.toggle('on', x === sg)); }
    const c = e.target.closest('[data-c]');
    if (c?.dataset.c === 'no') d.close();
    if (c?.dataset.c === 'reset') { delete set().apps[a.id]; save(); renderApps(); renderDock(); d.close(); }
  });
  f.slug.addEventListener('input', () => { f.iconMode.value = 'di'; });
  f.onsubmit = (e) => {
    e.preventDefault();
    const st = set(), icon = readIcon(f), name = f.name.value.trim() || a.name;
    if (isLink) {
      const l = st.links.find((x) => `link:${x.id}` === a.id);
      if (l) Object.assign(l, { name, url: normalizeUrl(f.url.value), icon, tile });
    } else {
      const next = { ...(st.apps[a.id] || {}) };
      next.name = name !== (S.apps.find((x) => x.urn === a.id)?.name) ? name : undefined;
      next.url = f.url.value.trim() ? normalizeUrl(f.url.value) : undefined;
      next.urlMode = urlMode || undefined;
      next.icon = icon !== 'auto' ? icon : undefined;
      next.tile = tile || undefined;
      Object.keys(next).forEach((k) => next[k] === undefined && delete next[k]);
      st.apps[a.id] = next;
    }
    save(); renderApps(); renderDock(); d.close();
  };
}
const normalizeUrl = (u) => { u = u.trim(); return /^[a-z]+:\/\//i.test(u) ? u : `http://${u}`; };
function linkDialog() {
  const d = openDialog(`<h2>${svg('link')}Novo atalho</h2><form class="form-grid">
    <label>Nome<input class="text-input" name="name" placeholder="Roteador" required autofocus></label>
    <label>Endereço<input class="text-input" name="url" placeholder="http://192.168.1.1" required></label>
    <label>Ícone${iconPicker('auto')}</label>
    <label>Fundo do ícone${tileOpts('')}</label>
    <div class="dlg-actions"><button type="button" class="pill-btn" data-c="no">Cancelar</button><button type="submit" class="pill-btn primary">Adicionar</button></div></form>`, { closeBtn: false, width: 460 });
  const f = $('form', d.el);
  let tile = '';
  d.el.addEventListener('click', (e) => {
    const t = e.target.closest('[data-tile]');
    if (t) { tile = t.dataset.tile; $$('[data-tile]', d.el).forEach((x) => x.classList.toggle('on', x === t)); }
    if (e.target.closest('[data-c="no"]')) d.close();
  });
  f.slug.addEventListener('input', () => { f.iconMode.value = 'di'; });
  f.onsubmit = (e) => {
    e.preventDefault();
    const id = Math.random().toString(36).slice(2, 10);
    let icon = readIcon(f);
    if (icon === 'auto') icon = 'letter';
    set().links.push({ id, name: f.name.value.trim(), url: normalizeUrl(f.url.value), icon, tile });
    save(); renderApps(); refreshSettings(); d.close();
    toast('Atalho adicionado', f.name.value.trim());
  };
}

/* Uso ao vivo — cartões, gráfico, discos e consumo por app */
let liveOpen = false;
function liveUsageDialog() {
  if (liveOpen) return;
  liveOpen = true;
  let metric = 'cpu', usageTimer, period = 'live', longPts = [], longTimer, listMode = 'apps', procFilter = '';
  const d = openDialog(`<h2>Uso ao vivo</h2>
    <div class="stat-cards">
      <button class="stat-card on" data-m="cpu"><div class="k">CPU</div><div class="v" data-bind="cpu"></div><div class="s" data-bind="cores"></div><div class="track"><i data-bind-w="cpu"></i></div></button>
      <button class="stat-card" data-m="mem"><div class="k">Memória</div><div class="v" data-bind="mem"></div><div class="s" data-bind="memFree"></div><div class="track"><i data-bind-w="mem"></i></div></button>
      <button class="stat-card" data-m="temp"><div class="k">Temperatura</div><div class="v" data-bind="temp"></div><div class="s" data-bind="fan"></div><div class="track"><i data-bind-w="temp"></i></div></button>
      <button class="stat-card" data-m="net"><div class="k">Rede</div><div class="v v-sm" data-bind="rx"></div><div class="s">↑ <span data-bind="tx"></span></div><div class="track"><i data-bind-w="net"></i></div></button>
    </div>
    <div class="spark-box"><div class="lbl"><span data-spark-title></span><span data-spark-extra></span><span class="f-spacer"></span>
      <div class="seg seg-sm"><button class="on" data-period="live">5 min</button><button data-period="24h">24 h</button><button data-period="7d">7 dias</button></div></div><svg class="spark" viewBox="0 0 300 70" preserveAspectRatio="none">
      <defs><linearGradient id="sparkFill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" style="stop-color:var(--brand-lighter);stop-opacity:.45"/><stop offset="1" style="stop-color:var(--brand-lighter);stop-opacity:0"/></linearGradient></defs>
      <path data-area fill="url(#sparkFill)"/><path data-line fill="none" style="stroke:var(--brand-lightest)" stroke-width="1.6" vector-effect="non-scaling-stroke"/><path data-line2 fill="none" style="stroke:rgba(255,255,255,.45)" stroke-width="1.2" stroke-dasharray="3 3" vector-effect="non-scaling-stroke"/></svg></div>
    <div class="live-sec">Discos</div><div class="disk-list"></div>
    <div class="live-sec-row"><span class="live-sec">${'<span data-list-title>Apps</span>'} <small data-list-sub>${S.docker ? 'CPU e memória por app (Docker)' : 'conecte o Docker para ver o consumo por app'}</small></span><span class="f-spacer"></span>
      <input class="text-input sm proc-filter" placeholder="Filtrar" hidden aria-label="Filtrar processos">
      <div class="seg seg-sm"><button class="on" data-list="apps">Apps</button><button data-list="procs">Processos</button></div></div>
    <div class="proc-list"><div class="empty-note">Carregando…</div></div>`, { width: 680, onClose: () => { liveOpen = false; off(); clearTimeout(usageTimer); clearTimeout(longTimer); } });
  const titles = { cpu: 'CPU', mem: 'Memória', temp: 'Temperatura', net: 'Rede (download e upload)' };
  const loadLong = async () => {
    clearTimeout(longTimer);
    if (period === 'live' || !liveOpen) return;
    try { longPts = (await api.statsHistory(period)).points || []; } catch { longPts = []; }
    drawChart();
    longTimer = setTimeout(loadLong, 60000);
  };
  const drawChart = () => {
    let h = period === 'live' ? S.history : longPts;
    if (metric === 'temp') h = h.filter((p) => p.temp);
    if (h.length < 2) {
      $('[data-line]', d.el).setAttribute('d', ''); $('[data-area]', d.el).setAttribute('d', ''); $('[data-line2]', d.el).setAttribute('d', '');
      $('[data-spark-title]', d.el).textContent = `${titles[metric]} · ${period === 'live' ? 'aguardando dados' : 'ainda sem histórico (o BrunOS guarda a média de cada minuto)'}`;
      $('[data-spark-extra]', d.el).textContent = '';
      return;
    }
    const val = (p) => metric === 'net' ? p.rx : p[metric] ?? 0;
    const vals = h.map(val), vals2 = metric === 'net' ? h.map((p) => p.tx) : null;
    const all = vals2 ? vals.concat(vals2) : vals;
    const mx = Math.max(...all), mn = Math.min(...all);
    const span = Math.max(mx - mn, metric === 'cpu' ? 10 : metric === 'mem' ? 5 : metric === 'temp' ? 4 : 50e3);
    const lo = Math.max(0, mn - span * 0.25), hi = mx + span * 0.35;
    const toPath = (arr) => arr.map((v, i) => `${i ? 'L' : 'M'}${((i / Math.max(1, arr.length - 1)) * 300).toFixed(1)} ${(68 - ((v - lo) / (hi - lo)) * 62).toFixed(1)}`).join('');
    const line = toPath(vals);
    $('[data-line]', d.el).setAttribute('d', line);
    $('[data-area]', d.el).setAttribute('d', `${line}L300 70L0 70Z`);
    $('[data-line2]', d.el).setAttribute('d', vals2 ? toPath(vals2) : '');
    const mins = Math.round((h[h.length - 1].t - h[0].t) / 60);
    if (period !== 'live') {
      const unit = metric === 'net' ? '' : metric === 'temp' ? ' °C' : '%';
      const f = (v) => (metric === 'net' ? fmtRate(v) : `${fmt(v, metric === 'cpu' ? 1 : 0)}${unit}`);
      const avg = vals.reduce((s, v) => s + v, 0) / vals.length;
      $('[data-spark-title]', d.el).textContent = `${titles[metric]} · ${period === '24h' ? 'últimas 24 h' : 'últimos 7 dias'}`;
      $('[data-spark-extra]', d.el).textContent = `máx. ${f(mx)} · média ${f(avg)}${metric === 'net' ? ' (download)' : ''}`;
      return;
    }
    $('[data-spark-title]', d.el).textContent = `${titles[metric]} · últimos ${mins || 1} min`;
    $('[data-spark-extra]', d.el).textContent = metric === 'cpu' ? `carga ${statValues().vals.load}` : metric === 'net' ? 'linha tracejada: upload' : metric === 'temp' ? (S.snapshot?.battery ? `bateria ${statValues().vals.battery}` : '') : '';
  };
  let forecast = [], lastDisks = '', smart = null;
  const drawDisks = () => {
    const rows = diskRowsHTML(S.snapshot?.disks || [], forecast, smart);
    if (rows !== lastDisks) { $('.disk-list', d.el).innerHTML = rows; lastDisks = rows; } // não refaz à toa (o botão perderia o clique)
  };
  if (set().diskForecast) api.disks.forecast().then((r) => { forecast = r.disks || []; if (liveOpen) drawDisks(); }).catch(() => { /* sem previsão: só não mostra */ });
  // a leitura roda em segundo plano no servidor: acompanha a cada 2 s até terminar
  const showSmart = (r) => { smart = r; lastDisks = ''; if (liveOpen) drawDisks(); };
  const followSmart = async (r) => {
    showSmart(r);
    for (let i = 0; r.running && i < 150 && liveOpen; i++) { await new Promise((ok) => setTimeout(ok, 2000)); r = await api.smart.get(); showSmart(r); }
    return r;
  };
  const retrySmart = async (s) => {
    try {
      const r = await followSmart(await api.smart.retry(s.key));
      const d = r.disks.find((x) => x.key === s.key);
      if (d?.noSmart) toast(`${s.dev}: continua sem SMART`, 'O adaptador também não aceitou do outro jeito.');
      else toast(`${s.dev}: SMART funcionando`, 'O BrunOS volta a ler a saúde deste disco.');
    } catch (err) { toastError(err, 'Não deu para tentar de novo'); throw err; }
  };
  api.smart.get().then((r) => (r.running ? followSmart(r) : showSmart(r))).catch(() => { /* sem SMART: só não mostra */ });
  const drawProcs = async () => {
    const r = await api.processes();
    if (!liveOpen || listMode !== 'procs') return;
    const s = r.summary;
    $('[data-list-sub]', d.el).textContent = `${s.total} processos · ${s.threads} threads · ${s.running} rodando agora`;
    const q = procFilter.trim().toLowerCase();
    const list = (q ? r.procs.filter((p) => `${p.name} ${p.cmd} ${p.user} ${p.app || ''}`.toLowerCase().includes(q)) : r.procs).slice(0, 80);
    const max = Math.max(5, ...list.map((p) => p.cpu));
    $('.proc-list', d.el).innerHTML = list.length ? list.map((p) => `<div class="proc pr-row" title="${esc(p.cmd || p.name)}\nPID ${p.pid} · ${esc(p.user)} · ${p.threads} thread(s)">
        <span class="pr-name"><b>${esc(p.name)}</b><small>${esc(p.cmd || '')}</small></span>
        ${p.app ? `<span class="pr-app">${esc(p.app)}</span>` : ''}<span class="pr-user">${esc(p.user)}</span>
        <span class="track"><i style="width:${clamp((p.cpu / max) * 100, 0, 100)}%"></i></span><span class="pct">${fmt(p.cpu, 1)}%</span><span class="mem">${fmtBytes(p.mem)}</span></div>`).join('')
      : `<div class="empty-note">${q ? 'Nenhum processo com esse nome' : 'Nenhum processo'}</div>`;
  };
  const drawUsage = async () => {
    try {
      if (listMode === 'procs') { await drawProcs(); if (liveOpen) usageTimer = setTimeout(drawUsage, 3000); return; }
      const list = await api.usage();
      if (listMode !== 'apps') return;
      if (!liveOpen) return;
      const max = Math.max(5, ...list.map((u) => u.cpu));
      $('.proc-list', d.el).innerHTML = list.length ? list.map((u) => {
        const a = u.urn ? appById(u.urn) : null;
        const icon = a ? appIconHTML(a, 'mini') : `<span class="mini" style="--tile:#fff"><img src="assets/icons/tipi.webp" alt=""></span>`;
        return `<div class="proc ${a ? 'clickable' : ''}" ${a ? `data-proc-app="${esc(a.id)}" title="Detalhes, logs e uso de ${esc(a.name)}"` : ''}>${icon}<span class="nm">${esc(a?.name || u.name)}</span><span class="track"><i style="width:${clamp((u.cpu / max) * 100, 0, 100)}%"></i></span><span class="pct">${fmt(u.cpu, 2)}%</span><span class="mem">${fmtBytes(u.memory)}</span></div>`;
      }).join('') : `<div class="empty-note">${S.docker ? 'Nenhum container rodando' : 'Docker não conectado'}</div>`;
    } catch { /* tenta de novo no próximo ciclo */ }
    if (liveOpen) usageTimer = setTimeout(drawUsage, 3000);
  };
  const draw = () => {
    bindStats(d.el);
    const sn = S.snapshot;
    if (sn) { const peak = Math.max(1e6, ...S.history.map((p) => p.rx)); const el = $('[data-bind-w="net"]', d.el); if (el) el.style.width = clamp((sn.net.rx / peak) * 100, 0, 100) + '%'; }
    drawChart(); drawDisks();
  };
  d.el.addEventListener('click', (e) => {
    if (e.target.closest('[data-disk="temp-help"]')) { driveTempHelp(); return; }
    const sb = e.target.closest('[data-smart]'); if (sb) { const s = smart?.disks.find((x) => x.dev === sb.dataset.smart); if (s) smartDialog(s, retrySmart); return; }
    if (e.target.closest('[data-disk="script"]')) { import('./energy.js').then((m) => m.scriptDialog()); return; }
    const sn = e.target.closest('[data-disk="smart-now"]');
    if (sn) {
      sn.disabled = true; sn.classList.add('busy');
      api.smart.refresh().then(followSmart).then((r) => {
        if (r.err) toast('Não deu para ler os discos', r.err, null, { error: true });
        else toast('Discos lidos', r.disks.some((x) => x.noSmart) ? 'Discos com "sem SMART" não são lidos (clique na etiqueta para os detalhes).' : 'Os que estavam parados ficam para a próxima.');
      }).catch((err) => { toastError(err, 'Não deu para ler os discos'); api.smart.get().then(showSmart).catch(() => {}); });
      return;
    }
    const pa = e.target.closest('[data-proc-app]'); if (pa) { openAppDetails(appById(pa.dataset.procApp)); return; }
    const pe = e.target.closest('[data-period]');
    if (pe) { period = pe.dataset.period; $$('[data-period]', d.el).forEach((x) => x.classList.toggle('on', x === pe)); if (period === 'live') { clearTimeout(longTimer); drawChart(); } else loadLong(); return; }
    const lm = e.target.closest('[data-list]');
    if (lm) {
      listMode = lm.dataset.list; $$('[data-list]', d.el).forEach((x) => x.classList.toggle('on', x === lm));
      $('[data-list-title]', d.el).textContent = listMode === 'apps' ? 'Apps' : 'Processos';
      $('[data-list-sub]', d.el).textContent = listMode === 'apps' ? (S.docker ? 'CPU e memória por app (Docker)' : 'conecte o Docker para ver o consumo por app') : 'lendo…';
      $('.proc-filter', d.el).hidden = listMode !== 'procs';
      $('.proc-list', d.el).innerHTML = '<div class="empty-note">Carregando…</div>';
      clearTimeout(usageTimer); drawUsage(); return;
    }
    const c = e.target.closest('[data-m]'); if (!c) return;
    metric = c.dataset.m; $$('[data-m]', d.el).forEach((x) => x.classList.toggle('on', x === c)); drawChart();
  });
  $('.proc-filter', d.el).addEventListener('input', debounce((e) => { procFilter = e.target.value; clearTimeout(usageTimer); drawUsage(); }, 250));
  const off = on('stats', draw);
  draw(); drawUsage();
}

/* Paleta de comandos (Ctrl+K) */
function commandPalette() {
  if ($('.cmdk')) return;
  const actions = [
    { icon: 'folder', label: 'Abrir Arquivos', run: () => openSheet('files', $('[data-sys="files"]')) },
    { icon: 'sliders', label: 'Abrir Configurações', run: () => openSheet('settings', $('[data-sys="settings"]')) },
    { icon: 'grid', label: 'Abrir Widgets (criar e editar)', run: () => openWidgets() },
    { icon: 'plus', label: 'Adicionar widget…', run: () => openWidgets({ add: page }) },
    { icon: 'sort', label: 'Organizar widgets', run: () => setWidgetEditing(true) },
    { icon: 'activity', label: 'Uso ao vivo', run: () => liveUsageDialog() },
    { icon: 'info', label: 'Informações do dispositivo', run: () => deviceDialog() },
    { icon: 'bell', label: 'Notificações', run: () => toggleNotifications() },
    { icon: 'power', label: 'Energia e desligamento seguro', run: () => openPower() },
    { icon: 'battery', label: 'Bateria e quedas de energia', run: () => openBattery() },
    { icon: 'globe', label: 'Internet: latência e quedas', run: () => internetDialog() },
    { icon: 'cpu', label: 'Processos do servidor', run: () => { liveUsageDialog(); setTimeout(() => $('[data-list="procs"]')?.click(), 50); } },
    { icon: 'link', label: 'Novo atalho…', run: () => linkDialog() },
    { icon: 'image', label: 'Próximo papel de parede', run: () => { const ids = BUILTIN_WALLPAPERS.map((i) => `builtin:${i}`); set().wallpaper = ids[(ids.indexOf(set().wallpaper) + 1) % ids.length]; save(); setWallpaper(set().wallpaper); } },
    { icon: 'upload', label: 'Enviar papel de parede…', run: () => $('#wp-file').click() },
    { icon: 'palette', label: 'Cor de destaque: automática', run: () => { set().accentMode = 'auto'; save(); applyBrand(); } },
    { icon: 'sparkles', label: 'Cor de destaque: vibrante', run: () => { set().accentMode = 'vibrant'; save(); applyBrand(); } },
    { icon: 'box', label: 'Abrir o Runtipi', run: () => SYS.runtipi.run() },
    { icon: 'terminal', label: 'Abrir o terminal (SSH)', hidden: S.terminal === false, run: () => openTerminal() },
    // Comandos salvos: digitam na Aba atual do Terminal (só com a janela aberta)
    ...(termMod?.paletteActions?.() || []),
    { icon: 'arrowUp', label: `Apps com atualização${S.updates?.apps.length ? ` (${S.updates.apps.length})` : ''}`, run: () => appUpdatesDialog() },
    { icon: 'refresh', label: 'Atualizar o Linux no Terminal', hidden: S.terminal === false, run: () => openTerminal({ typeCmd: 'sudo apt update && sudo apt upgrade' }) },
    { icon: 'bolt', label: 'Consumo e custo do servidor', run: () => openPower() },
    { icon: 'lock', label: 'Bloquear a tela', run: () => lockNow() },
    { icon: 'logout', label: 'Sair', run: () => logout() },
  ].filter((a) => !a.hidden);
  const d = openDialog(`<div class="cmdk-input">${svg('search')}<input placeholder="Pesquisar apps, ações e arquivos…" aria-label="Pesquisar"></div><div class="cmdk-list"></div>`, { cls: 'cmdk', wrapCls: 'cmdk-wrap', closeBtn: false });
  const input = $('input', d.el), list = $('.cmdk-list', d.el);
  let idx = 0, flat = [];
  const draw = () => {
    const q = input.value.trim().toLowerCase();
    const apps = allApps().filter((a) => !q || a.name.toLowerCase().includes(q));
    const acts = actions.filter((a) => !q || a.label.toLowerCase().includes(q));
    const fileAct = q && S.roots.length ? [{ icon: 'search', label: `Procurar “${input.value.trim()}” nos arquivos`, run: () => openSheet('files', $('[data-sys="files"]'), { search: input.value.trim() }) }] : [];
    flat = [...(q ? apps.map((a) => ({ app: a })) : []), ...acts.map((a) => ({ act: a })), ...fileAct.map((a) => ({ act: a }))];
    idx = clamp(idx, 0, Math.max(0, flat.length - 1));
    let i = -1;
    const item = (inner) => { i++; return `<button class="cmdk-item ${i === idx ? 'on' : ''}" data-i="${i}">${inner}</button>`; };
    list.innerHTML = `
      ${!q ? `<div class="cmdk-sec">Apps</div><div class="cmdk-top">${allApps().filter((a) => !a.o.hidden).slice(0, 8).map((a) => `<a href="${esc(appUrl(a) || '#')}" target="_blank" rel="noopener noreferrer" data-app="${esc(a.id)}">${appIconHTML(a)}<span>${esc(a.name)}</span></a>`).join('')}</div>` : ''}
      ${q && apps.length ? `<div class="cmdk-sec">Apps</div>${apps.map((a) => item(`${appIconHTML(a, 'mini')}${esc(a.name)}<kbd>Abrir ↵</kbd>`)).join('')}` : ''}
      ${acts.length ? `<div class="cmdk-sec">Ações</div>${acts.map((a) => item(`<span class="mini sys">${svg(a.icon)}</span>${esc(a.label)}`)).join('')}` : ''}
      ${fileAct.length ? `<div class="cmdk-sec">Arquivos</div>${fileAct.map((a) => item(`<span class="mini sys">${svg(a.icon)}</span>${esc(a.label)}`)).join('')}` : ''}
      ${!flat.length && q ? '<div class="empty-note">Nada encontrado.</div>' : ''}`;
    $('.cmdk-item.on', list)?.scrollIntoView({ block: 'nearest' });
  };
  const run = (k) => { const it = flat[k]; if (!it) return; d.close(); setTimeout(() => it.app ? openApp(it.app) : it.act.run(), 60); };
  input.addEventListener('input', () => { idx = 0; draw(); });
  input.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') { idx = Math.min(idx + 1, flat.length - 1); draw(); e.preventDefault(); }
    else if (e.key === 'ArrowUp') { idx = Math.max(idx - 1, 0); draw(); e.preventDefault(); }
    else if (e.key === 'Enter') { run(idx); e.preventDefault(); }
  });
  list.addEventListener('click', (e) => {
    const b = e.target.closest('[data-i]'); if (b) { run(+b.dataset.i); return; }
    const a = e.target.closest('[data-app]');
    if (a) { const app = appById(a.dataset.app); if (!appUrl(app)) { e.preventDefault(); openApp(app); } d.close(); }
  });
  draw();
  setTimeout(() => input.focus(), 30);
}

/* ── Login, primeira execução e sessão ─────────────────────────── */
function renderLogin(mode) {
  const f = $('#login-form'), fields = $('.login-fields', f);
  $('#login-error').textContent = '';
  if (mode === 'setup') {
    $('#login-title').textContent = 'Vamos configurar o BrunOS';
    $('#login-sub').textContent = 'Crie o usuário que vai entrar neste painel';
    fields.innerHTML = `
      <input class="login-input" name="displayName" placeholder="Seu nome (ex.: Bruno)" autocomplete="name" maxlength="40">
      <input class="login-input" name="username" placeholder="Usuário" autocomplete="username" required pattern="[a-zA-Z0-9._\\-]{3,32}" title="3 a 32 letras, números, ponto, hífen ou sublinhado">
      <div class="pw-field"><input name="password" type="password" placeholder="Senha (mín. 8 caracteres)" autocomplete="new-password" required minlength="8"><button type="button" class="pw-eye" aria-label="Mostrar senha">${svg('eye')}</button></div>
      <div class="strength"><i></i></div>
      <div class="pw-field"><input name="confirm" type="password" placeholder="Repita a senha" autocomplete="new-password" required minlength="8"></div>`;
    $('#login-btn').textContent = 'Criar e entrar';
    $('#login-hint').textContent = 'A senha fica guardada só neste servidor, protegida por hash (PBKDF2).';
    const pw = f.password, bar = $('.strength i', f);
    pw.addEventListener('input', () => {
      const v = pw.value; let sc = 0;
      if (v.length >= 8) sc++; if (v.length >= 12) sc++; if (/[A-Z]/.test(v) && /[a-z]/.test(v)) sc++; if (/\d/.test(v)) sc++; if (/[^A-Za-z0-9]/.test(v)) sc++;
      bar.style.width = (sc / 5) * 100 + '%'; bar.dataset.level = sc < 2 ? 'low' : sc < 4 ? 'mid' : 'high';
    });
    setTimeout(() => f.displayName.focus(), 300);
  } else {
    const name = displayName();
    $('#login-title').textContent = name ? `Boas-vindas de volta, ${name.split(' ')[0]}` : 'Boas-vindas de volta';
    $('#login-sub').textContent = 'Digite seu usuário e senha do BrunOS';
    let lastUser = ''; try { lastUser = localStorage.getItem('brunos.user') || ''; } catch { /* sem storage */ }
    fields.innerHTML = `
      <input class="login-input" name="username" placeholder="Usuário" autocomplete="username" required value="${esc(lastUser)}">
      <div class="pw-field"><input name="password" type="password" placeholder="Senha" autocomplete="current-password" required><button type="button" class="pw-eye" aria-label="Mostrar senha">${svg('eye')}</button></div>`;
    $('#login-btn').textContent = 'Entrar';
    $('#login-hint').textContent = api.demo ? 'Demonstração: qualquer usuário e senha funcionam.' : '';
    setTimeout(() => (lastUser ? f.password : f.username).focus(), 300);
  }
  f.dataset.mode = mode;
}
$('#login-form').addEventListener('click', (e) => {
  const eye = e.target.closest('.pw-eye'); if (!eye) return;
  const input = eye.previousElementSibling, show = input.type === 'password';
  input.type = show ? 'text' : 'password';
  eye.innerHTML = svg(show ? 'eyeOff' : 'eye');
});
$('#login-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const f = e.currentTarget, err = $('#login-error'), btn = $('#login-btn');
  err.textContent = '';
  const fail = (msg) => { err.textContent = msg; const l = $('#login'); l.classList.remove('shake'); void l.offsetWidth; l.classList.add('shake'); };
  btn.disabled = true; btn.classList.add('busy');
  try {
    if (f.dataset.mode === 'setup') {
      if (f.password.value !== f.confirm.value) { fail('As senhas não são iguais.'); return; }
      await api.setup({ displayName: f.displayName.value.trim(), username: f.username.value.trim(), password: f.password.value });
      S.pub.setupRequired = false;
    } else {
      await api.login({ username: f.username.value.trim(), password: f.password.value });
    }
    try { localStorage.setItem('brunos.user', f.username.value.trim()); } catch { /* ok */ }
    await enterDesktop(true);
  } catch (x) { fail(x.message); }
  finally { btn.disabled = false; btn.classList.remove('busy'); }
});
async function logout() {
  try { await api.logout(); } catch { /* sessão já expirada */ }
  lock();
}
function lock() {
  hideLock(true);
  dialogs.slice().forEach((c) => c());
  closeSheet(); closeMenu(); closePanel(); paintBanner(null);
  if (wEditing) { wEditing = false; document.body.classList.remove('w-editing'); $('#w-done')?.remove(); }
  S.stopStream?.(); S.stopStream = null;
  S.desktopReady = false; S.settingsLoaded = false;
  document.body.classList.add('is-locked');
  renderLogin('login');
}
on('unauthorized', () => { if (!document.body.classList.contains('is-locked') || isScreenLocked()) { lock(); toast('Sua sessão expirou', 'Entre de novo para continuar.'); } });

// Tela de bloqueio (J1): o que ela precisa saber do resto da interface
function weatherNow() {
  const it = (S.integrations || []).find((x) => x.kind === 'weather'), v = it && S.metrics[it.id]?.values;
  if (!v?.temp) return null;
  return { temp: `${fmt(v.temp.n, 0)}°`, icon: v.cond?.i, text: v.cond?.t || '' };
}
initLock({
  settings: () => set(),
  greeting: () => greeting(),
  extra: () => ({ weather: weatherNow(), unread: +($('#bell .bell-badge:not([hidden])')?.textContent || 0) }),
  active: () => !!S.desktopReady && !document.body.classList.contains('is-locked'),
  onLocked: (mode) => {
    closeMenu();
    // com senha o servidor para de mandar dados: fecha a conexão ao vivo até desbloquear
    if (mode === 'senha') { S.stopStream?.(); S.stopStream = null; }
  },
  onUnlocked: async (mode) => {
    if (mode !== 'senha') return;
    if (!S.desktopReady) { try { await enterDesktop(true); } catch (e) { toastError(e); } return; }
    startStream(); loadApps(); emit('unlocked');
  },
  onEnded: (msg) => { lock(); toast('Sessão encerrada', msg || 'Entre de novo com usuário e senha.'); },
});

async function loadApps() {
  try {
    const r = await api.apps();
    S.apps = r.apps; S.tipi = r.runtipi || {}; S.docker = r.docker; S.dockerActions = r.dockerActions;
    renderApps(); renderDock();
  } catch (e) { if (!/login|sessão/.test(e.message)) toastError(e, 'Não foi possível carregar os apps'); }
}
// Conexão ao vivo: estatísticas, avisos, valores das integrações e notas mudadas, tudo numa só
function startStream() {
  S.stopStream?.();
  S.stopStream = api.streamStats(
    // init chega ao conectar e a cada reconexão: relê avisos e a contagem para não perder nada
    (d) => {
      S.snapshot = d.snapshot; S.history = d.history || [];
      for (const v of d.metrics || []) S.metrics[v.id] = v;
      bindStats(); paintWidgets(); emit('stats'); refreshNotifications(); refreshPowerBanner(); refreshNotes();
    },
    (d) => { S.snapshot = d.snapshot; S.history.push(d.point); if (S.history.length > 150) S.history.shift(); bindStats(); paintWidgets(); emit('stats'); },
    (e) => { onNotify(e); if (e.kind.startsWith('power-shutdown')) refreshPowerBanner(); if (e.kind.startsWith('app-')) loadApps(); },
    // valores novos de uma integração (o servidor só manda quando busca)
    (m) => { if (m.removed) delete S.metrics[m.id]; else S.metrics[m.id] = m; paintWidgets(); emit('metrics'); },
    { onNote: noteChanged, onLocked: () => showLock('senha') },
  );
}
// Preferências do servidor. Uma falha passageira (rede acordando, proxy reiniciando) ganha mais
// tentativas; se continuar falhando, a entrada para com erro (NUNCA entra com o padrão).
async function fetchSettings() {
  for (let i = 0; ; i++) {
    try {
      const s = await api.settings();
      if (s && typeof s === 'object' && !Array.isArray(s)) return s;
      throw Object.assign(new Error('O servidor mandou as preferências num formato estranho.'), { status: 502 });
    } catch (e) {
      if (e.status === 401 || e.status === 423 || i >= 3) throw e;
      await new Promise((r) => setTimeout(r, 1500 * (i + 1)));
    }
  }
}
async function enterDesktop(animate) {
  S.settingsLoaded = false;
  const [me, settings, system, roots, wallpapers] = await Promise.all([
    api.me(), fetchSettings(), api.system().catch(() => null), api.files.roots().catch(() => []), api.wallpapers().catch(() => []),
  ]);
  S.me = me; S.system = system; S.roots = roots; S.wallpapers = wallpapers;
  S.settings = { ...structuredClone(DEFAULTS), ...settings };
  S.settingsLoaded = true;
  if (system) { S.tipi = system.runtipi || {}; S.docker = system.docker; S.dockerActions = system.dockerActions; S.terminal = system.terminal !== false; }
  W2(); // converte os widgets da 0.8.x, se for a primeira vez
  await loadIntegrations();
  const sched = scheduledWallpaper(); // papel de parede por horário: já entra com o da hora certa
  if (sched) set().wallpaper = sched;
  applyPrefs();
  setWallpaper(set().wallpaper, { instant: !animate });
  renderDock();
  await loadApps();
  refreshInternet(); refreshEnergy(); refreshUpdates();
  startStream();
  S.desktopReady = true;
  if (S.terminal) api.term.list().then((l) => { S.termTabs = l.length; renderDock(); }).catch(() => {}); // Abas que ficaram abertas
  // estado dos apps (bolinhas) atualizado a cada minuto, junto com o monitor do servidor
  clearInterval(S.appsTimer);
  S.appsTimer = setInterval(() => { if (!document.body.classList.contains('is-locked') && !document.hidden) { loadApps(); refreshInternet(); } }, 60000);
  document.body.classList.remove('is-locked');
  if (animate) { document.body.classList.add('just-unlocked'); setTimeout(() => document.body.classList.remove('just-unlocked'), 1600); }
  if (!set().welcomed) {
    set().welcomed = true; save();
    setTimeout(() => toast('Tudo pronto!', 'Clique com o botão direito nos ícones para personalizar, ou solte uma foto na tela para trocar o papel de parede.', { label: 'Configurar', run: () => openSheet('settings', $('[data-sys="settings"]')) }), 1300);
  } else if (api.demo) {
    setTimeout(() => toast('Modo demonstração', 'Sem servidor, os dados são simulados. No Runtipi tudo fica real.'), 1300);
  }
}

/* ── Preferências visuais ──────────────────────────────────────── */
function applyPrefs() {
  const st = set(), b = document.body, r = document.documentElement.style;
  b.classList.toggle('no-glass', !st.glass);
  b.classList.toggle('reduce-motion', st.reduceMotion);
  r.setProperty('--glass-blur', st.blur + 'px');
  applyBrand(); renderGreeting(); renderWidgets(); renderApps();
}

/* ── Eventos globais ───────────────────────────────────────────── */
document.addEventListener('keydown', (e) => {
  if (document.body.classList.contains('is-locked') || isScreenLocked()) return;
  // Ctrl+Shift+L bloqueia a tela (vale até dentro do terminal)
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key.toLowerCase() === 'l') { e.preventDefault(); lockNow(); return; }
  if (e.target.closest?.('.xterm')) return; // dentro do terminal, Esc e Ctrl+K são do shell
  if (e.key === 'Escape' && e.target.closest?.('.cm-editor')) return; // no editor, Esc fecha a busca dele
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); commandPalette(); }
  else if (e.key === 'Escape') {
    if (menuOpen()) closeMenu();
    else if (notificationsOpen()) closePanel();
    else if (dialogs.length) dialogs[dialogs.length - 1]();
    else if (currentSheet) closeSheet();
    else if (wEditing) setWidgetEditing(false);
  }
});
$('#search-pill').addEventListener('click', commandPalette);
$('#desktop').addEventListener('click', (e) => {
  const rm = e.target.closest('[data-wremove]'); if (rm) { deleteWidget(rm.dataset.wremove); return; }
  const add = e.target.closest('[data-wadd]'); if (add) { openWidgets({ add: +add.dataset.wadd }); return; }
  const wc = e.target.closest('[data-wcols]'); if (wc) { setAreaCols(+wc.dataset.wpage, wc.dataset.wcols === 'auto' ? 'auto' : +wc.dataset.wcols); return; }
  if (e.target.closest('[data-wresize]')) return;
  const fav = e.target.closest('[data-fav-root]');
  if (fav) { if (!wEditing) openSheet('files', $('[data-sys="files"]'), { root: fav.dataset.favRoot, path: fav.dataset.favPath }); return; }
  const w = e.target.closest('.widget-wrap[data-wid] > .widget');
  if (w) { if (wEditing) openWidgets({ edit: w.parentElement.dataset.wid }); else widgetClick(w.parentElement.dataset.wid); }
});
// segurar o dedo num widget (celular) abre o mesmo menu do botão direito
(() => {
  let timer = 0;
  $('#desktop').addEventListener('touchstart', (e) => {
    const w = e.target.closest('.widget-wrap[data-wid]'); if (!w || wEditing) return;
    const t = e.touches[0];
    timer = setTimeout(() => { timer = 0; widgetMenu(w.dataset.wid, t.clientX, t.clientY); }, 550);
  }, { passive: true });
  const cancel = () => { clearTimeout(timer); timer = 0; };
  $('#desktop').addEventListener('touchend', cancel);
  $('#desktop').addEventListener('touchmove', cancel, { passive: true });
})();
// Arrastar widgets (sempre, como os ícones): muda a ordem e, soltando em outra página, a página.
(() => {
  const desk = $('#desktop');
  let dragId = null, edgeTimer = 0;
  const clear = () => $$('.w-drop-before,.w-drop-area', desk).forEach((x) => x.classList.remove('w-drop-before', 'w-drop-area'));
  desk.addEventListener('dragstart', (e) => {
    const w = e.target.closest?.('.widget-wrap[data-wid]'); if (!w) return;
    if (e.target.closest?.('[data-wresize]')) { e.preventDefault(); return; }
    dragId = w.dataset.wid; w.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/x-brunos-widget', dragId);
  });
  desk.addEventListener('dragover', (e) => {
    if (!dragId) return;
    e.preventDefault();
    clear();
    const w = e.target.closest('.widget-wrap[data-wid]'), area = e.target.closest('[data-area]') || e.target.closest('.app-page');
    if (w && w.dataset.wid !== dragId) w.classList.add('w-drop-before');
    else if (area) area.classList.add('w-drop-area');
    const grid = $('#app-grid');
    if (grid.classList.contains('paged')) {
      const edge = e.clientX < 40 ? -1 : e.clientX > innerWidth - 40 ? 1 : 0;
      if (edge && !edgeTimer) edgeTimer = setTimeout(() => { edgeTimer = 0; goPage(page + edge); }, 650);
      if (!edge) { clearTimeout(edgeTimer); edgeTimer = 0; }
    }
  });
  desk.addEventListener('drop', (e) => {
    if (!dragId) return;
    e.preventDefault();
    const id = dragId, w = e.target.closest('.widget-wrap[data-wid]'), area = e.target.closest('[data-area]'), pg = e.target.closest('.app-page');
    dragId = null; clear();
    // alvo: a Área onde soltou, ou a página (inclusive uma página só de apps ou a página nova)
    const to = area ? +area.dataset.area : pg ? +pg.dataset.real : null;
    if (to == null) { renderApps(); return; }
    fixLoosePlaces();
    putWidget(id, W2().items[id], to, w && w.dataset.wid !== id ? w.dataset.wid : null);
    compactPages(); save(); renderApps();
  });
  desk.addEventListener('dragend', () => { dragId = null; clearTimeout(edgeTimer); edgeTimer = 0; clear(); $$('.widget-wrap.dragging', desk).forEach((x) => x.classList.remove('dragging')); });
})();
// Alça de tamanho (modo Organizar): arrastar o canto pula entre os 5 Tamanhos, conforme os quadros
(() => {
  const desk = $('#desktop');
  let rs = null;
  desk.addEventListener('pointerdown', (e) => {
    const h = e.target.closest('[data-wresize]'); if (!h) return;
    e.preventDefault(); e.stopPropagation();
    const wrap = h.closest('.widget-wrap'), area = wrap.closest('[data-area]'), cs = getComputedStyle(area), r = wrap.getBoundingClientRect();
    const tracks = cs.gridTemplateColumns.split(' ').filter(Boolean);
    rs = { id: h.dataset.wresize, wrap, x: e.clientX, y: e.clientY, w: r.width, h: r.height, cols: tracks.length,
      colW: parseFloat(tracks[0]) || 240, gapX: parseFloat(cs.columnGap) || 0, rowH: parseFloat(cs.gridAutoRows) || 174, gapY: parseFloat(cs.rowGap) || 0,
      size: widgetById(h.dataset.wresize)?.size };
    wrap.draggable = false; wrap.classList.add('resizing');
    h.setPointerCapture(e.pointerId);
  });
  desk.addEventListener('pointermove', (e) => {
    if (!rs) return;
    const c = clamp(Math.round((rs.w + e.clientX - rs.x + rs.gapX) / (rs.colW + rs.gapX)), 1, Math.max(1, rs.cols));
    const r = clamp(Math.round((rs.h + e.clientY - rs.y + rs.gapY) / (rs.rowH + rs.gapY)), 1, 2);
    const size = sizeFor(c, r);
    if (size === rs.size) return;
    rs.size = size;
    rs.wrap.style.gridColumn = ''; rs.wrap.style.gridRow = '';
    rs.wrap.style.cssText += spanStyle(size, rs.cols);
    rs.wrap.className = rs.wrap.className.replace(/wsz-\w/, `wsz-${size}`);
  });
  const end = () => {
    if (!rs) return;
    const { id, size } = rs;
    rs = null;
    if (W2().items[id] && size && W2().items[id].size !== size) { W2().items[id].size = size; save(); }
    renderApps();
  };
  desk.addEventListener('pointerup', end);
  desk.addEventListener('pointercancel', end);
})();
$('#desktop').addEventListener('contextmenu', (e) => {
  const w = e.target.closest('.widget-wrap[data-wid]');
  if (w) { e.preventDefault(); widgetMenu(w.dataset.wid, e.clientX, e.clientY); return; }
  if (e.target.closest('.app, input, a')) return;
  e.preventDefault();
  contextMenu(e.clientX, e.clientY, [
    { icon: 'image', label: 'Trocar papel de parede…', run: () => { settingsTab = 'look'; openSheet('settings', $('[data-sys="settings"]')); } },
    { icon: 'upload', label: 'Enviar papel de parede…', run: () => $('#wp-file').click() },
    { icon: 'plus', label: 'Adicionar widget…', run: () => openWidgets({ add: page }) },
    { icon: 'sort', label: wEditing ? 'Concluir organização dos widgets' : 'Organizar widgets', run: () => setWidgetEditing(!wEditing) },
    { icon: 'link', label: 'Novo atalho…', run: () => linkDialog() },
    { icon: 'refresh', label: 'Atualizar apps', run: () => loadApps() },
    '-',
    { icon: 'lock', label: 'Bloquear a tela', kbd: 'Ctrl+Shift+L', run: () => lockNow() },
    { icon: 'logout', label: 'Sair', run: () => logout() },
  ]);
});
$('#wp-file').addEventListener('change', (e) => { uploadWallpaper(e.target.files[0]); e.target.value = ''; });
let dragDepth = 0;
const isFileDrag = (e) => [...(e.dataTransfer?.types || [])].includes('Files');
const wallpaperDropAllowed = () => !document.body.classList.contains('is-locked') && !currentSheet && !dialogs.length;
addEventListener('dragenter', (e) => { if (isFileDrag(e) && wallpaperDropAllowed()) { dragDepth++; document.body.classList.add('dragover'); } });
addEventListener('dragleave', (e) => { if (isFileDrag(e) && --dragDepth <= 0) { dragDepth = 0; document.body.classList.remove('dragover'); } });
addEventListener('dragover', (e) => { if (isFileDrag(e) && wallpaperDropAllowed()) e.preventDefault(); });
addEventListener('drop', (e) => {
  if (!isFileDrag(e) || !wallpaperDropAllowed()) return;
  e.preventDefault(); dragDepth = 0; document.body.classList.remove('dragover');
  uploadWallpaper(e.dataTransfer.files[0]);
});
let rsz;
addEventListener('resize', () => { clearTimeout(rsz); rsz = setTimeout(() => { renderApps(); sizeDock(); }, 120); });
setInterval(() => { renderGreeting(); if (!document.body.classList.contains('is-locked')) applySchedule(); }, 60_000);
document.addEventListener('visibilitychange', () => { if (!document.hidden && !document.body.classList.contains('is-locked')) loadApps(); });

/* ── Início ────────────────────────────────────────────────────── */
(async function boot() {
  S.pub = await initApi();
  S.settings = { ...structuredClone(DEFAULTS), ...(S.pub.wallpaper ? { wallpaper: S.pub.wallpaper } : {}), ...(S.pub.accentMode ? { accentMode: S.pub.accentMode } : {}), ...(S.pub.manualHue != null ? { manualHue: S.pub.manualHue } : {}) };
  setWallpaper(set().wallpaper, { instant: true, pub: true });
  if (S.pub.setupRequired) { renderLogin('setup'); return; }
  try { await enterDesktop(false); }
  catch (e) {
    // Sessão bloqueada no servidor (recarregou a página com a tela bloqueada): pede só a senha
    // (sem montar o login por trás: ele puxaria o foco do campo da senha)
    if (e.status === 423) { showLock('senha'); return; }
    renderLogin('login');
    // logado, mas sem conseguir ler as preferências: avisa (sem isso pareceria só "sessão expirada")
    if (e.status && e.status !== 401) toastError(e, 'Não consegui carregar suas preferências');
  }
})();
