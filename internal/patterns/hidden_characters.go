package patterns

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
}
