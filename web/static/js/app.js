// BrunOS — casca da interface: login, área de trabalho, dock, widgets, configurações.
import { batteryWidgetHTML, paintBattery, batteryDialog } from './battery.js';
import { api, initApi } from './api.js';
import {
  $, $$, esc, html, clamp, fmt, fmtBytes, fmtRate, fmtDuration, debounce, on, emit, svg, folderSVG,
  toast, toastError, contextMenu, closeMenu, menuOpen, dialogs, openDialog, confirmDialog,
} from './ui.js';

/* ── Estado ────────────────────────────────────────────────────── */
const BUILTIN_WALLPAPERS = ['24', '2', '23', '19', '16', '14', '4', '1', '12', '6', '13', '10'];
const BUILTIN_ICONS = ['adguard-home', 'cloudflare', 'dashdot', 'filebrowser', 'homarr', 'immich', 'nextcloud', 'plex', 'qbittorrent', 'speedtest-tracker', 'syncthing', 'tailscale', 'tipi', 'transmission', 'vaultwarden', 'watchyourlan'];
const DEFAULTS = {
  wallpaper: 'builtin:24', accentMode: 'auto', manualHue: 209, glass: true, blur: 24, iconSize: 64,
  widgets: ['files', 'storage', 'system'], showStatus: true, reduceMotion: false, openIn: 'tab',
  urlMode: 'auto', serverHost: '', runtipiUrl: '', dock: [], order: [], apps: {}, links: [], favorites: [], welcomed: false,
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
function openTerminal() {
  import('./terminal.js').then((m) => m.terminalWindow({ settings: set(), saveSetting: (k, v) => { set()[k] = v; save(); }, defaultHost: S.tipi.internalIp || location.hostname }))
    .catch((e) => toastError(e, 'Não deu para abrir o terminal'));
}
function openUrl(href) { const l = document.createElement('a'); l.href = href; l.target = '_blank'; l.rel = 'noopener'; l.click(); }
const S = {
  pub: {}, me: null, settings: structuredClone(DEFAULTS), apps: [], tipi: {}, docker: false, dockerActions: false,
  system: null, snapshot: null, history: [], roots: [], wallpapers: [], localWallpaper: null, stopStream: null,
};
const set = () => S.settings;
const saveNow = async () => { try { await api.saveSettings(S.settings); } catch (e) { toastError(e, 'Não foi possível salvar as preferências'); } };
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
  const lbl = $('[data-brand-label]'); if (lbl) lbl.textContent = `hsl(${b.h} ${b.s}% ${b.l}%)`;
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

/* ── Widgets ───────────────────────────────────────────────────── */
const WIDGETS = {
  files: { label: 'Arquivos', title: 'Atalhos de pastas' },
  storage: { label: 'Armazenamento', title: 'Espaço em disco' },
  system: { label: 'Sistema', title: 'CPU, memória e temperatura' },
  network: { label: 'Rede', title: 'Download e upload' },
  battery: { label: 'Bateria', title: 'Carga, tomada, autonomia e saúde da bateria' },
};
const MAX_WIDGETS = 6;
function favorites() {
  const fav = set().favorites.filter((f) => S.roots.some((r) => r.id === f.root));
  if (fav.length) return fav.slice(0, 4);
  return S.roots.slice(0, 4).map((r) => ({ root: r.id, path: '', name: r.name }));
}
function storageTotals() {
  const disks = S.snapshot?.disks || [];
  return disks.reduce((acc, d) => ({ used: acc.used + d.used, total: acc.total + d.total }), { used: 0, total: 0 });
}
function renderWidgets() {
  const row = $('#widgets');
  const ws = set().widgets.filter((w) => WIDGETS[w]);
  row.style.display = ws.length ? '' : 'none';
  row.innerHTML = ws.map((w, i) => {
    let body = '';
    if (w === 'files') {
      const fav = favorites();
      body = `<div class="widget"><div class="w-files">${fav.length ? fav.map((f) => `<button class="cell cell-btn" data-fav-root="${esc(f.root)}" data-fav-path="${esc(f.path)}" title="${esc(f.name)}">${folderSVG()}<span>${esc(f.name)}</span></button>`).join('') : '<span class="w-empty">Nenhuma pasta disponível</span>'}</div></div>`;
    } else if (w === 'storage') {
      body = `<button class="widget" data-action="live"><span class="w-caption">Armazenamento</span>
        <span class="w-value"><span data-bind="storageUsed">—</span><span class="w-unit" data-bind="storageTotal"></span></span>
        <span class="w-sub" data-bind="storageFree"></span><span class="track white"><i data-bind-w="storage"></i></span></button>`;
    } else if (w === 'system') {
      body = `<button class="widget" data-action="live"><span class="w-stats">
        <span class="cell">${svg('cpu')}<span class="k">CPU</span><span class="v" data-bind="cpu">—</span></span>
        <span class="cell">${svg('memory')}<span class="k">Memória</span><span class="v" data-bind="memShort">—</span></span>
        <span class="cell">${svg('temp')}<span class="k">Temp.</span><span class="v" data-bind="temp">—</span></span></span></button>`;
    } else if (w === 'network') {
      body = `<button class="widget" data-action="live"><span class="w-net">
        <span class="cell"><span class="k">${svg('download')} Download</span><span class="v" data-bind="rx">—</span></span>
        <span class="cell"><span class="k">${svg('upload')} Upload</span><span class="v" data-bind="tx">—</span></span></span></button>`;
    } else if (w === 'battery') {
      body = batteryWidgetHTML();
    }
    return `<div class="widget-wrap" style="--i:${i}">${body}<div class="widget-label">${WIDGETS[w].label}</div></div>`;
  }).join('');
  bindStats();
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
function renderApps() {
  const grid = $('#app-grid');
  const appW = Math.min(Math.round(set().iconSize * 1.8), Math.floor((innerWidth - 40) / 4));
  document.documentElement.style.setProperty('--app-w', appW + 'px');
  document.documentElement.style.setProperty('--app-icon', Math.min(set().iconSize, Math.round(appW * 0.68)) + 'px');
  const apps = allApps().filter((a) => !a.o.hidden);
  if (!apps.length) {
    grid.innerHTML = `<div class="grid-empty">${S.tipi.found === false ? 'Não encontrei a pasta do Runtipi. Confira a montagem <code>/runtipi</code> no docker-compose.' : 'Nenhum app ainda.'}<br><button class="pill-btn" data-action="add-link">${svg('plus')}Adicionar atalho</button></div>`;
    return;
  }
  grid.innerHTML = apps.map((a, i) => {
    const url = appUrl(a);
    const dot = set().showStatus && a.state !== 'unknown' ? `<i class="status-dot ${a.state}" title="${{ running: 'Rodando', stopped: 'Parado', partial: 'Parcialmente rodando' }[a.state] || ''}"></i>` : '';
    return `<a class="app" role="listitem" href="${esc(url || '#')}" ${set().openIn === 'tab' ? 'target="_blank"' : ''} rel="noopener noreferrer" data-app="${esc(a.id)}" draggable="true" style="--i:${i}" title="${esc(a.name)}${url ? '\n' + esc(url) : ''}">
      ${appIconHTML(a, 'app-icon', dot)}<span class="app-name">${esc(a.name)}</span></a>`;
  }).join('');
}
(() => {
  const grid = $('#app-grid');
  grid.addEventListener('click', (e) => {
    if (e.target.closest('[data-action="add-link"]')) { linkDialog(); return; }
    const el = e.target.closest('.app'); if (!el) return;
    const a = appById(el.dataset.app);
    if (!appUrl(a)) { e.preventDefault(); openApp(a); }
  });
  grid.addEventListener('contextmenu', (e) => {
    const el = e.target.closest('.app'); if (!el) return;
    e.preventDefault();
    appMenu(appById(el.dataset.app), e.clientX, e.clientY, el);
  });
  let dragId = null;
  grid.addEventListener('dragstart', (e) => {
    const el = e.target.closest('.app'); if (!el) return;
    dragId = el.dataset.app; el.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/x-brunos-app', dragId);
  });
  grid.addEventListener('dragover', (e) => {
    if (!dragId) return;
    e.preventDefault();
    const el = e.target.closest('.app');
    $$('.drop-before', grid).forEach((x) => x.classList.remove('drop-before'));
    if (el && el.dataset.app !== dragId) el.classList.add('drop-before');
  });
  grid.addEventListener('drop', (e) => {
    if (!dragId) return;
    e.preventDefault(); e.stopPropagation();
    const el = e.target.closest('.app');
    const ids = allApps().map((a) => a.id).filter((id) => id !== dragId);
    const to = el ? ids.indexOf(el.dataset.app) : ids.length;
    ids.splice(to < 0 ? ids.length : to, 0, dragId);
    set().order = ids; save(); renderApps();
  });
  grid.addEventListener('dragend', () => { dragId = null; $$('.dragging,.drop-before', grid).forEach((x) => x.classList.remove('dragging', 'drop-before')); });
})();
function appMenu(a, x, y, el) {
  const st = set();
  const pinned = st.dock.includes(a.id);
  const alt = a.kind === 'runtipi' && a.port && a.localDomain;
  el?.classList.add('menu-open');
  contextMenu(x, y, [
    { icon: 'external', label: 'Abrir', run: () => openApp(a) },
    { icon: 'globe', label: 'Abrir pelo domínio local', hidden: !alt, run: () => window.open(appUrl(a, 'domain'), '_blank', 'noopener') },
    { icon: 'network', label: `Abrir por IP:porta (${a.port})`, hidden: !alt, run: () => window.open(appUrl(a, 'port'), '_blank', 'noopener') },
    { icon: 'copy', label: 'Copiar endereço', hidden: !appUrl(a), run: async () => { try { await navigator.clipboard.writeText(appUrl(a)); toast('Endereço copiado', appUrl(a)); } catch { toast('Não deu para copiar', appUrl(a)); } } },
    '-',
    { icon: 'edit', label: 'Personalizar…', run: () => editAppDialog(a) },
    { icon: 'pin', label: pinned ? 'Remover do dock' : 'Fixar no dock', run: () => { st.dock = pinned ? st.dock.filter((i) => i !== a.id) : [...st.dock, a.id]; save(); renderDock(); } },
    { icon: 'refresh', label: 'Reiniciar app', hidden: !(S.dockerActions && a.kind === 'runtipi' && a.state !== 'stopped'), run: () => restartApp(a) },
    '-',
    { icon: 'eyeOff', label: 'Ocultar da tela inicial', run: () => { st.apps[a.id] = { ...(st.apps[a.id] || {}), hidden: true }; save(); renderApps(); toast(`${a.name} oculto`, 'Mostre de novo em Configurações → Apps.'); } },
    { icon: 'trash', label: 'Remover atalho', danger: true, hidden: a.kind !== 'link', run: () => { st.links = st.links.filter((l) => `link:${l.id}` !== a.id); st.dock = st.dock.filter((i) => i !== a.id); save(); renderApps(); renderDock(); } },
  ], () => el?.classList.remove('menu-open'));
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
  settings: { label: 'Configurações', glyph: 'sliders', tile: 'linear-gradient(160deg,#a5adbd 0%,#5d6577 100%)', run: (el) => openSheet('settings', el) },
};
const DOCK = { base: 50, zoom: 80, range: 150 };
let dockMouseX = null, dockRaf = 0, tipFor = null;
function renderDock() {
  const box = $('.dock-items');
  const sysHTML = Object.entries(SYS).map(([k, it]) => `<div class="dock-item" data-sys="${k}" data-label="${esc(it.label)}">
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
const markDockOpen = () => $$('.dock-item').forEach((el) => el.classList.toggle('open', !!el.dataset.sys && el.dataset.sys === currentSheet));

/* ── Sheets ────────────────────────────────────────────────────── */
let currentSheet = null, sheetCleanup = null;
export function openSheet(kind, originEl, opts = {}) {
  const host = $('#sheet-host');
  if (currentSheet === kind) { if (kind === 'files' && opts.root) import('./files.js').then((m) => m.navigate(opts)); return; }
  sheetCleanup?.(); sheetCleanup = null;
  host.innerHTML = '';
  const backdrop = html('<div class="sheet-backdrop"></div>');
  const sheet = html(`<section class="sheet sheet-${kind}" role="dialog" aria-modal="true" aria-label="${kind === 'files' ? 'Arquivos' : 'Configurações'}"><button class="sheet-close" aria-label="Fechar">${svg('x')}</button><div class="sheet-body"></div></section>`);
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
    { group: 'Aparência', cat: 'look', icon: 'image', title: 'Papel de parede', desc: 'Escolha uma imagem ou envie a sua. Também dá para soltar o arquivo em qualquer lugar da tela inicial', ctrl: `<div class="wp-thumbs">${wps.map((id) => `<span class="wp-thumb-wrap"><button class="wp-thumb ${st.wallpaper === id ? 'on' : ''}" data-wp="${esc(id)}" style="background-image:url('${esc(wallpaperUrl(id, { thumb: true }))}')" aria-label="Papel de parede"></button>${id.startsWith('upload:') ? `<button class="wp-del" data-wp-del="${esc(id.slice(7))}" aria-label="Apagar">${svg('x')}</button>` : ''}</span>`).join('')}<button class="wp-thumb add" data-wp-add aria-label="Enviar imagem">${svg('plus')}</button></div>` },
    { group: 'Aparência', cat: 'look', icon: 'palette', title: 'Cor de destaque', desc: 'Automática tira a cor dominante do papel de parede; Vibrante pega a mais saturada', ctrl: `<span class="brand-chip"><i></i><span data-brand-label>hsl(${brandNow.h} ${brandNow.s}% ${brandNow.l}%)</span></span><div class="seg">${[['auto', 'Automática'], ['vibrant', 'Vibrante'], ['manual', 'Manual']].map(([k, l]) => `<button class="${st.accentMode === k ? 'on' : ''}" data-accent="${k}">${l}</button>`).join('')}</div>${st.accentMode === 'manual' ? `<div class="swatches">${SWATCH_HUES.map((h) => `<button class="swatch ${st.manualHue === h ? 'on' : ''}" data-hue="${h}" style="background:hsl(${h} 80% 48%)" aria-label="Matiz ${h}"></button>`).join('')}<input type="range" min="0" max="359" value="${st.manualHue}" data-hue-range aria-label="Matiz" style="--p:${st.manualHue / 3.59}%;width:110px"></div>` : ''}` },
    { group: 'Aparência', cat: 'look', icon: 'layers', title: 'Efeito de vidro', desc: 'Desfoque atrás dos widgets, do dock e das janelas. Desligar alivia computadores mais fracos', ctrl: `${range('blur', 0, 40, 'px')}${sw('glass', 'Vidro')}` },
    { group: 'Aparência', cat: 'look', icon: 'apps', title: 'Tamanho dos ícones', desc: 'Ícones da tela inicial', ctrl: range('iconSize', 48, 88, 'px') },
    { group: 'Aparência', cat: 'look', icon: 'grid', title: 'Widgets', desc: `Escolha e ordene clicando (até ${MAX_WIDGETS})`, ctrl: `<div class="chips">${Object.entries(WIDGETS).map(([k, w]) => `<button class="chip ${st.widgets.includes(k) ? 'on' : ''}" data-widget="${k}" title="${w.title}">${st.widgets.includes(k) ? `<b class="chip-n">${st.widgets.indexOf(k) + 1}</b>` : ''}${w.label}</button>`).join('')}</div>` },
    { group: 'Aparência', cat: 'look', icon: 'sparkles', title: 'Reduzir animações', desc: 'Desliga a ampliação do dock e as transições', ctrl: sw('reduceMotion') },
    { group: 'Apps', cat: 'apps', icon: 'globe', title: 'Endereço dos apps', desc: 'Automático usa o domínio local do Runtipi quando o app está exposto, e IP:porta nos outros', ctrl: `<div class="seg">${[['auto', 'Automático'], ['domain', 'Domínio local'], ['port', 'IP:porta']].map(([k, l]) => `<button class="${st.urlMode === k ? 'on' : ''}" data-urlmode="${k}">${l}</button>`).join('')}</div>` },
    { group: 'Apps', cat: 'apps', icon: 'box', title: 'Endereço do Runtipi', desc: `Usado pelos ícones Runtipi e Loja de apps do dock. Vazio = ${esc(runtipiAuto())}`, ctrl: `<input class="text-input" data-input="runtipiUrl" value="${esc(st.runtipiUrl || '')}" placeholder="${esc(runtipiAuto().replace(/^https?:\/\//, ''))}" aria-label="Endereço do Runtipi">` },
    { group: 'Apps', cat: 'apps', icon: 'network', title: 'Servidor para IP:porta', desc: `Vazio = o mesmo endereço que você usou para abrir o BrunOS (${esc(location.hostname || '—')})`, ctrl: `<input class="text-input" data-input="serverHost" value="${esc(st.serverHost)}" placeholder="${esc(S.tipi.internalIp || '192.168.1.50')}" aria-label="Servidor">` },
    { group: 'Apps', cat: 'apps', icon: 'pin', title: 'Apps no dock', desc: 'Aparecem depois do divisor, na ordem em que você marca', ctrl: dockPickerBtn() },
    { group: 'Apps', cat: 'apps', icon: 'link', title: 'Atalhos personalizados', desc: 'Sites e serviços que não estão no Runtipi (roteador, impressora, outros servidores)', ctrl: `<button class="pill-btn" data-act="add-link">${svg('plus')}Adicionar</button>` },
    { group: 'Apps', cat: 'apps', icon: 'activity', title: 'Status nos ícones', desc: 'Verde rodando, cinza parado, amarelo parcialmente rodando', ctrl: sw('showStatus') },
    { group: 'Apps', cat: 'apps', icon: 'external', title: 'Abrir apps em', desc: 'Nova aba ou na mesma aba', ctrl: `<div class="seg"><button class="${st.openIn === 'tab' ? 'on' : ''}" data-openin="tab">Nova aba</button><button class="${st.openIn === 'same' ? 'on' : ''}" data-openin="same">Mesma aba</button></div>` },
    { group: 'Apps', cat: 'apps', icon: 'eyeOff', title: 'Apps ocultos', desc: hidden.length ? hidden.map((a) => a.name).join(', ') : 'Nenhum app oculto', ctrl: hidden.length ? `<button class="pill-btn" data-act="unhide">Mostrar todos</button>` : '' },
    { group: 'Apps', cat: 'apps', icon: 'sort', title: 'Ordem dos apps', desc: 'Arraste os ícones na tela inicial para reordenar', ctrl: st.order.length ? `<button class="pill-btn" data-act="reset-order">Ordem alfabética</button>` : '' },
    { group: 'Conta', cat: 'account', icon: 'user', title: 'Nome de exibição', desc: 'Usado na saudação e na tela de login', ctrl: `<input class="text-input" data-input="displayName" value="${esc(displayName())}" aria-label="Nome de exibição">` },
    { group: 'Conta', cat: 'account', icon: 'key', title: 'Trocar senha', desc: `Usuário: ${esc(S.me?.username || '—')}. Ao trocar, as outras sessões abertas são encerradas`, ctrl: `<button class="pill-btn" data-act="password">Trocar</button>` },
    { group: 'Conta', cat: 'account', icon: 'logout', title: 'Sair', desc: 'Encerra esta sessão neste navegador', ctrl: `<button class="pill-btn" data-act="logout">Sair</button>` },
    { group: 'Sistema', cat: 'system', icon: 'info', title: 'Informações do dispositivo', desc: `${esc(info.os || 'Linux')} · ${esc(info.cpuModel || '')}`, ctrl: `<span class="chev">${svg('chevR')}</span>`, act: 'device' },
    { group: 'Sistema', cat: 'system', icon: 'box', title: 'Runtipi', desc: S.tipi.found ? `${S.apps.length} apps instalados · domínio local ${esc(S.tipi.localDomain || '—')}` : 'Pasta do Runtipi não encontrada (monte em /runtipi)', ctrl: `<button class="pill-btn" data-act="open-runtipi">Abrir ${svg('external')}</button>` },
    { group: 'Sistema', cat: 'system', icon: 'layers', title: 'Docker', desc: S.docker ? (S.dockerActions ? 'Conectado · reiniciar apps permitido' : 'Conectado em modo leitura (status e uso dos apps)') : 'Sem acesso ao Docker: status e uso por app ficam desligados', ctrl: `<span class="v1-note ${S.docker ? 'ok' : 'off'}">${S.docker ? 'ativo' : 'desligado'}</span>` },
    { group: 'Sistema', cat: 'system', icon: 'terminal', title: 'Terminal', desc: S.terminal === false ? 'Desligado no docker-compose (BRUNOS_TERMINAL=false)' : 'Abre um terminal no servidor por SSH, com o seu usuário e senha (nada fica guardado)', ctrl: S.terminal === false ? '' : `<button class="pill-btn" data-act="terminal">${svg('terminal')}Abrir terminal</button>` },
    { group: 'Sistema', cat: 'system', icon: 'folder', title: 'Pastas do gerenciador', desc: S.roots.length ? S.roots.map((r) => r.name + (r.readOnly ? ' (só leitura)' : '')).join(' · ') : 'Nenhuma pasta montada em /files', ctrl: `<button class="pill-btn" data-act="map-disks">${svg('disks')}Mapear discos</button>` },
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
    else if ((el = q('[data-widget]'))) {
      const k = el.dataset.widget;
      st.widgets = st.widgets.includes(k) ? st.widgets.filter((w) => w !== k) : [...st.widgets, k].slice(-MAX_WIDGETS);
      save(); renderWidgets(); redraw();
    }
    else if ((el = q('[data-act="dock-pick"]'))) dockPicker(el, () => { const b = $('[data-act="dock-pick"]', body); if (b) b.outerHTML = dockPickerBtn(); });
    else if ((el = q('[data-openin]'))) { st.openIn = el.dataset.openin; save(); renderApps(); redraw(); }
    else if ((el = q('[data-urlmode]'))) { st.urlMode = el.dataset.urlmode; save(); renderApps(); redraw(); }
    else if ((el = q('[data-act]'))) {
      const a = el.dataset.act;
      if (a === 'live') liveUsageDialog();
      else if (a === 'logout') logout();
      else if (a === 'device') deviceDialog();
      else if (a === 'password') passwordDialog();
      else if (a === 'add-link') linkDialog();
      else if (a === 'open-runtipi') SYS.runtipi.run();
      else if (a === 'terminal') openTerminal();
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
    if (t.matches('[data-range]')) {
      const k = t.dataset.range; set()[k] = +t.value; save();
      t.style.setProperty('--p', ((t.value - t.min) / (t.max - t.min)) * 100 + '%');
      t.nextElementSibling.textContent = t.value + (t.dataset.unit || '');
      applyPrefs();
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
  let metric = 'cpu', usageTimer;
  const d = openDialog(`<h2>Uso ao vivo</h2>
    <div class="stat-cards">
      <button class="stat-card on" data-m="cpu"><div class="k">CPU</div><div class="v" data-bind="cpu"></div><div class="s" data-bind="cores"></div><div class="track"><i data-bind-w="cpu"></i></div></button>
      <button class="stat-card" data-m="mem"><div class="k">Memória</div><div class="v" data-bind="mem"></div><div class="s" data-bind="memFree"></div><div class="track"><i data-bind-w="mem"></i></div></button>
      <button class="stat-card" data-m="temp"><div class="k">Temperatura</div><div class="v" data-bind="temp"></div><div class="s" data-bind="fan"></div><div class="track"><i data-bind-w="temp"></i></div></button>
      <button class="stat-card" data-m="net"><div class="k">Rede</div><div class="v v-sm" data-bind="rx"></div><div class="s">↑ <span data-bind="tx"></span></div><div class="track"><i data-bind-w="net"></i></div></button>
    </div>
    <div class="spark-box"><div class="lbl"><span data-spark-title></span><span data-spark-extra></span></div><svg class="spark" viewBox="0 0 300 70" preserveAspectRatio="none">
      <defs><linearGradient id="sparkFill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" style="stop-color:var(--brand-lighter);stop-opacity:.45"/><stop offset="1" style="stop-color:var(--brand-lighter);stop-opacity:0"/></linearGradient></defs>
      <path data-area fill="url(#sparkFill)"/><path data-line fill="none" style="stroke:var(--brand-lightest)" stroke-width="1.6" vector-effect="non-scaling-stroke"/><path data-line2 fill="none" style="stroke:rgba(255,255,255,.45)" stroke-width="1.2" stroke-dasharray="3 3" vector-effect="non-scaling-stroke"/></svg></div>
    <div class="live-sec">Discos</div><div class="disk-list"></div>
    <div class="live-sec">Apps <small>${S.docker ? 'CPU e memória por app (Docker)' : 'conecte o Docker para ver o consumo por app'}</small></div>
    <div class="proc-list"><div class="empty-note">Carregando…</div></div>`, { width: 640, onClose: () => { liveOpen = false; off(); clearTimeout(usageTimer); } });
  const titles = { cpu: 'CPU', mem: 'Memória', temp: 'Temperatura', net: 'Rede (download e upload)' };
  const drawChart = () => {
    const h = S.history;
    if (!h.length) return;
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
    $('[data-spark-title]', d.el).textContent = `${titles[metric]} · últimos ${mins || 1} min`;
    $('[data-spark-extra]', d.el).textContent = metric === 'cpu' ? `carga ${statValues().vals.load}` : metric === 'net' ? 'linha tracejada: upload' : metric === 'temp' ? (S.snapshot?.battery ? `bateria ${statValues().vals.battery}` : '') : '';
  };
  const drawDisks = () => {
    const disks = S.snapshot?.disks || [];
    $('.disk-list', d.el).innerHTML = disks.length ? disks.map((k) => `<div class="disk-row">
      <span class="disk-ico">${svg(k.system ? 'cpu' : 'disk')}</span>
      <span class="disk-name"><b>${esc(k.name)}</b><small>${esc(k.device.replace('/dev/', ''))}${k.model ? ' · ' + esc(k.model) : ''} · ${k.rotational ? 'HD' : 'SSD'}</small></span>
      <span class="track"><i style="width:${(k.used / k.total) * 100}%" class="${k.used / k.total > 0.9 ? 'warn' : ''}"></i></span>
      <span class="disk-val">${fmtBytes(k.used)} <small>/ ${fmtBytes(k.total)}</small></span></div>`).join('') : '<div class="empty-note">Nenhum disco encontrado</div>';
  };
  const drawUsage = async () => {
    try {
      const list = await api.usage();
      if (!liveOpen) return;
      const max = Math.max(5, ...list.map((u) => u.cpu));
      $('.proc-list', d.el).innerHTML = list.length ? list.map((u) => {
        const a = u.urn ? appById(u.urn) : null;
        const icon = a ? appIconHTML(a, 'mini') : `<span class="mini" style="--tile:#fff"><img src="assets/icons/tipi.webp" alt=""></span>`;
        return `<div class="proc">${icon}<span class="nm">${esc(a?.name || u.name)}</span><span class="track"><i style="width:${clamp((u.cpu / max) * 100, 0, 100)}%"></i></span><span class="pct">${fmt(u.cpu, 2)}%</span><span class="mem">${fmtBytes(u.memory)}</span></div>`;
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
    const c = e.target.closest('[data-m]'); if (!c) return;
    metric = c.dataset.m; $$('[data-m]', d.el).forEach((x) => x.classList.toggle('on', x === c)); drawChart();
  });
  const off = on('stats', draw);
  draw(); drawUsage();
}

/* Paleta de comandos (Ctrl+K) */
function commandPalette() {
  if ($('.cmdk')) return;
  const actions = [
    { icon: 'folder', label: 'Abrir Arquivos', run: () => openSheet('files', $('[data-sys="files"]')) },
    { icon: 'sliders', label: 'Abrir Configurações', run: () => openSheet('settings', $('[data-sys="settings"]')) },
    { icon: 'activity', label: 'Uso ao vivo', run: () => liveUsageDialog() },
    { icon: 'info', label: 'Informações do dispositivo', run: () => deviceDialog() },
    { icon: 'link', label: 'Novo atalho…', run: () => linkDialog() },
    { icon: 'image', label: 'Próximo papel de parede', run: () => { const ids = BUILTIN_WALLPAPERS.map((i) => `builtin:${i}`); set().wallpaper = ids[(ids.indexOf(set().wallpaper) + 1) % ids.length]; save(); setWallpaper(set().wallpaper); } },
    { icon: 'upload', label: 'Enviar papel de parede…', run: () => $('#wp-file').click() },
    { icon: 'palette', label: 'Cor de destaque: automática', run: () => { set().accentMode = 'auto'; save(); applyBrand(); } },
    { icon: 'sparkles', label: 'Cor de destaque: vibrante', run: () => { set().accentMode = 'vibrant'; save(); applyBrand(); } },
    { icon: 'box', label: 'Abrir o Runtipi', run: () => SYS.runtipi.run() },
    { icon: 'terminal', label: 'Abrir o terminal (SSH)', hidden: S.terminal === false, run: () => openTerminal() },
    { icon: 'logout', label: 'Sair', run: () => logout() },
  ];
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
  dialogs.slice().forEach((c) => c());
  closeSheet(); closeMenu();
  S.stopStream?.(); S.stopStream = null;
  document.body.classList.add('is-locked');
  renderLogin('login');
}
on('unauthorized', () => { if (!document.body.classList.contains('is-locked')) { lock(); toast('Sua sessão expirou', 'Entre de novo para continuar.'); } });

async function loadApps() {
  try {
    const r = await api.apps();
    S.apps = r.apps; S.tipi = r.runtipi || {}; S.docker = r.docker; S.dockerActions = r.dockerActions;
    renderApps(); renderDock();
  } catch (e) { if (!/login|sessão/.test(e.message)) toastError(e, 'Não foi possível carregar os apps'); }
}
async function enterDesktop(animate) {
  const [me, settings, system, roots, wallpapers] = await Promise.all([
    api.me(), api.settings().catch(() => ({})), api.system().catch(() => null), api.files.roots().catch(() => []), api.wallpapers().catch(() => []),
  ]);
  S.me = me; S.system = system; S.roots = roots; S.wallpapers = wallpapers;
  S.settings = { ...structuredClone(DEFAULTS), ...settings };
  if (system) { S.tipi = system.runtipi || {}; S.docker = system.docker; S.dockerActions = system.dockerActions; S.terminal = system.terminal !== false; }
  applyPrefs();
  setWallpaper(set().wallpaper, { instant: !animate });
  renderDock();
  await loadApps();
  S.stopStream?.();
  S.stopStream = api.streamStats(
    (d) => { S.snapshot = d.snapshot; S.history = d.history || []; bindStats(); emit('stats'); },
    (d) => { S.snapshot = d.snapshot; S.history.push(d.point); if (S.history.length > 150) S.history.shift(); bindStats(); emit('stats'); },
  );
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
  if (document.body.classList.contains('is-locked')) return;
  if (e.target.closest?.('.xterm')) return; // dentro do terminal, Esc e Ctrl+K são do shell
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); commandPalette(); }
  else if (e.key === 'Escape') {
    if (menuOpen()) closeMenu();
    else if (dialogs.length) dialogs[dialogs.length - 1]();
    else if (currentSheet) closeSheet();
  }
});
$('#search-pill').addEventListener('click', commandPalette);
$('#desktop').addEventListener('click', (e) => {
  if (e.target.closest('[data-action="live"]')) liveUsageDialog();
  if (e.target.closest('[data-action="battery"]')) batteryDialog(() => S.snapshot?.battery, (fn) => on('stats', fn));
  const fav = e.target.closest('[data-fav-root]');
  if (fav) openSheet('files', $('[data-sys="files"]'), { root: fav.dataset.favRoot, path: fav.dataset.favPath });
});
$('#desktop').addEventListener('contextmenu', (e) => {
  if (e.target.closest('.app, input, a')) return;
  e.preventDefault();
  contextMenu(e.clientX, e.clientY, [
    { icon: 'image', label: 'Trocar papel de parede…', run: () => { settingsTab = 'look'; openSheet('settings', $('[data-sys="settings"]')); } },
    { icon: 'upload', label: 'Enviar papel de parede…', run: () => $('#wp-file').click() },
    { icon: 'grid', label: 'Escolher widgets…', run: () => { settingsTab = 'look'; settingsQuery = 'widgets'; openSheet('settings', $('[data-sys="settings"]')); setTimeout(() => { settingsQuery = ''; }, 0); } },
    { icon: 'link', label: 'Novo atalho…', run: () => linkDialog() },
    { icon: 'refresh', label: 'Atualizar apps', run: () => loadApps() },
    '-',
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
setInterval(renderGreeting, 60_000);
document.addEventListener('visibilitychange', () => { if (!document.hidden && !document.body.classList.contains('is-locked')) loadApps(); });

/* ── Início ────────────────────────────────────────────────────── */
(async function boot() {
  S.pub = await initApi();
  S.settings = { ...structuredClone(DEFAULTS), ...(S.pub.wallpaper ? { wallpaper: S.pub.wallpaper } : {}), ...(S.pub.accentMode ? { accentMode: S.pub.accentMode } : {}), ...(S.pub.manualHue != null ? { manualHue: S.pub.manualHue } : {}) };
  setWallpaper(set().wallpaper, { instant: true, pub: true });
  if (S.pub.setupRequired) { renderLogin('setup'); return; }
  try { await enterDesktop(false); }
  catch { renderLogin('login'); }
})();
