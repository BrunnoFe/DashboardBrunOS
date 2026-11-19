// Detalhes dos avisos importantes (0.25.0): ao clicar num aviso de nível Aviso ou Importante, o painel do
// sino abre a explicação, o que fazer, os dados do caso e um botão de atalho. Sem DOM: os testes
// (tests/notifdetails.test.mjs) importam este arquivo direto.

// Só os avisos de nível Aviso e Importante abrem (escolha do Bruno).
export const canExpand = (e) => e?.level === 'aviso' || e?.level === 'critico';

const DISKS = { label: 'Abrir Discos', act: 'disks' };
const ENERGY = { label: 'Abrir Energia', act: 'power' };
const UPDATES = { label: 'Abrir atualizações', act: 'updates' };
const ACCOUNT = { label: 'Trocar a senha', act: 'password' };

// kind → { why, todo, action? }. action.act é tratado em app.js (setNotifActions).
const DETAILS = {
  'app-down': {
    why: 'O BrunOS testa cada app a cada minuto pela porta dele. Este app não respondeu por mais de 2 minutos seguidos.',
    todo: 'Abra os detalhes do app para ver os logs e reiniciar. Se ele voltar sozinho, chega o aviso "voltou".',
    action: { label: 'Ver detalhes do app', act: 'app' },
  },
  'disk-full': {
    why: 'Um disco está quase sem espaço. Disco cheio faz apps travarem e pode estragar arquivos que estavam sendo gravados.',
    todo: 'Apague ou mova arquivos grandes (a lixeira do Arquivos também ocupa espaço até ser esvaziada).',
    action: DISKS,
  },
  'disk-full-soon': {
    why: 'Pelo ritmo dos últimos dias, este disco vai encher em breve.',
    todo: 'Libere espaço com calma antes que encha, ou planeje um disco maior.',
    action: DISKS,
  },
  'disk-gone': {
    why: 'Um disco que o BrunOS conhecia não aparece mais no servidor: cabo solto, adaptador USB sem energia ou disco com defeito.',
    todo: 'Confira o cabo e a fonte do disco. Quando ele voltar, chega o aviso de volta.',
    action: DISKS,
  },
  'disk-hot': {
    why: 'A temperatura do disco passou do limite seguro. HDs acima de uns 50 °C e SSDs acima de uns 70 °C se desgastam mais rápido.',
    todo: 'Melhore a ventilação, afaste o disco de fontes de calor e evite cópias longas até ele esfriar.',
    action: DISKS,
  },
  'disk-unmounted': {
    why: 'A pasta do disco continua mapeada, mas não tem disco montado nela. O que for gravado ali vai para o disco do sistema, que pode encher.',
    todo: 'Confira o cabo e monte de novo pela janela Discos. Até resolver, pare os apps que gravam nessa pasta.',
    action: DISKS,
  },
  'disk-smart-fail': {
    why: 'O próprio disco avisa, pelo SMART, que está falhando. Isso costuma acontecer antes de perder dados.',
    todo: 'Copie agora o que for importante para outro lugar e planeje trocar o disco.',
    action: DISKS,
  },
  'disk-smart-sectors': {
    why: 'O disco tem setores danificados ou trocados por reservas. Alguns são normais com o tempo, mas o número subindo é sinal de desgaste.',
    todo: 'Acompanhe na janela Discos. Se continuar subindo, faça backup e troque o disco.',
    action: DISKS,
  },
  'disk-selftest': {
    why: 'O teste que o próprio disco faz em si mesmo terminou com problema.',
    todo: 'Veja o resultado na janela Discos e faça backup do que importa.',
    action: DISKS,
  },
  'internet-down': {
    why: 'O servidor não conseguiu falar com a internet (o BrunOS testa em três provedores diferentes).',
    todo: 'Confira o roteador e o cabo. Os apps usados dentro de casa continuam funcionando.',
    action: { label: 'Ver a internet', act: 'internet' },
  },
  'power-outage': {
    why: 'A tomada parou de mandar energia e o servidor está funcionando na bateria.',
    todo: 'Se a energia demorar, o desligamento seguro (se estiver ligado) desliga o servidor antes de a bateria acabar.',
    action: ENERGY,
  },
  'power-shutdown': {
    why: 'A bateria chegou ao limite configurado sem energia da tomada. O BrunOS vai desligar o servidor para ele não apagar de repente.',
    todo: 'Para cancelar, abra a janela Energia. Quando a energia voltar, ligue o servidor pelo botão.',
    action: ENERGY,
  },
  'power-shutdown-sent': {
    why: 'O pedido de desligar foi enviado ao servidor, que desliga em 1 minuto.',
    todo: 'Quando a energia voltar, ligue o servidor pelo botão. O BrunOS volta sozinho.',
  },
  'power-shutdown-failed': {
    why: 'O BrunOS tentou desligar o servidor pela conexão SSH e não conseguiu.',
    todo: 'Teste a conexão na janela Energia. Enquanto estiver na bateria, ele continua tentando a cada minuto.',
    action: ENERGY,
  },
  'power-setup-old': {
    why: 'O servidor está com uma configuração antiga do desligamento, que o Ubuntu passou a recusar.',
    todo: 'Na janela Energia, copie os comandos novos, cole no servidor e clique em Testar.',
    action: ENERGY,
  },
  'power-manual': {
    why: 'Foi pedido pelo BrunOS (com a senha) para desligar ou reiniciar o servidor. Ele espera 1 minuto antes.',
    todo: 'Se não foi você, cancele agora e troque a senha do BrunOS.',
    action: { label: 'Cancelar', act: 'cancel-power' },
  },
  'script-upgrade': {
    why: 'O script do servidor (o programa que faz as ações que precisam de administrador) não conseguiu se atualizar sozinho.',
    todo: 'Abra o botão de atualizações e use "Pelo Terminal": o comando já vem digitado.',
    action: UPDATES,
  },
  'os-reboot': {
    why: 'O Ubuntu instalou uma atualização (do núcleo do sistema, por exemplo) que só passa a valer depois de reiniciar o servidor.',
    todo: 'Reinicie quando for um bom momento: os apps e o BrunOS param por alguns minutos e voltam sozinhos.',
    action: { label: 'Reiniciar agora', act: 'reboot' },
  },
  'update-done': {
    why: 'A atualização não terminou bem. O motivo está no texto do aviso e no registro da atualização.',
    todo: 'Abra as atualizações para ver o registro e tentar de novo. Se um app ficou fora do ar, o backup feito antes pode ser restaurado pela página do app no Runtipi.',
    action: UPDATES,
  },
  'login-blocked': {
    why: 'Houve muitas tentativas de senha errada seguidas, e o BrunOS bloqueou esse endereço por um tempo.',
    todo: 'Se não foi você, alguém está tentando entrar: confira quem tem acesso à sua rede e troque a senha.',
    action: ACCOUNT,
  },
  'login-new': {
    why: 'O BrunOS foi acessado de um lugar novo (uma rede ou um navegador que ele ainda não conhecia).',
    todo: 'Se não foi você, troque a senha: as outras sessões são encerradas na hora.',
    action: ACCOUNT,
  },
  store: {
    why: 'O Runtipi não conseguiu terminar o pedido feito pela Loja.',
    todo: 'Abra a Loja e tente de novo. Falta de espaço ou de internet são as causas mais comuns; os logs do app ajudam a entender.',
    action: { label: 'Abrir a Loja', act: 'store' },
  },
};

