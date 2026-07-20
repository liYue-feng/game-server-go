package store

import (
	"errors"
	"testing"
)

func TestValidateAndCloseOnFailureClosesRedisOnceAndPreservesPingError(t *testing.T) {
	pingErr := errors.New("ping failed")
	closeErr := errors.New("close failed")
	closeCalls := 0

	err := validateAndCloseOnFailure(
		func() error { return pingErr },
		func() error {
			closeCalls++
			return closeErr
		},
	)

	if err != pingErr {
		t.Fatalf("validateAndCloseOnFailure() error = %v, want original ping error %v", err, pingErr)
	}
	if closeCalls != 1 {
		t.Fatalf("validateAndCloseOnFailure() close calls = %d, want 1", closeCalls)
	}
}
