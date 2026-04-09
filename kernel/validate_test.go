package kernel

import (
	"testing"
)

// TestLeverageFallback tests automatic correction when leverage exceeds limit
func TestLeverageFallback(t *testing.T) {
	tests := []struct {
		name         string
		decision     Decision
		accountEquity float64
		maxLeverage  int
		maxPosRatio  float64
		wantLeverage int // Expected leverage after correction
		wantError    bool
	}{
		{
			name: "Leverage exceeded - auto-correct to limit",
			decision: Decision{
				Symbol:          "TSLAUSDT",
				Action:          "open_long",
				Leverage:        20, // Exceeds limit
				PositionSizeUSD: 100,
				StopLoss:        200,
				TakeProfit:      300,
			},
			accountEquity: 1000,
			maxLeverage:   5,
			maxPosRatio:   0.5,
			wantLeverage:  5, // Should be corrected to 5
			wantError:     false,
		},
		{
			name: "Leverage within limit - no correction",
			decision: Decision{
				Symbol:          "XAUUSDT",
				Action:          "open_short",
				Leverage:        3, // Not exceeded
				PositionSizeUSD: 400,
				StopLoss:        3500,
				TakeProfit:      3000,
			},
			accountEquity: 1000,
			maxLeverage:   5,
			maxPosRatio:   0.5,
			wantLeverage:  3, // Stays unchanged
			wantError:     false,
		},
		{
			name: "Leverage is 0 - should error",
			decision: Decision{
				Symbol:          "QQQUSDT",
				Action:          "open_long",
				Leverage:        0, // Invalid
				PositionSizeUSD: 100,
				StopLoss:        400,
				TakeProfit:      500,
			},
			accountEquity: 1000,
			maxLeverage:   5,
			maxPosRatio:   0.5,
			wantLeverage:  0,
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDecision(&tt.decision, tt.accountEquity, tt.maxLeverage, tt.maxPosRatio)

			// Check error status
			if (err != nil) != tt.wantError {
				t.Errorf("validateDecision() error = %v, wantError %v", err, tt.wantError)
				return
			}

			// If shouldn't error, check if leverage was correctly corrected
			if !tt.wantError && tt.decision.Leverage != tt.wantLeverage {
				t.Errorf("Leverage not corrected: got %d, want %d", tt.decision.Leverage, tt.wantLeverage)
			}
		})
	}
}
