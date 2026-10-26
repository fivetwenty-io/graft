package regex

// rules holds what a flavor decides where PCRE2 and jq's Oniguruma read the
// same syntax differently. The parser, the class builder, and the compiler
// consult the table rather than testing the flavor, so each difference is
// named once, here.
type rules struct {
	// unicodeClasses makes \d, \w, \s, \b, and the POSIX classes
	// Unicode-aware. Without it they are ASCII, as in PCRE2 without UCP.
	unicodeClasses bool

	// spaceEscapes reads \h and \v as horizontal and vertical space, and
	// their capitals as the complements. Without it the four are letters.
	spaceEscapes bool

	// codePointU reads \uhhhh as a code point. Without it \u is the letter.
	codePointU bool

	// letterEscapes reads a backslash before a letter that names nothing as
	// the letter itself, as Oniguruma does, where PCRE2 refuses it. It also
	// makes \k and \g letters when no reference follows them, \p and \P
	// letters when no brace follows them, and a \E that ends no quote the
	// letter E.
	letterEscapes bool

	// byteEscapes reads \xhh and the octal escapes as bytes of the
	// pattern's UTF-8 encoding, as jq's Oniguruma does, so a run of them
	// spells one character. Without it each names a code point.
	byteEscapes bool

	// classQuote reads \Q...\E inside a bracket class as a quote. Without it
	// \Q and \E there are the letters Q and E.
	classQuote bool

	// asciiExtended limits the whitespace that Extended skips to the ASCII
	// space, tab, newline, carriage return, and form feed, as Oniguruma
	// does. Without it Extended skips PCRE2's list for UTF mode.
	asciiExtended bool

	// maxRepeat is the largest count a {} quantifier accepts.
	maxRepeat int

	// openLowerBound reads {,n} as a quantifier, as PCRE2 does. Without it
	// {,n} is literal text.
	openLowerBound bool

	// stackedQuantifiers lets a quantifier follow a quantifier, repeating
	// the repetition, so a{2}{3} is six a's and a** is a*. Without it the
	// second quantifier is a syntax error.
	stackedQuantifiers bool

	// quantifiedLookaround lets a quantifier follow a lookaround, as PCRE2
	// does. Without it the quantifier is a syntax error.
	quantifiedLookaround bool

	// pythonNames accepts (?P<name>...) and (?P=name).
	pythonNames bool

	// duplicateNames lets several groups share a name. A reference to the
	// name tries the last of them first and then each earlier one.
	duplicateNames bool

	// forwardNames lets a reference name a group that the pattern defines
	// after it.
	forwardNames bool

	// pcreReferences accepts \k{name} and the \g forms of a reference.
	pcreReferences bool

	// relativeK reads a number in \k<...> or \k'...' as a group number,
	// counting back from the reference when it has a minus sign and
	// forward when it has a plus.
	relativeK bool

	// literalHighDigits reads \8 or \9 at the start of a number above the
	// group count as the digit itself, as Oniguruma does. Without it the
	// number is a reference to a group that does not exist.
	literalHighDigits bool

	// boundedLookbehind applies PCRE2's rule that every branch of a
	// lookbehind has a length limit and that a branch whose length varies
	// looks back no more than 255 runes. Without it a lookbehind may look
	// back without limit.
	boundedLookbehind bool

	// posixProperties accepts the POSIX class names, such as Alpha and
	// XDigit, and the long names of the general categories, such as
	// Lowercase_Letter, inside \p{...}, with the meanings Oniguruma gives
	// them.
	posixProperties bool

	// foldBareProperty folds \p{Lu}, \p{Ll}, and \p{Lt} outside a class
	// under IgnoreCase, as PCRE2 does.
	foldBareProperty bool

	// rangeAfterSet reads a hyphen after a class escape, a property, or a
	// POSIX name inside a bracket class as a literal hyphen, as Oniguruma
	// does, where PCRE2 refuses it as an invalid range.
	rangeAfterSet bool

	// barePosixOutside reads [:alpha:] outside a bracket class as the
	// ordinary class of its runes, as Oniguruma does, where PCRE2 refuses
	// it.
	barePosixOutside bool

	// foldClassItems folds every item of a bracket class under IgnoreCase,
	// escapes, properties, and POSIX names included, and applies full
	// case folding, so ss matches ß and ß matches ss. Without it a class
	// folds only its literals and ranges, one code point to one.
	foldClassItems bool

	// fullCaseFold applies full case folding to literals under IgnoreCase.
	fullCaseFold bool

	// openSelfRef makes a reference inside the group it names fail while
	// the group is open, as Oniguruma does. Without it the reference sees
	// the group's last complete capture, as PCRE2's does.
	openSelfRef bool

	// anyRuneO reads \O as any rune, a newline included.
	anyRuneO bool

	// onigMessages words the errors that the two engines word differently
	// the way Oniguruma does.
	onigMessages bool
}

// flavorRules gives each flavor its rules.
var flavorRules = [...]rules{
	PCRE: {
		spaceEscapes:         true,
		codePointU:           true,
		classQuote:           true,
		maxRepeat:            65535,
		openLowerBound:       true,
		quantifiedLookaround: true,
		pythonNames:          true,
		forwardNames:         true,
		pcreReferences:       true,
		boundedLookbehind:    true,
		foldBareProperty:     true,
	},
	Oniguruma: {
		unicodeClasses:     true,
		letterEscapes:      true,
		byteEscapes:        true,
		asciiExtended:      true,
		maxRepeat:          100000,
		stackedQuantifiers: true,
		duplicateNames:     true,
		relativeK:          true,
		literalHighDigits:  true,
		posixProperties:    true,
		rangeAfterSet:      true,
		barePosixOutside:   true,
		foldClassItems:     true,
		fullCaseFold:       true,
		openSelfRef:        true,
		anyRuneO:           true,
		onigMessages:       true,
	},
}

// rules returns the flavor's rules, the PCRE rules for a flavor that is
// not defined.
func (f Flavor) rules() *rules {
	if int(f) < len(flavorRules) {
		return &flavorRules[f]
	}
	return &flavorRules[PCRE]
}
