package parser

import "testing"

func TestQuoteUnicodeRoundtrip(t *testing.T) {
	s := "Hello {b}world{/b} [player] %%\nnext"
	q := QuoteUnicode(s)
	quoted := "\"" + q + "\""
	dec, ok := decodeQuoted(quoted)
	if !ok {
		t.Fatalf("decode failed")
	}
	if dec != s {
		t.Fatalf("roundtrip mismatch %q vs %q", dec, s)
	}
}

func TestFileBase(t *testing.T) {
	if FileBase("/a/b/c.rpy") != "c.rpy" {
		t.Fatalf("filebase unix failed")
	}
	if FileBase("a\\b\\c.rpy") != "c.rpy" {
		t.Fatalf("filebase windows failed")
	}
	if FileBase("c.rpy") != "c.rpy" {
		t.Fatalf("filebase single failed")
	}
}

func TestEscapeValid(t *testing.T) {
	if !EscapeValid("hello") {
		t.Fatalf("escape valid failed")
	}
	if !EscapeValid("line\nbreak") {
		t.Fatalf("escape valid newline failed")
	}
}
