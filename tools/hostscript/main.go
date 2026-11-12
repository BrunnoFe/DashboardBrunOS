// Command hostscript imprime o script do servidor (hostcmd.HostScript) exatamente como o BrunOS o
// manda no "atualizar-script". O CI assina essa saída (ssh-keygen -Y sign -n brunos-host) e grava a
// assinatura em internal/hostcmd/assinatura.txt antes de gerar a imagem. Não entra no binário.
package main

import (
	"os"

	"brunos/internal/hostcmd"
)

func main() { _, _ = os.Stdout.WriteString(hostcmd.HostScript) }
