package vcard

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func decodeVCard21(t *testing.T, lines ...string) Card {
	t.Helper()
	body := append([]string{"BEGIN:VCARD", "VERSION:2.1"}, lines...)
	body = append(body, "END:VCARD", "")
	card, err := NewDecoder(strings.NewReader(strings.Join(body, "\r\n"))).Decode()
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	return card
}

func TestDecoder_quotedPrintable(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"fully encoded", "NOTE;CHARSET=UTF-8;ENCODING=QUOTED-PRINTABLE:=4B=6C=6F=C3=9F", "Kloß"},
		{"partly encoded", "NOTE;ENCODING=QUOTED-PRINTABLE:Klo=C3=9F", "Kloß"},
		{"lowercase parameter", "NOTE;encoding=quoted-printable:Klo=C3=9F", "Kloß"},
		{"bare parameter", "NOTE;CHARSET=UTF-8;QUOTED-PRINTABLE:Klo=C3=9F", "Kloß"},
		{"bare parameter last", "NOTE;QUOTED-PRINTABLE:Klo=C3=9F", "Kloß"},
		{"declared latin-1", "NOTE;CHARSET=ISO-8859-1;ENCODING=QUOTED-PRINTABLE:Klo=DF", "Kloß"},
		{"undeclared latin-1", "NOTE;ENCODING=QUOTED-PRINTABLE:Klo=DF", "Kloß"},
		{"line breaks", "NOTE;ENCODING=QUOTED-PRINTABLE:a=0D=0Ab=0Ac", "a\nb\nc"},
		{"invalid escape", "NOTE;ENCODING=QUOTED-PRINTABLE:1=2 =ZZ", "1=2 =ZZ"},
		{"not encoded", "NOTE:=4B=6C=6F", "=4B=6C=6F"},
		{"unsupported charset", "NOTE;CHARSET=SHIFT_JIS;ENCODING=QUOTED-PRINTABLE:=82=A0", "=82=A0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			card := decodeVCard21(t, test.line)
			if got := card.Value(FieldNote); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestDecoder_quotedPrintableSoftLineBreaks(t *testing.T) {
	card := decodeVCard21(t,
		"N;CHARSET=UTF-8;ENCODING=QUOTED-PRINTABLE:=4B=6C=6F=",
		"=C3=9F;=41=6C=65=",
		"=78;;;",
		"NOTE:after",
	)
	name := card.Name()
	if name.FamilyName != "Kloß" || name.GivenName != "Alex" {
		t.Fatalf("unexpected name: %+v", name)
	}
	if got := card.Value(FieldNote); got != "after" {
		t.Fatalf("expected the next property to survive, got %q", got)
	}
}

func TestDecoder_quotedPrintableSplitCharacter(t *testing.T) {
	card := decodeVCard21(t, "NOTE;ENCODING=QUOTED-PRINTABLE:M=C3=", "=BCnchen")
	if got := card.Value(FieldNote); got != "München" {
		t.Fatalf("expected München, got %q", got)
	}
}

func TestDecoder_quotedPrintableMalformedLastLine(t *testing.T) {
	card := decodeVCard21(t, "NOTE;ENCODING=QUOTED-PRINTABLE:=4B=")
	if got := card.Value(FieldNote); got != "K" {
		t.Fatalf("expected K, got %q", got)
	}
}

func TestDecoder_quotedPrintableOrganization(t *testing.T) {
	card := decodeVCard21(t, "ORG;CHARSET=UTF-8;ENCODING=QUOTED-PRINTABLE:M=C3=BCller GmbH;Vertrieb")
	want := []string{"Müller GmbH", "Vertrieb"}
	if got := card[FieldOrganization][0].Organization(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestDecoder_quotedPrintableDropsEncodingParams(t *testing.T) {
	card := decodeVCard21(t, "NOTE;CHARSET=UTF-8;ENCODING=QUOTED-PRINTABLE:Klo=C3=9F")
	var encoded bytes.Buffer
	if err := NewEncoder(&encoded).Encode(card); err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	output := encoded.String()
	if strings.Contains(output, encodingQuoted) || strings.Contains(output, paramCharset) {
		t.Fatalf("encoded card still claims the old encoding: %s", output)
	}
	if !strings.Contains(output, "NOTE:Kloß") {
		t.Fatalf("expected a plain UTF-8 note: %s", output)
	}
}

func TestDecoder_bareParams(t *testing.T) {
	card := decodeVCard21(t,
		"TEL;CELL:+4915112345678",
		"TEL;HOME;VOICE;PREF:030123",
		"EMAIL;INTERNET:a@example.com",
		"ADR;HOME:;;Str 1;Berlin;;;",
	)

	cell := card[FieldTelephone][0]
	if cell.Value != "+4915112345678" || !cell.Params.HasType("cell") {
		t.Fatalf("unexpected TEL: %+v", cell)
	}
	home := card[FieldTelephone][1]
	if home.Value != "030123" || !home.Params.HasType("home") || !home.Params.HasType("voice") || !home.Params.HasType("pref") {
		t.Fatalf("unexpected TEL: %+v", home)
	}
	if got := card.Value(FieldEmail); got != "a@example.com" {
		t.Fatalf("unexpected EMAIL: %q", got)
	}
	if got := card.Address(); got == nil || got.StreetAddress != "Str 1" || got.Locality != "Berlin" {
		t.Fatalf("unexpected ADR: %+v", got)
	}
}
