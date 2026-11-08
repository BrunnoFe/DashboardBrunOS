package dockerc

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func frame(kind byte, s string) []byte {
	h := make([]byte, 8)
	h[0] = kind
	binary.BigEndian.PutUint32(h[4:], uint32(len(s)))
	return append(h, s...)
}

func TestDemux(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(1, "linha 1\n"))
	in.Write(frame(2, "erro\n"))
	in.Write(frame(1, "linha 2\n"))
	var out bytes.Buffer
	if err := Demux(&in, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "linha 1\nerro\nlinha 2\n" {
		t.Fatalf("saiu %q", out.String())
	}
	// texto puro (container com TTY) passa direto
	out.Reset()
	if err := Demux(bytes.NewBufferString("olá, mundo sem cabeçalho\n"), &out); err != nil || out.String() != "olá, mundo sem cabeçalho\n" {
		t.Fatalf("texto puro: %q %v", out.String(), err)
	}
}

func TestIgnore304(t *testing.T) {
	if ignore304(errString("docker POST /containers/x/start: 304 ")) != nil {
		t.Fatal("304 não é erro")
	}
	if ignore304(errString("docker POST /x: 500 boom")) == nil {
		t.Fatal("500 é erro")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