// Explicação genérica para um aviso importante sem texto próprio (tipo novo, por exemplo).
const GENERIC = { why: 'O BrunOS registrou este aviso como importante.', todo: 'Leia o texto acima. Se o problema continuar, ele avisa de novo.' };

const fmtBytes = (b) => {
  const u = ['B', 'KB', 'MB', 'GB', 'TB']; let i = 0; let n = Math.abs(b);
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(n < 10 && i ? 1 : 0).replace('.', ',')} ${u[i]}`;
};
// dados que aparecem na lista; os outros campos do meta (prazo, simulação...) ficam de fora
const FACTS = {
  urn: ['App', (v) => String(v).split(':')[0]],
  ip: ['Endereço', String],
  browser: ['Navegador', String],
  os: ['Sistema', String],
  local: ['Rede', (v) => (v ? 'de casa' : 'de fora (internet)')],
  temp: ['Temperatura', (v) => `${Math.round(v)} °C`],
  daysLeft: ['Enche em', (v) => `${Math.max(0, Math.round(v))} dias`],
  perDay: ['Ritmo', (v) => `${fmtBytes(v)} por dia`],
};

/** Detalhes de um aviso: { why, todo, action?, facts: [[rótulo, valor]] } ou null se não abre. */
export function notifDetail(e, nowSec = Date.now() / 1000) {
  if (!canExpand(e)) return null;
  const d = DETAILS[e.kind] || GENERIC;
  const facts = Object.entries(e.meta || {}).filter(([k, v]) => FACTS[k] && v !== '' && v != null).map(([k, v]) => [FACTS[k][0], FACTS[k][1](v)]);
  facts.push(['Quando', new Date(e.time * 1000).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })]);
  let action = d.action || null;
  if (action?.act === 'app' && !e.meta?.urn) action = null;
  if (action?.act === 'cancel-power' && !(e.meta?.deadline > nowSec)) action = null; // já passou: nada a cancelar
  return { why: d.why, todo: d.todo, action, facts };
}

export const KNOWN_KINDS = Object.keys(DETAILS);
