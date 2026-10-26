package yq

import "testing"

func TestYQIntegersWidenExactly(t *testing.T) {
	// Exact decimals widen past int64, where yq 4.54.1 reports an overflow.
	wantYQ(t, `. + 1`, "9223372036854775807\n", "9223372036854775808\n")
	// "/" always gives !!float, and the rest give !!int for two !!int operands.
	wantYQ(t, `(. + 1 | tag), (. * 1.5 | tag), (. / 4 | tag), (. / 2 | tag), (. % 2 | tag), (5.5 % 2 | tag), (3 - 1 | tag)`, "2\n", "!!int\n!!float\n!!float\n!!float\n!!int\n!!float\n!!int\n")
	// A !!float result that is a whole number prints without a point, as yq prints it.
	wantYQ(t, `.a = 10 / 2 | .b = 2 * 3.0`, "{}\n", "a: 5\nb: 6\n")
	wantYQ(t, `. * 1.5, (0.1 + 0.2), (1 / 3)`, "2\n", "3\n0.3\n0.3333333333333333333333333333333333\n")
	wantYQError(t, `1 % 0`, "null\n", "cannot modulo by 0")
}

func TestYQFloatNumbers(t *testing.T) {
	// --float-numbers restores yq's int64 and float64 model.
	opts := testOpts
	opts.FloatNumbers = true
	wantYQOpts(t, opts, `1 / 2`, "null\n", "0.5\n")
	wantYQOpts(t, opts, `0.1 + 0.2`, "null\n", "0.30000000000000004\n")
	wantYQErrorOpts(t, opts, `. + 1`, "9223372036854775807\n", "9223372036854775807 + 1 overflows int64")
	// A float past float64's range fails to parse in yq's model.
	wantYQErrorOpts(t, opts, `1e1000 + 0`, "null\n", `strconv.ParseFloat: parsing "1e1000": value out of range`)
	wantYQErrorOpts(t, opts, `2.5 * 1.5e400`, "null\n", `strconv.ParseFloat: parsing "1.5e400": value out of range`)
}

func TestYQArithmeticRunsLeftFirst(t *testing.T) {
	wantYQ(t, `[(1, 2) + (10, 20)]`, "null\n", "- 11\n- 21\n- 12\n- 22\n")
	wantYQ(t, `[(1, 2) - (10, 20)]`, "null\n", "- -9\n- -19\n- -8\n- -18\n")
	wantYQ(t, `[(10, 20) % (3, 7)]`, "null\n", "- 1\n- 3\n- 2\n- 6\n")
	wantYQ(t, `[("a", "b") + ("c", "d")]`, "null\n", "- ac\n- ad\n- bc\n- bd\n")
	wantYQ(t, `[(1, 3) < (2, 4)]`, "null\n", "- true\n- true\n- false\n- true\n")
}

func TestYQAddingOtherNodes(t *testing.T) {
	wantYQ(t, `"3" + 1, (1 + "3"), ("a" + null), (null + "a"), ("a" + true), (true + "a")`, "null\n", "31\n13\na\na\natrue\ntruea\n")
	wantYQ(t, `"3" + 1 | tag`, "null\n", "!!str\n")
	wantYQ(t, `null + 1 | tag`, "null\n", "!!int\n")
	wantYQ(t, `[1] + 2, ([1] + {"a": 1}), ({"a": 1} + {"b": 2})`, "null\n", "- 1\n- 2\n- 1\n- a: 1\na: 1\nb: 2\n")
	in := "a: {k: 1}\nb: [1, [2]]\nd: !!custom v\ne: 2.50\n"
	wantYQ(t, `"x" + .d, (.e + "s")`, in, "xv\n2.50s\n")
	wantYQ(t, `.b + .a, (.a + {"z": 1})`, in, "[1, [2], {k: 1}]\n{k: 1, z: 1}\n")
	wantYQError(t, `1 + null`, "null\n", "!!int cannot be added to !!null")
	wantYQError(t, `true + 1`, "null\n", "!!bool cannot be added to !!int")
	wantYQError(t, `.d + 1`, in, "!!custom cannot be added to !!int")
	wantYQError(t, `2 + [1]`, "null\n", "!!seq () cannot be added to a !!int ()")
	wantYQError(t, `.a + 1`, in, "!!int () cannot be added to a !!map (a)")
	wantYQError(t, `.a.k + .b[1]`, in, "!!seq (b[1]) cannot be added to a !!int (a.k)")
}

