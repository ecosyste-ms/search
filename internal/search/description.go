package search

import (
	_ "embed"
	"regexp"
	"strings"
)

// Aliases from github.com/github/gemoji, commit fadaeaf1f1a9be82b321316a6c5502e43138b2f6.
// See emoji_aliases.LICENSE.
//
//go:embed emoji_aliases.txt
var emojiAliases string

var emojiCode = regexp.MustCompile(`:[a-z0-9_+\-]+:`)

var knownEmojiCodes = func() map[string]bool {
	codes := make(map[string]bool)
	for alias := range strings.FieldsSeq(emojiAliases) {
		codes[":"+alias+":"] = true
	}
	return codes
}()

func cleanDescription(description string) string {
	changed := false
	cleaned := emojiCode.ReplaceAllStringFunc(description, func(code string) string {
		if knownEmojiCodes[code] {
			changed = true
			return " "
		}
		return code
	})
	if !changed {
		return description
	}
	return strings.Join(strings.Fields(cleaned), " ")
}
