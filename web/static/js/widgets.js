// Widgets personalizáveis (etapa 7): Formatos, Tamanhos, Métricas da fonte "BrunOS" e Modelos.
// Termos em CONTEXT.md. Este módulo só monta HTML a partir de um item e de um contexto (valores,
// definições, séries), então serve igual para a tela inicial e para a prévia do editor.
import { esc, fmt, fmtBytes, fmtRate, fmtDuration, timeAgo, svg, folderSVG, clamp } from './ui.js';
import { lineSVG, barSVG, donutSVG, sparkSVG, stats } from './charts.js';
import { batteryGlyph, stateText } from './battery.js';

/* ── Tamanhos (em quadros) e Formatos ──────────────────────────── */
export const SIZES = {
  s: { label: 'Pequeno', c: 1, r: 1 },
  w: { label: 'Largo', c: 2, r: 1 },
  t: { label: 'Alto', c: 1, r: 2 },
  l: { label: 'Grande', c: 2, r: 2 },
  f: { label: 'Faixa', c: 0, r: 1 }, // c = 0: largura toda
};
export const FORMATS = {
  stat: { label: 'Destaque', icon: 'star', desc: 'Um número grande, uma linha de apoio e até 3 valores pequenos', max: 5, kinds: ['num', 'text'] },
  grid: { label: 'Grade', icon: 'grid', desc: 'De 2 a 6 valores com ícone', max: 6, kinds: ['num', 'text'] },
  list: { label: 'Lista', icon: 'list', desc: 'Uma lista do app (ex.: domínios) ou valores um embaixo do outro', max: 6, kinds: ['list', 'num', 'text'] },
  chart: { label: 'Gráfico', icon: 'activity', desc: 'Linha, barras ou rosca, com até 3 séries', max: 3, kinds: ['series', 'num'] },
  media: { label: 'Mídia', icon: 'image', desc: 'Foto ou capa de um app (Immich, Plex), com legenda', max: 1, kinds: ['media'] },
  shortcuts: { label: 'Atalhos', icon: 'folder', desc: 'Atalhos para pastas do Arquivos, em grade ou lista', max: 0, kinds: [] },
};
export const ROTATE = [[5, '5 min'], [15, '15 min'], [30, '30 min'], [60, '1 hora'], [360, '6 horas'], [1440, '1 por dia']];
export const CHART_TYPES = { line: 'Linha', bar: 'Barras', donut: 'Rosca' };
export const RANGES = { live: '5 min', '1h': '1 hora', '24h': '24 horas', '7d': '7 dias' };
export const WINDOWS = { live: 'Uso ao vivo', battery: 'Bateria', internet: 'Internet', power: 'Energia', files: 'Arquivos', settings: 'Configurações', widgets: 'Widgets' };
export const ICONS = ['cpu', 'memory', 'temp', 'fan', 'disks', 'disk', 'download', 'upload', 'net', 'globe', 'shield', 'clock', 'activity', 'battery', 'bolt', 'power', 'apps', 'box', 'alert', 'bell', 'star', 'home', 'user', 'image', 'folder', 'wifi', 'network', 'info', 'check', 'sparkles', 'layers', 'link'];

/* ── Fonte BrunOS (calculada no navegador, sem coleta nova) ────── */
const B = (id, name, kind, unit, icon, extra = {}) => ({ id: `brunos.${id}`, name, kind, unit, icon, source: 'BrunOS', ...extra });
export const BRUNOS_METRICS = [
  B('cpu', 'CPU', 'num', '%', 'cpu', { hist: 'cpu', max: 100 }),
  B('mem', 'Memória usada', 'num', 'B', 'memory'),
  B('mem_pct', 'Memória (%)', 'num', '%', 'memory', { hist: 'mem', max: 100 }),
  B('temp', 'Temperatura', 'num', '°C', 'temp', { hist: 'temp' }),
  B('fan', 'Ventoinha', 'num', 'RPM', 'fan'),
  B('load', 'Carga média', 'text', '', 'activity'),
  B('rx', 'Download', 'num', 'B/s', 'download', { hist: 'rx' }),
  B('tx', 'Upload', 'num', 'B/s', 'upload', { hist: 'tx' }),
  B('storage_used', 'Armazenamento usado', 'num', 'B', 'disks'),
  B('storage_free', 'Armazenamento livre', 'num', 'B', 'disks'),
  B('storage_pct', 'Armazenamento (%)', 'num', '%', 'disks', { max: 100 }),
  B('uptime', 'Tempo ligado', 'num', 's', 'clock'),
  B('battery', 'Bateria', 'num', '%', 'battery', { max: 100 }),
  B('battery_state', 'Bateria: situação', 'text', '', 'battery'),
  B('battery_wh', 'Bateria: energia', 'num', 'Wh', 'battery'),
  B('battery_w', 'Bateria: consumo', 'num', 'W', 'bolt'),
  B('battery_health', 'Bateria: saúde', 'num', '%', 'battery'),
  B('net_latency', 'Internet: latência', 'num', 'ms', 'globe', { hist: 'net' }),
  B('net_uptime', 'Internet: tempo no ar (24 h)', 'num', '%', 'globe'),
  B('apps_running', 'Apps rodando', 'num', '', 'apps'),
  B('apps_down', 'Apps fora do ar', 'num', '', 'alert'),
];
const BRUNOS_BY_ID = Object.fromEntries(BRUNOS_METRICS.map((m) => [m.id, m]));
export const brunosDef = (id) => BRUNOS_BY_ID[id];

