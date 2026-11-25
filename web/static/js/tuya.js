// Assistente de tomadas Tuya (0.31.0): em vez do tinytuya, o BrunOS pede à nuvem da Tuya a lista das
// tomadas (com a chave local), procura na rede de casa e grava a integração. A nuvem só é usada aqui; as
// leituras continuam pela rede de casa. A chave local nunca vem para o navegador.
import { api } from './api.js';
import { $, esc, svg, toast, toastError, openDialog, confirmDialog, fmt } from './ui.js';

const GUIDE = `<details class="ty-guide"><summary>${svg('info')}Como conseguir o Access ID e o Access Secret (uma vez só)</summary>
  <ol>
    <li>Entre em <b>iot.tuya.com</b> e crie uma conta de desenvolvedor (grátis).</li>
    <li>Em <b>Cloud → Development</b>, crie um projeto: tipo <i>Smart Home</i> e, em <i>Data Center</i>, a região do seu app
      (no Brasil, normalmente <b>Western America</b>).</li>
    <li>Na aba <b>Overview</b> do projeto ficam o <b>Access ID</b> e o <b>Access Secret</b>: copie para cá.</li>
    <li>Na aba <b>Devices → Link App Account</b>, clique em <i>Add App Account</i> e leia o QR code com o app
      <b>Smart Life</b> (ou Tuya Smart), em Eu → ícone de leitura no canto.</li>
    <li>Em <b>Cloud → Cloud Services</b>, confira se o <i>IoT Core</i> está ativo.</li>
  </ol>
  <p class="hint"><b>Erros comuns:</b> “região não liberada” → escolha aqui a mesma região do projeto; “conta não ligada” → falta o
  passo 4; “sem permissão” → o plano gratuito do IoT Core venceu (renove em Cloud Services, é grátis).</p>
</details>`;

