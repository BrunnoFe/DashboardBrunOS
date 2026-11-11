// Janela "Detalhes do app": estado, disponibilidade e uso nas últimas 24 h, logs dos containers e
// ações (ligar, parar, reiniciar; desinstalar abre a página do app no painel do Runtipi).
import { api } from './api.js';
import { $, esc, fmt, svg, toast, toastError, openDialog, confirmDialog, contextMenu } from './ui.js';

const STATE = { running: 'Rodando', stopped: 'Parado', partial: 'Parcialmente rodando', unknown: 'Sem informação' };
// tira os códigos de cor do terminal (\x1b[...m) que muitos apps põem nos logs
const ANSI = /\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07]*\x07/g;
const cleanLog = (s) => s.replace(ANSI, '').replace(/\r/g, '');
// "2026-11-08T01:23:45.123456789Z mensagem" → "08/11 01:23:45  mensagem" (hora local)
function prettyLine(l) {
  const m = /^(\d{4}-\d\d-\d\dT[\d:.]+Z)\s?(.*)$/.exec(l);
  if (!m) return l;
  const d = new Date(m[1]);
  if (isNaN(d)) return l;
  const p = (n) => String(n).padStart(2, '0');
  return `${p(d.getDate())}/${p(d.getMonth() + 1)} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}  ${m[2]}`;
}

function timelineHTML(points) {
  // 96 blocos de 15 min: verde = no ar, vermelho = caiu em algum momento, cinza = parado/sem medição
  const now = Math.floor(Date.now() / 60000) * 60, start = now - 1440 * 60;
  const blocks = Array.from({ length: 96 }, () => ({ up: 0, down: 0 }));
  for (const p of points) {
    const i = Math.floor((p.t - start) / 900);
    if (i < 0 || i > 95) continue;
    if (p.up === 1) blocks[i].up++;
    else if (p.up === 0) blocks[i].down++;
  }
  return blocks.map((b, i) => {
    const t = new Date((start + i * 900) * 1000), hh = `${String(t.getHours()).padStart(2, '0')}:${String(t.getMinutes()).padStart(2, '0')}`;
    const cls = b.down ? 'down' : b.up ? 'up' : '';
    const title = b.down ? `${hh}: fora do ar por ~${b.down} min` : b.up ? `${hh}: no ar` : `${hh}: parado ou sem medição`;
    return `<i class="${cls}" title="${title}"></i>`;
  }).join('');
}

function chartSVG(points, key) {
  const pts = points.filter((p) => p.up !== -1 || p[key] > 0);
  if (pts.length < 2) return '<div class="ai-empty">Ainda sem dados. O BrunOS mede a cada minuto; volte daqui a pouco.</div>';
  const now = Date.now() / 1000, start = now - 86400;
  const vals = pts.map((p) => p[key]), max = Math.max(...vals, key === 'cpu' ? 1 : 10);
  // memória quase não varia: a escala começa um pouco abaixo do mínimo para a variação aparecer
  const lo = key === 'mem' ? Math.min(...vals) * 0.85 : 0, span = Math.max(max - lo, key === 'cpu' ? 1 : 10);
  const x = (t) => ((t - start) / 86400) * 600, y = (v) => 96 - ((v - lo) / span) * 88;
  let d = '', prev = null;
  for (const p of pts) { // buracos (minutos sem medição) quebram a linha
    d += `${prev && p.t - prev.t <= 180 ? 'L' : 'M'}${x(p.t).toFixed(1)} ${y(p[key]).toFixed(1)}`;
    prev = p;
  }
  const avg = vals.reduce((s, v) => s + v, 0) / vals.length;
  const unit = key === 'cpu' ? '%' : ' MB';
  const hours = [0, 6, 12, 18, 24].map((h) => { const t = new Date((start + h * 3600) * 1000); return `<span style="left:${(h / 24) * 100}%">${h === 24 ? 'agora' : `${String(t.getHours()).padStart(2, '0')}h`}</span>`; }).join('');
  return `<div class="ai-chart-meta"><span>máx. <b>${fmt(max, key === 'cpu' ? 1 : 0)}${unit}</b></span><span>média <b>${fmt(avg, key === 'cpu' ? 1 : 0)}${unit}</b></span></div>
    <svg class="ai-chart" viewBox="0 0 600 100" preserveAspectRatio="none"><path d="${d}" fill="none" stroke="var(--brand-lightest)" stroke-width="1.6" vector-effect="non-scaling-stroke"/></svg>
    <div class="ai-hours">${hours}</div>`;
}

/**
 * ctx: { iconHTML(a), appUrl(a), openApp(a), runtipiBase(), dockerActions, monitored(a), setMonitored(a, on), reload() }
 */
