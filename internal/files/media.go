package files

// Formatos de dentro de um vídeo ou áudio (decisão 73): o navegador toca a imagem de um .mkv mas fica
// mudo quando o áudio é AC3/DTS/TrueHD, sem dar erro. O servidor lê só o cabeçalho (Matroska/WebM ou
// MP4) e devolve as faixas; quem decide se toca é o próprio navegador (canPlayType), porque isso muda
// de navegador para navegador.
//
// Recebe um io.ReaderAt para servir também a arquivos que não são locais. Lê no máximo alguns MB,
// pulando os blocos grandes (imagem e som) sem lê-los.

import (
	"encoding/binary"
	"errors"
	"io"
	"strings"
)

// Track é uma faixa de um arquivo de mídia.
type Track struct {
	Kind     string `json:"kind"`  // video, audio, sub
	Codec    string `json:"codec"` // h264, hevc, aac, ac3, dts... (ou o código original, se desconhecido)
	Name     string `json:"name,omitempty"`
	Lang     string `json:"lang,omitempty"`
	Default  bool   `json:"default"` // a faixa que o player escolhe sozinho
	Channels int    `json:"channels,omitempty"`
}

// MediaInfo é o que se sabe de um arquivo de mídia.
type MediaInfo struct {
	Container string  `json:"container"` // matroska, webm, mp4
	Tracks    []Track `json:"tracks"`
}

var errMedia = errors.New("formato de mídia não reconhecido")

const (
	maxMediaElem = 4 << 20 // maior bloco de cabeçalho lido inteiro (Tracks do MKV, stsd do MP4)
	maxMediaHops = 4096    // quantos blocos pular, no máximo, procurando o cabeçalho
)

// ProbeMedia lê o cabeçalho de um MKV/WebM ou MP4/MOV/M4A.
func ProbeMedia(r io.ReaderAt, size int64) (MediaInfo, error) {
	var head [12]byte
	n, _ := r.ReadAt(head[:], 0)
	switch {
	case n >= 4 && binary.BigEndian.Uint32(head[:4]) == 0x1A45DFA3:
		return probeMKV(r, size)
	case n >= 8 && mp4Top[string(head[4:8])]:
		return probeMP4(r, size)
	}
	return MediaInfo{}, errMedia
}

// Media lê os formatos de um arquivo de mídia de uma raiz.
func (s *Service) Media(rootID, rel string) (MediaInfo, error) {
	f, st, err := s.Open(rootID, rel)
	if err != nil {
		return MediaInfo{}, err
	}
	defer f.Close()
	if st.IsDir() {
		return MediaInfo{}, ErrBadPath
	}
	info, err := ProbeMedia(f, st.Size())
	if err != nil {
		return MediaInfo{}, errMedia
	}
	return info, nil
}

// ── Matroska / WebM (EBML) ────────────────────────────────────────

const (
	ebmlHeader  = 0x1A45DFA3
	ebmlDocType = 0x4282
	mkvSegment  = 0x18538067
	mkvSeekHead = 0x114D9B74
	mkvSeek     = 0x4DBB
	mkvSeekID   = 0x53AB
	mkvSeekPos  = 0x53AC
	mkvTracks   = 0x1654AE6B
	mkvCluster  = 0x1F43B675
	mkvEntry    = 0xAE
	mkvType     = 0x83
	mkvCodecID  = 0x86
	mkvName     = 0x536E
	mkvLang     = 0x22B59C
	mkvLangBCP  = 0x22B59D
	mkvDefault  = 0x88
	mkvAudio    = 0xE1
	mkvChannels = 0x9F
	unknownSize = -1
)

// ebmlHead lê o cabeçalho de um elemento em off: id (com os bits de marca), tamanho do conteúdo e
// quantos bytes o cabeçalho ocupa.
func ebmlHead(r io.ReaderAt, off int64) (id uint32, size int64, hlen int, err error) {
	var b [12]byte
	n, _ := r.ReadAt(b[:], off)
	if n < 2 {
		return 0, 0, 0, io.ErrUnexpectedEOF
	}
	return ebmlHeadBytes(b[:n])
}

