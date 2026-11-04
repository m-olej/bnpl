package ledger

import (
	"fmt"
	"math"
)

// CalculateFee returns the platform fee in minor units for a principal given a
// rate in basis points (1 bps = 0.01%). Integer arithmetic only: the result is
// floored, which is deterministic and never rounds in the platform's favour
// beyond a single minor unit.
//
// Callers must ensure feeBps is within [0, 10000] and principal >= 0.
func CalculateFee(principal, feeBps int64) (int64, error) {
	if principal < 0 {
		return 0, fmt.Errorf("%w: principal %d must be non-negative", ErrInvalidFee, principal)
	}
	if feeBps < 0 || feeBps > 10000 {
		return 0, fmt.Errorf("%w: feeBps %d out of range [0,10000]", ErrInvalidFee, feeBps)
	}
	if principal == 0 || feeBps == 0 {
		return 0, nil
	}
	if principal > math.MaxInt64/feeBps {
		return 0, fmt.Errorf("%w: principal %d * feeBps %d overflows int64", ErrAmountOverflow, principal, feeBps)
	}
	return (principal * feeBps) / 10000, nil
}
