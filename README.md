<div align="center">

<img src="web/static/assets/favicon.svg" alt="" width="84" height="84">

# BrunOS

**O painel bonito e leve para o seu servidor caseiro.**<br>
Seus apps, arquivos, discos e o uso do servidor num só lugar, no estilo do UmbrelOS.

[![Licença MIT](https://img.shields.io/badge/licen%C3%A7a-MIT-3b82f6?style=flat-square)](LICENSE)
[![Imagem Docker](https://img.shields.io/badge/docker-ghcr.io%2Fbrunnofe%2Fbrunos-2496ed?style=flat-square&logo=docker&logoColor=white)](https://github.com/BrunnoFe/DashboardBrunOS/pkgs/container/brunos)
[![Para o Runtipi](https://img.shields.io/badge/feito%20para-Runtipi-6d28d9?style=flat-square)](https://runtipi.io)
![Versão](https://img.shields.io/badge/dynamic/regex?url=https%3A%2F%2Fraw.githubusercontent.com%2FBrunnoFe%2FDashboardBrunOS%2Fmain%2FVERSION&search=%28%5Cd%2B%5C.%5Cd%2B%5C.%5Cd%2B%29&replace=%241&label=vers%C3%A3o&style=flat-square&color=0ea5e9)
![Em português](https://img.shields.io/badge/interface-pt--BR-16a34a?style=flat-square)

**🇧🇷 Português** · [🇺🇸 English](README.en.md)

<br>

### 👉 [Experimente agora, direto no navegador](https://brunnofe.github.io/DashboardBrunOS/) 👈
<sub>Modo demonstração: dados de mentira, qualquer usuário e senha entram.</sub>

<br>

[Conhecer](#conhecer) ·
[Instalar](#instalar) ·
[Perguntas](#perguntas) ·
[Documentação](#documentacao)

<br>

<a href="https://brunnofe.github.io/DashboardBrunOS/"><img src="docs/img/demo.gif" alt="BrunOS em ação: dock que cresce, arquivos, discos, loja e busca" width="760"></a>

</div>

<br>

## ✨ Por que o BrunOS?

Quem tem um servidor em casa costuma acabar com dezenas de apps, cada um numa porta diferente. O BrunOS junta tudo numa tela só, bonita e rápida, que você abre de qualquer aparelho.

|   |   |
|---|---|
| 🪶 **Leve de verdade** | A imagem tem cerca de 4 MB e o painel usa por volta de 10 MB de memória. Cabe até em servidor velho. |
| 🎨 **Bonito e do seu jeito** | Vidro, papel de parede, dock que cresce quando o mouse passa, widgets e cores que combinam com a imagem. |
| 🔌 **Pronto para o Runtipi** | Ele encontra os apps que você já instalou, com nome, ícone e endereço. Nada para cadastrar. |
| 🔒 **Seguro por padrão** | Roda sem privilégios, com usuário e senha seus. As ações que mexem no servidor pedem uma senha extra. |
| 🇧🇷 **Em português** | Interface e mensagens de erro em português do Brasil. |

<br>

<a id="conhecer"></a>

## 🖼️ O que ele faz

### Tudo à mão na tela inicial

Seus apps aparecem como ícones, com uma bolinha mostrando se estão no ar. Arraste um sobre o outro para criar pastas. Os **widgets** mostram o que importa: espaço nos discos, CPU, memória, temperatura, internet, clima, relógio, notas e lista de tarefas. Aperte <kbd>Ctrl</kbd>+<kbd>K</kbd> para buscar qualquer app, ação ou arquivo.

<div align="center">
<img src="docs/img/inicio.jpg" alt="Tela inicial do BrunOS: papel de parede, widgets de vidro, apps e dock" width="780">
</div>

<br>

<div align="center">
<img src="docs/img/busca.jpg" alt="Busca rápida com Ctrl+K" width="48%">
<img src="docs/img/widgets.jpg" alt="Janela de widgets com os modelos prontos" width="48%">
</div>

<br>

### Arquivos do servidor no navegador

Navegue, envie, baixe, edite textos, compacte, apague (com lixeira) e encontre arquivos duplicados. Funciona com várias abas, como num computador de verdade.

<div align="center">
<img src="docs/img/arquivos.jpg" alt="Gerenciador de arquivos" width="780">
</div>

<br>

### Saúde dos discos, sem susto

Veja cada disco com temperatura, espaço, desempenho e o estado de saúde explicado em português simples. O sino avisa **antes** de um disco dar problema, quando um disco novo é conectado ou quando um some.

<div align="center">
<img src="docs/img/discos.jpg" alt="Janela de discos com saúde, partições e temperatura" width="780">
</div>

<br>

### Uso do servidor ao vivo

Gráficos de CPU, memória, temperatura e rede, e quanto cada app está consumindo. Avisa no sino quando um app cai ou quando a internet some.

<div align="center">
<img src="docs/img/uso-ao-vivo.jpg" alt="Uso ao vivo do servidor" width="560">
</div>

<br>

### Loja para instalar e atualizar apps

Navegue pelos apps do Runtipi (e de outras lojas, como CasaOS e LinuxServer), instale com um formulário e atualize tudo sem abrir o terminal.

<div align="center">
<img src="docs/img/loja.jpg" alt="Loja de apps" width="780">
</div>

<br>

### Também no celular

A interface se adapta a telas pequenas: o mesmo painel, com a dock embaixo, na palma da mão.

<div align="center">
<img src="docs/img/celular-inicio.jpg" alt="Tela inicial no celular" width="240">
&nbsp;&nbsp;
<img src="docs/img/celular-discos.jpg" alt="Janela de discos no celular" width="240">
</div>

<br>

### E mais

<details>
<summary><b>🔔 Avisos no sino</b></summary>
<br>

Queda de energia, disco quente ou cheio, app que parou, internet que caiu e acesso de um aparelho novo ao painel.
</details>

<details>
<summary><b>⚡ Energia e desligamento seguro</b></summary>
<br>

Acompanhe quanto o servidor gasta (em watts e em reais, com projeção do mês). Em notebooks usados como servidor, o BrunOS pode desligar tudo com segurança quando a bateria estiver acabando.
</details>

<details>
<summary><b>💻 Terminal no navegador</b></summary>
<br>

Um terminal do servidor com abas, direto no painel, sem precisar de outro programa.
</details>

<details>
<summary><b>🔒 Tela de bloqueio</b></summary>
<br>

Um relógio grande sobre o papel de parede, com senha para voltar e bloqueio automático depois de um tempo parado.
</details>

<details>
<summary><b>⬆️ Atualizações pela interface</b></summary>
<br>

Um botão mostra quantas atualizações existem (apps e Linux). Dá para atualizar com um clique e acompanhar o andamento.
</details>

Todos os detalhes, um por um, estão em **[docs/recursos.md](docs/recursos.md)**.

<br>

<a id="instalar"></a>

## 🚀 Instalar em 3 passos

Você precisa de um servidor com o [Runtipi](https://runtipi.io) funcionando.

1. No Runtipi, adicione a loja de apps: `https://github.com/BrunnoFe/RuntipiAppStore`
2. Instale o **BrunOS** e confira no formulário a pasta do Runtipi.
3. Abra o app, crie seu usuário e pronto.

> O primeiro usuário só pode ser criado de dentro da rede de casa.

Quer testar antes, sem instalar nada? Abra a **[demonstração no navegador](https://brunnofe.github.io/DashboardBrunOS/)**, com dados de mentira.

Passo a passo completo, pastas e opções: **[docs/instalacao.md](docs/instalacao.md)**.

<br>

<a id="perguntas"></a>

## ❓ Perguntas rápidas

<details>
<summary><b>Preciso saber programar?</b></summary>
<br>

Não. Instalar e usar é tudo pela interface.
</details>

<details>
<summary><b>Funciona sem o Runtipi?</b></summary>
<br>

Ele foi feito para o Runtipi e é lá que mostra tudo o que sabe fazer. Em outros cenários, o uso fica limitado.
</details>

<details>
<summary><b>Meus dados ficam seguros?</b></summary>
<br>

Tudo fica no seu servidor, nada vai para a nuvem. O único contato com a internet é a previsão do tempo, e só se você criar um widget de clima. O painel roda sem privilégios, e ações delicadas (formatar disco, desinstalar app) pedem uma segunda senha.
</details>

<details>
<summary><b>Qual a diferença para o Homarr, Dashy e outros?</b></summary>
<br>

O BrunOS não é só um painel de atalhos: ele também gerencia arquivos, discos, apps e energia, continuando muito leve e com a cara do umbrelOS.
</details>

<details>
<summary><b>Posso usar no celular?</b></summary>
<br>

Sim, a interface se adapta a telas pequenas.
</details>

<br>

<a id="documentacao"></a>

## 📚 Documentação

| Para quê | Onde |
|---|---|
| Instalar, pastas e variáveis | [docs/instalacao.md](docs/instalacao.md) |
| Cada recurso em detalhe | [docs/recursos.md](docs/recursos.md) |
| Mexer no código, rodar testes, API | [docs/desenvolvimento.md](docs/desenvolvimento.md) |
| Como o BrunOS funciona por dentro | [docs/projeto/arquitetura.md](docs/projeto/arquitetura.md) |
| Por que cada decisão foi tomada | [docs/projeto/decisoes.md](docs/projeto/decisoes.md) |
| Histórico de versões | [docs/projeto/historico.md](docs/projeto/historico.md) |
| O que vem por aí | [docs/projeto/roadmap.md](docs/projeto/roadmap.md) |

<br>

## 💙 Créditos

Criado por **Bruno Barbosa**, com inspiração visual no [umbrelOS](https://github.com/getumbrel/umbrel). Os ícones vêm do [dashboard-icons](https://github.com/homarr-labs/dashboard-icons), a fonte é a Inter, o editor de texto é o CodeMirror e o terminal usa o xterm.js. A lista completa de licenças está em [docs/desenvolvimento.md](docs/desenvolvimento.md#créditos-e-licenças-de-terceiros).

Código sob licença [MIT](LICENSE). Os papéis de parede são do umbrelOS e valem só para uso pessoal.

<div align="center">
<br>
Gostou? Deixe uma ⭐ no repositório, ajuda muito!
</div>
