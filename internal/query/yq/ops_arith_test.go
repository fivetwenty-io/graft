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
}

func TestYQArithmeticRunsLeftFirst(t *testing.T) {
	wantYQ(t, `[(1, 2) + (10, 20)]`, "null\n", "- 11\n- 21\n- 12\n- 22\n")
	wantYQ(t, `[(1, 2) - (10, 20)]`, "null\n", "- -9\n- -19\n- -8\n- -18\n")
	wantYQ(t, `[(10, 20) % (3, 7)]`, "null\n", "- 1\n- 3\n- 2\n- 6\n")
	wantYQ(t, `[("a", "b") + ("c", "d")]`, "null\n", "- ac\n- ad\n- bc\n- bd\n")
	wantYQ(t, `[(1, 3) < (2, 4)]`, "null\n", "- true\n- true\n- false\n- true\n")
}
