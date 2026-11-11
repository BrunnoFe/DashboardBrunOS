// Consumo e custo do servidor (B3, termos em CONTEXT.md): seção da janela Energia.
// O consumo vem do Medidor (Automático, tomada, bateria, processador ou estimativa calibrada); o
// servidor amostra a cada minuto e esta tela só mostra (e muda o Medidor e a tarifa).
import { api } from './api.js';
import { $, esc, fmt, svg, openDialog, toast, toastError } from './ui.js';
import { lineSVG, barSVG } from './charts.js';

export const METER_LABEL = { plug: 'pela tomada', battery: 'pela bateria', rapl: 'só o processador', estimate: 'estimativa calibrada', auto: 'automático' };
const METERS = [['auto', 'Automático', 'tomada → bateria (nas quedas) → estimativa'], ['plug', 'Tomada inteligente', 'o servidor inteiro, direto da tomada'],
  ['battery', 'Bateria', 'só quando o servidor está sem energia da tomada'], ['rapl', 'Processador (RAPL)', 'só o processador, pelo script do servidor'],
  ['estimate', 'Estimativa calibrada', 'processador + o resto aprendido nas quedas']];
export const brl = (v) => (v ?? 0).toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });

/** Monta a seção Consumo dentro de `box` (janela Energia). ctx: { onChange() } */
export async function mountEnergy(box, ctx = {}) {
  let data, plugs = [], hist = null, view = 'day';
  const load = async () => {
    [data, hist] = await Promise.all([api.energy.get(), api.energy.history().catch(() => null)]);
    try { plugs = (await api.integrations.list()).integrations.filter((x) => x.kind === 'tuya'); } catch { plugs = []; }
  };
  try { await load(); } catch (e) { box.innerHTML = `<div class="empty-note">${esc(e.message)}</div>`; return; }
  const draw = () => {
    const n = data.now, c = data.config;
    const w = n.w != null ? `${fmt(n.w, n.w < 100 ? 1 : 0)} W` : '—';
    const pts = (hist?.minutes || []).filter((p) => p.v != null).map((p) => ({ t: p.t, v: p.v }));
    const days = (hist?.days || []).slice(-30);
    const chart = view === 'day'
      ? (pts.length >= 2 ? lineSVG([{ pts, cls: 'c0' }], { w: 560, h: 110, area: true }) : '<span class="w-empty">ainda sem histórico (aparece em alguns minutos)</span>')
      : (days.length ? barSVG([{ pts: days.map((d, i) => ({ t: i, v: d.kWh })), cls: 'c0' }], { w: 560, h: 110 }) : '<span class="w-empty">ainda sem dias completos</span>');
    const script = /script do servidor/.test(n.err || '');
    box.innerHTML = `<div class="en-now">
        <div class="en-big"><b>${esc(w)}</b><small>${n.w != null ? `agora · ${esc(METER_LABEL[n.meter] || '')}` : esc(n.err || 'medindo…')}</small></div>
        <div class="en-cells">
          <span class="cell"><span class="k">Hoje</span><span class="v">${fmt(n.todayKWh, 2)} kWh</span><small>${brl(n.todayCost)}</small></span>
          <span class="cell"><span class="k">Mês até agora</span><span class="v">${brl(n.monthCost)}</span><small>${fmt(n.monthKWh, 1)} kWh</small></span>
          <span class="cell"><span class="k">Projeção do mês</span><span class="v">${n.projection ? brl(n.projection) : '—'}</span><small>pelo ritmo até agora</small></span>
        </div></div>
      ${script ? `<div class="en-note">${svg('info')}<span>Para o processador e a estimativa, atualize o script do servidor (versão 4).</span><button class="pill-btn sm" data-en="script">Mostrar comandos</button></div>` : ''}
      <div class="en-chart-h"><div class="seg seg-sm"><button class="${view === 'day' ? 'on' : ''}" data-en-view="day">24 horas</button><button class="${view === 'month' ? 'on' : ''}" data-en-view="month">30 dias</button></div></div>
      <div class="en-chart">${chart}</div>
      <div class="s-row"><span class="s-ico">${svg('activity')}</span><span class="s-text"><b>Medidor</b><small>${esc(METERS.find(([k]) => k === c.meter)?.[2] || '')}</small></span>
        <span class="s-ctrl"><select class="text-input sm" data-en="meter" aria-label="Medidor">${METERS.map(([k, l]) => `<option value="${k}" ${c.meter === k ? 'selected' : ''}>${l}</option>`).join('')}</select></span></div>
      <div class="s-row ${c.meter === 'auto' || c.meter === 'plug' ? '' : 'dim'}"><span class="s-ico">${svg('plug')}</span><span class="s-text"><b>Tomada inteligente</b><small>${plugs.length ? 'A integração Tomada Tuya que mede o servidor' : 'Cadastre em Widgets → Integrações → Tomada Tuya'}</small></span>
        <span class="s-ctrl"><select class="text-input sm" data-en="plug" aria-label="Tomada" ${plugs.length ? '' : 'disabled'}><option value="">${plugs.length ? 'a primeira cadastrada' : 'nenhuma'}</option>${plugs.map((p) => `<option value="${esc(p.id)}" ${c.plugId === p.id ? 'selected' : ''}>${esc(p.name)}</option>`).join('')}</select></span></div>
      <div class="s-row"><span class="s-ico">${svg('bag')}</span><span class="s-text"><b>Tarifa (R$ por kWh)</b><small>Total da conta de luz ÷ kWh do mês, com impostos${n.offset != null ? ` · resto da máquina aprendido: ${fmt(n.offset, 1)} W` : ''}</small></span>
        <span class="s-ctrl"><input class="text-input sm en-tariff" data-en="tariff" type="number" step="0.01" min="0.01" max="10" value="${c.tariff}" aria-label="Tarifa"></span></div>`;
  };
  const save = async (patch) => {
    try { data = await api.energy.setConfig({ ...data.config, ...patch }); draw(); ctx.onChange?.(); setTimeout(async () => { try { data = await api.energy.get(); draw(); } catch { /* depois */ } }, 2500); }
    catch (e) { toastError(e, 'Não deu para salvar'); }
  };
  box.addEventListener('change', (e) => {
    const k = e.target.dataset.en;
    if (k === 'meter') save({ meter: e.target.value });
    else if (k === 'plug') save({ plugId: e.target.value });
    else if (k === 'tariff') { const v = parseFloat(String(e.target.value).replace(',', '.')); if (v > 0) save({ tariff: v }); }
  });
  box.addEventListener('click', async (e) => {
    const v = e.target.closest('[data-en-view]'); if (v) { view = v.dataset.enView; draw(); return; }
    if (e.target.closest('[data-en="script"]')) scriptDialog();
  });
  draw();
  const timer = setInterval(async () => { if (!box.isConnected) { clearInterval(timer); return; } try { await load(); draw(); } catch { /* tenta de novo */ } }, 60000);
}

