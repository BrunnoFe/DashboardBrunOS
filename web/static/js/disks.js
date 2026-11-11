// Discos na janela "Uso ao vivo": temperatura (HD com drivetemp ou NVMe) e previsão de disco cheio.
import { $, esc, fmtBytes, svg, toast, openDialog } from './ui.js';

// Mesmos limites dos avisos do servidor (watch.go): HD 55 °C, SSD/NVMe 70 °C.
const hotAt = (d) => (d.rotational ? 55 : 70);

function tempHTML(d) {
  if (d.temp == null) return '';
  const hot = hotAt(d), cls = d.temp >= hot ? 'hot' : d.temp >= hot - 5 ? 'warm' : '';
  const old = Date.now() / 1000 - d.tempAt > 180;
  const when = new Date(d.tempAt * 1000).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' });
  const title = old ? `Medida às ${when}. O HD está parado, e o BrunOS não o acorda só para medir.` : `Temperatura do disco (alerta a partir de ${hot} °C)`;
  return `<span class="disk-temp ${cls} ${old ? 'old' : ''}" title="${esc(title)}">${Math.round(d.temp)}°</span>`;
}

// "cheio em ~42 dias", "cheio em ~4 meses", "estável"
export function forecastText(f) {
  if (!f) return '';
  if (!f.enough) return 'previsão de espaço em alguns dias';
  if (f.daysLeft == null) return f.perDay < 0 ? 'liberando espaço' : 'uso estável';
  const n = f.daysLeft;
  const when = n < 1 ? 'hoje' : n < 60 ? `em ~${n} ${n === 1 ? 'dia' : 'dias'}` : n < 730 ? `em ~${Math.round(n / 30)} meses` : `em ~${Math.round(n / 365)} anos`;
  return `+${fmtBytes(f.perDay)}/dia · cheio ${when}`;
}

// SMART (D3): sda1 → sda, nvme0n1p2 → nvme0n1
const baseDev = (dev) => String(dev || '').replace('/dev/', '').replace(/^(nvme\d+n\d+)p\d+$/, '$1').replace(/^([a-z]+)\d+$/, '$1');
const HEALTH = { ok: ['OK', 'ok'], atencao: ['Atenção', 'warn'], falhando: ['Falhando', 'bad'] };
function smartBadge(s) {
  const h = s && HEALTH[s.health];
  return h ? `<button class="smart-badge ${h[1]}" data-smart="${esc(s.dev)}" title="Saúde do disco (SMART): clique para os detalhes">${h[0]}</button>` : '';
}

