// Package chunker groups units under budgets.
package chunker

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/galpt/renpy-tl/internal/config"
	"github.com/galpt/renpy-tl/internal/parser"
)

// Chunk holds a batch of units.
type Chunk struct {
	ID       string
	File     string
	Offset   int
	Units    []interface{}
	TokenEst int
	LineEst  int
}

func (c Chunk) Len() int { return len(c.Units) }

// Chunker enforces budgets.
type Chunker struct {
	MaxPairs  int
	MaxLines  int
	MaxTokens int
}

func New() *Chunker {
	return &Chunker{
		MaxPairs:  config.ChunkMaxPairs,
		MaxLines:  config.ChunkMaxLines,
		MaxTokens: config.ChunkMaxTokens,
	}
}

func estimateTokens(text string) int {
	return (len(text) + config.CharsPerToken - 1) / config.CharsPerToken
}

func (c *Chunker) unitTokens(u interface{}) int {
	var old, nw string
	switch v := u.(type) {
	case parser.StringPair:
		old = v.Old
		nw = v.New
	case parser.DialogueBlock:
		old = v.Old
		nw = v.New
	case *parser.StringPair:
		old = v.Old
		nw = v.New
	case *parser.DialogueBlock:
		old = v.Old
		nw = v.New
	}
	return estimateTokens(old+nw) + 10
}

func (c *Chunker) unitLines(u interface{}) int {
	var old string
	switch v := u.(type) {
	case parser.StringPair:
		old = v.Old
	case parser.DialogueBlock:
		old = v.Old
	case *parser.StringPair:
		old = v.Old
	case *parser.DialogueBlock:
		old = v.Old
	}
	return 1 + strings.Count(old, "\n")
}

// Chunk groups units per file, streaming, never split block.
func (c *Chunker) Chunk(units []interface{}) []Chunk {
	byFile := make(map[string][]interface{})
	for _, u := range units {
		var f string
		switch v := u.(type) {
		case parser.StringPair:
			f = v.File
		case parser.DialogueBlock:
			f = v.File
		case *parser.StringPair:
			f = v.File
		case *parser.DialogueBlock:
			f = v.File
		}
		byFile[f] = append(byFile[f], u)
	}
	// deterministic file order.
	files := make([]string, 0, len(byFile))
	for k := range byFile {
		files = append(files, k)
	}
	sort.Strings(files)

	var chunks []Chunk
	for _, fp := range files {
		fUnits := byFile[fp]
		sort.Slice(fUnits, func(a, b int) bool {
			var la, lb int
			switch v := fUnits[a].(type) {
			case parser.StringPair:
				la = v.Lineno
			case parser.DialogueBlock:
				la = v.Lineno
			case *parser.StringPair:
				la = v.Lineno
			case *parser.DialogueBlock:
				la = v.Lineno
			}
			switch v := fUnits[b].(type) {
			case parser.StringPair:
				lb = v.Lineno
			case parser.DialogueBlock:
				lb = v.Lineno
			case *parser.StringPair:
				lb = v.Lineno
			case *parser.DialogueBlock:
				lb = v.Lineno
			}
			return la < lb
		})
		var cur []interface{}
		curTokens := 0
		curLines := 0
		startOffset := 0
		for idx, unit := range fUnits {
			ut := c.unitTokens(unit)
			ul := c.unitLines(unit)
			exceeds := false
			if len(cur) > 0 {
				if len(cur)+1 > c.MaxPairs {
					exceeds = true
				}
				if curLines+ul > c.MaxLines {
					exceeds = true
				}
				if curTokens+ut > c.MaxTokens {
					exceeds = true
				}
			}
			if exceeds {
				id := filepath.Base(fp) + ":" + itoa(startOffset)
				chunks = append(chunks, Chunk{
					ID:       id,
					File:     fp,
					Offset:   startOffset,
					Units:    append([]interface{}(nil), cur...),
					TokenEst: curTokens,
					LineEst:  curLines,
				})
				cur = nil
				curTokens = 0
				curLines = 0
				startOffset = idx
			}
			cur = append(cur, unit)
			curTokens += ut
			curLines += ul
			if len(cur) >= c.MaxPairs || curLines >= c.MaxLines || curTokens >= c.MaxTokens {
				id := filepath.Base(fp) + ":" + itoa(startOffset)
				chunks = append(chunks, Chunk{
					ID:       id,
					File:     fp,
					Offset:   startOffset,
					Units:    append([]interface{}(nil), cur...),
					TokenEst: curTokens,
					LineEst:  curLines,
				})
				cur = nil
				curTokens = 0
				curLines = 0
				startOffset = idx + 1
			}
		}
		if len(cur) > 0 {
			id := filepath.Base(fp) + ":" + itoa(startOffset)
			chunks = append(chunks, Chunk{
				ID:       id,
				File:     fp,
				Offset:   startOffset,
				Units:    append([]interface{}(nil), cur...),
				TokenEst: curTokens,
				LineEst:  curLines,
			})
		}
	}
	return chunks
}

func itoa(i int) string {
	// small helper no fmt.
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
