package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/galpt/renpy-tl/internal/chunker"
	"github.com/galpt/renpy-tl/internal/parser"
	"github.com/galpt/renpy-tl/internal/validator"
	"github.com/galpt/renpy-tl/internal/writer"
)

func TestParseChunkValidateWrite(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "input")
	p := parser.New(root)
	units, err := p.ParseInputFolder(root)
	if err != nil {
		t.Fatalf("parse error %v", err)
	}
	if len(units) == 0 {
		t.Fatalf("no units parsed")
	}
	empty, err := p.FindEmptyInFolder(root)
	if err != nil {
		t.Fatalf("find empty %v", err)
	}
	if len(empty) == 0 {
		t.Fatalf("no empty found")
	}
	ch := chunker.New()
	chunks := ch.Chunk(empty)
	if len(chunks) == 0 {
		t.Fatalf("no chunks")
	}
	v := &validator.Validator{}
	proposed := make(map[string]string)
	for _, u := range empty {
		var key string
		switch o := u.(type) {
		case parser.StringPair:
			key = parser.FileBase(o.File) + "\x1f" + o.Old
			proposed[key] = "TR " + o.Old
		case parser.DialogueBlock:
			key = o.Hash + "\x1f" + o.Old
			proposed[key] = "TR " + o.Old
		}
	}
	valid := v.ValidateBatch(empty, proposed)
	if len(valid) == 0 {
		t.Fatalf("validate filtered all")
	}
	tmpOut, err := os.MkdirTemp("", "renpy-tl-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpOut)
	w, err := writer.New(root, tmpOut, "german")
	if err != nil {
		t.Fatalf("writer new %v", err)
	}
	written, err := w.Write(empty, valid)
	if err != nil {
		t.Fatalf("write %v", err)
	}
	if len(written) == 0 {
		t.Fatalf("no files written")
	}
	for _, f := range written {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %v", err)
		}
		if len(data) == 0 {
			t.Fatalf("empty output %s", f)
		}
	}
	if parser.FileBase("/a/b/c.rpy") != "c.rpy" {
		t.Fatalf("filebase failed")
	}
	if parser.FileBase("a\\b\\c.rpy") != "c.rpy" {
		t.Fatalf("filebase backslash failed")
	}
}

func TestSuffixQuotedPreservesSuffix(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "input")
	p := parser.New(root)
	path := filepath.Join(root, "suffix_quoted.rpy")
	units, err := p.ParseFile(path)
	if err != nil {
		t.Fatalf("parse file %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 units in suffix_quoted, got %d", len(units))
	}
	var plainFound bool
	var quotedFound bool
	for _, u := range units {
		d, ok := u.(parser.DialogueBlock)
		if !ok {
			t.Fatalf("expected DialogueBlock, got %T", u)
		}
		if !d.IsEmpty {
			t.Fatalf("expected empty, got %v", d)
		}
		switch d.Suffix {
		case "window_background":
			plainFound = true
		case "window_background=\"gui/textbox.png\"":
			quotedFound = true
		default:
			t.Fatalf("unexpected suffix %q", d.Suffix)
		}
	}
	if !plainFound {
		t.Fatalf("plain window_background not found")
	}
	if !quotedFound {
		t.Fatalf("quoted window_background not found")
	}
	empty, err := p.FindEmptyInFolder(root)
	if err != nil {
		t.Fatalf("find empty %v", err)
	}
	// filter to suffix_quoted only.
	var filtered []interface{}
	for _, u := range empty {
		var file string
		switch v := u.(type) {
		case parser.DialogueBlock:
			file = v.File
		case parser.StringPair:
			file = v.File
		}
		if strings.Contains(file, "suffix_quoted.rpy") {
			filtered = append(filtered, u)
		}
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 empty from suffix_quoted, got %d", len(filtered))
	}
	v := &validator.Validator{}
	proposed := make(map[string]string)
	for _, u := range filtered {
		d := u.(parser.DialogueBlock)
		key := d.Hash + "\x1f" + d.Old
		proposed[key] = "TR " + d.Old
	}
	valid := v.ValidateBatch(filtered, proposed)
	if len(valid) != 2 {
		t.Fatalf("validate filtered, expected 2 got %d", len(valid))
	}
	tmpOut, err := os.MkdirTemp("", "renpy-tl-suffix-quoted")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpOut)
	w, err := writer.New(root, tmpOut, "german")
	if err != nil {
		t.Fatalf("writer new %v", err)
	}
	written, err := w.Write(filtered, valid)
	if err != nil {
		t.Fatalf("write %v", err)
	}
	if len(written) == 0 {
		t.Fatalf("no files written")
	}
	var target string
	for _, f := range written {
		if strings.Contains(f, "suffix_quoted.rpy") {
			target = f
			break
		}
	}
	if target == "" {
		t.Fatalf("suffix_quoted not written, got %v", written)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "window_background=\"gui/textbox.png\"") {
		t.Fatalf("quoted suffix not preserved exact, content %q", content)
	}
	if !strings.Contains(content, "window_background") {
		t.Fatalf("plain suffix not preserved, content %q", content)
	}
	// both lines should contain TR translation and suffix exactly.
	if !strings.Contains(content, "\"TR Hello plain suffix\" window_background") {
		t.Fatalf("plain line not correct, got %q", content)
	}
	if !strings.Contains(content, "\"TR Hello quoted suffix\" window_background=\"gui/textbox.png\"") {
		t.Fatalf("quoted line not correct, got %q", content)
	}
}
