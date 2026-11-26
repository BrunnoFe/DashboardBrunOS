// Novidades (0.33.0, decisão 71): a janelinha com o que mudou, uma vez por navegador na primeira
// abertura de cada versão nova (também quando a atualização foi pelo Runtipi). Pulou versões: mostra
// todas, a mais nova em cima. Dá para rever em Configurações (clicando na versão do BrunOS).
// Como escrever: título curto em negrito e uma ou duas frases simples embaixo, como quem conta para um
// amigo. Nada de jargão; nenhuma frase com cara de manual.
import { esc, openDialog } from './ui.js';

export const NOVIDADES = [
  { v: '0.37.0', date: '2026-11-26', items: [
    { t: 'Seu Nextcloud dentro do Arquivos', d: 'Na integração do Nextcloud, coloque seu usuário e uma senha de app. A pasta dele aparece em Nuvem, como uma pasta do servidor.' },
    { t: 'Leve e traga arquivos', d: 'Copie ou mova entre o Nextcloud e os discos do servidor, envie, renomeie e crie pastas. O que você apaga vai para a lixeira do próprio Nextcloud.' },
    { t: 'Vídeos e fotos de lá também', d: 'Abra fotos e vídeos do Nextcloud direto na pré-visualização, avançando e voltando no vídeo sem baixar tudo.' },
  ] },
  { v: '0.36.0', date: '2026-11-26', items: [
    { t: 'De olho na placa de vídeo', d: 'Em Uso ao vivo apareceu o cartão GPU: uso, frequência, temperatura e consumo, com gráfico de 5 minutos até 30 dias.' },
    { t: 'Widgets da GPU', d: 'Em Widgets → Modelos, escolha GPU (os números de agora) ou GPU 24 h (o gráfico do dia).' },
    { t: 'Placa integrada também conta', d: 'Na Intel do servidor, o uso é uma estimativa pelo tempo em que ela descansa, e a memória de vídeo é a própria RAM.' },
  ] },
  { v: '0.35.0', date: '2026-11-26', items: [
    { t: 'Widget de torrents', d: 'Em Widgets → Modelos → Transmission: torrents. Mostra cada torrent com progresso, velocidades e estado, com filtro e botões para pausar ou retomar tudo e ligar o modo tartaruga.' },
    { t: 'Atualização que dá errado avisa', d: 'Se a versão nova não puder ser baixada, a tela de atualização some e diz o que aconteceu, em vez de fingir que deu certo.' },
    { t: 'Volta já na versão nova', d: 'Depois de atualizar o BrunOS, a página só recarrega quando a versão nova estiver no ar. Sem precisar apertar F5.' },
  ] },
  { v: '0.34.1', date: '2026-11-26', items: [
    { t: 'Escudo do AdGuard no canto esquerdo', d: 'Assim ele não fica em cima da logo no celular.' },
    { t: 'Volta ao verde na hora', d: 'Quando a pausa acaba, o escudo já mostra que o AdGuard voltou a proteger, sem demora.' },
    { t: 'Lembrete no "Até eu ligar"', d: 'Desligou até ligar de novo? O sino lembra, e o aviso some sozinho quando a proteção volta.' },
  ] },
  { v: '0.34.0', date: '2026-11-25', items: [
    { t: 'Chega de filme mudo sem explicação', d: 'Quando o som de um vídeo é de um tipo que o navegador não toca (DTS, Dolby, TrueHD), a prévia avisa e oferece baixar ou abrir no Plex, que converte o som.' },
    { t: 'Pausar o AdGuard num toque', d: 'Um escudinho ao lado das Atualizações mostra se o AdGuard está protegendo. Toque nele para pausar por 5 minutos, meia hora, 1 hora ou até ligar de novo.' },
    { t: 'Prévia mais arrumada', d: 'O botão Editar só aparece em arquivo de texto, e no celular o nome do arquivo volta a caber no topo.' },
  ] },
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
