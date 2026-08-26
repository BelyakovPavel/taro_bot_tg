// Package payment is the integration seam for online payments. The
// concrete gateway (YuKassa, Robokassa, Stripe, CloudPayments, ...) is
// not chosen yet; until one is configured the Service runs in free mode
// and grants every feature.
package payment

import (
	"context"
	"errors"
)

// Feature identifies a payable feature of the bot.
type Feature string

const (
	// FeatureTarotSpread is the main tarot spread (3 cards + AI forecast).
	FeatureTarotSpread Feature = "tarot_spread"
	// FeatureTarotClarifier is the additional clarifier card.
	FeatureTarotClarifier Feature = "tarot_clarifier"
	// FeatureNumerology is the numerology / natal chart reading.
	FeatureNumerology Feature = "numerology"
)

// PaymentStatus is the lifecycle state of a payment.
type PaymentStatus string

const (
	PaymentStatusPending PaymentStatus = "pending"
	PaymentStatusPaid    PaymentStatus = "paid"
	PaymentStatusFailed  PaymentStatus = "failed"
)

// ErrPaymentRequired is returned by Service.Require when a feature has
// to be paid for.
var ErrPaymentRequired = errors.New("payment required")

// CreatePaymentRequest describes a payment to be created.
type CreatePaymentRequest struct {
	Feature     Feature
	UserUID     string
	Amount      float64
	Currency    string
	Description string
}

// Payment is the result of creating a payment.
type Payment struct {
	ID       string
	Status   PaymentStatus
	Amount   float64
	Currency string
	PayURL   string // hosted checkout page, if the provider supports it
}

// Provider is the payment gateway interface. Implement it for the
// concrete provider once it is chosen.
type Provider interface {
	// Name returns the gateway name, e.g. "yukassa" or "robokassa".
	Name() string
	// CreatePayment registers a payment for the feature.
	CreatePayment(ctx context.Context, req CreatePaymentRequest) (*Payment, error)
	// CheckPayment returns the current status of a payment.
	CheckPayment(ctx context.Context, paymentID string) (*Payment, error)
}

// Service gates payable features. With no provider configured it runs
// in free mode and grants every feature.
type Service struct {
	provider Provider
}

// New returns a Service backed by the given provider (nil = free mode).
func New(provider Provider) *Service {
	return &Service{provider: provider}
}

// Provider returns the configured provider (nil in free mode).
func (s *Service) Provider() Provider { return s.provider }

// IsFree reports whether payments are not configured yet.
func (s *Service) IsFree() bool { return s.provider == nil }

// Require checks whether the user may use the feature for free.
// It returns (nil, nil) when access is granted — either because the
// service is in free mode or because the user already has entitlement.
// Otherwise it returns ErrPaymentRequired together with a created
// Payment so the caller can start the checkout flow (e.g. send the
// hosted PayURL to the user).
func (s *Service) Require(ctx context.Context, userUID string, feature Feature) (*Payment, error) {
	if s.provider == nil {
		return nil, nil // free mode
	}

	// TODO(payments): check stored entitlements for userUID + feature;
	// when not covered, create a payment via s.provider.CreatePayment
	// and return (payment, ErrPaymentRequired).
	return nil, errors.New("payment provider is not implemented yet")
}