/** Valor atual de uma Métrica da fonte BrunOS. S = { snapshot, internet, apps }. */
export function brunosValue(id, S) {
  const sn = S.snapshot, b = sn?.battery, st = S.internet;
  const disks = sn?.disks || [];
  const used = disks.reduce((s, d) => s + d.used, 0), total = disks.reduce((s, d) => s + d.total, 0);
  switch (id) {
    case 'brunos.cpu': return sn && { n: sn.cpu.percent, max: 100 };
    case 'brunos.mem': return sn && { n: sn.mem.used, max: sn.mem.total };
    case 'brunos.mem_pct': return sn && { n: (sn.mem.used / sn.mem.total) * 100, max: 100 };
    case 'brunos.temp': return sn && (sn.temp != null ? { n: sn.temp } : { t: '—' });
    case 'brunos.fan': return sn && (sn.fanRpm ? { n: sn.fanRpm } : { t: 'sem ventoinha' });
    case 'brunos.load': return sn && { t: sn.cpu.load.map((v) => fmt(v, 2)).join(' · ') };
    case 'brunos.rx': return sn && { n: sn.net.rx };
    case 'brunos.tx': return sn && { n: sn.net.tx };
    case 'brunos.storage_used': return sn && { n: used, max: total };
    case 'brunos.storage_free': return sn && { n: total - used };
    case 'brunos.storage_pct': return sn && { n: total ? (used / total) * 100 : 0, max: 100 };
    case 'brunos.uptime': return sn && { n: sn.uptime };
    case 'brunos.battery': {
      if (!sn) return undefined;
      if (!b) return { t: 'sem bateria' };
      const p = b.percent;
      return { n: p, max: 100, glyph: b, level: p >= 90 ? 'ok' : p >= 30 ? '' : p >= 20 ? 'aviso' : 'critico' };
    }
    case 'brunos.battery_state': { if (!sn) return undefined; const s = stateText(b); return { t: b ? `${s.title} · ${s.sub.charAt(0).toLowerCase()}${s.sub.slice(1)}` : s.sub }; }
    case 'brunos.battery_wh': return b?.energyWh != null ? { n: b.energyWh } : sn && { t: '—' };
    case 'brunos.battery_w': return b?.powerW != null ? { n: b.powerW } : sn && { t: '—' };
    case 'brunos.battery_health': return b?.health != null ? { n: b.health } : sn && { t: '—' };
    case 'brunos.net_latency':
      if (!st) return undefined;
      if (!st.enabled) return { t: 'desligado' };
      if (!st.checked) return { t: 'medindo…' };
      return st.up ? { n: st.ms } : { t: 'sem internet', level: 'critico' };
    case 'brunos.net_uptime': return st?.uptime != null ? { n: st.uptime } : undefined;
    case 'brunos.apps_running': return S.apps && { n: S.apps.filter((a) => a.state === 'running').length };
    case 'brunos.apps_down': return S.apps && { n: S.apps.filter((a) => a.health === 'down' && a.state !== 'stopped').length };
    default: return undefined;
  }
}

/* ── Formatação dos valores ────────────────────────────────────── */
const compactFmt = new Intl.NumberFormat('pt-BR', { notation: 'compact', maximumFractionDigits: 1 });
const autoDec = (n) => (Math.abs(n) >= 100 || Number.isInteger(n) ? 0 : 1);
const splitUnit = (s) => { const i = s.lastIndexOf(' '); return i < 0 ? { v: s, u: '' } : { v: s.slice(0, i), u: ` ${s.slice(i + 1)}` }; };

/** { v, u } do valor de uma Métrica, com as escolhas do Espaço (unidade, casas, compacto). */
export function fmtVal(val, def, slot = {}) {
  if (!val) return { v: '—', u: '' };
  if (val.n == null) return { v: val.t ?? '—', u: '' };
  const n = val.n, unit = def?.unit || '', d = slot.decimals ?? null;
  if (slot.unit) return { v: slot.compact ? compactFmt.format(n) : fmt(n, d ?? autoDec(n)), u: ` ${slot.unit}` };
  switch (unit) {
    case 'B': return splitUnit(fmtBytes(n, d ?? 1));
    case 'B/s': return splitUnit(fmtRate(n));
    case '%': return { v: fmt(n, d ?? (n < 10 && !Number.isInteger(n) ? 1 : 0)), u: '%' };
    case 'ts': return { v: timeAgo(n), u: '' };
    case 's': return { v: fmtDuration(n), u: '' };
    case '°C': return { v: fmt(n, d ?? 0), u: ' °C' };
    default: return { v: slot.compact ? compactFmt.format(n) : fmt(n, d ?? autoDec(n)), u: unit ? ` ${unit}` : '' };
  }
}
export const fmtText = (val, def, slot) => { const f = fmtVal(val, def, slot); return f.v + f.u; };