func TestYQIntegersKeepTheLeftBase(t *testing.T) {
	in := "a: 0x10\no: 0o17\nn: 0x0a\nx: 0X1f\n"
	wantYQ(t, `.a + 1, (.a * 2), (.a - 1), (.a % 3), (.x + 1)`, in, "0x11\n0x20\n0xF\n0x1\n0x20\n")
	// yq prints 0x-A, which reads back as a string, so a negative result
	// stays in decimal.
	wantYQ(t, `.n - 20`, in, "-10\n")
	wantYQ(t, `.o * 2, (.o + 1), (.o + .a), (.a + .o)`, in, "0o36\n0o20\n0o37\n0x1F\n")
	wantYQ(t, `1 + .a, (.a + 1 | tag), (.a + 1 | . + 1)`, in, "17\n!!int\n0x12\n")
	opts := testOpts
	opts.FloatNumbers = true
	wantYQOpts(t, opts, `.a + 1, (.o * 2)`, in, "0x11\n0o36\n")
}

func TestYQDecimalRemainder(t *testing.T) {
	wantYQ(t, `[5.5 % 2, -5.5 % 2, 7 % 2.5]`, "null\n", "- 1.5\n- -1.5\n- 2\n")
}

func TestYQHexLiterals(t *testing.T) {
	wantYQ(t, `.a = 0x10`, "a: 1\n", "a: 0x10\n")
	wantYQ(t, `.a + 0x10`, "a: 1\n", "17\n")
	wantYQ(t, `.a = 0xff + 1`, "a: 1\n", "a: 0x100\n")
	wantYQ(t, `.a = 0x10 * 2`, "a: 1\n", "a: 0x20\n")
	wantYQ(t, `0x10 | . + 1`, "a: 1\n", "0x11\n")
	wantYQ(t, `[0x10, 0X1f]`, "a: 1\n", "- 0x10\n- 0X1f\n")
	wantYQ(t, `0x10 | tag`, "a: 1\n", "!!int\n")
	wantYQ(t, `.a = (0x10 | tostring)`, "a: 1\n", "a: \"0x10\"\n")
	wantYQError(t, `.a = 0xFFFFFFFFFFFFFFFFFF`, "a: 1\n", `strconv.ParseInt: parsing "FFFFFFFFFFFFFFFFFF": value out of range`)
	wantYQ(t, `0xA - 20`, "a: 1\n", "-10\n")
	wantYQError(t, `0x`, "a: 1\n", `1:2: lexer: invalid input text "x"`)
}

func TestYQLenientEscapes(t *testing.T) {
	// yq keeps \u as it is, the backslash included.
	bs := string(rune(92))
	wantYQ(t, `.a = "`+bs+`u00e9`+bs+`x"`, "a: 1\n", "a: "+bs+"u00e9"+bs+"x\n")
	wantYQ(t, `.a = "`+bs+`ud83d`+bs+`ude00" | .a | length`, "a: 1\n", "12\n")
	wantYQ(t, `.a = "\d"`, "a: 1\n", "a: \\d\n")
	wantYQ(t, `.a = "x\/y"`, "a: 1\n", "a: x\\/y\n")
	wantYQ(t, `.a = "\x\0\U\'"`, "a: 1\n", "a: \\x\\0\\U\\'\n")
	wantYQ(t, `.a = "\\d\"q\n"`, "a: 1\n", "a: |\n  \\d\"q\n")
	wantYQ(t, `.a = "\a\v" | .a | length`, "a: 1\n", "2\n")
	wantYQ(t, `.a = "t\(1+1)\d"`, "a: 1\n", "a: t2\\d\n")
}

