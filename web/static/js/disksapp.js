// Janela Discos (0.15.0): lista dos discos à esquerda e, à direita, o disco escolhido em cinco abas
// (Visão geral, Saúde, Desempenho, Espaço, Linha do tempo). Tudo só lê, menos três ações seguras:
// o teste do próprio disco (SMART), o teste de velocidade (só lê) e ejetar um disco USB desmontado.
import { api } from './api.js';
import { $, $$, esc, fmt, fmtBytes, fmtRate, svg, toast, toastError, confirmDialog, timeAgo } from './ui.js';
import { mountChart, periodSeg, PERIOD_LONG, fmtTime } from './charts.js';
import { forecastText, driveTempHelp } from './disks.js';

const TABS = [['overview', 'Visão geral'], ['health', 'Saúde'], ['perf', 'Desempenho'], ['space', 'Espaço'], ['timeline', 'Linha do tempo']];
const HEALTH = { ok: ['OK', 'ok', 'O disco não relata problemas.'],
  atencao: ['Atenção', 'warn', 'Apareceram setores com problema. Não é falha imediata, mas é o sinal clássico de que o disco começou a se desgastar: confira o backup do que está nele.'],
  falhando: ['Falhando', 'bad', 'O próprio disco avisa que está falhando. Copie o que está nele para outro lugar o quanto antes.'] };
const lsGet = (k) => { try { return localStorage.getItem(k); } catch { return null; } };
const lsSet = (k, v) => { try { localStorage.setItem(k, v); } catch { /* sem storage */ } };
const fmtN = (n) => (n == null ? '—' : Number(n).toLocaleString('pt-BR'));
const when = (t) => (t ? new Date(t * 1000).toLocaleString('pt-BR', { day: '2-digit', month: '2-digit', year: '2-digit', hour: '2-digit', minute: '2-digit' }) : '—');

let D = null; // estado da janela aberta

