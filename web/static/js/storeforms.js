// Janela de instalar e de editar um app (0.21.0, decisão 62; antes eram duas: Instalação e Ajustes).
// Carregado só quando a pessoa clica em Instalar ou Configurar na Loja (import dinâmico a partir do store.js).
//
// Uma tela só, com seções que abrem e fecham: O app pergunta e Acesso (abertas), Pastas e recursos e
// Opcionais (fechadas, com um resumo), Avançado. Os Ajustes já vêm preenchidos com o que a receita do app
// traz e o BrunOS grava só o que mudou. Cada opção tem um "?" que abre a explicação logo abaixo (funciona no
// toque). Em janela estreita e no celular a janela ocupa a tela toda, com os botões presos embaixo.
import { api } from './api.js';
import { $, esc, svg, toast, openDialog } from './ui.js';

const DESTINOS = ['/media', '/movies', '/tv', '/music', '/photos', '/books', '/downloads', '/data', '/config', '/backups'];
const MEMORIA = [[0, 'Sem limite (recomendado)'], [256, '256 MB'], [512, '512 MB'], [1024, '1 GB'], [2048, '2 GB'], [4096, '4 GB'], [8192, '8 GB'], [16384, '16 GB']];
const NUCLEOS = [[0, 'Sem limite (recomendado)'], [0.5, 'Meio núcleo'], [1, '1 núcleo'], [2, '2 núcleos'], [3, '3 núcleos'], [4, '4 núcleos'], [6, '6 núcleos'], [8, '8 núcleos']];
const SHM = [[0, 'Como o app vem'], [256, '256 MB'], [512, '512 MB'], [1024, '1 GB'], [2048, '2 GB'], [4096, '4 GB']];
const REINICIO = [['', 'Como o app vem (recomendado)'], ['unless-stopped', 'Sempre, menos quando eu parar'], ['always', 'Sempre'], ['on-failure', 'Só quando der erro'], ['no', 'Nunca']];
const PRONTAS = [
  { label: 'Fuso horário de Brasília', vars: [['TZ', 'America/Sao_Paulo']] },
  { label: 'Usuário padrão do Runtipi (1000)', vars: [['PUID', '1000'], ['PGID', '1000']] },
];
const CMD_SENHA = 'sudo brunos-host senha-config';

// pergunta da instalação que parece ser uma pasta do servidor (ganha a lista de discos e pastas)
const isFolderField = (f) => f.type === 'text' && !f.options?.length
  && /(path|dir|folder|pasta|media|m[íi]dia|movies|filmes|series|music|downloads|library|biblioteca|volume)/i.test(`${f.env} ${f.label}`);