/** Abre o assistente. onDone: chamado depois de gravar uma tomada (para recarregar as integrações). */
export async function tuyaAssistant({ onDone } = {}) {
  let info, regions = [], plugs = null, busy = false;
  const d = openDialog('<div class="ty"><p class="dlg-text">Carregando…</p></div>', { width: 600, cls: 'ty-dlg' });
  const box = () => $('.ty', d.el);

  const head = `<h2>${svg('plug')} Assistente de tomadas</h2>
    <p class="dlg-text">Encontra as suas tomadas Tuya pela conta do app e pela rede de casa, sem tinytuya. A nuvem da Tuya só é usada
    aqui; o consumo continua sendo lido direto da tomada, sem internet.</p>`;

  const drawLogin = (change) => {
    box().innerHTML = `${head}${GUIDE}
      <form class="form-grid ty-form" onsubmit="return false">
        <label>Região do projeto<select class="text-input" data-f="region">${regions.map((r) => `<option value="${esc(r.id)}" ${r.id === (info?.region || 'us') ? 'selected' : ''}>${esc(r.name)}</option>`).join('')}</select></label>
        <label>Access ID<input class="text-input" data-f="accessId" autocomplete="off" spellcheck="false" maxlength="64"></label>
        <label>Access Secret<input class="text-input" data-f="secret" type="password" autocomplete="new-password" maxlength="64">
          <small class="hint">Fica guardado cifrado no servidor. Usar pede a senha do BrunOS.</small></label>
        <p class="ty-err" role="alert"></p>
        <div class="dlg-actions">${change ? '<button type="button" class="pill-btn" data-c="back">Voltar</button>' : ''}
          <button type="button" class="pill-btn primary" data-c="save">${svg('check')}Conectar</button></div>
      </form>`;
    setTimeout(() => $('[data-f="accessId"]', d.el)?.focus(), 50);
  };

  const plugRow = (p) => {
    const st = p.ip
      ? `<span class="ty-ok">${svg('check')}na rede em <b>${esc(p.ip)}</b> · versão ${esc(p.version)}${p.power != null ? ` · <b>${esc(fmt(p.power, 1))} W</b> agora` : ''}</span>`
      : p.noKey ? '<span class="ty-warn">a nuvem não mandou a chave local (não é uma tomada com chave?)</span>'
        : `<span class="ty-warn">não respondeu na rede de casa (está ligada e no mesmo Wi-Fi?)${p.online ? '' : ' · a nuvem diz que está desligada'}</span>`;
    let acts = '';
    if (p.ip) {
      if (p.known && p.same) acts = `<span class="ty-same">${svg('check')}Tudo certo: “${esc(p.knownAs)}” já está com estes dados</span><button class="pill-btn sm" data-add="${esc(p.id)}">Adicionar como nova</button>`;
      else if (p.known) acts = `<button class="pill-btn sm primary" data-upd="${esc(p.id)}">${svg('refresh')}Atualizar “${esc(p.knownAs)}”</button><button class="pill-btn sm" data-add="${esc(p.id)}">Adicionar como nova</button>`;
      else acts = `<button class="pill-btn sm primary" data-add="${esc(p.id)}">${svg('plus')}Adicionar</button>`;
    }
    return `<div class="ty-plug" data-id="${esc(p.id)}"><span class="ty-ico">${svg('plug')}</span>
      <span class="ty-txt"><b>${esc(p.name)}</b><small>${esc(p.product || '')}</small>${st}</span>
      <span class="ty-acts">${acts}</span></div>`;
  };

  const drawMain = () => {
    const reg = regions.find((r) => r.id === info.region)?.name || info.region;
    box().innerHTML = `${head}
      <div class="ty-acct">${svg('link')}<span>Conta da Tuya: <b>${esc(info.accessId)}</b> · ${esc(reg)}</span>
        <button class="pill-btn sm" data-c="change">Trocar</button><button class="pill-btn sm danger" data-c="forget">Esquecer</button></div>
      <div class="ty-list">${plugs === null ? `<div class="ty-empty">${svg('search')}<p>Clique em “Procurar tomadas”: o BrunOS lista as tomadas da conta e procura cada uma na rede de casa.</p></div>`
        : plugs.length ? plugs.map(plugRow).join('') : '<div class="ty-empty"><p>Nenhuma tomada na conta.</p></div>'}</div>
      ${plugs?.some((p) => p.ip) ? `<p class="hint">${svg('info')} Dica: no roteador, reserve o IP de cada tomada (DHCP fixo). Se o IP mudar, o sino avisa e é só procurar de novo aqui.</p>` : ''}
      <div class="dlg-actions"><button class="pill-btn" data-c="close">Fechar</button>
        <button class="pill-btn primary" data-c="scan" ${busy ? 'disabled' : ''}>${svg(busy ? 'refresh' : 'search', busy ? 'spin' : '')}${busy ? 'Procurando… (até 1 minuto)' : plugs ? 'Procurar de novo' : 'Procurar tomadas'}</button></div>`;
  };

  const draw = () => (info?.configured ? drawMain() : drawLogin(false));

  try {
    const r = await api.tuya.cloud();
    info = r.cloud; regions = r.regions || [];
    draw();
  } catch (e) { d.close(); toastError(e, 'Não deu para abrir o assistente'); return; }

  const apply = async (id, mode) => {
    const p = plugs.find((x) => x.id === id);
    let name = '';
    if (mode === 'add') {
      name = p.known ? `${p.name} (teste)` : p.name;
    } else if (!(await confirmDialog(`Atualizar “${p.knownAs}”?`, `O cadastro continua o mesmo (widgets, Medidor e histórico seguem ligados nele), só com o IP ${p.ip}, a versão ${p.version} e a chave local de agora.`, 'Atualizar'))) return;
    try {
      const it = await api.tuya.apply({ id, mode, name });
      toast(mode === 'add' ? 'Tomada adicionada' : 'Tomada atualizada', it.name);
      if (mode === 'update') Object.assign(p, { same: true });
      else Object.assign(p, { known: it.id, knownAs: it.name, same: true });
      drawMain();
      onDone?.();
    } catch (e) { toastError(e, 'Não deu para gravar a tomada'); }
  };

  d.el.addEventListener('click', async (e) => {
    const c = e.target.closest('[data-c]')?.dataset.c;
    const upd = e.target.closest('[data-upd]')?.dataset.upd, add = e.target.closest('[data-add]')?.dataset.add;
    if (upd) return apply(upd, 'update');
    if (add) return apply(add, 'add');
    if (c === 'close') return d.close();
    if (c === 'change') return drawLogin(true);
    if (c === 'back') return drawMain();
    if (c === 'forget') {
      if (!(await confirmDialog('Esquecer a conta da Tuya?', 'O Access ID e o Access Secret são apagados do BrunOS. As tomadas já cadastradas continuam funcionando (elas não usam a nuvem).', 'Esquecer', { danger: true }))) return;
      try { await api.tuya.forget(); info = { configured: false }; plugs = null; draw(); toast('Conta da Tuya esquecida'); } catch (err) { toastError(err); }
      return;
    }
    if (c === 'save') {
      const v = (k) => $(`[data-f="${k}"]`, d.el).value.trim();
      const err = $('.ty-err', d.el), btn = e.target.closest('button');
      btn.disabled = true; err.textContent = '';
      try { info = (await api.tuya.saveCloud({ region: v('region'), accessId: v('accessId'), secret: v('secret') })).cloud; plugs = null; drawMain(); toast('Conta da Tuya conectada'); }
      catch (er) { btn.disabled = false; err.textContent = er.message; }
      return;
    }
    if (c === 'scan' && !busy) {
      busy = true; drawMain();
      try { plugs = (await api.tuya.scan()).plugs || []; } catch (er) { toastError(er, 'Não deu para procurar as tomadas'); }
      busy = false; drawMain();
    }
  });
}
