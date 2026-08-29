// renpy-tl fills empty new strings in RenPy translation files.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/galpt/renpy-tl/internal/adapter"
	"github.com/galpt/renpy-tl/internal/chunker"
	"github.com/galpt/renpy-tl/internal/config"
	"github.com/galpt/renpy-tl/internal/parser"
	"github.com/galpt/renpy-tl/internal/reporter"
	"github.com/galpt/renpy-tl/internal/validator"
	"github.com/galpt/renpy-tl/internal/writer"
)

func main() {
	var inputFolder string
	var outputFolder string
	var translateTo string
	var dryRun bool
	var mock bool

	flag.StringVar(&inputFolder, "input-folder", "", "input folder with .rpy files")
	flag.StringVar(&outputFolder, "output-folder", "", "output folder for translated files")
	flag.StringVar(&translateTo, "translate-to", "", "target language")
	flag.BoolVar(&dryRun, "dry-run", false, "do not write files")
	flag.BoolVar(&mock, "mock", false, "use mock translations")
	// keep legacy aliases for compatibility
	var langAlias string
	flag.StringVar(&langAlias, "lang", "", "legacy alias for translate-to")
	var tlRootAlias string
	flag.StringVar(&tlRootAlias, "tl-root", "", "legacy alias for input-folder")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s --input-folder ./input --output-folder ./output --translate-to german [--dry-run] [--mock]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if langAlias != "" && translateTo == "" {
		translateTo = langAlias
	}
	if tlRootAlias != "" && inputFolder == "" {
		inputFolder = tlRootAlias
	}

	if inputFolder == "" || outputFolder == "" || translateTo == "" {
		fmt.Fprintln(os.Stderr, "error: --input-folder, --output-folder, --translate-to are required")
		flag.Usage()
		os.Exit(2)
	}

	// normalize language to lower
	translateTo = strings.ToLower(strings.TrimSpace(translateTo))

	// fail fast on invalid language
	if !config.LangRE.MatchString(translateTo) {
		fmt.Fprintf(os.Stderr, "error: invalid language: %s\n", translateTo)
		os.Exit(2)
	}
	if strings.Contains(translateTo, "..") || strings.Contains(translateTo, "/") || strings.Contains(translateTo, "\\") {
		fmt.Fprintf(os.Stderr, "error: invalid language path: %s\n", translateTo)
		os.Exit(2)
	}
	if config.ForbiddenTargets[translateTo] {
		fmt.Fprintf(os.Stderr, "error: forbidden target: %s\n", translateTo)
		os.Exit(2)
	}

	// check input folder
	if _, err := os.Stat(inputFolder); err != nil {
		fmt.Fprintf(os.Stderr, "input folder not found: %s\n", inputFolder)
		os.Exit(2)
	}
	// ensure output folder exists
	if err := os.MkdirAll(outputFolder, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "cannot create output folder: %v\n", err)
		os.Exit(2)
	}

	// load TOML if not mock and not dry-run without key? keep strict: only TOML, no ENV
	var cfg adapter.Config
	var cfgPath string
	if !mock {
		c, p, _ := adapter.LoadTOML()
		cfg = c
		cfgPath = p
		if !dryRun && cfg.APIKey == "" {
			// require key for real run
			fmt.Fprintf(os.Stderr, "error: opencode-api-key not found in %s\n", cfgPath)
			if cfgPath == "" {
				fmt.Fprintln(os.Stderr, "hint: create renpy-tl.toml next to binary")
			}
			os.Exit(2)
		}
	}

	rep := &reporter.Reporter{}
	p := parser.New(inputFolder)

	// find empty units in input folder
	empty, err := p.FindEmptyInFolder(inputFolder)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(2)
	}

	// demo fallback like python helper: if no empty, use source blocks as demo when mock+dry-run
	if len(empty) == 0 {
		// check if input folder has any rpy at all
		all, _ := p.ParseInputFolder(inputFolder)
		hasFiles := false
		_ = filepath.Walk(inputFolder, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && filepath.Ext(path) == ".rpy" {
				hasFiles = true
			}
			return nil
		})
		if hasFiles && len(all) > 0 {
			// no empty, nothing to do
		} else {
			// try mock demo using first 10 from any files if exists
			if mock || dryRun {
				limit := 10
				if len(all) > limit {
					all = all[:limit]
				}
				// mark as empty for demo
				for i := range all {
					switch v := all[i].(type) {
					case parser.StringPair:
						v.IsEmpty = true
						v.New = ""
						all[i] = v
					case parser.DialogueBlock:
						v.IsEmpty = true
						v.New = ""
						all[i] = v
					}
				}
				empty = all[:min(len(all), 10)]
				if len(empty) > 0 {
					fmt.Fprintf(os.Stderr, "no empty found for %s, using %d source blocks as demo\n", translateTo, len(empty))
				}
			}
		}
		_ = hasFiles
	}

	rep.AddBlocks(len(empty))
	if len(empty) == 0 {
		fmt.Println("no empty blocks found")
		rep.Print()
		return
	}

	ch := chunker.New()
	chunks := ch.Chunk(empty)
	rep.AddChunk(len(chunks))
	fmt.Printf("created %d chunks from %d blocks\n", len(chunks), len(empty))
	for i, c := range chunks {
		if i >= 5 {
			break
		}
		fmt.Printf("  chunk %s pairs=%d tokens~%d lines~%d\n", c.ID, c.Len(), c.TokenEst, c.LineEst)
	}

	var ad *adapter.Adapter
	if mock {
		ad = adapter.NewMock()
	} else {
		ad = adapter.NewFromConfig(cfg)
	}

	val := &validator.Validator{}
	totalValid := make(map[string]string)

	for _, ck := range chunks {
		var raw map[string]string
		if mock {
			raw = adapter.MockTranslate(ck.Units, "TR: ")
		} else {
			// if dry-run and no key, also mock to show validation
			if ad.APIKey == "" {
				raw = adapter.MockTranslate(ck.Units, "TR: ")
			} else {
				m, err := ad.TranslateChunk(ck.Units)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					fmt.Fprintln(os.Stderr, "Please check renpy-tl.toml and try again")
					os.Exit(1)
				}
				// map to validator key style
				// ad.TranslateChunk returns keyFor -> value, validator expects file/hash\x1fold
				// convert
				tmp := make(map[string]string)
				for _, u := range ck.Units {
					k := ""
					var vk string
					switch v := u.(type) {
					case parser.StringPair:
						k = parser.FileBase(v.File) + "\x1f" + v.Old
						vk = v.File + "\x1f" + v.Old
					case parser.DialogueBlock:
						k = v.Hash + "\x1f" + v.Old
						vk = v.Hash + "\x1f" + v.Old
					}
					if val, ok := m[k]; ok {
						tmp[vk] = val
					}
				}
				raw = tmp
			}
		}
		valid := val.ValidateBatch(ck.Units, raw)
		rep.AddValid(len(valid))
		rep.AddSkipped(len(ck.Units) - len(valid))
		for k, v := range valid {
			totalValid[k] = v
		}
	}

	fmt.Printf("validated %d / %d\n", len(totalValid), len(empty))

	if dryRun {
		fmt.Println("dry-run, not writing")
		rep.Print()
		return
	}

	w, err := writer.New(inputFolder, outputFolder, translateTo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "writer error: %v\n", err)
		os.Exit(2)
	}
	written, err := w.Write(empty, totalValid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write error: %v\n", err)
		os.Exit(1)
	}
	rep.AddFiles(len(written))
	fmt.Printf("wrote %d files\n", len(written))
	for i, f := range written {
		if i >= 10 {
			break
		}
		fmt.Printf("  %s\n", f)
	}
	rep.Print()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
