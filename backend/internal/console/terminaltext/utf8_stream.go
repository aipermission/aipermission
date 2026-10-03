package terminaltext

import (
	"strings"
	"unicode/utf8"
)

// UTF8Stream carries at most three bytes between text frames. Each invalid byte,
// including bytes of an unfinished rune at EOF, becomes a replacement character.
type UTF8Stream struct {
	pending [utf8.UTFMax - 1]byte
	length  int
}

func (stream *UTF8Stream) Write(chunk []byte, final bool) string {
	data := make([]byte, stream.length+len(chunk))
	copy(data, stream.pending[:stream.length])
	copy(data[stream.length:], chunk)
	clear(stream.pending[:])
	stream.length = 0
	var output strings.Builder
	output.Grow(len(data))
	end := 0
	for end < len(data) {
		if !final && !utf8.FullRune(data[end:]) {
			stream.length = copy(stream.pending[:], data[end:])
			break
		}
		r, size := utf8.DecodeRune(data[end:])
		if r == utf8.RuneError && size == 1 {
			output.WriteRune(utf8.RuneError)
		} else {
			output.Write(data[end : end+size])
		}
		end += size
	}
	clear(data)
	return output.String()
}
