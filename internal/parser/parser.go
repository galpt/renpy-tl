// Package parser reads RenPy translation files.
package parser

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/galpt/renpy-tl/internal/config"
)

// quote table mirrors RenPy quote unicode.
func QuoteUnicode(s string) string {
	// order matters. backslash first.
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\a", "\\a")
	s = strings.ReplaceAll(s, "\b", "\\b")
	s = strings.ReplaceAll(s, "\f", "\\f")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = strings.ReplaceAll(s, "\v", "\\v")
	return s
}

// StringPair holds a strings block entry.
type StringPair struct {
	Kind      string `json:"kind"`
	File      string `json:"file"`
	Lineno    int    `json:"lineno"`
	OldRaw    string `json:"old_raw"`
	NewRaw    string `json:"new_raw"`
	Old       string `json:"old"`
	New       string `json:"new"`
	IsEmpty   bool   `json:"is_empty"`
	OldQuoted string `json:"old_quoted"`
	HasBOM    bool   `json:"has_bom"`
}

// DialogueBlock holds a dialogue entry.
type DialogueBlock struct {
	Kind       string  `json:"kind"`
	File       string  `json:"file"`
	Lineno     int     `json:"lineno"`
	Identifier string  `json:"identifier"`
	Hash       string  `json:"hash"`
	Label      string  `json:"label"`
	OldRaw     string  `json:"old_raw"`
	NewRaw     string  `json:"new_raw"`
	Old        string  `json:"old"`
	New        string  `json:"new"`
	Speaker    *string `json:"speaker"`
	Suffix     string  `json:"suffix"`
	IsEmpty    bool    `json:"is_empty"`
	HasBOM     bool    `json:"has_bom"`
}

// Unit is common interface for blocks.
type Unit interface {
	GetFile() string
	GetLineno() int
	GetOld() string
	GetNew() string
	GetIsEmpty() bool
	GetHasBOM() bool
}

func (s StringPair) GetFile() string  { return s.File }
func (s StringPair) GetLineno() int   { return s.Lineno }
func (s StringPair) GetOld() string   { return s.Old }
func (s StringPair) GetNew() string   { return s.New }
func (s StringPair) GetIsEmpty() bool { return s.IsEmpty }
func (s StringPair) GetHasBOM() bool  { return s.HasBOM }

func (d DialogueBlock) GetFile() string  { return d.File }
func (d DialogueBlock) GetLineno() int   { return d.Lineno }
func (d DialogueBlock) GetOld() string   { return d.Old }
func (d DialogueBlock) GetNew() string   { return d.New }
func (d DialogueBlock) GetIsEmpty() bool { return d.IsEmpty }
func (d DialogueBlock) GetHasBOM() bool  { return d.HasBOM }

// decodeQuoted decodes a quoted literal like "\"hi\\n\"" via literal_eval parity.
func decodeQuoted(quoted string) (string, bool) {
	if len(quoted) < 2 {
		return "", false
	}
	// remove backslash + actual newline inside quoted.
	if strings.Contains(quoted, "\\\n") {
		quoted = strings.ReplaceAll(quoted, "\\\n", "")
	}
	qchar := quoted[0]
	if qchar != '"' && qchar != '\'' {
		return "", false
	}
	if quoted[len(quoted)-1] != qchar {
		return "", false
	}
	inner := quoted[1 : len(quoted)-1]
	// unescape python style.
	decoded, ok := unescapePythonString(inner)
	if !ok {
		return "", false
	}
	if len([]byte(decoded)) > config.MaxStringBytes {
		return "", false
	}
	// also ensure valid utf8.
	if !utf8.ValidString(decoded) {
		return "", false
	}
	return decoded, true
}

