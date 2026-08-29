package e2e

import (
	"os"
	"path/filepath"
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
