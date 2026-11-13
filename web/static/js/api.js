// Cliente da API do BrunOS. Sem backend (ex.: abrindo o HTML direto, ou no artefato
// de pré-visualização) cai num modo demonstração com dados simulados.
import { emit } from './ui.js';

const H = { 'X-BrunOS': '1' };

async function call(method, path, body, { raw = false } = {}) {
  const opts = { method, headers: { ...H }, credentials: 'same-origin' };
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  if (res.status === 401 && !path.startsWith('/api/auth/login')) emit('unauthorized');
  if (res.status === 423) emit('locked'); // Bloqueio com senha feito em outra aba deste navegador
  if (raw) return res;
  let data = null;
  const ct = res.headers.get('content-type') || '';
  if (ct.includes('json')) data = await res.json();
  if (!res.ok) { const e = new Error(data?.error || `Erro ${res.status}`); e.status = res.status; e.data = data; throw e; }
  return data;
}
const q = (o) => new URLSearchParams(o).toString();

/* ── Envio em pedaços com retomada ───────────────────────────────── */
const BIG_UPLOAD = 16 << 20, CHUNK = 8 << 20;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const lsGet = (k) => { try { return localStorage.getItem(k); } catch { return null; } };
const lsSet = (k, v) => { try { v == null ? localStorage.removeItem(k) : localStorage.setItem(k, v); } catch { /* sem storage: só não retoma depois de recarregar */ } };

function sendChunk(id, offset, blob, onProgress, setXhr) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    setXhr(xhr);
    xhr.open('PUT', `/api/files/upload/chunk?${q({ id, offset })}`);
    xhr.setRequestHeader('X-BrunOS', '1');
    xhr.setRequestHeader('Content-Type', 'application/octet-stream');
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress(e.loaded / e.total);
    xhr.onload = () => {
      let data = {}; try { data = JSON.parse(xhr.responseText); } catch { /* vazio */ }
      if (xhr.status < 300) resolve(data.offset);
      else reject(Object.assign(new Error(data.error || `Erro ${xhr.status}`), { status: xhr.status, data }));
    };
    xhr.onerror = () => reject(Object.assign(new Error('Conexão perdida'), { status: 0 }));
    xhr.onabort = () => reject(Object.assign(new Error('Cancelado'), { status: -1 }));
    xhr.send(blob);
  });
}

function chunkedUpload(root, path, name, file, onProgress) {
  const key = `brunos.upload.${[root, path, name, file.size, file.lastModified].join('|')}`;
  let aborted = false, xhr = null, id = null;
  const promise = (async () => {
    let offset = 0;
    id = lsGet(key);
    if (id) { try { offset = (await call('GET', `/api/files/upload/status?${q({ id })}`)).offset; } catch { id = null; } }
    if (!id) { id = (await call('POST', '/api/files/upload/start', { root, path, name, size: file.size })).id; lsSet(key, id); }
    let fails = 0;
    while (offset < file.size) {
      if (aborted) { call('POST', '/api/files/upload/cancel', { id }).catch(() => {}); lsSet(key, null); throw new Error('Cancelado'); }
      const start = offset, end = Math.min(file.size, offset + CHUNK);
      try {
        offset = await sendChunk(id, start, file.slice(start, end), (p) => onProgress?.((start + p * (end - start)) / file.size), (x) => { xhr = x; });
        fails = 0;
        onProgress?.(offset / file.size);
      } catch (e) {
        if (aborted || e.status === -1) throw new Error('Cancelado');
        if (e.status === 409 && e.data?.offset != null) { offset = e.data.offset; continue; } // servidor já tinha esse pedaço
        if (e.status === 404) { lsSet(key, null); throw e; }
        if (e.status >= 400 && e.status < 500 && e.status !== 408 && e.status !== 429) throw e; // erro que não melhora tentando de novo
        if (++fails > 8) throw new Error('Conexão perdida. Envie o mesmo arquivo de novo para continuar de onde parou');
        await sleep(Math.min(30000, 1000 * 2 ** fails));
        try { offset = (await call('GET', `/api/files/upload/status?${q({ id })}`)).offset; } catch { /* tenta de novo no próximo ciclo */ }
      }
    }
    const r = await call('POST', '/api/files/upload/finish', { id });
    lsSet(key, null);
    return r;
  })();
  const abort = () => {
    aborted = true; xhr?.abort();
    if (id) { call('POST', '/api/files/upload/cancel', { id }).catch(() => {}); lsSet(key, null); }
  };
  return { promise, abort };
}

const real = {
  demo: false,
  public: () => call('GET', '/api/public'),
  setup: (b) => call('POST', '/api/auth/setup', b),
  login: (b) => call('POST', '/api/auth/login', b),
  logout: () => call('POST', '/api/auth/logout', {}),
  me: () => call('GET', '/api/auth/me'),
  updateMe: (b) => call('PUT', '/api/auth/me', b),
  password: (b) => call('POST', '/api/auth/password', b),
  lockState: () => call('GET', '/api/auth/lock'),
  lock: () => call('POST', '/api/auth/lock', {}),
  unlock: (password) => call('POST', '/api/auth/unlock', { password }),
  notes: {
    get: (id) => call('GET', `/api/notes/${encodeURIComponent(id)}`),
    put: (id, text) => call('PUT', `/api/notes/${encodeURIComponent(id)}`, { text }),
    del: (id) => call('DELETE', `/api/notes/${encodeURIComponent(id)}`),
  },
  geocode: (name) => call('GET', `/api/integrations/geocode?${q({ q: name })}`),
  energy: {
    get: () => call('GET', '/api/energy'),
    setConfig: (c) => call('PUT', '/api/energy/config', c),
    history: () => call('GET', '/api/energy/history'),
  },
  smart: { get: () => call('GET', '/api/smart'), refresh: () => call('POST', '/api/smart/refresh', {}), retry: (key) => call('POST', '/api/smart/retry', { key }),
    selftest: (key, kind) => call('POST', '/api/smart/selftest', { key, kind }) },
  updates: {
    get: () => call('GET', '/api/updates'),
    check: (part = '') => call('POST', `/api/updates/check${part ? `?${q({ part })}` : ''}`, {}),
    setEvery: (every) => call('PUT', '/api/updates/config', { every }),
    run: (kind, urn, backup = false) => call('POST', '/api/updates/run', { kind, urn, backup }),
    backupSize: (urn) => call('GET', `/api/updates/backup-size?${q({ urn })}`),
    script: () => call('POST', '/api/updates/script', {}),
  },
  settings: () => call('GET', '/api/settings'),
  saveSettings: (s) => call('PUT', '/api/settings', s),
  settingsBackups: () => call('GET', '/api/settings/backups'),
  settingsRestore: (date) => call('POST', '/api/settings/restore', { date }),
  wallpapers: () => call('GET', '/api/wallpapers'),
  wallpaperUrl: (name) => `/api/wallpapers/${encodeURIComponent(name)}`,
  publicWallpaperUrl: () => '/api/public/wallpaper',
  async uploadWallpaper(file) {
    const res = await fetch('/api/wallpapers', { method: 'PUT', headers: { ...H, 'Content-Type': 'application/octet-stream' }, body: file, credentials: 'same-origin' });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || 'Falha no envio');
    return data;
  },
  deleteWallpaper: (name) => call('DELETE', `/api/wallpapers/${encodeURIComponent(name)}`),
  stats: () => call('GET', '/api/stats'),
  // estatísticas ao vivo; os avisos novos da central (event: notify) e os valores das integrações
  // (event: metrics) chegam pela mesma conexão
  streamStats(onInit, onPoint, onNotify, onMetrics, { onNote, onLocked } = {}) {
    let es, stopped = false, retry;
    const open = () => {
      es = new EventSource('/api/stats/stream');
      es.addEventListener('init', (e) => onInit(JSON.parse(e.data)));
      es.addEventListener('notify', (e) => onNotify?.(JSON.parse(e.data)));
      es.addEventListener('metrics', (e) => onMetrics?.(JSON.parse(e.data)));
      es.addEventListener('note', (e) => onNote?.(JSON.parse(e.data)));
      // bloqueou com a conexão aberta: o servidor encerra; não reconecta até desbloquear
      es.addEventListener('locked', () => { stopped = true; es.close(); onLocked?.(); });
      es.onmessage = (e) => onPoint(JSON.parse(e.data));
      es.onerror = () => { es.close(); if (!stopped) retry = setTimeout(open, 4000); };
    };
    open();
    return () => { stopped = true; clearTimeout(retry); es?.close(); };
  },
  system: () => call('GET', '/api/system'),
  statsHistory: (range) => call('GET', `/api/stats/history?${q({ range })}`),
  // Históricos (0.14.0): várias séries de uma vez; periodo = 5m | 1h | 24h | 48h | 7d | 30d
  history: (series, periodo) => call('GET', `/api/history?${series.map((s) => `serie=${encodeURIComponent(s)}`).join('&')}&${q({ periodo })}`),
  historyNames: (prefixo) => call('GET', `/api/history/series?${q({ prefixo })}`),
  internet: () => call('GET', '/api/internet'),
  metricSeries: (id, range) => call('GET', `/api/metrics/series?${q({ id, range })}`),
  integrationImage: (id, ref) => `/api/integrations/${encodeURIComponent(id)}/image?${q({ ref })}`,
  integrations: {
    kinds: () => call('GET', '/api/integrations/kinds'),
    list: () => call('GET', '/api/integrations'),
    suggest: () => call('GET', '/api/integrations/suggest'),
    add: (b) => call('POST', '/api/integrations', b),
    update: (id, b) => call('PUT', `/api/integrations/${encodeURIComponent(id)}`, b),
    del: (id) => call('DELETE', `/api/integrations/${encodeURIComponent(id)}`),
    test: (b) => call('POST', '/api/integrations/test', b),
  },
  processes: (kernel = false) => call('GET', `/api/processes${kernel ? '?kernel=1' : ''}`),
  events: {
    list: () => call('GET', '/api/events'),
    read: (ids = []) => call('POST', '/api/events/read', { ids }),
    clear: () => call('POST', '/api/events/clear', {}),
  },
  disks: {
    forecast: () => call('GET', '/api/disks/forecast'),
    // janela Discos (0.15.0)
    list: () => call('GET', '/api/disks'),
    refresh: () => call('POST', '/api/disks/refresh', {}),
    timeline: (key) => call('GET', `/api/disks/timeline?${q({ key })}`),
    state: (dev) => call('GET', `/api/disks/state?${q({ dev })}`),
    errors: (dev) => call('GET', `/api/disks/errors?${q({ dev })}`),
    speed: (dev) => call('POST', '/api/disks/speed', { dev }),
    eject: (dev) => call('POST', '/api/disks/eject', { dev }),
  },
  power: {
    get: () => call('GET', '/api/power'),
    save: (cfg) => call('PUT', '/api/power/config', cfg),
    cancel: () => call('POST', '/api/power/cancel', {}),
    setup: () => call('GET', '/api/power/setup'),
    test: (b) => call('POST', '/api/power/test', b),
    charge: (b) => call('POST', '/api/power/charge', b),
  },
  term: {
    open: (b) => call('POST', '/api/terminal/open', b),
    input: (id, data) => call('POST', '/api/terminal/input', { id, data }),
    resize: (id, cols, rows) => call('POST', '/api/terminal/resize', { id, cols, rows }),
    close: (id) => call('POST', '/api/terminal/close', { id }),
    forget: (host, port) => call('POST', '/api/terminal/forget', { host, port }),
    list: () => call('GET', '/api/terminal/sessions'),
    busy: (id) => call('POST', '/api/terminal/busy', { id }),
    key: {
      get: () => call('GET', '/api/terminal/key'),
      create: (password) => call('POST', '/api/terminal/key', { password }),
      unlock: (password) => call('POST', '/api/terminal/key/unlock', { password }),
      del: () => call('DELETE', '/api/terminal/key'),
    },
    // saída do terminal por Server-Sent Events (base64); reconecta sozinho se a rede piscar.
    // replay: na primeira conexão, pede o fim da tela (Aba reaberta depois de minimizar).
    stream(id, { onData, onExit, onReplaced, onError, onLocked, replay = false }) {
      let es, stopped = false, fails = 0, first = true;
      const open = () => {
        es = new EventSource(`/api/terminal/stream?${q({ id, ...(first && replay ? { replay: 1 } : {}) })}`);
        first = false;
        es.addEventListener('ready', () => { fails = 0; });
        es.onmessage = (e) => { const bin = atob(e.data); const u8 = new Uint8Array(bin.length); for (let i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i); onData(u8); };
        es.addEventListener('exit', (e) => { stopped = true; es.close(); onExit(JSON.parse(e.data).message); });
        es.addEventListener('replaced', () => { stopped = true; es.close(); onReplaced?.(); });
        es.addEventListener('locked', () => { stopped = true; es.close(); onLocked?.(); });
        es.onerror = () => { es.close(); if (stopped) return; if (++fails > 5) { stopped = true; onError?.(); } else setTimeout(open, 1500); };
      };
      open();
      return () => { stopped = true; es?.close(); };
    },
  },
  apps: () => call('GET', '/api/apps'),
  usage: () => call('GET', '/api/usage'),
  restart: (urn) => call('POST', '/api/apps/restart', { urn }),
  logoUrl: (urn) => `/api/apps/logo?${q({ urn })}`,
  app: {
    start: (urn) => call('POST', '/api/apps/start', { urn }),
    stop: (urn) => call('POST', '/api/apps/stop', { urn }),
    containers: (urn) => call('GET', `/api/apps/containers?${q({ urn })}`),
    history: (urn, range = '24h') => call('GET', `/api/apps/history?${q({ urn, range })}`),
    async logs(urn, id, tail = 300) {
      const res = await fetch(`/api/apps/logs?${q({ urn, id, tail })}`, { headers: H, credentials: 'same-origin' });
      if (!res.ok) { const d = await res.json().catch(() => ({})); throw new Error(d.error || `Erro ${res.status}`); }
      return res.text();
    },
    // linhas novas dos logs (SSE); devolve a função que para de acompanhar
    follow(urn, id, onLine, onEnd) {
      const es = new EventSource(`/api/apps/logs/stream?${q({ urn, id })}`);
      es.onmessage = (e) => onLine(JSON.parse(e.data));
      es.addEventListener('end', () => { es.close(); onEnd?.(); });
      es.onerror = () => { if (es.readyState === EventSource.CLOSED) onEnd?.(); };
      return () => es.close();
    },
  },
  files: {
    roots: () => call('GET', '/api/files/roots'),
    disks: () => call('GET', '/api/files/disks'),
    setDisks: (mapped) => call('PUT', '/api/files/disks', { mapped }),
    list: (root, path) => call('GET', `/api/files/list?${q({ root, path })}`),
    search: (root, path, query) => call('GET', `/api/files/search?${q({ root, path, q: query })}`),
    mkdir: (root, path, name) => call('POST', '/api/files/mkdir', { root, path, name }),
    rename: (root, path, name) => call('POST', '/api/files/rename', { root, path, name }),
    paste: (root, paths, destRoot, destPath, copy) => call('POST', '/api/files/paste', { root, paths, destRoot, destPath, copy }),
    trash: (root, paths) => call('POST', '/api/files/trash', { root, paths }),
    del: (root, paths) => call('POST', '/api/files/delete', { root, paths }),
    trashList: () => call('GET', '/api/files/trash'),
    restore: (root, id) => call('POST', '/api/files/trash/restore', { root, id }),
    trashDelete: (root, id) => call('POST', '/api/files/trash/delete', { root, id }),
    trashEmpty: (root = '') => call('POST', '/api/files/trash/empty', { root }),
    rawUrl: (root, path, download) => `/api/files/raw?${q({ root, path, ...(download ? { download: 1 } : {}) })}`,
    zipUrl: (root, paths) => `/api/files/zip?${new URLSearchParams([['root', root], ...paths.map((p) => ['p', p])])}`,
    text: (root, path) => call('GET', `/api/files/text?${q({ root, path })}`),
    async saveText(root, path, text, mtime) {
      const res = await fetch(`/api/files/text?${q({ root, path, mtime })}`, { method: 'PUT', headers: { ...H, 'Content-Type': 'text/plain; charset=utf-8' }, body: text, credentials: 'same-origin' });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) { const e = new Error(data.error || `Erro ${res.status}`); e.status = res.status; throw e; }
      return data;
    },
    newFile: (root, path, name) => call('POST', '/api/files/newfile', { root, path, name }),
    extract: (root, path) => call('POST', '/api/files/extract', { root, path }),
    duplicates: (root, path, min) => call('GET', `/api/files/duplicates?${q({ root, path, min })}`),
    usage: (root, path, refresh) => call('GET', `/api/files/usage?${q({ root, path, ...(refresh ? { refresh: 1 } : {}) })}`),
    // Arquivos grandes vão em pedaços de 8 MB: se a conexão cair, continua de onde parou (e, se a
    // página for recarregada, escolher o mesmo arquivo para a mesma pasta retoma o envio).
    upload(root, path, name, file, onProgress) {
      if (file.size > BIG_UPLOAD) return chunkedUpload(root, path, name, file, onProgress);
      const xhr = new XMLHttpRequest();
      const promise = new Promise((resolve, reject) => {
        xhr.open('PUT', `/api/files/upload?${q({ root, path, name })}`);
        xhr.setRequestHeader('X-BrunOS', '1');
        xhr.setRequestHeader('Content-Type', 'application/octet-stream');
        xhr.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total);
        xhr.onload = () => {
          let data = {}; try { data = JSON.parse(xhr.responseText); } catch { /* vazio */ }
          xhr.status < 300 ? resolve(data) : reject(new Error(data.error || `Erro ${xhr.status}`));
        };
        xhr.onerror = () => reject(new Error('Conexão perdida'));
        xhr.onabort = () => reject(new Error('Cancelado'));
        xhr.send(file);
      });
      return { promise, abort: () => xhr.abort() };
    },
  },
};

