// Instalar, editar a instalação e Ajustes dentro do BrunOS (0.20.0, decisão 61). Carregado só quando a
// pessoa clica em Instalar, Instalação ou Ajustes na Loja (import dinâmico a partir do store.js).
//
// Instalar/editar: as perguntas do app (da loja), a porta e o endereço; o BrunOS manda ao Runtipi pelo
// script do servidor e acompanha até o app ligar. Ajustes: pastas do servidor, memória, núcleos, placa de
// vídeo e variáveis, com listas prontas e o recomendado já marcado; o que foi feito à mão no Runtipi fica.
import { api } from './api.js';
import { $, esc, svg, toast, openDialog } from './ui.js';

const DESTINOS = ['/media', '/movies', '/tv', '/music', '/photos', '/books', '/downloads', '/data', '/config', '/backups'];
const MEMORIA = [[0, 'Sem limite (recomendado)'], [256, '256 MB'], [512, '512 MB'], [1024, '1 GB'], [2048, '2 GB'], [4096, '4 GB'], [8192, '8 GB'], [16384, '16 GB']];
const NUCLEOS = [[0, 'Sem limite (recomendado)'], [0.5, 'Meio núcleo'], [1, '1 núcleo'], [2, '2 núcleos'], [3, '3 núcleos'], [4, '4 núcleos'], [6, '6 núcleos'], [8, '8 núcleos']];
const PRONTAS = [
  { label: 'Fuso horário de Brasília', vars: [['TZ', 'America/Sao_Paulo']] },
  { label: 'Usuário padrão do Runtipi (1000)', vars: [['PUID', '1000'], ['PGID', '1000']] },
];
const CMD_SENHA = 'sudo brunos-host senha-config';

// pergunta da instalação que parece ser uma pasta do servidor (ganha a lista de discos e pastas)
const isFolderField = (f) => f.type === 'text' && !f.options?.length
  && /(path|dir|folder|pasta|media|m[íi]dia|movies|filmes|series|music|downloads|library|biblioteca|volume)/i.test(`${f.env} ${f.label}`);

/* ── Pastas do servidor ─────────────────────────────────────────── */
// separa um caminho em pasta pronta + subpasta (a pasta pronta mais comprida que serve)
function splitHost(host, folders) {
  let best = null;
  for (const f of folders) if ((host === f.host || host.startsWith(`${f.host}/`)) && (!best || f.host.length > best.host.length)) best = f;
  return best ? { base: best.host, sub: host.slice(best.host.length).replace(/^\/+/, '') } : { base: host ? '@manual' : folders[0]?.host || '@manual', sub: host };
}
const joinHost = (base, sub) => (base === '@manual' ? sub.trim() : base + (sub.trim() ? `/${sub.trim().replace(/^\/+|\/+$/g, '')}` : ''));

function folderHTML(folders, host, key) {
  const { base, sub } = splitHost(host || '', folders);
  const opts = folders.map((f) => `<option value="${esc(f.host)}" ${f.host === base ? 'selected' : ''}>${esc(f.name)}</option>`).join('');
  return `<span class="sf-folder" data-folder="${esc(key)}"><select class="text-input" data-fbase aria-label="Pasta do servidor">${opts}<option value="@manual" ${base === '@manual' ? 'selected' : ''}>Outro caminho…</option></select>
    <input class="text-input" data-fsub value="${esc(sub)}" placeholder="${base === '@manual' ? '/mnt/disco/pasta' : 'subpasta (opcional, ex.: Filmes)'}" list="sf-sub-${esc(key)}" autocomplete="off"><datalist id="sf-sub-${esc(key)}"></datalist></span>`;
}
const subCache = {};
async function suggestSubs(wrap, folders) {
  const base = $('[data-fbase]', wrap).value, f = folders.find((x) => x.host === base);
  const sub = $('[data-fsub]', wrap);
  sub.placeholder = base === '@manual' ? '/mnt/disco/pasta' : 'subpasta (opcional, ex.: Filmes)';
  const list = $('datalist', wrap);
  if (!f?.rootId) { list.innerHTML = ''; return; }
  try {
    subCache[f.rootId] ||= api.files.list(f.rootId, '').then((r) => (r.entries || []).filter((e) => e.dir).map((e) => e.name).slice(0, 80));
    list.innerHTML = (await subCache[f.rootId]).map((n) => `<option value="${esc(n)}">`).join('');
  } catch { list.innerHTML = ''; }
}
function wireFolders(root, folders) {
  root.querySelectorAll('[data-folder]').forEach((w) => { suggestSubs(w, folders); $('[data-fbase]', w).onchange = () => suggestSubs(w, folders); });
}
const readFolder = (w) => joinHost($('[data-fbase]', w).value, $('[data-fsub]', w).value);

