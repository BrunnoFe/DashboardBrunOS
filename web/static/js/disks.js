// Discos no Uso ao vivo (resumo: nome, selo do SMART, temperatura e espaço) e peças que a janela Discos
// também usa (previsão de disco cheio, ajuda do drivetemp). Os detalhes ficam na janela Discos (0.15.0).
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

// sda1 → sda, nvme0n1p2 → nvme0n1
export const baseDev = (dev) => String(dev || '').replace('/dev/', '').replace(/^(nvme\d+n\d+)p\d+$/, '$1').replace(/^([a-z]+)\d+$/, '$1');
const HEALTH = { ok: ['OK', 'ok'], atencao: ['Atenção', 'warn'], falhando: ['Falhando', 'bad'] };
function smartBadge(s) {
  if (s?.noSmart) return '<span class="smart-badge bad" title="O adaptador deste disco não deixa ler a saúde (SMART)">sem SMART</span>';
  const h = s && HEALTH[s.health];
  return h ? `<span class="smart-badge ${h[1]}" title="Saúde do disco (SMART): clique para os detalhes">${h[0]}</span>` : '';
}

/** Linhas do Uso ao vivo: cada uma abre o disco na janela Discos (data-open-dev). */
export function diskRowsHTML(disks, forecast = [], smart = null) {
  if (!disks.length) return '<div class="empty-note">Nenhum disco encontrado</div>';
  const fc = Object.fromEntries(forecast.map((f) => [f.name, f]));
  const sm = Object.fromEntries((smart?.disks || []).map((s) => [s.dev, s]));
  return disks.map((k) => {
    const f = fc[k.name], ft = forecastText(f), soon = f?.daysLeft != null && f.daysLeft <= 30, dev = baseDev(k.device);
    return `<button class="disk-row clickable" data-open-dev="${esc(dev)}" title="Abrir ${esc(k.name)} na janela Discos">
      <span class="disk-ico">${svg(k.system ? 'cpu' : 'disk')}</span>
      <span class="disk-name"><b>${esc(k.name)}${tempHTML(k)}${smartBadge(sm[dev])}</b><small>${esc(k.device.replace('/dev/', ''))}${k.model ? ' · ' + esc(k.model) : ''} · ${k.rotational ? 'HD' : 'SSD'}</small>${ft ? `<small class="disk-fc ${soon ? 'soon' : ''}">${esc(ft)}</small>` : ''}</span>
      <span class="track"><i style="width:${(k.used / k.total) * 100}%" class="${k.used / k.total > 0.9 ? 'warn' : ''}"></i></span>
      <span class="disk-val">${fmtBytes(k.used)} <small>/ ${fmtBytes(k.total)}</small></span></button>`;
  }).join('');
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
