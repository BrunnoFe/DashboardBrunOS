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
   0.25.1, aguardando teste; decisão 64): botão de energia no canto (depois do sino) com Bloquear, Sair, Reiniciar e
   Desligar; os mesmos botões ao lado de "Sair" nas Configurações; Desligar/Reiniciar pedem a senha do
   BrunOS e esperam 1 min com Cancelar no sino; sem chave SSH, levam à janela Energia; pedido novo
   `reiniciar` no script (troca assinada, sem colar nada); "Reiniciar agora" no painel de Atualizações
   quando o Ubuntu pedir. Avisos de nível Aviso/Importante expandem no painel (explicação, o que fazer,
   dados e botão de atalho); clicar no balão abre o sino no aviso.
3. **0.26.0: Formas de acesso**: o Automático segue a Forma de acesso usada para abrir o BrunOS
   (`brunos.server.lan` → `https://app.server.lan`; `IP:porta` ou `server.lan:porta` → mesmo nome com a
   porta do app), com troca para a outra se a escolhida não funcionar; o vigia testa a porta (já faz) e, a
   cada ~10 min, se o domínio responde pelo Traefik; as duas formas com bolinha verde/cinza no botão
   direito ("Abrir por…"), em Personalizar e no Monitor de apps.

## Em aberto

- Tirar a conversão das preferências antigas de widgets (`widgets2` v1/v2 → v3, em `app.js`) quando o Bruno
  quiser; a 0.13.0 já foi testada.
- Senha também para parar app e para adicionar ou remover lojas: o Bruno disse que não precisa por
  enquanto (17/11/2026).
- Itens do `ideias-futuras.md` que o Bruno pedir passam para cá, com versão.

## Como lançar uma versão

Está no `CLAUDE.md` (seção "Como lançar uma versão").
