package files

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// ── montagem de MKV de mentira ───────────────────────────────────

func ebmlID(id uint32) []byte {
	switch {
	case id > 0xFFFFFF:
		return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	case id > 0xFFFF:
		return []byte{byte(id >> 16), byte(id >> 8), byte(id)}
	case id > 0xFF:
		return []byte{byte(id >> 8), byte(id)}
	}
	return []byte{byte(id)}
}

// el monta um elemento com tamanho de 8 bytes (sempre cabe, e testa números EBML longos).
func el(id uint32, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	sz := make([]byte, 8)
	binary.BigEndian.PutUint64(sz, uint64(len(body)))
	sz[0] = 0x01
	return append(append(ebmlID(id), sz...), body...)
}

func u8(v byte) []byte { return []byte{v} }

func mkvTrack(typ byte, codec, lang string, def bool, ch byte) []byte {
	parts := [][]byte{el(mkvType, u8(typ)), el(mkvCodecID, []byte(codec))}
	if lang != "" {
		parts = append(parts, el(mkvLang, []byte(lang)))
	}
	if !def {
		parts = append(parts, el(mkvDefault, u8(0)))
	}
	if ch > 0 {
		parts = append(parts, el(mkvAudio, el(mkvChannels, u8(ch))))
	}
	return el(mkvEntry, parts...)
}

func mkvFile(doc string, segment ...[]byte) []byte {
	head := el(ebmlHeader, el(ebmlDocType, []byte(doc)))
	return append(head, el(mkvSegment, segment...)...)
}

