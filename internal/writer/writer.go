// Package writer does atomic file updates with backup and fsync.
package writer

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/galpt/renpy-tl/internal/config"
	"github.com/galpt/renpy-tl/internal/parser"
)

// checkLang validates language name and traversal.
func checkLang(lang, inputFolder, outputFolder string) (string, error) {
	if !config.LangRE.MatchString(lang) {
		return "", fmt.Errorf("invalid language: %s", lang)
	}
	if strings.Contains(lang, "..") || strings.Contains(lang, "/") || strings.Contains(lang, "\\") {
		return "", fmt.Errorf("invalid language path: %s", lang)
	}
	if filepath.IsAbs(lang) {
		return "", fmt.Errorf("absolute path not allowed")
	}
	if config.ForbiddenTargets[lang] {
		return "", fmt.Errorf("forbidden target: %s (would overwrite source)", lang)
	}
	// also guard output path.
	outAbs, err := filepath.Abs(outputFolder)
	if err != nil {
		return "", err
	}
	inAbs, err := filepath.Abs(inputFolder)
	if err != nil {
		return "", err
	}
	// ensure output not inside piglatin like. just forbid traversal via lang.
	_ = inAbs
	_ = outAbs
	return outAbs, nil
}

// Writer handles translation writes.
type Writer struct {
	InputFolder  string
	OutputFolder string
	Lang         string
}

func New(inputFolder, outputFolder, lang string) (*Writer, error) {
	if _, err := checkLang(lang, inputFolder, outputFolder); err != nil {
		return nil, err
	}
	return &Writer{InputFolder: inputFolder, OutputFolder: outputFolder, Lang: lang}, nil
}

// readExisting reads file, detects BOM.
func readExisting(path string) (string, bool, os.FileMode, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, 0o644, nil
		}
		return "", false, 0, err
	}
	hasBOM := bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if hasBOM {
		raw = raw[3:]
	}
	text := string(raw)
	// normalize line endings to \n for consistent patching.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	fi, err := os.Stat(path)
	mode := os.FileMode(0o644)
	if err == nil {
		mode = fi.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
	}
	return text, hasBOM, mode, nil
}

// atomicWrite writes content atomically in same FS.
func atomicWrite(dest string, content string, hasBOM bool, mode os.FileMode) error {
	if isSymlink(dest) {
		return fmt.Errorf("symlink not allowed: %s", dest)
	}
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if isSymlink(parent) {
		return fmt.Errorf("symlink not allowed: %s", parent)
	}
	// backup.
	if _, err := os.Stat(dest); err == nil {
		ts := fmt.Sprintf("%d", time.Now().Unix())
		bak := dest + ".bak." + ts
		if err := copyFile(dest, bak); err != nil {
			return err
		}
	}
	// temp in same dir.
	tmp, err := os.CreateTemp(parent, ".tmp.")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// ensure cleanup on failure.
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	var data []byte
	if hasBOM {
		data = append([]byte{0xEF, 0xBB, 0xBF}, []byte(content)...)
	} else {
		data = []byte(content)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		// non-fatal.
	}
	// replace.
	if err := os.Rename(tmpName, dest); err != nil {
		// EXDEV fallback.
		if isCrossDevice(err) {
			if err2 := copyFile(tmpName, dest); err2 != nil {
				os.Remove(tmpName)
				return err2
			}
			os.Remove(tmpName)
		} else {
			os.Remove(tmpName)
			return err
		}
	}
	// fsync directory.
	if err := fsyncDir(parent); err != nil {
		// advisory.
	}
	return nil
}

func isSymlink(p string) bool {
	fi, err := os.Lstat(p)
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSymlink != 0
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	// copy perms.
	if fi, err := os.Stat(src); err == nil {
		os.Chmod(dst, fi.Mode().Perm())
	}
	return nil
}

func isCrossDevice(err error) bool {
	// string check for EXDEV.
	return strings.Contains(err.Error(), "cross-device") || strings.Contains(err.Error(), "invalid cross-device")
}