// unescapePythonString handles escapes. \a \b \f \n \r \t \v \\ \' \" \x \ooo \u \U \N.
func unescapePythonString(s string) (string, bool) {
	var out bytes.Buffer
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			out.WriteByte(c)
			i++
			continue
		}
		// escape.
		if i+1 >= len(s) {
			return "", false
		}
		n := s[i+1]
		switch n {
		case '\\':
			out.WriteByte('\\')
			i += 2
		case '\'':
			out.WriteByte('\'')
			i += 2
		case '"':
			out.WriteByte('"')
			i += 2
		case 'a':
			out.WriteByte('\a')
			i += 2
		case 'b':
			out.WriteByte('\b')
			i += 2
		case 'f':
			out.WriteByte('\f')
			i += 2
		case 'n':
			out.WriteByte('\n')
			i += 2
		case 'r':
			out.WriteByte('\r')
			i += 2
		case 't':
			out.WriteByte('\t')
			i += 2
		case 'v':
			out.WriteByte('\v')
			i += 2
		case 'x':
			// \xhh.
			if i+3 >= len(s) {
				return "", false
			}
			hex := s[i+2 : i+4]
			var v int
			_, err := fmt.Sscanf(hex, "%02x", &v)
			if err != nil {
				return "", false
			}
			out.WriteByte(byte(v))
			i += 4
		case 'u':
			// \uXXXX.
			if i+5 >= len(s) {
				return "", false
			}
			hex := s[i+2 : i+6]
			var v int
			_, err := fmt.Sscanf(hex, "%04x", &v)
			if err != nil {
				return "", false
			}
			out.WriteRune(rune(v))
			i += 6
		case 'U':
			if i+9 >= len(s) {
				return "", false
			}
			hex := s[i+2 : i+10]
			var v int
			_, err := fmt.Sscanf(hex, "%08x", &v)
			if err != nil {
				return "", false
			}
			out.WriteRune(rune(v))
			i += 10
		case 'N':
			// N name simplified. keep literal.
			// find closing brace.
			end := strings.IndexByte(s[i:], '}')
			if end == -1 {
				return "", false
			}
			// not supported. treat as unicode replacement. skip.
			// For parity just keep original sequence without conversion.
			// but to stay conservative fail if encountered.
			return "", false
		default:
			// octal \ooo (1-3 digits).
			if n >= '0' && n <= '7' {
				oct := string(n)
				j := i + 2
				for k := 0; k < 2 && j < len(s) && s[j] >= '0' && s[j] <= '7'; k++ {
					oct += string(s[j])
					j++
				}
				var v int
				_, err := fmt.Sscanf(oct, "%o", &v)
				if err != nil {
					return "", false
				}
				out.WriteByte(byte(v))
				i = j
			} else {
				// unknown escape keep as is. python keeps backslash plus char for unknown.
				// python literal eval would treat unknown as error except for specific.
				// we treat as literal char.
				out.WriteByte(n)
				i += 2
			}
		}
	}
	return out.String(), true
}

