package compliance

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// NormaliseResult holds the normalised prompt and any flags raised during normalisation
type NormaliseResult struct {
	Original   string
	Normalised string
	Flags      []string // descriptions of what was detected and normalised
	Suspicious bool     // true if evasion patterns were detected
}

// NormalisePrompt runs the full pre-processing pipeline on a prompt before
// it reaches the AI classifier. Detects and neutralises common evasion techniques:
//
//  1. Base64 encoded payloads
//  2. Leet speak substitutions
//  3. Excessive character insertion (h-e-l-l-o, h.e.l.l.o)
//  4. Unicode lookalike characters
//  5. Excessive whitespace / zero-width characters
//  6. Repeated character obfuscation
func NormalisePrompt(prompt string) NormaliseResult {
	result := NormaliseResult{
		Original:   prompt,
		Normalised: prompt,
		Flags:      []string{},
	}

	// Run each normalisation step in order
	result = decodeBase64Segments(result)
	result = removeZeroWidthChars(result)
	result = normaliseUnicodeLookalikes(result)
	result = normaliseCharacterInsertion(result)
	result = normaliseLeetSpeak(result)
	result = collapseExcessiveWhitespace(result)

	if len(result.Flags) > 0 {
		result.Suspicious = true
		fmt.Printf("[Normaliser] %d evasion pattern(s) detected — flags: %v\n",
			len(result.Flags), result.Flags)
		if result.Original != result.Normalised {
			fmt.Printf("[Normaliser] Original: %q\n", truncateStr(result.Original, 80))
			fmt.Printf("[Normaliser] Normalised: %q\n", truncateStr(result.Normalised, 80))
		}
	}

	return result
}

// decodeBase64Segments finds and decodes base64 strings within the prompt.
// Many evasion attempts encode the harmful payload in base64 and ask the
// model to "decode and execute" or embed it in otherwise benign text.
func decodeBase64Segments(r NormaliseResult) NormaliseResult {
	// Base64 pattern: sequences of 20+ base64 chars (shorter ones are likely
	// legitimate data like IDs, not encoded harmful content)
	b64Pattern := regexp.MustCompile(`[A-Za-z0-9+/]{20,}={0,2}`)

	found := false
	result := b64Pattern.ReplaceAllStringFunc(r.Normalised, func(match string) string {
		decoded, err := base64.StdEncoding.DecodeString(match)
		if err != nil {
			// Try URL-safe base64
			decoded, err = base64.URLEncoding.DecodeString(match)
			if err != nil {
				return match // not valid base64, leave as-is
			}
		}

		// Only replace if the decoded string is printable text
		decodedStr := string(decoded)
		if isPrintableASCII(decodedStr) && len(decodedStr) > 5 {
			found = true
			return "[base64-decoded: " + decodedStr + "]"
		}
		return match
	})

	if found {
		r.Normalised = result
		r.Flags = append(r.Flags, "base64-encoded-content")
	}
	return r
}

// removeZeroWidthChars removes invisible Unicode characters used to bypass
// keyword detection. Zero-width space (U+200B), zero-width non-joiner (U+200C),
// zero-width joiner (U+200D), and other invisible characters can be inserted
// between letters to prevent keyword matching while remaining invisible to humans.
func removeZeroWidthChars(r NormaliseResult) NormaliseResult {
	invisibleChars := []string{
		"\u200B", // zero-width space
		"\u200C", // zero-width non-joiner
		"\u200D", // zero-width joiner
		"\u2060", // word joiner
		"\uFEFF", // zero-width no-break space (BOM)
		"\u00AD", // soft hyphen
		"\u034F", // combining grapheme joiner
	}

	result := r.Normalised
	found := false
	for _, char := range invisibleChars {
		if strings.Contains(result, char) {
			result = strings.ReplaceAll(result, char, "")
			found = true
		}
	}

	if found {
		r.Normalised = result
		r.Flags = append(r.Flags, "zero-width-characters")
	}
	return r
}

