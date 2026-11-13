// Janela "Detalhes do app": estado, disponibilidade e uso nas últimas 24 h, logs dos containers e
// ações (ligar, parar, reiniciar; desinstalar abre a página do app no painel do Runtipi).
import { api } from './api.js';
import { mountChart, periodSeg, fmtTime, PERIOD_LONG } from './charts.js';
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

const SPAN = { '1h': 3600, '24h': 86400, '48h': 2 * 86400, '7d': 7 * 86400, '30d': 30 * 86400 };

function timelineHTML(points, range, step) {
  // 96 blocos no período: verde = no ar, vermelho = caiu em algum momento, cinza = parado/sem medição.
  // Nos períodos longos "up" é a fração do tempo no ar (0 a 1) de cada ponto.
  const span = SPAN[range] || 86400, now = Math.floor(Date.now() / 1000), start = now - span, bs = span / 96;
  const blocks = Array.from({ length: 96 }, () => ({ n: 0, down: 0 }));
  for (const p of points) {
    if (p.up == null) continue;
    const i = Math.floor((p.t - start) / bs);
    if (i < 0 || i > 95) continue;
    blocks[i].n++;
    blocks[i].down += (1 - p.up) * (step || 60) / 60; // minutos fora do ar
  }
  return blocks.map((b, i) => {
    const hh = fmtTime(start + i * bs, span);
    const cls = b.down >= 0.5 ? 'down' : b.n ? 'up' : '';
    const title = cls === 'down' ? `${hh}: fora do ar por ~${Math.round(b.down)} min` : b.n ? `${hh}: no ar` : `${hh}: parado ou sem medição`;
    return `<i class="${cls}" title="${title}"></i>`;
  }).join('');
}

/**
 * ctx: { iconHTML(a), appUrl(a), openApp(a), runtipiBase(), dockerActions, monitored(a), setMonitored(a, on), reload() }
 */
export function appDetails(a, ctx) {
  let containers = [], cid = '', stopFollow = null, history = null, metric = 'cpu', range = '24h', chart = null;
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
      ${tipi ? `<div class="ai-period">${periodSeg(range)}</div>
      <div class="ai-sec"><div class="ai-sec-h"><b>Disponibilidade</b><small data-ai-span>${PERIOD_LONG[range]}</small><span class="f-spacer"></span><span class="ai-uptime"></span></div><div class="ai-timeline"></div></div>
      <div class="ai-sec"><div class="ai-sec-h"><b>Uso</b><small data-ai-span>${PERIOD_LONG[range]}</small><span class="ai-chart-meta"></span><span class="f-spacer"></span>
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
    $('.ai-timeline', d.el).innerHTML = timelineHTML(history.points, range, history.step);
    $('.ai-uptime', d.el).innerHTML = history.uptime != null ? `<b>${fmt(history.uptime, history.uptime === 100 ? 0 : 2)}%</b> no ar` : '<small>sem medição ainda</small>';
    const f = metric === 'cpu' ? (v) => `${fmt(v, v > 0 && v < 10 ? 1 : 0)}%` : (v) => `${fmt(v, 0)} MB`;
    const pts = history.points.filter((p) => p[metric] != null).map((p) => ({ t: p.t, v: p[metric], max: p[`${metric}Max`] }));
    chart ||= mountChart($('.ai-chart-box', d.el), [], { h: 110 });
    chart.update([{ pts, cls: 'c0' }], { fmt: f, step: history.step || 60, min: metric === 'cpu' ? 0 : undefined, empty: 'Ainda sem dados. O BrunOS mede a cada minuto; volte daqui a pouco.' });
    const vals = pts.map((p) => p.v);
    $('.ai-chart-meta', d.el).innerHTML = vals.length ? `pico <b>${f(Math.max(...pts.map((p) => p.max ?? p.v)))}</b> · média <b>${f(vals.reduce((s, v) => s + v, 0) / vals.length)}</b>` : '';
  };
  const loadHistory = async () => {
    try { history = await api.app.history(a.urn, range); drawHistory(); } catch { /* sem histórico */ }
  };

  (async () => {
    await loadHistory();
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
      const pe = e.target.closest('[data-period]');
      if (pe) {
        range = pe.dataset.period;
        d.el.querySelectorAll('[data-period]').forEach((x) => x.classList.toggle('on', x === pe));
        d.el.querySelectorAll('[data-ai-span]').forEach((x) => { x.textContent = PERIOD_LONG[range]; });
        loadHistory(); return;
      }
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