// extractQuoted finds first quoted string in line.
// Returns quoted, suffix, speaker.
func extractQuoted(line string) (string, string, *string, bool) {
	stripped := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(stripped, "#") {
		stripped = strings.TrimLeft(stripped[1:], " \t")
	}
	// find first quote.
	qpos := -1
	var qchar byte
	for i := 0; i < len(stripped); i++ {
		if stripped[i] == '"' || stripped[i] == '\'' {
			qpos = i
			qchar = stripped[i]
			break
		}
	}
	if qpos == -1 {
		return "", "", nil, false
	}
	var speaker *string
	if qpos > 0 {
		sp := strings.TrimSpace(stripped[:qpos])
		if sp != "" {
			s := sp
			speaker = &s
		}
	}
	// scan for closing quote.
	escaped := false
	end := -1
	for i := qpos + 1; i < len(stripped); i++ {
		ch := stripped[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == qchar {
			end = i
			break
		}
	}
	if end == -1 {
		return "", "", nil, false
	}
	quoted := stripped[qpos : end+1]
	suffix := strings.TrimSpace(stripped[end+1:])
	return quoted, suffix, speaker, true
}

// Parser walks translation files.
type Parser struct {
	Root string
}

func New(root string) *Parser {
	return &Parser{Root: root}
}

// readTextPreserve returns normalized text, hasBOM flag.
func readTextPreserve(path string) (string, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	if len(raw) > config.MaxFileBytes {
		return "", false, fmt.Errorf("file too large: %s %d > %d", path, len(raw), config.MaxFileBytes)
	}
	hasBOM := bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if hasBOM {
		raw = raw[3:]
	}
	text := string(raw)
	// normalize line endings to \n.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	// handle backslash newline continuation.
	text = strings.ReplaceAll(text, "\\\n", "")
	return text, hasBOM, nil
}

// ParseFile returns units for a single file.
func (p *Parser) ParseFile(path string) ([]interface{}, error) {
	text, hasBOM, err := readTextPreserve(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(text, "\n")
	var out []interface{}
	i := 0
	for i < len(lines) {
		line := lines[i]
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			i++
			continue
		}
		m := config.TranslateRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			i++
			continue
		}
		// extract lang and ident via named groups.
		langIdx := config.TranslateRE.SubexpIndex("lang")
		identIdx := config.TranslateRE.SubexpIndex("ident")
		_ = langIdx // keep for parity.
		ident := ""
		if identIdx >= 0 && identIdx < len(m) {
			ident = m[identIdx]
		}
		headerLineno := i + 1
		if ident == "strings" {
			i++
			for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
				i++
			}
			for i < len(lines) {
				cur := lines[i]
				if strings.TrimSpace(cur) == "" {
					i++
					continue
				}
				if config.TranslateRE.MatchString(strings.TrimSpace(cur)) {
					break
				}
				if strings.HasPrefix(strings.TrimLeft(cur, " \t"), "#") {
					i++
					continue
				}
				if strings.HasPrefix(strings.TrimLeft(cur, " \t"), "old ") {
					oldLine := cur
					quoted, _, _, ok := extractQuoted(oldLine)
					if !ok {
						i++
						continue
					}
					oldDecoded, ok := decodeQuoted(quoted)
					if !ok {
						i++
						continue
					}
					i++
					for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
						i++
					}
					if i >= len(lines) {
						break
					}
					newLine := lines[i]
					if !strings.HasPrefix(strings.TrimLeft(newLine, " \t"), "new ") {
						continue
					}
					quoted2, _, _, ok2 := extractQuoted(newLine)
					if !ok2 {
						i++
						continue
					}
					newDecoded, ok2 := decodeQuoted(quoted2)
					if !ok2 {
						i++
						continue
					}
					isEmpty := newDecoded == ""
					out = append(out, StringPair{
						Kind:      "strings",
						File:      path,
						Lineno:    headerLineno,
						OldRaw:    oldLine,
						NewRaw:    newLine,
						Old:       oldDecoded,
						New:       newDecoded,
						IsEmpty:   isEmpty,
						OldQuoted: quoted,
						HasBOM:    hasBOM,
					})
					i++
				} else {
					i++
					if i < len(lines) && config.TranslateRE.MatchString(strings.TrimSpace(cur)) {
						break
					}
				}
			}
			continue
		}
		// dialogue.
		hm := config.HashRE.FindString(ident)
		if hm == "" {
			i++
			continue
		}
		hashPart := hm
		loc := config.HashRE.FindStringIndex(ident)
		label := ""
		if loc != nil {
			label = ident[:loc[0]]
		}
		i++
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		if i >= len(lines) {
			break
		}
		oldComment := lines[i]
		if !strings.HasPrefix(strings.TrimLeft(oldComment, " \t"), "#") {
			i++
			continue
		}
		quotedOld, _, oldSpeaker, ok := extractQuoted(oldComment)
		if !ok {
			i++
			continue
		}
		_ = oldSpeaker
		oldDecoded, ok := decodeQuoted(quotedOld)
		if !ok {
			i++
			continue
		}
		i++
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			// if next is translate header, break.
			if i < len(lines) && config.TranslateRE.MatchString(strings.TrimSpace(lines[i])) {
				break
			}
			i++
			if i < len(lines) && config.TranslateRE.MatchString(strings.TrimSpace(lines[i])) {
				break
			}
		}
		if i >= len(lines) {
			break
		}
		newLine := lines[i]
		if strings.HasPrefix(strings.TrimLeft(newLine, " \t"), "#") || strings.TrimSpace(newLine) == "" {
			i++
			continue
		}
		quotedNew, suffix, speaker, ok := extractQuoted(newLine)
		if !ok {
			i++
			continue
		}
		newDecoded, ok := decodeQuoted(quotedNew)
		if !ok {
			i++
			continue
		}
		isEmpty := newDecoded == ""
		// speaker fallback handled by caller, keep as found.
		_ = quotedOld
		out = append(out, DialogueBlock{
			Kind:       "dialogue",
			File:       path,
			Lineno:     headerLineno,
			Identifier: strings.TrimSpace(ident),
			Hash:       hashPart,
			Label:      label,
			OldRaw:     oldComment,
			NewRaw:     newLine,
			Old:        oldDecoded,
			New:        newDecoded,
			Speaker:    speaker,
			Suffix:     strings.TrimSpace(suffix),
			IsEmpty:    isEmpty,
			HasBOM:     hasBOM,
		})
		i++
	}
	return out, nil
}