// normaliseUnicodeLookalikes replaces visually similar Unicode characters with
// their ASCII equivalents. Attackers use Cyrillic а (U+0430) instead of Latin a,
// or mathematical bold letters, to bypass keyword filters.
func normaliseUnicodeLookalikes(r NormaliseResult) NormaliseResult {
	// Map of common lookalike → ASCII equivalent
	lookalikes := map[rune]rune{
		// Cyrillic lookalikes
		'а': 'a', 'е': 'e', 'о': 'o', 'р': 'p', 'с': 'c',
		'х': 'x', 'у': 'y', 'А': 'A', 'В': 'B', 'Е': 'E',
		'К': 'K', 'М': 'M', 'Н': 'H', 'О': 'O', 'Р': 'P',
		'С': 'C', 'Т': 'T', 'Х': 'X',
		// Greek lookalikes
		'α': 'a', 'β': 'b', 'ε': 'e', 'ο': 'o', 'τ': 't',
		// Mathematical variants (bold, italic, etc.)
		'𝐚': 'a', '𝐛': 'b', '𝐜': 'c', '𝐝': 'd', '𝐞': 'e',
		'𝐟': 'f', '𝐠': 'g', '𝐡': 'h', '𝐢': 'i', '𝐣': 'j',
		// Full-width variants
		'ａ': 'a', 'ｂ': 'b', 'ｃ': 'c', 'ｄ': 'd', 'ｅ': 'e',
		'ｆ': 'f', 'ｇ': 'g', 'ｈ': 'h', 'ｉ': 'i', 'ｊ': 'j',
		'ｋ': 'k', 'ｌ': 'l', 'ｍ': 'm', 'ｎ': 'n', 'ｏ': 'o',
		'ｐ': 'p', 'ｑ': 'q', 'ｒ': 'r', 'ｓ': 's', 'ｔ': 't',
		'ｕ': 'u', 'ｖ': 'v', 'ｗ': 'w', 'ｘ': 'x', 'ｙ': 'y', 'ｚ': 'z',
	}

	result := []rune(r.Normalised)
	found := false
	for i, ch := range result {
		if ascii, ok := lookalikes[ch]; ok {
			result[i] = ascii
			found = true
		}
	}

	if found {
		r.Normalised = string(result)
		r.Flags = append(r.Flags, "unicode-lookalikes")
	}
	return r
}

// normaliseCharacterInsertion removes deliberate character insertion used to
// break up keywords. Examples:
//
//	"i-g-n-o-r-e" → "ignore"
//	"i.g.n.o.r.e" → "ignore"
//	"i g n o r e" → "ignore"  (single-spaced individual letters)
func normaliseCharacterInsertion(r NormaliseResult) NormaliseResult {
	// Pattern: single letter followed by separator, repeated 3+ times
	// Covers: a-b-c-d, a.b.c.d, a b c d
	dashPattern := regexp.MustCompile(`\b([a-zA-Z][-. ]){3,}[a-zA-Z]\b`)

	found := false
	result := dashPattern.ReplaceAllStringFunc(r.Normalised, func(match string) string {
		// Remove the separators to reconstruct the word
		cleaned := regexp.MustCompile(`[-. ]`).ReplaceAllString(match, "")
		if cleaned != match {
			found = true
		}
		return cleaned
	})

	if found {
		r.Normalised = result
		r.Flags = append(r.Flags, "character-insertion-obfuscation")
	}
	return r
}

// normaliseLeetSpeak converts common leet speak substitutions back to standard
// letters. Leet speak (1337 speak) replaces letters with visually similar
// numbers or symbols to evade keyword detection.
func normaliseLeetSpeak(r NormaliseResult) NormaliseResult {
	// Only apply leet speak normalisation if we detect a meaningful concentration
	// of substitutions — avoids false positives on legitimate content like
	// "version 1.0" or "3 items"
	leetSubstitutions := map[string]string{
		"0": "o", "1": "i", "3": "e", "4": "a",
		"5": "s", "7": "t", "@": "a", "$": "s",
		"|": "i", "!": "i",
	}

	// Count how many substitutions are present
	count := 0
	for leet := range leetSubstitutions {
		count += strings.Count(r.Normalised, leet)
	}

	// Only normalise if there's a meaningful density of leet characters
	// relative to the total length — avoids false positives
	totalLen := len(r.Normalised)
	if totalLen == 0 || float64(count)/float64(totalLen) < 0.08 {
		return r
	}

	result := r.Normalised
	for leet, normal := range leetSubstitutions {
		result = strings.ReplaceAll(result, leet, normal)
	}

	if result != r.Normalised {
		r.Normalised = result
		r.Flags = append(r.Flags, "leet-speak")
	}
	return r
}

// collapseExcessiveWhitespace normalises whitespace — multiple spaces, tabs,
// and newlines collapsed to single spaces. Excess whitespace is sometimes
// used to push content past context window limits or confuse tokenisers.
func collapseExcessiveWhitespace(r NormaliseResult) NormaliseResult {
	wsPattern := regexp.MustCompile(`\s{2,}`)
	result := wsPattern.ReplaceAllString(strings.TrimSpace(r.Normalised), " ")

	if result != r.Normalised {
		r.Normalised = result
		r.Flags = append(r.Flags, "excessive-whitespace")
	}
	return r
}

// isPrintableASCII returns true if all characters in s are printable ASCII
func isPrintableASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// truncateStr shortens a string to maxLen for log output
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