export function diskRowsHTML(disks, forecast = [], smart = null) {
  if (!disks.length) return '<div class="empty-note">Nenhum disco encontrado</div>';
  const fc = Object.fromEntries(forecast.map((f) => [f.name, f]));
  const sm = Object.fromEntries((smart?.disks || []).map((s) => [s.dev, s]));
  const rows = disks.map((k) => {
    const f = fc[k.name], ft = forecastText(f), soon = f?.daysLeft != null && f.daysLeft <= 30;
    return `<div class="disk-row">
      <span class="disk-ico">${svg(k.system ? 'cpu' : 'disk')}</span>
      <span class="disk-name"><b>${esc(k.name)}${tempHTML(k)}${smartBadge(sm[baseDev(k.device)])}</b><small>${esc(k.device.replace('/dev/', ''))}${k.model ? ' · ' + esc(k.model) : ''} · ${k.rotational ? 'HD' : 'SSD'}</small>${ft ? `<small class="disk-fc ${soon ? 'soon' : ''}">${esc(ft)}</small>` : ''}</span>
      <span class="track"><i style="width:${(k.used / k.total) * 100}%" class="${k.used / k.total > 0.9 ? 'warn' : ''}"></i></span>
      <span class="disk-val">${fmtBytes(k.used)} <small>/ ${fmtBytes(k.total)}</small></span></div>`;
  }).join('');
  // HD sem temperatura: o driver drivetemp não está carregado no servidor
  const hint = disks.some((k) => k.rotational && k.temp == null)
    ? `<div class="disk-hint">${svg('info')}<span>Para ver a temperatura dos HDs, ative o sensor no servidor.</span><button class="pill-btn" data-disk="temp-help">Como ativar</button></div>` : '';
  // saúde dos discos: quando foi lida, "Ler agora" e o que falta instalar
  const when = smart?.checkedAt ? new Date(smart.checkedAt * 1000).toLocaleString('pt-BR', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' }) : '';
  const smartRow = smart ? `<div class="disk-hint smart-row">${svg('shield')}<span>${smart.err ? esc(smart.err) : when ? `Saúde dos discos (SMART) lida em ${esc(when)}. HD parado não é acordado: fica para a próxima.` : 'A saúde dos discos (SMART) é lida a cada 6 horas.'}</span>
    ${/script do servidor|smartmontools/.test(smart.err || '') ? `<button class="pill-btn" data-disk="script">Mostrar comandos</button>` : ''}<button class="pill-btn" data-disk="smart-now">${svg('refresh')}Ler agora</button></div>` : '';
  return rows + hint + smartRow;
}

const fmtN = (n) => (n == null ? '—' : Number(n).toLocaleString('pt-BR'));
/** Detalhes do SMART de um disco. */
export function smartDialog(s) {
  const [label, cls] = HEALTH[s.health] || ['—', ''];
  const row = (k, v, bad) => `<div class="kv ${bad ? 'bad' : ''}"><span>${k}</span><span>${v}</span></div>`;
  const years = s.powerOnHours != null ? ` (${(s.powerOnHours / 8766).toLocaleString('pt-BR', { maximumFractionDigits: 1 })} anos)` : '';
  const text = { ok: 'O disco não relata problemas.', atencao: 'Apareceram setores com problema. Não é falha imediata, mas é o sinal clássico de que o disco começou a se desgastar: confira o backup do que está nele.', falhando: 'O próprio disco avisa que está falhando. Copie o que está nele para outro lugar o quanto antes.' }[s.health] || '';
  const d = openDialog(`<h2>${svg('shield')}Saúde de ${esc(s.dev)}</h2>
    <p class="dlg-text"><span class="smart-badge ${cls}">${label}</span> ${esc(s.model || '')}${s.serial ? ` · série ${esc(s.serial)}` : ''}</p>
    <p class="dlg-text">${esc(text)}</p>
    <div class="card smart-card">
      ${row('Temperatura', s.temp != null ? `${Math.round(s.temp)} °C` : '—')}
      ${row('Horas ligado', `${fmtN(s.powerOnHours)}${years}`)}
      ${row('Vezes ligado', fmtN(s.powerCycles))}
      ${row('Setores realocados', fmtN(s.reallocated), s.reallocated > 0)}
      ${row('Setores pendentes', fmtN(s.pending), s.pending > 0)}
      ${row('Setores incorrigíveis', fmtN(s.uncorrectable), s.uncorrectable > 0)}
      ${row('Erros de cabo (CRC)', fmtN(s.crcErrors))}
      ${s.percentUsed != null ? row('Desgaste (NVMe)', `${s.percentUsed}%`, s.percentUsed >= 90) + row('Erros de mídia (NVMe)', fmtN(s.mediaErrors), s.mediaErrors > 0) : ''}
      ${row('Última leitura completa', s.checkedAt ? new Date(s.checkedAt * 1000).toLocaleString('pt-BR') : '—')}
    </div>
    ${(s.history || []).length > 1 ? `<p class="dlg-text"><small>Mudanças: ${s.history.map((h) => `${new Date(h.at * 1000).toLocaleDateString('pt-BR')}: ${h.reallocated} realocados, ${h.pending} pendentes`).map(esc).join(' · ')}</small></p>` : ''}
    <p class="dlg-text"><small>Erros de cabo (CRC) costumam ser cabo ou adaptador USB ruim, não o disco.</small></p>
    <div class="dlg-actions"><button class="pill-btn primary" data-c="ok">Fechar</button></div>`, { width: 480 });
  d.el.addEventListener('click', (e) => { if (e.target.closest('[data-c="ok"]')) d.close(); });
}

const TEMP_CMDS = 'sudo modprobe drivetemp\necho drivetemp | sudo tee /etc/modules-load.d/drivetemp.conf';

export function driveTempHelp() {
  const d = openDialog(`<h2>${svg('disks')}Temperatura dos HDs</h2>
    <p class="dlg-text">O Linux tem um driver chamado <b>drivetemp</b> que lê a temperatura dos HDs e SSDs SATA, mas o Ubuntu não o carrega sozinho. Rode estes dois comandos no servidor (o segundo faz ele voltar depois de reiniciar):</p>
    <pre class="pw-cmds">${esc(TEMP_CMDS)}</pre>
    <p class="dlg-text"><small>A temperatura aparece em até 1 minuto, sem reiniciar o BrunOS. Para não acordar um HD parado, o BrunOS só mede de novo quando o disco está em uso; parado, ele mostra a última medida. Discos NVMe já mostram a temperatura sem nada disso.</small></p>
    <div class="dlg-actions"><button class="pill-btn" data-c="copy">${svg('copy')}Copiar comandos</button><button class="pill-btn primary" data-c="ok">Entendi</button></div>`, { width: 520 });
  d.el.addEventListener('click', async (e) => {
    const c = e.target.closest('[data-c]')?.dataset.c;
    if (c === 'ok') d.close();
    else if (c === 'copy') {
      try { await navigator.clipboard.writeText(TEMP_CMDS); toast('Comandos copiados', 'Cole no terminal do servidor.'); }
      catch { const r = document.createRange(); r.selectNodeContents($('.pw-cmds', d.el)); getSelection().removeAllRanges(); getSelection().addRange(r); toast('Selecionei os comandos', 'Copie com Ctrl+C.'); }
    }
  });
}