func TestYQArithmeticKeepsTheLeftAnchor(t *testing.T) {
	wantYQ(t, `.a += 1`, "a: &x 1\nb: *x\n", "a: &x 2\nb: *x\n")
	wantYQ(t, `.a -= 1`, "a: &x 3\n", "a: &x 2\n")
	wantYQ(t, `.a *= 2`, "a: &x 3 # c\n", "a: &x 6 # c\n")
	wantYQ(t, `.a += "b"`, "a: &x s\n", "a: &x sb\n")
	wantYQ(t, `.a += {"k": 1}`, "a: &x {j: 0}\n", "a: &x {j: 0, k: 1}\n")
	wantYQ(t, `.a += [2]`, "a: &x [1]\n", "a: &x [1, 2]\n")
	wantYQ(t, `.a = .a + 1`, "a: &x 1\n", "a: &x 2\n")
	wantYQ(t, `.c = .a + 1`, "a: &x 1\n", "a: &x 1\nc: 2\n")
	wantYQ(t, `.a = .a / 2`, "a: &x 3\n", "a: &x 1.5\n")
	wantYQ(t, `.a = .a % 2`, "a: &x 3\n", "a: &x 1\n")
}

func TestYQIntegerZeroHasNoSign(t *testing.T) {
	wantYQ(t, `.a * 0`, "a: -3\n", "0\n")
	wantYQ(t, `.a * 0`, "a: !foo -3\n", "0\n")
	wantYQ(t, `.a * -1`, "a: 0\n", "0\n")
	wantYQ(t, `.a * -1`, "a: !foo 0\n", "0\n")
	wantYQ(t, `.a + 0`, "a: -0\n", "0\n")
	wantYQ(t, `.a * -1`, "a: 0.0\n", "-0\n")
}

// A float divided or reduced by zero is an infinity or NaN, as in yq,
// where the integer remainder alone is an error. yq's == compares the
// text a scalar prints as, so a NaN equals a NaN.
func TestYQFloatZeroDivisor(t *testing.T) {
	for _, opts := range []Options{testOpts, {Caps: testOpts.Caps, FloatNumbers: true}} {
		wantYQOpts(t, opts, `[(.a / 0) > 1000, (-.a / 0) < -1000, ((0 / 0) == (0 / 0)), (.a / 0 | tag)]`, "a: 1\n", "- true\n- true\n- true\n- '!!float'\n")
		wantYQOpts(t, opts, `(.a % 0) == (.a % 0)`, "a: 5.5\n", "true\n")
	}
	_, err := runYQ(t, testOpts, `.a % 0`, "a: 5\n", false)
	wantErrContains(t, err, "cannot modulo by 0")
}

// A scalar with a custom tag over text that reads as a number takes part
// in arithmetic as that number, and the result keeps the tag when the
// tagged node is on the left.
func TestYQCustomTagArithmetic(t *testing.T) {
	in := "a: !count 41\n"
	wantYQ(t, `.a + 1`, in, "42\n")
	wantYQ(t, `.a - 1`, in, "40\n")
	wantYQ(t, `.b = (.a + 1)`, in, "a: !count 41\nb: !count 42\n")
	wantYQ(t, `.b = (1 + .a)`, in, "a: !count 41\nb: 42\n")
	wantYQ(t, `.b = (.a + .a)`, in, "a: !count 41\nb: !count 82\n")
	wantYQ(t, `.a / 2`, in, "20.5\n")
	wantYQ(t, `.a % 5`, in, "1\n")
	wantYQ(t, `.a + 1.5`, in, "42.5\n")
}
