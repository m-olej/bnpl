package ledger

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestSplitInstallments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		total   int64
		count   int
		want    []int64
		wantErr error
	}{
		{
			name:  "canonical 100 dollars over 3",
			total: 10000, count: 3,
			want: []int64{3334, 3333, 3333},
		},
		{
			name:  "evenly divisible",
			total: 9999, count: 3,
			want: []int64{3333, 3333, 3333},
		},
		{
			name:  "pay in 4 exact",
			total: 10000, count: 4,
			want: []int64{2500, 2500, 2500, 2500},
		},
		{
			name:  "single installment",
			total: 10, count: 1,
			want: []int64{10},
		},
		{
			name:  "one minor unit over one",
			total: 1, count: 1,
			want: []int64{1},
		},
		{
			name:  "remainder of two cents lands on first",
			total: 5, count: 2,
			want: []int64{3, 2},
		},
		{
			name:  "exactly one minor unit each",
			total: 3, count: 3,
			want: []int64{1, 1, 1},
		},
		{
			name:  "max int64 single",
			total: math.MaxInt64, count: 1,
			want: []int64{math.MaxInt64},
		},
		{
			name:  "large total over 7",
			total: 999999999999, count: 7,
			// 999999999999 = 7*142857142857 + 0, remainder 0.
			want: []int64{142857142857, 142857142857, 142857142857, 142857142857,
				142857142857, 142857142857, 142857142857},
		},
		{
			name:  "large total with remainder over 7",
			total: 100000000000, count: 7,
			// 100000000000 = 7*14285714285 + 5; remainder 5 goes to first.
			want: []int64{14285714290, 14285714285, 14285714285, 14285714285,
				14285714285, 14285714285, 14285714285},
		},
		{
			name:  "zero total rejected",
			total: 0, count: 3,
			wantErr: ErrInvalidInstallmentSplit,
		},
		{
			name:  "fewer cents than installments rejected",
			total: 2, count: 3,
			wantErr: ErrInvalidInstallmentSplit,
		},
		{
			name:  "negative total rejected",
			total: -1, count: 2,
			wantErr: ErrInvalidInstallmentSplit,
		},
		{
			name:  "zero count rejected",
			total: 100, count: 0,
			wantErr: ErrInvalidInstallmentSplit,
		},
		{
			name:  "negative count rejected",
			total: 100, count: -1,
			wantErr: ErrInvalidInstallmentSplit,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := SplitInstallments(tt.total, tt.count)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("SplitInstallments(%d, %d) error = %v, want %v", tt.total, tt.count, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitInstallments(%d, %d) unexpected error: %v", tt.total, tt.count, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SplitInstallments(%d, %d) = %v, want %v", tt.total, tt.count, got, tt.want)
			}

			// Invariant: the parts always sum back to the total.
			var sum int64
			for _, v := range got {
				if v <= 0 {
					t.Fatalf("installment amount %d is not positive", v)
				}
				sum += v
			}
			if sum != tt.total {
				t.Fatalf("sum of installments = %d, want %d", sum, tt.total)
			}
		})
	}
}

func TestBuildSchedule(t *testing.T) {
	t.Parallel()

	firstDue := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	interval := 14 * 24 * time.Hour

	plans, err := BuildSchedule(10000, 4, firstDue, interval)
	if err != nil {
		t.Fatalf("BuildSchedule unexpected error: %v", err)
	}
	if len(plans) != 4 {
		t.Fatalf("got %d plans, want 4", len(plans))
	}

	wantAmounts := []int64{2500, 2500, 2500, 2500}
	for i, p := range plans {
		if p.Number != i+1 {
			t.Errorf("plan[%d].Number = %d, want %d", i, p.Number, i+1)
		}
		if p.Amount != wantAmounts[i] {
			t.Errorf("plan[%d].Amount = %d, want %d", i, p.Amount, wantAmounts[i])
		}
		wantDue := firstDue.Add(time.Duration(i) * interval)
		if !p.DueDate.Equal(wantDue) {
			t.Errorf("plan[%d].DueDate = %s, want %s", i, p.DueDate, wantDue)
		}
	}

	if _, err := BuildSchedule(100, 3, firstDue, -time.Second); !errors.Is(err, ErrInvalidInstallmentSplit) {
		t.Fatalf("negative interval error = %v, want ErrInvalidInstallmentSplit", err)
	}
}
