// Widget e janela da bateria. Num notebook usado como servidor, a bateria é o nobreak:
// o widget mostra carga, se está na tomada, tempo restante (ou até encher), energia, consumo e saúde.
import { $, esc, fmt, svg, openDialog } from './ui.js';

// Cor do nível: verde a partir de 90%, branco de 30 a 89%, amarelo de 20 a 29%, vermelho abaixo de 20%.
export const levelClass = (p) => (p >= 90 ? 'lv-full' : p >= 30 ? 'lv-ok' : p >= 20 ? 'lv-low' : 'lv-crit');

export function fmtMinutes(m) {
  if (m == null || !isFinite(m)) return '';
  m = Math.max(0, Math.round(m));
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60), r = m % 60;
  return r ? `${h} h ${String(r).padStart(2, '0')}` : `${h} h`;
}

// Desenho da bateria (horizontal), preenchida até o nível, com raio quando carrega.
export function batteryGlyph(b, cls = '') {
  const p = Math.max(0, Math.min(100, b?.percent ?? 0));
  const charging = b?.status === 'Charging';
  return `<svg class="bat-glyph ${levelClass(p)} ${cls}" viewBox="0 0 34 18" aria-hidden="true">
    <rect x="0.75" y="0.75" width="29" height="16.5" rx="4.2" class="bat-shell"/>
    <path d="M31.6 6.2c1 .3 1.6 1.2 1.6 2.8s-.6 2.5-1.6 2.8z" class="bat-cap"/>
    <rect x="3" y="3" width="${(24.5 * p) / 100}" height="12" rx="2.4" class="bat-fill"/>
    ${charging ? '<path d="M16.6 2.6l-5 7.1h3.9l-1.4 5.7 5.3-7.4h-4z" class="bat-bolt"/>' : ''}
  </svg>`;
}

export function stateText(b) {
  if (!b) return { title: 'Sem bateria', sub: 'Nenhuma bateria encontrada' };
  const ac = b.ac ?? b.status !== 'Discharging';
  if (b.status === 'Discharging' || ac === false) {
    return { title: 'Na bateria', sub: b.minutesLeft != null ? `${fmtMinutes(b.minutesLeft)} restantes` : 'Calculando autonomia…' };
  }
  if (b.status === 'Charging') {
    const lim = b.chargeLimit ? ` até ${b.chargeLimit}%` : '';
    return { title: 'Carregando', sub: b.minutesFull != null ? `Cheia${lim} em ${fmtMinutes(b.minutesFull)}` : `Carregando${lim}` };
  }
  const full = b.status === 'Full' || b.percent >= 99;
  return {
    title: 'Na tomada',
    sub: b.estimate != null ? `Autonomia estimada: ${fmtMinutes(b.estimate)}` : full ? 'Carga completa' : b.chargeLimit ? `Carga mantida em ${b.chargeLimit}%` : 'Carga mantida',
  };
}

export function batteryWidgetHTML() {
  return `<button class="widget w-battery" data-action="battery" aria-label="Bateria">
    <span class="wb-top"><span class="w-caption">Bateria</span><span class="wb-power" data-bat="power"></span></span>
    <span class="wb-main"><span data-bat="glyph"></span><span class="w-value" data-bat="pct">—</span><span class="wb-state" data-bat="title"></span></span>
    <span class="wb-sub" data-bat="sub"></span>
    <span class="wb-cells">
      <span class="cell"><span class="k">Energia</span><span class="v" data-bat="wh">—</span></span>
      <span class="cell"><span class="k" data-bat="wlabel">Consumo</span><span class="v" data-bat="w">—</span></span>
      <span class="cell"><span class="k">Saúde</span><span class="v" data-bat="health">—</span></span>
    </span>
  </button>`;
}

const setText = (el, v) => { if (el && el.textContent !== v) el.textContent = v; };
const setHTML = (el, v) => { if (el && el._h !== v) { el.innerHTML = v; el._h = v; } };

// Atualiza qualquer bloco com data-bat=... dentro de root.
export function paintBattery(root, b) {
  const q = (k) => root.querySelectorAll(`[data-bat="${k}"]`);
  if (!q('pct').length) return;
  const st = stateText(b);
  const ac = b ? (b.ac ?? b.status !== 'Discharging') : null;
  q('glyph').forEach((el) => setHTML(el, batteryGlyph(b)));
  q('pct').forEach((el) => { setText(el, b ? `${b.percent}%` : '—'); el.className = `w-value ${b ? levelClass(b.percent) : ''}`; });
  q('title').forEach((el) => setText(el, st.title));
  q('sub').forEach((el) => setText(el, st.sub));
  q('power').forEach((el) => {
    const v = b == null ? '' : ac ? `<span class="wb-pill on" title="Na tomada">${svg('plug')}</span>` : `<span class="wb-pill" title="Desconectado da tomada">${svg('plugOff')}</span>`;
    setHTML(el, v);
  });
  q('wh').forEach((el) => setText(el, b?.energyWh ? `${fmt(b.energyWh, 1)} Wh` : '—'));
  q('wlabel').forEach((el) => setText(el, b?.status === 'Charging' ? 'Entrada' : 'Consumo'));
  q('w').forEach((el) => setText(el, b?.powerW > 0.05 ? `${fmt(b.powerW, 1)} W` : '0 W'));
  q('health').forEach((el) => setText(el, b?.health != null ? `${b.health}%` : '—'));
}

