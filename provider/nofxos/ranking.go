package nofxos

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// ============================================================================
// OI Ranking
// ============================================================================

// OIRankingItem represents a single coin's OI ranking entry
type OIRankingItem struct {
	Symbol       string  `json:"symbol"`
	OI           float64 `json:"oi"`
	OIDelta      float64 `json:"oi_delta"`
	OIDeltaValue float64 `json:"oi_delta_value"`
	OIDeltaPct   float64 `json:"oi_delta_percent"`
	Price        float64 `json:"price"`
	PriceDelta   float64 `json:"price_delta"`
}

// OIRankingData contains market-wide OI ranking data
type OIRankingData struct {
	TopPositions []OIRankingItem `json:"top_positions"`
	LowPositions []OIRankingItem `json:"low_positions"`
	Duration     string          `json:"duration"`
	FetchedAt    time.Time       `json:"fetched_at"`
}

// OIRankingResponse is the API response structure
type OIRankingResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Duration string          `json:"duration"`
		Top      []OIRankingItem `json:"top"`
		Low      []OIRankingItem `json:"low"`
	} `json:"data"`
}

// GetOIRanking retrieves market-wide OI ranking data
func (c *Client) GetOIRanking(duration string, limit int) (*OIRankingData, error) {
	if duration == "" {
		duration = "1h"
	}
	if limit <= 0 {
		limit = 10
	}

	endpoint := fmt.Sprintf("/api/oi/ranking?duration=%s&limit=%d", duration, limit)

	body, err := c.doRequest(endpoint)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	var response OIRankingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON parsing failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned failure status")
	}

	result := &OIRankingData{
		TopPositions: response.Data.Top,
		LowPositions: response.Data.Low,
		Duration:     duration,
		FetchedAt:    time.Now(),
	}

	log.Printf("✓ Fetched OI ranking: %d top, %d low", len(result.TopPositions), len(result.LowPositions))
	return result, nil
}

// FormatOIRankingForAI formats OI ranking data for AI consumption
func FormatOIRankingForAI(data *OIRankingData, lang Language) string {
	if data == nil {
		return ""
	}
	if lang == LangChinese {
		return formatOIRankingZH(data)
	}
	return formatOIRankingEN(data)
}

