// Widgets personalizáveis (etapa 7): Formatos, Tamanhos, Métricas da fonte "BrunOS" e Modelos.
// Termos em CONTEXT.md. Este módulo só monta HTML a partir de um item e de um contexto (valores,
// definições, séries), então serve igual para a tela inicial e para a prévia do editor.
import { esc, fmt, fmtBytes, fmtRate, fmtDuration, timeAgo, svg, folderSVG, clamp } from './ui.js';
import { lineSVG, barSVG, donutSVG, sparkSVG, stats, initChartTips } from './charts.js';
import { batteryGlyph, stateText } from './battery.js';

/* ── Tamanhos (em quadros) e Formatos ──────────────────────────── */
// 0.13.0: Tamanho livre, largura × altura em Quadros (w × h, de 1 × 1 até a Medida da Área). Os nomes
// ficam para os tamanhos conhecidos. As letras antigas (s, w, t, l, f) só existem para converter widgets
// guardados antes e como "classe" que os Formatos usam para se arrumar (sizeKey).
export const MAX_W = 8, MAX_H = 5; // 0.13.2: Medida até 8 × 5 (vale quando cabe; senão a Área fica compacta)
export const SIZES = {
  s: { label: 'Pequeno', c: 1, r: 1 },
  w: { label: 'Largo', c: 2, r: 1 },
  t: { label: 'Alto', c: 1, r: 2 },
  l: { label: 'Grande', c: 2, r: 2 },
  f: { label: 'Faixa', c: 4, r: 1 }, // antiga "largura toda": vira 4 × 1 (a Área limita à Medida)
};
/** Classe do tamanho para o desenho: pequeno, largo, alto, grande (e faixa = bem largo e baixo). */
export function sizeKey(w, h) {
  if (h <= 1) return w <= 1 ? 's' : w >= 4 ? 'f' : 'w';
  return w <= 1 ? 't' : 'l';
}
/** Nome do tamanho: "Pequeno", "Largo", "Alto", "Grande" ou "3 × 2". */
export function sizeLabel(w, h) {
  const named = { '1x1': 'Pequeno', '2x1': 'Largo', '1x2': 'Alto', '2x2': 'Grande' }[`${w}x${h}`];
  return named || `${w} × ${h}`;
}
/** Menor Tamanho em que o Formato ainda fica bonito (o gráfico de linha/barras pede 2 de largura). */
export function minSize(item) {
  if (item?.format === 'chart' && (item.chart?.type || 'line') !== 'donut') return [2, 1];
  return [1, 1];
}
export const FORMATS = {
  stat: { label: 'Destaque', icon: 'star', desc: 'Um número grande, uma linha de apoio e até 3 valores pequenos', max: 5, kinds: ['num', 'text'] },
  grid: { label: 'Grade', icon: 'grid', desc: 'De 2 a 6 valores com ícone', max: 6, kinds: ['num', 'text'] },
  list: { label: 'Lista', icon: 'list', desc: 'Uma lista do app (ex.: domínios) ou valores um embaixo do outro', max: 6, kinds: ['list', 'num', 'text'] },
  chart: { label: 'Gráfico', icon: 'activity', desc: 'Linha, barras ou rosca, com até 3 séries', max: 3, kinds: ['series', 'num'] },
  media: { label: 'Mídia', icon: 'image', desc: 'Foto ou capa de um app (Immich, Plex), com legenda', max: 1, kinds: ['media'] },
  shortcuts: { label: 'Atalhos', icon: 'folder', desc: 'Atalhos para pastas do Arquivos, em grade ou lista', max: 0, kinds: [] },
  clock: { label: 'Relógio', icon: 'clock', desc: 'Hora digital ou de ponteiros, com data e fuso horário à escolha', max: 0, kinds: [] },
  notes: { label: 'Notas', icon: 'note', desc: 'Texto livre ou lista de tarefas, escrito no próprio widget e salvo no servidor', max: 0, kinds: [] },
};
// Fusos para o Relógio ('' = o do aparelho)
export const TIMEZONES = [['', 'Do aparelho'], ['America/Sao_Paulo', 'Brasília'], ['America/Manaus', 'Manaus'], ['America/Noronha', 'Fernando de Noronha'], ['America/Rio_Branco', 'Rio Branco'],
  ['Europe/Lisbon', 'Lisboa'], ['Europe/London', 'Londres'], ['Europe/Paris', 'Paris'], ['America/New_York', 'Nova York'], ['America/Los_Angeles', 'Los Angeles'], ['Asia/Tokyo', 'Tóquio'], ['UTC', 'UTC']];
