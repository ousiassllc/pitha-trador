package marketcalendar_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

func TestPhaseAt(t *testing.T) {
	tests := []struct {
		name string
		at   time.Time
		want marketcalendar.Phase
	}{
		{"before morning open", jst(2026, 9, 29, 8, 59), marketcalendar.PhaseClosed},
		{"前場寄り", jst(2026, 9, 29, 9, 0), marketcalendar.PhaseOpeningAuction},
		{"前場ザラ場の最初の分", jst(2026, 9, 29, 9, 1), marketcalendar.PhaseContinuous},
		{"前場の最終分は引けのオークションではない", jst(2026, 9, 29, 11, 29), marketcalendar.PhaseContinuous},
		{"昼休み開始", jst(2026, 9, 29, 11, 30), marketcalendar.PhaseClosed},
		{"昼休み中", jst(2026, 9, 29, 12, 0), marketcalendar.PhaseClosed},
		{"昼休み終了直前", jst(2026, 9, 29, 12, 29), marketcalendar.PhaseClosed},
		{"後場寄り", jst(2026, 9, 29, 12, 30), marketcalendar.PhaseOpeningAuction},
		{"後場ザラ場", jst(2026, 9, 29, 12, 31), marketcalendar.PhaseContinuous},
		{"ザラ場の最終分", jst(2026, 9, 29, 15, 24), marketcalendar.PhaseContinuous},
		{"引けのクロージング・オークション開始", jst(2026, 9, 29, 15, 25), marketcalendar.PhaseClosingAuction},
		{"大引け直前", jst(2026, 9, 29, 15, 29), marketcalendar.PhaseClosingAuction},
		{"大引け", jst(2026, 9, 29, 15, 30), marketcalendar.PhaseClosed},
		{"土曜", jst(2026, 10, 3, 10, 0), marketcalendar.PhaseClosed},
		{"祝日の寄り", jst(2026, 1, 12, 9, 0), marketcalendar.PhaseClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := marketcalendar.TSE.PhaseAt(tt.at); got != tt.want {
				t.Fatalf("PhaseAt(%v) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}