/** Comandos de atualização do script do servidor (versão 4: consumo, SMART e atualizações). */
export async function scriptDialog() {
  let cmds = '';
  try { cmds = (await api.power.setup()).updateCommands; } catch (e) { toastError(e, 'Não deu para gerar os comandos'); return; }
  const d = openDialog(`<h2>${svg('terminal')}Atualizar o script do servidor</h2>
    <p class="dlg-text">A versão 4 do script <code>brunos-host</code> traz pedidos novos, todos só de leitura: consumo do processador, saúde dos discos (SMART) e atualizações do Linux. Cole estes comandos no servidor uma vez (por exemplo no Terminal do BrunOS). Para a saúde dos discos, instale também o <code>smartmontools</code>.</p>
    <pre class="pw-cmds" tabindex="0">${esc(`sudo apt install -y smartmontools\n${cmds}`)}</pre>
    <div class="dlg-actions"><button class="pill-btn" data-c="copy">${svg('copy')}Copiar</button><button class="pill-btn primary" data-c="ok">Pronto</button></div>`, { width: 640 });
  d.el.addEventListener('click', async (e) => {
    const c = e.target.closest('[data-c]')?.dataset.c;
    if (c === 'ok') d.close();
    else if (c === 'copy') { try { await navigator.clipboard.writeText($('.pw-cmds', d.el).textContent); toast('Comandos copiados', 'Cole no terminal do servidor.'); } catch { toast('Não deu para copiar', 'Selecione o texto e copie com Ctrl+C.'); } }
  });
}