const sw = (on, attr, label) => `<button type="button" class="switch ${on ? 'on' : ''}" ${attr} role="switch" aria-checked="${!!on}" aria-label="${esc(label)}"></button>`;
function wireSwitches(root, onChange) {
  root.addEventListener('click', (e) => {
    const b = e.target.closest('.switch');
    if (!b || !root.contains(b)) return;
    b.classList.toggle('on'); b.setAttribute('aria-checked', b.classList.contains('on'));
    onChange?.(b);
  });
}

/* ── Instalar e editar a instalação ─────────────────────────────── */
/** ctx = { runtipi(): abre a tela do Runtipi, done(): recarrega a Loja e a tela inicial } */
export async function installDialog(a, ctx, { edit = false } = {}) {
  const d = openDialog(`<div class="sf"><div class="st-loading"><span class="upd-spin"></span>Carregando as perguntas do app…</div></div>`, { width: 640, backdropClose: false });
  const box = $('.sf', d.el);
  let F;
  try { F = await api.store.form(a.urn); } catch (err) { box.innerHTML = `<h2>${esc(a.name)}</h2><p class="st-err">${esc(err.message || String(err))}</p>${footer('Fechar')}`; wireFallback(); return; }
  const cur = F.current;
  const fields = F.fields || [];
  const val = (f) => (cur ? cur.values?.[f.env] : undefined) ?? (edit ? '' : f.default ?? '');
  const input = (f) => {
    const v = val(f), key = esc(f.env || f.label);
    if (f.type === 'boolean') return sw(v === true || v === 'true', `data-v="${key}" data-kind="bool"`, f.label);
    if (f.options?.length) return `<select class="text-input" data-v="${key}" data-kind="text">${f.required ? '' : '<option value="">—</option>'}${f.options.map((o) => `<option value="${esc(o.value)}" ${String(v) === o.value ? 'selected' : ''}>${esc(o.label)}</option>`).join('')}</select>`;
    if (isFolderField(f)) return `<span data-v="${key}" data-kind="folder">${folderHTML(F.folders, String(v || ''), f.env)}</span>`;
    if (f.type === 'number') return `<input class="text-input" type="number" data-v="${key}" data-kind="number" value="${esc(v)}" ${f.min != null ? `min="${f.min}"` : ''} ${f.max != null ? `max="${f.max}"` : ''}>`;
    if (f.type === 'password') {
      const kept = edit && cur?.secrets?.includes(f.env);
      return `<input class="text-input" type="password" data-v="${key}" data-kind="password" autocomplete="new-password" placeholder="${kept ? 'Guardada: deixe em branco para manter' : esc(f.placeholder || '')}">`;
    }
    return `<input class="text-input" type="${f.type === 'email' ? 'email' : f.type === 'url' ? 'url' : 'text'}" data-v="${key}" data-kind="text" value="${esc(v)}" placeholder="${esc(f.placeholder || '')}" autocomplete="off">`;
  };
  const port = cur?.port || F.port || '';
  const exposedLocal = cur ? cur.exposedLocal : true;
  box.innerHTML = `<h2>${edit ? `Instalação de ${esc(a.name)}` : `Instalar ${esc(a.name)}`}</h2>
    <p class="sf-intro">${edit ? 'Mude as respostas da instalação. Ao salvar, o Runtipi reinicia o app com as respostas novas.' : 'O Runtipi instala o app com estas respostas. O que já vem preenchido é o recomendado.'}</p>
    <form class="sf-form" autocomplete="off">
      ${fields.length ? `<h4>O app pergunta</h4>${fields.map((f) => `<label class="sf-field"><span class="sf-lbl">${esc(f.label)}${f.required ? ' <i>obrigatório</i>' : ''}</span>${input(f)}${f.hint ? `<small>${esc(f.hint)}</small>` : ''}</label>`).join('')}` : ''}
      <h4>Acesso</h4>
      <label class="sf-field"><span class="sf-lbl">Porta</span><input class="text-input" type="number" name="port" min="1024" max="65535" value="${esc(port)}"><small>Por ela o app abre em http://servidor:porta. A da loja já vem preenchida.</small></label>
      <div class="sf-row"><span>Abrir a porta no servidor <small>(recomendado)</small></span>${sw(cur ? cur.openPort : true, 'data-sw="openPort"', 'Abrir a porta')}</div>
      ${F.exposable && F.localDomain ? `<div class="sf-row"><span>Endereço local com cadeado <small>https://${esc(a.id)}.${esc(F.localDomain)}</small></span>${sw(exposedLocal, 'data-sw="exposedLocal"', 'Endereço local')}</div>` : ''}
      ${F.exposable ? `<div class="sf-row"><span>Expor na internet com um domínio <small>(só se você tem um domínio apontando para o servidor)</small></span>${sw(cur?.exposed, 'data-sw="exposed"', 'Expor na internet')}</div>
        <label class="sf-field" data-domain ${cur?.exposed ? '' : 'hidden'}><span class="sf-lbl">Domínio</span><input class="text-input" name="domain" value="${esc(cur?.domain || '')}" placeholder="${esc(a.id)}.meudominio.com"></label>` : ''}
      <p class="st-err" hidden></p>
      ${footer(edit ? 'Salvar e reiniciar' : 'Instalar')}
    </form>`;
  wireFolders(box, F.folders || []);
  wireSwitches(box, (b) => { if (b.dataset.sw === 'exposed') { const dm = $('[data-domain]', box); if (dm) dm.hidden = !b.classList.contains('on'); } });
  wireFallback();
  const err = $('.st-err', box), go = $('[data-c="go"]', box);
  $('form', box).addEventListener('submit', async (e) => {
    e.preventDefault();
    const values = {};
    box.querySelectorAll('[data-v]').forEach((el) => {
      const k = el.dataset.v, kind = el.dataset.kind;
      if (kind === 'bool') values[k] = el.classList.contains('on');
      else if (kind === 'folder') values[k] = readFolder($('[data-folder]', el));
      else if (kind === 'number') values[k] = el.value === '' ? '' : Number(el.value);
      else if (kind === 'password') values[k] = el.value;
      else values[k] = el.value.trim();
    });
    const on = (k) => $(`[data-sw="${k}"]`, box)?.classList.contains('on') || false;
    const body = { urn: a.urn, values, port: Number($('[name="port"]', box).value) || 0, openPort: on('openPort'), exposedLocal: on('exposedLocal'), exposed: on('exposed'), domain: $('[name="domain"]', box)?.value.trim() || '' };
    err.hidden = true; go.disabled = true;
    go.innerHTML = `<span class="upd-spin"></span>${edit ? 'Salvando…' : 'Mandando ao Runtipi…'}`;
    try {
      await (edit ? api.store.reconfig(body) : api.store.install(body));
    } catch (ex) {
      err.textContent = ex.message || String(ex); err.hidden = false; go.disabled = false; go.textContent = edit ? 'Salvar e reiniciar' : 'Instalar';
      return;
    }
    if (edit) { d.close(); toast(`Instalação de ${a.name} alterada`, 'O Runtipi reiniciou o app com as respostas novas.'); ctx.done(); return; }
    follow(a, ctx, box, d);
  });

  function footer(label) {
    return `<div class="dlg-actions sf-actions"><button type="button" class="pill-btn sf-ghost" data-c="runtipi">${svg('external')}Abrir no Runtipi</button><span class="sf-grow"></span><button type="button" class="pill-btn" data-c="close">${label === 'Fechar' ? 'Fechar' : 'Cancelar'}</button>${label === 'Fechar' ? '' : `<button type="submit" class="pill-btn primary" data-c="go">${esc(label)}</button>`}</div>`;
  }
  function wireFallback() {
    $('[data-c="runtipi"]', box)?.addEventListener('click', () => { d.close(); ctx.runtipi(); });
    $('[data-c="close"]', box)?.addEventListener('click', () => d.close());
  }
}

// Acompanha a instalação até o app ligar (continua mesmo se a janela for fechada).
async function follow(a, ctx, box, d) {
  const view = (html) => { if (box.isConnected) box.innerHTML = html; };
  view(`<h2>Instalando ${esc(a.name)}</h2><div class="sf-progress"><span class="upd-spin"></span><span>O Runtipi está baixando e ligando o app. Pode levar alguns minutos na primeira vez; pode fechar esta janela que o BrunOS avisa quando terminar.</span></div>
    <div class="dlg-actions"><button class="pill-btn" data-c="close">Fechar</button></div>`);
  $('[data-c="close"]', box)?.addEventListener('click', () => d.close());
  const until = Date.now() + 25 * 60000;
  let fails = 0;
  while (Date.now() < until) {
    await new Promise((r) => setTimeout(r, 4000));
    let st;
    try { st = (await api.store.status(a.urn)).status; fails = 0; } catch { if (++fails > 5) break; continue; }
    if (st === 'running' || st === 'stopped') {
      toast(`${a.name} instalado`, 'Já está na tela inicial.');
      view(`<h2>${esc(a.name)} instalado</h2><p class="sf-intro">Pronto! O app já aparece na tela inicial.</p><div class="dlg-actions"><button class="pill-btn primary" data-c="close">Fechar</button></div>`);
      $('[data-c="close"]', box)?.addEventListener('click', () => d.close());
      ctx.done();
      return;
    }
    if (st === 'gone') {
      toast(`A instalação de ${a.name} falhou`, 'Veja o motivo nos registros do Runtipi.', null, { error: true });
      view(`<h2>A instalação não deu certo</h2><p class="sf-intro">O Runtipi não conseguiu instalar ${esc(a.name)} e desfez a instalação. O motivo fica nos registros do painel do Runtipi (por exemplo: uma pergunta obrigatória ou a imagem que não baixou).</p>
        <div class="dlg-actions"><button class="pill-btn" data-c="runtipi">${svg('external')}Abrir no Runtipi</button><button class="pill-btn primary" data-c="close">Fechar</button></div>`);
      $('[data-c="close"]', box)?.addEventListener('click', () => d.close());
      $('[data-c="runtipi"]', box)?.addEventListener('click', () => { d.close(); ctx.runtipi(); });
      ctx.done();
      return;
    }
  }
  toast(`Ainda instalando ${a.name}?`, 'O BrunOS parou de acompanhar; confira na Loja daqui a pouco.');
  ctx.done();
}

/* ── Ajustes ────────────────────────────────────────────────────── */
/** ctx = { done(), openTerminal(opts) | null } */
export async function settingsDialog(a, ctx) {
  const d = openDialog(`<div class="sf"><div class="st-loading"><span class="upd-spin"></span>Lendo os ajustes do app…</div></div>`, { width: 700, backdropClose: false });
  const box = $('.sf', d.el);
  let R;
  try { R = await api.store.settings(a.urn); } catch (err) { box.innerHTML = `<h2>Ajustes de ${esc(a.name)}</h2><p class="st-err">${esc(err.message || String(err))}</p><div class="dlg-actions"><button class="pill-btn" data-c="close">Fechar</button></div>`; $('[data-c="close"]', box).onclick = () => d.close(); return; }
  const folders = R.folders || [];
  if (R.error) {
    box.innerHTML = `<h2>Ajustes de ${esc(a.name)}</h2><p class="st-err">${esc(R.error)}</p><p class="sf-intro">O arquivo de ajustes foi escrito à mão no Runtipi e tem um erro. Para não estragar nada, o BrunOS não mexe nele: corrija no editor do Runtipi.</p>
      <pre class="sf-raw">${esc(R.raw || '')}</pre><div class="dlg-actions"><button class="pill-btn" data-c="close">Fechar</button></div>`;
    $('[data-c="close"]', box).onclick = () => d.close();
    return;
  }
  const st = R.state;
  const M = { mounts: st.mounts.map((m) => ({ ...m })), env: st.env.map((v) => ({ ...v })), memoryMB: st.memoryMB || 0, cpus: st.cpus || 0, gpu: !!st.gpu };
  const opt = (list, cur, label) => {
    const has = list.some(([v]) => v === cur);
    return `${list.map(([v, l]) => `<option value="${v}" ${v === cur ? 'selected' : ''}>${l}</option>`).join('')}${has ? '' : `<option value="${cur}" selected>${label(cur)} (atual)</option>`}`;
  };
  const sync = () => { // lê a tela de volta para o modelo antes de redesenhar
    if (!box.querySelector('.sf-form')) return;
    M.mounts = [...box.querySelectorAll('[data-mount]')].map((r) => ({ host: readFolder($('[data-folder]', r)), container: $('[data-dst]', r).value.trim(), readOnly: $('[data-ro]', r).classList.contains('on') }));
    M.env = [...box.querySelectorAll('[data-env]')].map((r) => ({ key: $('[data-k]', r).value.trim(), value: $('[data-val]', r).value }));
    M.memoryMB = Number($('[name="mem"]', box).value) || 0;
    M.cpus = Number($('[name="cpu"]', box).value) || 0;
    M.gpu = $('[data-sw="gpu"]', box).classList.contains('on');
  };
  const draw = (keepPw = '') => {
    box.innerHTML = `<h2>Ajustes de ${esc(a.name)}</h2>
      <p class="sf-intro">Pastas do servidor dentro do app, limites e variáveis. Ficam num arquivo do Runtipi que continua valendo depois das atualizações do app.</p>
      ${st.manual.length ? `<p class="st-note">${svg('info')}<span>Feito à mão no Runtipi, fica como está: ${st.manual.map(esc).join(', ')}.</span></p>` : ''}
      ${st.risks.length ? `<p class="st-note sf-warn">${svg('alert')}<span>O arquivo já tinha, escrito à mão: ${st.risks.map(esc).join(', ')}. O BrunOS mantém, mas não deixa acrescentar nada parecido.</span></p>` : ''}
      <form class="sf-form" autocomplete="off">
        <h4>Pastas do servidor</h4>
        <div class="sf-mounts">${M.mounts.map((m, i) => `<div class="sf-mount" data-mount="${i}">${folderHTML(folders, m.host, `m${i}`)}
          <span class="sf-arrow">${svg('chevR')}</span><input class="text-input sf-dst" data-dst value="${esc(m.container)}" placeholder="dentro do app (ex.: /movies)" list="sf-dsts" aria-label="Pasta dentro do app">
          <span class="sf-ro">${sw(m.readOnly, 'data-ro', 'Só leitura')}<small>Só leitura</small></span>
          <button type="button" class="pill-btn sm sf-x" data-rm="${i}" aria-label="Tirar">${svg('x')}</button></div>`).join('') || '<p class="sf-empty">Nenhuma pasta extra. Use para o app enxergar seus filmes, fotos ou downloads.</p>'}</div>
        <datalist id="sf-dsts">${DESTINOS.map((x) => `<option value="${x}">`).join('')}</datalist>
        <button type="button" class="pill-btn sm" data-add="mount">${svg('plus')}Adicionar pasta</button>
        <small class="sf-hint">Só leitura é o recomendado para mídias: o app vê os arquivos, mas não apaga nem muda nada.</small>
        <h4>Limites</h4>
        <div class="sf-grid2"><label class="sf-field"><span class="sf-lbl">Memória</span><select class="text-input" name="mem">${opt(MEMORIA, M.memoryMB, (v) => `${v} MB`)}</select></label>
          <label class="sf-field"><span class="sf-lbl">Processador</span><select class="text-input" name="cpu">${opt(NUCLEOS, M.cpus, (v) => `${v} núcleos`)}</select></label></div>
        <div class="sf-row"><span>Usar a placa de vídeo <small>(converter vídeo no Plex, Jellyfin, Immich…)</small></span>${sw(M.gpu, 'data-sw="gpu"', 'Placa de vídeo')}</div>
        <h4>Variáveis</h4>
        <div class="sf-envs">${M.env.map((v, i) => `<div class="sf-env" data-env="${i}"><input class="text-input" data-k value="${esc(v.key)}" placeholder="NOME" aria-label="Nome"><span>=</span><input class="text-input" data-val value="${esc(v.value)}" placeholder="valor" aria-label="Valor"><button type="button" class="pill-btn sm sf-x" data-rmenv="${i}" aria-label="Tirar">${svg('x')}</button></div>`).join('')}</div>
        <div class="sf-chips"><button type="button" class="pill-btn sm" data-add="env">${svg('plus')}Adicionar variável</button>${PRONTAS.map((p, i) => `<button type="button" class="pill-btn sm sf-ghost" data-preset="${i}">${esc(p.label)}</button>`).join('')}</div>
        <details class="sf-adv"><summary>Ver o arquivo de ajustes (avançado)</summary><pre class="sf-raw">${esc(R.raw || '(ainda não existe: o BrunOS cria ao salvar)')}</pre></details>
        <div class="sf-save"><label class="sf-field"><span class="sf-lbl">Senha de configuração</span><input class="text-input" type="password" name="pw" autocomplete="off" value="${esc(keepPw)}"></label>
          <div class="sf-row"><span>Reiniciar o app agora para valer</span>${sw(true, 'data-sw="restart"', 'Reiniciar agora')}</div></div>
        <p class="st-err" hidden></p>
        <div class="dlg-actions"><button type="button" class="pill-btn" data-c="close">Cancelar</button><button type="submit" class="pill-btn primary" data-c="go">Salvar</button></div>
      </form>`;
    wireFolders(box, folders);
    $('[data-c="close"]', box).onclick = () => d.close();
  };
  wireSwitches(box);
  box.addEventListener('click', (e) => {
    const t = (s) => e.target.closest(s);
    let el;
    const pw = $('[name="pw"]', box)?.value || '';
    if (t('[data-add="mount"]')) { sync(); M.mounts.push({ host: folders[0]?.host || '', container: '', readOnly: true }); draw(pw); }
    else if (t('[data-add="env"]')) { sync(); M.env.push({ key: '', value: '' }); draw(pw); }
    else if ((el = t('[data-rm]'))) { sync(); M.mounts.splice(+el.dataset.rm, 1); draw(pw); }
    else if ((el = t('[data-rmenv]'))) { sync(); M.env.splice(+el.dataset.rmenv, 1); draw(pw); }
    else if ((el = t('[data-preset]'))) {
      sync();
      for (const [k, v] of PRONTAS[+el.dataset.preset].vars) { const x = M.env.find((y) => y.key === k); if (x) x.value = v; else M.env.push({ key: k, value: v }); }
      draw(pw);
    } else if (t('[data-c="term"]')) { d.close(); ctx.openTerminal?.({ typeCmd: CMD_SENHA }); toast('Comando digitado no Terminal', 'Confira e aperte Enter.'); }
  });
  box.addEventListener('submit', async (e) => {
    e.preventDefault();
    sync();
    const err = $('.st-err', box), go = $('[data-c="go"]', box), password = $('[name="pw"]', box).value;
    const bad = M.mounts.find((m) => !m.container.startsWith('/') || !m.host);
    if (bad) { err.textContent = 'Em cada pasta, escolha a pasta do servidor e onde ela aparece dentro do app (começando com /, ex.: /movies).'; err.hidden = false; return; }
    if (!password) { err.textContent = 'Digite a Senha de configuração.'; err.hidden = false; return; }
    const env = M.env.filter((v) => v.key);
    err.hidden = true; go.disabled = true; go.innerHTML = '<span class="upd-spin"></span>Salvando…';
    try {
      const r = await api.store.saveSettings({ urn: a.urn, password, restart: $('[data-sw="restart"]', box).classList.contains('on'), settings: { mounts: M.mounts, memoryMB: M.memoryMB, cpus: M.cpus, gpu: M.gpu, env } });
      d.close();
      if (r.restartErr) toast(`Ajustes de ${a.name} salvos`, `Mas o app não reiniciou: ${r.restartErr}`, null, { error: true });
      else toast(`Ajustes de ${a.name} salvos`, 'Uma cópia do arquivo anterior ficou guardada no servidor.');
      ctx.done();
    } catch (ex) {
      const m = ex.message || String(ex);
      go.disabled = false; go.textContent = 'Salvar';
      if (/ainda não existe Senha de (configuração|formatação)/.test(m)) {
        err.innerHTML = `Ainda não existe Senha de configuração. Ela é criada uma vez, no Terminal do servidor: <code>${CMD_SENHA}</code>${ctx.openTerminal ? ` <button type="button" class="pill-btn sm" data-c="term">${svg('terminal')}Criar agora</button>` : ''}`;
      } else err.textContent = m;
      err.hidden = false;
    }
  });
  draw();
}

export const _test = { splitHost, joinHost, isFolderField };
