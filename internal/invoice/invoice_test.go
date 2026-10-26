package invoice

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseMoney(t *testing.T) {
	cases := []struct {
		in      string
		want    Money
		wantErr bool
	}{
		{"12.34", 1234, false},
		{"0", 0, false},
		{"-5.5", -550, false},
		{" 7 ", 700, false},
		{"999999999.99", 99999999999, false},
		{"1.234", 0, true},
		{"1e3", 0, true},
		{"", 0, true},
		{"abc", 0, true},
		{"999999999999999999999", 0, true},
	}
	for _, tc := range cases {
		got, err := ParseMoney(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseMoney(%q) = %d, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMoney(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseMoney(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestMoneyUnmarshalRejectsExponent(t *testing.T) {
	var payload struct {
		Amount *Money `json:"amount"`
	}
	if err := json.Unmarshal([]byte(`{"amount":1e5}`), &payload); err == nil {
		t.Fatal("expected exponent notation to be rejected")
	}
	if err := json.Unmarshal([]byte(`{"amount":"19.90"}`), &payload); err != nil {
		t.Fatalf("decimal string amount: %v", err)
	}
	if payload.Amount == nil || *payload.Amount != 1990 {
		t.Fatalf("amount = %v, want 1990", payload.Amount)
	}
	if err := json.Unmarshal([]byte(`{"amount":12.5}`), &payload); err != nil {
		t.Fatalf("numeric amount: %v", err)
	}
	if payload.Amount == nil || *payload.Amount != 1250 {
		t.Fatalf("amount = %v, want 1250", payload.Amount)
	}
}

func baseInvoice() *Invoice {
	return &Invoice{
		ID:            1,
		AmountCents:   1000,
		Currency:      "CNY",
		Category:      CategoryOther,
		InvoiceDate:   "2025-01-31",
		InvoiceNumber: "123",
		SellerName:    "ACME",
		Status:        StatusNeedsReview,
		Version:       3,
	}
}

func TestUpdateApplyRejectsStaleVersion(t *testing.T) {
	inv := baseInvoice()
	patch := Update{Version: 2}
	if err := patch.Apply(inv); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestUpdateApplyRejectsProcessing(t *testing.T) {
	inv := baseInvoice()
	inv.Status = StatusProcessing
	if err := (Update{Version: 3}).Apply(inv); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestUpdateApplyValidatesFields(t *testing.T) {
	money := Money(500)
	badDate := "2025-02-30"
	inv := baseInvoice()
	patch := Update{Version: 3, InvoiceDate: &badDate, Amount: &money}
	if err := patch.Apply(inv); err == nil {
		t.Fatal("expected invalid date to be rejected")
	}

	inv = baseInvoice()
	badCurrency := "XXX"
	patch = Update{Version: 3, Currency: &badCurrency}
	if err := patch.Apply(inv); err == nil {
		t.Fatal("expected unsupported currency to be rejected")
	}

	inv = baseInvoice()
	badCategory := "travel"
	patch = Update{Version: 3, Category: &badCategory}
	if err := patch.Apply(inv); err == nil {
		t.Fatal("expected invalid category to be rejected")
	}
}

func TestUpdateApplyConfirmRequiresCoreFields(t *testing.T) {
	inv := baseInvoice()
	money := Money(1000)
	if err := (Update{Version: 3, Confirm: true, Amount: &money}).Apply(inv); err != nil {
		t.Fatalf("confirm with amount: %v", err)
	}
	if inv.Status != StatusDone {
		t.Fatalf("status = %s, want %s", inv.Status, StatusDone)
	}

	inv = baseInvoice()
	inv.SellerName = ""
	if err := (Update{Version: 3, Confirm: true, Amount: &money}).Apply(inv); err == nil {
		t.Fatal("expected confirm without seller to be rejected")
	}
}

func TestUpdateApplyAllowsNegativeRedInvoice(t *testing.T) {
	inv := baseInvoice()
	credit := Money(-2500)
	if err := (Update{Version: 3, Amount: &credit}).Apply(inv); err != nil {
		t.Fatalf("negative amount: %v", err)
	}
	if inv.AmountCents != -2500 {
		t.Fatalf("amount = %d, want -2500", inv.AmountCents)
	}
}

func TestValidateDateRange(t *testing.T) {
	if err := ValidateDateRange("2025-01-01", "2025-01-31"); err != nil {
		t.Fatalf("valid range rejected: %v", err)
	}
	if err := ValidateDateRange("2025-02-01", "2025-01-01"); err == nil {
		t.Fatal("expected inverted range to be rejected")
	}
	if err := ValidateDateRange("", "2025-13-01"); err == nil {
		t.Fatal("expected invalid month to be rejected")
	}
}

func TestValidateFieldsCurrencyMinorUnits(t *testing.T) {
	if err := ValidateFields("2025-01-01", 150, "JPY", CategoryOther); err == nil {
		t.Fatal("expected fractional yen amount to be rejected")
	}
	if err := ValidateFields("2025-01-01", 100, "JPY", CategoryOther); err != nil {
		t.Fatalf("whole-unit yen amount rejected: %v", err)
	}
	if err := ValidateFields("2025-01-01", 100, "CNY", CategoryOther); err != nil {
		t.Fatalf("cents for CNY: %v", err)
	}
}