// ParseDirectory walks root/lang recursively.
func (p *Parser) ParseDirectory(lang string) ([]interface{}, error) {
	target := filepath.Join(p.Root, lang)
	if _, err := os.Stat(target); err != nil {
		return nil, nil
	}
	var out []interface{}
	_ = filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".rpy" {
			return nil
		}
		// skip symlinks.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		// extra check via Lstat.
		if fi, e := os.Lstat(path); e == nil && fi.Mode()&fs.ModeSymlink != 0 {
			return nil
		}
		blocks, e := p.ParseFile(path)
		if e != nil {
			// file too large, skip.
			return nil
		}
		out = append(out, blocks...)
		return nil
	})
	// sort for deterministic order file then lineno.
	sort.Slice(out, func(a, b int) bool {
		var fa, fb string
		var la, lb int
		switch v := out[a].(type) {
		case StringPair:
			fa = v.File
			la = v.Lineno
		case DialogueBlock:
			fa = v.File
			la = v.Lineno
		case *StringPair:
			fa = v.File
			la = v.Lineno
		case *DialogueBlock:
			fa = v.File
			la = v.Lineno
		}
		switch v := out[b].(type) {
		case StringPair:
			fb = v.File
			lb = v.Lineno
		case DialogueBlock:
			fb = v.File
			lb = v.Lineno
		case *StringPair:
			fb = v.File
			lb = v.Lineno
		case *DialogueBlock:
			fb = v.File
			lb = v.Lineno
		}
		if fa == fb {
			return la < lb
		}
		return fa < fb
	})
	return out, nil
}

// ParseInputFolder parses all .rpy files under a flat input folder.
func (p *Parser) ParseInputFolder(inputFolder string) ([]interface{}, error) {
	var out []interface{}
	_ = filepath.WalkDir(inputFolder, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".rpy" {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if fi, e := os.Lstat(path); e == nil && fi.Mode()&fs.ModeSymlink != 0 {
			return nil
		}
		blocks, e := p.ParseFile(path)
		if e != nil {
			return nil
		}
		out = append(out, blocks...)
		return nil
	})
	sort.Slice(out, func(a, b int) bool {
		var fa, fb string
		var la, lb int
		switch v := out[a].(type) {
		case StringPair:
			fa = v.File
			la = v.Lineno
		case DialogueBlock:
			fa = v.File
			la = v.Lineno
		case *StringPair:
			fa = v.File
			la = v.Lineno
		case *DialogueBlock:
			fa = v.File
			la = v.Lineno
		}
		switch v := out[b].(type) {
		case StringPair:
			fb = v.File
			lb = v.Lineno
		case DialogueBlock:
			fb = v.File
			lb = v.Lineno
		case *StringPair:
			fb = v.File
			lb = v.Lineno
		case *DialogueBlock:
			fb = v.File
			lb = v.Lineno
		}
		if fa == fb {
			return la < lb
		}
		return fa < fb
	})
	return out, nil
}

// FindEmpty returns only empty units for lang under root.
func (p *Parser) FindEmpty(lang string) ([]interface{}, error) {
	all, err := p.ParseDirectory(lang)
	if err != nil {
		return nil, err
	}
	var out []interface{}
	for _, b := range all {
		switch v := b.(type) {
		case StringPair:
			if v.IsEmpty {
				out = append(out, v)
			}
		case DialogueBlock:
			if v.IsEmpty {
				out = append(out, v)
			}
		}
	}
	return out, nil
}

// FindEmptyInFolder finds empties under flat folder.
func (p *Parser) FindEmptyInFolder(folder string) ([]interface{}, error) {
	all, err := p.ParseInputFolder(folder)
	if err != nil {
		return nil, err
	}
	var out []interface{}
	for _, b := range all {
		switch v := b.(type) {
		case StringPair:
			if v.IsEmpty {
				out = append(out, v)
			}
		case DialogueBlock:
			if v.IsEmpty {
				out = append(out, v)
			}
		}
	}
	return out, nil
}

// Helper to get file name handling both separators.
func FileBase(p string) string {
	// normalize.
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	return parts[len(parts)-1]
}

// EscapeValid checks escaping roundtrip.
func EscapeValid(s string) bool {
	q := QuoteUnicode(s)
	// wrap in quotes.
	quoted := "\"" + q + "\""
	_, ok := decodeQuoted(quoted)
	if !ok {
		return false
	}
	// roundtrip.
	dec, _ := decodeQuoted(quoted)
	return dec == s
}

// ExtractTags returns multiset of tags.
func ExtractTags(s string) map[string]int {
	cur := config.TagCurlyRE.FindAllString(s, -1)
	sq := config.TagSquareRE.FindAllString(s, -1)
	// %% tokens.
	rePct := regexp.MustCompile(`%%`)
	pct := rePct.FindAllString(s, -1)
	m := make(map[string]int)
	for _, t := range cur {
		m[t]++
	}
	for _, t := range sq {
		m[t]++
	}
	for _, t := range pct {
		m[t]++
	}
	return m
}
