# Structured ORG compatibility patch

Base: github.com/emersion/go-vcard v0.1.0 (MIT; original LICENSE retained).
All original production and test files are included. Only card.go, decoder.go,
encoder.go and the added organization.go, organization_test.go and
decoder_vcard21_test.go differ from that base.

The original decoder unescapes backslashes before a caller can distinguish a
literal semicolon from a component separator. The encoder also lacks component
escaping. Application-only splitting cannot recover that lost information.

This patch preserves decoded ORG components in private Field metadata while
keeping Value's existing text representation. Field.Organization and
NewOrganization expose unescaped components; the encoder escapes each component
individually. Other properties and codecs remain unchanged. No protocol-visible
private marker or runtime monkey patch is used.

Remove this local replacement when upstream provides equivalent component-aware
ORG decoding and encoding. Keep the organization codec, import/subscription and
HTTP regression tests when upgrading. Run this module's upstream and added tests
with `go test ./...`, as well as the application's dual-database vCard/DAV suites.

# vCard 2.1 decoding patch

Phones commonly export vCard 2.1, which the original decoder does not support.
It stored quoted-printable values undecoded, and it misread parameters without a
name: TEL;CELL:+123 became a CELL parameter with an empty value, losing the number.

This patch decodes ENCODING=QUOTED-PRINTABLE values, including soft line breaks,
and converts UTF-8 and ISO-8859-1 to UTF-8. Values in other character sets stay
as they are. Decoded fields lose their ENCODING and CHARSET parameters. Parameters
without a name are stored as TYPE, or as ENCODING for encoding names.

Keep decoder_vcard21_test.go and the application's import tests when upgrading.
