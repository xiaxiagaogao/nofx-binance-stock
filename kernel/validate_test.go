package kernel

import (
	"strings"
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

// TestAddActionsValidation covers add_long / add_short — same field requirements as
// open_long / open_short, plus the SL/TP direction logic must treat add_long as long-side.
func TestAddActionsValidation(t *testing.T) {
	tests := []struct {
		name      string
		decision  Decision
		wantError bool
		errSubstr string // optional: a substring expected in the error message
	}{
		{
			name: "add_long valid — same shape as open_long",
			decision: Decision{
				Symbol:          "NVDAUSDT",
				Action:          "add_long",
				Leverage:        5,
				PositionSizeUSD: 30,
				StopLoss:        200,
				TakeProfit:      240,
			},
			wantError: false,
		},
		{
			name: "add_short valid — same shape as open_short",
			decision: Decision{
				Symbol:          "METAUSDT",
				Action:          "add_short",
				Leverage:        5,
				PositionSizeUSD: 30,
				StopLoss:        700,
				TakeProfit:      600,
			},
			wantError: false,
		},
		{
			name: "add_long with inverted SL/TP — should fail like open_long",
			decision: Decision{
				Symbol:          "NVDAUSDT",
				Action:          "add_long",
				Leverage:        5,
				PositionSizeUSD: 30,
				StopLoss:        240, // SL above TP — invalid for long
				TakeProfit:      200,
			},
			wantError: true,
			errSubstr: "long",
		},
		{
			name: "add_short with inverted SL/TP — should fail like open_short",
			decision: Decision{
				Symbol:          "METAUSDT",
				Action:          "add_short",
				Leverage:        5,
				PositionSizeUSD: 30,
				StopLoss:        600, // SL below TP — invalid for short
				TakeProfit:      700,
			},
			wantError: true,
			errSubstr: "short",
		},
		{
			name: "add_long below min position size",
			decision: Decision{
				Symbol:          "NVDAUSDT",
				Action:          "add_long",
				Leverage:        5,
				PositionSizeUSD: 5, // Below 12 USDT min
				StopLoss:        200,
				TakeProfit:      240,
			},
			wantError: true,
			errSubstr: "too small",
		},
		{
			name: "add_long with zero leverage",
			decision: Decision{
				Symbol:          "NVDAUSDT",
				Action:          "add_long",
				Leverage:        0,
				PositionSizeUSD: 30,
				StopLoss:        200,
				TakeProfit:      240,
			},
			wantError: true,
			errSubstr: "leverage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDecision(&tt.decision, 1000, 10, 0.5)
			if (err != nil) != tt.wantError {
				t.Errorf("validateDecision() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if tt.wantError && tt.errSubstr != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q does not contain expected substring %q", err.Error(), tt.errSubstr)
				}
			}
		})
	}
}
