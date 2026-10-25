package decimal

import (
	"math/big"
	"testing"
)

func wantEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func add(t *testing.T, a, b string) Decimal {
	t.Helper()
	r, err := MustParse(a).Add(MustParse(b))
	if err != nil {
		t.Fatalf("%s + %s: %v", a, b, err)
	}
	return r
}

func sub(t *testing.T, a, b string) Decimal {
	t.Helper()
	r, err := MustParse(a).Sub(MustParse(b))
	if err != nil {
		t.Fatalf("%s - %s: %v", a, b, err)
	}
	return r
}

func mul(t *testing.T, a, b string) Decimal {
	t.Helper()
	r, err := MustParse(a).Mul(MustParse(b))
	if err != nil {
		t.Fatalf("%s * %s: %v", a, b, err)
	}
	return r
}

func rem(t *testing.T, a, b string) Decimal {
	t.Helper()
	r, err := MustParse(a).Rem(MustParse(b))
	if err != nil {
		t.Fatalf("%s %% %s: %v", a, b, err)
	}
	return r
}

func quo(t *testing.T, a, b string) Decimal {
	t.Helper()
	r, err := MustParse(a).Quo(MustParse(b), Context128)
	if err != nil {
		t.Fatalf("%s / %s: %v", a, b, err)
	}
	return r
}

// rat returns a finite Decimal's exact value, which is the oracle the
// randomized tests check the arithmetic against.
func rat(t *testing.T, d Decimal) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(d.Scientific())
	if !ok {
		t.Fatalf("big.Rat cannot read %q", d.Scientific())
	}
	return r
}