export const ROTATE = [[5, '5 min'], [15, '15 min'], [30, '30 min'], [60, '1 hora'], [360, '6 horas'], [1440, '1 por dia']];
export const CHART_TYPES = { line: 'Linha', bar: 'Barras', donut: 'Rosca' };
export const RANGES = { live: '5 min', '1h': '1 hora', '24h': '24 horas', '48h': '48 horas', '7d': '7 dias', '30d': '30 dias' };
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
  // 0.11.0: consumo e custo (Medidor) e atualizações
  B('power_w', 'Consumo do servidor', 'num', 'W', 'bolt', { hist: 'energy' }),
  B('energy_meter', 'Consumo: medidor em uso', 'text', '', 'activity'),
  B('energy_today', 'Energia de hoje', 'num', 'kWh', 'chart'),
  B('energy_month', 'Energia do mês', 'num', 'kWh', 'chart'),
  B('cost_today', 'Gasto de hoje', 'num', 'R$', 'bag'),
  B('cost_month', 'Gasto do mês', 'num', 'R$', 'bag'),
  B('cost_projection', 'Projeção do gasto do mês', 'num', 'R$', 'bag'),
  B('apps_updates', 'Apps com atualização', 'num', '', 'arrowUp'),
  B('os_updates', 'Atualizações do Linux', 'num', '', 'refresh'),
  B('os_security', 'Atualizações de segurança', 'num', '', 'shield'),
  B('os_reboot', 'Reinício necessário', 'text', '', 'power'),
];
const METER_TXT = { plug: 'pela tomada', battery: 'pela bateria', rapl: 'só o processador', estimate: 'estimativa calibrada' };
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
    case 'brunos.power_w': { const e = S.energy; if (!e) return undefined; return e.w != null ? { n: e.w } : { t: e.err ? 'sem medidor' : 'medindo…' }; }
    case 'brunos.energy_meter': return S.energy && { t: S.energy.w != null ? METER_TXT[S.energy.meter] || '' : (S.energy.err || 'medindo…') };
    case 'brunos.energy_today': return S.energy && { n: S.energy.todayKWh };
    case 'brunos.energy_month': return S.energy && { n: S.energy.monthKWh };
    case 'brunos.cost_today': return S.energy && { n: S.energy.todayCost };
    case 'brunos.cost_month': return S.energy && { n: S.energy.monthCost };
    case 'brunos.cost_projection': return S.energy && (S.energy.projection ? { n: S.energy.projection } : { t: '—' });
    case 'brunos.apps_updates': return S.updates && { n: S.updates.apps.length };
    case 'brunos.os_updates': return S.updates && (S.updates.os.err ? { t: '—' } : { n: S.updates.os.total });
    case 'brunos.os_security': return S.updates && (S.updates.os.err ? { t: '—' } : { n: S.updates.os.security, level: S.updates.os.security ? 'aviso' : '' });
    case 'brunos.os_reboot': return S.updates && (S.updates.os.reboot ? { t: 'sim', level: 'aviso' } : { t: 'não' });
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
    case 'R$': return { v: `R$ ${fmt(n, d ?? 2)}`, u: '' };
    case 'kWh': return { v: fmt(n, d ?? (n < 10 ? 2 : 1)), u: ' kWh' };
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
    { id: 'energy', name: 'Consumo e custo', desc: 'Watts agora, hoje, gasto do mês e projeção', item: { format: 'stat', size: 'w', title: 'Consumo', bar: false, slots: [S1('brunos.power_w', { spark: true }), S1('brunos.energy_meter', { label: '' }), S1('brunos.energy_today', { label: 'Hoje' }), S1('brunos.cost_month', { label: 'Mês até agora' }), S1('brunos.cost_projection', { label: 'Projeção' })], click: { kind: 'window', target: 'power' } } },
    { id: 'energy-chart', name: 'Consumo 24 h', desc: 'Gráfico do consumo com mínimo, máximo e média', item: { format: 'chart', size: 'w', title: 'Consumo', slots: [S1('brunos.power_w')], chart: { type: 'line', range: '24h', stats: true }, click: { kind: 'window', target: 'power' } } },
    { id: 'cost', name: 'Gasto do mês', desc: 'Quanto o servidor gastou de luz no mês e a projeção', item: { format: 'stat', size: 's', title: 'Luz do servidor', bar: false, slots: [S1('brunos.cost_month'), S1('brunos.cost_projection', { label: 'projeção do mês' })], click: { kind: 'window', target: 'power' } } },
    { id: 'updates', name: 'Atualizações', desc: 'Apps com atualização, Linux e reinício necessário', item: { format: 'grid', title: 'Atualizações', caption: false, layout: 'rows', slots: [S1('brunos.apps_updates', { label: 'Apps' }), S1('brunos.os_updates', { label: 'Linux' }), S1('brunos.os_reboot', { label: 'Reiniciar' })], click: { kind: 'window', target: 'settings' } } },
    { id: 'clock', name: 'Relógio', desc: 'Hora grande com a data', item: { format: 'clock', title: 'Relógio', caption: false, clock: { style: 'digital', seconds: false, date: true, tz: '' } } },
    { id: 'clock-analog', name: 'Relógio de ponteiros', desc: 'Relógio analógico', item: { format: 'clock', title: 'Relógio', caption: false, clock: { style: 'analog', seconds: true, date: false, tz: '' } } },
    { id: 'notes', name: 'Bloco de notas', desc: 'Texto livre, salvo sozinho no servidor', item: { format: 'notes', size: 't', title: 'Notas', notes: { mode: 'text' } } },
    { id: 'todo', name: 'Lista de tarefas', desc: 'Itens com caixinha para marcar', item: { format: 'notes', size: 't', title: 'Tarefas', notes: { mode: 'list' } } },
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
  transmission: (it) => [
    { id: 'speed', name: `${it.name}: velocidade`, desc: 'Download e upload agora, com mini-gráfico', item: { format: 'grid', title: it.name, caption: false, layout: 'rows', slots: [S1(`${it.id}.down`, { label: 'Download', spark: true }), S1(`${it.id}.up`, { label: 'Upload' }), S1(`${it.id}.downloading`, { label: 'Baixando' })] } },
    { id: 'list', name: `${it.name}: baixando agora`, desc: 'Cada torrent com progresso, velocidade e quanto falta', item: { format: 'list', size: 't', title: 'Baixando', slots: [S1(`${it.id}.downloading_list`)], list: { max: 0, bars: false } } },
  ],
  syncthing: (it) => [
    { id: 'status', name: `${it.name}: situação`, desc: 'Pastas em dia, quanto falta e aparelhos conectados', item: { format: 'grid', title: it.name, caption: false, layout: 'rows', slots: [S1(`${it.id}.synced`, { label: 'Pastas em dia' }), S1(`${it.id}.need`, { label: 'Falta' }), S1(`${it.id}.devices_online`, { label: 'Conectados' })] } },
    { id: 'folders', name: `${it.name}: pastas`, desc: 'Cada pasta e como ela está', item: { format: 'list', size: 't', title: 'Syncthing', slots: [S1(`${it.id}.folders_list`)], list: { max: 0, bars: false } } },
  ],
  nextcloud: (it) => [
    { id: 'summary', name: `${it.name}: resumo`, desc: 'Usuários, arquivos e espaço livre', item: { format: 'grid', title: it.name, caption: false, layout: 'rows', slots: [S1(`${it.id}.users`, { label: 'Usuários' }), S1(`${it.id}.files`, { label: 'Arquivos', compact: true }), S1(`${it.id}.free`, { label: 'Livre' })] } },
    { id: 'active', name: `${it.name}: uso`, desc: 'Ativos agora, nas últimas 24 h e compartilhamentos', item: { format: 'list', size: 's', title: it.name, slots: [S1(`${it.id}.active_5m`, { label: 'Ativos agora' }), S1(`${it.id}.active_24h`, { label: 'Ativos em 24 h' }), S1(`${it.id}.shares`, { label: 'Compartilhamentos' })] } },
  ],
  speedtest: (it) => [
    { id: 'last', name: `${it.name}: último teste`, desc: 'Download, upload e ping', item: { format: 'grid', title: 'Velocidade', caption: false, slots: [S1(`${it.id}.download`, { label: 'Download' }), S1(`${it.id}.upload`, { label: 'Upload' }), S1(`${it.id}.ping`, { label: 'Ping' })] } },
    { id: 'chart', name: `${it.name}: 7 dias`, desc: 'Gráfico de download e upload', item: { format: 'chart', size: 'w', title: 'Velocidade da internet', slots: [S1(`${it.id}.download_series`, { label: 'Download' }), S1(`${it.id}.upload_series`, { label: 'Upload' })], chart: { type: 'line', range: '7d', stats: true } } },
    { id: 'big', name: `${it.name}: destaque`, desc: 'Download grande com o histórico', item: { format: 'stat', title: 'Download', slots: [S1(`${it.id}.download`, { spark: true }), S1(`${it.id}.last_test`, { label: '' }), S1(`${it.id}.upload`, { label: 'Upload' }), S1(`${it.id}.ping`, { label: 'Ping' })] } },
  ],
  weather: (it) => [
    { id: 'now', name: `${it.name}: agora`, desc: 'Temperatura, ícone do tempo, máxima, mínima e chuva', item: { format: 'stat', title: it.options?.city?.split(',')[0] || 'Clima', bar: false, slots: [S1(`${it.id}.temp`), S1(`${it.id}.cond`, { label: '' }), S1(`${it.id}.max`, { label: 'Máx.' }), S1(`${it.id}.min`, { label: 'Mín.' }), S1(`${it.id}.rain`, { label: 'Chuva' })] } },
    { id: 'week', name: `${it.name}: próximos dias`, desc: 'Previsão com ícone, máxima e mínima de cada dia', item: { format: 'list', size: 't', title: 'Previsão', slots: [S1(`${it.id}.forecast`)], list: { max: 0, bars: false } } },
    { id: 'wide', name: `${it.name}: semana na faixa`, desc: 'A previsão dos 7 dias lado a lado', item: { format: 'list', size: 'f', title: 'Previsão', caption: false, slots: [S1(`${it.id}.forecast`)], list: { max: 7, bars: false } } },
    { id: 'details', name: `${it.name}: detalhes`, desc: 'Sensação, umidade, vento e chuva', item: { format: 'grid', title: 'Clima', caption: false, slots: [S1(`${it.id}.feels`, { label: 'Sensação' }), S1(`${it.id}.humidity`, { label: 'Umidade' }), S1(`${it.id}.wind`, { label: 'Vento' }), S1(`${it.id}.rain`, { label: 'Chuva' })] } },
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
  it.clock = { style: 'digital', seconds: false, date: true, tz: '', ...(item?.clock || {}) };
  it.notes = { mode: 'text', ...(item?.notes || {}) };
  if (!FORMATS[it.format]) it.format = 'stat';
  // Tamanho livre (0.13.0); widgets guardados antes têm só a letra
  const old = SIZES[item?.size] || SIZES.s;
  const [mw, mh] = minSize(it);
  it.w = clamp(Math.round(+item?.w || old.c), mw, MAX_W);
  it.h = clamp(Math.round(+item?.h || old.r), mh, MAX_H);
  it.size = sizeKey(it.w, it.h); // só para o desenho; o que vale é w × h
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
// Área útil do widget em px no desenho base (Quadro 240 × 148, vãos de 28 e 18 + rótulo de 26; a Área
// inteira escala com zoom). Serve para o viewBox dos gráficos e para as proporções.
const dims = (it) => [it.w * 240 + (it.w - 1) * 28 - 28, it.h * 148 + (it.h - 1) * 44 - 28];

function cellsOf(n, item, layout) {
  const size = item.size;
  const rowsMode = layout === 'rows', colsMode = layout === 'cols';
  const by = {
    s: rowsMode ? (n <= 3 ? 1 : 2) : colsMode ? Math.min(n, 3) : n <= 3 ? n : n === 4 ? 2 : 3,
    w: rowsMode ? (n <= 2 ? 1 : 2) : n <= 4 ? n : 3,
    t: colsMode ? Math.min(n, 2) : n <= 3 ? 1 : 2,
    l: rowsMode ? (n <= 3 ? 1 : 2) : n <= 2 ? n : n <= 4 ? 2 : 3,
    f: rowsMode ? Math.min(n, 3) : n,
  };
  const cols = Math.max(1, by[size] || n), rows = Math.ceil(n / cols);
  const [w, h] = dims(item);
  return { cols, rows, row: (w / cols) / (h / rows) > 1.9 };
}

// Ícone: o escolhido no Espaço, o que vem com o valor (ex.: tempo do Clima) ou o da Métrica
const iconOf = (slot, def, val) => svg(slot.icon || val?.i || def?.icon || 'info', String(slot.icon || val?.i || def?.icon || '').startsWith('wx-') ? 'wx' : '');
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
  const subVal = sub ? ctx.value(sub.metric) : null;
  // ícone grande: bateria, ou o tempo do Clima (vindo do valor principal ou da linha de apoio)
  const wx = val?.i || subVal?.i;
  const glyph = val?.glyph ? batteryGlyph(val.glyph, 'wf-glyph') : wx ? svg(wx, 'wf-glyph wx') : '';
  let subTxt = '';
  if (sub) {
    const sv = subVal, sd = ctx.def(sub.metric);
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
  const { cols, row } = cellsOf(n, item, item.layout);
  return `${item.caption ? `<span class="w-caption wf-cap">${esc(item.title)}</span>` : ''}<span class="wf-grid ${row ? 'rows' : 'pills'}" style="--cols:${cols}">${item.slots.map((s) => {
    const def = ctx.def(s.metric), val = ctx.value(s.metric), f = fmtVal(val, def, s);
    const sp = s.spark ? `<span class="wf-cspark">${sparkFor(s, ctx, item.size, 100, 18)}</span>` : '';
    return row
      ? `<span class="cell wf-cell"><span class="k">${iconOf(s, def, val)} ${esc(labelOf(s, def))}</span>${sp}<span class="v${lv(levelOf(val, def, s))}">${esc(f.v + f.u)}</span></span>`
      : `<span class="cell wf-cell">${iconOf(s, def, val)}<span class="k">${esc(labelOf(s, def))}</span><span class="v${lv(levelOf(val, def, s))}">${esc(f.v + f.u)}</span>${sp}</span>`;
  }).join('')}</span>`;
}

function listHTML(item, ctx) {
  const first = item.slots[0];
  if (!first) return `<span class="w-empty">Escolha uma lista ou métricas</span>`;
  const def0 = ctx.def(first.metric);
  const cap = item.caption ? `<span class="w-caption wf-cap">${esc(item.title)}</span>` : '';
  if (def0?.kind === 'list') {
    const per = item.h <= 1 ? 3 : 4 * item.h; // cada linha a mais mostra mais itens
    const max = item.list?.max > 0 ? Math.min(item.list.max, per) : per;
    let items = (ctx.value(first.metric)?.l || []).slice(0, max);
    const top = Math.max(1, ...items.map((x) => x.n || 0));
    // itens com ícone próprio (previsão do Clima): lado a lado na Faixa/Largo, um por linha no resto
    const all = ctx.value(first.metric)?.l || [];
    if (all.some((x) => x.i)) {
      const wide = item.h <= 1 && item.w >= 2;
      const cap7 = item.h <= 1 ? Math.min(7, item.w <= 1 ? 3 : item.w * 2 + 1) : 7;
      items = all.slice(0, item.list?.max > 0 ? Math.min(item.list.max, cap7) : cap7);
      return `${cap}<span class="wf-list wf-wxlist ${wide ? 'days' : ''}">${items.map((x) => wide
        ? (() => { const [temps, rain] = (x.v || '').split(' · '), [mx, mn] = (temps || '').split(' / ');
          return `<span class="wf-day"><small>${esc(x.k)}</small>${svg(x.i || 'wx-cloud', 'wx')}<b>${esc(mx || '')}</b>${mn ? `<small class="wf-min">${esc(mn)}</small>` : ''}${rain ? `<small class="wf-rain">${esc(rain)}</small>` : ''}</span>`; })()
        : `<span class="wf-li wf-wxrow">${svg(x.i || 'wx-cloud', 'wx')}<span class="wf-lk">${esc(x.k)}</span><span class="wf-lv">${esc(x.v || '')}</span></span>`).join('')}</span>`;
    }
    return `${cap}<span class="wf-list ${item.h <= 1 && item.w >= 2 ? 'cols' : ''}">${items.length ? items.map((x) => x.n == null && x.v ? `<span class="wf-li stack"><span class="wf-lk" title="${esc(x.k)}">${esc(x.k)}</span><small class="wf-lsub" title="${esc(x.v)}">${esc(x.v)}</small></span>` : `<span class="wf-li"><span class="wf-lk" title="${esc(x.k)}">${esc(x.k)}</span><span class="wf-lv">${esc(x.n != null ? fmtText({ n: x.n }, { unit: first.unit ? '' : def0.unit }, first) : x.v || '')}</span>${item.list?.bars !== false && x.n != null ? `<span class="wf-lbar"><i style="width:${((x.n / top) * 100).toFixed(1)}%"></i></span>` : ''}</span>`).join('') : '<span class="w-empty">Sem itens ainda</span>'}</span>`;
  }
  return `${cap}<span class="wf-list">${item.slots.map((s) => {
    const def = ctx.def(s.metric), val = ctx.value(s.metric);
    return `<span class="wf-li rowv"><span class="wf-lk">${iconOf(s, def, val)} ${esc(labelOf(s, def))}</span>${s.spark ? `<span class="wf-cspark">${sparkFor(s, ctx, item.size, 100, 18)}</span>` : ''}<span class="wf-lv${lv(levelOf(val, def, s))}">${esc(fmtText(val, def, s))}</span></span>`;
  }).join('')}</span>`;
}

function chartHTML(item, ctx) {
  initChartTips();
  if (!item.slots.length) return `<span class="w-empty">Escolha o que vai no gráfico</span>`;
  const [W, H] = dims(item);
  const type = item.chart?.type || 'line';
  const head0 = item.slots[0], d0 = ctx.def(head0.metric), v0 = ctx.value(head0.metric);
  const cap = `<span class="wf-chead"><span class="w-caption">${esc(item.title || labelOf(head0, d0))}</span>`;
  if (type === 'donut') {
    const parts = item.slots.map((s, i) => ({ v: ctx.value(s.metric)?.n ?? 0, cls: `c${i}` }));
    const one = item.slots.length === 1;
    const total = one ? (v0?.max ?? head0.max ?? (d0?.unit === '%' ? 100 : null)) : null;
    const f = fmtVal(v0, d0, head0);
    // rosca: cresce com o lado menor (84 no Pequeno, 92 nos baixos, até ocupar bem os altos)
    const sz = item.h <= 1 ? (item.w <= 1 ? 84 : 92) : Math.round(Math.min(W * 0.62, H - 40, 300));
    const legend = item.slots.map((s, i) => { const d = ctx.def(s.metric); return `<span class="wf-leg"><i class="c${i}"></i>${esc(labelOf(s, d))} <b>${esc(fmtText(ctx.value(s.metric), d, s))}</b></span>`; }).join('');
    return `${cap}</span><span class="wf-donut ${item.size}"><span class="wf-ring" style="--sz:${sz}px">${donutSVG(parts, { total: one ? total ?? undefined : undefined, size: 100, thick: 11 })}<span class="wf-ringv"><b class="${lv(levelOf(v0, d0, head0)).trim()}">${esc(f.v)}<small>${esc(f.u)}</small></b>${one && head0.label ? `<small>${esc(head0.label)}</small>` : ''}</span></span>${one ? '' : `<span class="wf-legend">${legend}</span>`}</span>`;
  }
  const series = item.slots.map((s, i) => ({ pts: ctx.series(s.metric, item.chart?.range || '24h') || [], cls: `c${i}` }));
  const has = series.some((s) => s.pts.filter((p) => p.v != null).length >= 2);
  const headVal = d0?.kind === 'num' && v0 ? `<span class="wf-cval">${esc(fmtText(v0, d0, head0))}</span>` : '';
  const legend = item.slots.length > 1 ? `<span class="wf-legend inline">${item.slots.map((s, i) => `<span class="wf-leg"><i class="c${i}"></i>${esc(labelOf(s, ctx.def(s.metric)))}</span>`).join('')}</span>` : '';
  const chH = Math.round(H * (item.chart?.stats ? 0.62 : 0.72));
  // valor ao passar o mouse (0.14.0) e Ponta viva só no "5 min" (Período ao vivo)
  const fmtOf = (s) => (v) => fmtText({ n: v }, ctx.def(s.metric)?.kind === 'series' ? { unit: ctx.def(s.metric).unit } : ctx.def(s.metric), s);
  const tip = { fmts: item.slots.map(fmtOf), names: item.slots.length > 1 ? item.slots.map((s) => labelOf(s, ctx.def(s.metric))) : [] };
  const liveTxt = (item.chart?.range || '24h') === 'live' && has ? (() => { const l = [...series[0].pts].reverse().find((p) => p.v != null); return l ? fmtOf(head0)(l.v) : null; })() : null;
  const svgTxt = !has ? '' : type === 'bar' ? barSVG(series, { w: W, h: chH, tip }) : lineSVG(series, { w: W, h: chH, area: series.length === 1, maxGap: ctx.maxGap?.(head0.metric), tip, live: liveTxt });
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
  // pastas por linha e linhas pelo Tamanho: cada Quadro a mais na largura/altura mostra mais pastas
  const cols = list ? (item.w <= 1 ? 1 : Math.min(item.w, 4)) : Math.min(item.w * 2, 8);
  const rows = list ? (item.h <= 1 ? 3 : item.h * 4 - 1) : item.h * 2;
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

// Relógio: a hora vem do navegador (nada do servidor). Com segundos, a tela inicial redesenha a cada 1 s.
function clockNow(tz) {
  const d = new Date(), o = tz ? { timeZone: tz } : {};
  try {
    const p = Object.fromEntries(new Intl.DateTimeFormat('pt-BR', { ...o, hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).formatToParts(d).map((x) => [x.type, x.value]));
    return { h: +p.hour, m: +p.minute, s: +p.second, hh: p.hour, mm: p.minute, ss: p.second,
      date: d.toLocaleDateString('pt-BR', { ...o, weekday: 'long', day: 'numeric', month: 'long' }),
      short: d.toLocaleDateString('pt-BR', { ...o, weekday: 'short', day: 'numeric', month: 'short' }) };
  } catch { return clockNow(''); } // fuso inválido: usa o do aparelho
}
export const clockTicks = (it) => it.format === 'clock' && it.clock?.seconds;
function clockHTML(item) {
  const c = item.clock || {}, t = clockNow(c.tz);
  const tzName = c.tz ? (TIMEZONES.find(([z]) => z === c.tz)?.[1] || c.tz.split('/').pop().replace(/_/g, ' ')) : '';
  const cap = item.caption ? `<span class="w-caption">${esc(item.title)}</span>` : '';
  // ponteiros no Pequeno/Largo: data curta ao lado ("ter., 10 de nov.")
  const short = c.style === 'analog' && item.h <= 1;
  const date = c.date !== false ? `<span class="wf-cdate">${esc(short ? t.short : t.date)}</span>` : '';
  const tz = tzName ? `<span class="wf-ctz">${svg('globe')}${esc(tzName)}</span>` : '';
  if (c.style === 'analog') {
    const hand = (deg, len, w, cls) => `<line class="${cls}" x1="50" y1="50" x2="50" y2="${50 - len}" stroke-width="${w}" transform="rotate(${deg.toFixed(2)} 50 50)"/>`;
    const ticks = Array.from({ length: 12 }, (_, i) => `<line x1="50" y1="${i % 3 ? 9 : 7.5}" x2="50" y2="${i % 3 ? 12.5 : 15}" transform="rotate(${i * 30} 50 50)"/>`).join('');
    return `${cap}<span class="wf-clock analog wsz-${item.size}"><svg viewBox="0 0 100 100" aria-label="${t.hh}:${t.mm}"><circle class="face" cx="50" cy="50" r="46"/><g class="ticks">${ticks}</g>
      ${hand((t.h % 12) * 30 + t.m * 0.5, 24, 5, 'hh')}${hand(t.m * 6 + t.s * 0.1, 34, 3.4, 'mh')}${c.seconds ? hand(t.s * 6, 37, 1.4, 'sh') : ''}<circle class="pin" cx="50" cy="50" r="3.2"/></svg>
      ${date || tz ? `<span class="wf-cinfo">${date}${tz}</span>` : ''}</span>`;
  }
  return `${cap}<span class="wf-clock digital wsz-${item.size}"><b>${t.hh}<i>:</i>${t.mm}${c.seconds ? `<small>${t.ss}</small>` : ''}</b>${date}${tz}</span>`;
}

// Notas: o texto é montado e editado pela tela inicial (notes.js); aqui só o lugar, que não muda
// (redesenhar apagaria o que está sendo digitado). Na prévia do editor, um exemplo.
function notesHTML(item, ctx, id) {
  const mode = item.notes?.mode === 'list' ? 'list' : 'text';
  const cap = item.caption ? `<span class="w-caption wf-cap">${esc(item.title)}</span>` : '';
  if (!id) {
    return `${cap}<span class="wf-notes preview ${mode}">${mode === 'list'
      ? ['Trocar o filtro da ventoinha', 'Atualizar o Immich', 'Backup dos HDs'].map((x, i) => `<span class="wn-item ${i === 0 ? 'done' : ''}"><i class="wn-box">${i === 0 ? svg('check') : ''}</i><span>${x}</span></span>`).join('')
      : '<span class="wn-text">Senha do Wi-Fi das visitas na geladeira.\nLigar para o provedor na segunda.</span>'}</span>`;
  }
  return `${cap}<span class="wf-notes ${mode}" data-note="${esc(id)}" data-mode="${mode}"></span>`;
}

const RENDER = { stat: statHTML, grid: gridHTML, list: listHTML, chart: chartHTML, media: mediaHTML, shortcuts: shortcutsHTML, clock: clockHTML, notes: notesHTML };

/** HTML de dentro do widget (o que muda quando chegam valores novos). id: o do widget (Notas). */
export function innerHTML(item, ctx, id = '') {
  return (RENDER[item.format] || statHTML)(item, ctx, id) + errBadge(item, ctx);
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

/** Lugar na grade da Área (0.13.0): coluna e linha (a partir de 0) e o Tamanho em Quadros. */
export const posStyle = (p) => (p ? `grid-column:${p.x + 1} / span ${p.w};grid-row:${p.y + 1} / span ${p.h};` : '');

/**
 * O widget inteiro (moldura + rótulo). Formato Atalhos não é botão (as pastas são). pos = o lugar na
 * Área ({ x, y, w, h }); sem pos (prévia do editor) ele ocupa o espaço que lhe derem. O arrastar é
 * feito pela tela inicial (pointer events); a alça de tamanho e o "−" aparecem só no modo Organizar.
 */
export function widgetHTML(item, ctx, { id = '', i = 0, editing = false, pos = null } = {}) {
  // Atalhos e Notas não são botão (as pastas e o texto são); Notas só arrasta no modo Organizar,
  // senão selecionar o texto com o mouse arrastaria o widget
  const tag = item.format === 'shortcuts' || item.format === 'notes' ? 'div' : 'button';
  const drag = id && (item.format !== 'notes' || editing);
  const own = item.look?.follow === false;
  const hideLabel = own && item.look?.hideLabel;
  return `<div class="widget-wrap wsz-${item.size}" data-wid="${esc(id)}" data-w="${item.w}" data-h="${item.h}" ${drag ? 'data-drag="1"' : ''} style="--i:${i};${posStyle(pos)}">
    <${tag} class="widget wf wfm-${item.format} ${own ? 'w-own' : ''} ${own && item.look?.hue != null ? 'w-hue' : ''}" style="${lookStyle(item)}" ${tag === 'button' ? `type="button" aria-label="${esc(item.title || FORMATS[item.format].label)}"` : ''}>${innerHTML(item, ctx, id)}</${tag}>
    <div class="widget-label ${hideLabel ? 'is-hidden' : ''}">${esc(item.title || '')}</div>
    ${editing ? `<button class="w-remove" data-wremove="${esc(id)}" aria-label="Remover widget">${svg('minus')}</button><button class="w-more" data-wmore="${esc(id)}" aria-label="Mais opções do widget">${svg('more')}</button><span class="w-resize" data-wresize="${esc(id)}" title="Arraste para mudar o tamanho" aria-hidden="true"></span>` : ''}
  </div>`;
}

/** Para onde o clique vai quando o Bruno deixou "automático". */
export function autoClick(item) {
  if (item.format === 'shortcuts' || item.format === 'notes' || item.format === 'clock') return { kind: 'none' };
  const m = item.slots[0]?.metric || '';
  if (m.startsWith('brunos.')) {
    if (m.startsWith('brunos.battery')) return { kind: 'window', target: 'battery' };
    if (m.startsWith('brunos.net_')) return { kind: 'window', target: 'internet' };
    if (m.startsWith('brunos.apps') || m.startsWith('brunos.os_')) return { kind: 'none' };
    if (/^brunos\.(power_w|energy_|cost_)/.test(m)) return { kind: 'window', target: 'power' };
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