/** Cores por faixa: { warn, crit }. warn < crit = maior é pior (temperatura); warn > crit = menor é pior (download). */
export function defaultColors(def) {
  const r = def?.ranges;
  if (!r || r.length < 2 || r[0].max == null || r[1].max == null) return null;
  return { warn: r[0].max, crit: r[1].max };
}
export function levelOf(val, def, slot = {}) {
  if (val?.level != null && val.level !== '') return val.level;
  if (val?.level === '') return '';
  const c = slot.colors === undefined ? defaultColors(def) : slot.colors;
  if (!c || val?.n == null || c.warn == null || c.crit == null) return '';
  const n = val.n;
  if (c.warn <= c.crit) return n > c.crit ? 'critico' : n > c.warn ? 'aviso' : 'ok';
  return n < c.crit ? 'critico' : n < c.warn ? 'aviso' : 'ok';
}
const lv = (l) => (l ? ` wl-${l}` : '');

/* ── Modelos ───────────────────────────────────────────────────── */
const S1 = (metric, extra = {}) => ({ metric, ...extra });
export const BLANK = { format: 'stat', size: 's', title: '', caption: true, slots: [], chart: { type: 'line', range: '24h', stats: false }, layout: 'auto', list: { max: 0, bars: true }, bar: true, click: { kind: 'auto' }, look: { follow: true } };

function brunosModels() {
  return [
    { id: 'storage', name: 'Armazenamento', desc: 'Espaço usado e livre nos discos', item: { format: 'stat', title: 'Armazenamento', slots: [S1('brunos.storage_used'), S1('brunos.storage_free', { label: 'livres' })], click: { kind: 'window', target: 'live' } } },
    { id: 'system', name: 'Sistema', desc: 'CPU, memória e temperatura', item: { format: 'grid', title: 'Sistema', caption: false, slots: [S1('brunos.cpu', { label: 'CPU' }), S1('brunos.mem', { label: 'Memória' }), S1('brunos.temp', { label: 'Temp.' })], click: { kind: 'window', target: 'live' } } },
    { id: 'network', name: 'Rede', desc: 'Download e upload agora', item: { format: 'grid', title: 'Rede', caption: false, layout: 'rows', slots: [S1('brunos.rx'), S1('brunos.tx')], click: { kind: 'window', target: 'live' } } },
    { id: 'battery', name: 'Bateria', desc: 'Carga, situação, energia, consumo e saúde', item: { format: 'stat', title: 'Bateria', bar: false, slots: [S1('brunos.battery'), S1('brunos.battery_state', { label: '' }), S1('brunos.battery_wh', { label: 'Energia' }), S1('brunos.battery_w', { label: 'Consumo' }), S1('brunos.battery_health', { label: 'Saúde' })], click: { kind: 'window', target: 'battery' } } },
    { id: 'internet', name: 'Internet', desc: 'Latência e tempo no ar nas últimas 24 h', item: { format: 'stat', title: 'Internet', slots: [S1('brunos.net_latency', { spark: true }), S1('brunos.net_uptime', { label: 'no ar · 24 h' })], click: { kind: 'window', target: 'internet' } } },
    { id: 'files', name: 'Arquivos', desc: 'Atalhos para as pastas favoritas', item: { format: 'shortcuts', title: 'Arquivos', caption: false } },
    { id: 'cpu-chart', name: 'CPU e temperatura', desc: 'Gráfico das últimas 24 horas', item: { format: 'chart', size: 'w', title: 'CPU e temperatura', slots: [S1('brunos.cpu'), S1('brunos.temp')], chart: { type: 'line', range: '24h', stats: true }, click: { kind: 'window', target: 'live' } } },
    { id: 'net-chart', name: 'Tráfego de rede', desc: 'Download e upload nas últimas 24 horas', item: { format: 'chart', size: 'w', title: 'Rede', slots: [S1('brunos.rx', { label: 'Download' }), S1('brunos.tx', { label: 'Upload' })], chart: { type: 'line', range: '24h', stats: false }, click: { kind: 'window', target: 'live' } } },
  ];
}
// Modelos de cada Tipo de integração (preenchidos com a integração escolhida)
const KIND_MODELS = {
  adguard: (it) => [
    { id: 'summary', name: `${it.name}: resumo`, desc: 'Consultas, % bloqueado e tempo de resposta', item: { format: 'grid', title: it.name, caption: false, slots: [S1(`${it.id}.queries`, { label: 'Consultas', compact: true }), S1(`${it.id}.blocked_pct`, { label: 'Bloqueado' }), S1(`${it.id}.avg_time`, { label: 'Resposta' })] } },
    { id: 'chart', name: `${it.name}: por hora`, desc: 'Consultas e bloqueios em barras', item: { format: 'chart', size: 'w', title: it.name, slots: [S1(`${it.id}.queries_series`, { label: 'Consultas' }), S1(`${it.id}.blocked_series`, { label: 'Bloqueios' })], chart: { type: 'bar', range: '24h', stats: false } } },
    { id: 'top', name: `${it.name}: mais bloqueados`, desc: 'Lista dos domínios mais bloqueados', item: { format: 'list', size: 't', title: 'Mais bloqueados', slots: [S1(`${it.id}.top_blocked`)] } },
    { id: 'ring', name: `${it.name}: anel`, desc: '% bloqueado num anel', item: { format: 'chart', size: 's', title: it.name, slots: [S1(`${it.id}.blocked_pct`, { label: 'bloqueado' })], chart: { type: 'donut', range: '24h', stats: false } } },
  ],
  immich: (it) => [
    { id: 'today', name: `${it.name}: neste dia`, desc: 'Fotos deste dia em anos anteriores (ou aleatórias)', item: { format: 'media', size: 'l', title: 'Neste dia', slots: [S1(`${it.id}.photo_today`)], media: { rotate: 30 } } },
    { id: 'random', name: `${it.name}: foto aleatória`, desc: 'Uma foto diferente a cada meia hora', item: { format: 'media', size: 'w', title: 'Fotos', slots: [S1(`${it.id}.photo_random`)], media: { rotate: 30 } } },
    { id: 'totals', name: `${it.name}: totais`, desc: 'Fotos e vídeos guardados', item: { format: 'grid', title: it.name, caption: false, slots: [S1(`${it.id}.photos`, { label: 'Fotos', compact: true }), S1(`${it.id}.videos`, { label: 'Vídeos', compact: true })] } },
  ],
  plex: (it) => [
    { id: 'now', name: `${it.name}: tocando agora`, desc: 'Capa, título, quem assiste e quanto já foi', item: { format: 'media', size: 'w', title: 'Tocando agora', slots: [S1(`${it.id}.now_media`)] } },
    { id: 'libs', name: `${it.name}: bibliotecas`, desc: 'Filmes, séries e artistas', item: { format: 'grid', title: it.name, caption: false, slots: [S1(`${it.id}.movies`, { label: 'Filmes' }), S1(`${it.id}.shows`, { label: 'Séries' }), S1(`${it.id}.sessions`, { label: 'Agora' })] } },
    { id: 'list', name: `${it.name}: sessões`, desc: 'Lista do que está tocando', item: { format: 'list', size: 't', title: 'No Plex agora', slots: [S1(`${it.id}.now_playing`)] } },
  ],
  watchyourlan: (it) => [
    { id: 'home', name: `${it.name}: rede de casa`, desc: 'Online, desconhecidos e total', item: { format: 'grid', title: 'Rede de casa', caption: false, layout: 'rows', slots: [S1(`${it.id}.online`, { label: 'Online' }), S1(`${it.id}.unknown`, { label: 'Desconhecidos', colors: { warn: 0, crit: 99 } }), S1(`${it.id}.total`, { label: 'Total' })] } },
    { id: 'online', name: `${it.name}: online agora`, desc: 'Lista dos aparelhos ligados', item: { format: 'list', size: 't', title: 'Online agora', slots: [S1(`${it.id}.online_list`)], list: { max: 0, bars: false } } },
    { id: 'unknown', name: `${it.name}: desconhecidos`, desc: 'Aparelhos que o WatchYourLAN não conhece', item: { format: 'list', size: 's', title: 'Desconhecidos', slots: [S1(`${it.id}.unknown_list`)], list: { max: 0, bars: false } } },
  ],
  tailscale: (it) => [
    { id: 'status', name: `${it.name}: situação`, desc: 'Conectado, aparelhos online e o IP deste servidor', item: { format: 'grid', title: 'Tailscale', caption: false, layout: 'rows', slots: [S1(`${it.id}.state`, { label: 'Situação' }), S1(`${it.id}.online`, { label: 'Online' }), S1(`${it.id}.self_ip`, { label: 'IP' })] } },
    { id: 'peers', name: `${it.name}: aparelhos`, desc: 'Quem está na sua rede Tailscale', item: { format: 'list', size: 't', title: 'Tailscale', slots: [S1(`${it.id}.peers`)], list: { max: 0, bars: false } } },
  ],
  speedtest: (it) => [
    { id: 'last', name: `${it.name}: último teste`, desc: 'Download, upload e ping', item: { format: 'grid', title: 'Velocidade', caption: false, slots: [S1(`${it.id}.download`, { label: 'Download' }), S1(`${it.id}.upload`, { label: 'Upload' }), S1(`${it.id}.ping`, { label: 'Ping' })] } },
    { id: 'chart', name: `${it.name}: 7 dias`, desc: 'Gráfico de download e upload', item: { format: 'chart', size: 'w', title: 'Velocidade da internet', slots: [S1(`${it.id}.download_series`, { label: 'Download' }), S1(`${it.id}.upload_series`, { label: 'Upload' })], chart: { type: 'line', range: '7d', stats: true } } },
    { id: 'big', name: `${it.name}: destaque`, desc: 'Download grande com o histórico', item: { format: 'stat', title: 'Download', slots: [S1(`${it.id}.download`, { spark: true }), S1(`${it.id}.last_test`, { label: '' }), S1(`${it.id}.upload`, { label: 'Upload' }), S1(`${it.id}.ping`, { label: 'Ping' })] } },
  ],
};

