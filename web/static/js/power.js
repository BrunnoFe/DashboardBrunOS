// Energia: faixa de contagem regressiva do desligamento seguro, janela de configuração
// (com o assistente que cria o usuário restrito no servidor) e a lista de quedas de energia.
import { api } from './api.js';
import { $, esc, html, svg, toast, toastError, openDialog, confirmDialog } from './ui.js';
import { fmtMinutes } from './battery.js';
import { mountEnergy } from './energy.js';

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
    let dur = o.end ? fmtMinutes((o.end - o.start) / 60) || 'menos de 1 min' : 'agora, na bateria';
    if (o.off) dur += ' · apagou';
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
  let cfg = { ...data.config }, setup = null, dirty = false, needsUpdate = !!data.needsUpdate, showUpdate = false;
  let charge = data.charge || { kind: 'none' };
  let chargeLimit = charge.want || (charge.current && charge.current < 100 ? charge.current : 80);
  let chargeOn = !!charge.want;
  const d = openDialog(`<h2>${svg('bolt')}Energia</h2>
    <p class="pw-intro">O notebook é o servidor e a bateria é o nobreak. Se a energia cair e a bateria chegar ao limite, o BrunOS avisa, espera um pouco (dá para cancelar) e desliga o servidor com segurança, antes de a bateria acabar.</p>
    <div class="pw-state" data-state></div>
    <div class="s-group-title">Consumo e custo</div>
    <div class="s-group pw-group en-box" data-energy><div class="empty-note">Carregando…</div></div>
    <div class="s-group-title">Desligamento seguro</div>
    <div class="s-group pw-group" data-cfg></div>
    <div class="s-group-title">Saúde da bateria</div>
    <div class="s-group pw-group" data-charge></div>
    <div class="s-group-title">Conexão com o servidor</div>
    <div class="s-group pw-group" data-conn></div>
    <div class="s-group-title">Quedas de energia</div>
    <div data-outages>${outagesHTML(data.outages)}</div>
    <div class="dlg-actions"><button class="pill-btn" data-c="close">Fechar</button><button class="pill-btn primary" data-c="save" disabled>Salvar</button></div>`,
  { width: 620, closeBtn: false, onClose: () => { open = false; } });

  mountEnergy($('[data-energy]', d.el));
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
    // servidor configurado com o comando antigo (shutdown -h +1, recusado pelo Ubuntu 26.04)
    const update = needsUpdate ? `<div class="pw-step pw-update"><span class="pw-n">${svg('alert')}</span><div class="pw-step-body"><b>Atualize a configuração do servidor</b>
        <small>O Ubuntu passou a recusar o comando de desligar que o BrunOS usava, porque as atualizações automáticas seguram o desligamento. Os comandos novos instalam um script pequeno (<code>/usr/local/sbin/brunos-host</code>) que só aceita desligar e ajustar o limite de carga, e trocam a chave do usuário <code>${esc(cfg.user)}</code>. Depois de colar, clique em <b>Testar</b> no passo 2: o teste confirma a atualização.</small>
        ${showUpdate && setup ? `<pre class="pw-cmds" tabindex="0">${esc(setup.updateCommands)}</pre>
          <div class="pw-row"><button class="pill-btn" data-c="copy-update">${svg('copy')}Copiar comandos</button>${openTerminal ? `<button class="pill-btn" data-c="term">${svg('terminal')}Abrir terminal</button>` : ''}</div>
          <small class="hint">O último comando (<code>visudo -c</code>) deve responder “parsed OK”. Enquanto você não atualizar, o desligamento continua usando a chave antiga.</small>`
        : `<div class="pw-row"><button class="pill-btn primary" data-c="show-update">${svg('key')}Mostrar comandos novos</button></div>`}
      </div></div>` : '';
    $('[data-conn]', d.el).innerHTML = `${update}
      <div class="pw-step"><span class="pw-n">1</span><div class="pw-step-body"><b>Crie no servidor o usuário que só consegue desligar</b>
        <small>O BrunOS tem uma chave própria. Os comandos criam o usuário <code>${esc(cfg.user)}</code>, sem senha, que só aceita essa chave e só pode desligar o servidor. O container continua sem nenhum privilégio.</small>
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
  // Limite de carga: o servidor detecta o que o notebook oferece (contínuo, liga/desliga ou nada)
  const drawCharge = () => {
    const box = $('[data-charge]', d.el);
    const model = charge.model ? ` (${esc(charge.model)})` : '';
    const notReady = charge.ready ? '' : '<small class="pw-charge-note">Para ajustar, configure a conexão com o servidor abaixo e clique em Testar.</small>';
    if (charge.kind === 'range') {
      const p = ((chargeLimit - 50) / 45) * 100;
      const now = charge.current && charge.current < 100 ? `Agora o notebook para de carregar em ${charge.current}%.` : 'Agora carrega até 100%.';
      box.innerHTML = `
        <div class="s-row"><span class="s-ico">${svg('battery')}</span><span class="s-text"><b>Limitar a carga</b><small>Parar de carregar antes de 100% faz a bateria durar mais anos. ${now}</small>${notReady}</span><span class="s-ctrl"><button class="switch ${chargeOn ? 'on' : ''}" data-csw="range" role="switch" aria-checked="${chargeOn}" ${charge.ready ? '' : 'disabled'}></button></span></div>
        <div class="s-row ${chargeOn ? '' : 'dim'}"><span class="s-ico">${svg('plug')}</span><span class="s-text"><b>Parar de carregar em</b><small>80% é um bom equilíbrio. Volta a carregar 5 pontos abaixo.</small></span><span class="s-ctrl"><input type="range" min="50" max="95" step="5" value="${chargeLimit}" data-climit style="--p:${p}%" aria-label="Limite de carga" ${chargeOn && charge.ready ? '' : 'disabled'}><span class="range-val">${chargeLimit}%</span><button class="pill-btn primary" data-c="charge-apply" ${chargeOn && charge.ready && chargeLimit !== charge.want ? '' : 'disabled'}>Aplicar</button></span></div>`;
    } else if (charge.kind === 'toggle') {
      box.innerHTML = `<div class="s-row"><span class="s-ico">${svg('battery')}</span><span class="s-text"><b>Proteção de carga</b><small>O notebook para de carregar num limite definido pelo fabricante (em geral entre 60% e 80%). Agora: ${charge.on ? 'ligada' : 'desligada'}.</small>${notReady}</span><span class="s-ctrl"><button class="switch ${charge.on ? 'on' : ''}" data-csw="toggle" role="switch" aria-checked="${!!charge.on}" ${charge.ready ? '' : 'disabled'}></button></span></div>`;
    } else {
      box.innerHTML = `<div class="s-row pw-unsupported"><span class="s-ico">${svg('battery')}</span><span class="s-text"><b>Limite de carga</b><small>Este notebook${model} não permite limitar a carga pelo Linux (em alguns modelos, só pela BIOS). O BrunOS confere isso uma vez, quando liga.</small></span><span class="s-ctrl"><button class="pill-btn" data-c="charge-detect">Verificar de novo</button></span></div>`;
    }
  };
  const applyCharge = async (body, btn) => {
    if (btn) btn.disabled = true;
    try {
      const r = await api.power.charge(body);
      charge = r.charge; chargeOn = !!charge.want;
      toast('Limite de carga aplicado', charge.kind === 'toggle' ? `Proteção ${charge.on ? 'ligada' : 'desligada'}.` : charge.want ? `O notebook para de carregar em ${charge.current}%.` : 'O notebook volta a carregar até 100%.');
    } catch (err) {
      if (err.data?.charge) charge = err.data.charge;
      chargeOn = !!charge.want;
      toastError(err, 'Não deu para aplicar');
    }
    drawCharge();
  };
  const markDirty = () => { dirty = true; $('[data-c="save"]', d.el).disabled = false; };
  drawState(data.status); drawCfg(); drawCharge(); drawConn();

  const runTest = async (trust = '') => {
    const val = (k) => $(`[data-in="${k}"]`, d.el).value.trim();
    const body = { host: val('host'), port: parseInt(val('port'), 10) || 22, user: val('user') || 'brunos-power', trust };
    const box = $('[data-test]', d.el), btn = $('[data-c="test"]', d.el);
    box.className = 'pw-test'; box.textContent = 'Testando…'; btn.disabled = true;
    try {
      const r = await api.power.test(body);
      cfg = { ...cfg, host: r.config.host, port: r.config.port, user: r.config.user, testedAt: r.config.testedAt, hostSetup: r.config.hostSetup };
      needsUpdate = false; charge = { ...charge, ready: true };
      drawConn(); drawCfg(); drawCharge();
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
    const t = e.target.closest('[data-sw],[data-csw],[data-grace],[data-c]'); if (!t || t.disabled) return;
    if (t.dataset.sw) { cfg[t.dataset.sw] = !cfg[t.dataset.sw]; drawCfg(); markDirty(); return; }
    if (t.dataset.csw === 'toggle') { await applyCharge({ protect: charge.on ? 'off' : 'on' }, t); return; }
    if (t.dataset.csw === 'range') {
      if (chargeOn && charge.want) { await applyCharge({ limit: 100 }, t); return; } // desligar = carregar até 100%
      chargeOn = !chargeOn; drawCharge(); return; // ligar: escolhe o valor e clica em Aplicar
    }
    if (t.dataset.grace) { cfg.graceSec = +t.dataset.grace; drawCfg(); markDirty(); return; }
    const c = t.dataset.c;
    if (c === 'close') {
      if (dirty && !(await confirmDialog('Sair sem salvar?', 'As mudanças na configuração de energia serão perdidas.', 'Sair'))) return;
      d.close();
    } else if (c === 'setup') {
      try { setup = await api.power.setup(); cfg.user = setup.user || cfg.user; drawConn(); } catch (err) { toastError(err, 'Não deu para criar a chave'); }
    } else if (c === 'show-update') {
      try { setup = setup || await api.power.setup(); showUpdate = true; drawConn(); } catch (err) { toastError(err, 'Não deu para montar os comandos'); }
    } else if (c === 'copy' || c === 'copy-update') {
      const text = c === 'copy' ? setup.commands : setup.updateCommands;
      try { await navigator.clipboard.writeText(text); toast('Comandos copiados', 'Cole no terminal do servidor.'); }
      catch { const pre = t.closest('.pw-step-body').querySelector('.pw-cmds'); const r = document.createRange(); r.selectNodeContents(pre); getSelection().removeAllRanges(); getSelection().addRange(r); toast('Selecionei os comandos', 'Copie com Ctrl+C.'); }
    } else if (c === 'charge-detect') {
      try { const r = await api.power.charge({ detect: true }); charge = r.charge; drawCharge(); toast('Verificado', charge.kind === 'none' ? 'Este notebook continua sem limite de carga.' : 'Limite de carga disponível.'); }
      catch (err) { toastError(err, 'Não deu para verificar'); }
    } else if (c === 'charge-apply') {
      await applyCharge({ limit: chargeLimit }, t);
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
    } else if (e.target.matches('[data-climit]')) {
      chargeLimit = +e.target.value;
      e.target.style.setProperty('--p', ((chargeLimit - 50) / 45) * 100 + '%');
      e.target.nextElementSibling.textContent = chargeLimit + '%';
      const b = $('[data-c="charge-apply"]', d.el); if (b) b.disabled = chargeLimit === charge.want;
    } else if (e.target.matches('[data-in]')) markDirty();
  });
  // mantém o estado atualizado enquanto a janela está aberta
  const poll = setInterval(async () => {
    if (!open) { clearInterval(poll); return; }
    try { const r = await api.power.get(); drawState(r.status); $('[data-outages]', d.el).innerHTML = outagesHTML(r.outages); } catch { /* próxima */ }
  }, 10000);
}
