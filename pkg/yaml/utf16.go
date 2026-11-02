package yaml

import (
	"encoding/binary"
	"unicode/utf16"
)

// toUTF8 returns data as UTF-8 when it starts with a UTF-16 byte order
// mark, which graft's reader would otherwise take for text, and returns
// data itself otherwise. A UTF-16 document with no content, with an odd
// number of bytes, or with an unpaired surrogate is a syntax error. The
// errors say nothing about the contents.
func toUTF8(data []byte) ([]byte, error) {
	if len(data) < 2 {
		return data, nil
	}
	var order binary.ByteOrder
	switch {
	case data[0] == 0xFF && data[1] == 0xFE:
		order = binary.LittleEndian
	case data[0] == 0xFE && data[1] == 0xFF:
		order = binary.BigEndian
	default:
		return data, nil
	}
	body := data[2:]
	if len(body) == 0 {
		return nil, &DecodeError{Kind: ErrorKindSyntax, Msg: "a UTF-16 document has no content after its byte order mark"}
	}
	if len(body)%2 != 0 {
		return nil, &DecodeError{Kind: ErrorKindSyntax, Msg: "a UTF-16 document has an odd number of bytes"}
	}
	units := make([]uint16, len(body)/2)
	for i := range units {
		units[i] = order.Uint16(body[2*i:])
	}
	for i := 0; i < len(units); i++ {
		switch u := units[i]; {
		case u >= 0xD800 && u < 0xDC00 && i+1 < len(units) && units[i+1] >= 0xDC00 && units[i+1] < 0xE000:
			i++
		case u >= 0xD800 && u < 0xE000:
			return nil, &DecodeError{Kind: ErrorKindSyntax, Msg: "a UTF-16 document holds an unpaired surrogate"}
		}
	}
	runes := utf16.Decode(units)
	return []byte(string(runes)), nil
}