export function appDetails(a, ctx) {
  let containers = [], cid = '', stopFollow = null, history = null, metric = 'cpu';
  const tipi = a.kind === 'runtipi';
  const d = openDialog(`<div class="ai">
      <div class="ai-head">${ctx.iconHTML(a)}<div class="ai-title"><b>${esc(a.name)}</b><small>${tipi ? `${esc(a.store || '')}${a.version ? ` · versão ${esc(a.version)}` : ''}` : 'Atalho'}</small><span class="ai-state"></span></div>
        <span class="f-spacer"></span><button class="icon-btn" data-ai="more" aria-label="Mais opções">${svg('more')}</button></div>
      <div class="ai-actions">
        <button class="pill-btn primary" data-ai="open">${svg('external')}Abrir</button>
        ${tipi ? `<button class="pill-btn" data-ai="start" hidden>${svg('power')}Ligar</button><button class="pill-btn" data-ai="stop" hidden>${svg('power')}Parar</button><button class="pill-btn" data-ai="restart" hidden>${svg('refresh')}Reiniciar</button>` : ''}
      </div>
      ${tipi && a.update ? `<div class="ai-update">${svg('arrowUp')}<span><b>Atualização disponível</b><small>versão ${esc(a.update.installed || '?')} → ${esc(a.update.available || '?')}</small></span><span class="f-spacer"></span><button class="pill-btn primary" data-ai="update">${svg('external')}Atualizar no Runtipi</button></div>` : ''}
      ${tipi && !ctx.dockerActions ? '<p class="ai-note">Para ligar, parar e reiniciar por aqui, ative “Permitir reiniciar apps pelo BrunOS” nas configurações do BrunOS no Runtipi.</p>' : ''}
      ${tipi ? `<div class="ai-sec"><div class="ai-sec-h"><b>Disponibilidade</b><small>últimas 24 h</small><span class="f-spacer"></span><span class="ai-uptime"></span></div><div class="ai-timeline"></div></div>
      <div class="ai-sec"><div class="ai-sec-h"><b>Uso</b><small>últimas 24 h, a cada minuto</small><span class="f-spacer"></span>
        <div class="seg"><button class="on" data-metric="cpu">CPU</button><button data-metric="mem">Memória</button></div></div><div class="ai-chart-box"></div></div>
      <div class="ai-sec ai-logs-sec"><div class="ai-sec-h"><b>Logs</b><span class="ai-ct"></span><span class="f-spacer"></span>
        <button class="pill-btn sm" data-ai="follow">${svg('activity')}Ao vivo</button><button class="pill-btn sm" data-ai="reload">${svg('refresh')}</button><button class="pill-btn sm" data-ai="copy">${svg('copy')}</button></div>
        <pre class="ai-logs">Carregando…</pre></div>` : `<p class="ai-note">${esc(ctx.appUrl(a) || 'Sem endereço')}</p>`}
    </div>`, { cls: 'ai-dialog', width: 820, onClose: () => stopFollow?.() });

  const paintState = () => {
    const down = a.health === 'down';
    $('.ai-state', d.el).innerHTML = tipi ? `<i class="status-dot ${a.state} ${down ? 'down' : ''}"></i>${down ? 'Fora do ar' : STATE[a.state] || ''}` : '';
    if (!tipi) return;
    const can = ctx.dockerActions;
    $('[data-ai="start"]', d.el).hidden = !can || a.state === 'running';
    $('[data-ai="stop"]', d.el).hidden = !can || a.state === 'stopped';
    $('[data-ai="restart"]', d.el).hidden = !can || a.state === 'stopped';
  };
  paintState();
  if (!tipi) { wire(); return; }

  const logBox = $('.ai-logs', d.el);
  const atBottom = () => logBox.scrollHeight - logBox.scrollTop - logBox.clientHeight < 40;
  const appendLines = (lines) => {
    const stick = atBottom();
    logBox.append(document.createTextNode(lines.map((l) => prettyLine(cleanLog(l))).join('\n') + '\n'));
    // não deixa a caixa crescer sem fim acompanhando ao vivo
    if (logBox.textContent.length > 600_000) logBox.textContent = logBox.textContent.slice(-400_000);
    if (stick) logBox.scrollTop = logBox.scrollHeight;
  };
  const loadLogs = async () => {
    stopFollow?.(); stopFollow = null; $('[data-ai="follow"]', d.el).classList.remove('on');
    logBox.textContent = 'Carregando…';
    try {
      const text = await api.app.logs(a.urn, cid, 400);
      logBox.textContent = '';
      const lines = text.split('\n').filter(Boolean);
      if (lines.length) appendLines(lines); else logBox.textContent = 'Sem linhas de log.';
      logBox.scrollTop = logBox.scrollHeight;
    } catch (e) { logBox.textContent = e.message; }
  };
  const drawContainers = () => {
    const box = $('.ai-ct', d.el);
    if (containers.length <= 1) { box.innerHTML = containers[0] ? `<small>${esc(containers[0].name)}</small>` : ''; return; }
    box.innerHTML = `<select class="text-input sm" data-ai="ct">${containers.map((c) => `<option value="${esc(c.id)}" ${c.id === cid ? 'selected' : ''}>${esc(c.service || c.name)}${c.state !== 'running' ? ` (${esc(c.state)})` : ''}</option>`).join('')}</select>`;
  };
  const drawHistory = () => {
    if (!history) return;
    $('.ai-timeline', d.el).innerHTML = timelineHTML(history.points);
    $('.ai-uptime', d.el).innerHTML = history.uptime != null ? `<b>${fmt(history.uptime, history.uptime === 100 ? 0 : 2)}%</b> no ar` : '<small>sem medição ainda</small>';
    $('.ai-chart-box', d.el).innerHTML = chartSVG(history.points, metric);
  };

  (async () => {
    try { history = await api.app.history(a.urn); drawHistory(); } catch { /* sem histórico */ }
    try {
      containers = await api.app.containers(a.urn);
      cid = (containers.find((c) => c.service === a.id) || containers.find((c) => c.state === 'running') || containers[0])?.id || '';
      drawContainers();
      if (cid) loadLogs(); else logBox.textContent = 'Nenhum container encontrado para este app.';
    } catch (e) { logBox.textContent = e.message; }
  })();

  const action = async (kind) => {
    const verbs = { start: ['Ligar', 'ligado', 'ligar'], stop: ['Parar', 'parado', 'parar'], restart: ['Reiniciar', 'reiniciado', 'reiniciar'] }[kind];
    const note = kind === 'stop' ? 'Pelo BrunOS o Runtipi não fica sabendo: se o servidor ou o Runtipi reiniciar, o app volta a ligar. Para parar de vez, pare pelo painel do Runtipi.' : kind === 'start' ? 'Os containers do app serão ligados.' : 'Os containers do app serão reiniciados. Leva alguns segundos.';
    if (!(await confirmDialog(`${verbs[0]} ${a.name}?`, note, verbs[0], { danger: kind === 'stop' }))) return;
    const btn = $(`[data-ai="${kind}"]`, d.el); btn.disabled = true;
    try {
      await (kind === 'restart' ? api.restart(a.urn) : api.app[kind](a.urn));
      toast(`${a.name} ${verbs[1]}`);
      a.state = kind === 'stop' ? 'stopped' : 'running'; if (kind === 'stop') a.health = 'unknown';
      paintState(); ctx.reload?.();
      setTimeout(async () => { try { containers = await api.app.containers(a.urn); drawContainers(); } catch { /* ignora */ } }, 1500);
    } catch (e) { toastError(e, `Não foi possível ${verbs[2]}`); }
    btn.disabled = false;
  };

  function wire() {
    d.el.addEventListener('click', async (e) => {
      const m = e.target.closest('[data-metric]');
      if (m) { metric = m.dataset.metric; d.el.querySelectorAll('[data-metric]').forEach((x) => x.classList.toggle('on', x === m)); drawHistory(); return; }
      const b = e.target.closest('[data-ai]'); if (!b || b.disabled || b.tagName === 'SELECT') return;
      const k = b.dataset.ai;
      if (k === 'open') ctx.openApp(a);
      else if (k === 'update') window.open(`${ctx.runtipiBase()}/apps/${encodeURIComponent(a.update.store)}/${encodeURIComponent(a.update.id)}`, '_blank', 'noopener');
      else if (k === 'start' || k === 'stop' || k === 'restart') action(k);
      else if (k === 'reload') loadLogs();
      else if (k === 'copy') { try { await navigator.clipboard.writeText(logBox.textContent); toast('Logs copiados'); } catch { toast('Não deu para copiar'); } }
      else if (k === 'follow') {
        if (stopFollow) { stopFollow(); stopFollow = null; b.classList.remove('on'); return; }
        b.classList.add('on');
        logBox.scrollTop = logBox.scrollHeight;
        stopFollow = api.app.follow(a.urn, cid, (line) => appendLines([line]), () => { b.classList.remove('on'); stopFollow = null; });
      } else if (k === 'more') {
        const r = b.getBoundingClientRect(), mon = ctx.monitored(a);
        contextMenu(r.right - 220, r.bottom + 6, [
          { icon: 'box', label: 'Abrir no Runtipi', hidden: !tipi, run: () => window.open(`${ctx.runtipiBase()}/apps/${encodeURIComponent(a.store)}/${encodeURIComponent(a.id)}`, '_blank', 'noopener') },
          { icon: 'bell', label: mon ? 'Não avisar se este app cair' : 'Avisar se este app cair', hidden: !tipi, run: () => { ctx.setMonitored(a, !mon); toast(mon ? 'Sem avisos deste app' : 'Avisos ligados', mon ? `${a.name} não vai mais gerar aviso de queda.` : ''); } },
          '-',
          { icon: 'trash', label: 'Desinstalar…', danger: true, hidden: !tipi, run: uninstall },
        ]);
      }
    });
    d.el.addEventListener('change', (e) => { if (e.target.matches('[data-ai="ct"]')) { cid = e.target.value; loadLogs(); } });
  }
  async function uninstall() {
    const url = `${ctx.runtipiBase()}/apps/${encodeURIComponent(a.store)}/${encodeURIComponent(a.id)}`;
    if (!(await confirmDialog(`Desinstalar ${a.name}?`, 'A desinstalação é feita pelo Runtipi, que remove os containers e, se você pedir, os dados do app. Vou abrir a página do app no painel do Runtipi: lá, use o menu do app → “Desinstalar”.', 'Abrir no Runtipi', { danger: true }))) return;
    window.open(url, '_blank', 'noopener');
  }
  wire();
}