/** Modelos agrupados: [{ group, models: [{ id, name, desc, item }] }]. */
export function modelGroups(integrations = []) {
  const groups = [{ group: 'BrunOS', models: brunosModels().map((m) => ({ ...m, id: `brunos:${m.id}` })) }];
  for (const it of integrations) {
    const mk = KIND_MODELS[it.kind];
    if (mk) groups.push({ group: it.name, models: mk(it).map((m) => ({ ...m, id: `${it.id}:${m.id}` })) });
  }
  groups.push({ group: 'Em branco', models: Object.entries(FORMATS).map(([k, f]) => ({ id: `blank:${k}`, name: f.label, desc: f.desc, item: { format: k, title: f.label, size: k === 'chart' ? 'w' : 's' } })) });
  return groups;
}

/** Completa um item com os padrões (itens antigos ou incompletos continuam funcionando). */
export function normalize(item) {
  const it = { ...structuredClone(BLANK), ...structuredClone(item || {}) };
  it.chart = { ...BLANK.chart, ...(item?.chart || {}) };
  it.list = { ...BLANK.list, ...(item?.list || {}) };
  it.click = { ...BLANK.click, ...(item?.click || {}) };
  it.look = { ...BLANK.look, ...(item?.look || {}) };
  it.files = { layout: 'grid', showFree: false, ...(item?.files || {}) };
  it.media = { rotate: 30, caption: true, ...(item?.media || {}) };
  it.folders = Array.isArray(item?.folders) ? item.folders.filter((f) => f && f.root).slice(0, 24) : [];
  if (!SIZES[it.size]) it.size = 's';
  if (!FORMATS[it.format]) it.format = 'stat';
  it.slots = (it.slots || []).filter((s) => s && typeof s.metric === 'string').slice(0, FORMATS[it.format].max || 0);
  return it;
}

