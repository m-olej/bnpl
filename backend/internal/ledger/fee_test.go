package ledger

import (
	"errors"
	"math"
	"testing"
)

func TestCalculateFee(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		principal int64
		feeBps    int64
		want      int64
		wantErr   error
	}{
		{name: "zero rate", principal: 10000, feeBps: 0, want: 0},
		{name: "zero principal", principal: 0, feeBps: 250, want: 0},
		{name: "2.5 percent of 100.00", principal: 10000, feeBps: 250, want: 250},
		{name: "floors fractional cent", principal: 999, feeBps: 250, want: 24}, // 24.975 -> 24
		{name: "100 percent", principal: 5000, feeBps: 10000, want: 5000},
		{name: "one basis point", principal: 1000000, feeBps: 1, want: 100},
		{name: "single cent", principal: 1, feeBps: 250, want: 0},
		{
			name: "negative principal rejected", principal: -1, feeBps: 100,
			wantErr: ErrInvalidFee,
		},
		{
			name: "rate above 100 percent rejected", principal: 100, feeBps: 10001,
			wantErr: ErrInvalidFee,
		},
		{
			name: "negative rate rejected", principal: 100, feeBps: -1,
			wantErr: ErrInvalidFee,
		},
		{
			name: "overflow rejected", principal: math.MaxInt64, feeBps: 2,
			wantErr: ErrAmountOverflow,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := CalculateFee(tt.principal, tt.feeBps)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("CalculateFee(%d, %d) error = %v, want %v", tt.principal, tt.feeBps, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CalculateFee(%d, %d) unexpected error: %v", tt.principal, tt.feeBps, err)
			}
			if got != tt.want {
				t.Fatalf("CalculateFee(%d, %d) = %d, want %d", tt.principal, tt.feeBps, got, tt.want)
			}
		})
	}
}
