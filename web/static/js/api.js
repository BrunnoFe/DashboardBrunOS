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
  if (raw) return res;
  let data = null;
  const ct = res.headers.get('content-type') || '';
  if (ct.includes('json')) data = await res.json();
  if (!res.ok) throw new Error(data?.error || `Erro ${res.status}`);
  return data;
}
const q = (o) => new URLSearchParams(o).toString();

const real = {
  demo: false,
  public: () => call('GET', '/api/public'),
  setup: (b) => call('POST', '/api/auth/setup', b),
  login: (b) => call('POST', '/api/auth/login', b),
  logout: () => call('POST', '/api/auth/logout', {}),
  me: () => call('GET', '/api/auth/me'),
  updateMe: (b) => call('PUT', '/api/auth/me', b),
  password: (b) => call('POST', '/api/auth/password', b),
  settings: () => call('GET', '/api/settings'),
  saveSettings: (s) => call('PUT', '/api/settings', s),
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
  streamStats(onInit, onPoint) {
    let es, stopped = false, retry;
    const open = () => {
      es = new EventSource('/api/stats/stream');
      es.addEventListener('init', (e) => onInit(JSON.parse(e.data)));
      es.onmessage = (e) => onPoint(JSON.parse(e.data));
      es.onerror = () => { es.close(); if (!stopped) retry = setTimeout(open, 4000); };
    };
    open();
    return () => { stopped = true; clearTimeout(retry); es?.close(); };
  },
  system: () => call('GET', '/api/system'),
  apps: () => call('GET', '/api/apps'),
  usage: () => call('GET', '/api/usage'),
  restart: (urn) => call('POST', '/api/apps/restart', { urn }),
  logoUrl: (urn) => `/api/apps/logo?${q({ urn })}`,
  files: {
    roots: () => call('GET', '/api/files/roots'),
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
    upload(root, path, name, file, onProgress) {
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
function makeDemo() {
  const LS = 'brunos.demo.settings';
  const load = () => { try { return JSON.parse(localStorage.getItem(LS) || '{}'); } catch { return {}; } };
  let settings = load();
  let logged = false;
  const GB = 1e9;
  const disks = [
    { name: 'Sistema', device: '/dev/sdc2', total: 312 * GB, used: 33 * GB, system: true, rotational: false, model: 'SSD' },
    { name: '1TB_HDD', device: '/dev/sdd', total: 984 * GB, used: 463 * GB, rotational: true, model: 'HDD' },
    { name: '1TB_HDD2', device: '/dev/sda', total: 984 * GB, used: 246 * GB, rotational: true, model: 'HDD' },
    { name: '1TB_HDD3', device: '/dev/sdb', total: 984 * GB, used: 462 * GB, rotational: true, model: 'HDD' },
  ].map((d) => ({ ...d, free: d.total - d.used }));
  const s = { cpu: 12, mem: 2.6 * GB, temp: 51, rx: 40e3, tx: 300e3 };
  const walk = (v, step, lo, hi) => Math.min(hi, Math.max(lo, v + (Math.random() - 0.5) * step));
  const history = [];
  const snap = () => ({
    t: Math.floor(Date.now() / 1000),
    cpu: { percent: Math.round(s.cpu * 10) / 10, cores: 2, model: 'Intel(R) Core(TM) i5 (demonstração)', load: [0.4, 0.3, 0.2] },
    mem: { total: 7.7 * GB, used: s.mem, available: 7.7 * GB - s.mem, swapTotal: 2 * GB, swapUsed: 0.1 * GB },
    temp: Math.round(s.temp * 10) / 10,
    sensors: [{ chip: 'coretemp', label: 'Package id 0', value: s.temp }, { chip: 'dell_smm', label: 'Ambient', value: 47 }],
    fanRpm: 2321, battery: { percent: 100, status: 'Full' }, disks,
    net: { rx: s.rx, tx: s.tx, ifaces: ['enp2s0'] }, uptime: 2 * 86400 + 4 * 3600,
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
    home: D('', [D('ptbruno', [
      D('Documentos', [D('Mestrado', [F('Dissertação_v12.docx', 3.1e6, 4), F('analise_ERP.py', 18e3, 2), F('figuras_P300.png', 840e3, 2)]), F('Laudo_modelo.docx', 210e3, 30), F('Planilha_correcao.xlsx', 96e3, 3), F('notas.md', 4e3, 0)]),
      D('Downloads', [F('ubuntu-24.04-live-server-amd64.iso', 2.6e9, 21), F('Neuropsychologia_guide.pdf', 1.4e6, 7)]),
      D('Fotos', [F('IMG_2041.heic', 3.2e6, 7), F('wallpaper-lago.jpg', 2.1e6, 60)]),
      D('runtipi', [D('apps', []), D('app-data', [])], 40),
    ])]),
    '1tb-hdd': D('', [D('Filmes', [F('Filme (2003).mkv', 7.4e9, 30), F('Outro filme.mkv', 2.2e9, 31)]), D('Séries', [])]),
    '1tb-hdd2': D('', [D('Backups', [F('backup-2026-10.tar.gz', 21e9, 9)]), D('Músicas', [F('faixa.flac', 34e6, 100)])]),
    '1tb-hdd3': D('', [D('Immich', []), D('Nextcloud', [])]),
  };
  const rootsMeta = [['home', 'Home', 312, 33], ['1tb-hdd', '1TB_HDD', 984, 463], ['1tb-hdd2', '1TB_HDD2', 984, 246], ['1tb-hdd3', '1TB_HDD3', 984, 462]];
  const trashItems = [];
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
    settings: () => wait(settings, 30),
    saveSettings: (v) => { settings = v; try { localStorage.setItem(LS, JSON.stringify(v)); } catch { /* sem storage */ } return wait({ ok: true }, 10); },
    wallpapers: () => wait([]),
    wallpaperUrl: () => '',
    publicWallpaperUrl: () => '',
    uploadWallpaper: () => Promise.reject(new Error('No modo demonstração a imagem fica só nesta aba')),
    deleteWallpaper: () => wait({ ok: true }),
    stats: () => wait({ snapshot: snap(), history }),
    streamStats(onInit, onPoint) {
      onInit({ snapshot: snap(), history: [...history] });
      const t = setInterval(() => onPoint(tick()), 2000);
      return () => clearInterval(t);
    },
    system: () => wait({ info: { hostname: 'server', os: 'Ubuntu 24.04 LTS', kernel: '6.8.0', cpuModel: 'Intel(R) Core(TM) i5 (demonstração)', cores: 2, arch: 'amd64' }, runtipi: { found: true, localDomain: 'server.lan', internalIp: '192.168.1.50' }, docker: true, dockerActions: false, version: 'demo', startedAt: now - 7200, maxUploadGB: 50 }),
    apps: () => wait({ apps, runtipi: { found: true, localDomain: 'server.lan', internalIp: '192.168.1.50' }, docker: true, dockerActions: false }),
    usage: () => wait([{ urn: '', name: 'Runtipi (sistema)', project: 'runtipi', cpu: 1.2 + Math.random(), memory: 420e6 }, ...apps.filter((a) => a.state === 'running').map((a) => ({ urn: a.urn, name: a.name, project: a.project, cpu: Math.random() * 3, memory: 20e6 + Math.random() * 600e6 }))].sort((a, b) => b.cpu - a.cpu), 200),
    restart: () => Promise.reject(new Error('Indisponível na demonstração')),
    logoUrl: () => '',
    files: {
      roots: () => wait(rootsMeta.map(([id, name, t, u]) => ({ id, name, readOnly: false, total: t * GB, free: (t - u) * GB }))),
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