export const newId = () => `w${Math.random().toString(36).slice(2, 9)}`;

/** Converte a preferência antiga `widgets` (0.8.x) nos Modelos equivalentes, na página 1. */
export function migrate(oldList) {
  const byOld = Object.fromEntries(brunosModels().map((m) => [m.id, m.item]));
  const items = {}, ids = [];
  for (const k of oldList || []) {
    const src = byOld[k];
    if (!src) continue;
    const id = newId();
    items[id] = { ...src, model: `brunos:${k}` };
    ids.push(id);
  }
  return { v: 1, areas: ids.length ? { 0: ids } : {}, items };
}

/* ── Desenho ───────────────────────────────────────────────────── */
// Área útil aproximada de cada Tamanho (para o viewBox dos gráficos)
const DIMS = { s: [212, 120], w: [480, 120], t: [212, 300], l: [480, 300], f: [960, 120] };

function cellsOf(n, size, layout) {
  const rowsMode = layout === 'rows', colsMode = layout === 'cols';
  const by = {
    s: rowsMode ? (n <= 3 ? 1 : 2) : colsMode ? Math.min(n, 3) : n <= 3 ? n : n === 4 ? 2 : 3,
    w: rowsMode ? (n <= 2 ? 1 : 2) : n <= 4 ? n : 3,
    t: colsMode ? Math.min(n, 2) : n <= 3 ? 1 : 2,
    l: rowsMode ? (n <= 3 ? 1 : 2) : n <= 2 ? n : n <= 4 ? 2 : 3,
    f: rowsMode ? Math.min(n, 3) : n,
  };
  const cols = Math.max(1, by[size] || n), rows = Math.ceil(n / cols);
  const [w, h] = DIMS[size];
  return { cols, rows, row: (w / cols) / (h / rows) > 1.9 };
}

const iconOf = (slot, def) => svg(slot.icon || def?.icon || 'info');
const labelOf = (slot, def) => (slot.label != null ? slot.label : (def?.name || '').replace(/^.* › /, ''));

function errBadge(item, ctx) {
  const errs = [...new Set(item.slots.map((s) => ctx.err(s.metric)).filter(Boolean))];
  return errs.length ? `<span class="w-err" title="${esc(errs.join('\n'))}">${svg('alert')}</span>` : '';
}
function sparkFor(slot, ctx, size, w = 200, h = 28) {
  const pts = ctx.spark(slot.metric);
  return pts ? sparkSVG(pts, { w, h, maxGap: ctx.maxGap?.(slot.metric) }) : '';
}

function statHTML(item, ctx) {
  const [main, sub, ...cells] = item.slots;
  if (!main) return `<span class="w-empty">Escolha uma métrica</span>`;
  const def = ctx.def(main.metric), val = ctx.value(main.metric);
  const f = fmtVal(val, def, main);
  const max = val?.max ?? main.max ?? null;
  const maxTxt = max != null && def?.unit === 'B' && !main.unit ? ` / ${fmtBytes(max)}` : '';
  const glyph = val?.glyph ? batteryGlyph(val.glyph, 'wf-glyph') : '';
  let subTxt = '';
  if (sub) {
    const sv = ctx.value(sub.metric), sd = ctx.def(sub.metric);
    const lbl = sub.label != null ? sub.label : labelOf(sub, sd);
    subTxt = `${fmtText(sv, sd, sub)}${lbl ? ` ${lbl}` : ''}`;
  }
  const pct = max && val?.n != null ? clamp((val.n / max) * 100, 0, 100) : null;
  const big = item.size === 't' || item.size === 'l';
  return `${item.caption ? `<span class="w-caption">${esc(item.title)}</span>` : ''}
    <span class="w-value wf-main${lv(levelOf(val, def, main))}">${glyph}<span>${esc(f.v)}</span><span class="w-unit">${esc(f.u + maxTxt)}</span></span>
    ${main.spark ? `<span class="wf-spark ${cells.length && !big ? 'low' : ''}">${sparkFor(main, ctx, item.size, 200, big ? 70 : cells.length ? 18 : 30)}</span>` : ''}
    ${subTxt ? `<span class="w-sub wf-sub">${esc(subTxt)}</span>` : ''}
    ${pct != null && item.bar !== false && (!cells.length || big) ? `<span class="track wf-bar ${item.look?.follow === false ? '' : 'white'}"><i style="width:${pct.toFixed(1)}%"></i></span>` : ''}
    ${cells.length ? `<span class="wb-cells wf-cells" style="--n:${cells.length}">${cells.map((c) => { const d = ctx.def(c.metric), v = ctx.value(c.metric); return `<span class="cell" title="${esc(labelOf(c, d))}"><span class="k">${esc(labelOf(c, d))}</span><span class="v${lv(levelOf(v, d, c))}">${esc(fmtText(v, d, c))}</span></span>`; }).join('')}</span>` : ''}`;
}