// pasta da receita escrita de um jeito que se entende
const niceHost = (h) => String(h).replace(/^\$\{?APP_DATA_DIR\}?/, 'pasta do app').replace(/^\$\{ROOT_FOLDER_HOST\}\/media/, 'mídia do Runtipi');
const imageRepo = (img) => { const s = String(img || '').split('@')[0], i = s.lastIndexOf(':'); return i > s.lastIndexOf('/') ? s.slice(0, i) : s; };
const imageTag = (img) => { const s = String(img || '').split('@')[0], i = s.lastIndexOf(':'); return i > s.lastIndexOf('/') ? s.slice(i + 1) : ''; };

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
    <input class="text-input" data-fsub value="${esc(sub)}" placeholder="${base === '@manual' ? '/mnt/disco/pasta' : 'subpasta (opcional, ex.: Filmes)'}" list="sf-sub-${esc(key)}" autocomplete="off" aria-label="Subpasta"><datalist id="sf-sub-${esc(key)}"></datalist></span>`;
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

/* ── Pedacinhos de tela ─────────────────────────────────────────── */
const sw = (on, attr, label) => `<button type="button" class="switch ${on ? 'on' : ''}" ${attr} role="switch" aria-checked="${!!on}" aria-label="${esc(label)}"></button>`;
const qBtn = '<button type="button" class="sf-q" aria-label="O que é isto?" aria-expanded="false">?</button>';
const helpTxt = (h) => (h ? `<small class="sf-help" hidden>${h}</small>` : '');
// campo com rótulo, "?" e explicação; html = o controle
const field = (label, html, { help = '', hint = '', req = false, attrs = '' } = {}) => `<div class="sf-field" ${attrs}><span class="sf-lbl">${label}${req ? ' <i>obrigatório</i>' : ''}${help ? qBtn : ''}</span>${helpTxt(help)}${html}${hint ? `<small>${hint}</small>` : ''}</div>`;
// linha com chave liga/desliga
const row = (title, sub, ctrl, help = '', attrs = '') => `<div class="sf-rowwrap" ${attrs}><div class="sf-row"><span><span class="sf-t">${title}${help ? qBtn : ''}</span>${sub ? `<small>${sub}</small>` : ''}</span>${ctrl}</div>${helpTxt(help)}</div>`;
const section = (key, icon, title, open, body) => `<details class="sf-sec" data-sec="${key}" ${open ? 'open' : ''}><summary><span class="sf-sico">${svg(icon)}</span><span class="sf-stxt"><b>${title}</b><small data-sum></small></span><span class="sf-chev">${svg('chevR')}</span></summary><div class="sf-body">${body}</div></details>`;
const opt = (list, cur, label) => {
  const has = list.some(([v]) => v === cur);
  return `${list.map(([v, l]) => `<option value="${esc(v)}" ${v === cur ? 'selected' : ''}>${esc(l)}</option>`).join('')}${has ? '' : `<option value="${esc(cur)}" selected>${esc(label(cur))} (atual)</option>`}`;
};

/* ── A janela ───────────────────────────────────────────────────── */
/** ctx = { runtipi(): abre a tela do Runtipi, done(): recarrega a Loja e a tela inicial, openTerminal(opts) | null } */
export async function appDialog(a, ctx, { edit = false, open = '' } = {}) {
  let busy = false;
  const d = openDialog(`<div class="sf"><div class="st-loading"><span class="upd-spin"></span>${edit ? 'Lendo a instalação do app…' : 'Carregando as perguntas do app…'}</div></div>`,
    { width: 680, backdropClose: false, cls: 'sf-dlg', beforeClose: () => !busy });
  const box = $('.sf', d.el);
  let F;
  try { F = await api.store.form(a.urn); } catch (err) { fail(err.message || String(err)); return; }
  if (edit && F.currentErr && !F.current) { fail(F.currentErr); return; }

  const cur = F.current, fields = F.fields || [], folders = F.folders || [];
  const rec = { env: [], mounts: [], ports: [], image: '', restart: '', ...(F.recipe || {}) };
  const privs = F.privileges || [];
  const newScript = (F.script || 0) >= 15; // script do servidor 15: Ajustes e privilégios já na instalação
  const ajOK = (edit || newScript) && !F.settingsErr; // dá para mexer nos Ajustes nesta janela
  const st0 = { mounts: [], env: [], memoryMB: 0, cpus: 0, gpu: false, image: '', restart: '', exists: false, manual: [], risks: [], ...(F.settings || {}) };
  const repo = imageRepo(rec.image || st0.image);
  const manualImage = st0.image && rec.image && imageRepo(st0.image) !== imageRepo(rec.image); // escrita à mão no Runtipi

  // Ajustes em edição: pastas extras, variáveis (as da receita + as extras), limites, imagem, reinício
  const recEnv = new Map(rec.env.map((v) => [v.key, v.value]));
  const userEnv = new Map(st0.env.map((v) => [v.key, v.value]));
  const M = {
    mounts: st0.mounts.map((m) => ({ ...m })),
    env: [...rec.env.map((v) => ({ key: v.key, value: userEnv.has(v.key) ? userEnv.get(v.key) : v.value, rec: v.value })),
      ...st0.env.filter((v) => !recEnv.has(v.key)).map((v) => ({ key: v.key, value: v.value, rec: null }))],
    memoryMB: st0.memoryMB || 0, cpus: st0.cpus || 0, gpu: !!st0.gpu, tag: manualImage ? '' : imageTag(st0.image), restart: st0.restart || '',
    shm: st0.shmMB || 0,
  };
  // app de área de trabalho pelo navegador (Brave, Firefox...) sem memória compartilhada na receita: trava
  const needShm = rec.desktop && !rec.shmMB && !st0.shmMB;

  /* respostas da instalação */
  const val = (f) => (cur ? cur.values?.[f.env] : undefined) ?? (edit ? '' : f.default ?? '');
  const input = (f) => {
    const v = val(f), key = esc(f.env || f.label);
    if (f.type === 'boolean') return sw(v === true || v === 'true', `data-v="${key}" data-kind="bool"`, f.label);
    if (f.options?.length) return `<select class="text-input" data-v="${key}" data-kind="text">${f.required ? '' : '<option value="">—</option>'}${f.options.map((o) => `<option value="${esc(o.value)}" ${String(v) === o.value ? 'selected' : ''}>${esc(o.label)}</option>`).join('')}</select>`;
    if (isFolderField(f)) return `<span data-v="${key}" data-kind="folder">${folderHTML(folders, String(v || ''), f.env)}</span>`;
    if (f.type === 'number') return `<input class="text-input" type="number" data-v="${key}" data-kind="number" value="${esc(v)}" ${f.min != null ? `min="${f.min}"` : ''} ${f.max != null ? `max="${f.max}"` : ''}>`;
    if (f.type === 'password') {
      const kept = edit && cur?.secrets?.includes(f.env);
      return `<input class="text-input" type="password" data-v="${key}" data-kind="password" autocomplete="new-password" placeholder="${kept ? 'Guardada: deixe em branco para manter' : esc(f.placeholder || '')}">`;
    }
    return `<input class="text-input" type="${f.type === 'email' ? 'email' : f.type === 'url' ? 'url' : 'text'}" data-v="${key}" data-kind="text" value="${esc(v)}" placeholder="${esc(f.placeholder || '')}" autocomplete="off">`;
  };
  const askHTML = fields.map((f) => field(esc(f.label), input(f), { req: f.required, hint: f.hint ? esc(f.hint) : '' })).join('');

  const port = cur?.port || F.port || '';
  const exposedLocal = cur ? cur.exposedLocal : true;
  const sub = cur?.localSubdomain || a.id;
  const accessHTML = `
    ${field('Porta', `<input class="text-input" type="number" name="port" min="1024" max="65535" value="${esc(port)}">`, { help: 'Número pelo qual o app abre direto no endereço do servidor (http://servidor:porta). A da loja já vem preenchida; troque só se outro app já usar essa porta.' })}
    ${row('Abrir a porta no servidor', '(recomendado)', sw(cur ? cur.openPort : true, 'data-sw="openPort"', 'Abrir a porta'), 'Deixa o app acessível pelo IP do servidor e a porta acima. Desligado, ele só abre pelo endereço local com cadeado.')}
    ${F.exposable && F.localDomain ? `${row('Endereço local com cadeado', '', sw(exposedLocal, 'data-sw="exposedLocal"', 'Endereço local'), `Um endereço https na sua rede (com o cadeado do Runtipi), em vez de IP e porta. Você escolhe o nome; o final .${esc(F.localDomain)} é o domínio local do Runtipi.`)}
      <div class="sf-addr" data-local ${exposedLocal ? '' : 'hidden'}><span>https://</span><input class="text-input" name="sub" value="${esc(sub)}" maxlength="63" autocomplete="off" spellcheck="false" aria-label="Nome do endereço local"><span>.${esc(F.localDomain)}</span></div>` : ''}
    ${row('Pedir login do Runtipi', '(desligado = como o app vem)', sw(cur?.enableAuth, 'data-sw="enableAuth"', 'Pedir login do Runtipi'), 'O Runtipi pede o usuário e a senha dele antes de abrir o app. Útil para apps que não têm login próprio.')}
    ${row('Mostrar no painel de visitantes', '', sw(cur?.guest, 'data-sw="guest"', 'Mostrar no painel de visitantes'), 'O app aparece no painel de visitantes do Runtipi (a página que quem não está logado vê). Deixe desligado se não usa esse painel.')}
    ${F.exposable ? `${row('Expor na internet com um domínio', '(só se você tem um domínio apontando para o servidor)', sw(cur?.exposed, 'data-sw="exposed"', 'Expor na internet'), 'O app fica acessível de fora de casa por um domínio seu (ex.: app.meudominio.com), com cadeado. Precisa do domínio apontando para o seu IP e das portas 80 e 443 abertas no roteador.')}
      ${field('Domínio', `<input class="text-input" name="domain" value="${esc(cur?.domain || '')}" placeholder="${esc(a.id)}.meudominio.com" autocomplete="off">`, { attrs: `data-domain ${cur?.exposed ? '' : 'hidden'}` })}` : ''}`;

  const privHTML = privs.length ? `<div class="sf-priv"><div class="sf-privh">${svg('alert')}<b>Este app requer privilégios</b></div>
      <p>A receita pede poderes sobre o servidor que os apps comuns não têm. Com eles, um app com defeito ou malicioso pode mexer no servidor inteiro. Instale só se confia no app.</p>
      <ul>${privs.map((p) => `<li>${esc(p)}</li>`).join('')}</ul>
      ${edit ? '' : `<label class="sf-check"><input type="checkbox" name="risks"> Entendo os riscos e quero instalar</label>`}</div>` : '';
  const oldNote = !edit && !newScript ? `<p class="st-note">${svg('info')}<span>Para mudar pastas e recursos já na instalação${privs.length ? ' e para instalar apps que requerem privilégios' : ''}, o script do servidor precisa ser atualizado (abra as atualizações, ao lado do sino). Ou use “Abrir no Runtipi”.</span></p>` : '';
  const leftNote = !edit && st0.exists ? `<p class="st-note">${svg('info')}<span>Este app tem Ajustes de uma instalação anterior (o Runtipi guarda ao desinstalar). Eles já estão em Pastas e recursos e Opcionais: mude ou tire o que não quiser.</span></p>` : '';
  const offNote = cur && cur.userConfigEnabled === false ? `<p class="st-note sf-warn">${svg('alert')}<span>Os Ajustes estão desligados para este app no Runtipi: o que você mudar em Pastas e recursos e Opcionais só vale depois de ligar de novo lá.</span></p>` : '';

  box.innerHTML = `<h2>${edit ? `Configurar ${esc(a.name)}` : `Instalar ${esc(a.name)}`}</h2>
    <p class="sf-intro">${edit ? 'Mude o que quiser. Ao salvar, o app reinicia com as mudanças.' : 'O que já vem preenchido é o recomendado. Toque no “?” para entender cada opção.'}</p>
    <form class="sf-form" autocomplete="off" novalidate>
      <div class="sf-scroll">
        ${privHTML}${oldNote}${leftNote}${offNote}
        ${fields.length ? section('ask', 'edit', 'O app pergunta', true, askHTML) : ''}
        ${section('access', 'network', 'Acesso', true, accessHTML)}
        ${ajOK ? '<div data-aj></div>' : F.settingsErr ? section('res', 'folder', 'Pastas e recursos', false, `<p class="st-err">${esc(F.settingsErr)}</p><p class="sf-intro">O arquivo de Ajustes foi escrito à mão no Runtipi e tem um erro. Para não estragar nada, o BrunOS não mexe nele: corrija no editor do Runtipi.</p>${F.settingsRaw ? `<pre class="sf-raw">${esc(F.settingsRaw)}</pre>` : ''}`) : ''}
      </div>
      <div class="sf-foot">
        <div class="sf-pw" hidden>${field('Senha de configuração', `<input class="text-input" type="password" name="pw" autocomplete="off" maxlength="200">`, { help: 'A senha das ações sensíveis do servidor (a mesma de formatar discos e desinstalar apps), diferente da senha do BrunOS. É pedida porque esta instalação muda Ajustes ou dá privilégios ao app.' })}</div>
        <p class="st-err" hidden></p>
        <div class="dlg-actions sf-actions"><button type="button" class="pill-btn sf-ghost" data-c="runtipi">${svg('external')}<span>Abrir no Runtipi</span></button><span class="sf-grow"></span><button type="button" class="pill-btn" data-c="close">Cancelar</button><button type="submit" class="pill-btn primary" data-c="go">${edit ? 'Salvar' : 'Instalar'}</button></div>
      </div>
    </form>`;

  const ajBox = $('[data-aj]', box);
  /* ── Ajustes (redesenhados ao acrescentar ou tirar linhas) ── */
  const syncAj = () => {
    if (!ajBox?.firstChild) return;
    M.mounts = [...ajBox.querySelectorAll('[data-mount]')].map((r) => ({ host: readFolder($('[data-folder]', r)), container: $('[data-dst]', r).value.trim(), readOnly: $('[data-ro]', r).classList.contains('on') }));
    M.env = [...ajBox.querySelectorAll('[data-env]')].map((r) => {
      const i = +r.dataset.env;
      return { key: $('[data-k]', r).value.trim(), value: $('[data-val]', r).value, rec: M.env[i]?.rec ?? null };
    });
    M.memoryMB = Number($('[name="mem"]', ajBox).value) || 0;
    M.cpus = Number($('[name="cpu"]', ajBox).value) || 0;
    M.gpu = $('[data-sw="gpu"]', ajBox).classList.contains('on');
    M.tag = $('[name="tag"]', ajBox)?.value.trim() || '';
    M.restart = $('[name="restart"]', ajBox).value;
    M.shm = Number($('[name="shm"]', ajBox)?.value) || 0;
  };
  const drawAj = () => {
    if (!ajBox) return;
    const openSecs = new Set([...ajBox.querySelectorAll('details[open]')].map((x) => x.dataset.sec));
    if (!ajBox.firstChild && open === 'aj') openSecs.add('res');
    const recMounts = rec.mounts.map((m) => `<div class="sf-lock"><span class="sf-lico" title="Vem da receita do app">${svg('lock')}</span><span class="sf-ltxt"><b>${esc(niceHost(m.host))}</b><small>dentro do app: ${esc(m.container)}${m.readOnly ? ' · só leitura' : ''}</small></span>
      ${m.host.startsWith('/') || m.host.startsWith('$') ? `<button type="button" class="pill-btn sm sf-ghost" data-swap="${esc(m.container)}">Trocar pasta</button>` : ''}</div>`).join('');
    const resHTML = `
      ${rec.mounts.length ? field('Pastas que o app já usa', `<div class="sf-locks">${recMounts}</div>`, { help: 'Vêm da receita do app e não dá para tirar (o Runtipi junta os Ajustes à receita, não apaga). Para usar outra pasta do servidor no lugar, toque em Trocar pasta: entra uma pasta extra com o mesmo destino, que vale no lugar da original.' }) : ''}
      ${field('Pastas extras do servidor', `<div class="sf-mounts">${M.mounts.map((m, i) => `<div class="sf-mount" data-mount="${i}">${folderHTML(folders, m.host, `m${i}`)}
          <span class="sf-arrow">${svg('chevR')}</span><input class="text-input sf-dst" data-dst value="${esc(m.container)}" placeholder="dentro do app (ex.: /movies)" list="sf-dsts" aria-label="Pasta dentro do app">
          <span class="sf-ro">${sw(m.readOnly, 'data-ro', 'Só leitura')}<small>Só leitura</small></span>
          <button type="button" class="pill-btn sm sf-x" data-rm="${i}" aria-label="Tirar">${svg('x')}</button></div>`).join('') || '<p class="sf-empty">Nenhuma pasta extra.</p>'}</div>
        <datalist id="sf-dsts">${DESTINOS.map((x) => `<option value="${x}">`).join('')}</datalist>
        <button type="button" class="pill-btn sm sf-add" data-add="mount">${svg('plus')}Adicionar pasta</button>`, { help: 'Para o app enxergar seus filmes, fotos ou downloads de um disco. À esquerda, a pasta do servidor; à direita, onde ela aparece dentro do app. Só leitura é o recomendado para mídias: o app vê os arquivos, mas não apaga nem muda nada.' })}
      <div class="sf-grid2">${field('Memória', `<select class="text-input" name="mem">${opt(MEMORIA, M.memoryMB, (v) => `${v} MB`)}</select>`, { help: 'O máximo de memória que o app pode usar. Sem limite é o recomendado; limite só um app que costuma pesar no servidor.' })}
        ${field('Processador', `<select class="text-input" name="cpu">${opt(NUCLEOS, M.cpus, (v) => `${v} núcleos`)}</select>`, { help: 'Quantos núcleos do processador o app pode ocupar no máximo. Sem limite é o recomendado.' })}</div>
      ${row('Usar a placa de vídeo', '(converter vídeo no Plex, Jellyfin, Immich…)', sw(M.gpu, 'data-sw="gpu"', 'Placa de vídeo'), 'Dá ao app acesso à placa de vídeo do servidor (/dev/dri), para converter vídeos sem pesar o processador. Só faz diferença em apps de vídeo e fotos.')}`;
    const envRows = M.env.map((v, i) => `<div class="sf-env ${v.rec !== null ? 'rec' : ''}" data-env="${i}">
        <input class="text-input" data-k value="${esc(v.key)}" placeholder="NOME" aria-label="Nome" ${v.rec !== null ? 'readonly' : ''}><span>=</span>
        <input class="text-input" data-val value="${esc(v.value)}" placeholder="valor" aria-label="Valor de ${esc(v.key || 'variável')}">
        ${v.rec !== null ? `<button type="button" class="pill-btn sm sf-x" data-reset="${i}" aria-label="Voltar ao valor da receita" title="Voltar ao valor da receita" ${v.value === v.rec ? 'disabled' : ''}>${svg('refresh')}</button>` : `<button type="button" class="pill-btn sm sf-x" data-rmenv="${i}" aria-label="Tirar">${svg('x')}</button>`}</div>`).join('');
    const optHTML = `
      ${field('Variáveis', `<div class="sf-envs">${envRows || '<p class="sf-empty">A receita não traz variáveis.</p>'}</div>
        <div class="sf-chips"><button type="button" class="pill-btn sm" data-add="env">${svg('plus')}Adicionar variável</button>${PRONTAS.map((p, i) => `<button type="button" class="pill-btn sm sf-ghost" data-preset="${i}">${esc(p.label)}</button>`).join('')}</div>`,
        { help: 'Configurações que o app lê ao ligar (ex.: TZ = fuso horário). As que já vêm preenchidas são as da receita: mude o valor se precisar, ou volte ao original com a setinha. Valores com ${...} são preenchidos pelo Runtipi com as respostas da instalação.' })}
      <div class="sf-grid2">${field('Versão da imagem', manualImage ? `<input class="text-input" value="${esc(st0.image)}" disabled>` : `<input class="text-input" name="tag" value="${esc(M.tag)}" placeholder="${esc(imageTag(rec.image) || 'latest')} (a do app)" autocomplete="off" spellcheck="false">`,
        { help: manualImage ? 'A imagem foi trocada à mão no Runtipi; o BrunOS deixa como está.' : `Deixe em branco para seguir a versão do app (recomendado): ele atualiza junto com a loja. Escrever uma versão (ex.: ${esc(imageTag(rec.image) || '1.2.3')}) fixa o app nela, e as atualizações da loja deixam de trocar a versão.` })}
        ${field('Reiniciar sozinho', `<select class="text-input" name="restart">${opt(REINICIO, M.restart, (v) => v)}</select>`, { help: 'O que o Docker faz quando o app para ou o servidor reinicia. “Como o app vem” usa o que a receita e o Runtipi já definem.' })}</div>
      ${field(`Memória compartilhada${needShm ? ' <i>recomendado: 1 GB</i>' : ''}`, `<select class="text-input" name="shm">${opt(SHM.map(([v, l]) => [v, v === 0 && rec.shmMB ? `Como o app vem (${rec.shmMB >= 1024 ? `${rec.shmMB / 1024} GB` : `${rec.shmMB} MB`})` : v === 0 ? 'Como o app vem (64 MB)' : v === 1024 && rec.desktop ? '1 GB (recomendado para este app)' : l]), M.shm, (v) => `${v} MB`)}</select>`,
        { help: 'Área de memória que alguns programas usam para trocar dados rápido (/dev/shm). O Docker dá só 64 MB. Navegadores e programas com tela pelo navegador (Brave, Chromium, Firefox, Webtop...) ficam lentos e travando com isso: para eles, 1 GB.', hint: needShm ? 'Este app costuma travar com o padrão de 64 MB.' : '' })}
      ${edit ? field('Limite de backups', `<input class="text-input" type="number" name="maxBackups" min="0" max="100" value="${cur?.maxBackups ?? ''}" placeholder="padrão do Runtipi">`, { help: 'Quantos backups do app o Runtipi guarda; os mais antigos são apagados. Em branco, vale o padrão das configurações do Runtipi; 0 = sem limite.' }) : ''}
      ${rec.ports.length ? field('Portas extras da receita', `<div class="sf-locks">${rec.ports.map((p) => `<div class="sf-lock"><span class="sf-lico">${svg('lock')}</span><span class="sf-ltxt"><b>${esc(p)}</b><small>servidor : app</small></span></div>`).join('')}</div>`, { help: 'Portas que o app abre no servidor além da principal (para descoberta na rede, streaming…). Vêm da receita.' }) : ''}`;
    const advHTML = `${st0.manual.length ? `<p class="st-note">${svg('info')}<span>Feito à mão no Runtipi, fica como está: ${st0.manual.map(esc).join(', ')}.</span></p>` : ''}
      ${st0.risks.length ? `<p class="st-note sf-warn">${svg('alert')}<span>O arquivo já tinha, escrito à mão: ${st0.risks.map(esc).join(', ')}. O BrunOS mantém, mas não acrescenta nada parecido.</span></p>` : ''}
      ${field('Arquivo de Ajustes', `${edit ? `<div class="sf-rawbar"><button type="button" class="pill-btn sm" data-c="raw-edit">${svg('edit')}Editar o arquivo</button><small>Para opções que não têm botão (command, labels, healthcheck, sysctls...).</small></div>` : ''}
        <div class="sf-rawed" data-rawed hidden></div><pre class="sf-raw" data-rawpre>${esc(F.settingsRaw || '(ainda não existe: o BrunOS cria ao salvar, se você mudar algo)')}</pre>`,
        { help: `O arquivo user-config do Runtipi onde ficam os Ajustes, no formato do docker-compose. O Runtipi junta ele à receita do app em todo início, e ele vale depois das atualizações.${edit ? ' Editando o texto, os botões de Pastas e recursos e Opcionais ficam parados até salvar. Se o texto der privilégios novos ao app (privileged, rede do servidor, aparelhos, pastas do sistema...), o BrunOS mostra a lista e só grava depois de “Entendo os riscos”.' : ' Depois de instalar, dá para editar o texto em Configurar.'}` })}`;
    ajBox.innerHTML = section('res', 'folder', 'Pastas e recursos', openSecs.has('res'), resHTML) + section('opt', 'sliders', 'Opcionais', openSecs.has('opt'), optHTML)
      + section('adv', 'code', 'Avançado', openSecs.has('adv'), advHTML);
    wireFolders(ajBox, folders);
  };
  drawAj();

  // o que a janela mandaria hoje (para saber o que mudou)
  const settingsOut = () => {
    syncAj();
    return { mounts: M.mounts, memoryMB: M.memoryMB, cpus: M.cpus, gpu: M.gpu,
      env: M.env.filter((v) => v.key && (v.rec === null || v.value !== v.rec)).map(({ key, value }) => ({ key, value })),
      image: manualImage ? st0.image : M.tag ? `${repo}:${M.tag}` : '', restart: M.restart, shmMB: M.shm };
  };
  const on = (k) => $(`[data-sw="${k}"]`, box)?.classList.contains('on') || false;
  const instBody = () => {
    const values = {};
    box.querySelectorAll('[data-v]').forEach((el) => {
      const k = el.dataset.v, kind = el.dataset.kind;
      if (kind === 'bool') values[k] = el.classList.contains('on');
      else if (kind === 'folder') values[k] = readFolder($('[data-folder]', el));
      else if (kind === 'number') values[k] = el.value === '' ? '' : Number(el.value);
      else if (kind === 'password') values[k] = el.value;
      else values[k] = el.value.trim();
    });
    const b = { urn: a.urn, values, port: Number($('[name="port"]', box).value) || 0, openPort: on('openPort'), exposedLocal: on('exposedLocal'), exposed: on('exposed'),
      domain: $('[name="domain"]', box)?.value.trim() || '', enableAuth: on('enableAuth'), guest: on('guest') };
    const s = $('[name="sub"]', box);
    if (s) b.localSubdomain = s.value.trim().toLowerCase();
    const mb = $('[name="maxBackups"]', box);
    if (edit && mb && mb.value !== '') b.maxBackups = Number(mb.value);
    return b;
  };
  const inst0 = JSON.stringify(instBody());
  const aj0 = ajOK ? JSON.stringify(settingsOut()) : '';
  // 0.22.0: editor do texto do arquivo de Ajustes (CodeMirror, carregado só ao clicar em "Editar o arquivo")
  let rawEd = null;
  const raw0 = F.settingsRaw || '';
  const rawDirty = () => !!rawEd && rawEd.text() !== raw0;
  const ajDirty = () => (rawEd ? rawDirty() : ajOK && JSON.stringify(settingsOut()) !== aj0);
  async function openRawEditor() {
    if (rawEd || !ajBox) return;
    const btn = $('[data-c="raw-edit"]', box);
    btn.disabled = true; btn.innerHTML = '<span class="upd-spin"></span>Abrindo o editor…';
    let cm;
    try { cm = await import('../vendor/codemirror/codemirror.mjs'); } catch { btn.disabled = false; btn.textContent = 'Não deu para abrir o editor'; return; }
    const host = $('[data-rawed]', box);
    host.hidden = false; $('[data-rawpre]', box).hidden = true;
    rawEd = cm.createEditor({ parent: host, doc: raw0, name: 'docker-compose.yml', onChange: () => refresh() });
    ajBox.classList.add('raw-mode');
    btn.closest('.sf-rawbar').innerHTML = `<small>${svg('info')} Editando o texto: os botões de Pastas e recursos e Opcionais ficam parados até salvar.</small>`;
    rawEd.focus();
    refresh();
  }
  const needPw = () => ajDirty() || (!edit && privs.length > 0);

  // resumo das seções fechadas e o campo de senha, conforme o que está escolhido
  const refresh = () => {
    if (ajBox?.firstChild) {
      const extra = M.mounts.length, envCh = settingsOut().env.length;
      const res = [`${rec.mounts.length + extra} ${rec.mounts.length + extra === 1 ? 'pasta' : 'pastas'}${extra ? ` (${extra} extra${extra > 1 ? 's' : ''})` : ''}`,
        M.memoryMB ? `memória ${MEMORIA.find(([v]) => v === M.memoryMB)?.[1] || `${M.memoryMB} MB`}` : 'sem limite', M.gpu ? 'placa de vídeo' : ''].filter(Boolean);
      $('[data-sec="res"] [data-sum]', box).textContent = res.join(' · ');
      $('[data-sec="opt"] [data-sum]', box).textContent = [`${M.env.length} ${M.env.length === 1 ? 'variável' : 'variáveis'}${envCh ? ` (${envCh} mudada${envCh > 1 ? 's' : ''})` : ''}`,
        M.tag ? `versão ${M.tag}` : 'versão do app', M.restart ? REINICIO.find(([v]) => v === M.restart)?.[1].toLowerCase() : ''].filter(Boolean).join(' · ');
      $('[data-sec="adv"] [data-sum]', box).textContent = st0.exists ? 'arquivo de Ajustes' : 'nada escrito ainda';
    }
    const acc = [on('exposedLocal') && $('[name="sub"]', box) ? `${$('[name="sub"]', box).value || a.id}.${F.localDomain}` : `porta ${$('[name="port"]', box).value || port}`, on('enableAuth') ? 'com login' : '', on('exposed') ? 'na internet' : ''].filter(Boolean);
    $('[data-sec="access"] [data-sum]', box).textContent = acc.join(' · ');
    if (fields.length) $('[data-sec="ask"] [data-sum]', box).textContent = `${fields.length} ${fields.length === 1 ? 'pergunta' : 'perguntas'}`;
    $('.sf-pw', box).hidden = !needPw();
    // setinha de "voltar ao valor da receita" só acende quando o valor mudou
    ajBox?.querySelectorAll('[data-reset]').forEach((b) => { const v = M.env[+b.dataset.reset]; b.disabled = !v || v.value === v.rec; });
  };

  /* ── Eventos ── */
  box.addEventListener('click', (e) => {
    const t = (s) => e.target.closest(s);
    let el;
    if ((el = t('.sf-q'))) {
      e.preventDefault();
      const h = el.closest('.sf-field, .sf-rowwrap')?.querySelector(':scope > .sf-help');
      if (h) { h.hidden = !h.hidden; el.setAttribute('aria-expanded', String(!h.hidden)); el.classList.toggle('on', !h.hidden); }
      return;
    }
    if ((el = t('.switch'))) {
      el.classList.toggle('on'); el.setAttribute('aria-checked', el.classList.contains('on'));
      const k = el.dataset.sw;
      if (k === 'exposed') { const dm = $('[data-domain]', box); if (dm) dm.hidden = !el.classList.contains('on'); }
      if (k === 'exposedLocal') { const lc = $('[data-local]', box); if (lc) lc.hidden = !el.classList.contains('on'); }
      refresh();
      return;
    }
    if (t('[data-add="mount"]')) { syncAj(); M.mounts.push({ host: folders[0]?.host || '', container: '', readOnly: true }); drawAj(); }
    else if ((el = t('[data-swap]'))) { syncAj(); if (!M.mounts.some((m) => m.container === el.dataset.swap)) M.mounts.push({ host: folders[0]?.host || '', container: el.dataset.swap, readOnly: false }); drawAj(); }
    else if (t('[data-add="env"]')) { syncAj(); M.env.push({ key: '', value: '', rec: null }); drawAj(); }
    else if ((el = t('[data-rm]'))) { syncAj(); M.mounts.splice(+el.dataset.rm, 1); drawAj(); }
    else if ((el = t('[data-rmenv]'))) { syncAj(); M.env.splice(+el.dataset.rmenv, 1); drawAj(); }
    else if ((el = t('[data-reset]'))) { syncAj(); const v = M.env[+el.dataset.reset]; v.value = v.rec; drawAj(); }
    else if ((el = t('[data-preset]'))) {
      syncAj();
      for (const [k, v] of PRONTAS[+el.dataset.preset].vars) { const x = M.env.find((y) => y.key === k); if (x) x.value = v; else M.env.push({ key: k, value: v, rec: null }); }
      drawAj();
    } else if (t('[data-c="raw-edit"]')) { openRawEditor(); return; }
    else if (t('[data-c="term"]')) { d.close(); ctx.openTerminal?.({ typeCmd: CMD_SENHA }); toast('Comando digitado no Terminal', 'Confira e aperte Enter.'); return; }
    else if (t('[data-c="runtipi"]')) { d.close(); ctx.runtipi(); return; }
    else if (t('[data-c="close"]')) { d.close(); return; }
    else return;
    refresh();
  });
  box.addEventListener('input', refresh);
  box.addEventListener('change', refresh);
  refresh();

  const err = $('.sf-foot .st-err', box), go = $('[data-c="go"]', box);
  const showErr = (m, isHtml = false) => { if (isHtml) err.innerHTML = m; else err.textContent = m; err.hidden = false; err.scrollIntoView({ block: 'nearest' }); };
  $('form', box).addEventListener('submit', async (e) => {
    e.preventDefault();
    if (busy) return;
    err.hidden = true;
    const settings = ajOK ? settingsOut() : null;
    const bad = settings?.mounts.find((m) => !m.container.startsWith('/') || !m.host);
    if (bad) { openSec('res'); showErr('Em cada pasta extra, escolha a pasta do servidor e onde ela aparece dentro do app (começando com /, ex.: /movies).'); return; }
    if (!edit && privs.length && !$('[name="risks"]', box)?.checked) { box.querySelector('.sf-scroll').scrollTop = 0; showErr('Este app requer privilégios: leia a lista no começo e marque “Entendo os riscos”.'); return; }
    if (!edit && privs.length && !newScript) { showErr('Atualize o script do servidor para instalar este app pelo BrunOS, ou use “Abrir no Runtipi”.'); return; }
    const password = $('[name="pw"]', box).value;
    if (needPw() && !password) { showErr('Digite a Senha de configuração.'); $('[name="pw"]', box).focus(); return; }
    const body = instBody();
    const instDirty = JSON.stringify(body) !== inst0, setDirty = ajDirty();
    if (edit && !instDirty && !setDirty) { d.close(); toast('Nada mudou', `${a.name} continua como estava.`); return; }
    busy = true; go.disabled = true;
    go.innerHTML = `<span class="upd-spin"></span>${edit ? 'Salvando…' : 'Mandando ao Runtipi…'}`;
    let savedSettings = false;
    try {
      if (edit) {
        if (setDirty && rawEd) {
          const r = await api.store.saveSettingsRaw({ urn: a.urn, password, restart: !instDirty, raw: rawEd.text(), acceptRisks: !!$('[name="rawrisks"]', box)?.checked });
          savedSettings = true;
          if (r.restartErr) toast(`Ajustes de ${a.name} salvos`, `Mas o app não reiniciou: ${r.restartErr}`, null, { error: true });
          if (r.risks?.length) toast(`${a.name} agora requer privilégios`, r.risks.join('; '));
        } else if (setDirty) { const r = await api.store.saveSettings({ urn: a.urn, password, restart: !instDirty, settings }); savedSettings = true; if (r.restartErr) toast(`Ajustes de ${a.name} salvos`, `Mas o app não reiniciou: ${r.restartErr}`, null, { error: true }); }
        if (instDirty) await api.store.reconfig(body);
      } else {
        await api.store.install({ ...body, ...(ajOK && newScript ? { settings } : {}), password: needPw() ? password : '', acceptRisks: !!$('[name="risks"]', box)?.checked });
      }
    } catch (ex) {
      busy = false; go.disabled = false; go.textContent = edit ? 'Salvar' : 'Instalar';
      const m = ex.message || String(ex);
      if (ex.data?.risks?.length) { // texto novo dá privilégios: mostra a lista e pede "Entendo os riscos"
        let rb = $('.sf-rawrisks', box);
        if (!rb) { rb = document.createElement('div'); rb.className = 'sf-priv sf-rawrisks'; err.before(rb); }
        rb.innerHTML = `<div class="sf-privh">${svg('alert')}<b>O texto novo dá privilégios ao app</b></div><p>Com eles, um app com defeito ou malicioso pode mexer no servidor inteiro:</p>
          <ul>${ex.data.risks.map((p) => `<li>${esc(p)}</li>`).join('')}</ul><label class="sf-check"><input type="checkbox" name="rawrisks"> Entendo os riscos e quero dar esses privilégios</label>`;
        showErr('Leia a lista acima, marque “Entendo os riscos” e salve de novo.');
        return;
      }
      if (/ainda não existe Senha de (configuração|formatação)/.test(m)) {
        showErr(`Ainda não existe Senha de configuração. Ela é criada uma vez, no Terminal do servidor: <code>${CMD_SENHA}</code>${ctx.openTerminal ? ` <button type="button" class="pill-btn sm" data-c="term">${svg('terminal')}Criar agora</button>` : ''}`, true);
      } else showErr(savedSettings ? `Os Ajustes foram salvos, mas a instalação não mudou: ${m}` : m);
      if (savedSettings) ctx.done();
      return;
    }
    busy = false;
    if (edit) { d.close(); toast(`${a.name} configurado`, 'O app reiniciou com as mudanças.'); ctx.done(); return; }
    follow(a, ctx, box, d);
  });

  function openSec(k) { const s = $(`[data-sec="${k}"]`, box); if (s) s.open = true; }
  function fail(msg) {
    box.innerHTML = `<h2>${esc(a.name)}</h2><p class="st-err">${esc(msg)}</p><div class="dlg-actions sf-actions"><button type="button" class="pill-btn sf-ghost" data-c="runtipi">${svg('external')}Abrir no Runtipi</button><span class="sf-grow"></span><button type="button" class="pill-btn" data-c="close">Fechar</button></div>`;
    $('[data-c="runtipi"]', box).onclick = () => { d.close(); ctx.runtipi(); };
    $('[data-c="close"]', box).onclick = () => d.close();
  }
}

// Instalações em andamento (0.21.2): a Loja mostra "Instalando…" no lugar de Instalar e o sino ganha um anel
// animado enquanto houver alguma. Avisa por um evento da janela ("brunos:installs"), sem acoplar os módulos.
export const installing = new Map(); // urn → { name, since }
function installEvent(a, on) {
  if (on) installing.set(a.urn, { name: a.name, since: Date.now() }); else installing.delete(a.urn);
  const list = [...installing.entries()].map(([urn, v]) => ({ urn, ...v }));
  window.brunosInstalls = list; // para a Loja que abrir depois saber o que já está instalando
  window.dispatchEvent(new CustomEvent('brunos:installs', { detail: { list } }));
}

// Acompanha a instalação até o app ligar (continua mesmo se a janela for fechada).
async function follow(a, ctx, box, d) {
  installEvent(a, true);
  try { await followLoop(a, ctx, box, d); } finally { installEvent(a, false); }
}
async function followLoop(a, ctx, box, d) {
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
      view(`<h2>A instalação não deu certo</h2><p class="sf-intro">O Runtipi não conseguiu instalar ${esc(a.name)} e desfez a instalação. Se você mudou Ajustes, eles foram tirados (uma cópia ficou no servidor). O motivo fica nos registros do painel do Runtipi (por exemplo: uma pergunta obrigatória ou a imagem que não baixou).</p>
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

// nomes antigos (0.20.0)
export const installDialog = (a, ctx, o = {}) => appDialog(a, ctx, o);
export const settingsDialog = (a, ctx) => appDialog(a, ctx, { edit: true, open: 'aj' });

export const _test = { splitHost, joinHost, isFolderField, imageRepo, imageTag, niceHost };
