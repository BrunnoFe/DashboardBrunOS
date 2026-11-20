# Roadmap

O que está **combinado e ainda não terminou**. O que já foi entregue está resumido em `historico.md`; o
que foi adiado ou sugerido sem data, em `ideias-futuras.md`; os porquês das escolhas, em `decisoes.md`.

Como se trabalha: uma versão por etapa, e o Bruno testa no servidor antes da próxima. Nada de privilégios
no container: o que precisa de root no host vai por SSH com chave restrita. Testar sempre também em tela de
celular.

## Estado atual (versão no arquivo `VERSION`)

- Entregue e testada no servidor: tudo até a 0.21.2 (inclusive 0.23.0, cujo retorno virou a 0.23.1).
- **Aguardando teste do Bruno**: 0.23.1 (guias no Arquivos; duas janelas independentes) e a 0.22.0 (editor
  do arquivo de Ajustes em Configurar → Avançado), que não tem confirmação explícita.

## Melhorias de 18/11/2026 (combinadas com o Bruno, três versões)

1. **0.24.0: Ícones** (lançada e testada pelo Bruno em 18/11): bug do link colado no campo (virava um nome
   impossível) e do `onerror` inline barrado pela CSP (atalho sem nem a inicial); campo Catálogo aceita
   nome ou link, com prévia; Enviar arquivo (svg/png/ico/jpg, até 1 MB); Ícones guardados no servidor
   (decisão 63).
2. **0.25.0: Energia e avisos** (lançada em 18/11; o teste achou que cancelar não funcionava, corrigido na
   0.25.1, testada pelo Bruno em 19/11; decisão 64): botão de energia no canto (depois do sino) com Bloquear, Sair, Reiniciar e
   Desligar; os mesmos botões ao lado de "Sair" nas Configurações; Desligar/Reiniciar pedem a senha do
   BrunOS e esperam 1 min com Cancelar no sino; sem chave SSH, levam à janela Energia; pedido novo
   `reiniciar` no script (troca assinada, sem colar nada); "Reiniciar agora" no painel de Atualizações
   quando o Ubuntu pedir. Avisos de nível Aviso/Importante expandem no painel (explicação, o que fazer,
   dados e botão de atalho); clicar no balão abre o sino no aviso.
3. **0.26.0: Formas de acesso** (lançada em 19/11, aguardando teste do Bruno; decisão 65): o Automático segue a Forma de acesso usada para abrir o BrunOS
   (`brunos.server.lan` → `https://app.server.lan`; `IP:porta` ou `server.lan:porta` → mesmo nome com a
   porta do app), com troca para a outra se a escolhida não funcionar; o vigia testa a porta (já faz) e, a
   cada ~10 min, se o domínio responde pelo Traefik; as duas formas com bolinha verde/cinza no botão
   direito ("Abrir por…"), em Personalizar e no Monitor de apps.

## Revisão de segurança de 19/11/2026 (0.27.0, lançada em 19/11, aguardando teste do Bruno)

Os detalhes ficam no relatório privado. O que entrou:
- Go 1.27 e `x/crypto` atualizado; `govulncheck` no CI.
- Limite de tentativas de senha mais firme (inclusive na troca de senha) e prazos contra conexões lentas.
- Lixeira, logos e configurações das lojas não seguem atalhos; integrações com credencial presa ao
  endereço e destinos internos proibidos.
- Loja: selo "Requer privilégios" mais rigoroso; adicionar loja passa a pedir a Senha de configuração
  (o Bruno tinha dito em 17/11 que não precisava; entrou no lote aprovado em 19/11) e instalar sem senha
  só de loja confiável (script do servidor 18).
- Regras do proxy do Docker mais estreitas; CI com actions fixadas e assinatura num job separado.
- Interface: escapes, sandbox na Janela de app, demonstração só onde não há servidor e dados genéricos.

## Revisão de segurança, lotes 3 e 4 (prontos em 20/11/2026, ainda não lançados)

Escolhas do Bruno em 19/11: ocultos da Home só leitura; segredos somem do Arquivos (pasta de dados do
BrunOS, app.env dos apps, .env do Runtipi, chaves privadas de .ssh); código da aba + Terminal (a Chave do
Terminal pede a senha ao abrir o Terminal, não no login); senha de novo nas ações destrutivas (5 min); IP
só do Traefik; Abas do Terminal por Sessão; nomes de endereço aceitos (Configurações → Sistema → Endereços
aceitos). Depois de atualizar, todo mundo precisa entrar de novo uma vez.

Também em aberto: o proxy do Docker por arquivo de socket em vez da rede, e a chave do script aceitar só a
rede do Docker (os dois precisam ser testados no servidor real). Os processos com a linha de comando inteira
(V13 do relatório) não entraram em nenhum lote.

## Em aberto

- Tirar a conversão das preferências antigas de widgets (`widgets2` v1/v2 → v3, em `app.js`) quando o Bruno
  quiser; a 0.13.0 já foi testada.
- Senha também para parar app e para remover lojas: o Bruno disse que não precisa por enquanto
  (17/11/2026). Adicionar loja passou a pedir na revisão de segurança de 19/11.
- Itens do `ideias-futuras.md` que o Bruno pedir passam para cá, com versão.

## Como lançar uma versão

Está no `CLAUDE.md` (seção "Como lançar uma versão").