/* ══ Modo demonstração ══════════════════════════════════════════ */
// camadas dos Históricos (iguais às do servidor): passo e quantidade de pontos por período
const DEMO_P = { '5m': { step: 60, n: 5 }, '1h': { step: 60, n: 60 }, '24h': { step: 60, n: 1440 }, '48h': { step: 60, n: 2880 }, '7d': { step: 300, n: 2016 }, '30d': { step: 1200, n: 2160 } };
function makeDemo() {
  const LS = 'brunos.demo.settings';
  const load = () => { try { return JSON.parse(localStorage.getItem(LS) || '{}'); } catch { return {}; } };
  let settings = load();
  let logged = false;
  const GB = 1e9;
  const nowS = Math.floor(Date.now() / 1000);
  const disks = [
    { name: 'Sistema', device: '/dev/sdc2', total: 312 * GB, used: 33 * GB, system: true, rotational: false, model: 'SSD', temp: 34, tempAt: nowS },
    { name: 'Disco1', device: '/dev/sdd', total: 984 * GB, used: 463 * GB, rotational: true, model: 'HDD', temp: 41, tempAt: nowS },
    { name: 'Disco2', device: '/dev/sda', total: 984 * GB, used: 246 * GB, rotational: true, model: 'HDD', temp: 37, tempAt: nowS - 3 * 3600 }, // parado há 3 h
    { name: 'Disco3', device: '/dev/sdb', total: 984 * GB, used: 462 * GB, rotational: true, model: 'HDD', temp: 52, tempAt: nowS },
  ].map((d) => ({ ...d, free: d.total - d.used }));
  // previsão de disco cheio: 30 dias de histórico simulado (GB por dia de cada disco)
  const growth = { Sistema: 0.15, 'Disco1': 3.6, 'Disco2': 0, 'Disco3': 12.4 };
  const forecast = disks.map((d) => {
    const g = growth[d.name] * GB;
    const points = Array.from({ length: 30 }, (_, i) => ({ d: new Date((nowS - (29 - i) * 86400) * 1000).toISOString().slice(0, 10), u: d.used - (29 - i) * g, f: d.free + (29 - i) * g }));
    return { name: d.name, perDay: g, enough: true, points, ...(g > 0 && d.free / g < 3650 ? { daysLeft: Math.floor(d.free / g) } : {}) };
  });
  const s = { cpu: 12, mem: 2.6 * GB, temp: 51, rx: 40e3, tx: 300e3 };
  const walk = (v, step, lo, hi) => Math.min(hi, Math.max(lo, v + (Math.random() - 0.5) * step));
  const history = [];
  const snap = () => ({
    t: Math.floor(Date.now() / 1000),
    cpu: { percent: Math.round(s.cpu * 10) / 10, cores: 2, model: 'Intel(R) Core(TM) i5 (demonstração)', load: [0.4, 0.3, 0.2] },
    mem: { total: 7.7 * GB, used: s.mem, available: 7.7 * GB - s.mem, swapTotal: 2 * GB, swapUsed: 0.1 * GB },
    temp: Math.round(s.temp * 10) / 10,
    sensors: [{ chip: 'coretemp', label: 'Package id 0', value: s.temp }, { chip: 'dell_smm', label: 'Ambient', value: 47 }],
    fanRpm: 2321, battery: { percent: 86, status: 'Charging', ac: true, energyWh: 35.4, fullWh: 41.2, designWh: 48.8, powerW: 7.6, health: 84, cycles: 213, minutesFull: 46, voltage: 12.4, tech: 'Li-ion', manufacturer: 'SMP', model: 'DELL 0X5', chargeLimit: null, history: Array.from({ length: 96 }, (_, i) => ({ t: Math.floor(Date.now() / 1000) - (95 - i) * 300, p: Math.round(i < 40 ? 100 - i * 1.3 : i < 50 ? 48 + (i - 40) * 0.2 : Math.min(86, 50 + (i - 50) * 0.9)), ac: !(i >= 10 && i < 50) })) }, disks,
    net: { rx: s.rx, tx: s.tx, ifaces: ['enp2s0'] }, uptime: 2 * 86400 + 4 * 3600,
    // leitura e escrita de cada disco (janela Discos, ao vivo)
    diskIO: ['sda', 'sdb', 'sdc', 'sdd'].map((name, i) => { const busy = Math.random() < 0.3; return { name, read: busy ? Math.round(Math.random() * 40e6 / (i + 1)) : Math.round(Math.random() * 2e5), write: busy ? Math.round(Math.random() * 12e6) : Math.round(Math.random() * 1e5), busy: busy ? +(5 + Math.random() * 30).toFixed(1) : +(Math.random() * 2).toFixed(1) }; }),
  });
  const tick = () => {
    s.cpu = walk(s.cpu, 8, 3, 40); s.mem = walk(s.mem, 0.1 * GB, 2.2 * GB, 3.2 * GB); s.temp = walk(s.temp, 1.5, 46, 62);
    s.rx = walk(s.rx, 200e3, 2e3, 2e6); s.tx = walk(s.tx, 200e3, 2e3, 2e6);
    const sn = snap();
    const p = { t: sn.t, cpu: sn.cpu.percent, mem: Math.round((s.mem / (7.7 * GB)) * 1000) / 10, temp: sn.temp, rx: s.rx, tx: s.tx };
    history.push(p); if (history.length > 150) history.shift();
    return { snapshot: sn, point: p };
  };
  for (let i = 0; i < 90; i++) tick();
  history.forEach((p, i) => { p.t -= (history.length - 1 - i) * 2; }); // um ponto a cada 2 s, como no servidor

  const A = (id, store, name, port, icon, extra = {}) => ({ urn: `${id}:${store}`, id, store, name, port, localDomain: `${id}-${store}.server.lan`, protocol: 'https', project: `${id}_${store}`, hasLogo: false, builtinIcon: icon, state: 'running', containers: 1, running: 1, shortDesc: '', ...extra });
  const apps = [
    A('immich', 'migrated', 'Immich', 2283, 'immich'), A('nextcloud-mini', 'migrated', 'Nextcloud', 8083, 'nextcloud'),
    A('plex', 'migrated', 'Plex', 32400, 'plex'), A('syncthing', 'migrated', 'Syncthing', 8384, 'syncthing'),
    A('vaultwarden', 'migrated', 'Vaultwarden', 8107, 'vaultwarden'), A('adguard', 'migrated', 'AdGuard Home', 8104, 'adguard-home'),
    A('transmission', 'migrated', 'Transmission', 9091, 'transmission'), A('qbittorrent', 'migrated', 'qBittorrent', 8080, 'qbittorrent'),
    A('speedtest-tracker', 'migrated', 'Speedtest Tracker', 8765, 'speedtest-tracker'), A('watchyourlan', 'migrated', 'WatchYourLAN', 8840, 'watchyourlan'),
    A('filebrowser', 'migrated', 'File Browser', 8087, 'filebrowser'), A('dashdot-lite', 'personal-store', 'Dashdot', 3001, 'dashdot'),
    A('homarr', 'migrated', 'Homarr', 7575, 'homarr', { state: 'stopped', running: 0 }), A('tailscale', 'migrated', 'Tailscale', 0, 'tailscale', { localDomain: '' }),
  ];

  // Árvore de arquivos simulada
  const now = Math.floor(Date.now() / 1000), day = 86400;
  const F = (name, size, ago) => ({ name, size, mtime: now - ago * day });
  const D = (name, children, ago = 2) => ({ name, dir: true, children, mtime: now - ago * day });
  const tree = {
    home: D('', [D('usuario', [
      D('Documentos', [D('Mestrado', [F('Dissertação_v12.docx', 3.1e6, 4), F('analise_ERP.py', 18e3, 2), F('figuras_P300.png', 840e3, 2)]), F('Laudo_modelo.docx', 210e3, 30), F('Planilha_correcao.xlsx', 96e3, 3), F('notas.md', 4e3, 0)]),
      D('Downloads', [F('ubuntu-24.04-live-server-amd64.iso', 2.6e9, 21), F('Neuropsychologia_guide.pdf', 1.4e6, 7)]),
      D('Fotos', [F('IMG_2041.heic', 3.2e6, 7), F('wallpaper-lago.jpg', 2.1e6, 60)]),
      D('runtipi', [D('apps', []), D('app-data', [])], 40),
    ])]),
    'disco1': D('', [D('Filmes', [F('Filme (2003).mkv', 7.4e9, 30), F('Outro filme.mkv', 2.2e9, 31)]), D('Séries', [])]),
    'disco2': D('', [D('Backups', [F('backup-2026-10.tar.gz', 21e9, 9)]), D('Músicas', [F('faixa.flac', 34e6, 100)])]),
    'disco3': D('', [D('Immich', []), D('Nextcloud', [])]),
  };
  const rootsMeta = [['home', 'Home', 312, 33], ['disco1', 'Disco1', 984, 463], ['disco2', 'Disco2', 984, 246], ['disco3', 'Disco3', 984, 462], ['ssd', 'SSD', 0, 0], ['ssd-nvme', 'SSD_NVME', 0, 0]];
  let demoMapped = ['Disco1', 'Disco2', 'Disco3'];
  // Janela Discos (0.15.0): SMART com atributos e testes, e a lista detalhada dos discos
  const at = (id, name, value, worst, thresh, raw, extra = {}) => ({ id, name, value, worst, thresh, raw, ...extra });
  const hdAttrs = (realloc, pending, temp, hours, crc = 0) => [at(1, 'Raw_Read_Error_Rate', 100, 100, 62, 0, { prefail: true }), at(3, 'Spin_Up_Time', 140, 133, 33, 1820, { prefail: true }),
    at(4, 'Start_Stop_Count', 92, 92, 0, 8120), at(5, 'Reallocated_Sector_Ct', realloc ? 98 : 100, realloc ? 98 : 100, 5, realloc, { prefail: true }), at(9, 'Power_On_Hours', 12, 12, 0, hours),
    at(12, 'Power_Cycle_Count', 95, 95, 0, 5400), at(191, 'G-Sense_Error_Rate', 100, 100, 0, 37), at(192, 'Power-Off_Retract_Count', 100, 100, 0, 211), at(193, 'Load_Cycle_Count', 61, 61, 0, 391204),
    at(194, 'Temperature_Celsius', 100 - temp, 45, 0, temp, { rawStr: `${temp} (Min/Max 18/52)` }), at(197, 'Current_Pending_Sector', 100, 100, 0, pending), at(198, 'Offline_Uncorrectable', 100, 100, 0, 0),
    at(199, 'UDMA_CRC_Error_Count', 200, 200, 0, crc), at(241, 'Total_LBAs_Written', 100, 100, 0, 9.1e10), at(242, 'Total_LBAs_Read', 100, 100, 0, 2.3e11)];
  const demoSmartDisks = [
    { key: 'D-SDA', dev: 'sda', tran: 'usb', mode: 'sat', model: 'HD USB 1TB', health: 'atencao', passed: true, temp: 39, tempAt: now - 600, powerOnHours: 38900, powerCycles: 5400, reallocated: 8, pending: 1, uncorrectable: 0, crcErrors: 0, mediaErrors: 0, checkedAt: now - 3600,
      history: [{ at: now - 86400 * 20, reallocated: 0, pending: 0, passed: true }, { at: now - 3600, reallocated: 8, pending: 1, passed: true }],
      attrs: hdAttrs(8, 1, 39, 38900, 3), shortMin: 2, longMin: 182, tests: [{ kind: 'Short offline', status: 'Completed without error', passed: true, hours: 38750 }, { kind: 'Extended offline', status: 'Completed: read failure', passed: false, hours: 38020 }] },
    { key: 'D-SDB', dev: 'sdb', tran: 'usb', model: 'Case USB', health: '', noSmart: true, noSmartWhy: 'travou', noSmartAt: now - 7200, failMode: 'sat', reallocated: 0, pending: 0, uncorrectable: 0, crcErrors: 0, mediaErrors: 0, checkedAt: 0 },
    { key: 'D-SDC', dev: 'sdc', tran: 'sata', mode: 'auto', model: 'SSD SATA 240GB', health: 'ok', passed: true, temp: 34, tempAt: now - 300, powerOnHours: 9100, powerCycles: 880, reallocated: 0, pending: 0, uncorrectable: 0, crcErrors: 0, mediaErrors: 0, checkedAt: now - 3600,
      attrs: [at(5, 'Reallocated_Sector_Ct', 100, 100, 10, 0, { prefail: true }), at(9, 'Power_On_Hours', 99, 99, 0, 9100), at(12, 'Power_Cycle_Count', 99, 99, 0, 880), at(174, 'Unexpect_Power_Loss_Ct', 100, 100, 0, 41),
        at(177, 'Wear_Leveling_Count', 94, 94, 0, 61, { prefail: true }), at(194, 'Temperature_Celsius', 66, 52, 0, 34), at(199, 'UDMA_CRC_Error_Count', 100, 100, 0, 0), at(241, 'Total_LBAs_Written', 99, 99, 0, 3.4e10)],
      shortMin: 2, longMin: 30, quietTest: true, lastRun: { kind: 'curto', asked: now - 5400, ended: now - 5130, result: 'interrompido', status: 'Aborted by host', quiet: true },
      tests: [{ kind: 'Short offline', status: 'Aborted by host', passed: false, hours: 9100 }, { kind: 'Short offline', status: 'Completed without error', passed: true, hours: 8800 }] },
    { key: 'D-SDD', dev: 'sdd', tran: 'sata', mode: 'auto', model: 'HD SATA 1TB', health: 'ok', passed: true, temp: 33, tempAt: now - 900, powerOnHours: 17200, powerCycles: 2100, reallocated: 0, pending: 0, uncorrectable: 0, crcErrors: 0, mediaErrors: 0, checkedAt: now - 3600,
      attrs: hdAttrs(0, 0, 33, 17200), shortMin: 2, longMin: 160, tests: [{ kind: 'Short offline', status: 'Completed without error', passed: true, hours: 17100 }] },
  ];
  const demoSpeeds = { 'D-SDD': { mbps: 112.4, bytes: 1073741824, secs: 9.55, at: now - 86400 * 2 } };
  let demoSpeedBusy = '';
  const demoEjected = new Set();
  const P = (name, size, fsType, label, mounts, used, extra = {}) => ({ name, path: `/dev/${name}`, fsType, label, size: size * GB, mounts, ...(used != null ? { used: used * GB, avail: (size * 0.985 - used) * GB } : {}), ...extra });
  const demoDiskList = () => [
    { key: 'D-SDA', dev: 'sda', model: 'HD USB 1TB', vendor: 'Genérico', serial: 'D-SDA', rev: 'AX1P5E', tran: 'usb', ptType: 'gpt', size: 1000 * GB, rotational: true, byId: '/dev/disk/by-id/usb-Generico_HD_USB_1TB_D-SDA-0:0',
      parts: [P('sda1', 1000, 'ext4', 'Disco2', ['/mnt/Disco2'], 246, { fstab: '/mnt/Disco2', hist: 'Disco2', root: 'disco2' })], first: now - 86400 * 210,
      apps: [{ urn: 'syncthing:migrated', name: 'Syncthing', folders: ['/mnt/Disco2/Backups'] }] },
    { key: 'D-SDB', dev: 'sdb', model: 'Case USB', vendor: 'Genérico', serial: 'D-SDB', tran: 'usb', ptType: 'gpt', size: 1000 * GB, rotational: true, byId: '/dev/disk/by-id/usb-Generico_Case_USB_D-SDB-0:0',
      parts: [P('sdb1', 1000, 'ext4', 'Disco3', ['/mnt/Disco3'], 462, { fstab: '/mnt/Disco3', hist: 'Disco3', root: 'disco3' })], first: now - 86400 * 180,
      apps: [{ urn: 'immich:migrated', name: 'Immich', folders: ['/mnt/Disco3/Immich'] }, { urn: 'nextcloud-mini:migrated', name: 'Nextcloud', folders: ['/mnt/Disco3/Nextcloud'] }] },
    { key: 'D-SDC', dev: 'sdc', model: 'SSD SATA 240GB', serial: 'D-SDC', rev: 'S0516A0', tran: 'sata', ptType: 'gpt', size: 360 * GB, rotational: false, system: true, byId: '/dev/disk/by-id/ata-SSD_SATA_240GB_D-SDC',
      parts: [P('sdc1', 1.1, 'vfat', '', ['/boot/efi'], 0.006), P('sdc2', 312, 'ext4', '', ['/'], 33, { hist: 'Sistema' }), P('sdc3', 46, 'swap', '', ['[SWAP]'])], first: now - 86400 * 240, apps: [] },
    { key: 'D-SDD', dev: 'sdd', model: 'HD SATA 1TB', vendor: 'ATA', serial: 'D-SDD', rev: '01.01A01', tran: 'sata', ptType: 'gpt', size: 1000 * GB, rotational: true, byId: '/dev/disk/by-id/ata-HD_SATA_1TB_D-SDD',
      parts: [P('sdd1', 1000, 'ext4', 'Disco1', ['/mnt/Disco1'], 463, { fstab: '/mnt/Disco1', hist: 'Disco1', root: 'disco1' })], first: now - 86400 * 240,
      apps: [{ urn: 'plex:migrated', name: 'Plex', folders: ['/mnt/Disco1/Filmes', '/mnt/Disco1/Séries'] }, { urn: 'transmission:migrated', name: 'Transmission', folders: ['/mnt/Disco1/Downloads'] }] },
    { key: 'D-SDE', dev: 'sde', model: 'Ultra Fit', vendor: 'SanDisk', serial: 'D-SDE', rev: '1.00', tran: 'usb', ptType: 'dos', size: 64 * GB, rotational: false, removable: true, byId: '/dev/disk/by-id/usb-SanDisk_Ultra_Fit_D-SDE-0:0',
      parts: [P('sde1', 64, 'exfat', 'PENDRIVE', [], null)], first: now - 3 * 3600, apps: [] },
  ].filter((d) => !demoEjected.has(d.key)).map((d) => {
    const sm = demoSmartDisks.find((x) => x.key === d.key);
    return { ...d, smart: sm, temp: sm?.temp, tempAt: sm?.tempAt, speed: demoSpeeds[d.key], speedBusy: demoSpeedBusy === d.dev };
  });
  const demoTimeline = {
    'D-SDA': [{ at: now - 3600, kind: 'smart', level: 'aviso', text: 'Saúde (SMART) mudou de OK para Atenção' }, { at: now - 3600, kind: 'smart', level: 'aviso', text: 'Setores com problema: 8 realocados, 1 pendentes' },
      { at: now - 86400 * 4, kind: 'temperatura', text: 'Esfriou: 48 °C' }, { at: now - 86400 * 4 - 5400, kind: 'temperatura', level: 'aviso', text: 'Chegou a 56 °C (alerta a partir de 55 °C)' },
      { at: now - 86400 * 9, kind: 'montado', text: 'Montado em /mnt/Disco2' }, { at: now - 86400 * 9 - 60, kind: 'conectado', text: 'Conectado de novo como /dev/sda' },
      { at: now - 86400 * 9 - 900, kind: 'sumiu', level: 'aviso', text: 'Sumiu da lista de discos (desligado ou desconectado)' }, { at: now - 86400 * 210, kind: 'visto', text: 'Visto pela primeira vez pelo BrunOS como /dev/sda' }],
    'D-SDE': [{ at: now - 3 * 3600, kind: 'novo', text: 'Conectado pela primeira vez como /dev/sde' }],
  };
  const demoRoots = () => rootsMeta.filter(([id, name]) => id === 'home' || demoMapped.includes(name)).map(([id, name, t, u]) => ({ id, name, kind: id === 'home' ? 'place' : 'disk', readOnly: false, total: t * GB, free: (t - u) * GB }));
  const trashItems = [];

  // Central de avisos e energia simuladas (#contagem no endereço mostra a contagem regressiva)
  let evId = 0;
  const E = (ago, kind, level, title, body, read = true) => ({ id: `d${++evId}`, time: now - ago, kind, level, title, body, read });
  let demoEvents = [
    E(60 * 8, 'login-new', 'aviso', 'Novo acesso: Safari no iPhone', 'Alguém entrou no BrunOS com Safari no iPhone pela internet (177.32.10.4). Se foi você, pode ignorar. Se não foi, troque a senha em Configurações → Trocar senha.', false),
    E(60 * 40, 'power-restored', 'info', 'A energia voltou', 'Ficou 12 min na bateria e gastou 9% da carga.', false),
    E(60 * 52, 'power-outage', 'aviso', 'A energia caiu', 'O servidor está funcionando na bateria (97%). Autonomia estimada: 2 h 10 min.', false),
    E(86400 * 2, 'disk-mounted', 'info', 'O disco Disco3 voltou', 'O disco está montado de novo.'),
    E(86400 * 2 + 600, 'disk-unmounted', 'critico', 'O disco Disco3 se desmontou', 'A pasta continua mapeada, mas não tem disco montado nela. O que for gravado ali vai para o disco do sistema. Confira o cabo e a montagem (sudo mount -a).'),
  ];
  const countdown = location.hash === '#contagem';
  // #atualizar simula um servidor configurado na 0.8.0 (comando antigo de desligar)
  const oldSetup = location.hash === '#atualizar';
  let powerCfg = oldSetup
    ? { enabled: true, threshold: 20, graceSec: 120, simulate: false, host: '192.168.1.50', port: 22, user: 'brunos-power', testedAt: now - 86400, hostSetup: 0 }
    : { enabled: countdown, threshold: 20, graceSec: 120, simulate: true, host: '', port: 22, user: 'brunos-power', testedAt: 0, hostSetup: 0 };
  // limite de carga: #semlimite (Notebook 15), #protecao (liga/desliga) ou contínuo (padrão)
  const chargeState = location.hash === '#semlimite' || oldSetup ? { kind: 'none', model: 'Acme Notebook 15' }
    : location.hash === '#protecao' ? { kind: 'toggle', model: 'Lenovo IdeaPad 3', on: false, protect: '' }
    : { kind: 'range', model: 'Lenovo ThinkPad T480', current: 100, want: 0 };
  const demoCharge = () => ({ ...chargeState, ready: !!powerCfg.testedAt && powerCfg.hostSetup >= 2, checkedAt: now });
  const demoKeyConst = () => 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDemonstracaoChaveFalsaDoBrunOSqueNaoServe brunos-power';
  const shutCmd = 'sudo -n /usr/bin/systemctl poweroff --check-inhibitors=no --when=+1min';
  const keyCmds = [`echo 'restrict,command="${shutCmd}" ${demoKeyConst()}' | sudo tee /home/brunos-power/.ssh/authorized_keys >/dev/null`,
    'sudo chown brunos-power:brunos-power /home/brunos-power/.ssh/authorized_keys', 'sudo chmod 600 /home/brunos-power/.ssh/authorized_keys',
    `echo 'brunos-power ALL=(root) NOPASSWD: ${shutCmd.slice(8)}' | sudo tee /etc/sudoers.d/brunos-power >/dev/null`, 'sudo chmod 440 /etc/sudoers.d/brunos-power', 'sudo visudo -c'];
  const powerStatus = () => countdown
    ? { phase: 'countdown', deadline: now + 95, remaining: Math.max(0, now + 95 - Math.floor(Date.now() / 1000)), hasBattery: true, onBattery: true, percent: 19, simulate: true }
    : { phase: powerCfg.enabled ? 'armed' : 'off', hasBattery: true, onBattery: false, percent: 86, simulate: powerCfg.simulate };
  const outages = [
    { start: now - 60 * 52, end: now - 60 * 40, startPct: 97, endPct: 88, minPct: 88 },
    { start: now - 86400 * 2, end: now - 86400 * 2 + 3600 * 5.7, startPct: 100, endPct: 3, minPct: 3, shutdown: 'falhou', off: true },
    { start: now - 86400 * 6, end: now - 86400 * 6 + 3600 * 1.5, startPct: 100, endPct: 41, minPct: 41, shutdown: 'simulado' },
  ];
  const demoKey = demoKeyConst();

  // Integrações simuladas (mesmo formato da API real)
  const R = (max, level) => ({ max, level });
  const demoKinds = [
    { id: 'adguard', name: 'AdGuard Home', desc: 'Consultas DNS, bloqueios e quem mais usa a rede', interval: 60,
      fields: [{ id: 'url', label: 'Endereço', type: 'url', placeholder: 'http://192.168.0.10:8104', required: true }, { id: 'user', label: 'Usuário', type: 'text', help: 'O mesmo usuário do painel do AdGuard' }, { id: 'secret', label: 'Senha', type: 'secret' }],
      metrics: [
        { id: 'queries', name: 'Consultas DNS', kind: 'num', icon: 'globe', history: 'queries_series' }, { id: 'blocked', name: 'Bloqueadas', kind: 'num', icon: 'shield', history: 'blocked_series' },
        { id: 'blocked_pct', name: '% bloqueado', kind: 'num', unit: '%', icon: 'shield' }, { id: 'avg_time', name: 'Tempo médio de resposta', kind: 'num', unit: 'ms', icon: 'clock', ranges: [R(50, 'ok'), R(200, 'aviso'), R(null, 'critico')] },
        { id: 'protection', name: 'Proteção', kind: 'text', icon: 'shield' }, { id: 'protection_on', name: 'Proteção ligada (1 = sim)', kind: 'num', icon: 'shield' }, { id: 'version', name: 'Versão', kind: 'text', icon: 'info' },
        { id: 'top_domains', name: 'Domínios mais consultados', kind: 'list', icon: 'globe' }, { id: 'top_blocked', name: 'Domínios mais bloqueados', kind: 'list', icon: 'shield' }, { id: 'top_clients', name: 'Aparelhos que mais consultam', kind: 'list', icon: 'monitor' },
        { id: 'queries_series', name: 'Consultas por hora', kind: 'series', icon: 'chart' }, { id: 'blocked_series', name: 'Bloqueios por hora', kind: 'series', icon: 'chart' },
      ],
      alerts: [{ id: 'protection_off', name: 'Proteção desligada', desc: 'Avisa quando a proteção do AdGuard é desligada', metric: 'protection_on', op: '<', default: 1, level: 'aviso', fixed: true }, { id: 'down', name: 'AdGuard sem responder', desc: 'Avisa quando o AdGuard para de responder por 5 minutos', op: 'down', level: 'aviso', fixed: true }] },
    { id: 'speedtest', name: 'Speedtest Tracker', desc: 'Velocidade da internet medida pelos testes agendados', interval: 600,
      fields: [{ id: 'url', label: 'Endereço', type: 'url', placeholder: 'http://192.168.0.10:8765', required: true }, { id: 'secret', label: 'Token da API', type: 'secret', required: true, help: 'No Speedtest Tracker: menu do usuário → API Tokens → criar com a permissão “Read results”' }],
      metrics: [
        { id: 'download', name: 'Download', kind: 'num', unit: 'Mb/s', icon: 'download', history: 'download_series' }, { id: 'upload', name: 'Upload', kind: 'num', unit: 'Mb/s', icon: 'upload', history: 'upload_series' },
        { id: 'ping', name: 'Ping', kind: 'num', unit: 'ms', icon: 'clock', history: 'ping_series', ranges: [R(40, 'ok'), R(100, 'aviso'), R(null, 'critico')] }, { id: 'jitter', name: 'Variação do ping (jitter)', kind: 'num', unit: 'ms', icon: 'clock' },
        { id: 'last_test', name: 'Último teste', kind: 'num', unit: 'ts', icon: 'clock' }, { id: 'isp', name: 'Provedor', kind: 'text', icon: 'globe' }, { id: 'server', name: 'Servidor do teste', kind: 'text', icon: 'server' },
        { id: 'download_series', name: 'Download (7 dias)', kind: 'series', unit: 'Mb/s', icon: 'chart' }, { id: 'upload_series', name: 'Upload (7 dias)', kind: 'series', unit: 'Mb/s', icon: 'chart' }, { id: 'ping_series', name: 'Ping (7 dias)', kind: 'series', unit: 'ms', icon: 'chart' },
      ],
      alerts: [{ id: 'slow_download', name: 'Download baixo', desc: 'Avisa quando o último teste der menos que isto', metric: 'download', op: '<', default: 100, unit: 'Mb/s', level: 'aviso' }, { id: 'high_ping', name: 'Ping alto', desc: 'Avisa quando o último teste der mais que isto', metric: 'ping', op: '>', default: 100, unit: 'ms', level: 'info' }, { id: 'down', name: 'Speedtest sem responder', desc: 'Avisa quando o Speedtest Tracker para de responder', op: 'down', level: 'info', fixed: true }] },
  ];
  let demoInteg = [
    { id: 'ag1', kind: 'adguard', name: 'AdGuard Home', url: 'http://192.168.1.50:8104', user: 'admin', hasSecret: true, interval: 60, alerts: { protection_off: { on: true } } },
    { id: 'st1', kind: 'speedtest', name: 'Speedtest Tracker', url: 'http://192.168.1.50:8765', hasSecret: true, interval: 600, alerts: { slow_download: { on: true, value: 100 } } },
  ];
  const hourStart = Math.floor(Date.now() / 3600e3) * 3600;
  const qSeries = Array.from({ length: 24 }, (_, i) => { const h = new Date((hourStart - (23 - i) * 3600) * 1000).getHours(); return Math.round((h >= 8 && h <= 23 ? 900 : 180) * (0.7 + Math.random() * 0.6)); });
  const bSeries = qSeries.map((q) => Math.round(q * (0.18 + Math.random() * 0.1)));
  const speedPts = (base, spread) => Array.from({ length: 168 }, (_, i) => (i % 6 === 5 ? +(base + (Math.random() - 0.5) * spread).toFixed(1) : null));
  const dlS = speedPts(290, 60), ulS = speedPts(95, 20), pgS = speedPts(10, 6);
  const agValues = () => {
    const queries = qSeries.reduce((s, v) => s + v, 0), blocked = bSeries.reduce((s, v) => s + v, 0);
    return {
      queries: { n: queries }, blocked: { n: blocked }, blocked_pct: { n: +(blocked / queries * 100).toFixed(1) }, avg_time: { n: +(12 + Math.random() * 6).toFixed(1) },
      protection: { t: 'Ligada' }, protection_on: { n: 1 }, version: { t: 'v0.107.52' },
      top_domains: { l: [['connectivitycheck.gstatic.com', 2210], ['api.spotify.com', 1630], ['graph.instagram.com', 1222], ['i.ytimg.com', 940], ['web.whatsapp.com', 812], ['plex.tv', 433]].map(([k, n]) => ({ k, n })) },
      top_blocked: { l: [['doubleclick.net', 712], ['app-measurement.com', 588], ['ads.youtube.com', 421], ['graph.facebook.com', 301], ['metrics.icloud.com', 187], ['telemetry.microsoft.com', 140], ['adservice.google.com', 98], ['tracking.miui.com', 61]].map(([k, n]) => ({ k, n })) },
      top_clients: { l: [['192.168.1.20 (celular)', 6120], ['192.168.1.31 (TV)', 4410], ['192.168.1.50 (servidor)', 2980]].map(([k, n]) => ({ k, n })) },
      queries_series: { s: { step: 3600, start: hourStart - 23 * 3600, points: qSeries } }, blocked_series: { s: { step: 3600, start: hourStart - 23 * 3600, points: bSeries } },
    };
  };
  const stValues = () => ({
    download: { n: 287.4 }, upload: { n: 94.1 }, ping: { n: 9.8 }, jitter: { n: 1.2 }, last_test: { n: now - 1800 }, isp: { t: 'Vivo Fibra' }, server: { t: 'Claro · São Paulo' },
    download_series: { s: { step: 3600, start: hourStart - 167 * 3600, points: dlS } }, upload_series: { s: { step: 3600, start: hourStart - 167 * 3600, points: ulS } }, ping_series: { s: { step: 3600, start: hourStart - 167 * 3600, points: pgS } },
  });
  // 0.9.2: Immich, Plex, WatchYourLAN e Tailscale (fotos e capas = papéis de parede que vêm no BrunOS)
  const M = (id, name, kind, extra = {}) => ({ id, name, kind, ...extra });
  demoKinds.push(
    { id: 'immich', name: 'Immich', desc: 'Fotos e vídeos: totais e fotos no formato Mídia (neste dia, aleatórias, favoritas ou de um álbum)', interval: 600,
      fields: [{ id: 'url', label: 'Endereço', type: 'url', placeholder: 'http://192.168.0.10:2283', required: true }, { id: 'secret', label: 'Chave de API', type: 'secret', required: true, help: 'No Immich: foto do perfil → Configurações da conta → Chaves de API → Nova chave.' }, { id: 'album', label: 'Álbum (opcional)', type: 'text', placeholder: 'ex.: Viagens', help: 'Nome do álbum usado na métrica “Fotos do álbum”' }],
      metrics: [M('photos', 'Fotos', 'num', { icon: 'image' }), M('videos', 'Vídeos', 'num', { icon: 'film' }), M('total', 'Fotos e vídeos', 'num', { icon: 'image' }), M('usage', 'Espaço usado (chave de administrador)', 'num', { unit: 'B', icon: 'disks' }), M('version', 'Versão', 'text', { icon: 'info' }),
        M('photo_today', 'Fotos: neste dia', 'media', { icon: 'image', look: 'photo' }), M('photo_random', 'Fotos: aleatórias', 'media', { icon: 'image', look: 'photo' }), M('photo_favorite', 'Fotos: favoritas', 'media', { icon: 'star', look: 'photo' }), M('photo_album', 'Fotos: do álbum escolhido', 'media', { icon: 'image', look: 'photo' })],
      alerts: [{ id: 'down', name: 'Immich sem responder', op: 'down', level: 'aviso', fixed: true }] },
    { id: 'plex', name: 'Plex', desc: 'O que está tocando agora, com a capa, e o tamanho das bibliotecas', interval: 15,
      fields: [{ id: 'url', label: 'Endereço', type: 'url', placeholder: 'http://192.168.0.10:32400', required: true }, { id: 'secret', label: 'Token do Plex', type: 'secret', required: true, help: 'No Plex pelo navegador: abra qualquer filme → ⋯ → Obter informações → Ver XML. O token é o texto depois de “X-Plex-Token=”.' }],
      metrics: [M('sessions', 'Assistindo agora', 'num', { icon: 'film' }), M('transcodes', 'Convertendo agora (transcode)', 'num', { icon: 'cpu' }), M('now_playing', 'O que está tocando', 'list', { icon: 'film' }), M('now_media', 'Capa do que está tocando', 'media', { icon: 'film', look: 'poster' }), M('movies', 'Filmes', 'num', { icon: 'film' }), M('shows', 'Séries', 'num', { icon: 'monitor' }), M('artists', 'Artistas (música)', 'num', { icon: 'music' }), M('version', 'Versão', 'text', { icon: 'info' })],
      alerts: [{ id: 'down', name: 'Plex sem responder', op: 'down', level: 'aviso', fixed: true }] },
    { id: 'watchyourlan', name: 'WatchYourLAN', desc: 'Quem está na rede de casa e aviso de aparelho desconhecido', interval: 120,
      fields: [{ id: 'url', label: 'Endereço', type: 'url', placeholder: 'http://192.168.0.10:8840', required: true }],
      metrics: [M('online', 'Aparelhos online', 'num', { icon: 'wifi' }), M('offline', 'Aparelhos desligados', 'num', { icon: 'wifi' }), M('total', 'Aparelhos no total', 'num', { icon: 'network' }), M('known', 'Aparelhos conhecidos', 'num', { icon: 'check' }), M('unknown', 'Aparelhos desconhecidos', 'num', { icon: 'alert' }), M('online_list', 'Lista: online agora', 'list', { icon: 'wifi' }), M('unknown_list', 'Lista: desconhecidos', 'list', { icon: 'alert' })],
      alerts: [{ id: 'new_device', name: 'Aparelho novo na rede', desc: 'Avisa quando aparece um aparelho que o WatchYourLAN marca como desconhecido', metric: 'unknown_list', op: 'new', level: 'aviso', fixed: true }, { id: 'down', name: 'WatchYourLAN sem responder', op: 'down', level: 'info', fixed: true }] },
    { id: 'tailscale', name: 'Tailscale', desc: 'Aparelhos da sua rede Tailscale: quem está online e o IP deste servidor', interval: 60, noUrl: true, fields: [],
      note: 'Usa a mesma conexão SSH do desligamento seguro (Configurações → Energia) e precisa do script do servidor na versão 3: copie os comandos de atualização abaixo e cole no servidor uma vez.',
      metrics: [M('state', 'Situação', 'text', { icon: 'shield' }), M('state_ok', 'Conectado (1 = sim)', 'num', { icon: 'shield' }), M('online', 'Aparelhos online', 'num', { icon: 'network' }), M('total', 'Aparelhos na rede', 'num', { icon: 'network' }), M('peers', 'Lista de aparelhos', 'list', { icon: 'network' }), M('self_ip', 'IP deste servidor', 'text', { icon: 'server' }), M('exit_node', 'Saída para a internet', 'text', { icon: 'globe' }), M('health', 'Avisos do Tailscale', 'text', { icon: 'alert' })],
      alerts: [{ id: 'disconnected', name: 'Tailscale desconectado', metric: 'state_ok', op: '<', default: 1, level: 'aviso', fixed: true }] },
  );
  demoKinds.push(
    { id: 'transmission', name: 'Transmission', desc: 'Velocidade, torrents ativos e o que está baixando agora', interval: 30,
      fields: [{ id: 'url', label: 'Endereço', type: 'url', placeholder: 'http://192.168.0.10:9091', required: true }, { id: 'user', label: 'Usuário (se tiver)', type: 'text' }, { id: 'secret', label: 'Senha (se tiver)', type: 'secret' }],
      metrics: [M('down', 'Download agora', 'num', { unit: 'B/s', icon: 'download' }), M('up', 'Upload agora', 'num', { unit: 'B/s', icon: 'upload' }), M('downloading', 'Baixando', 'num', { icon: 'download' }), M('seeding', 'Semeando', 'num', { icon: 'upload' }), M('active', 'Ativos', 'num', { icon: 'activity' }), M('paused', 'Pausados', 'num', { icon: 'clock' }), M('total', 'Torrents no total', 'num', { icon: 'box' }), M('errors', 'Com erro', 'num', { icon: 'alert' }), M('downloaded', 'Baixado no total', 'num', { unit: 'B', icon: 'download' }), M('uploaded', 'Enviado no total', 'num', { unit: 'B', icon: 'upload' }), M('downloading_list', 'Lista: baixando agora', 'list', { icon: 'download' })],
      alerts: [{ id: 'errors', name: 'Torrent com erro', desc: 'Avisa quando algum torrent fica com erro', metric: 'errors', op: '>', default: 0, level: 'info', fixed: true }, { id: 'down', name: 'Transmission sem responder', op: 'down', level: 'info', fixed: true }] },
    { id: 'syncthing', name: 'Syncthing', desc: 'Pastas sincronizadas, quanto falta e aparelhos conectados', interval: 60,
      fields: [{ id: 'url', label: 'Endereço', type: 'url', placeholder: 'http://192.168.0.10:8384', required: true }, { id: 'secret', label: 'Chave da API', type: 'secret', required: true, help: 'No Syncthing: Ações → Configurações → Geral → Chave da API' }],
      metrics: [M('folders', 'Pastas', 'num', { icon: 'folder' }), M('synced', 'Pastas em dia', 'num', { icon: 'check' }), M('need', 'Falta sincronizar', 'num', { unit: 'B', icon: 'refresh' }), M('errors', 'Arquivos com erro', 'num', { icon: 'alert' }), M('devices_online', 'Aparelhos conectados', 'num', { icon: 'network' }), M('devices', 'Aparelhos no total', 'num', { icon: 'network' }), M('folders_list', 'Lista: pastas', 'list', { icon: 'folder' }), M('devices_list', 'Lista: aparelhos', 'list', { icon: 'network' }), M('version', 'Versão', 'text', { icon: 'info' }), M('uptime', 'Tempo ligado', 'num', { unit: 's', icon: 'clock' })],
      alerts: [{ id: 'errors', name: 'Erro de sincronização', desc: 'Avisa quando alguma pasta tem arquivos que não conseguem sincronizar', metric: 'errors', op: '>', default: 0, level: 'aviso', fixed: true }, { id: 'down', name: 'Syncthing sem responder', op: 'down', level: 'info', fixed: true }] },
    { id: 'nextcloud', name: 'Nextcloud', desc: 'Usuários, arquivos, compartilhamentos e espaço livre', interval: 300,
      fields: [{ id: 'url', label: 'Endereço', type: 'url', placeholder: 'http://192.168.0.10:8083', required: true }, { id: 'user', label: 'Usuário administrador (deixe vazio para usar o token)', type: 'text' }, { id: 'secret', label: 'Token do serverinfo (ou senha de app)', type: 'secret', required: true, help: 'Recomendado: um token só de leitura do app serverinfo.' }],
      metrics: [M('users', 'Usuários', 'num', { icon: 'user' }), M('active_5m', 'Ativos nos últimos 5 min', 'num', { icon: 'activity' }), M('active_24h', 'Ativos nas últimas 24 h', 'num', { icon: 'activity' }), M('files', 'Arquivos', 'num', { icon: 'doc' }), M('shares', 'Compartilhamentos', 'num', { icon: 'link' }), M('free', 'Espaço livre', 'num', { unit: 'B', icon: 'disks' }), M('free_gb', 'Espaço livre (GB)', 'num', { unit: 'GB', icon: 'disks' }), M('db_size', 'Tamanho do banco de dados', 'num', { unit: 'B', icon: 'box' }), M('version', 'Versão', 'text', { icon: 'info' })],
      alerts: [{ id: 'low_space', name: 'Pouco espaço no Nextcloud', desc: 'Avisa quando o espaço livre fica abaixo disto', metric: 'free_gb', op: '<', default: 20, unit: 'GB', level: 'aviso' }, { id: 'down', name: 'Nextcloud sem responder', op: 'down', level: 'aviso', fixed: true }] },
  );
  demoInteg.push(
    { id: 'tr1', kind: 'transmission', name: 'Transmission', url: 'http://192.168.1.50:9091', hasSecret: false, interval: 30, alerts: {}, options: {} },
    { id: 'sy1', kind: 'syncthing', name: 'Syncthing', url: 'http://192.168.1.50:8384', hasSecret: true, interval: 60, alerts: { errors: { on: true } }, options: {} },
    { id: 'nc1', kind: 'nextcloud', name: 'Nextcloud', url: 'http://192.168.1.50:8083', hasSecret: true, interval: 300, alerts: { low_space: { on: true, value: 20 } }, options: {} },
  );
  demoInteg.push(
    { id: 'im1', kind: 'immich', name: 'Immich', url: 'http://192.168.1.50:2283', hasSecret: true, interval: 600, alerts: {}, options: { album: 'Viagens' } },
    { id: 'px1', kind: 'plex', name: 'Plex', url: 'http://192.168.1.50:32400', hasSecret: true, interval: 15, alerts: {}, options: {} },
    { id: 'wy1', kind: 'watchyourlan', name: 'WatchYourLAN', url: 'http://192.168.1.50:8840', hasSecret: false, interval: 120, alerts: { new_device: { on: true } }, options: {} },
    { id: 'ts1', kind: 'tailscale', name: 'Tailscale', url: '', hasSecret: false, interval: 60, alerts: {}, options: {} },
  );
  const photo = (ref, k, v) => ({ k, v, id: ref, img: ref });
  const imValues = () => ({ photos: { n: 18342 }, videos: { n: 612 }, total: { n: 18954 }, version: { t: 'v2.1.0' },
    photo_today: { l: [photo('14', '8 de nov. de 2019', 'há 7 anos · Ouro Preto, Brasil'), photo('19', '8 de nov. de 2021', 'há 5 anos · Florianópolis, Brasil'), photo('6', '8 de nov. de 2023', 'há 3 anos')] },
    photo_random: { l: ['2', '23', '16', '4', '1', '12', '13', '10'].map((r, i) => photo(r, `${3 + i} de mar. de 20${18 + (i % 6)}`, ['Serra da Mantiqueira', 'Chapada Diamantina', 'Lençóis', 'Bonito'][i % 4])) },
    photo_favorite: { l: [photo('24', '1 de jan. de 2024', 'Praia do Rosa'), photo('12', '15 de jul. de 2022', 'Campos do Jordão')] },
    photo_album: { l: [photo('16', '20 de abr. de 2025', 'Viagens · Patagônia'), photo('4', '22 de abr. de 2025', 'Viagens · Patagônia')] } });
  const pxValues = () => ({ sessions: { n: 2 }, transcodes: { n: 1 }, movies: { n: 412 }, shows: { n: 37 }, artists: { n: 128 }, version: { t: '1.41.2' },
    now_playing: { l: [{ k: 'Minha Série · T2E5 · O Começo', v: 'Ana · TV da sala', n: 38 }, { k: 'Um Filme', v: 'Bruno · Celular · pausado', n: 72 }] },
    now_media: { l: [{ k: 'Minha Série · T2E5 · O Começo', v: 'Ana · TV da sala', n: 38, img: '13' }, { k: 'Um Filme', v: 'Bruno · Celular · pausado', n: 72, img: '10' }] } });
  const dev = (k, v, id) => ({ k, v, id });
  const wyValues = () => ({ online: { n: 14 }, offline: { n: 9 }, total: { n: 23 }, known: { n: 21 }, unknown: { n: 2 },
    online_list: { l: [dev('Notebook do Bruno', '192.168.1.20 · Dell'), dev('TV da sala', '192.168.1.31 · LG'), dev('iPhone', '192.168.1.40 · Apple'), dev('servidor (servidor)', '192.168.1.50 · Dell'), dev('Chromecast', '192.168.1.61 · Google'), dev('Impressora', '192.168.1.70 · HP')] },
    unknown_list: { l: [dev('galaxy-a54', '192.168.1.88 · Samsung', 'bb:bb'), dev('192.168.1.93', 'Espressif (ESP32)', 'cc:cc')] } });
  const tsValues = () => ({ state: { t: 'Conectado' }, state_ok: { n: 1 }, online: { n: 3 }, total: { n: 5 }, self_ip: { t: '100.64.0.1' }, exit_node: { t: 'direto (sem saída por outro aparelho)' }, health: { t: 'nenhum' },
    peers: { l: [dev('iphone-bruno', 'online · 100.64.0.2 · iOS'), dev('notebook', 'online · 100.64.0.3 · windows'), dev('tablet', 'online · 100.64.0.4 · android'), dev('pc-trabalho', 'há 3 h · 100.64.0.5 · windows'), dev('raspberry', 'há 12 dias · 100.64.0.6 · linux')] } });
  const trValues = () => ({ down: { n: 2.1e6 + Math.random() * 4e5 }, up: { n: 48e3 }, downloading: { n: 2 }, seeding: { n: 7 }, active: { n: 9 }, paused: { n: 1 }, total: { n: 12 }, errors: { n: 0 }, downloaded: { n: 5.2e11 }, uploaded: { n: 1.9e11 },
    downloading_list: { l: [{ k: 'ubuntu-24.04-live-server-amd64.iso', v: '82% · 1,8 MB/s · falta 2 min' }, { k: 'Documentário (2024) 1080p', v: '38% · 340 KB/s · falta 41 min' }] } });
  const syValues = () => ({ folders: { n: 4 }, synced: { n: 3 }, need: { n: 2.5e9 }, errors: { n: 0 }, devices_online: { n: 2 }, devices: { n: 3 }, version: { t: 'v1.29.0' }, uptime: { n: 3 * 86400 },
    folders_list: { l: [dev('Fotos do celular', 'em dia'), dev('Documentos', 'em dia'), dev('Música', 'sincronizando · falta 2,5 GB'), dev('Backups', 'em dia')] },
    devices_list: { l: [dev('Celular do Bruno', 'conectado'), dev('Notebook', 'conectado'), dev('PC do trabalho', 'desconectado')] } });
  const ncValues = () => ({ users: { n: 4 }, active_5m: { n: 1 }, active_24h: { n: 3 }, files: { n: 120345 }, shares: { n: 17 }, free: { n: 1.5e11 }, free_gb: { n: 150 }, db_size: { n: 5.4e8 }, version: { t: '30.0.2' } });
  // 0.10.0: Clima (Open-Meteo)
  demoKinds.push({ id: 'weather', name: 'Clima', desc: 'Tempo agora e previsão dos próximos dias (Open-Meteo, sem chave)', interval: 900, noUrl: true,
    note: 'Os dados vêm do Open-Meteo, um serviço gratuito. Ele recebe só as coordenadas da cidade.',
    fields: [{ id: 'city', label: 'Cidade', type: 'city', placeholder: 'Digite o nome da cidade', required: true, help: 'Escreva o nome e escolha na lista' }, { id: 'lat', label: 'Latitude', type: 'hidden' }, { id: 'lon', label: 'Longitude', type: 'hidden' }],
    metrics: [M('temp', 'Temperatura agora', 'num', { unit: '°C', icon: 'temp' }), M('feels', 'Sensação térmica', 'num', { unit: '°C', icon: 'temp' }), M('cond', 'Condição', 'text', { icon: 'wx-partly' }), M('max', 'Máxima de hoje', 'num', { unit: '°C', icon: 'arrowUp' }), M('min', 'Mínima de hoje', 'num', { unit: '°C', icon: 'arrowDown' }), M('rain', 'Chance de chuva hoje', 'num', { unit: '%', icon: 'wx-rain' }), M('humidity', 'Umidade', 'num', { unit: '%', icon: 'wx-drop' }), M('wind', 'Vento', 'num', { unit: 'km/h', icon: 'wx-wind' }), M('forecast', 'Previsão (7 dias)', 'list', { icon: 'wx-partly' }), M('city', 'Cidade', 'text', { icon: 'home' })],
    alerts: [{ id: 'hot', name: 'Calor forte', desc: 'Avisa quando a temperatura passar disto', metric: 'temp', op: '>', default: 35, unit: '°C', level: 'info' }, { id: 'rain', name: 'Chuva provável hoje', desc: 'Avisa quando a chance de chuva hoje passar disto', metric: 'rain', op: '>', default: 70, unit: '%', level: 'info' }] });
  demoKinds.push({ id: 'tuya', name: 'Tomada Tuya', desc: 'Consumo medido na tomada (W, V, A e kWh), pela rede de casa, sem nuvem', interval: 30, noUrl: true,
    note: 'Precisa do IP, do ID e da chave local da tomada (tirados uma vez com o tinytuya). A chave fica guardada cifrada no BrunOS.',
    fields: [{ id: 'host', label: 'IP da tomada', type: 'text', placeholder: '192.168.1.60', required: true }, { id: 'devid', label: 'ID da tomada (Device ID)', type: 'text', placeholder: 'bf12...', required: true },
      { id: 'secret', label: 'Chave local (local key)', type: 'secret', required: true, help: 'No arquivo devices.json do tinytuya, campo "key" (16 caracteres)' }, { id: 'version', label: 'Versão do protocolo', type: 'text', placeholder: '3.5', help: 'Aparece no "tinytuya scan" (3.3, 3.4 ou 3.5). Vazio = 3.5' }],
    metrics: [M('power', 'Potência agora', 'num', { unit: 'W', icon: 'bolt' }), M('voltage', 'Tensão', 'num', { unit: 'V', icon: 'bolt' }), M('current', 'Corrente', 'num', { unit: 'A', icon: 'bolt' }), M('energy', 'Energia somada pela tomada', 'num', { unit: 'kWh', icon: 'chart' }), M('on', 'Ligada (1 = sim)', 'num', { icon: 'power' })],
    alerts: [{ id: 'down', name: 'Tomada sem responder', desc: 'Avisa quando a tomada para de responder por 5 minutos', op: 'down', level: 'info', fixed: true }] });
  demoInteg.push({ id: 'tu1', kind: 'tuya', name: 'Tomada do servidor', url: '', hasSecret: true, interval: 30, alerts: {}, options: { host: '192.168.1.60', devid: 'bf12xxxxxxxx', version: '3.5' } });
  demoInteg.push({ id: 'wx1', kind: 'weather', name: 'Clima', url: '', hasSecret: false, interval: 900, alerts: {}, options: { city: 'Belo Horizonte, Minas Gerais', lat: '-19.92', lon: '-43.94' } });
  const WD = ['Dom', 'Seg', 'Ter', 'Qua', 'Qui', 'Sex', 'Sáb'];
  const wxValues = () => {
    const night = new Date().getHours() < 6 || new Date().getHours() >= 18;
    const days = [['wx-partly', 29, 18, 10], ['wx-rain', 25, 17, 80], ['wx-storm', 24, 18, 90], ['wx-cloud', 26, 17, 30], ['wx-sun', 30, 16, 0], ['wx-sun', 31, 17, 0], ['wx-drizzle', 27, 18, 40]];
    return { temp: { n: 26.4 }, feels: { n: 27.9 }, cond: { t: 'Parcialmente nublado', i: night ? 'wx-partly-night' : 'wx-partly' }, max: { n: 29 }, min: { n: 18 }, rain: { n: 10 }, humidity: { n: 61 }, wind: { n: 12 }, city: { t: 'Belo Horizonte' },
      forecast: { l: days.map(([i, mx, mn, p], d) => ({ k: d ? WD[(new Date().getDay() + d) % 7] : 'Hoje', v: `${mx}° / ${mn}°${p >= 30 ? ` · ${p}%` : ''}`, i })) } };
  };
  const demoValues = (it) => ({ adguard: agValues, speedtest: stValues, immich: imValues, plex: pxValues, watchyourlan: wyValues, tailscale: tsValues, transmission: trValues, syncthing: syValues, nextcloud: ncValues, weather: wxValues, tuya: () => ({ power: { n: +(16.5 + Math.random()).toFixed(1) }, voltage: { n: 125.3 }, current: { n: 0.203 }, energy: { n: 0.09 }, on: { n: 1 } }) }[it.kind] || (() => ({})))();
  const demoViews = () => demoInteg.map((it) => ({ id: it.id, at: Math.floor(Date.now() / 1000), values: demoValues(it) }));
  const nodeAt = (root, path) => {
    let n = tree[root];
    for (const p of (path || '').split('/').filter(Boolean)) { n = n?.children?.find((c) => c.name === p); }
    if (!n) throw new Error('arquivo ou pasta não encontrado');
    return n;
  };
  const split = (p) => { const i = p.lastIndexOf('/'); return [i < 0 ? '' : p.slice(0, i), i < 0 ? p : p.slice(i + 1)]; };
  const entryOf = (c) => ({ name: c.name, dir: !!c.dir, size: c.dir ? 0 : c.size, mtime: c.mtime, hidden: c.name.startsWith('.') });
  const wait = (v, ms = 120) => new Promise((r) => setTimeout(() => r(structuredClone(v)), ms));

  return {
    demo: true,
    public: () => wait({ setupRequired: false, displayName: 'Bruno', version: 'demo', wallpaper: settings.wallpaper, accentMode: settings.accentMode, manualHue: settings.manualHue }, 30),
    setup: () => { logged = true; return wait({ ok: true }); },
    login: () => { logged = true; return wait({ ok: true }, 300); },
    logout: () => { logged = false; return wait({ ok: true }); },
    me: () => logged ? wait({ username: 'bruno', displayName: settings.displayName || 'Bruno' }) : Promise.reject(new Error('faça login')),
    updateMe: (b) => { settings.displayName = b.displayName; return wait({ ok: true }); },
    password: () => wait({ ok: true }),
    lockState: () => wait({ locked: false }),
    lock: () => wait({ locked: true }),
    unlock: (pw) => pw ? wait({ locked: false }, 300) : Promise.reject(Object.assign(new Error('senha incorreta'), { status: 403 })),
    notes: (() => {
      const k = 'brunos.demo.notes';
      const all = () => { try { return JSON.parse(localStorage.getItem(k) || '{}'); } catch { return {}; } };
      const put = (m) => { try { localStorage.setItem(k, JSON.stringify(m)); } catch { /* sem storage */ } };
      const seed = { text: '[x] Trocar o filtro da ventoinha\n[ ] Atualizar o Immich\n[ ] Backup dos HDs no domingo', at: 0 };
      return {
        get: (id) => wait(all()[id] || (id.startsWith('demo') ? seed : { text: '', at: 0 }), 40),
        put: (id, text) => { const m = all(); m[id] = { text, at: Date.now() }; put(m); return wait(m[id], 60); },
        del: (id) => { const m = all(); delete m[id]; put(m); return wait({ ok: true }); },
      };
    })(),
    // 0.11.0: consumo (tomada de ~17 W), SMART e atualizações simulados
    energy: (() => {
      let cfg = { meter: 'auto', plugId: '', tariff: 0.9 };
      const t0 = Math.floor(Date.now() / 60000) * 60 - 1439 * 60;
      const minutes = Array.from({ length: 1440 }, (_, i) => ({ t: t0 + i * 60, v: +(15 + Math.sin(i / 90) * 2 + (i % 240 < 30 ? 6 : 0) + Math.random()).toFixed(1) }));
      const days = Array.from({ length: 30 }, (_, i) => { const d = new Date(Date.now() - (29 - i) * 86400e3); const k = +(0.38 + Math.random() * 0.08).toFixed(3); return { date: d.toISOString().slice(0, 10), kWh: k, cost: +(k * cfg.tariff).toFixed(2) }; });
      const now = () => { const day = new Date().getDate(), month = days.slice(-day).reduce((s, d) => s + d.kWh, 0); return { w: +(16.5 + Math.random()).toFixed(1), meter: 'plug', todayKWh: 0.274, monthKWh: +month.toFixed(3), tariff: cfg.tariff, todayCost: +(0.274 * cfg.tariff).toFixed(2), monthCost: +(month * cfg.tariff).toFixed(2), projection: +(month / day * 30 * cfg.tariff).toFixed(2), offset: 7.2, at: Math.floor(Date.now() / 1000) }; };
      return { get: () => wait({ config: cfg, now: now() }), setConfig: (c) => { cfg = { ...cfg, ...c }; return wait({ config: cfg, now: now() }); }, history: () => wait({ minutes, days }, 150) };
    })(),
    smart: (() => {
      const disks = demoSmartDisks;
      // teste do próprio disco: o andamento cai 10% a cada leitura e termina sem erros
      // (a cada leitura cai 10%; no fim fica o resultado no lastRun e quanto levou)
      const step = () => disks.forEach((d) => {
        if (!d.test) return;
        d.test.remaining = Math.max(0, 100 - Math.floor((Date.now() / 1000 - d.testAsked) / 6) * 10);
        if (d.test.remaining > 0) return;
        const t = Math.floor(Date.now() / 1000);
        d.tests = [{ kind: d.test.kind === 'longo' ? 'Extended offline' : 'Short offline', status: 'Completed without error', passed: true, hours: d.powerOnHours }, ...(d.tests || [])].slice(0, 5);
        d.lastRun = { kind: d.test.kind, asked: d.testAsked, ended: t, result: 'ok', status: 'Completed without error' };
        if (d.test.kind === 'curto') d.shortTook = t - d.testAsked;
        d.test = null; d.testAsked = 0;
      });
      return { get: () => { step(); return wait({ disks, checkedAt: now - 3600 }); }, refresh: () => wait({ disks, checkedAt: Math.floor(Date.now() / 1000) }, 900), retry: () => wait({ disks, checkedAt: now - 3600 }, 1500),
        selftest: (key, kind) => { const d = disks.find((x) => x.key === key); if (d?.test) return Promise.reject(Object.assign(new Error('este disco já está fazendo um teste: espere terminar'), { status: 409 })); d.test = { kind, remaining: 100 }; d.testKind = kind; d.testAsked = Math.floor(Date.now() / 1000); return wait({ disks, checkedAt: now - 3600 }, 700); } };
    })(),
    updates: (() => {
      const s = { apps: [{ urn: 'plex:migrated', id: 'plex', store: 'migrated', name: 'Plex', installed: '1.41.2', available: '1.42.1' }, { urn: 'immich:migrated', id: 'immich', store: 'migrated', name: 'Immich', installed: 'v2.1.0', available: 'v2.2.0' }],
        os: { total: 12, security: 3, reboot: true, packages: ['linux-image-generic', 'linux-headers-generic', 'openssl', 'libssl3t64', 'tzdata', 'snapd', 'curl', 'libcurl4t64', 'python3.12', 'vim', 'git', 'docker-ce'], checkedAt: now - 7200 }, checkedAt: now - 7200 };
      s.every = '24h';
      s.script = { have: 7, want: 7, auto: true, checkedAt: now - 60 };
      // demonstração da atualização: o andamento avança a cada leitura e termina em ~12 s
      const LOG = ['Hit:1 http://archive.ubuntu.com/ubuntu noble InRelease', 'Reading package lists...', 'Calculating upgrade...', 'The following packages will be upgraded:', 'Unpacking openssl (3.0.13-0ubuntu3.5)...', 'Setting up openssl (3.0.13-0ubuntu3.5)...', 'Processing triggers for man-db...'];
      const APPLOG = ['Pulling images...', 'Stopping app...', 'Updating app files...', 'Starting app...', 'App updated successfully'];
      const BKLOG = ['Backing up app...', 'Compressing app-data...', 'Backup created: backup-20261112.tar.gz'];
      const tick = () => {
        const j = s.job; if (!j?.running) return;
        const lines = j.kind === 'linux' ? LOG : j.backup ? [...BKLOG, ...APPLOG] : APPLOG, n = Math.floor((Date.now() / 1000 - j.startedAt) / 2);
        j.lines = lines.slice(0, Math.min(n + 1, lines.length));
        if (j.backup) j.phase = n < BKLOG.length ? 'backup' : 'atualizando';
        if (n >= lines.length) {
          Object.assign(j, { running: false, ok: true, finishedAt: Math.floor(Date.now() / 1000) });
          if (j.kind === 'linux') s.os = { ...s.os, total: 0, security: 0, phased: 2, packages: ['snapd', 'distro-info'] }; else s.apps = s.apps.filter((a) => a.urn !== j.target);
        }
      };
      return {
        get: () => { tick(); return wait(s); },
        check: (part) => {
          const t = Math.floor(Date.now() / 1000);
          s.checkedAt = t;
          if (part !== 'linux') { s.appsAt = t; s.storesAt = t; }
          return wait(s, part ? 900 : 1500);
        },
        setEvery: (every) => { s.every = every; return wait(s); },
        run: (kind, urn, backup) => {
          if (s.job?.running) return Promise.reject(Object.assign(new Error('já tem uma atualização rodando: espere ela terminar'), { status: 409 }));
          const u = s.apps.find((a) => a.urn === urn);
          s.job = { kind, target: urn || '', name: kind === 'linux' ? 'Linux' : u?.name || urn, from: u?.installed, to: u?.available, startedAt: Math.floor(Date.now() / 1000), running: true, lines: [], ok: false, backup: kind === 'app' && !!backup, phase: backup ? 'backup' : '' };
          return wait(s, 400);
        },
        backupSize: () => wait({ size: 1.8e9, free: 640e9 }, 900),
        script: () => wait(s, 600),
      };
    })(),
    geocode: (q) => wait([['Belo Horizonte', 'Minas Gerais', -19.92, -43.94], ['São Paulo', 'São Paulo', -23.55, -46.63], ['Rio de Janeiro', 'Rio de Janeiro', -22.91, -43.17], ['Florianópolis', 'Santa Catarina', -27.6, -48.55], ['Lisboa', 'Lisboa', 38.72, -9.14]]
      .filter(([n]) => n.toLowerCase().includes(q.toLowerCase().slice(0, 3))).map(([name, region, lat, lon]) => ({ name, region, country: name === 'Lisboa' ? 'Portugal' : 'Brasil', lat, lon })), 200),
    settings: () => wait(settings, 30),
    saveSettings: (v) => { settings = v; try { localStorage.setItem(LS, JSON.stringify(v)); } catch { /* sem storage */ } return wait({ ok: true }, 10); },
    settingsBackups: () => wait([0, 1, 2].map((i) => ({ date: new Date(Date.now() - i * 864e5).toISOString().slice(0, 10), widgets: Object.keys(settings?.widgets2?.items || {}).length, pages: Object.values(settings?.widgets2?.areas || {}).filter((l) => l.length).length }))),
    settingsRestore: () => wait({ ok: true }),
    wallpapers: () => wait([]),
    wallpaperUrl: () => '',
    publicWallpaperUrl: () => '',
    uploadWallpaper: () => Promise.reject(new Error('No modo demonstração a imagem fica só nesta aba')),
    deleteWallpaper: () => wait({ ok: true }),
    stats: () => wait({ snapshot: snap(), history }),
    events: {
      list: () => wait({ events: demoEvents, unread: demoEvents.filter((e) => !e.read).length }),
      read: (ids = []) => { demoEvents.forEach((e) => { if (!ids.length || ids.includes(e.id)) e.read = true; }); return wait({ unread: demoEvents.filter((e) => !e.read).length }); },
      clear: () => { demoEvents = []; return wait({ unread: 0 }); },
    },
    disks: {
      forecast: () => wait({ disks: forecast }),
      list: () => wait({ disks: demoDiskList(), checkedAt: Math.floor(Date.now() / 1000) - 40 }, 150),
      refresh: () => wait({ disks: demoDiskList(), checkedAt: Math.floor(Date.now() / 1000) }, 700),
      timeline: (key) => wait({ events: demoTimeline[key] || [{ at: now - 86400 * 240, kind: 'visto', text: 'Visto pela primeira vez pelo BrunOS' }] }, 150),
      state: (dev) => wait(dev === 'sdb' ? { state: 'desconhecido', why: 'o adaptador deste disco travou com pedidos especiais, então o BrunOS não pergunta' } : { state: dev === 'sda' ? 'dormindo' : 'acordado', why: '' }, 500),
      errors: (dev) => wait({ lines: dev === 'sda' ? [
        { at: '2026-11-04T09:12:40-0300', text: 'sd 6:0:0:0: [sda] tag#7 FAILED Result: hostbyte=DID_OK driverbyte=DRIVER_OK cmd_age=3s' },
        { at: '2026-11-04T09:12:40-0300', text: 'critical medium error, dev sda, sector 1213472 op 0x0:(READ) flags 0x80700 phys_seg 1 prio class 2' },
        { at: '2026-11-04T09:12:38-0300', text: 'usb 2-1: reset high-speed USB device number 4 using xhci_hcd' }] : [] }, 900),
      speed: (dev) => { const k = demoDiskList().find((d) => d.dev === dev)?.key; demoSpeedBusy = dev; setTimeout(() => { demoSpeedBusy = ''; demoSpeeds[k] = { mbps: dev === 'sdc' ? 512.3 : 96.8, bytes: 1073741824, secs: 10.2, at: Math.floor(Date.now() / 1000) }; }, 5000); return wait({ disks: demoDiskList() }, 300); },
      eject: (dev) => { const d = demoDiskList().find((x) => x.dev === dev); if (!d || d.tran !== 'usb' || d.parts.some((p) => p.mounts.length)) return Promise.reject(new Error('o disco ainda está montado: desmonte antes de ejetar')); demoEjected.add(d.key); return wait({ ok: true }, 900); },
    },
    power: {
      get: () => wait({ config: powerCfg, status: powerStatus(), outages, drainW: 9.4, keyReady: !!powerCfg.testedAt, suggestedHost: '192.168.1.50', needsUpdate: !!powerCfg.testedAt && powerCfg.hostSetup < 2, charge: demoCharge() }),
      charge: (b) => {
        if (b.detect) return wait({ charge: demoCharge() }, 300);
        if (!demoCharge().ready) return Promise.reject(Object.assign(new Error('configure (ou atualize) a conexão com o servidor nesta janela e clique em Testar'), { status: 400, data: { charge: demoCharge() } }));
        if (chargeState.kind === 'range') { chargeState.current = b.limit; chargeState.want = b.limit >= 100 ? 0 : b.limit; }
        else if (chargeState.kind === 'toggle') { chargeState.on = b.protect === 'on'; chargeState.protect = b.protect; }
        return wait({ charge: demoCharge() }, 700);
      },
      save: (c) => {
        if (c.enabled && !c.simulate && !powerCfg.testedAt) return Promise.reject(new Error('teste a conexão com o servidor antes de desligar o modo simulação'));
        powerCfg = { ...powerCfg, ...c, testedAt: powerCfg.testedAt };
        return wait({ config: powerCfg, status: powerStatus() });
      },
      cancel: () => wait({ status: { ...powerStatus(), phase: 'cancelled' } }),
      setup: () => wait({ publicKey: demoKey, fingerprint: 'SHA256:demo', user: 'brunos-power', command: shutCmd, suggestedHost: '192.168.1.50',
        authorizedLine: `restrict,command="${shutCmd}" ${demoKey}`,
        commands: ['sudo useradd --system --create-home --shell /bin/sh brunos-power', "sudo usermod -p '*' brunos-power", 'sudo install -d -m 700 -o brunos-power -g brunos-power /home/brunos-power/.ssh', ...keyCmds].join('\n'),
        updateCommands: keyCmds.join('\n') }, 200),
      test: (b) => b.trust
        ? (() => { powerCfg = { ...powerCfg, host: b.host, port: b.port, user: b.user, testedAt: now, hostSetup: powerCfg.testedAt ? powerCfg.hostSetup : 2 }; return wait({ ok: true, config: powerCfg }, 600); })()
        : Promise.reject(Object.assign(new Error('servidor ainda não confirmado'), { status: 409, data: { unknownHost: true, addr: `${b.host}:${b.port || 22}`, keyType: 'ssh-ed25519', fingerprint: 'SHA256:q3J8Zf0bVx1nRk2y7Tn5mWcL0aHf9uE4sPd6GiYoXbM' } })),
    },
    streamStats(onInit, onPoint, onNotify, onMetrics) {
      onInit({ snapshot: snap(), history: [...history], metrics: demoViews() });
      const t = setInterval(() => onPoint(tick()), 2000);
      // o AdGuard "responde" a cada 15 s na demonstração
      const m = setInterval(() => { const ag = demoViews().find((v) => v.id === 'ag1'); if (ag) onMetrics?.(ag); }, 15000);
      return () => { clearInterval(t); clearInterval(m); };
    },
    integrationImage: (id, ref) => `assets/thumbs/${encodeURIComponent(ref)}.avif`,
    metricSeries: (id, range) => {
      const step = range === '7d' ? 3600 : 300, n = range === '7d' ? 168 : range === '1h' ? 12 : 288;
      const t0 = Math.floor(Date.now() / 1000 / step) * step - (n - 1) * step;
      return wait({ range, series: { step, start: t0, points: Array.from({ length: n }, (_, i) => +(1 + Math.sin(i / 9) * 0.4 + Math.random() * 0.4).toFixed(2)) } }, 200);
    },
    integrations: {
      kinds: () => wait(demoKinds, 30),
      list: () => wait({ integrations: demoInteg, views: demoViews() }, 60),
      suggest: () => wait([{ kind: 'adguard', name: 'AdGuard Home', url: 'http://192.168.1.50:8104', app: 'adguard' }, { kind: 'speedtest', name: 'Speedtest Tracker', url: 'http://192.168.1.50:8765', app: 'speedtest-tracker' }], 80),
      add: (b) => {
        const k = demoKinds.find((x) => x.id === b.kind);
        if (!k) return Promise.reject(new Error('tipo de integração desconhecido'));
        if (!k.noUrl && !/^https?:\/\/[^/@]+/.test(b.url || '')) return Promise.reject(new Error('endereço inválido: use http:// ou https:// seguido do IP ou nome e da porta'));
        const it = { id: Math.random().toString(16).slice(2, 14), kind: b.kind, name: b.name || k.name, url: k.noUrl ? '' : b.url.replace(/\/+$/, ''), user: b.user || '', hasSecret: !!b.secret, interval: b.interval || k.interval, alerts: b.alerts || {}, options: b.options || {} };
        demoInteg.push(it);
        return wait(it, 200);
      },
      update: (id, b) => {
        const it = demoInteg.find((x) => x.id === id);
        if (!it) return Promise.reject(new Error('integração não encontrada'));
        Object.assign(it, { name: b.name || it.name, url: (b.url || it.url || '').replace(/\/+$/, ''), user: b.user ?? it.user, interval: b.interval || it.interval, alerts: b.alerts || it.alerts, options: b.options || it.options || {}, hasSecret: b.secret != null ? !!b.secret : it.hasSecret });
        return wait(it, 200);
      },
      del: (id) => { demoInteg = demoInteg.filter((x) => x.id !== id); return wait({ ok: true }, 120); },
      test: (b) => (/^https?:\/\//.test(b.url || '') || demoKinds.find((k) => k.id === (b.kind || demoInteg.find((x) => x.id === b.id)?.kind))?.noUrl ? wait({ ok: true, values: demoValues({ kind: b.kind || demoInteg.find((x) => x.id === b.id)?.kind }) }, 700)
        : Promise.reject(new Error('endereço inválido: use http:// ou https:// seguido do IP ou nome e da porta'))),
    },
    statsHistory: (range) => {
      const { step, n } = DEMO_P[range] || DEMO_P['24h'], long = step > 60;
      const t0 = Math.floor(Date.now() / 1000 / step) * step;
      return wait({ range, step, points: Array.from({ length: n }, (_, i) => {
        const t = t0 - (n - 1 - i) * step, hr = new Date(t * 1000).getHours(), evening = hr >= 19 && hr <= 23;
        const p = { t, cpu: +(6 + (evening ? 14 : 3) * Math.random() + Math.sin(i / 20) * 2).toFixed(1), mem: +(33 + Math.sin(i / 90) * 4 + (evening ? 3 : 0)).toFixed(1),
          temp: +(48 + (evening ? 7 : 1) + Math.random() * 2).toFixed(1), rx: Math.round((evening ? 900e3 : 60e3) * (0.5 + Math.random())), tx: Math.round((evening ? 200e3 : 20e3) * (0.5 + Math.random())) };
        if (long) Object.assign(p, { cpuMax: +(p.cpu * (1.5 + Math.random() * 2)).toFixed(1), memMax: +(p.mem + 2).toFixed(1), tempMax: +(p.temp + 3).toFixed(1), rxMax: p.rx * 3, txMax: p.tx * 3 });
        return p;
      }) }, 250);
    },
    // Históricos (0.14.0): séries sintéticas com o formato da API
    history: (series, periodo) => {
      const { step, n } = DEMO_P[periodo] || DEMO_P['24h'], t0 = Math.floor(Date.now() / 1000 / step) * step, long = step > 60;
      const gen = (name, i, t) => {
        const hr = new Date(t * 1000).getHours(), ev = hr >= 19 && hr <= 23, r = Math.random();
        if (name === 'net.lat') return i % 211 === 5 ? null : +(11 + r * 6 + (i % 97 === 0 ? 40 : 0)).toFixed(1);
        if (name === 'net.up') return i % 211 === 5 ? 0 : 1;
        if (name.endsWith('.busy')) return +(ev ? 4 + r * 10 : r * 2).toFixed(1);
        if (name.endsWith('.temp')) return i % 20 ? null : +(34 + Math.sin(i / 400) * 3 + (ev ? 4 : 0) + r).toFixed(1); // SMART: um ponto a cada 20 min
        if (name.endsWith('.rd')) return Math.round((ev ? 8e6 : 2e5) * r);
        if (name.endsWith('.wr')) return Math.round((ev ? 3e6 : 1e5) * r);
        if (name === 'sys.fan') return Math.round(2100 + (ev ? 600 : 0) + r * 200);
        if (name === 'sys.bat') return 100;
        return +(5 + r * 10).toFixed(1);
      };
      const out = {};
      for (const name of series) {
        out[name] = [];
        for (let i = 0; i < n; i++) {
          const t = t0 - (n - 1 - i) * step, v = gen(name, i, t);
          if (v != null) out[name].push(long ? [t, v, +(v * 1.6).toFixed(1)] : [t, v]);
        }
      }
      return wait({ periodo, step, series: out }, 200);
    },
    historyNames: (prefixo) => wait({ series: ['disk.sda.rd', 'disk.sda.wr', 'disk.sda.busy', 'disk.sdb.rd', 'disk.sdb.wr', 'disk.sdb.busy'].filter((s) => s.startsWith(prefixo)) }),
    internet: () => {
      const t0 = Math.floor(Date.now() / 60000) * 60;
      const points = Array.from({ length: 1440 }, (_, i) => ({ t: t0 - (1439 - i) * 60, ms: i > 610 && i < 618 ? -1 : +(11 + Math.random() * 6 + (i % 97 === 0 ? 40 : 0)).toFixed(1) }));
      return wait({ up: true, checked: true, ms: 12.4, avg: 14.1, uptime: 99.44, enabled: true, points,
        outages: [{ start: t0 - 829 * 60, end: t0 - 822 * 60 }, { start: t0 - 3 * 86400, end: t0 - 3 * 86400 + 2400 }] }, 200);
    },
    processes: () => wait({ summary: { total: 214, threads: 1180, running: 2, cores: 2, memKB: 7.7e6, load: 0.42 }, procs: [
      { pid: 1820, name: 'immich', cmd: 'node /usr/src/app/dist/main', user: 'root', cpu: 6.4, mem: 512e6, memPct: 6.5, threads: 11, app: 'Immich' },
      { pid: 2210, name: 'Plex Media Serv', cmd: '/usr/lib/plexmediaserver/Plex Media Server', user: 'usuario', cpu: 3.1, mem: 260e6, memPct: 3.3, threads: 23, app: 'Plex' },
      { pid: 2981, name: 'postgres', cmd: 'postgres: immich immich 172.18.0.5(45234) idle', user: 'uid 999', cpu: 1.2, mem: 140e6, memPct: 1.8, threads: 1, app: 'Immich' },
      { pid: 1, name: 'systemd', cmd: '/sbin/init', user: 'root', cpu: 0.3, mem: 12e6, memPct: 0.2, threads: 1 },
      { pid: 911, name: 'dockerd', cmd: '/usr/bin/dockerd -H fd://', user: 'root', cpu: 0.9, mem: 95e6, memPct: 1.2, threads: 18 },
      { pid: 3012, name: 'brunos', cmd: '/brunos', user: 'usuario', cpu: 0.2, mem: 11e6, memPct: 0.1, threads: 7, app: 'BrunOS' },
      { pid: 1204, name: 'sshd', cmd: 'sshd: /usr/sbin/sshd -D [listener]', user: 'root', cpu: 0, mem: 8e6, memPct: 0.1, threads: 1 },
    ].map((p) => ({ ppid: 1, state: 'S', ...p })) }, 300),
    system: () => wait({ info: { hostname: 'server', os: 'Ubuntu 24.04 LTS', kernel: '6.8.0', cpuModel: 'Intel(R) Core(TM) i5 (demonstração)', cores: 2, arch: 'amd64' }, runtipi: { found: true, localDomain: 'server.lan', internalIp: '192.168.1.50' }, docker: true, dockerActions: false, version: 'demo', startedAt: now - 7200, maxUploadGB: 50 }),
    apps: () => wait({ apps: apps.map((a) => ({ ...a, update: { 'plex:migrated': { urn: 'plex:migrated', id: 'plex', store: 'migrated', name: 'Plex', installed: '1.41.2', available: '1.42.1' }, 'immich:migrated': { urn: 'immich:migrated', id: 'immich', store: 'migrated', name: 'Immich', installed: 'v2.1.0', available: 'v2.2.0' } }[a.urn] })), runtipi: { found: true, localDomain: 'server.lan', internalIp: '192.168.1.50' }, docker: true, dockerActions: false }),
    usage: () => wait([{ urn: '', name: 'Runtipi (sistema)', project: 'runtipi', cpu: 1.2 + Math.random(), memory: 420e6 }, ...apps.filter((a) => a.state === 'running').map((a) => ({ urn: a.urn, name: a.name, project: a.project, cpu: Math.random() * 3, memory: 20e6 + Math.random() * 600e6 }))].sort((a, b) => b.cpu - a.cpu), 200),
    restart: () => Promise.reject(new Error('Indisponível na demonstração')),
    app: (() => {
      const ts = (ago) => new Date(Date.now() - ago * 1000).toISOString();
      const demoLines = (name) => [
        `${ts(3600)} [info] ${name} iniciando…`, `${ts(3590)} [info] escutando na porta 8080`, `${ts(1200)} [warn] uma requisição demorou 2,3 s`,
        `${ts(300)} [info] tarefa agendada concluída`, `${ts(60)} [info] GET /api/status 200 4ms`, `${ts(5)} [info] GET /api/status 200 3ms`,
      ];
      return {
        start: () => Promise.reject(new Error('Indisponível na demonstração')),
        stop: () => Promise.reject(new Error('Indisponível na demonstração')),
        containers: (urn) => { const a = apps.find((x) => x.urn === urn); const id = a?.id || 'app'; return wait(urn === 'immich:migrated'
          ? [{ id: 'c1', name: 'immich-server', service: 'immich-server', state: 'running', status: 'Up 2 days', image: 'ghcr.io/immich-app/immich-server:v2' }, { id: 'c2', name: 'immich-db', service: 'immich-db', state: 'running', status: 'Up 2 days (healthy)', image: 'tensorchord/pgvecto-rs:pg16' }, { id: 'c3', name: 'immich-redis', service: 'immich-redis', state: 'running', status: 'Up 2 days', image: 'redis:7' }]
          : [{ id: 'c1', name: `${id}_migrated-${id}-1`, service: id, state: a?.state === 'stopped' ? 'exited' : 'running', status: a?.state === 'stopped' ? 'Exited (0) 3 hours ago' : 'Up 2 days', image: `${id}:latest` }]); },
        history: (urn, range = '24h') => {
          const { step, n } = DEMO_P[range] || DEMO_P['24h'], long = step > 60;
          const t0 = Math.floor(Date.now() / 1000 / step) * step, seed = urn.length, noUp = urn === 'homarr:migrated';
          const points = Array.from({ length: n }, (_, i) => {
            const t = t0 - (n - 1 - i) * step, hour = new Date(t * 1000).getHours();
            const busy = hour >= 19 && hour <= 23 ? 3 : 1;
            const p = { t, cpu: Math.max(0, +(busy * (0.6 + Math.sin(i / 37 + seed) * 0.4 + Math.random() * 0.5)).toFixed(2)), mem: +(180 + seed * 9 + Math.sin(i / 200) * 25).toFixed(1) };
            if (!noUp) p.up = (i > n * 0.2 && i < n * 0.2 + 9 && urn === 'plex:migrated') ? 0 : 1;
            if (long) { p.cpuMax = +(p.cpu * 2.2).toFixed(2); p.memMax = +(p.mem + 12).toFixed(1); }
            return p;
          });
          const counted = points.filter((p) => p.up != null);
          return wait({ points, step, range, health: noUp ? 'unknown' : 'up', ...(counted.length ? { uptime: +(counted.reduce((s, p) => s + p.up, 0) / counted.length * 100).toFixed(2) } : {}) }, 250);
        },
        logs: (urn) => wait(demoLines(urn.split(':')[0]).join('\n') + '\n', 200),
        follow(urn, id, onLine) { const t = setInterval(() => onLine(`${new Date().toISOString()} [info] GET /api/status 200 ${2 + Math.floor(Math.random() * 6)}ms`), 2500); return () => clearInterval(t); },
      };
    })(),
    term: (() => {
      // várias Abas: cada sessão guarda a tela (para reabrir depois de minimizar) e a linha digitada
      const enc = new TextEncoder(), tabs = new Map();
      let trusted = false, keyInfo = { exists: false, unlocked: false }, n = 0;
      const prompt = '\x1b[1;32musuario@servidor\x1b[0m:\x1b[1;34m~\x1b[0m$ ';
      const write = (t, s) => { t.hist += s; t.out?.(enc.encode(s)); };
      const fp = 'SHA256:q3J8Zf0bVx1nRk2y7Tn5mWcL0aHf9uE4sPd6GiYoXbM';
      return {
        open: (b) => {
          if (b.useKey && !keyInfo.exists) return Promise.reject(Object.assign(new Error('a Chave do Terminal ainda não foi criada'), { status: 400, data: { noKey: true } }));
          if (b.useKey && !keyInfo.unlocked) return Promise.reject(Object.assign(new Error('digite a senha do BrunOS para usar a Chave do Terminal'), { status: 403, data: { keyLocked: true } }));
          if (!b.trust && !trusted) return Promise.reject(Object.assign(new Error('servidor ainda não confirmado'), { status: 409, data: { unknownHost: true, addr: `${b.host}:${b.port || 22}`, keyType: 'ssh-ed25519', fingerprint: fp } }));
          if (tabs.size >= 4) return Promise.reject(Object.assign(new Error('muitas sessões abertas; feche alguma antes'), { status: 429 }));
          trusted = true;
          const id = `demo${++n}`;
          tabs.set(id, { id, addr: `${b.host}:${b.port || 22}`, user: b.user, created: Date.now(), hist: '', line: '', out: null, running: '' });
          return wait({ id, addr: `${b.host}:${b.port || 22}`, user: b.user }, 400);
        },
        list: () => wait([...tabs.values()].map((t) => ({ id: t.id, addr: t.addr, user: t.user, created: t.created, running: t.running ? [t.running] : undefined }))),
        busy: (id) => wait({ known: true, names: tabs.get(id)?.running ? [tabs.get(id).running] : [] }, 150),
        key: {
          get: () => wait(keyInfo.exists ? { ...keyInfo, publicKey: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDemoChaveDoTerminalQueNaoServeParaNada brunos-terminal', fingerprint: 'SHA256:demoTerminalKey', createdAt: new Date().toISOString() } : keyInfo),
          create: (pw) => { if (!pw) return Promise.reject(Object.assign(new Error('senha do BrunOS incorreta'), { status: 403 })); keyInfo = { exists: true, unlocked: true }; return api.term.key.get(); },
          unlock: (pw) => { if (!pw) return Promise.reject(Object.assign(new Error('senha do BrunOS incorreta'), { status: 403 })); keyInfo.unlocked = true; return api.term.key.get(); },
          del: () => { keyInfo = { exists: false, unlocked: false }; return wait({ ok: true }); },
        },
        input: (id, d) => {
          const t = tabs.get(id); if (!t) return Promise.reject(Object.assign(new Error('sessão de terminal não encontrada'), { status: 404 }));
          for (const ch of d) {
            if (ch === '\r') {
              const cmd = t.line.trim(); t.line = '';
              if (/^(sleep|apt|tail)\b/.test(cmd)) { t.running = cmd.split(' ')[0]; write(t, '\r\n'); setTimeout(() => { if (t.running) { t.running = ''; write(t, prompt); } }, 20000); continue; }
              write(t, '\r\n' + (cmd === 'uptime' ? ' 17:42:10 up 2 days, 16:03,  1 user,  load average: 0,31, 0,28, 0,25\r\n' : cmd === 'docker ps' ? 'CONTAINER ID   IMAGE                         STATUS       NAMES\r\n3f2a1b9c8d7e   ghcr.io/brunnofe/brunos:0.10.0   Up 2 hours   brunos-1\r\n' : cmd ? `demonstração: "${cmd}" não roda aqui\r\n` : '') + prompt);
            } else if (ch === '\x03') { if (t.running) { t.running = ''; write(t, '^C\r\n' + prompt); } }
            else if (ch === '\x7f') { if (t.line) { t.line = t.line.slice(0, -1); write(t, '\b \b'); } }
            else if (!t.running) { t.line += ch; write(t, ch); }
          }
          return wait(null, 5);
        },
        resize: () => wait(null, 5),
        close: (id) => { tabs.delete(id); return wait(null, 5); },
        forget: () => { trusted = false; return wait(null, 5); },
        stream(id, h) {
          const t = tabs.get(id); if (!t) { setTimeout(() => h.onExit('sessão encerrada'), 10); return () => {}; }
          if (h.replay && t.hist) setTimeout(() => h.onData(enc.encode(t.hist)), 50);
          t.out = h.onData;
          if (!t.hist) setTimeout(() => write(t, 'Welcome to Ubuntu 24.04.1 LTS (GNU/Linux 6.17.0-8-generic x86_64)\r\n\r\n * Modo demonstração: nada aqui roda de verdade.\r\n\r\nLast login: Sat Nov  7 17:40:02 2026 from 192.168.100.20\r\n' + prompt), 300);
          return () => { if (t.out === h.onData) t.out = null; };
        },
      };
    })(),
    logoUrl: () => '',
    files: {
      roots: () => wait(demoRoots()),
      disks: () => wait(rootsMeta.filter(([id]) => id !== 'home').map(([id, name, t, u]) => ({ id, name, mounted: t > 0, mapped: demoMapped.includes(name), total: t * GB, free: (t - u) * GB }))),
      setDisks: (m) => { demoMapped = m; rootsMeta.forEach(([id]) => { tree[id] ||= D('', []); }); return wait(demoRoots()); },
      list: async (root, path) => wait({ root, path, entries: nodeAt(root, path).children.map(entryOf).sort((a, b) => (b.dir - a.dir) || a.name.localeCompare(b.name, 'pt-BR')) }),
      search: async (root, path, query) => {
        const hits = []; const qq = query.toLowerCase();
        const walkT = (n, p) => n.children?.forEach((c) => { const cp = p ? `${p}/${c.name}` : c.name; if (c.name.toLowerCase().includes(qq)) hits.push({ ...entryOf(c), path: cp }); if (c.dir) walkT(c, cp); });
        walkT(nodeAt(root, path), path || '');
        return wait({ hits, truncated: false });
      },
      mkdir: async (root, path, name) => { const n = nodeAt(root, path); if (n.children.some((c) => c.name === name)) throw new Error('já existe um item com esse nome'); n.children.push(D(name, [], 0)); return wait({ path: `${path}/${name}` }); },
      rename: async (root, path, name) => { const [dir, base] = split(path); const c = nodeAt(root, dir).children.find((x) => x.name === base); c.name = name; c.mtime = now; return wait({ ok: true }); },
      paste: async (root, paths, destRoot, destPath, copy) => {
        const dest = nodeAt(destRoot, destPath);
        for (const p of paths) {
          const [dir, base] = split(p); const parent = nodeAt(root, dir); const i = parent.children.findIndex((x) => x.name === base);
          const item = copy ? structuredClone(parent.children[i]) : parent.children.splice(i, 1)[0];
          dest.children.push(item);
        }
        return wait({ ok: true });
      },
      trash: async (root, paths) => {
        for (const p of paths) {
          const [dir, base] = split(p); const parent = nodeAt(root, dir); const i = parent.children.findIndex((x) => x.name === base);
          const [item] = parent.children.splice(i, 1);
          trashItems.unshift({ root, id: Math.random().toString(16).slice(2, 18).padEnd(16, '0'), name: item.name, path: p, deletedAt: new Date().toISOString(), dir: !!item.dir, size: item.size || 0, _item: item });
        }
        return wait({ ok: true });
      },
      del: async (root, paths) => { for (const p of paths) { const [dir, base] = split(p); const parent = nodeAt(root, dir); parent.children = parent.children.filter((x) => x.name !== base); } return wait({ ok: true }); },
      trashList: () => wait(trashItems.map(({ _item, ...t }) => t)),
      restore: async (root, id) => { const i = trashItems.findIndex((t) => t.id === id); const [t] = trashItems.splice(i, 1); let parent; try { parent = nodeAt(root, split(t.path)[0]); } catch { parent = tree[root]; } parent.children.push(t._item); return wait({ path: t.path }); },
      trashDelete: async (root, id) => { const i = trashItems.findIndex((t) => t.id === id); trashItems.splice(i, 1); return wait({ ok: true }); },
      trashEmpty: async () => { trashItems.length = 0; return wait({ ok: true }); },
      text: async (root, path) => {
        const n = nodeAt(root, path);
        n.text ??= path.endsWith('.py') ? '# análise dos dados do ERP (demonstração)\nimport numpy as np\n\ndef media(xs):\n    """Média simples."""\n    return sum(xs) / len(xs)\n\nprint(media([1, 2, 3]))\n'
          : '# Notas\n\n- [x] Testar o **BrunOS** no servidor\n- [ ] Ligar o `drivetemp`\n\n```yaml\nservices:\n  brunos:\n    image: ghcr.io/brunnofe/brunos:0.6.0\n```\n';
        return wait({ text: n.text, mtime: n.mtime * 1000, size: n.text.length, readOnly: false });
      },
      saveText: async (root, path, text) => { const n = nodeAt(root, path); n.text = text; n.size = text.length; n.mtime = Math.floor(Date.now() / 1000); return wait({ mtime: n.mtime * 1000 }, 200); },
      newFile: async (root, path, name) => { const n = nodeAt(root, path); if (n.children.some((c) => c.name === name)) throw new Error('já existe um item com esse nome'); n.children.push({ ...F(name, 0, 0), text: '' }); return wait({ path: `${path}/${name}` }); },
      extract: () => Promise.reject(new Error('Indisponível na demonstração')),
      duplicates: () => wait({ groups: [
        { size: 7.4e9, files: [{ path: 'Filmes/Filme (2003).mkv', mtime: now - 30 * day }, { path: 'Séries/Filme (2003) cópia.mkv', mtime: now - 2 * day }] },
        { size: 2.1e6, files: [{ path: 'Fotos/wallpaper-lago.jpg', mtime: now - 60 * day }, { path: 'Downloads/wallpaper-lago (1).jpg', mtime: now - 5 * day }, { path: 'Documentos/lago.jpg', mtime: now - 1 * day }] },
      ], wasted: 7.4e9 + 4.2e6, scanned: 1834, truncated: false }, 900),
      usage: async (root, path) => {
        const sizeOf = (n) => n.dir ? n.children.reduce((s, c) => s + sizeOf(c), 0) : n.size;
        const countOf = (n) => n.dir ? n.children.reduce((s, c) => s + countOf(c), 0) : 1;
        const n = nodeAt(root, path);
        const items = n.children.map((c) => ({ name: c.name, dir: !!c.dir, size: sizeOf(c), ...(c.dir ? { count: countOf(c) } : {}) })).sort((a, b) => b.size - a.size);
        return wait({ path, size: sizeOf(n), count: countOf(n), items, truncated: false, scannedAt: now }, 400);
      },
      rawUrl: () => '',
      zipUrl: () => '',
      upload(root, path, name, file, onProgress) {
        let p = 0, t;
        const promise = new Promise((resolve) => {
          t = setInterval(() => {
            p = Math.min(1, p + 0.08 + Math.random() * 0.1); onProgress?.(p);
            if (p >= 1) { clearInterval(t); const n = nodeAt(root, path); n.children.push(F(name.split('/').pop(), file.size, 0)); resolve({ path: `${path}/${name}` }); }
          }, 120);
        });
        return { promise, abort: () => clearInterval(t) };
      },
    },
  };
}

export let api = real;

// Detecta se há backend; sem ele (artefato/arquivo local) usa a demonstração.
export async function initApi() {
  try {
    const res = await fetch('/api/public', { headers: H, credentials: 'same-origin' });
    if (res.ok && (res.headers.get('content-type') || '').includes('json')) {
      const pub = await res.json();
      api = real;
      return pub;
    }
  } catch { /* sem servidor */ }
  api = makeDemo();
  return api.public();
}
