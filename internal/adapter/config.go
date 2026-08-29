package adapter

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// LoadTOML reads renpy-tl.toml next to binary or cwd.
// Keys exactly: ai-model and opencode-api-key, no ENV fallback.
func LoadTOML() (Config, string, error) {
	candidates := []string{}
	// binary dir
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "renpy-tl.toml"))
	}
	// cwd
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "renpy-tl.toml"))
	}
	// also try relative to binary's real path
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			cfg, err := parseTOML(p)
			return cfg, p, err
		}
	}
	return Config{}, "", nil
}

func parseTOML(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	var cfg Config
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// split on = (first)
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// strip quotes
		val = strings.Trim(val, `"`)
		val = strings.Trim(val, `'`)
		switch key {
		case "ai-model":
			cfg.Model = val
		case "opencode-api-key":
			cfg.APIKey = val
		}
	}
	return cfg, sc.Err()
}
