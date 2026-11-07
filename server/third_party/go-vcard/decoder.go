package vcard

import (
	"bufio"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	paramEncoding  = "ENCODING"
	paramCharset   = "CHARSET"
	encodingQuoted = "QUOTED-PRINTABLE"
	endCard        = "END:VCARD"
)

// A Decoder parses cards.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder creates a new Decoder reading cards from an io.Reader.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r)}
}

func (dec *Decoder) readLine() (string, error) {
	l, err := dec.r.ReadString('\n')
	l = strings.TrimRight(l, "\r\n")
	if len(l) > 0 && err == io.EOF {
		return l, nil
	} else if err != nil {
		return l, err
	}

	if isQuotedPrintableLine(l) {
		return dec.readQuotedPrintableLines(l)
	}

	var sb strings.Builder
	sb.WriteString(l)

	for {
		next, err := dec.r.Peek(1)
		if err == io.EOF {
			break
		} else if err != nil {
			return sb.String(), err
		}

		if ch := next[0]; ch != ' ' && ch != '\t' {
			break
		}

		if _, err := dec.r.Discard(1); err != nil {
			return sb.String(), err
		}

		folded, err := dec.r.ReadString('\n')
		if err != nil {
			return sb.String(), err
		}
		sb.WriteString(strings.TrimRight(folded, "\r\n"))
	}

	return sb.String(), nil
}

// readQuotedPrintableLines joins soft line breaks, which unlike folding are
// marked by a trailing "=" and need no leading whitespace on the next line.
func (dec *Decoder) readQuotedPrintableLines(l string) (string, error) {
	for strings.HasSuffix(l, "=") {
		l = strings.TrimSuffix(l, "=")

		// A malformed last line must not swallow the end of the card.
		next, _ := dec.r.Peek(len(endCard))
		if strings.EqualFold(string(next), endCard) {
			break
		}

		continued, err := dec.r.ReadString('\n')
		if err != nil && err != io.EOF {
			return l, err
		}
		l += strings.TrimRight(continued, "\r\n")
		if err == io.EOF {
			break
		}
	}
	return l, nil
}

func isQuotedPrintableLine(l string) bool {
	i := strings.IndexByte(l, ':')
	return i >= 0 && strings.Contains(strings.ToUpper(l[:i]), encodingQuoted)
}

// Decode parses a single card.
func (dec *Decoder) Decode() (Card, error) {
	card := make(Card)

	var hasBegin, hasEnd bool
	for {
		l, err := dec.readLine()
		if err == io.EOF {
			break
		} else if err != nil {
			return card, err
		}

		k, f, err := parseLine(l)
		if err != nil {
			continue
		}

		if !hasBegin {
			if k == "BEGIN" {
				if strings.ToUpper(f.Value) != "VCARD" {
					return card, errors.New("vcard: invalid BEGIN value")
				}
				hasBegin = true
				continue
			} else {
				return card, errors.New("vcard: no BEGIN field found")
			}
		} else if k == "END" {
			if strings.ToUpper(f.Value) != "VCARD" {
				return card, errors.New("vcard: invalid END value")
			}
			hasEnd = true
			break
		}

		card[k] = append(card[k], f)
	}

	if !hasEnd {
		if !hasBegin {
			return nil, io.EOF
		}
		return card, errors.New("vcard: no END field found")
	}
	return card, nil
}

func parseLine(l string) (key string, field *Field, err error) {
	field = new(Field)
	field.Group, l = parseGroup(l)
	key, hasParams, l, err := parseKey(l)
	if err != nil {
		return
	}

	if hasParams {
		field.Params, l, err = parseParams(l)
		if err != nil {
			return
		}
	}

	l = decodeQuotedPrintableField(field, l)
	field.Value = parseValue(l)
	if key == FieldOrganization {
		// Preserve component boundaries before generic text unescaping loses them.
		field.organization = &organizationValue{value: field.Value, components: parseOrganization(l)}
	}
	return
}

func parseGroup(s string) (group, tail string) {
	i := strings.IndexAny(s, ".;:")
	if i < 0 || s[i] != '.' {
		return "", s
	}
	return s[:i], s[i+1:]
}

func parseKey(s string) (key string, params bool, tail string, err error) {
	i := strings.IndexAny(s, ";:")
	if i < 0 {
		err = errors.New("vcard: invalid property key")
		return
	}
	return strings.ToUpper(s[:i]), s[i] == ';', s[i+1:], nil
}

