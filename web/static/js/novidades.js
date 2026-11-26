// Novidades (0.33.0, decisão 71): a janelinha com o que mudou, uma vez por navegador na primeira
// abertura de cada versão nova (também quando a atualização foi pelo Runtipi). Pulou versões: mostra
// todas, a mais nova em cima. Dá para rever em Configurações (clicando na versão do BrunOS).
// Como escrever: título curto em negrito e uma ou duas frases simples embaixo, como quem conta para um
// amigo. Nada de jargão; nenhuma frase com cara de manual.
import { esc, openDialog } from './ui.js';

export const NOVIDADES = [
  { v: '0.33.0', date: '2026-11-25', items: [
    { t: 'Uma tela bonita enquanto atualiza', d: 'Ao atualizar o BrunOS ou reiniciar o servidor, aparece a logo girando com as etapas. Quando tudo volta, a página recarrega sozinha.' },
    { t: 'Tudo um pouco menor, se você quiser', d: 'Em Configurações → Aparência, dá para diminuir (ou aumentar) a tela inicial e as janelas, de 70% a 110%. Cada aparelho guarda a sua.' },
    { t: 'Anel no ícone do app que está atualizando', d: 'Você vê de longe quem está se atualizando. Quando termina, aparece um ✓ rapidinho.' },
    { t: 'Esta janelinha', d: 'Ela aparece uma vez a cada versão nova. Para rever, é só clicar na versão do BrunOS, nas Configurações.' },
  ] },
  { v: '0.32.0', date: '2026-11-25', items: [
    { t: 'Cabeçalho da lista parado no lugar', d: 'No Arquivos, Nome, Modificado, Tamanho e Tipo ficam fixos no topo, sem os arquivos passando por cima ao rolar.' },
    { t: 'Caminho que cabe na janela', d: 'Em pastas bem fundas, as do meio viram "…". Um clique nele e você pula para qualquer uma.' },
    { t: 'Arquivos bem no celular', d: 'Numa janela estreita, os botões viram ícones e a busca vira uma lupa, sem nada encavalado.' },
    { t: 'Atualizações em dia de verdade', d: 'Depois de atualizar um app ou o Linux, o painel já mostra que está tudo certo, sem continuar oferecendo a mesma atualização.' },
  ] },
];

const KEY = 'brunos.seenVersion';
const parse = (v) => (/^\d+\.\d+\.\d+$/.test(v || '') ? v.split('.').map(Number) : null);
function cmp(a, b) { const x = parse(a), y = parse(b); for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] - y[i]; return 0; }
const MESES = ['jan.', 'fev.', 'mar.', 'abr.', 'maio', 'jun.', 'jul.', 'ago.', 'set.', 'out.', 'nov.', 'dez.'];
const fmtDate = (d) => { const [y, m, dd] = d.split('-').map(Number); return `${dd} de ${MESES[m - 1]} de ${y}`; };

/** Ao entrar: versão nova neste navegador → mostra as novidades dela (e das puladas). */
export function checkNovidades(version) {
  if (!parse(version)) return; // modo demonstração / versão de teste
  let seen = '';
  try { seen = localStorage.getItem(KEY) || ''; } catch { return; }
  if (seen === version) return;
  // primeira vez com as Novidades: conta como se viesse da versão de antes desta (mostra só a atual)
  if (!parse(seen)) seen = NOVIDADES.find((n) => cmp(n.v, version) < 0)?.v || '0.0.0';
  const list = NOVIDADES.filter((n) => cmp(n.v, seen) > 0 && cmp(n.v, version) <= 0);
  try { localStorage.setItem(KEY, version); } catch { /* sem armazenamento: aparece de novo */ }
  if (list.length) showNovidades(list);
}

/** Instalação nova: marca a versão como vista (as Novidades começam a contar da próxima). */
export function markSeen(version) {
  if (!parse(version)) return;
  try { if (!localStorage.getItem(KEY)) localStorage.setItem(KEY, version); } catch { /* sem armazenamento */ }
}

/** Abre a janelinha (sem lista: as das últimas versões, para rever). */
export function showNovidades(list = NOVIDADES.slice(0, 3)) {
  const sec = (n) => `<section class="nv-ver"><div class="nv-ver-head"><b>${esc(n.v)}</b><small>${esc(fmtDate(n.date))}</small></div>
    <ul>${n.items.map((it) => `<li><b>${esc(it.t)}</b><small>${esc(it.d)}</small></li>`).join('')}</ul></section>`;
  const d = openDialog(`<div class="nv-head"><span class="logo" aria-hidden="true"></span><div><h2>Novidades do BrunOS</h2>
      <small>${list.length > 1 ? `O que chegou nas últimas ${list.length} versões` : `O que chegou na versão ${esc(list[0].v)}`}</small></div></div>
    <div class="nv-list">${list.map(sec).join('')}</div>
    <div class="dlg-actions"><button class="pill-btn primary" data-nv="ok" autofocus>Legal!</button></div>`, { cls: 'nv-dlg', width: 480 });
  d.el.addEventListener('click', (e) => { if (e.target.closest('[data-nv="ok"]')) d.close(); });
  return d;
}