func ebmlHeadBytes(b []byte) (id uint32, size int64, hlen int, err error) {
	il := vintLen(b[0])
	if il == 0 || il > 4 || len(b) < il+1 {
		return 0, 0, 0, errMedia
	}
	for _, c := range b[:il] {
		id = id<<8 | uint32(c)
	}
	sl := vintLen(b[il])
	if sl == 0 || len(b) < il+sl {
		return 0, 0, 0, errMedia
	}
	v := int64(b[il] & (0xFF >> sl))
	all := v == int64(0xFF>>sl)
	for _, c := range b[il+1 : il+sl] {
		v = v<<8 | int64(c)
		all = all && c == 0xFF
	}
	if all {
		v = unknownSize
	}
	return id, v, il + sl, nil
}

// vintLen: quantos bytes tem um número EBML, pelo primeiro byte (0 = inválido).
func vintLen(b byte) int {
	for i := 0; i < 8; i++ {
		if b&(0x80>>i) != 0 {
			return i + 1
		}
	}
	return 0
}

func probeMKV(r io.ReaderAt, size int64) (MediaInfo, error) {
	info := MediaInfo{Container: "matroska"}
	id, sz, hl, err := ebmlHead(r, 0)
	if err != nil || id != ebmlHeader || sz < 0 || sz > 4096 {
		return info, errMedia
	}
	hdr := make([]byte, sz)
	if _, err := r.ReadAt(hdr, int64(hl)); err != nil {
		return info, errMedia
	}
	eachChild(hdr, func(id uint32, data []byte) {
		if id == ebmlDocType && string(data) == "webm" {
			info.Container = "webm"
		}
	})
	off := int64(hl) + sz
	id, sz, hl, err = ebmlHead(r, off)
	if err != nil || id != mkvSegment {
		return info, errMedia
	}
	seg := off + int64(hl) // as posições do SeekHead contam a partir daqui
	end := size
	if sz != unknownSize && seg+sz < end {
		end = seg + sz
	}
	tracksAt := int64(-1)
	off = seg
	for hop := 0; off < end && hop < maxMediaHops; hop++ {
		id, sz, hl, err := ebmlHead(r, off)
		if err != nil {
			break
		}
		body := off + int64(hl)
		switch id {
		case mkvTracks:
			return readMKVTracks(r, body, sz, info)
		case mkvSeekHead:
			if sz > 0 && sz <= maxMediaElem {
				buf := make([]byte, sz)
				if _, err := r.ReadAt(buf, body); err == nil {
					if p := seekTracks(buf); p >= 0 {
						tracksAt = seg + p
					}
				}
			}
		case mkvCluster:
			// os blocos de imagem e som começaram: o Tracks só pode estar onde o SeekHead disse
			if tracksAt >= 0 {
				id, sz, hl, err := ebmlHead(r, tracksAt)
				if err == nil && id == mkvTracks {
					return readMKVTracks(r, tracksAt+int64(hl), sz, info)
				}
			}
			return info, errMedia
		}
		if sz == unknownSize {
			break
		}
		off = body + sz
	}
	if tracksAt >= 0 {
		if id, sz, hl, err := ebmlHead(r, tracksAt); err == nil && id == mkvTracks {
			return readMKVTracks(r, tracksAt+int64(hl), sz, info)
		}
	}
	return info, errMedia
}

// seekTracks acha no SeekHead a posição do Tracks (relativa ao começo do Segment), ou -1.
func seekTracks(buf []byte) int64 {
	pos := int64(-1)
	eachChild(buf, func(id uint32, data []byte) {
		if id != mkvSeek {
			return
		}
		var sid uint32
		var p int64 = -1
		eachChild(data, func(id uint32, d []byte) {
			switch id {
			case mkvSeekID:
				for _, c := range d {
					sid = sid<<8 | uint32(c)
				}
			case mkvSeekPos:
				p = int64(beUint(d))
			}
		})
		if sid == mkvTracks && p >= 0 {
			pos = p
		}
	})
	return pos
}

func readMKVTracks(r io.ReaderAt, at, sz int64, info MediaInfo) (MediaInfo, error) {
	if sz <= 0 || sz > maxMediaElem {
		return info, errMedia
	}
	buf := make([]byte, sz)
	if _, err := r.ReadAt(buf, at); err != nil && !errors.Is(err, io.EOF) {
		return info, errMedia
	}
	eachChild(buf, func(id uint32, data []byte) {
		if id != mkvEntry {
			return
		}
		t := Track{Default: true, Lang: "eng"} // valores padrão do Matroska
		var typ uint64
		var codec, bcp string
		eachChild(data, func(id uint32, d []byte) {
			switch id {
			case mkvType:
				typ = beUint(d)
			case mkvCodecID:
				codec = cstr(d)
			case mkvName:
				t.Name = cstr(d)
			case mkvLang:
				t.Lang = cstr(d)
			case mkvLangBCP:
				bcp = cstr(d)
			case mkvDefault:
				t.Default = beUint(d) != 0
			case mkvAudio:
				eachChild(d, func(id uint32, a []byte) {
					if id == mkvChannels {
						t.Channels = int(beUint(a))
					}
				})
			}
		})
		if bcp != "" {
			t.Lang = bcp
		}
		switch typ {
		case 1:
			t.Kind = "video"
		case 2:
			t.Kind = "audio"
		case 17:
			t.Kind = "sub"
		default:
			return
		}
		t.Codec = mkvCodec(codec)
		info.Tracks = append(info.Tracks, t)
	})
	if info.Tracks == nil {
		return info, errMedia
	}
	return info, nil
}

