# Visão geral do BrunOS

## Origem e objetivo

O Bruno usava o **Homarr** com um CSS customizado para imitar o **umbrelOS** no servidor caseiro dele
(Ubuntu Server + Runtipi). Ele pediu um painel próprio que:

- reproduzisse a interface do umbrelOS: vidro, ícones, sombras, fonte Inter, papel de parede, dock com
  os apps principais, muita personalização ("algo fofo, legal, bonito");
- desse acesso aos apps do Runtipi, gerenciamento de arquivos, configurações gerais e uso de hardware;
- fosse **leve** ("é fundamental que essa interface seja relativamente leve");
- tivesse **usuário e senha próprios** criados na primeira execução, com credenciais bem guardadas;
- lesse **todas as pastas e discos possíveis, mas com segurança** (`/home` + discos montados em `/mnt`).

A inspiração visual veio do código do Umbrel (`getumbrel/umbrel`, `packages/ui`) e de um vídeo que o
Bruno mandou mostrando a interface. Valores de sombras, curvas de animação e a ampliação do dock foram
adaptados de lá; os papéis de parede são os do Umbrel (licença PolyForm Noncommercial, uso pessoal).

## Princípios

1. Leveza: binário Go estático sobre `scratch`, ~4 MB de imagem, ~10 MiB de RAM.
2. Segurança sem privilégios: container restrito, proxy do Docker com lista de rotas, nada de root.
3. Fidelidade visual ao umbrelOS, com tema derivado do papel de parede.
4. Tudo em português do Brasil.
5. Configurações salvas no servidor (iguais em qualquer dispositivo).
6. Testes automáticos com Docker de verdade antes de publicar qualquer imagem.

## Funcionalidades (versão 0.3.0)

**Tela inicial**
- Saudação ("Boa tarde, Bruno."), logo, papel de parede (12 embutidos + envio próprio, arrastar e soltar).
- Cor de destaque automática (histograma de matiz do papel de parede), "vibrante" ou manual.
- Widgets (até 6): Arquivos (atalhos), Armazenamento, Sistema (CPU/memória/temperatura), Rede,
  Bateria. Clicar abre "Uso ao vivo" ou a janela da bateria.
- Grade de apps do Runtipi com logo, nome e bolinha de status (verde rodando, cinza parado, amarelo
  parcial). Arrastar para reordenar, menu de contexto (abrir, abrir pelo IP:porta, personalizar
  nome/ícone/URL, ocultar, reiniciar se permitido). Atalhos personalizados para sites fora do Runtipi.
- Dock: Arquivos, Runtipi, Loja de apps (abre `/app-store` do Runtipi), Uso ao vivo, Configurações,
  divisor e apps fixados. Ampliação igual à do Umbrel (50 → 80 px num raio de 150 px).
- Paleta de comandos `Ctrl+K` (apps, ações, busca nos arquivos, terminal).

**Arquivos**
- Barra lateral: Locais (Home), **Discos** (seção recolhível; "Mapear" escolhe quais pastas de `/mnt`
  aparecem, salvo em `/data/disks.json`), Favoritos, Lixeira.
- Lista/grade, ordenação, seleção múltipla, menus de contexto, copiar/recortar/colar (inclusive entre
  discos), renomear, nova pasta, envio com fila e progresso, arrastar e soltar, baixar (zip para pastas),
  pré-visualização (imagem, vídeo, áudio, PDF, texto), busca, arquivos ocultos, lixeira por disco.

**Monitoramento**
- CPU, memória (sem cache), temperatura (coretemp/k10temp/dell_smm...), ventoinha, discos (por
  dispositivo real), rede (interfaces físicas), uptime, carga. Atualização por SSE a cada 2 s.
- Uso por app (CPU e memória somados por projeto do Docker Compose).
- Bateria: carga com cor por nível (verde ≥ 90%, branco 30–89%, amarelo 20–29%, vermelho < 20%),
  tomada, tempo restante ou até encher, autonomia estimada na tomada, energia (Wh), consumo (W), saúde,
  ciclos, histórico de 24 h.

**Configurações**
- Aparência (papel de parede, cor, vidro/desfoque, tamanho dos ícones, widgets, reduzir animações).
- Apps (endereço por domínio local ou IP:porta, servidor, endereço do Runtipi, apps no dock em lista
  suspensa com caixas de seleção, atalhos, status, abrir em nova aba, ocultos, ordem).
- Conta (nome de exibição, trocar senha, sair).
- Sistema (informações do dispositivo, Runtipi, Docker, **Terminal**, pastas/discos, exportar
  preferências, restaurar aparência).

**Terminal**
- Janela com xterm.js que conecta por SSH ao servidor com usuário e senha ou chave privada.
- Confirmação da impressão digital da chave do servidor na primeira conexão (TOFU).