/* ── nomes e rótulos ─────────────────────────────────────────── */
const mountsOf = (d) => d.parts.flatMap((p) => p.mounts || []);
const isMounted = (d) => mountsOf(d).length > 0;
export function diskName(d) {
  const labeled = d.parts.find((p) => p.label && (p.mounts || []).length) || d.parts.find((p) => p.label);
  if (labeled) return labeled.label;
  const mnt = mountsOf(d).find((m) => /^\/(mnt|media)\//.test(m));
  if (mnt) return mnt.split('/').pop();
  if (d.system) return 'Disco do sistema';
  return d.model || d.dev;
}
const kindLabel = (d) => (d.tran === 'nvme' ? 'SSD NVMe' : `${d.rotational ? 'HD' : 'SSD'}${d.tran === 'usb' ? ' USB' : d.tran === 'sata' ? ' SATA' : ''}`);
const TRAN = { usb: 'USB', sata: 'SATA (interno)', nvme: 'NVMe (interno)', ata: 'IDE' };
const iconOf = (d) => (d.system ? 'server' : d.tran === 'usb' ? 'usb' : 'disk');
function usedOf(d) {
  let used = 0, total = 0;
  for (const p of d.parts) if (p.used != null && p.avail != null) { used += p.used; total += p.used + p.avail; }
  return total ? { used, total } : null;
}
function healthBadge(d, big = false) {
  const s = d.smart;
  if (s?.noSmart) return `<span class="smart-badge bad ${big ? 'big' : ''}">sem SMART</span>`;
  const h = s && HEALTH[s.health];
  return h ? `<span class="smart-badge ${h[1]} ${big ? 'big' : ''}">${h[0]}</span>` : '';
}
function tempChip(d) {
  if (d.temp == null) return '';
  const hot = d.rotational ? 55 : 70, cls = d.temp >= hot ? 'hot' : d.temp >= hot - 5 ? 'warm' : '';
  const old = Date.now() / 1000 - d.tempAt > 1800;
  return `<span class="disk-temp ${cls} ${old ? 'old' : ''}" title="${esc(old ? `Medida ${timeAgo(d.tempAt)}: o disco está parado e o BrunOS não o acorda só para medir` : `Temperatura (alerta a partir de ${hot} °C)`)}">${Math.round(d.temp)}°</span>`;
}

/* ── montagem da janela ──────────────────────────────────────── */
export function mountDisks(body, opts = {}) {
  D = { body, ctx: opts.ctx || {}, list: [], meta: {}, smart: null, tab: TABS.some(([k]) => k === opts.tab) ? opts.tab : (lsGet('brunos.disksTab') || 'overview'),
    sel: null, want: { key: opts.key, dev: opts.dev }, period: 'live', charts: [], live: {}, paneKey: '', timer: 0, detail: !!(opts.key || opts.dev) };
  if (!TABS.some(([k]) => k === D.tab)) D.tab = 'overview';
  body.innerHTML = `<div class="dk ${D.detail ? 'show-detail' : ''}">
    <aside class="dk-side"><div class="dk-side-head"><h1 class="sheet-title">Discos</h1>
      <button class="icon-btn" data-act="refresh" title="Ler a lista de discos de novo" aria-label="Atualizar">${svg('refresh')}</button></div>
      <div class="dk-list"><div class="sheet-loading"></div></div><div class="dk-foot"></div></aside>
    <section class="dk-main"><div class="sheet-loading"></div></section></div>`;
  const root = $('.dk', body);
  root.addEventListener('click', onClick);
  const offStats = D.ctx.onStats?.(onStats);
  load();
  return () => { offStats?.(); clearTimeout(D?.timer); D?.charts.forEach((c) => c.destroy?.()); D = null; };
}

/** Janela já aberta: escolhe outro disco/aba (Uso ao vivo → "Abrir Discos" de um disco). */
export function focusDisk(opts = {}) {
  if (!D) return;
  if (opts.tab) D.tab = opts.tab;
  const d = D.list.find((x) => (opts.key && x.key === opts.key) || (opts.dev && x.dev === opts.dev));
  if (d) select(d.key, true);
}

async function load(force = false) {
  if (!D) return;
  clearTimeout(D.timer);
  try {
    const [r, s] = await Promise.all([force ? api.disks.refresh() : api.disks.list(), api.smart.get().catch(() => null)]);
    if (!D) return;
    D.list = r.disks || []; D.meta = r; D.smart = s; D.err = '';
  } catch (e) { if (!D) return; D.err = e.message || String(e); }
  if (!D.sel || !D.list.some((d) => d.key === D.sel)) {
    const w = D.want, saved = lsGet('brunos.disksSel');
    const pick = D.list.find((d) => (w.key && d.key === w.key) || (w.dev && d.dev === w.dev)) || D.list.find((d) => d.key === saved) || D.list[0];
    D.sel = pick?.key || null;
  }
  renderSide();
  paintMain();
  // acompanha mais de perto o que está andando (lista lendo, teste do disco, velocidade, SMART lendo)
  const busy = D.meta.loading || D.smart?.running || D.list.some((d) => d.speedBusy || d.smart?.test);
  D.timer = setTimeout(() => load(), busy ? 3000 : 20000);
}

const cur = () => D?.list.find((d) => d.key === D.sel);

function select(key, detail = true) {
  D.sel = key; lsSet('brunos.disksSel', key);
  D.detail = detail; $('.dk', D.body)?.classList.toggle('show-detail', detail);
  renderSide(); paintMain(true);
}

/* ── lista ───────────────────────────────────────────────────── */
function itemHTML(d) {
  const u = usedOf(d);
  return `<button class="dk-item ${d.key === D.sel ? 'on' : ''}" data-key="${esc(d.key)}">
    <span class="dk-ico ${d.tran === 'usb' ? 'usb' : ''}">${svg(iconOf(d))}</span>
    <span class="dk-it-txt"><span class="dk-it-name">${esc(diskName(d))}</span>
      <small>${fmtBytes(d.size)} · ${esc(kindLabel(d))}</small>
      ${u ? `<span class="f-mini-track"><i style="width:${Math.min(100, (u.used / u.total) * 100)}%" class="${u.used / u.total > 0.9 ? 'warn' : ''}"></i></span>` : ''}</span>
    <span class="dk-it-badges">${tempChip(d)}${healthBadge(d)}</span></button>`;
}
function renderSide() {
  if (!D) return;
  const box = $('.dk-list', D.body);
  if (!D.list.length) {
    box.innerHTML = D.meta.loading ? '<div class="empty-note">Lendo os discos…</div>' : '';
  } else {
    const main = D.list.filter((d) => d.system || isMounted(d)).sort((a, b) => (b.system ? 1 : 0) - (a.system ? 1 : 0));
    const loose = D.list.filter((d) => !d.system && !isMounted(d));
    box.innerHTML = main.map(itemHTML).join('') + (loose.length ? `<div class="f-sec">Sem pasta de montagem</div>${loose.map(itemHTML).join('')}` : '');
  }
  const m = D.meta;
  $('.dk-foot', D.body).innerHTML = D.err ? `<p class="dk-note bad">${esc(D.err)}</p>`
    : m.err ? `<p class="dk-note bad">${esc(m.err)}</p>${/script do servidor/.test(m.err) ? `<button class="pill-btn" data-act="script">Mostrar comandos</button>` : ''}`
    : m.checkedAt ? `<p class="dk-note">${m.loading ? 'Lendo…' : `Lida ${esc(timeAgo(m.checkedAt))}`}</p>` : '';
}

/* ── detalhe ─────────────────────────────────────────────────── */
function paintMain(force = false) {
  if (!D) return;
  const main = $('.dk-main', D.body), d = cur();
  if (!d) {
    D.paneKey = '';
    main.innerHTML = D.meta.loading || (!D.meta.checkedAt && !D.err && !D.meta.err) ? '<div class="sheet-loading"></div>'
      : `<div class="empty-note big">${svg('disks')}<b>Nenhum disco para mostrar</b><span>${esc(D.meta.err || D.err || 'O servidor não mandou a lista de discos.')}</span>
        ${/script do servidor/.test(D.meta.err || '') ? `<button class="pill-btn" data-act="script">Mostrar comandos</button>` : ''}</div>`;
    return;
  }
  // só refaz o que mudou (o gráfico e a rolagem não pulam a cada 20 s)
  const head = headHTML(d);
  if (force || !$('.dk-head', main) || main.dataset.key !== d.key) {
    main.dataset.key = d.key;
    main.innerHTML = `<div class="dk-head-wrap">${head}</div><div class="dk-pane"></div>`;
    D.paneKey = '';
  } else if ($('.dk-head-wrap', main).innerHTML !== head) $('.dk-head-wrap', main).innerHTML = head;
  paintPane();
}

function headHTML(d) {
  const usb = d.tran === 'usb' && !d.system, mounted = isMounted(d);
  const eject = usb ? `<button class="pill-btn" data-act="eject" ${mounted ? `disabled title="${esc(`Desmonte antes de ejetar (o disco está em ${mountsOf(d).join(', ')})`)}"` : 'title="Desliga o disco com segurança para tirar o cabo"'}>${svg('eject')}Ejetar com segurança</button>` : '';
  return `<button class="dk-back" data-act="back">${svg('chevL')}Discos</button>
    <div class="dk-hero"><span class="dk-ico big ${d.tran === 'usb' ? 'usb' : ''}">${svg(iconOf(d))}</span>
      <div class="dk-hero-txt"><h2>${esc(diskName(d))} ${healthBadge(d)} ${tempChip(d)}</h2>
      <p>${esc([d.model, fmtBytes(d.size), kindLabel(d), '/dev/' + d.dev].filter(Boolean).join(' · '))}</p></div>
      <div class="dk-head-act">${eject}</div></div>
    <div class="tabs dk-tabs" role="tablist">${TABS.map(([k, l]) => `<button role="tab" class="${D.tab === k ? 'on' : ''}" data-tab="${k}">${l}</button>`).join('')}</div>`;
}

function paintPane(force = false) {
  const d = cur(), pane = $('.dk-pane', D.body);
  if (!d || !pane) return;
  if (D.tab === 'perf') { // gráficos: monta uma vez; nas leituras seguintes só o teste de velocidade muda
    if (D.paneKey !== `perf:${d.key}` || force) { D.paneKey = `perf:${d.key}`; renderPerf(d, pane); } else paintSpeed(d);
    return;
  }
  const key = `${D.tab}:${JSON.stringify(d)}:${JSON.stringify(D.smart?.running)}:${D.smart?.err || ''}`;
  if (key === D.paneKey && !force) return;
  D.paneKey = key;
  D.charts.forEach((c) => c.destroy?.()); D.charts = [];
  const scroll = pane.scrollTop;
  if (D.tab === 'overview') renderOverview(d, pane);
  else if (D.tab === 'health') renderHealth(d, pane);
  else if (D.tab === 'space') renderSpace(d, pane);
  else if (D.tab === 'timeline') renderTimeline(d, pane);
  pane.scrollTop = scroll;
}

/* ── Visão geral ─────────────────────────────────────────────── */
const PT = { gpt: 'GPT', dos: 'MBR (antiga)' };
const kv = (k, v, extra = '') => `<div class="kv ${extra}"><span>${k}</span><span>${v}</span></div>`;
const COLORS = ['c0', 'c1', 'c2', 'c3', 'c4', 'c5'];
function renderOverview(d, pane) {
  const parts = d.parts.filter((p) => p.size > 0);
  const total = d.size || parts.reduce((s, p) => s + p.size, 0) || 1;
  const bar = parts.map((p, i) => `<i class="${COLORS[i % COLORS.length]}" style="flex:${Math.max(p.size / total, 0.012)}" title="${esc(`${p.name} · ${fmtBytes(p.size)}`)}"></i>`).join('');
  const free = total - parts.reduce((s, p) => s + p.size, 0);
  const rows = parts.map((p, i) => {
    const mnt = (p.mounts || []).map((m) => (m === '[SWAP]' ? 'memória de troca (swap)' : m)).join(', ');
    const where = mnt ? `montada em <b>${esc(mnt)}</b>` : p.holder ? `usada por baixo (${esc(p.holder)})` : '<span class="dk-dim">sem pasta de montagem</span>';
    const fst = p.fstab && !(p.mounts || []).includes(p.fstab) ? `<small class="dk-warn-t">O /etc/fstab manda montar em ${esc(p.fstab)}, mas não está montada</small>` : p.fstab ? '<small>monta sozinha ao ligar (fstab)</small>' : mnt && !p.whole ? '<small class="dk-dim">não está no /etc/fstab: não volta sozinha ao reiniciar</small>' : '';
    const use = p.used != null && p.avail != null ? `${fmtBytes(p.used)} <small>de ${fmtBytes(p.used + p.avail)}</small>` : fmtBytes(p.size);
    return `<div class="dk-part"><i class="dk-swatch ${COLORS[i % COLORS.length]}"></i>
      <span class="dk-part-txt"><b>${esc(p.label || p.name)}</b> <small>${esc([p.whole ? 'disco inteiro' : p.name, p.fsType || 'sem sistema de arquivos'].filter(Boolean).join(' · '))}</small>
      <small>${where}</small>${fst}</span><span class="dk-part-val">${use}</span></div>`;
  }).join('');
  const apps = d.apps.length ? `<div class="dk-apps">${d.apps.map((a) => `<button class="chip on" data-app="${esc(a.urn || '')}" title="${esc(a.folders.join('\n'))}">${D.ctx.appIcon ? D.ctx.appIcon(a.urn) : ''}${esc(a.name)}</button>`).join('')}</div>`
    : `<p class="dk-small">${d.system ?'É o disco do sistema: o Linux, o Docker, o Runtipi e os dados dos apps ficam nele.' : 'Nenhum app tem pasta deste disco montada.'}</p>`;
  pane.innerHTML = `<div class="dk-cols">
    <div class="card">${kv('Modelo', esc(d.model || '—'))}${d.vendor ? kv('Fabricante', esc(d.vendor)) : ''}${kv('Número de série', esc(d.serial || '—'))}${d.rev ? kv('Firmware', esc(d.rev)) : ''}
      ${kv('Conexão', esc(TRAN[d.tran] || d.tran || '—'))}${kv('Tipo', d.tran === 'nvme' ? 'SSD NVMe' : d.rotational ? 'HD (disco que gira)' : 'SSD')}${kv('Tamanho', fmtBytes(d.size))}
      ${d.ptType ? kv('Tabela de partições', esc(PT[d.ptType] || d.ptType)) : ''}${d.wwn ? kv('WWN', esc(d.wwn)) : ''}</div>
    <div class="card">${kv('Agora', `<span class="dk-state" data-state>${svg('clock')}conferindo…</span>`)}
      ${kv('Caminho atual', `/dev/${esc(d.dev)}`)}<p class="dk-small">O nome /dev/${esc(d.dev)} pode mudar quando o servidor reinicia ou o cabo troca de porta.</p>
      ${d.byId ? `${kv('Caminho fixo', `<button class="dk-copy" data-copy="${esc(d.byId)}" title="Copiar">${svg('copy')}</button>`)}<code class="dk-code">${esc(d.byId)}</code>` : ''}
      ${d.first ? kv('Visto pela primeira vez', esc(when(d.first))) : ''}</div></div>
    <div class="live-sec">Partições</div>
    <div class="card"><div class="dk-bar">${bar}${free > total * 0.01 ? `<i class="free" style="flex:${free / total}" title="Espaço sem partição: ${fmtBytes(free)}"></i>` : ''}</div>
      ${rows || '<p class="dk-dim">Este disco não tem partições.</p>'}${free > total * 0.01 ? `<div class="dk-part"><i class="dk-swatch free"></i><span class="dk-part-txt"><b>Espaço sem partição</b></span><span class="dk-part-val">${fmtBytes(free)}</span></div>` : ''}</div>
    <div class="live-sec">Apps que usam este disco</div>${apps}`;
  loadState(d);
}
async function loadState(d) {
  const el = () => $('[data-state]', D?.body);
  if (!el()) return;
  try {
    const r = await api.disks.state(d.dev);
    const txt = { dormindo: [svg('clock'), 'dormindo (parado)', 'ok'], acordado: [svg('activity'), 'acordado', 'on'], desconhecido: [svg('info'), 'não dá para saber', ''] }[r.state] || [svg('info'), r.state, ''];
    if (el()) el().outerHTML = `<span class="dk-state ${txt[2]}" data-state title="${esc(r.why || 'O BrunOS pergunta sem acordar o disco')}">${txt[0]}${txt[1]}</span>`;
  } catch (e) { if (el()) el().outerHTML = `<span class="dk-state" data-state title="${esc(e.message)}">${svg('info')}não deu para conferir</span>`; }
}

/* ── Saúde ───────────────────────────────────────────────────── */
// Atributos ATA: nome em pt-BR, explicação e a regra de "normal / preocupante" (pelo valor cru).
const hours = (h) => `${fmtN(h)} h (${(h / 8766).toLocaleString('pt-BR', { maximumFractionDigits: 1 })} anos)`;
const lba = (n) => fmtBytes(n * 512);
const ATA = {
  1: ['Erros de leitura (taxa)', 'Cada fabricante conta de um jeito; vale o valor normalizado contra o limite.'],
  3: ['Tempo para começar a girar', 'Quanto o motor leva para chegar na velocidade.'],
  4: ['Vezes que parou e voltou a girar', 'Normal subir quando o disco dorme e acorda.'],
  5: ['Setores realocados', 'Pedaços do disco que estragaram e foram trocados por reservas. Qualquer número acima de zero merece atenção; subindo, é sinal de desgaste.', (v) => (v > 100 ? 'bad' : v > 0 ? 'warn' : 'ok')],
  7: ['Erros ao posicionar a cabeça (taxa)', 'Como o 1: cada fabricante conta de um jeito.'],
  9: ['Horas ligado', 'Tempo total de uso do disco.', null, hours],
  10: ['Tentativas de girar', 'O motor precisou tentar de novo para girar. Acima de zero pode ser fonte fraca ou motor cansado.', (v) => (v > 0 ? 'warn' : 'ok')],
  12: ['Vezes ligado', 'Quantas vezes o disco foi ligado.'],
  170: ['Reserva disponível (SSD)', 'Blocos reserva do SSD para trocar os que gastarem.'],
  171: ['Falhas ao gravar (SSD)', 'O SSD não conseguiu gravar um bloco.', (v) => (v > 0 ? 'warn' : 'ok')],
  172: ['Falhas ao apagar (SSD)', 'O SSD não conseguiu apagar um bloco.', (v) => (v > 0 ? 'warn' : 'ok')],
  173: ['Desgaste por regravação (SSD)', 'Média de vezes que cada bloco foi regravado.'],
  174: ['Desligamentos sem aviso', 'Quedas de energia ou cabo puxado com o SSD ligado.'],
  177: ['Vida útil (SSD)', 'O valor normalizado é a vida que resta (100 = novo).', null, null, (a) => (a.value && a.value <= 10 ? 'bad' : a.value && a.value <= 30 ? 'warn' : 'ok')],
  181: ['Falhas ao gravar (SSD, total)', 'Blocos que o SSD não conseguiu gravar.', (v) => (v > 0 ? 'warn' : 'ok')],
  182: ['Falhas ao apagar (SSD, total)', 'Blocos que o SSD não conseguiu apagar.', (v) => (v > 0 ? 'warn' : 'ok')],
  183: ['Blocos ruins em uso', 'Erros encontrados durante o uso.', (v) => (v > 0 ? 'warn' : 'ok')],
  184: ['Erros no caminho dos dados', 'O dado chegou diferente do que saiu dentro do próprio disco.', (v) => (v > 0 ? 'bad' : 'ok')],
  187: ['Erros que o disco não corrigiu', 'Leituras que falharam mesmo com a correção do disco. Acima de zero: confira o backup.', (v) => (v > 0 ? 'warn' : 'ok')],
  188: ['Comandos que demoraram demais', 'Costuma ser cabo, adaptador USB ou fonte, não o disco.', (v) => (v > 50 ? 'warn' : 'ok')],
  189: ['Gravações com a cabeça alta', 'A cabeça ficou mais longe do prato do que devia (vibração).'],
  190: ['Temperatura (fluxo de ar)', 'Temperatura medida pelo disco.', null, (v) => `${v & 0xff} °C`],
  191: ['Pancadas e vibração', 'Quantas vezes o disco sentiu um tranco.'],
  192: ['Desligamentos bruscos', 'O disco desligou e recolheu a cabeça às pressas (energia cortada).'],
  193: ['Vezes que a cabeça estacionou', 'HDs de notebook estacionam muito para economizar; a maioria aguenta de 300 mil a 600 mil.', (v) => (v > 600000 ? 'warn' : 'ok')],
  194: ['Temperatura', 'Temperatura medida pelo disco.', null, (v) => `${v & 0xff} °C`],
  195: ['Erros corrigidos na hora', 'O disco errou e corrigiu sozinho. Normal ter muitos.'],
  196: ['Trocas de setor (eventos)', 'Quantas vezes o disco trocou um setor ruim por reserva.', (v) => (v > 0 ? 'warn' : 'ok')],
  197: ['Setores pendentes', 'Setores com leitura instável esperando para ser trocados. Acima de zero merece atenção.', (v) => (v > 0 ? 'warn' : 'ok')],
  198: ['Setores incorrigíveis', 'Setores que o disco não consegue ler de jeito nenhum. Acima de zero: confira o backup.', (v) => (v > 0 ? 'bad' : 'ok')],
  199: ['Erros de cabo (CRC)', 'Dados que chegaram errados pelo cabo. Costuma ser cabo ou adaptador USB ruim, não o disco.', (v) => (v > 0 ? 'warn' : 'ok')],
  200: ['Erros de gravação (taxa)', 'Como o 1: cada fabricante conta de um jeito.'],
  202: ['Vida útil restante (SSD)', 'Porcentagem de vida que resta.'],
  231: ['Vida útil restante (SSD)', 'O valor normalizado é a vida que resta (100 = novo).', null, null, (a) => (a.value && a.value <= 10 ? 'bad' : a.value && a.value <= 30 ? 'warn' : 'ok')],
  233: ['Desgaste (SSD)', 'O valor normalizado cai de 100 conforme o SSD gasta.', null, null, (a) => (a.value && a.value <= 10 ? 'bad' : a.value && a.value <= 30 ? 'warn' : 'ok')],
  240: ['Horas com a cabeça em voo', 'Tempo lendo ou gravando.'],
  241: ['Total gravado', 'Tudo o que já foi gravado no disco.', null, lba],
  242: ['Total lido', 'Tudo o que já foi lido do disco.', null, lba],
};
// Campos do registro de saúde do NVMe
const NVME = {
  critical_warning: ['Aviso crítico', 'O próprio SSD avisando de um problema grave. Tem que ser zero.', (v) => (v > 0 ? 'bad' : 'ok')],
  temperature: ['Temperatura', 'Temperatura do SSD.', (v) => (v >= 70 ? 'warn' : 'ok'), (v) => `${v} °C`],
  available_spare: ['Reserva disponível', 'Blocos reserva para trocar os que gastarem (%).', null, (v) => `${v}%`],
  available_spare_threshold: ['Limite da reserva', 'Abaixo disto o SSD avisa que está no fim (%).', null, (v) => `${v}%`],
  percentage_used: ['Desgaste', 'Quanto da vida útil prevista já foi usada (pode passar de 100%).', (v) => (v >= 100 ? 'bad' : v >= 80 ? 'warn' : 'ok'), (v) => `${v}%`],
  data_units_read: ['Total lido', 'Tudo o que já foi lido do SSD.', null, (v) => fmtBytes(v * 512000)],
  data_units_written: ['Total gravado', 'Tudo o que já foi gravado no SSD.', null, (v) => fmtBytes(v * 512000)],
  host_reads: ['Pedidos de leitura', 'Quantos pedidos de leitura o SSD recebeu.'],
  host_writes: ['Pedidos de gravação', 'Quantos pedidos de gravação o SSD recebeu.'],
  controller_busy_time: ['Tempo ocupado', 'Minutos trabalhando.', null, (v) => `${fmtN(v)} min`],
  power_cycles: ['Vezes ligado', 'Quantas vezes o SSD foi ligado.'],
  power_on_hours: ['Horas ligado', 'Tempo total de uso do SSD.', null, hours],
  unsafe_shutdowns: ['Desligamentos sem aviso', 'Quedas de energia ou desligamentos bruscos.'],
  media_errors: ['Erros de mídia', 'Dados que o SSD não conseguiu ler. Tem que ser zero.', (v) => (v > 0 ? 'warn' : 'ok')],
  num_err_log_entries: ['Erros registrados', 'Entradas no registro de erros do SSD. Costuma subir por pedidos que o SSD não conhece; não é defeito sozinho.'],
};
const STATUS = { ok: ['normal', 'ok'], warn: ['preocupante', 'warn'], bad: ['preocupante', 'bad'] };
function attrRow(a) {
  const info = a.key ? NVME[a.key] : ATA[a.id];
  const [name, desc, rule, show, ruleNorm] = info || [a.name.replace(/_/g, ' '), ''];
  let st = rule ? rule(a.raw) : ruleNorm ? ruleNorm(a) : '';
  if (a.failed || (a.thresh > 0 && a.value > 0 && a.value <= a.thresh)) st = 'bad';
  const val = show ? show(a.raw) : a.rawStr || fmtN(a.raw);
  const norm = !a.key && a.value ? `<small title="Valor normalizado (100 ou 200 = novo; ruim abaixo do limite)">${a.value}${a.thresh ? ` · limite ${a.thresh}` : ''}</small>` : '';
  const pill = st ? `<span class="dk-pill ${STATUS[st][1]}">${STATUS[st][0]}</span>` : '<span class="dk-pill">informativo</span>';
  return { st, html: `<div class="dk-attr ${st}"><span class="dk-attr-name"><b>${esc(name)}</b>${a.id ? `<small class="dk-dim">#${a.id}</small>` : ''}${desc ? `<small>${esc(desc)}</small>` : ''}</span>
    <span class="dk-attr-val">${esc(val)}${norm}</span>${pill}</div>` };
}
const TKIND = (k) => (/short/i.test(k) ? 'Curto' : /extended|long/i.test(k) ? 'Longo' : /conveyance/i.test(k) ? 'Transporte' : k);
function tStatus(s) {
  const r = [[/without error/i, 'sem erros'], [/aborted by host/i, 'interrompido pelo computador'], [/interrupted/i, 'interrompido (o disco foi desligado ou reiniciado)'],
    [/read failure/i, 'erro de leitura'], [/electrical/i, 'falha elétrica'], [/servo|seek/i, 'falha mecânica'], [/handling damage/i, 'dano por pancada'], [/fatal/i, 'erro grave']].find(([re]) => re.test(s));
  return r ? r[1] : s;
}
function renderHealth(d, pane) {
  const s = d.smart, sm = D.smart;
  const status = sm?.running ? 'Lendo a saúde dos discos (SMART), um de cada vez…' : sm?.err ? esc(sm.err)
    : s?.checkedAt ? `Lida em ${esc(when(s.checkedAt))}. A saúde é lida a cada 6 horas; HD parado não é acordado.` : 'A saúde (SMART) é lida a cada 6 horas.';
  const foot = `<div class="disk-hint smart-row">${svg('shield')}<span>${status}</span>
    ${/script do servidor|smartmontools/.test(sm?.err || '') && !sm?.running ? `<button class="pill-btn" data-act="script">Mostrar comandos</button>` : ''}
    <button class="pill-btn ${sm?.running ? 'busy' : ''}" data-act="smart-now" ${sm?.running ? 'disabled' : ''}>${svg('refresh')}${sm?.running ? 'Lendo…' : 'Ler agora'}</button></div>`;
  if (!s) { pane.innerHTML = `<div class="empty-note big">${svg('shield')}<b>A saúde deste disco ainda não foi lida</b><span>O BrunOS lê o SMART de cada disco pelo script do servidor.</span></div>${foot}`; return; }
  if (s.noSmart) {
    const usb = s.tran === 'usb';
    pane.innerHTML = `<div class="card dk-health"><p class="dk-lead"><span class="smart-badge bad big">sem SMART</span>${s.noSmartAt ? ` desde ${esc(when(s.noSmartAt))}` : ''}</p>
      <p>${s.noSmartWhy === 'travou' ? 'O adaptador deste disco não respondeu ao pedido da saúde do disco (SMART). Quando isso acontece, o Linux costuma reiniciar a conexão USB do disco.' : 'O adaptador deste disco respondeu, mas sem nenhum dado da saúde do disco (SMART).'}
      O disco em si não tem problema por causa disso: ler e gravar arquivos continua normal. Para não forçar o adaptador, o BrunOS parou de perguntar a este disco (nem saúde, nem temperatura, nem se está dormindo).</p>
      ${usb ? '<p>Isso é comum em cases USB: o chip do case nem sempre aceita os comandos do SMART. Um case com chip <b>ASMedia</b> costuma funcionar.</p>' : ''}
      <p class="dk-small">"Tentar de novo" pede do outro jeito (${s.failMode === 'sat' ? 'automático' : 'como disco SATA'}). Se o adaptador também não aceitar, ele pode reiniciar a conexão USB do disco por alguns segundos: faça com o disco parado (nada copiando nem tocando).</p>
      <div class="btn-row start"><button class="pill-btn" data-act="smart-retry">${svg('refresh')}Tentar de novo</button></div></div>${foot}`;
    return;
  }
  const [label, cls, text] = HEALTH[s.health] || ['—', '', ''];
  const test = s.test;
  const testCard = `<div class="card dk-test"><div class="dk-test-head"><b>Teste do próprio disco</b>
      <span class="dk-dim">O disco lê a si mesmo procurando defeitos, sem apagar nada. Dá para continuar usando enquanto isso (fica um pouco mais lento).</span></div>
    ${test ? `<div class="dk-progress"><span>Teste ${esc(test.kind || '')} rodando · falta ${test.remaining}%</span><span class="track"><i style="width:${100 - test.remaining}%"></i></span>
      <small class="dk-dim">O BrunOS confere o andamento a cada 3 minutos e avisa no sino quando terminar.</small></div>` : ''}
    <div class="btn-row start"><button class="pill-btn" data-act="test" data-kind="curto" ${test ? 'disabled' : ''}>${svg('play')}Teste curto${s.shortMin ? ` (~${s.shortMin} min)` : ''}</button>
      <button class="pill-btn" data-act="test" data-kind="longo" ${test ? 'disabled' : ''}>${svg('play')}Teste longo${s.longMin ? ` (~${s.longMin >= 90 ? `${Math.round(s.longMin / 60)} h` : `${s.longMin} min`})` : ''}</button></div>
    ${(s.tests || []).length ? `<div class="dk-tests">${s.tests.map((t) => `<div class="dk-tline ${t.passed ? 'ok' : 'bad'}">${svg(t.passed ? 'check' : 'alert')}<span>${esc(TKIND(t.kind))}: ${esc(tStatus(t.status))}</span><small class="dk-dim">com ${fmtN(t.hours)} h ligado</small></div>`).join('')}</div>` : '<p class="dk-small">Nenhum teste registrado neste disco ainda.</p>'}</div>`;
  const rows = (s.attrs || []).map(attrRow).sort((a, b) => ({ bad: 0, warn: 1 }[a.st] ?? 2) - ({ bad: 0, warn: 1 }[b.st] ?? 2));
  const hint = s.attrs?.length ? '' : '<p class="dk-small">A lista completa dos atributos aparece na próxima leitura (o script do servidor precisa estar na versão 10).</p>';
  pane.innerHTML = `<div class="card dk-health"><p class="dk-lead"><span class="smart-badge ${cls} big">${label}</span> ${esc(text)}</p>
      <div class="dk-quick">${[['Temperatura', s.temp != null ? `${Math.round(s.temp)} °C` : '—'], ['Horas ligado', s.powerOnHours != null ? hours(s.powerOnHours) : '—'], ['Vezes ligado', fmtN(s.powerCycles)],
        ...(s.percentUsed != null ? [['Desgaste', `${s.percentUsed}%`]] : [['Setores com problema', fmtN((s.reallocated || 0) + (s.pending || 0) + (s.uncorrectable || 0))]])].map(([k, v]) => `<div><small>${k}</small><b>${v}</b></div>`).join('')}</div></div>
    ${testCard}
    <div class="live-sec">Atributos do SMART <small>o que cada número quer dizer</small></div>
    <div class="card dk-attrs">${rows.map((r) => r.html).join('') || '<p class="dk-dim">Sem atributos.</p>'}</div>${hint}
    ${(s.history || []).length > 1 ? `<p class="dk-small">Mudanças: ${s.history.map((h) => `${new Date(h.at * 1000).toLocaleDateString('pt-BR')}: ${h.reallocated} realocados, ${h.pending} pendentes`).map(esc).join(' · ')}</p>` : ''}
    ${foot}`;
}

/* ── Desempenho ──────────────────────────────────────────────── */
const SPEED_REF = 'Para comparar: HD por USB 2.0 fica perto de 35 MB/s; HD por USB 3 ou SATA, de 80 a 200 MB/s; SSD SATA, perto de 500 MB/s; NVMe passa de 1.500 MB/s.';
function renderPerf(d, pane) {
  D.charts.forEach((c) => c.destroy?.()); D.charts = [];
  pane.innerHTML = `<div class="spark-box"><div class="lbl"><span>Leitura e escrita · <span data-plabel>${PERIOD_LONG[D.period]}</span></span><span class="f-spacer"></span>${periodSeg(D.period, true)}</div>
      <div class="dk-legend"><span><i class="c0"></i>leitura</span><span><i class="c1"></i>escrita</span></div><div class="dk-chart" data-c="io"></div></div>
    <div class="spark-box"><div class="lbl"><span>Ocupado (% do tempo lendo ou gravando)</span></div><div class="dk-chart" data-c="busy"></div></div>
    <div class="spark-box"><div class="lbl"><span>Temperatura · <span data-tlabel></span></span></div><div class="dk-chart" data-c="temp"></div></div>
    ${d.rotational && d.temp == null && !d.smart?.noSmart ? `<div class="disk-hint">${svg('info')}<span>Para ver a temperatura dos HDs, ative o sensor no servidor.</span><button class="pill-btn" data-act="temp-help">Como ativar</button></div>` : ''}
    <div class="card dk-speed" data-speed></div>`;
  const opt = (fmtv, extra = {}) => ({ h: 110, fmt: fmtv, min: 0, ...extra });
  D.chartIO = mountChart($('[data-c="io"]', pane), [], opt((v) => fmtRate(v)));
  D.chartBusy = mountChart($('[data-c="busy"]', pane), [], opt((v) => `${fmt(v, v < 10 ? 1 : 0)}%`, { max: 100 }));
  D.chartTemp = mountChart($('[data-c="temp"]', pane), [], opt((v) => `${fmt(v, 0)} °C`, { min: undefined }));
  D.charts = [D.chartIO, D.chartBusy, D.chartTemp];
  paintSpeed(d);
  loadPerf(d);
}
async function loadPerf(d) {
  const p = D.period, live = p === 'live', names = ['rd', 'wr', 'busy'].map((k) => `disk.${d.dev}.${k}`), tName = `disk.${d.dev}.temp`, tPeriod = live ? '24h' : p;
  $('[data-tlabel]', D.body) && ($('[data-tlabel]', D.body).textContent = PERIOD_LONG[tPeriod]);
  try {
    const [r, rt] = await Promise.all([api.history(names, live ? '5m' : p), api.history([tName], tPeriod)]);
    if (!D || cur()?.key !== d.key || D.period !== p || D.tab !== 'perf') return;
    const pts = (n) => (r.series?.[n] || []).map(([t, v, max]) => ({ t, v, max }));
    if (live) { // os pontos de 1 min do Histórico e, depois, os de 2 s que chegarem
      const L = D.live[d.dev] || (D.live[d.dev] = []);
      const cut = L[0]?.t ?? Infinity;
      D.seed = { dev: d.dev, rd: pts(names[0]).filter((x) => x.t < cut), wr: pts(names[1]).filter((x) => x.t < cut), busy: pts(names[2]).filter((x) => x.t < cut) };
    } else D.seed = { dev: d.dev, rd: pts(names[0]), wr: pts(names[1]), busy: pts(names[2]), step: r.step };
    const tp = (rt.series?.[tName] || []).map(([t, v, max]) => ({ t, v, max }));
    // a do SMART chega a cada 20 min: a linha só quebra em buracos maiores que isso
    D.chartTemp?.update([{ name: 'Temperatura', pts: tp, cls: 'c0' }], { fmt: (v) => `${fmt(v, 0)} °C`, step: Math.max(rt.step || 60, 1200), empty: 'ainda sem medidas de temperatura deste disco' });
  } catch { if (D) D.seed = { dev: d.dev, rd: [], wr: [], busy: [] }; }
  drawPerf();
}
function drawPerf() {
  const d = cur(); if (!d || !D.seed || D.tab !== 'perf') return;
  const live = D.period === 'live', L = live ? (D.live[d.dev] || []) : [];
  const join = (k) => [...D.seed[k], ...L.map((x) => ({ t: x.t, v: x[k] }))];
  const o = { live, step: live ? 0 : (D.seed.step || 60), empty: live ? 'aguardando dados' : 'ainda sem histórico deste disco' };
  D.chartIO?.update([{ name: 'Leitura', pts: join('rd'), cls: 'c0' }, { name: 'Escrita', pts: join('wr'), cls: 'c1', area: false }], { ...o, fmt: (v) => fmtRate(v), min: 0 });
  D.chartBusy?.update([{ name: 'Ocupado', pts: join('busy'), cls: 'c0' }], { ...o, fmt: (v) => `${fmt(v, v < 10 ? 1 : 0)}%`, min: 0, max: 100 });
}
function onStats() {
  if (!D) return;
  const sn = D.ctx.snapshot, t = Math.floor(Date.now() / 1000);
  for (const io of sn?.diskIO || []) {
    const L = D.live[io.name] || (D.live[io.name] = []);
    L.push({ t, rd: io.read, wr: io.write, busy: io.busy });
    while (L.length && L[0].t < t - 300) L.shift();
  }
  if (D.tab === 'perf' && D.period === 'live') drawPerf();
}
function paintSpeed(d) {
  const el = $('[data-speed]', D.body); if (!el) return;
  const sp = d.speed;
  const html = `<div class="dk-test-head"><b>${svg('gauge')}Teste de velocidade</b>
      <span class="dk-dim">Lê 1 GB do começo do disco, direto do disco. Não grava nada. Leva uns 10 segundos num HD; se o disco estiver dormindo, ele acorda.</span></div>
    ${d.speedBusy ? `<div class="dk-progress"><span>Lendo 1 GB…</span><span class="track indeterminate"><i></i></span></div>` : ''}
    ${sp ? `<p class="dk-speed-val"><b>${fmt(sp.mbps, sp.mbps < 100 ? 1 : 0)} MB/s</b> <small class="dk-dim">leitura · ${esc(when(sp.at))}</small></p>` : ''}
    ${d.speedErr ? `<p class="dk-note bad">${esc(d.speedErr)}</p>` : ''}
    <div class="btn-row start"><button class="pill-btn" data-act="speed" ${d.speedBusy || D.list.some((x) => x.speedBusy) ? 'disabled' : ''}>${svg('play')}${sp ? 'Testar de novo' : 'Testar velocidade'}</button></div>
    <p class="dk-small">${SPEED_REF}</p>`;
  if (el.innerHTML !== html) el.innerHTML = html;
}

/* ── Espaço ──────────────────────────────────────────────────── */
async function renderSpace(d, pane) {
  const parts = d.parts.filter((p) => (p.mounts || []).some((m) => m !== '[SWAP]'));
  if (!parts.length) {
    pane.innerHTML = `<div class="empty-note big">${svg('disks')}<b>Este disco não está montado</b><span>O espaço aparece quando alguma partição dele tiver uma pasta de montagem.</span></div>`;
    return;
  }
  pane.innerHTML = parts.map((p, i) => {
    const pct = p.used != null && p.avail != null ? (p.used / (p.used + p.avail)) * 100 : null;
    return `<div class="card dk-space" data-part="${i}"><div class="dk-space-head"><b>${esc(p.label || p.name)}</b><small class="dk-dim">${esc((p.mounts || []).join(', '))}</small></div>
      ${pct != null ? `<div class="dk-space-num"><b>${fmtBytes(p.used)}</b> usados de ${fmtBytes(p.used + p.avail)} · <b>${fmtBytes(p.avail)}</b> livres</div>
        <span class="track big"><i style="width:${pct}%" class="${pct > 90 ? 'warn' : ''}"></i></span>` : ''}
      <div class="dk-fc" data-fc></div><div class="dk-chart" data-hist></div>
      <div class="btn-row start">${p.root ? `<button class="pill-btn" data-act="spacemap" data-root="${esc(p.root)}" data-name="${esc(p.label || p.name)}">${svg('treemap')}Mapa do espaço</button>
        <button class="pill-btn" data-act="dupes" data-root="${esc(p.root)}" data-name="${esc(p.label || p.name)}">${svg('dupes')}Arquivos duplicados</button>`
        : `<span class="dk-small">Para usar o Mapa do espaço e os Duplicados, mapeie esta pasta no Arquivos.</span>${D.ctx.mapDisks ? `<button class="pill-btn" data-act="map">${svg('folder')}Mapear discos</button>` : ''}`}</div></div>`;
  }).join('');
  let fc = [];
  try { fc = (await api.disks.forecast()).disks || []; } catch { /* sem histórico: só não mostra */ }
  if (!D || cur()?.key !== d.key || D.tab !== 'space') return;
  parts.forEach((p, i) => {
    const box = $(`[data-part="${i}"]`, pane); if (!box) return;
    const f = fc.find((x) => x.name === p.hist);
    if (!f || !f.points?.length) { $('[data-fc]', box).innerHTML = '<p class="dk-small">O histórico do espaço começa a aparecer depois de alguns dias (um ponto por dia).</p>'; return; }
    $('[data-fc]', box).innerHTML = `<p class="dk-small">${esc(forecastText(f) || '')}</p>`;
    const pts = f.points.map((x) => ({ t: Date.parse(x.d + 'T12:00:00') / 1000, v: x.u }));
    // de zero até a capacidade: dá para ver de relance o quanto do disco já foi usado
    const last = f.points[f.points.length - 1], cap = p.used != null && p.avail != null ? p.used + p.avail : last.u + last.f;
    const c = mountChart($('[data-hist]', box), [{ name: 'Usado', pts, cls: 'c0' }], { h: 100, fmt: (v) => fmtBytes(v), min: 0, max: cap, step: 86400, tLabel: (t) => fmtTime(t, 0, { day: true }), empty: 'sem histórico ainda' });
    D.charts.push(c);
  });
}

/* ── Linha do tempo ──────────────────────────────────────────── */
const EV_ICON = { visto: 'sparkles', novo: 'plus', conectado: 'plug', sumiu: 'plugOff', montado: 'folder', desmontado: 'folder', smart: 'shield', teste: 'check', temperatura: 'temp', velocidade: 'gauge', ejetado: 'eject' };
async function renderTimeline(d, pane) {
  pane.innerHTML = `<div class="card dk-tl" data-tl><div class="empty-note">Carregando…</div></div>
    <div class="live-sec">Erros no registro do Linux <small>últimos 30 dias, só deste disco</small></div><div class="card dk-log" data-log><div class="empty-note">Procurando…</div></div>`;
  api.disks.timeline(d.key).then((r) => {
    const box = $('[data-tl]', D?.body); if (!box || cur()?.key !== d.key) return;
    const evs = r.events || [];
    box.innerHTML = evs.length ? evs.map((e) => `<div class="dk-ev ${e.level === 'aviso' ? 'warn' : ''}"><span class="dk-ev-ico">${svg(EV_ICON[e.kind] || 'info')}</span>
      <span class="dk-ev-txt">${esc(e.text)}</span><small class="dk-dim" title="${esc(when(e.at))}">${esc(when(e.at))}</small></div>`).join('')
      : '<div class="empty-note">Nada registrado ainda. Daqui para a frente o BrunOS anota quando o disco conecta, some, monta, esquenta ou muda de saúde.</div>';
  }).catch((e) => { const box = $('[data-tl]', D?.body); if (box) box.innerHTML = `<div class="empty-note">${esc(e.message)}</div>`; });
  api.disks.errors(d.dev).then((r) => {
    const box = $('[data-log]', D?.body); if (!box || cur()?.key !== d.key) return;
    const lines = r.lines || [];
    box.innerHTML = lines.length ? `<p class="dk-small">Erros de leitura, resets e desconexões do USB que o Linux anotou. Um ou outro isolado é comum; muitos seguidos indicam cabo, fonte ou disco com problema.</p>
      <div class="dk-lines">${lines.map((l) => `<div><small>${esc(l.at.replace('T', ' ').slice(0, 16))}</small><code>${esc(l.text)}</code></div>`).join('')}</div>`
      : `<div class="empty-note">${svg('check')} Nenhum erro deste disco nos últimos 30 dias.</div>`;
  }).catch((e) => { const box = $('[data-log]', D?.body); if (box) box.innerHTML = `<div class="empty-note">${esc(e.message)}</div>`; });
}

/* ── cliques ─────────────────────────────────────────────────── */
async function followSmart() {
  for (let i = 0; i < 150 && D; i++) {
    D.smart = await api.smart.get();
    if (!D) return;
    D.paneKey = ''; paintPane();
    if (!D.smart.running) break;
    await new Promise((ok) => setTimeout(ok, 2000));
  }
  load();
}
async function onClick(e) {
  const t = e.target;
  const it = t.closest('.dk-item'); if (it) { select(it.dataset.key, true); return; }
  const tab = t.closest('[data-tab]');
  if (tab) {
    D.tab = tab.dataset.tab; lsSet('brunos.disksTab', D.tab); $$('[data-tab]', D.body).forEach((x) => x.classList.toggle('on', x === tab));
    const pane = $('.dk-pane', D.body); D.paneKey = ''; if (pane) pane.scrollTop = 0; // aba nova começa do topo
    paintPane(true); return;
  }
  const pe = t.closest('[data-period]');
  if (pe) { D.period = pe.dataset.period; $$('[data-period]', D.body).forEach((x) => x.classList.toggle('on', x === pe)); $('[data-plabel]', D.body).textContent = PERIOD_LONG[D.period]; const d = cur(); if (d) loadPerf(d); return; }
  const cp = t.closest('[data-copy]');
  if (cp) { try { await navigator.clipboard.writeText(cp.dataset.copy); toast('Caminho copiado', cp.dataset.copy); } catch { toast('Não deu para copiar', cp.dataset.copy); } return; }
  const ap = t.closest('[data-app]'); if (ap) { if (ap.dataset.app) D.ctx.openApp?.(ap.dataset.app); return; }
  const b = t.closest('[data-act]'); if (!b) return;
  const d = cur(), act = b.dataset.act;
  if (act === 'back') { D.detail = false; $('.dk', D.body).classList.remove('show-detail'); return; }
  if (act === 'refresh') { b.classList.add('busy'); await load(true); b.classList.remove('busy'); return; }
  if (act === 'script') { D.ctx.scriptDialog?.(); return; }
  if (act === 'temp-help') { driveTempHelp(); return; }
  if (act === 'map') { D.ctx.mapDisks?.(); return; }
  if (act === 'spacemap' || act === 'dupes') {
    const m = await import('./filetools.js');
    if (act === 'spacemap') m.spaceDialog({ root: b.dataset.root, path: '', rootName: b.dataset.name, onOpen: (p) => D?.ctx.openFiles?.(b.dataset.root, p) });
    else m.duplicatesDialog({ root: b.dataset.root, path: '', rootName: b.dataset.name, readOnly: false, onChanged: () => {} });
    return;
  }
  if (!d) return;
  if (act === 'smart-now') {
    b.disabled = true; b.classList.add('busy');
    try { D.smart = await api.smart.refresh(); await followSmart(); toast('Discos lidos', 'Os que estavam parados ficam para a próxima.'); }
    catch (err) { toastError(err, 'Não deu para ler os discos'); load(); }
    return;
  }
  if (act === 'smart-retry') {
    b.disabled = true; b.classList.add('busy');
    try {
      D.smart = await api.smart.retry(d.smart.key); await followSmart();
      const s = D?.smart?.disks.find((x) => x.key === d.smart.key);
      toast(s?.noSmart ? `${d.dev}: continua sem SMART` : `${d.dev}: SMART funcionando`, s?.noSmart ? 'O adaptador também não aceitou do outro jeito.' : 'O BrunOS volta a ler a saúde deste disco.');
    } catch (err) { toastError(err, 'Não deu para tentar de novo'); b.disabled = false; b.classList.remove('busy'); }
    return;
  }
  if (act === 'test') {
    const kind = b.dataset.kind, s = d.smart;
    const dur = kind === 'curto' ? (s?.shortMin ? `uns ${s.shortMin} minutos` : 'uns 2 minutos') : (s?.longMin ? `umas ${Math.max(1, Math.round(s.longMin / 60))} horas` : 'algumas horas');
    const ok = await confirmDialog(`Teste ${kind} em ${diskName(d)}?`, `O disco lê a si mesmo procurando defeitos, sem apagar nada. Leva ${dur}. Dá para continuar usando o disco (fica um pouco mais lento). Se o disco for desligado no meio, o teste é interrompido.${kind === 'longo' && d.tran === 'usb' ? ' Alguns cases USB desligam o disco parado no meio do teste longo; se isso acontecer, o resultado aparece como interrompido.' : ''}`, 'Começar o teste');
    if (!ok) return;
    b.disabled = true;
    try { D.smart = await api.smart.selftest(d.smart.key, kind); toast('Teste iniciado', `O BrunOS avisa no sino quando terminar (${dur}).`); load(); }
    catch (err) { toastError(err, 'Não deu para começar o teste'); b.disabled = false; }
    return;
  }
  if (act === 'speed') {
    b.disabled = true;
    try { const r = await api.disks.speed(d.dev); D.list = r.disks || D.list; renderSide(); paintSpeed(cur()); load(); }
    catch (err) { toastError(err, 'Não deu para testar a velocidade'); b.disabled = false; }
    return;
  }
  if (act === 'eject') {
    const ok = await confirmDialog(`Ejetar ${diskName(d)}?`, 'O BrunOS grava o que estiver pendente e desliga o disco. Depois disso dá para tirar o cabo com segurança. Para usar de novo, tire e coloque o cabo (ou desligue e ligue o case).', 'Ejetar');
    if (!ok) return;
    b.disabled = true;
    try { await api.disks.eject(d.dev); toast('Disco ejetado', 'Já pode tirar o cabo.'); setTimeout(() => load(true), 3500); }
    catch (err) { toastError(err, 'Não deu para ejetar'); b.disabled = false; }
  }
}