func parseParams(s string) (params Params, tail string, err error) {
	tail = s
	params = make(Params)
	for tail != "" {
		i := strings.IndexAny(tail, "=;:")
		if i < 0 {
			err = errors.New("vcard: malformed parameters")
			return
		}
		if tail[i] != '=' {
			addBareParam(params, tail[:i])
			last := tail[i] == ':'
			tail = tail[i+1:]
			if last {
				break
			}
			continue
		}

		k := strings.ToUpper(tail[:i])

		var values []string
		var more bool
		values, more, tail, err = parseParamValues(tail[i+1:])
		if err != nil {
			return
		}

		params[k] = append(params[k], values...)

		if !more {
			break
		}
	}
	return
}

// addBareParam stores a vCard 2.1 parameter that has no name, such as CELL in
// TEL;CELL:+123, under the name later versions give it.
func addBareParam(params Params, value string) {
	if value == "" {
		return
	}
	switch strings.ToUpper(value) {
	case encodingQuoted, "BASE64", "8BIT", "7BIT":
		params.Add(paramEncoding, value)
	default:
		params.Add(ParamType, value)
	}
}

func parseParamValues(s string) (values []string, more bool, tail string, err error) {
	if s == "" {
		return
	}
	quote := s[0]

	var vs string
	if quote == '"' {
		vs, tail, err = parseQuoted(s[1:], quote)
		if tail == "" || (tail[0] != ';' && tail[0] != ':') {
			err = errors.New("vcard: malformed quoted parameter value")
			return
		}
		more = tail[0] != ':'
		tail = tail[1:]
	} else {
		i := strings.IndexAny(s, ";:")
		if i < 0 {
			vs = s
		} else {
			vs, more, tail = s[:i], s[i] != ':', s[i+1:]
		}
	}

	values = strings.Split(vs, ",")
	for i, value := range values {
		values[i] = parseValue(value)
	}
	return
}

func parseQuoted(s string, quote byte) (value, tail string, err error) {
	tail = s
	var buf []rune
	for tail != "" {
		if tail[0] == quote {
			tail = tail[1:]
			break
		}

		var r rune
		r, _, tail, err = strconv.UnquoteChar(tail, quote)
		if err != nil {
			return
		}
		buf = append(buf, r)
	}
	value = string(buf)
	return
}

// decodeQuotedPrintableField returns the decoded value and drops the parameters
// describing the old encoding, so re-encoding the field cannot claim it again.
// Values in a character set that cannot be converted are returned unchanged.
func decodeQuotedPrintableField(field *Field, value string) string {
	if !strings.EqualFold(field.Params.Get(paramEncoding), encodingQuoted) {
		return value
	}
	decoded, ok := decodeCharset(decodeQuotedPrintable(value), field.Params.Get(paramCharset))
	if !ok {
		return value
	}
	delete(field.Params, paramEncoding)
	delete(field.Params, paramCharset)
	return strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(decoded)
}

// decodeQuotedPrintable keeps anything that is not a valid escape: vCard
// exporters are less strict than mime/quotedprintable, which would reject it.
func decodeQuotedPrintable(s string) []byte {
	decoded := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '=' && i+2 < len(s) {
			if b, err := hex.DecodeString(s[i+1 : i+3]); err == nil {
				decoded = append(decoded, b[0])
				i += 2
				continue
			}
		}
		decoded = append(decoded, s[i])
	}
	return decoded
}

// decodeCharset treats a missing CHARSET as UTF-8, falling back to ISO-8859-1,
// which older exporters often use without declaring it.
func decodeCharset(b []byte, charset string) (string, bool) {
	switch strings.ToUpper(charset) {
	case "", "UTF-8", "UTF8", "US-ASCII":
		if utf8.Valid(b) {
			return string(b), true
		}
	case "ISO-8859-1", "LATIN1":
	default:
		return "", false
	}

	runes := make([]rune, len(b))
	for i, c := range b {
		runes[i] = rune(c)
	}
	return string(runes), true
}

var valueParser = strings.NewReplacer("\\\\", "\\", "\\n", "\n", "\\,", ",")

func parseValue(s string) string {
	return valueParser.Replace(s)
}
