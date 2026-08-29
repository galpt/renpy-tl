// Package validator checks 9 invariants, fail closed.
package validator

import (
	"strings"

	"github.com/galpt/renpy-tl/internal/parser"
)

// Validator is pure, no side effects.
type Validator struct{}

// ValidateBlock checks invariants for a single unit.
func (v *Validator) ValidateBlock(original interface{}, proposed string) (bool, string) {
	// invariant 3. only empty may be filled.
	var isEmpty bool
	var old string
	switch o := original.(type) {
	case parser.StringPair:
		isEmpty = o.IsEmpty
		old = o.Old
	case parser.DialogueBlock:
		isEmpty = o.IsEmpty
		old = o.Old
	case *parser.StringPair:
		isEmpty = o.IsEmpty
		old = o.Old
	case *parser.DialogueBlock:
		isEmpty = o.IsEmpty
		old = o.Old
	default:
		return false, "unknown type"
	}
	_ = old
	if !isEmpty {
		return false, "only empty new may be filled"
	}
	if proposed == "" {
		return false, "proposed empty"
	}
	// invariant 5. escaping valid.
	if !parser.EscapeValid(proposed) {
		return false, "escaping invalid"
	}
	if !parser.EscapeValid(old) {
		return false, "old escaping invalid"
	}
	// invariant 6. tags preserved exact multiset.
	oldTags := parser.ExtractTags(old)
	newTags := parser.ExtractTags(proposed)
	if !equalTagMap(oldTags, newTags) {
		return false, "tags mismatch"
	}
	// invariant 8. newline count preserved.
	if strings.Count(old, "\n") != strings.Count(proposed, "\n") {
		return false, "newline count mismatch"
	}
	// invariant 9. parseability already via EscapeValid.
	// invariant 1 2 4 7 are structural and handled by caller mapping.
	return true, "ok"
}

func equalTagMap(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// ValidateBatch filters proposed map to valid entries.
// key is (hash,old) for dialogue, (file,old) for strings encoded as string with \x1f separator.
func (v *Validator) ValidateBatch(units []interface{}, proposed map[string]string) map[string]string {
	valid := make(map[string]string)
	for _, u := range units {
		var key string
		switch o := u.(type) {
		case parser.StringPair:
			key = parser.FileBase(o.File) + "\x1f" + o.Old
		case parser.DialogueBlock:
			key = o.Hash + "\x1f" + o.Old
		case *parser.StringPair:
			key = parser.FileBase(o.File) + "\x1f" + o.Old
		case *parser.DialogueBlock:
			key = o.Hash + "\x1f" + o.Old
		}
		prop, ok := proposed[key]
		if !ok {
			continue
		}
		if ok2, _ := v.ValidateBlock(u, prop); ok2 {
			valid[key] = prop
		}
	}
	return valid
}

// CheckAtomicity validates block counts.
func (v *Validator) CheckAtomicity(origCount, newCount int) bool { return origCount == newCount }

// CheckHashUnchanged validates hash.
func (v *Validator) CheckHashUnchanged(a, b string) bool { return a == b }

// CheckOldUnchanged validates old.
func (v *Validator) CheckOldUnchanged(a, b string) bool { return a == b }
