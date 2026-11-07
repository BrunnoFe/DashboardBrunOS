// Energia: faixa de contagem regressiva do desligamento seguro, janela de configuração
// (com o assistente que cria o usuário restrito no servidor) e a lista de quedas de energia.
import { api } from './api.js';
import { $, esc, html, svg, toast, toastError, openDialog, confirmDialog } from './ui.js';
import { fmtMinutes } from './battery.js';

const PHASE = {
  off: ['Desativado', 'off'], armed: ['Vigiando a bateria', 'ok'], countdown: ['Contagem regressiva', 'crit'],
  running: ['Desligando agora', 'crit'], done: ['Já agiu nesta queda de energia', 'ok'], cancelled: ['Cancelado nesta queda', 'off'], failed: ['Não conseguiu desligar', 'crit'],
};
const SHUT = { simulado: 'simulação', executado: 'desligou', cancelado: 'cancelado', falhou: 'falhou' };

/* ── Faixa da contagem regressiva ──────────────────────────────── */
let banner = null, bannerTimer = 0;
export async function refreshPowerBanner() {
  try { const r = await api.power.get(); paintBanner(r.status); return r; } catch { return null; }
}
export function paintBanner(st) {
  clearInterval(bannerTimer);
  if (!st || st.phase !== 'countdown') { banner?.remove(); banner = null; return; }
  const endAt = Date.now() + (st.remaining ?? 0) * 1000;
  if (!banner) {
    banner = html(`<div class="power-banner" role="alert"><span class="pb-ico">${svg('bolt')}</span><span class="pb-text"><b></b><small></small></span><button class="pill-btn" data-pb="cancel">Cancelar</button></div>`);
    document.body.append(banner);
    $('[data-pb="cancel"]', banner).onclick = async () => {
      try { const r = await api.power.cancel(); paintBanner(r.status); toast('Desligamento cancelado', 'O BrunOS volta a vigiar na próxima queda de energia.'); }
      catch (e) { toastError(e, 'Não deu para cancelar'); }
    };
  }
  banner.classList.toggle('sim', !!st.simulate);
  $('small', banner).textContent = st.simulate
    ? `Modo simulação: nada será desligado de verdade · bateria em ${st.percent}%`
    : `Sem energia da tomada e bateria em ${st.percent}%`;
  const tick = () => {
    const s = Math.max(0, Math.round((endAt - Date.now()) / 1000));
    const mmss = `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
    $('b', banner).textContent = s > 0 ? `${st.simulate ? 'Simulação: o servidor desligaria' : 'O servidor vai desligar'} em ${mmss}` : (st.simulate ? 'Simulação: desligaria agora' : 'Desligando o servidor…');
    if (s <= 0) { clearInterval(bannerTimer); setTimeout(refreshPowerBanner, 6000); }
  };
  tick();
  bannerTimer = setInterval(tick, 1000);
}

/* ── Quedas de energia ─────────────────────────────────────────── */
export function outagesHTML(list, max = 8) {
  if (!list?.length) return '<div class="empty-note">Nenhuma queda de energia registrada ainda.</div>';
  return `<div class="outage-list">${list.slice(0, max).map((o) => {
    const d = new Date(o.start * 1000);
    const when = d.toLocaleDateString('pt-BR', { day: '2-digit', month: 'short' }) + ' · ' + d.toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' });
    const dur = o.end ? fmtMinutes((o.end - o.start) / 60) || 'menos de 1 min' : 'agora, na bateria';
    return `<div class="outage ${o.end ? '' : 'live'}"><span class="o-ico">${svg(o.end ? 'plug' : 'plugOff')}</span>
      <span class="o-main"><b>${esc(when)}</b><small>${esc(dur)}</small></span>
      ${o.shutdown ? `<span class="o-tag ${esc(o.shutdown)}">${esc(SHUT[o.shutdown] || o.shutdown)}</span>` : ''}
      <span class="o-pct">${o.startPct}% → ${o.end ? o.endPct : o.minPct}%</span></div>`;
  }).join('')}</div>`;
}

/* ── Janela de configuração ────────────────────────────────────── */
let open = false;
export async function powerDialog({ openTerminal } = {}) {
  if (open) return;
  let data;
  try { data = await api.power.get(); } catch (e) { toastError(e, 'Não deu para abrir a energia'); return; }
  open = true;
  let cfg = { ...data.config }, setup = null, dirty = false;
  const d = openDialog(`<h2>${svg('bolt')}Energia</h2>
    <p class="pw-intro">O notebook é o servidor e a bateria é o nobreak. Se a energia cair e a bateria chegar ao limite, o BrunOS avisa, espera um pouco (dá para cancelar) e desliga o servidor com segurança, antes de a bateria acabar.</p>
    <div class="pw-state" data-state></div>
    <div class="s-group pw-group" data-cfg></div>
    <div class="s-group-title">Conexão com o servidor</div>
    <div class="s-group pw-group" data-conn></div>
    <div class="s-group-title">Quedas de energia</div>
    <div data-outages>${outagesHTML(data.outages)}</div>
    <div class="dlg-actions"><button class="pill-btn" data-c="close">Fechar</button><button class="pill-btn primary" data-c="save" disabled>Salvar</button></div>`,
  { width: 620, closeBtn: false, onClose: () => { open = false; } });

  const tested = () => !!cfg.testedAt;
  const drawState = (st) => {
    const [label, cls] = PHASE[st.phase] || [st.phase, 'off'];
    const detail = !st.hasBattery ? 'Nenhuma bateria encontrada neste servidor.'
      : st.onBattery ? `Na bateria agora (${st.percent}%).` : `Na tomada (${st.percent}%).`;
    $('[data-state]', d.el).innerHTML = `<span class="pw-dot ${cls}"></span><b>${esc(label)}</b><span>${esc(detail)}${st.simulate && st.phase !== 'off' ? ' Modo simulação ligado.' : ''}</span>`;
  };
  const sw = (key, on, disabled = false) => `<button class="switch ${on ? 'on' : ''}" data-sw="${key}" role="switch" aria-checked="${!!on}" ${disabled ? 'disabled' : ''}></button>`;
  const drawCfg = () => {
    const p = ((cfg.threshold - 5) / 45) * 100;
    $('[data-cfg]', d.el).innerHTML = `
      <div class="s-row"><span class="s-ico">${svg('power')}</span><span class="s-text"><b>Desligamento seguro</b><small>Desliga o servidor quando a bateria chegar ao limite sem energia da tomada</small></span><span class="s-ctrl">${sw('enabled', cfg.enabled)}</span></div>
      <div class="s-row ${cfg.enabled ? '' : 'dim'}"><span class="s-ico">${svg('battery')}</span><span class="s-text"><b>Desligar com a bateria em</b><small>Deixe uma folga: a bateria pode cair mais rápido no fim</small></span><span class="s-ctrl"><input type="range" min="5" max="50" value="${cfg.threshold}" data-threshold style="--p:${p}%" aria-label="Limite da bateria"><span class="range-val">${cfg.threshold}%</span></span></div>
      <div class="s-row ${cfg.enabled ? '' : 'dim'}"><span class="s-ico">${svg('clock')}</span><span class="s-text"><b>Esperar antes de desligar</b><small>Tempo para cancelar (ou para a energia voltar)</small></span><span class="s-ctrl"><div class="seg">${[60, 120, 300, 600].map((s) => `<button class="${cfg.graceSec === s ? 'on' : ''}" data-grace="${s}">${s / 60} min</button>`).join('')}</div></span></div>
      <div class="s-row ${cfg.enabled ? '' : 'dim'}"><span class="s-ico">${svg('shield')}</span><span class="s-text"><b>Modo simulação</b><small>${tested() ? 'Só registra o que faria, sem desligar. Desligue quando quiser que funcione de verdade.' : 'Só registra o que faria, sem desligar. Para desativar, primeiro teste a conexão abaixo.'}</small></span><span class="s-ctrl">${sw('simulate', cfg.simulate, cfg.simulate && !tested())}</span></div>`;
  };
  const drawConn = () => {
    const host = cfg.host || data.suggestedHost || '';
    $('[data-conn]', d.el).innerHTML = `
      <div class="pw-step"><span class="pw-n">1</span><div class="pw-step-body"><b>Crie no servidor o usuário que só consegue desligar</b>
        <small>O BrunOS tem uma chave própria. Os comandos criam o usuário <code>${esc(cfg.user)}</code>, sem senha, que só aceita essa chave e só pode rodar <code>shutdown</code>. O container continua sem nenhum privilégio.</small>
        ${setup ? `<pre class="pw-cmds" tabindex="0">${esc(setup.commands)}</pre>
          <div class="pw-row"><button class="pill-btn" data-c="copy">${svg('copy')}Copiar comandos</button>${openTerminal ? `<button class="pill-btn" data-c="term">${svg('terminal')}Abrir terminal</button>` : ''}</div>
          <small class="hint">O último comando (<code>visudo -c</code>) confere se está tudo certo: deve responder “parsed OK”.</small>`
        : `<div class="pw-row"><button class="pill-btn" data-c="setup">${svg('key')}Mostrar comandos</button></div>`}
      </div></div>
      <div class="pw-step"><span class="pw-n">2</span><div class="pw-step-body"><b>Teste a conexão</b>
        <small>O teste só confere a chave: conecta e sai <b>sem</b> rodar nada (não desliga o servidor).</small>
        <div class="pw-row pw-inputs">
          <label>Servidor<input class="text-input" data-in="host" value="${esc(host)}" placeholder="192.168.1.50"></label>
          <label>Porta<input class="text-input pw-port" data-in="port" value="${esc(cfg.port || 22)}" inputmode="numeric"></label>
          <label>Usuário<input class="text-input" data-in="user" value="${esc(cfg.user || 'brunos-power')}"></label>
          <button class="pill-btn primary" data-c="test">Testar</button>
        </div>
        <div class="pw-test ${tested() ? 'ok' : ''}" data-test>${tested() ? `${svg('check')} Conexão testada em ${esc(new Date(cfg.testedAt * 1000).toLocaleString('pt-BR'))}` : ''}</div>
      </div></div>`;
  };
  const markDirty = () => { dirty = true; $('[data-c="save"]', d.el).disabled = false; };
  drawState(data.status); drawCfg(); drawConn();

  const runTest = async (trust = '') => {
    const val = (k) => $(`[data-in="${k}"]`, d.el).value.trim();
    const body = { host: val('host'), port: parseInt(val('port'), 10) || 22, user: val('user') || 'brunos-power', trust };
    const box = $('[data-test]', d.el), btn = $('[data-c="test"]', d.el);
    box.className = 'pw-test'; box.textContent = 'Testando…'; btn.disabled = true;
    try {
      const r = await api.power.test(body);
      cfg = { ...cfg, host: r.config.host, port: r.config.port, user: r.config.user, testedAt: r.config.testedAt };
      drawConn(); drawCfg();
      toast('Conexão funcionando', 'A chave do BrunOS foi aceita pelo servidor.');
    } catch (e) {
      btn.disabled = false;
      if (e.status === 409 && e.data?.unknownHost) {
        box.textContent = '';
        const ok = await confirmDialog('Confirmar o servidor', `Primeira conexão com ${e.data.addr}. Impressão digital da chave do servidor (${e.data.keyType}): ${e.data.fingerprint}. Confira com "ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub" no servidor.`, 'Confiar e testar');
        if (ok) runTest(e.data.fingerprint);
        return;
      }
      box.className = 'pw-test err';
      box.textContent = e.data?.hostKeyChanged ? 'A chave deste servidor mudou desde a última conexão. Se você reinstalou o sistema, esqueça a chave antiga na janela do Terminal e teste de novo.' : e.message;
    }
  };

  d.el.addEventListener('click', async (e) => {
    const t = e.target.closest('[data-sw],[data-grace],[data-c]'); if (!t || t.disabled) return;
    if (t.dataset.sw) { cfg[t.dataset.sw] = !cfg[t.dataset.sw]; drawCfg(); markDirty(); return; }
    if (t.dataset.grace) { cfg.graceSec = +t.dataset.grace; drawCfg(); markDirty(); return; }
    const c = t.dataset.c;
    if (c === 'close') {
      if (dirty && !(await confirmDialog('Sair sem salvar?', 'As mudanças na configuração de energia serão perdidas.', 'Sair'))) return;
      d.close();
    } else if (c === 'setup') {
      try { setup = await api.power.setup(); cfg.user = setup.user || cfg.user; drawConn(); } catch (err) { toastError(err, 'Não deu para criar a chave'); }
    } else if (c === 'copy') {
      try { await navigator.clipboard.writeText(setup.commands); toast('Comandos copiados', 'Cole no terminal do servidor.'); }
      catch { const pre = $('.pw-cmds', d.el); const r = document.createRange(); r.selectNodeContents(pre); getSelection().removeAllRanges(); getSelection().addRange(r); toast('Selecionei os comandos', 'Copie com Ctrl+C.'); }
    } else if (c === 'term') openTerminal?.();
    else if (c === 'test') runTest();
    else if (c === 'save') {
      const val = (k) => $(`[data-in="${k}"]`, d.el)?.value.trim();
      const body = { ...cfg, host: val('host') ?? cfg.host, port: parseInt(val('port'), 10) || 22, user: val('user') || cfg.user };
      if (body.enabled && !body.simulate && !(await confirmDialog('Desligar de verdade?', `Com o modo simulação desligado, o servidor será desligado quando a bateria chegar a ${body.threshold}% sem energia da tomada.`, 'Ativar'))) return;
      try {
        const r = await api.power.save(body);
        cfg = { ...r.config }; dirty = false; $('[data-c="save"]', d.el).disabled = true;
        drawState(r.status); drawCfg(); drawConn(); paintBanner(r.status);
        toast('Energia configurada', !cfg.enabled ? 'Desligamento seguro desativado.' : cfg.simulate ? `Simulação ligada: avisa com a bateria em ${cfg.threshold}%.` : `Desliga com a bateria em ${cfg.threshold}%.`);
      } catch (err) { toastError(err, 'Não deu para salvar'); }
    }
  });
  d.el.addEventListener('input', (e) => {
    if (e.target.matches('[data-threshold]')) {
      cfg.threshold = +e.target.value;
      e.target.style.setProperty('--p', ((cfg.threshold - 5) / 45) * 100 + '%');
      e.target.nextElementSibling.textContent = cfg.threshold + '%';
      markDirty();
    } else if (e.target.matches('[data-in]')) markDirty();
  });
  // mantém o estado atualizado enquanto a janela está aberta
  const poll = setInterval(async () => {
    if (!open) { clearInterval(poll); return; }
    try { const r = await api.power.get(); drawState(r.status); $('[data-outages]', d.el).innerHTML = outagesHTML(r.outages); } catch { /* próxima */ }
  }, 10000);
}
