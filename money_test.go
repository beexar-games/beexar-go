package beexar_test

import (
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	beexar "github.com/beexar-games/beexar-go"
)

func TestParseMoneyKeepsItsScale(t *testing.T) {
	for _, in := range []string{"0.90", "100", "1.2300"} {
		if got := beexar.MustParseMoney(in).String(); got != in {
			t.Errorf("ParseMoney(%q).String() = %q", in, got)
		}
	}
}

func TestParseMoneyRejectsTheExponentBomb(t *testing.T) {
	// The whole point: this parses in microseconds in every bignum library and
	// then asks for a multi-gigabyte integer on the first rescale.
	start := time.Now()
	_, err := beexar.ParseMoney("1E2000000000")
	if !errors.Is(err, beexar.ErrMalformedAmount) {
		t.Fatalf("expected ErrMalformedAmount, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("rejection took %v — the guard must run on shape, before arithmetic", elapsed)
	}
}

func TestParseMoneyRejectsWhatTheContractRejects(t *testing.T) {
	bad := []string{
		"",                    // empty
		"1.12345678901234567", // 17 fractional digits, one past the ceiling
		"-1.00",               // negative
		"+1.00",               // signed
		" 1.00",               // padded
		"1,00",                // wrong separator
		"NaN", "Infinity",     // not decimals
		".5", "1.", // incomplete
		strings.Repeat("1", 41), // longer than maxLength 40
	}
	for _, in := range bad {
		if _, err := beexar.ParseMoney(in); err == nil {
			t.Errorf("ParseMoney(%q) should have failed", in)
		}
	}
}

func TestMoneyArithmetic(t *testing.T) {
	if got := beexar.MustParseMoney("0.1").Add(beexar.MustParseMoney("0.2")).String(); got != "0.3" {
		t.Errorf("0.1 + 0.2 = %q, want 0.3", got)
	}
	if got := beexar.MustParseMoney("100.00").Sub(beexar.MustParseMoney("0.005")).String(); got != "99.995" {
		t.Errorf("scale alignment lost digits: %q", got)
	}

	after := beexar.MustParseMoney("10.00").Sub(beexar.MustParseMoney("25.00"))
	if !after.IsNegative() {
		t.Error("subtraction must be able to go negative so a bet can be refused")
	}
	if got := after.ClampToZero().String(); got != "0.00" {
		t.Errorf("ClampToZero = %q, want 0.00", got)
	}

	if !beexar.MustParseMoney("1.5").Equal(beexar.MustParseMoney("1.50")) {
		t.Error("1.5 and 1.50 are the same amount")
	}

	// Well past float64's exact-integer range.
	huge := beexar.MustParseMoney("90071992547409910.99")
	if got := huge.Add(beexar.MustParseMoney("0.01")).String(); got != "90071992547409911.00" {
		t.Errorf("large-value addition = %q", got)
	}
}

func TestMoneyFormatTruncatesTowardZero(t *testing.T) {
	// Rounding up here would inflate what the operator owes.
	if got := beexar.MustParseMoney("1.999").Format(2); got != "1.99" {
		t.Errorf("Format(2) = %q, want 1.99", got)
	}
	if got := beexar.MustParseMoney("1").Format(4); got != "1.0000" {
		t.Errorf("Format(4) = %q, want 1.0000", got)
	}
	// bonus_amount allows 12 fractional digits; platform money allows 16.
	if got := beexar.MustParseMoney("1.1234567890123456").FormatBonus(); got != "1.123456789012" {
		t.Errorf("FormatBonus = %q", got)
	}
}

func TestMoneyJSONIsAlwaysAString(t *testing.T) {
	b, err := json.Marshal(struct {
		Balance beexar.Money `json:"balance"`
	}{beexar.MustParseMoney("12.30")})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"balance":"12.30"}` {
		t.Errorf("marshalled as %s — an amount must never become a JSON number", b)
	}

	var out struct {
		Balance beexar.Money `json:"balance"`
	}
	if err := json.Unmarshal([]byte(`{"balance":12.3}`), &out); err == nil {
		t.Error("a JSON number must be refused, not silently converted")
	}
}

func TestMoneyFromMinorUnits(t *testing.T) {
	m, err := beexar.MoneyFromMinorUnits(big.NewInt(9970), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.String(); got != "99.70" {
		t.Errorf("MoneyFromMinorUnits = %q, want 99.70", got)
	}
	if _, err := beexar.MoneyFromMinorUnits(big.NewInt(1), 17); err == nil {
		t.Error("a scale past MaxScale must be refused")
	}
}

func TestValidCurrencyDoesNotNormalise(t *testing.T) {
	for _, ok := range []string{"EUR", "USD", "BTC", "CUSTOM_WIN", "XBT1"} {
		if !beexar.ValidCurrency(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"eur", "Eur", "EU", "1EUR", "_EUR", ""} {
		if beexar.ValidCurrency(bad) {
			t.Errorf("%q should be rejected, not upcased", bad)
		}
	}
}