func formatOIRankingZH(data *OIRankingData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## OI 排行 (%s)\n\n", data.Duration))

	if len(data.TopPositions) > 0 {
		sb.WriteString("**OI 增长最多**\n")
		sb.WriteString("| 币种 | OI变化值 | OI变化% | 价格变化 |\n")
		sb.WriteString("|------|---------|---------|----------|\n")
		for _, item := range data.TopPositions {
			sb.WriteString(fmt.Sprintf("| %s | %s | %+.2f%% | %+.2f%% |\n",
				item.Symbol, formatValue(item.OIDeltaValue), item.OIDeltaPct, item.PriceDelta*100))
		}
		sb.WriteString("\n")
	}

	if len(data.LowPositions) > 0 {
		sb.WriteString("**OI 减少最多**\n")
		sb.WriteString("| 币种 | OI变化值 | OI变化% | 价格变化 |\n")
		sb.WriteString("|------|---------|---------|----------|\n")
		for _, item := range data.LowPositions {
			sb.WriteString(fmt.Sprintf("| %s | %s | %.2f%% | %+.2f%% |\n",
				item.Symbol, formatValue(item.OIDeltaValue), item.OIDeltaPct, item.PriceDelta*100))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func formatOIRankingEN(data *OIRankingData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## OI Ranking (%s)\n\n", data.Duration))

	if len(data.TopPositions) > 0 {
		sb.WriteString("**Top OI Increase**\n")
		sb.WriteString("| Symbol | OI Change | OI Change% | Price Change |\n")
		sb.WriteString("|--------|-----------|------------|-------------|\n")
		for _, item := range data.TopPositions {
			sb.WriteString(fmt.Sprintf("| %s | %s | %+.2f%% | %+.2f%% |\n",
				item.Symbol, formatValue(item.OIDeltaValue), item.OIDeltaPct, item.PriceDelta*100))
		}
		sb.WriteString("\n")
	}

	if len(data.LowPositions) > 0 {
		sb.WriteString("**Top OI Decrease**\n")
		sb.WriteString("| Symbol | OI Change | OI Change% | Price Change |\n")
		sb.WriteString("|--------|-----------|------------|-------------|\n")
		for _, item := range data.LowPositions {
			sb.WriteString(fmt.Sprintf("| %s | %s | %.2f%% | %+.2f%% |\n",
				item.Symbol, formatValue(item.OIDeltaValue), item.OIDeltaPct, item.PriceDelta*100))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ============================================================================
// NetFlow Ranking
// ============================================================================

// NetFlowRankingItem represents a single coin's fund flow ranking entry
type NetFlowRankingItem struct {
	Symbol    string  `json:"symbol"`
	FlowValue float64 `json:"flow_value"`
	Price     float64 `json:"price"`
}

// NetFlowRankingData contains market-wide fund flow ranking data
type NetFlowRankingData struct {
	InstitutionFutureTop []NetFlowRankingItem `json:"institution_future_top"`
	InstitutionFutureLow []NetFlowRankingItem `json:"institution_future_low"`
	PersonalFutureTop    []NetFlowRankingItem `json:"personal_future_top"`
	PersonalFutureLow    []NetFlowRankingItem `json:"personal_future_low"`
	Duration             string               `json:"duration"`
	FetchedAt            time.Time            `json:"fetched_at"`
}

// NetFlowRankingResponse is the API response structure
type NetFlowRankingResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Duration             string               `json:"duration"`
		InstitutionFutureTop []NetFlowRankingItem `json:"institution_future_top"`
		InstitutionFutureLow []NetFlowRankingItem `json:"institution_future_low"`
		PersonalFutureTop    []NetFlowRankingItem `json:"personal_future_top"`
		PersonalFutureLow    []NetFlowRankingItem `json:"personal_future_low"`
	} `json:"data"`
}

// GetNetFlowRanking retrieves market-wide fund flow ranking data
func (c *Client) GetNetFlowRanking(duration string, limit int) (*NetFlowRankingData, error) {
	if duration == "" {
		duration = "1h"
	}
	if limit <= 0 {
		limit = 10
	}

	endpoint := fmt.Sprintf("/api/netflow/ranking?duration=%s&limit=%d", duration, limit)

	body, err := c.doRequest(endpoint)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	var response NetFlowRankingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON parsing failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned failure status")
	}

	result := &NetFlowRankingData{
		InstitutionFutureTop: response.Data.InstitutionFutureTop,
		InstitutionFutureLow: response.Data.InstitutionFutureLow,
		PersonalFutureTop:    response.Data.PersonalFutureTop,
		PersonalFutureLow:    response.Data.PersonalFutureLow,
		Duration:             duration,
		FetchedAt:            time.Now(),
	}

	log.Printf("✓ Fetched NetFlow ranking: inst_in=%d, inst_out=%d",
		len(result.InstitutionFutureTop), len(result.InstitutionFutureLow))
	return result, nil
}

// FormatNetFlowRankingForAI formats NetFlow ranking data for AI consumption
func FormatNetFlowRankingForAI(data *NetFlowRankingData, lang Language) string {
	if data == nil {
		return ""
	}
	if lang == LangChinese {
		return formatNetFlowRankingZH(data)
	}
	return formatNetFlowRankingEN(data)
}

func formatNetFlowRankingZH(data *NetFlowRankingData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 资金流排行 (%s)\n\n", data.Duration))

	if len(data.InstitutionFutureTop) > 0 {
		sb.WriteString("**机构资金流入**\n")
		for _, item := range data.InstitutionFutureTop {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Symbol, formatValue(item.FlowValue)))
		}
		sb.WriteString("\n")
	}

	if len(data.InstitutionFutureLow) > 0 {
		sb.WriteString("**机构资金流出**\n")
		for _, item := range data.InstitutionFutureLow {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Symbol, formatValue(item.FlowValue)))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func formatNetFlowRankingEN(data *NetFlowRankingData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Fund Flow Ranking (%s)\n\n", data.Duration))

	if len(data.InstitutionFutureTop) > 0 {
		sb.WriteString("**Institution Inflow**\n")
		for _, item := range data.InstitutionFutureTop {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Symbol, formatValue(item.FlowValue)))
		}
		sb.WriteString("\n")
	}

	if len(data.InstitutionFutureLow) > 0 {
		sb.WriteString("**Institution Outflow**\n")
		for _, item := range data.InstitutionFutureLow {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Symbol, formatValue(item.FlowValue)))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
