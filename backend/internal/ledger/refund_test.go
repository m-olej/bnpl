package ledger

import (
	"errors"
	"testing"
)

func TestRefundFee(t *testing.T) {
	tests := []struct {
		name        string
		totalFee    int64
		refundedFee int64
		amount      int64
		outstanding int64
		wantFee     int64
		wantErr     error
	}{
		{
			name:        "full refund reverses remaining fee",
			totalFee:    250,
			amount:      10000,
			outstanding: 10000,
			wantFee:     250,
		},
		{
			name:        "partial refund reverses proportionally",
			totalFee:    300,
			amount:      1000,
			outstanding: 3000,
			wantFee:     100,
		},
		{
			name:        "partial refund floors the reversal",
			totalFee:    3,
			amount:      10,
			outstanding: 30,
			wantFee:     1,
		},
		{
			name:        "no fee to reverse",
			totalFee:    0,
			amount:      500,
			outstanding: 1000,
			wantFee:     0,
		},
		{
			name:        "already fully reversed",
			totalFee:    250,
			refundedFee: 250,
			amount:      100,
			outstanding: 100,
			wantFee:     0,
		},
		{
			name:        "clamped when remaining fee exceeds amount",
			totalFee:    200,
			amount:      50,
			outstanding: 50,
			wantFee:     50,
		},
		{
			name:        "clamped when proportional exceeds amount",
			totalFee:    1000,
			amount:      1,
			outstanding: 1,
			wantFee:     1,
		},
		{
			name:        "nothing outstanding is an error",
			totalFee:    250,
			amount:      10,
			outstanding: 0,
			wantErr:     ErrRefundExceedsOutstanding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := refundFee(tt.totalFee, tt.refundedFee, tt.amount, tt.outstanding)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantFee {
				t.Fatalf("refundFee = %d, want %d", got, tt.wantFee)
			}
			if got < 0 || got > tt.amount {
				t.Fatalf("refundFee = %d out of [0, %d]", got, tt.amount)
			}
		})
	}
}
