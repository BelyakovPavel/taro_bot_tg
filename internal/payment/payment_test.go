package payment

import (
	"context"
	"testing"
)

// fakeProvider implements Provider for tests that need a non-nil one.
type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) CreatePayment(context.Context, CreatePaymentRequest) (*Payment, error) {
	return nil, nil
}
func (fakeProvider) CheckPayment(context.Context, string) (*Payment, error) { return nil, nil }

func TestFreeMode(t *testing.T) {
	s := New(nil)
	if !s.IsFree() {
		t.Errorf("IsFree() = false, want true with nil provider")
	}
	if s.Provider() != nil {
		t.Errorf("Provider() = %v, want nil", s.Provider())
	}

	// Free mode grants every feature without a payment.
	pay, err := s.Require(context.Background(), "uid-1", FeatureTarotSpread)
	if err != nil {
		t.Errorf("Require in free mode: %v", err)
	}
	if pay != nil {
		t.Errorf("Require in free mode returned payment %+v", pay)
	}
}

func TestProviderSet(t *testing.T) {
	s := New(fakeProvider{})
	if s.IsFree() {
		t.Errorf("IsFree() = true, want false with provider set")
	}
	if s.Provider() == nil {
		t.Errorf("Provider() = nil, want fakeProvider")
	}

	// Until the entitlement logic is implemented, Require reports that
	// the provider is not implemented rather than silently granting.
	if _, err := s.Require(context.Background(), "uid-1", FeatureNumerology); err == nil {
		t.Errorf("Require with provider set: expected error")
	}
}
