package patterns

import "unicode/utf8"

var HiddenCharacters = Category{
	Name:        "hidden_characters",
	Description: "Contains invisible or misleading Unicode characters",
	Patterns:    nil, // not pattern-based — uses custom detection
}

// HiddenCharRunes are Unicode codepoints that are invisible or misleading in code.
var HiddenCharRunes = []struct {
	Char rune
	Name string
}{
	{'\u200B', "zero-width space"},
	{'\u200C', "zero-width non-joiner"},
	{'\u200D', "zero-width joiner"},
	{'\u200E', "left-to-right mark"},
	{'\u200F', "right-to-left mark"},
	{'\u2060', "word joiner"},
	{'\u2061', "function application"},
	{'\u2062', "invisible times"},
	{'\u2063', "invisible separator"},
	{'\u2064', "invisible plus"},
	{'\uFEFF', "byte order mark"},
	{'\u00AD', "soft hyphen"},
	{'\u034F', "combining grapheme joiner"},
	{'\u061C', "arabic letter mark"},
	{'\u202A', "left-to-right embedding"},
	{'\u202B', "right-to-left embedding"},
	{'\u202C', "pop directional formatting"},
	{'\u202D', "left-to-right override"},
	{'\u202E', "right-to-left override"},  // Trojan Source attack
	{'\u2066', "left-to-right isolate"},
	{'\u2067', "right-to-left isolate"},
	{'\u2068', "first strong isolate"},
	{'\u2069', "pop directional isolate"},
	// Variation selectors
	{'\uFE00', "variation selector 1"},
	{'\uFE01', "variation selector 2"},
	{'\uFE0F', "variation selector 16 (emoji)"},
	{'\uFE0E', "variation selector 15 (text)"},
}

// confusableMap is a precomputed lookup for NormalizeConfusables.
var confusableMap map[rune]string

func init() {
	confusableMap = make(map[rune]string, len(ConfusableChars))
	for _, c := range ConfusableChars {
		confusableMap[c.Char] = c.LooksLike
	}
}

// NormalizeConfusables replaces all known confusable Unicode characters with
// their ASCII equivalents. This allows pattern matching to catch homoglyph
// attacks like Cyrillic "е" in "os/еxec" → "os/exec".
func NormalizeConfusables(s string) string {
	var changed bool
	for _, r := range s {
		if _, ok := confusableMap[r]; ok {
			changed = true
			break
		}
	}
	if !changed {
		return s // fast path: no confusables found
	}

	var b []byte
	for _, r := range s {
		if ascii, ok := confusableMap[r]; ok {
			b = append(b, ascii...)
		} else {
			var buf [utf8.UTFMax]byte
			n := utf8.EncodeRune(buf[:], r)
			b = append(b, buf[:n]...)
		}
	}
	return string(b)
}

// ConfusableChars maps deceptive Unicode characters to the ASCII they impersonate.
// These look like common programming characters but aren't, allowing pattern bypass.
var ConfusableChars = []struct {
	Char        rune
	LooksLike   string
	Name        string
}{
	// Confusable letters
	{'\u0410', "A", "cyrillic A"},
	{'\u0412', "B", "cyrillic B"},
	{'\u0421', "C", "cyrillic C"},
	{'\u0415', "E", "cyrillic E"},
	{'\u041D', "H", "cyrillic H"},
	{'\u041A', "K", "cyrillic K"},
	{'\u041C', "M", "cyrillic M"},
	{'\u041E', "O", "cyrillic O"},
	{'\u0420', "P", "cyrillic P"},
	{'\u0422', "T", "cyrillic T"},
	{'\u0425', "X", "cyrillic X"},
	{'\u0430', "a", "cyrillic a"},
	{'\u0435', "e", "cyrillic e"},
	{'\u043E', "o", "cyrillic o"},
	{'\u0440', "p", "cyrillic p"},
	{'\u0441', "c", "cyrillic c"},
	{'\u0443', "y", "cyrillic y"},
	{'\u0445', "x", "cyrillic x"},
	{'\u0455', "s", "cyrillic s"},
	{'\u0456', "i", "cyrillic i"},
	// Confusable punctuation
	{'\uFF0E', ".", "fullwidth period"},
	{'\uFF08', "(", "fullwidth left paren"},
	{'\uFF09', ")", "fullwidth right paren"},
	{'\uFF1B', ";", "fullwidth semicolon"},
	{'\uFF1A', ":", "fullwidth colon"},
	{'\uFF0F', "/", "fullwidth slash"},
	{'\u2044', "/", "fraction slash"},
	{'\uFF3F', "_", "fullwidth underscore"},
	{'\uFF0A', "*", "fullwidth asterisk"},
	// Confusable quotes
	{'\u201C', "\"", "left double quotation"},
	{'\u201D', "\"", "right double quotation"},
	{'\u2018', "'", "left single quotation"},
	{'\u2019', "'", "right single quotation"},
	// Greek lowercase (lookalikes for Latin)
	{'\u03B1', "a", "greek alpha"},   // α → a
	{'\u03BF', "o", "greek omicron"}, // ο → o
	{'\u03B5', "e", "greek epsilon"}, // ε → e (approximate)
	{'\u03B9', "i", "greek iota"},    // ι → i
	{'\u03BA', "k", "greek kappa"},   // κ → k (approximate)
	{'\u03BD', "v", "greek nu"},      // ν → v
	{'\u03C1', "p", "greek rho"},     // ρ → p
	{'\u03C5', "u", "greek upsilon"}, // υ → u (approximate)
	// Homoglyph digits
	{'\u01B5', "2", "latin Z with stroke (looks like 2)"},
	{'\u03F3', "j", "greek yot"},
	{'\u1D00', "a", "latin small capital A"},
	{'\uA731', "s", "latin small capital S"},
}