function gridHTML(item, ctx) {
  const n = item.slots.length;
  if (!n) return `<span class="w-empty">Escolha as métricas</span>`;
  const { cols, row } = cellsOf(n, item.size, item.layout);
  return `${item.caption ? `<span class="w-caption wf-cap">${esc(item.title)}</span>` : ''}<span class="wf-grid ${row ? 'rows' : 'pills'}" style="--cols:${cols}">${item.slots.map((s) => {
    const def = ctx.def(s.metric), val = ctx.value(s.metric), f = fmtVal(val, def, s);
    const sp = s.spark ? `<span class="wf-cspark">${sparkFor(s, ctx, item.size, 100, 18)}</span>` : '';
    return row
      ? `<span class="cell wf-cell"><span class="k">${iconOf(s, def)} ${esc(labelOf(s, def))}</span>${sp}<span class="v${lv(levelOf(val, def, s))}">${esc(f.v + f.u)}</span></span>`
      : `<span class="cell wf-cell">${iconOf(s, def)}<span class="k">${esc(labelOf(s, def))}</span><span class="v${lv(levelOf(val, def, s))}">${esc(f.v + f.u)}</span>${sp}</span>`;
  }).join('')}</span>`;
}

function listHTML(item, ctx) {
  const first = item.slots[0];
  if (!first) return `<span class="w-empty">Escolha uma lista ou métricas</span>`;
  const def0 = ctx.def(first.metric);
  const cap = item.caption ? `<span class="w-caption wf-cap">${esc(item.title)}</span>` : '';
  if (def0?.kind === 'list') {
    const per = { s: 3, w: 3, t: 8, l: 8, f: 3 }[item.size];
    const max = item.list?.max > 0 ? Math.min(item.list.max, per) : per;
    const items = (ctx.value(first.metric)?.l || []).slice(0, max);
    const top = Math.max(1, ...items.map((x) => x.n || 0));
    return `${cap}<span class="wf-list ${item.size === 'f' || item.size === 'w' ? 'cols' : ''}">${items.length ? items.map((x) => x.n == null && x.v ? `<span class="wf-li stack"><span class="wf-lk" title="${esc(x.k)}">${esc(x.k)}</span><small class="wf-lsub" title="${esc(x.v)}">${esc(x.v)}</small></span>` : `<span class="wf-li"><span class="wf-lk" title="${esc(x.k)}">${esc(x.k)}</span><span class="wf-lv">${esc(x.n != null ? fmtText({ n: x.n }, { unit: first.unit ? '' : def0.unit }, first) : x.v || '')}</span>${item.list?.bars !== false && x.n != null ? `<span class="wf-lbar"><i style="width:${((x.n / top) * 100).toFixed(1)}%"></i></span>` : ''}</span>`).join('') : '<span class="w-empty">Sem itens ainda</span>'}</span>`;
  }
  return `${cap}<span class="wf-list">${item.slots.map((s) => {
    const def = ctx.def(s.metric), val = ctx.value(s.metric);
    return `<span class="wf-li rowv"><span class="wf-lk">${iconOf(s, def)} ${esc(labelOf(s, def))}</span>${s.spark ? `<span class="wf-cspark">${sparkFor(s, ctx, item.size, 100, 18)}</span>` : ''}<span class="wf-lv${lv(levelOf(val, def, s))}">${esc(fmtText(val, def, s))}</span></span>`;
  }).join('')}</span>`;
}