// eachChild percorre os elementos de um bloco já lido (ignora o resto se algo vier quebrado).
func eachChild(buf []byte, fn func(id uint32, data []byte)) {
	for len(buf) > 0 {
		id, sz, hl, err := ebmlHeadBytes(buf)
		if err != nil || sz < 0 || int64(len(buf)-hl) < sz {
			return
		}
		fn(id, buf[hl:int64(hl)+sz])
		buf = buf[int64(hl)+sz:]
	}
}

func beUint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

func cstr(b []byte) string { return strings.TrimRight(string(b), "\x00") }

var mkvCodecs = map[string]string{
	"V_MPEG4/ISO/AVC": "h264", "V_MPEGH/ISO/HEVC": "hevc", "V_VP8": "vp8", "V_VP9": "vp9", "V_AV1": "av1",
	"V_MPEG4/ISO/ASP": "mpeg4", "V_MPEG4/ISO/SP": "mpeg4", "V_MPEG2": "mpeg2", "V_MPEG1": "mpeg1",
	"V_MS/VFW/FOURCC": "vfw", "V_THEORA": "theora",
	"A_AAC": "aac", "A_AC3": "ac3", "A_EAC3": "eac3", "A_E-AC-3": "eac3", "A_DTS": "dts",
	"A_TRUEHD": "truehd", "A_MLP": "truehd", "A_OPUS": "opus", "A_VORBIS": "vorbis", "A_FLAC": "flac",
	"A_MPEG/L3": "mp3", "A_MPEG/L2": "mp2", "A_ALAC": "alac",
	"S_TEXT/UTF8": "srt", "S_TEXT/ASS": "ass", "S_TEXT/SSA": "ass", "S_TEXT/WEBVTT": "vtt",
	"S_HDMV/PGS": "pgs", "S_VOBSUB": "vobsub",
}

func mkvCodec(id string) string {
	if c, ok := mkvCodecs[id]; ok {
		return c
	}
	switch {
	case strings.HasPrefix(id, "A_AAC"):
		return "aac"
	case strings.HasPrefix(id, "A_DTS"):
		return "dts"
	case strings.HasPrefix(id, "A_PCM"):
		return "pcm"
	case strings.HasPrefix(id, "A_AC3"):
		return "ac3"
	}
	return id
}

// ── MP4 / MOV (ISO BMFF) ──────────────────────────────────────────

var mp4Top = map[string]bool{"ftyp": true, "moov": true, "mdat": true, "free": true, "skip": true, "wide": true, "pnot": true}

// box lê o cabeçalho de uma caixa: tipo, onde começa o conteúdo e onde ela termina.
func mp4Box(r io.ReaderAt, off, end int64) (typ string, body, next int64, err error) {
	var b [16]byte
	if n, _ := r.ReadAt(b[:8], off); n < 8 {
		return "", 0, 0, io.ErrUnexpectedEOF
	}
	sz := int64(binary.BigEndian.Uint32(b[:4]))
	typ, body = string(b[4:8]), off+8
	switch sz {
	case 0:
		sz = end - off
	case 1:
		if n, _ := r.ReadAt(b[8:16], off+8); n < 8 {
			return "", 0, 0, io.ErrUnexpectedEOF
		}
		sz, body = int64(binary.BigEndian.Uint64(b[8:16])), off+16
	}
	if sz < body-off || off+sz > end {
		return "", 0, 0, errMedia
	}
	return typ, body, off + sz, nil
}