func TestProbeMKV(t *testing.T) {
	tracks := el(mkvTracks,
		mkvTrack(1, "V_MPEG4/ISO/AVC", "", true, 0),
		mkvTrack(2, "A_DTS", "por", true, 6),
		mkvTrack(2, "A_AAC", "eng", false, 2),
		mkvTrack(17, "S_TEXT/UTF8", "por", false, 0),
	)
	data := mkvFile("matroska", el(0x1549A966, []byte("info")), tracks, el(mkvCluster, make([]byte, 64)))
	info, err := ProbeMedia(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Container != "matroska" || len(info.Tracks) != 4 {
		t.Fatalf("info = %+v", info)
	}
	v, a1, a2, s := info.Tracks[0], info.Tracks[1], info.Tracks[2], info.Tracks[3]
	if v.Kind != "video" || v.Codec != "h264" || !v.Default {
		t.Errorf("vídeo = %+v", v)
	}
	if a1.Kind != "audio" || a1.Codec != "dts" || a1.Lang != "por" || a1.Channels != 6 || !a1.Default {
		t.Errorf("áudio 1 = %+v", a1)
	}
	if a2.Codec != "aac" || a2.Default || a2.Channels != 2 {
		t.Errorf("áudio 2 = %+v", a2)
	}
	if s.Kind != "sub" || s.Codec != "srt" {
		t.Errorf("legenda = %+v", s)
	}
}

// Tracks depois dos blocos de imagem: só o SeekHead diz onde está.
func TestProbeMKVSeekHead(t *testing.T) {
	tracks := el(mkvTracks, mkvTrack(1, "V_VP9", "", true, 0), mkvTrack(2, "A_OPUS", "", true, 2))
	cluster := el(mkvCluster, make([]byte, 1000))
	// SeekHead com tamanho fixo: a posição do Tracks = SeekHead + Cluster
	seek := func(pos uint64) []byte {
		p := make([]byte, 8)
		binary.BigEndian.PutUint64(p, pos)
		return el(mkvSeekHead, el(mkvSeek, el(mkvSeekID, ebmlID(mkvTracks)), el(mkvSeekPos, p)))
	}
	sh := seek(0)
	sh = seek(uint64(len(sh) + len(cluster)))
	data := mkvFile("webm", sh, cluster, tracks)
	info, err := ProbeMedia(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Container != "webm" || len(info.Tracks) != 2 || info.Tracks[0].Codec != "vp9" || info.Tracks[1].Codec != "opus" {
		t.Fatalf("info = %+v", info)
	}
	if info.Tracks[1].Lang != "eng" { // padrão do Matroska quando não diz
		t.Errorf("idioma padrão = %q", info.Tracks[1].Lang)
	}
}

func TestProbeMKVNoTracks(t *testing.T) {
	data := mkvFile("matroska", el(mkvCluster, make([]byte, 32)))
	if _, err := ProbeMedia(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("sem Tracks deveria dar erro")
	}
}

// ── montagem de MP4 de mentira ───────────────────────────────────

func box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(b, uint32(8+len(body)))
	copy(b[4:], typ)
	return append(b, body...)
}

func mp4Trak(handler, format string, enabled bool, lang string, ch uint16) []byte {
	tkhd := make([]byte, 84)
	if enabled {
		tkhd[3] = 1
	}
	mdhd := make([]byte, 24) // versão 0: vf(4) + datas(8) + escala(4) + duração(4) + idioma(2) + 2
	if lang != "" {
		v := uint16(lang[0]-0x60)<<10 | uint16(lang[1]-0x60)<<5 | uint16(lang[2]-0x60)
		binary.BigEndian.PutUint16(mdhd[20:], v)
	}
	hdlr := make([]byte, 24)
	copy(hdlr[8:], handler)
	entry := make([]byte, 28) // reservado(6) + ref(2) + reservado(8) + canais(2) + ...
	binary.BigEndian.PutUint16(entry[16:], ch)
	stsd := append(make([]byte, 8), box(format, entry)...)
	stsd[7] = 1
	return box("trak", box("tkhd", tkhd), box("mdia", box("mdhd", mdhd), box("hdlr", hdlr),
		box("minf", box("stbl", box("stsd", stsd)))))
}

func TestProbeMP4MoovAtEnd(t *testing.T) {
	moov := box("moov", box("mvhd", make([]byte, 100)),
		mp4Trak("vide", "hvc1", true, "", 0),
		mp4Trak("soun", "ac-3", true, "por", 6),
		mp4Trak("soun", "mp4a", true, "eng", 2),
	)
	data := append(box("ftyp", []byte("isom0000")), box("mdat", make([]byte, 5000))...)
	data = append(data, moov...)
	info, err := ProbeMedia(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Container != "mp4" || len(info.Tracks) != 3 {
		t.Fatalf("info = %+v", info)
	}
	v, a1, a2 := info.Tracks[0], info.Tracks[1], info.Tracks[2]
	if v.Kind != "video" || v.Codec != "hevc" || !v.Default {
		t.Errorf("vídeo = %+v", v)
	}
	if a1.Codec != "ac3" || a1.Lang != "por" || a1.Channels != 6 || !a1.Default {
		t.Errorf("áudio 1 = %+v", a1)
	}
	// o player usa só a primeira faixa ligada de cada tipo
	if a2.Codec != "aac" || a2.Default {
		t.Errorf("áudio 2 = %+v", a2)
	}
}

func TestProbeUnknown(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("RIFF....AVI "), make([]byte, 64)} {
		if _, err := ProbeMedia(bytes.NewReader(data), int64(len(data))); err == nil {
			t.Errorf("%q deveria dar erro", data)
		}
	}
	// caixa com tamanho maior que o arquivo não pode travar nem estourar
	bad := box("ftyp", []byte("isom"))
	binary.BigEndian.PutUint32(bad, 1<<30)
	if _, err := ProbeMedia(bytes.NewReader(bad), int64(len(bad))); err == nil {
		t.Error("caixa quebrada deveria dar erro")
	}
}

func TestServiceMedia(t *testing.T) {
	dir := t.TempDir()
	data := mkvFile("matroska", el(mkvTracks, mkvTrack(1, "V_MPEG4/ISO/AVC", "", true, 0), mkvTrack(2, "A_EAC3", "", true, 6)))
	if err := os.WriteFile(filepath.Join(dir, "filme.mkv"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	s, errs := New(func() []RootConfig { return []RootConfig{{ID: "m", Name: "Mídia", Path: dir}} })
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	info, err := s.Media("m", "filme.mkv")
	if err != nil || len(info.Tracks) != 2 || info.Tracks[1].Codec != "eac3" {
		t.Fatalf("info = %+v, err = %v", info, err)
	}
	if _, err := s.Media("m", "nada.mkv"); err == nil {
		t.Error("arquivo que não existe deveria dar erro")
	}
}