function chartHTML(item, ctx) {
  if (!item.slots.length) return `<span class="w-empty">Escolha o que vai no gráfico</span>`;
  const [W, H] = DIMS[item.size];
  const type = item.chart?.type || 'line';
  const head0 = item.slots[0], d0 = ctx.def(head0.metric), v0 = ctx.value(head0.metric);
  const cap = `<span class="wf-chead"><span class="w-caption">${esc(item.title || labelOf(head0, d0))}</span>`;
  if (type === 'donut') {
    const parts = item.slots.map((s, i) => ({ v: ctx.value(s.metric)?.n ?? 0, cls: `c${i}` }));
    const one = item.slots.length === 1;
    const total = one ? (v0?.max ?? head0.max ?? (d0?.unit === '%' ? 100 : null)) : null;
    const f = fmtVal(v0, d0, head0);
    const sz = item.size === 's' ? 84 : item.size === 'w' || item.size === 'f' ? 92 : 170;
    const legend = item.slots.map((s, i) => { const d = ctx.def(s.metric); return `<span class="wf-leg"><i class="c${i}"></i>${esc(labelOf(s, d))} <b>${esc(fmtText(ctx.value(s.metric), d, s))}</b></span>`; }).join('');
    return `${cap}</span><span class="wf-donut ${item.size}"><span class="wf-ring" style="--sz:${sz}px">${donutSVG(parts, { total: one ? total ?? undefined : undefined, size: 100, thick: 11 })}<span class="wf-ringv"><b class="${lv(levelOf(v0, d0, head0)).trim()}">${esc(f.v)}<small>${esc(f.u)}</small></b>${one && head0.label ? `<small>${esc(head0.label)}</small>` : ''}</span></span>${one ? '' : `<span class="wf-legend">${legend}</span>`}</span>`;
  }
  const series = item.slots.map((s, i) => ({ pts: ctx.series(s.metric, item.chart?.range || '24h') || [], cls: `c${i}` }));
  const has = series.some((s) => s.pts.filter((p) => p.v != null).length >= 2);
  const headVal = d0?.kind === 'num' && v0 ? `<span class="wf-cval">${esc(fmtText(v0, d0, head0))}</span>` : '';
  const legend = item.slots.length > 1 ? `<span class="wf-legend inline">${item.slots.map((s, i) => `<span class="wf-leg"><i class="c${i}"></i>${esc(labelOf(s, ctx.def(s.metric)))}</span>`).join('')}</span>` : '';
  const chH = Math.round(H * (item.chart?.stats ? 0.62 : 0.72));
  const svgTxt = !has ? '' : type === 'bar' ? barSVG(series, { w: W, h: chH }) : lineSVG(series, { w: W, h: chH, area: series.length === 1, maxGap: ctx.maxGap?.(head0.metric) });
  let st = '';
  if (item.chart?.stats && has) {
    const s = stats(series[0].pts), def = d0?.kind === 'series' ? { unit: d0.unit } : d0;
    if (s) st = `<span class="wf-stats">mín. ${esc(fmtText({ n: s.min }, def, head0))} · máx. ${esc(fmtText({ n: s.max }, def, head0))} · média ${esc(fmtText({ n: s.avg }, def, head0))}</span>`;
  }
  return `${cap}${headVal}</span>${legend}<span class="wf-chart">${svgTxt || `<span class="w-empty">${ctx.loading?.(item) ? 'carregando…' : 'ainda sem histórico'}</span>`}</span>${st}`;
}

// Ícones e cores que o Bruno pode escolher para cada pasta do formato Atalhos
export const FOLDER_ICONS = ['folder', 'film', 'music', 'image', 'doc', 'download', 'box', 'disks', 'star', 'home', 'user', 'code'];
export const FOLDER_HUES = [209, 259, 330, 6, 24, 45, 145, 174];
function folderIcon(f) {
  if ((!f.icon || f.icon === 'folder') && f.color == null) return folderSVG();
  return `<span class="wf-ficon" style="--fh:${f.color ?? 'var(--bh)'}">${svg(f.icon || 'folder')}</span>`;
}
function freeOf(f, roots) {
  const r = roots.find((x) => x.id === f.root);
  return r && r.total > 0 ? { free: r.free, pct: clamp(((r.total - r.free) / r.total) * 100, 0, 100) } : null;
}
function shortcutsHTML(item, ctx) {
  const list = item.files?.layout === 'list';
  const roots = ctx.roots?.() || [];
  const chosen = (item.folders || []).filter((f) => roots.some((r) => r.id === f.root));
  const all = chosen.length ? chosen.map((f) => ({ ...f, name: f.name || f.path.split('/').pop() || roots.find((r) => r.id === f.root)?.name || f.root })) : ctx.favorites();
  const cols = list ? { s: 1, w: 2, t: 1, l: 2, f: 4 }[item.size] : { s: 2, w: 4, t: 2, l: 4, f: 6 }[item.size];
  const rows = list ? { s: 3, w: 3, t: 7, l: 7, f: 3 }[item.size] : { s: 2, w: 2, t: 4, l: 4, f: 2 }[item.size];
  const fav = all.slice(0, cols * rows);
  const showFree = item.files?.showFree;
  const cell = (f) => {
    const fr = showFree ? freeOf(f, roots) : null;
    const free = fr ? `<small class="wf-free">${esc(fmtBytes(fr.free))} livres</small>` : '';
    const bar = fr && list ? `<span class="wf-fbar"><i style="width:${fr.pct.toFixed(1)}%"></i></span>` : '';
    return `<button class="cell cell-btn ${list ? 'wf-frow' : ''}" data-fav-root="${esc(f.root)}" data-fav-path="${esc(f.path)}" title="${esc(f.name)}">${folderIcon(f)}<span class="wf-fname"><span>${esc(f.name)}</span>${free}</span>${bar}</button>`;
  };
  return `${item.caption ? `<span class="w-caption wf-cap">${esc(item.title)}</span>` : ''}<span class="w-files wf-files ${list ? 'list' : ''} ${showFree ? 'free' : ''}" style="--cols:${cols};--rows:${rows}">${fav.length ? fav.map(cell).join('') : '<span class="w-empty">Nenhuma pasta disponível</span>'}</span>`;
}