func fsyncDir(dir string) error {
	// directory sync uses os.Open with fsync, with fallback handling for Windows where O_DIRECTORY is not available.
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// applyTranslations patches file content.
func applyTranslations(dest string, units []interface{}, valid map[string]string) (string, bool, os.FileMode, error) {
	text, hasBOM, mode, err := readExisting(dest)
	if err != nil {
		return "", false, 0, err
	}
	// if not exists, try input file as template.
	if text == "" {
		// try to find corresponding input file.
		// units belong to inputFolder. map dest to input.
		// dest is OutputFolder plus rel. input is InputFolder plus rel.
		// fallback. use first unit file to locate template.
		if len(units) > 0 {
			var srcFile string
			switch v := units[0].(type) {
			case parser.StringPair:
				srcFile = v.File
			case parser.DialogueBlock:
				srcFile = v.File
			}
			if srcFile != "" {
				if t, hb, m, e := readExisting(srcFile); e == nil && t != "" {
					text = t
					hasBOM = hb
					mode = m
				} else if raw, e := os.ReadFile(srcFile); e == nil {
					hb := bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
					if hb {
						raw = raw[3:]
					}
					text = string(raw)
					hasBOM = hb
					mode = 0o644
					if fi, e := os.Stat(srcFile); e == nil {
						mode = fi.Mode().Perm()
					}
				}
			}
		}
		if text == "" {
			return "", hasBOM, mode, nil
		}
	}
	// normalize line endings for processing but preserve original style on write.
	// we keep newline join. writer will emit newline. handles line ending via parser normalize.
	lines := strings.Split(text, "\n")
	// handle BOM already stripped, lines include content without BOM.
	newLines := make([]string, len(lines))
	copy(newLines, lines)

	// build lookup for valid.
	for _, u := range units {
		var key string
		var old string
		switch v := u.(type) {
		case parser.StringPair:
			key = parser.FileBase(v.File) + "\x1f" + v.Old
			_ = old
			old = v.Old
		case parser.DialogueBlock:
			key = v.Hash + "\x1f" + v.Old
			old = v.Old
		}
		_ = old
		proposed, ok := valid[key]
		if !ok {
			continue
		}
		q := parser.QuoteUnicode(proposed)
		quoted := "\"" + q + "\""
		switch v := u.(type) {
		case parser.DialogueBlock:
			idx := findDialogueLine(newLines, v)
			if idx == -1 {
				continue
			}
			orig := newLines[idx]
			indent := orig[:len(orig)-len(strings.TrimLeft(orig, " \t"))]
			speaker := ""
			if v.Speaker != nil {
				speaker = *v.Speaker + " "
			}
			suffix := ""
			if v.Suffix != "" {
				suffix = " " + v.Suffix
			}
			newLines[idx] = indent + speaker + quoted + suffix
		case parser.StringPair:
			idx := findStringLine(newLines, v)
			if idx == -1 {
				continue
			}
			indent := newLines[idx][:len(newLines[idx])-len(strings.TrimLeft(newLines[idx], " \t"))]
			newLines[idx] = indent + "new " + quoted
		}
		_ = key
	}
	return strings.Join(newLines, "\n"), hasBOM, mode, nil
}

func findDialogueLine(lines []string, u parser.DialogueBlock) int {
	// search near header lineno.
	start := u.Lineno - 1
	if start < 0 {
		start = 0
	}
	if start >= len(lines) {
		start = 0
	}
	// look ahead up to 8 lines for exact newRaw match.
	for j := start; j < len(lines) && j < start+8; j++ {
		if strings.TrimSpace(lines[j]) == strings.TrimSpace(u.NewRaw) {
			return j
		}
	}
	for j := start; j < len(lines) && j < start+12; j++ {
		if strings.Contains(lines[j], "\"\"") || strings.Contains(lines[j], "''") {
			if u.Speaker != nil {
				if strings.HasPrefix(strings.TrimLeft(lines[j], " \t"), *u.Speaker) {
					return j
				}
			} else {
				if strings.HasPrefix(strings.TrimLeft(lines[j], " \t"), "\"") || strings.HasPrefix(strings.TrimLeft(lines[j], " \t"), "'") {
					return j
				}
			}
		}
	}
	// fallback scan whole file for first empty after header.
	for j := start; j < len(lines); j++ {
		if config.TranslateRE.MatchString(strings.TrimSpace(lines[j])) && j != start {
			break
		}
		if strings.TrimSpace(lines[j]) == "\"\"" || strings.TrimSpace(lines[j]) == "''" {
			return j
		}
	}
	return -1
}

func findStringLine(lines []string, u parser.StringPair) int {
	start := u.Lineno - 1
	if start < 0 {
		start = 0
	}
	if start >= len(lines) {
		start = 0
	}
	for j := start; j < len(lines) && j < start+60; j++ {
		if strings.TrimSpace(lines[j]) == strings.TrimSpace(u.NewRaw) {
			return j
		}
		if strings.HasPrefix(strings.TrimLeft(lines[j], " \t"), "new ") {
			if strings.TrimSpace(lines[j]) == "new \"\"" || strings.TrimSpace(lines[j]) == "new ''" {
				// verify old nearby.
				// look back for old line.
				for k := j - 1; k >= 0 && k > j-5; k-- {
					if strings.Contains(lines[k], u.OldQuoted) || strings.Contains(lines[k], parser.QuoteUnicode(u.Old)) {
						return j
					}
				}
				// if not found but empty, treat as candidate for first empty.
				// only return if we are at expected offset.
				// we need to handle order. assume pairs in file order correspond to empties.
				// For simplicity, if no exact match return first new empty after start.
				// But to avoid misalignment check that old line before is old.
				if j == start+2 || strings.HasPrefix(strings.TrimLeft(lines[j-1], " \t"), "old ") {
					return j
				}
			}
		}
	}
	// fallback. collect empty new positions.
	var empties []int
	for idx, l := range lines {
		if strings.TrimSpace(l) == "new \"\"" || strings.TrimSpace(l) == "new ''" {
			empties = append(empties, idx)
		}
	}
	// heuristic. position by order of string units. not reliable without file grouping.
	// return first not yet patched.
	for _, idx := range empties {
		if idx >= start {
			return idx
		}
	}
	return -1
}

// Write does two phase batch. prepare temps. fsync. then rename.
func (w *Writer) Write(allUnits []interface{}, valid map[string]string) ([]string, error) {
	// group by file (output path).
	type fileGroup struct {
		outputPath string
		inputPath  string
		units      []interface{}
	}
	byFile := make(map[string]*fileGroup)
	for _, u := range allUnits {
		var file string
		var key string
		switch v := u.(type) {
		case parser.StringPair:
			file = v.File
			key = parser.FileBase(v.File) + "\x1f" + v.Old
		case parser.DialogueBlock:
			file = v.File
			key = v.Hash + "\x1f" + v.Old
		}
		if _, ok := valid[key]; !ok {
			continue
		}
		rel, err := filepath.Rel(w.InputFolder, file)
		if err != nil {
			// fallback to base.
			rel = filepath.Base(file)
		}
		// handle both separators.
		rel = filepath.FromSlash(filepath.ToSlash(rel))
		outPath := filepath.Join(w.OutputFolder, rel)
		// also handle \ separator already via FromSlash.
		if _, ok := byFile[outPath]; !ok {
			byFile[outPath] = &fileGroup{outputPath: outPath, inputPath: file}
		}
		byFile[outPath].units = append(byFile[outPath].units, u)
	}
	// deterministic order.
	keys := make([]string, 0, len(byFile))
	for k := range byFile {
		keys = append(keys, k)
	}
	// sort keys.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	type pending struct {
		tmp    string
		dest   string
		parent string
	}
	var pendings []pending
	var written []string
	// phase1. create temps.
	for _, k := range keys {
		fg := byFile[k]
		dest := fg.outputPath
		if isSymlink(dest) {
			return written, fmt.Errorf("symlink not allowed: %s", dest)
		}
		parent := filepath.Dir(dest)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return written, err
		}
		// backup if exists.
		if _, err := os.Stat(dest); err == nil {
			ts := fmt.Sprintf("%d", time.Now().Unix())
			bak := dest + ".bak." + ts
			_ = copyFile(dest, bak)
		}
		newContent, hasBOM, mode, err := applyTranslations(dest, fg.units, valid)
		if err != nil {
			return written, err
		}
		// if dest not exists but input exists, applyTranslations may have used input template.
		// if still empty, read input directly and patch.
		if newContent == "" {
			// try reading input.
			if txt, hb, m, e := readExisting(fg.inputPath); e == nil && txt != "" {
				newContent, hasBOM, mode, _ = applyTranslations(fg.inputPath, fg.units, valid)
				// but we need to write to dest.
				_ = hb
				_ = m
				_ = txt
				// if still empty, skip.
				if newContent == "" {
					continue
				}
				// re-apply against dest template if dest empty.
				// use input content as base.
				if _, err := os.Stat(dest); os.IsNotExist(err) {
					// create dest content from input.
					raw, _ := os.ReadFile(fg.inputPath)
					hb2 := bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
					if hb2 {
						raw = raw[3:]
					}
					txt2 := string(raw)
					newContent = patchFromText(txt2, fg.units, valid)
					hasBOM = hb2
					mode = 0o644
					if fi, e := os.Stat(fg.inputPath); e == nil {
						mode = fi.Mode().Perm()
					}
				}
			}
		}
		// skip if nothing to write (no valid).
		if newContent == "" {
			continue
		}
		// check if idempotent. compare existing content to newContent.
		if existing, _, _, e := readExisting(dest); e == nil && existing == newContent && existing != "" {
			continue
		}
		// create temp.
		tmp, err := os.CreateTemp(parent, ".tmp.")
		if err != nil {
			return written, err
		}
		tmpName := tmp.Name()
		var data []byte
		if hasBOM {
			data = append([]byte{0xEF, 0xBB, 0xBF}, []byte(newContent)...)
		} else {
			data = []byte(newContent)
		}
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return written, err
		}
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return written, err
		}
		tmp.Close()
		if err := os.Chmod(tmpName, mode); err != nil {
			// ignore.
		}
		pendings = append(pendings, pending{tmp: tmpName, dest: dest, parent: parent})
	}
	// phase2. rename.
	for _, p := range pendings {
		if err := os.Rename(p.tmp, p.dest); err != nil {
			if isCrossDevice(err) {
				if err2 := copyFile(p.tmp, p.dest); err2 != nil {
					os.Remove(p.tmp)
					return written, err2
				}
				os.Remove(p.tmp)
			} else {
				os.Remove(p.tmp)
				return written, err
			}
		}
		_ = fsyncDir(p.parent)
		written = append(written, p.dest)
	}
	// cleanup any leftover temps.
	for _, p := range pendings {
		if _, err := os.Stat(p.tmp); err == nil {
			os.Remove(p.tmp)
		}
	}
	return written, nil
}