func probeMP4(r io.ReaderAt, size int64) (MediaInfo, error) {
	info := MediaInfo{Container: "mp4"}
	off := int64(0)
	for hop := 0; off < size && hop < maxMediaHops; hop++ {
		typ, body, next, err := mp4Box(r, off, size)
		if err != nil {
			break
		}
		if typ == "moov" { // às vezes no fim do arquivo: pular as caixas grandes custa só uma leitura
			mp4Children(r, body, next, func(typ string, b, e int64) {
				if typ == "trak" {
					if t, ok := mp4Track(r, b, e); ok {
						info.Tracks = append(info.Tracks, t)
					}
				}
			})
			if info.Tracks == nil {
				return info, errMedia
			}
			// o player usa a primeira faixa ligada de cada tipo
			seen := map[string]bool{}
			for i := range info.Tracks {
				t := &info.Tracks[i]
				t.Default = t.Default && !seen[t.Kind]
				seen[t.Kind] = seen[t.Kind] || t.Default
			}
			return info, nil
		}
		off = next
	}
	return info, errMedia
}

func mp4Children(r io.ReaderAt, off, end int64, fn func(typ string, body, next int64)) {
	for hop := 0; off < end && hop < maxMediaHops; hop++ {
		typ, body, next, err := mp4Box(r, off, end)
		if err != nil {
			return
		}
		fn(typ, body, next)
		off = next
	}
}

// mp4Track lê uma faixa: tipo (hdlr), idioma (mdhd), ligada (tkhd) e formato (stsd).
func mp4Track(r io.ReaderAt, off, end int64) (Track, bool) {
	t := Track{}
	var handler string
	read := func(at int64, n int) []byte {
		b := make([]byte, n)
		if m, _ := r.ReadAt(b, at); m < n {
			return nil
		}
		return b
	}
	var walk func(off, end int64)
	walk = func(off, end int64) {
		mp4Children(r, off, end, func(typ string, b, e int64) {
			switch typ {
			case "mdia", "minf", "stbl":
				walk(b, e)
			case "tkhd":
				if v := read(b, 4); v != nil {
					t.Default = v[3]&1 != 0 // flag "ligada"
				}
			case "hdlr":
				if v := read(b+8, 4); v != nil {
					handler = string(v)
				}
			case "mdhd":
				v := read(b, 4)
				if v == nil {
					return
				}
				at := b + 4 + 16
				if v[0] == 1 {
					at = b + 4 + 28
				}
				if l := read(at, 2); l != nil {
					t.Lang = mp4Lang(binary.BigEndian.Uint16(l))
				}
			case "stsd":
				// versão/flags (4) + quantidade (4), depois a primeira entrada: tamanho (4) + formato (4)
				v := read(b+8, 8)
				if v == nil {
					return
				}
				t.Codec = mp4Codec(string(v[4:8]))
				if a := read(b+8+24, 2); a != nil && handler == "soun" {
					t.Channels = int(binary.BigEndian.Uint16(a))
				}
			}
		})
	}
	walk(off, end)
	switch handler {
	case "vide":
		t.Kind = "video"
	case "soun":
		t.Kind = "audio"
	case "sbtl", "subt", "text", "clcp":
		t.Kind = "sub"
	default:
		return t, false
	}
	return t, t.Codec != ""
}

// mp4Lang: três letras de 5 bits cada (ISO 639-2).
func mp4Lang(v uint16) string {
	if v == 0 || v == 0x7FFF {
		return ""
	}
	b := []byte{byte(v>>10&31) + 0x60, byte(v>>5&31) + 0x60, byte(v&31) + 0x60}
	for _, c := range b {
		if c < 'a' || c > 'z' {
			return ""
		}
	}
	if s := string(b); s != "und" {
		return s
	}
	return ""
}

var mp4Codecs = map[string]string{
	"avc1": "h264", "avc3": "h264", "hvc1": "hevc", "hev1": "hevc", "vp09": "vp9", "av01": "av1",
	"mp4v": "mpeg4", "mp4a": "aac", "ac-3": "ac3", "ec-3": "eac3", "dtsc": "dts", "dtsh": "dts",
	"dtsl": "dts", "dtse": "dts", "mlpa": "truehd", "Opus": "opus", "fLaC": "flac", ".mp3": "mp3",
	"alac": "alac", "lpcm": "pcm", "sowt": "pcm", "twos": "pcm", "tx3g": "tx3g", "wvtt": "vtt",
	"c608": "cc", "apch": "prores", "apcn": "prores", "apcs": "prores", "apco": "prores", "ap4h": "prores",
}

func mp4Codec(f string) string {
	if c, ok := mp4Codecs[f]; ok {
		return c
	}
	return strings.TrimSpace(f)
}