// Mídia: foto cobrindo o widget (Immich) ou capa em pé ao lado do texto (Plex). A troca de foto é feita
// aqui, pelo relógio: todas as abas mostram a mesma foto e nada é pedido ao servidor além da imagem.
function mediaHTML(item, ctx) {
  const slot = item.slots[0];
  if (!slot) return `<span class="w-empty">Escolha a fonte das fotos</span>`;
  const def = ctx.def(slot.metric), list = ctx.value(slot.metric)?.l || [];
  const iid = slot.metric.split('.')[0];
  const poster = def?.look === 'poster';
  if (!list.length) {
    return `<span class="wf-mempty">${svg(poster ? 'film' : 'image')}<span>${esc(poster ? 'Nada tocando agora' : ctx.value(slot.metric) ? 'Nenhuma foto encontrada' : 'carregando…')}</span></span>`;
  }
  const every = Math.max(1, item.media?.rotate || 30) * 60000;
  const x = poster ? list[0] : list[Math.floor(Date.now() / every) % list.length];
  const src = x.img ? ctx.imageUrl(iid, x.img) : '';
  const cap = item.media?.caption !== false;
  if (poster) {
    const more = list.length > 1 ? `<span class="wf-more">+${list.length - 1}</span>` : '';
    return `<span class="wf-poster">${src ? `<img src="${esc(src)}" alt="" loading="lazy" decoding="async">` : svg('film')}</span>
      <span class="wf-ptext">${item.caption ? `<span class="w-caption">${esc(item.title)}</span>` : ''}<b>${esc(x.k)}</b><small>${esc(x.v || '')}</small>
      ${x.n != null ? `<span class="track wf-pbar"><i style="width:${clamp(x.n, 0, 100).toFixed(1)}%"></i></span>` : ''}${more}</span>`;
  }
  return `<span class="wf-photo">${src ? `<img src="${esc(src)}" alt="" loading="lazy" decoding="async">` : ''}</span>
    ${cap ? `<span class="wf-pcap">${item.caption ? `<span class="w-caption">${esc(item.title)}</span>` : ''}<b>${esc(x.k || '')}</b>${x.v ? `<small>${esc(x.v)}</small>` : ''}</span>` : ''}`;
}

const RENDER = { stat: statHTML, grid: gridHTML, list: listHTML, chart: chartHTML, media: mediaHTML, shortcuts: shortcutsHTML };

/** HTML de dentro do widget (o que muda quando chegam valores novos). */
export function innerHTML(item, ctx) {
  return (RENDER[item.format] || statHTML)(item, ctx) + errBadge(item, ctx);
}

/** Estilo de aparência própria (só quando não segue o tema). */
export function lookStyle(item) {
  const l = item.look || {};
  if (l.follow !== false) return '';
  let s = '';
  if (l.opacity != null) s += `--w-alpha:${clamp(l.opacity, 0, 100) / 100};`;
  if (l.hue != null) s += `--bh:${l.hue};--bs:80%;--bl:50%;`;
  return s;
}

/** Posição na grade: largura em colunas (limitada às colunas da Área) e altura em linhas. */
export function spanStyle(size, cols = 6) {
  const sz = SIZES[size] || SIZES.s;
  return `${sz.c ? `grid-column:span ${Math.min(sz.c, cols)};` : 'grid-column:1 / -1;'}${sz.r > 1 ? `grid-row:span ${sz.r};` : ''}`;
}

/**
 * O widget inteiro (moldura + rótulo). Formato Atalhos não é botão (as pastas são).
 * Arrastável sempre que tiver id (como os ícones); a alça de tamanho e o "−" só no modo Organizar.
 */
export function widgetHTML(item, ctx, { id = '', i = 0, editing = false, cols = 6 } = {}) {
  const tag = item.format === 'shortcuts' ? 'div' : 'button';
  const own = item.look?.follow === false;
  const hideLabel = own && item.look?.hideLabel;
  return `<div class="widget-wrap wsz-${item.size}" data-wid="${esc(id)}" style="--i:${i};${spanStyle(item.size, cols)}" ${id ? 'draggable="true"' : ''}>
    <${tag} class="widget wf wfm-${item.format} ${own ? 'w-own' : ''} ${own && item.look?.hue != null ? 'w-hue' : ''}" style="${lookStyle(item)}" ${tag === 'button' ? `type="button" aria-label="${esc(item.title || FORMATS[item.format].label)}"` : ''}>${innerHTML(item, ctx)}</${tag}>
    <div class="widget-label ${hideLabel ? 'is-hidden' : ''}">${esc(item.title || '')}</div>
    ${editing ? `<button class="w-remove" data-wremove="${esc(id)}" aria-label="Remover widget">${svg('minus')}</button><span class="w-resize" data-wresize="${esc(id)}" title="Arraste para mudar o tamanho" aria-hidden="true"></span>` : ''}
  </div>`;
}

/** Tamanho mais próximo para uma largura (colunas) e altura (linhas) pedidas pela alça. */
export function sizeFor(c, r) {
  if (r >= 2) return c >= 2 ? 'l' : 't';
  return c >= 3 ? 'f' : c === 2 ? 'w' : 's';
}

/** Para onde o clique vai quando o Bruno deixou "automático". */
export function autoClick(item) {
  if (item.format === 'shortcuts') return { kind: 'none' };
  const m = item.slots[0]?.metric || '';
  if (m.startsWith('brunos.')) {
    if (m.startsWith('brunos.battery')) return { kind: 'window', target: 'battery' };
    if (m.startsWith('brunos.net_')) return { kind: 'window', target: 'internet' };
    if (m.startsWith('brunos.apps')) return { kind: 'none' };
    return { kind: 'window', target: 'live' };
  }
  const iid = m.split('.')[0];
  return iid ? { kind: 'app', target: iid } : { kind: 'none' };
}

/** Lista de métricas que o Formato aceita, para o editor. */
export const acceptsKind = (format, kind, def) => {
  const f = FORMATS[format];
  if (!f) return false;
  if (format === 'chart') return kind === 'series' || (kind === 'num' && !!def?.chartable);
  return f.kinds.includes(kind);
};