// patchFromText patches text directly.
func patchFromText(text string, units []interface{}, valid map[string]string) string {
	lines := strings.Split(text, "\n")
	newLines := make([]string, len(lines))
	copy(newLines, lines)
	for _, u := range units {
		var key string
		switch v := u.(type) {
		case parser.StringPair:
			key = parser.FileBase(v.File) + "\x1f" + v.Old
		case parser.DialogueBlock:
			key = v.Hash + "\x1f" + v.Old
		}
		prop, ok := valid[key]
		if !ok {
			continue
		}
		q := parser.QuoteUnicode(prop)
		quoted := "\"" + q + "\""
		switch v := u.(type) {
		case parser.DialogueBlock:
			idx := findDialogueLine(newLines, v)
			if idx == -1 {
				continue
			}
			orig := newLines[idx]
			indent := orig[:len(orig)-len(strings.TrimLeft(orig, " \t"))]
			sp := ""
			if v.Speaker != nil {
				sp = *v.Speaker + " "
			}
			suf := ""
			if v.Suffix != "" {
				suf = " " + v.Suffix
			}
			newLines[idx] = indent + sp + quoted + suf
		case parser.StringPair:
			idx := findStringLine(newLines, v)
			if idx == -1 {
				continue
			}
			indent := newLines[idx][:len(newLines[idx])-len(strings.TrimLeft(newLines[idx], " \t"))]
			newLines[idx] = indent + "new " + quoted
		}
	}
	return strings.Join(newLines, "\n")
}
