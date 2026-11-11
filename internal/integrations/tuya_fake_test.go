package integrations

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Validação cruzada com o tinytuya: TUYA_FAKE="127.0.0.1:7001,3.5" aponta para uma tomada falsa feita
// com as funções do próprio tinytuya (script fora do repositório). Sem a variável, o teste é pulado.
func TestTuyaAgainstTinytuya(t *testing.T) {
	spec := os.Getenv("TUYA_FAKE")
	if spec == "" {
		t.Skip("TUYA_FAKE não definido")
	}
	addr, ver, _ := strings.Cut(spec, ",")
	dps, err := TuyaStatus(context.Background(), addr, "dev123", "0123456789abcdef", ver)
	if err != nil {
		t.Fatal(err)
	}
	if dps["19"] != float64(169) || dps["20"] != float64(1253) || dps["1"] != true {
		t.Fatalf("dps inesperado: %v", dps)
	}
}
