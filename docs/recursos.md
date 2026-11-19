# Recursos em detalhe

[← Voltar ao README](../README.md)

Tudo o que o BrunOS faz, assunto por assunto.

## Tela inicial, busca e widgets

- Papel de parede (os do pacote ou o seu), cor de destaque tirada da imagem e efeito de vidro ajustável.
- Os apps do Runtipi aparecem como ícones, com a bolinha de status. Arraste um app sobre outro para criar uma pasta; com muitos apps, a tela inicial vira páginas.
- **Ícones:** em *Personalizar* (botão direito num app) ou num atalho novo, escolha um ícone do catálogo do [dashboardicons.com](https://dashboardicons.com) colando o nome ou o link, ou envie o seu próprio arquivo (SVG, PNG, ICO ou JPG, até 1 MB). Uma prévia mostra o resultado antes de salvar. Os ícones ficam guardados no servidor e aparecem mesmo sem internet.
- A busca (`Ctrl+K`) acha apps, ações e arquivos.
- **Tela de bloqueio:** `Ctrl+Shift+L` (ou a busca, ou o botão direito na tela inicial) mostra um relógio grande sobre o papel de parede. Em *Configurações → Conta* você escolhe se ela pede a senha para voltar e depois de quanto tempo sem uso ela bloqueia sozinha.
- **Widgets:** na janela *Widgets* há modelos de armazenamento, sistema, rede, internet, bateria, consumo, gráficos, Relógio (digital ou de ponteiros, com fuso horário), Bloco de notas e Lista de tarefas (escritos direto na tela inicial e iguais em todos os aparelhos). Cadastrando a integração *Clima* com o nome da sua cidade, aparecem temperatura, ícone do tempo e previsão dos próximos dias.

## Arquivos

Gerenciador completo, com abas: lixeira, envio com retomada, zip, busca, editor de texto, extrair arquivos, caça a duplicados e mapa do espaço. Em *Arquivos → Discos → Mapear* você escolhe quais discos aparecem.

## Discos

A janela *Discos* mostra cada disco do servidor, inclusive os que não estão montados: identificação, partições, quais apps usam o disco, se ele está dormindo, a saúde (SMART) com cada número explicado, o teste do próprio disco, leitura e escrita ao vivo e dos últimos 30 dias, temperatura, teste de velocidade, espaço com previsão, uma linha do tempo (conectado, desconectado, montado, saúde mudou, esquentou) e os erros que o Linux anotou.

- Discos USB desmontados podem ser ejetados com segurança.
- Dá para montar uma partição numa pasta de `/mnt` (volta sozinha quando o servidor reinicia), desmontar, trocar o rótulo e formatar um disco inteiro (ext4, Btrfs, exFAT ou NTFS). Formatar pede a senha do BrunOS e a **Senha de configuração**, criada só no Terminal do servidor (`sudo brunos-host senha-config`) ou em *Configurações → Sistema*.
- O sino avisa quando um disco começa a falhar, quando um disco novo é conectado e quando um disco some.
- O SMART lê um disco de cada vez e descobre sozinho o jeito certo de falar com cada gaveta USB; uma gaveta que não deixa o SMART passar aparece com a etiqueta *sem SMART* e não é mais incomodada.

## Uso ao vivo e notificações

- A janela *Uso ao vivo* mostra a temperatura de cada disco e, se ligada, a previsão de quando ele enche, o histórico de CPU, memória, temperatura e rede (5 min, 24 h ou 7 dias) e a lista de processos do servidor.
- O widget *Internet* mostra a latência e quantas vezes a internet caiu (o sino avisa quando cai e quando volta).
- O **sino** reúne os avisos do servidor: queda e volta da energia, desligamento seguro, disco mapeado que se desmontou, disco quente, disco quase cheio (ou que deve encher em breve, se a previsão estiver ligada em *Configurações → Sistema*) e acesso ao BrunOS de um navegador ou rede que nunca entrou.

## Apps e Loja

A *Loja* (ícone da sacola no dock) mostra os apps de todas as lojas do Runtipi com categorias, busca, favoritos e a página de cada app, sem usar internet: ela lê o que o Runtipi já baixou. Parar, iniciar e desinstalar apps e adicionar, remover e atualizar lojas são feitos pelo próprio BrunOS, com o servidor preparado em *Configurações → Sistema → Energia*. Desinstalar apaga os dados do app e por isso pede a Senha de configuração (uma segunda senha, só para as ações sensíveis do servidor).

Com o script do servidor atualizado, **instalar acontece na própria Loja**: as perguntas do app aparecem num formulário (com listas prontas de discos e pastas e o recomendado já marcado), o Runtipi instala e o BrunOS acompanha até o app ligar. Depois, cada app instalado tem **Instalação** (mudar as respostas) e **Ajustes**: pastas do servidor dentro do app, limite de memória e de núcleos, placa de vídeo e variáveis. Os Ajustes ficam no arquivo de ajustes do Runtipi, valem depois das atualizações, respeitam o que já foi escrito à mão no Runtipi e pedem a Senha de configuração.

**Mais apps:** a Loja sugere as lojas **CasaOS** e **LinuxServer**, convertidas todo dia para o formato do Runtipi pelo [Garimpo](https://github.com/BrunnoFe/Garimpo) (um clique para adicionar). Cada loja aparece num cartão próprio, e os apps dela levam o selo da origem ("via CasaOS"). Receitas que pedem poderes sobre o servidor (privileged, socket do Docker, rede do servidor, pastas do sistema), de qualquer loja, aparecem com o selo **Requer privilégios**, ficam juntas na guia **Apps com Privilégios** e só instalam depois de você marcar "Entendo os riscos" e digitar a Senha de configuração.

**Detalhes de cada app:** clique com o botão direito num ícone → *Detalhes, logs e uso* para ver a disponibilidade e o consumo de CPU e memória nas últimas 24 h, os logs de cada container (inclusive ao vivo) e ligar, parar ou reiniciar o app (com *Permitir reiniciar apps* ligado no formulário do Runtipi). A cada minuto o BrunOS confere se cada app responde e avisa no sino quando algum cai (dá para desligar em *Configurações → Apps*).

## Atualizações

O botão ao lado do sino mostra quantas atualizações há (apps do Runtipi e pacotes do Linux). Um clique abre o painel e já confere; dali dá para atualizar cada app e o Linux sem abrir o terminal, acompanhando o andamento. A atualização roda no próprio servidor e continua mesmo se o BrunOS reiniciar no meio. A frequência da conferência automática fica em *Configurações → Sistema → Procurar atualizações*.

## Energia e consumo

- **Consumo:** a janela *Energia* mostra quanto o servidor gasta (W, kWh e R$ por dia e por mês, com projeção) pela tarifa que você informa. O consumo vem de uma tomada inteligente Tuya (integração *Tomada Tuya*, pela rede de casa, sem nuvem), da bateria durante as quedas de energia ou de uma estimativa que o BrunOS aprende.
- **Desligamento seguro:** em notebooks usados como servidor, *Configurações → Sistema → Energia* registra as quedas de energia e pode desligar o servidor com segurança quando a bateria chegar a um limite sem energia da tomada. Antes de desligar aparece uma contagem regressiva com botão para cancelar. O recurso começa em modo simulação e tem um assistente que mostra os comandos para preparar o servidor e testa a conexão.

## Terminal

Em *Configurações → Sistema → Terminal* (ou `Ctrl+K` → "terminal"), o BrunOS abre um terminal no servidor por SSH, com o seu usuário e senha (ou chave privada). Na primeira conexão ele mostra a impressão digital da chave do servidor para você conferir com `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`.

O terminal tem até 4 abas, botão de minimizar (as abas continuam rodando e o ícone aparece na dock), comandos salvos em botões e conexões salvas. Com a *Chave do Terminal* (no ⚙ da janela), ele entra direto no servidor sem pedir a senha do SSH: basta colar uma vez no servidor o comando que a janela mostra. Antes de fechar uma aba, o BrunOS confere se ainda tem algo rodando nela e pergunta; o tempo para fechar abas esquecidas fica em *Configurações → Sistema*.

Precisa do `openssh-server` ligado no servidor. Para desligar o recurso, use `BRUNOS_TERMINAL=false`.
