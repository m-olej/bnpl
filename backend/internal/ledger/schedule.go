package ledger

import (
	"fmt"
	"time"
)

// InstallmentPlan is a single scheduled repayment.
type InstallmentPlan struct {
	// Number is 1-based (installment #1 is the first payment).
	Number  int
	Amount  int64
	DueDate time.Time
}

// SplitInstallments divides total (minor units) into count installments.
//
// It is deterministic: the integer-division remainder is allocated to the
// first installment (AGENTS.md section 1.1, e.g. $100.00 / 3 -> 3334, 3333,
// 3333). Every installment is guaranteed to be strictly positive because
// total must be at least count (one minor unit each).
func SplitInstallments(total int64, count int) ([]int64, error) {
	if count < 1 {
		return nil, fmt.Errorf("%w: count %d must be >= 1", ErrInvalidInstallmentSplit, count)
	}
	if total < 0 {
		return nil, fmt.Errorf("%w: total %d must be non-negative", ErrInvalidInstallmentSplit, total)
	}
	if total < int64(count) {
		return nil, fmt.Errorf(
			"%w: total %d minor units cannot fund %d positive installments",
			ErrInvalidInstallmentSplit, total, count,
		)
	}

	base := total / int64(count)
	remainder := total % int64(count)

	amounts := make([]int64, count)
	for i := range amounts {
		amounts[i] = base
	}
	amounts[0] += remainder
	return amounts, nil
}

// BuildSchedule produces a full installment plan: deterministic amounts plus
// due dates spaced by interval starting at firstDue.
func BuildSchedule(total int64, count int, firstDue time.Time, interval time.Duration) ([]InstallmentPlan, error) {
	if interval < 0 {
		return nil, fmt.Errorf("%w: interval %s must be non-negative", ErrInvalidInstallmentSplit, interval)
	}
	amounts, err := SplitInstallments(total, count)
	if err != nil {
		return nil, err
	}

	plans := make([]InstallmentPlan, count)
	for i, amount := range amounts {
		plans[i] = InstallmentPlan{
			Number:  i + 1,
			Amount:  amount,
			DueDate: firstDue.Add(time.Duration(i) * interval),
		}
	}
	return plans, nil
}
