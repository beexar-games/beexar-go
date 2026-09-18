package beexar

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// ErrMalformedAmount is returned for a string outside the contract's amount
// shape.
var ErrMalformedAmount = errors.New("beexar: not a valid decimal amount")

const (
	// MaxScale bounds fractional digits, mirroring the platform's
	// NUMERIC(38,16) storage.
	MaxScale = 16
	// MaxClientDecimalLen mirrors the `maxLength: 40` the OpenAPI contract puts
	// on amount fields.
	MaxClientDecimalLen = 40
)

// clientDecimal is the shape the contract accepts: a non-negative plain
// decimal, at most MaxScale fractional digits, and NO scientific notation.
//
// Rejecting the exponent form is load-bearing, not cosmetic. "1E2000000000"
// parses in microseconds in every bignum library and costs nothing until the
// first rescale, at which point it asks for a multi-gigabyte integer and takes
// the process with it. The guard has to run on shape, before any arithmetic.
var clientDecimal = regexp.MustCompile(`^\d+(\.\d{1,16})?$`)

var currencyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,31}$`)

// Money is a decimal amount in a currency's main unit — "0.90" is ninety cents.
//
// There is no constructor taking a float. IEEE-754 cannot hold 0.1, and a
// wallet that rounds a player's balance by a hundredth of a cent per round is a
// reconciliation incident rather than a rounding detail.
//
// The zero value is a valid zero amount at scale 0.
type Money struct {
	units *big.Int
	scale int
}

// ZeroMoney is zero at scale 2 — the scale the contract's examples use.
var ZeroMoney = Money{units: big.NewInt(0), scale: 2}

func (m Money) int() *big.Int {
	if m.units == nil {
		return big.NewInt(0)
	}
	return m.units
}

// ParseMoney reads a decimal string from the wire.
func ParseMoney(s string) (Money, error) {
	if len(s) == 0 || len(s) > MaxClientDecimalLen || !clientDecimal.MatchString(s) {
		return Money{}, fmt.Errorf("%w: %q", ErrMalformedAmount, s)
	}
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return Money{}, fmt.Errorf("%w: %q", ErrMalformedAmount, s)
		}
		return Money{units: n, scale: 0}, nil
	}
	digits := s[:dot] + s[dot+1:]
	n, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return Money{}, fmt.Errorf("%w: %q", ErrMalformedAmount, s)
	}
	return Money{units: n, scale: len(s) - dot - 1}, nil
}

// MustParseMoney is ParseMoney for constants and tests. It panics on a bad
// value, so never give it anything that came off the wire.
func MustParseMoney(s string) Money {
	m, err := ParseMoney(s)
	if err != nil {
		panic(err)
	}
	return m
}

// MoneyFromMinorUnits builds an amount from an integer number of minor units —
// the bridge for a ledger that stores cents. MoneyFromMinorUnits(12345, 2) is
// "123.45".
func MoneyFromMinorUnits(units *big.Int, scale int) (Money, error) {
	if scale < 0 || scale > MaxScale {
		return Money{}, fmt.Errorf("beexar: scale must be in [0,%d], got %d", MaxScale, scale)
	}
	return Money{units: new(big.Int).Set(units), scale: scale}, nil
}

// MinorUnits returns the unscaled integer value at Scale.
func (m Money) MinorUnits() *big.Int { return new(big.Int).Set(m.int()) }

// Scale is the number of fractional digits the amount is expressed in.
func (m Money) Scale() int { return m.scale }

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

func (m Money) rescale(target int) *big.Int {
	switch {
	case target == m.scale:
		return new(big.Int).Set(m.int())
	case target > m.scale:
		return new(big.Int).Mul(m.int(), pow10(target-m.scale))
	default:
		// Truncate toward zero, the same direction the platform truncates, so a
		// rounding step can never inflate what the operator owes.
		return new(big.Int).Quo(m.int(), pow10(m.scale-target))
	}
}

// Format renders with exactly scale fractional digits, truncating toward zero.
func (m Money) Format(scale int) string {
	if scale < 0 || scale > MaxScale {
		panic(fmt.Sprintf("beexar: scale must be in [0,%d], got %d", MaxScale, scale))
	}
	u := m.rescale(scale)
	neg := u.Sign() < 0
	digits := new(big.Int).Abs(u).String()
	if len(digits) < scale+1 {
		digits = strings.Repeat("0", scale+1-len(digits)) + digits
	}
	out := digits[:len(digits)-scale]
	if scale > 0 {
		out += "." + digits[len(digits)-scale:]
	}
	if neg {
		out = "-" + out
	}
	return out
}

// String renders at the amount's own scale — the canonical wire form.
func (m Money) String() string { return m.Format(m.scale) }

// FormatBonus renders bonus_amount, which the contract caps at 12 fractional
// digits while platform money allows 16. Clamping here keeps a 16-decimal
// currency from emitting a value the contract would reject.
func (m Money) FormatBonus() string {
	s := m.scale
	if s > 12 {
		s = 12
	}
	return m.Format(s)
}

// MarshalJSON emits the wire form, so an amount can never be serialised as a
// JSON number.
func (m Money) MarshalJSON() ([]byte, error) { return json.Marshal(m.String()) }

// UnmarshalJSON accepts only the contract's decimal-string shape.
func (m *Money) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("%w: amount must be a JSON string", ErrMalformedAmount)
	}
	parsed, err := ParseMoney(s)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

func (m Money) align(other Money) (*big.Int, *big.Int, int) {
	s := m.scale
	if other.scale > s {
		s = other.scale
	}
	return m.rescale(s), other.rescale(s), s
}

// Add returns m + other.
func (m Money) Add(other Money) Money {
	a, b, s := m.align(other)
	return Money{units: a.Add(a, b), scale: s}
}

// Sub returns m - other. The result may be negative, which is how an
// insufficient balance is detected.
func (m Money) Sub(other Money) Money {
	a, b, s := m.align(other)
	return Money{units: a.Sub(a, b), scale: s}
}

// Cmp returns -1, 0 or 1.
func (m Money) Cmp(other Money) int {
	a, b, _ := m.align(other)
	return a.Cmp(b)
}

// Equal compares by value, so "1.5" and "1.50" are equal.
func (m Money) Equal(other Money) bool { return m.Cmp(other) == 0 }

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.int().Sign() == 0 }

// IsNegative reports whether the amount is below zero.
func (m Money) IsNegative() bool { return m.int().Sign() < 0 }

// IsPositive reports whether the amount is above zero.
func (m Money) IsPositive() bool { return m.int().Sign() > 0 }

// ClampToZero returns m, or zero when m is negative.
func (m Money) ClampToZero() Money {
	if m.IsNegative() {
		return Money{units: big.NewInt(0), scale: m.scale}
	}
	return m
}

// ValidCurrency reports whether code matches the contract's currency pattern:
// ISO-4217 fiat, a crypto ticker, or an operator's CUSTOM_ code.
//
// Lowercase is REJECTED, never normalised — the platform's schema does the same,
// and quietly upcasing here would hide an operator bug until settlement.
func ValidCurrency(code string) bool { return currencyPattern.MatchString(code) }

// CurrencyPattern is the regular expression currencies must match.
const CurrencyPattern = `^[A-Z][A-Z0-9_]{2,31}$`