// Janela com detalhes e o histórico de carga das últimas horas.
let open = false;
// extra.loadOutages() devolve o HTML da lista de quedas de energia; extra.openPower abre a janela Energia.
export function batteryDialog(getBattery, onStats, extra = {}) {
  if (open) return;
  open = true;
  let hist = getBattery()?.history || [];
  const d = openDialog(`<h2>Bateria</h2>
    <div class="bat-hero">
      <span class="bat-hero-glyph" data-bat="glyph"></span>
      <span class="bat-hero-txt"><span class="w-value" data-bat="pct">—</span><span class="bat-hero-state"><b data-bat="title"></b><small data-bat="sub"></small></span></span>
      <span data-bat="power"></span>
    </div>
    <div class="spark-box"><div class="lbl"><span data-bh-title>Carga</span><span data-bh-extra></span></div>
      <svg class="spark bat-spark" viewBox="0 0 300 70" preserveAspectRatio="none"><path data-bh-ac class="bh-ac"/><path data-bh-area class="bh-area"/><path data-bh-line class="bh-line" vector-effect="non-scaling-stroke"/></svg></div>
    <div class="bat-grid" data-bh-grid></div>
    ${extra.loadOutages ? `<div class="live-sec bat-sec"><span>Quedas de energia</span>${extra.openPower ? `<button class="pill-btn" data-bat-power>${svg('power')}Desligamento seguro</button>` : ''}</div><div data-bat-outages><div class="empty-note">Carregando…</div></div>` : ''}
    <p class="bat-note">A autonomia vem do consumo medido pela bateria. Na tomada, o BrunOS usa o último consumo medido enquanto ela descarregava; desligue o carregador por alguns minutos para ele aprender.</p>`,
  { width: 560, onClose: () => { open = false; off(); } });
  if (extra.loadOutages) {
    extra.loadOutages().then((h) => { const el = $('[data-bat-outages]', d.el); if (el) el.innerHTML = h; }).catch(() => { const el = $('[data-bat-outages]', d.el); if (el) el.innerHTML = ''; });
    $('[data-bat-power]', d.el)?.addEventListener('click', () => { d.close(); extra.openPower(); });
  }
  const draw = () => {
    const b = getBattery();
    if (b?.history) hist = b.history;
    paintBattery(d.el, b);
    const rows = b ? [
      ['Carga atual', b.energyWh ? `${fmt(b.energyWh, 1)} Wh` : '—'],
      ['Capacidade (cheia)', b.fullWh ? `${fmt(b.fullWh, 1)} Wh` : '—'],
      ['Capacidade de fábrica', b.designWh ? `${fmt(b.designWh, 1)} Wh` : '—'],
      ['Saúde', b.health != null ? `${b.health}%` : '—'],
      [b.status === 'Charging' ? 'Potência de carga' : 'Consumo', b.powerW > 0.05 ? `${fmt(b.powerW, 1)} W` : '0 W'],
      ['Tensão', b.voltage ? `${fmt(b.voltage, 1)} V` : '—'],
      ['Ciclos de carga', b.cycles != null ? String(b.cycles) : '—'],
      ['Limite de carga', b.chargeLimit ? `${b.chargeLimit}%` : 'sem limite'],
      ['Tecnologia', b.tech || '—'],
      ['Modelo', [b.manufacturer, b.model].filter(Boolean).join(' ') || '—'],
    ] : [];
    const g = $('[data-bh-grid]', d.el);
    setHTML(g, rows.map(([k, v]) => `<div class="kv"><span>${k}</span><span>${esc(v)}</span></div>`).join(''));
    // gráfico: 0–100%, faixa clara onde estava na tomada
    if (hist.length > 1) {
      const t0 = hist[0].t, t1 = hist[hist.length - 1].t, span = Math.max(1, t1 - t0);
      const x = (t) => (((t - t0) / span) * 300).toFixed(1), y = (p) => (68 - (p / 100) * 64).toFixed(1);
      const line = hist.map((p, i) => `${i ? 'L' : 'M'}${x(p.t)} ${y(p.p)}`).join('');
      $('[data-bh-line]', d.el).setAttribute('d', line);
      $('[data-bh-area]', d.el).setAttribute('d', `${line}L300 70L0 70Z`);
      let ac = '';
      hist.forEach((p, i) => { if (p.ac && i < hist.length - 1) ac += `M${x(p.t)} 0H${x(hist[i + 1].t)}V70H${x(p.t)}Z`; });
      $('[data-bh-ac]', d.el).setAttribute('d', ac);
      const hrs = span / 3600;
      $('[data-bh-title]', d.el).textContent = `Carga · últimas ${hrs >= 1 ? fmt(hrs, hrs < 10 ? 1 : 0) + ' h' : Math.round(span / 60) + ' min'}`;
      $('[data-bh-extra]', d.el).textContent = 'faixa clara: na tomada';
    } else {
      $('[data-bh-title]', d.el).textContent = 'Carga';
      $('[data-bh-extra]', d.el).textContent = 'o histórico aparece depois de alguns minutos';
    }
  };
  const off = onStats(draw);
  draw();
}
