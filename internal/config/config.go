// Package config holds shared constants.
package config

import "regexp"

const (
	ChunkMaxPairs  = 500
	ChunkMaxLines  = 1500
	ChunkMaxTokens = 80000
	CharsPerToken  = 4

	MaxFileBytes   = 2 * 1024 * 1024
	MaxStringBytes = 4 * 1024

	OpenCodeBaseURL = "https://opencode.ai/zen/go/v1"
	OpenCodeModel   = "muse-spark-1.2-contributor"
)

// compiled patterns, keep strict parity with Python helper.
var (
	HashRE      = regexp.MustCompile(`_[0-9a-f]{20}_[0-9a-f]{24}(?:_\d+)?`)
	TranslateRE = regexp.MustCompile(`^translate\s+(?P<lang>[a-zA-Z0-9_]+)\s+(?P<ident>.+?)\s*:\s*$`)
	LangRE      = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)

	TagCurlyRE  = regexp.MustCompile(`\{[^\}]+\}`)
	TagSquareRE = regexp.MustCompile(`\[[^\]]+\]`)

	// TOML keys expected.
	TOMLModelKey = "ai-model"
	TOMLKeyKey   = "opencode-api-key"
)

// forbidden targets that would overwrite source.
var ForbiddenTargets = map[string]bool{
	"piglatin":  true,
	"pig_latin": true,
	"rot13":     true,
	"rot_13":    true,
}
