package nofxos

import (
	"encoding/json"
	"fmt"
	"log"
)

// ============================================================================
// AI500 Top Rated Coins
// ============================================================================

// AI500Response is the API response for top rated coins
type AI500Response struct {
	Success bool `json:"success"`
	Data    struct {
		Coins []AI500Coin `json:"coins"`
	} `json:"data"`
}

// AI500Coin represents a single coin from the AI500 ranking
type AI500Coin struct {
	Symbol string  `json:"symbol"`
	Score  float64 `json:"score"`
	Rank   int     `json:"rank"`
}

// GetTopRatedCoins retrieves top-rated coins from AI500
func (c *Client) GetTopRatedCoins(limit int) ([]string, error) {
	if limit <= 0 {
		limit = 30
	}

	endpoint := fmt.Sprintf("/api/ai500/top?limit=%d", limit)

	body, err := c.doRequest(endpoint)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	var response AI500Response
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON parsing failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned failure status")
	}

	var symbols []string
	for _, coin := range response.Data.Coins {
		symbols = append(symbols, coin.Symbol)
	}

	log.Printf("✓ Fetched AI500 top %d coins", len(symbols))
	return symbols, nil
}

// ============================================================================
// OI Positions (Top / Low)
// ============================================================================

// OIPositionItem represents a single OI position entry
type OIPositionItem struct {
	Symbol       string  `json:"symbol"`
	OI           float64 `json:"oi"`
	OIDelta      float64 `json:"oi_delta"`
	OIDeltaValue float64 `json:"oi_delta_value"`
	OIDeltaPct   float64 `json:"oi_delta_percent"`
}

// OIPositionsResponse is the API response for OI positions
type OIPositionsResponse struct {
	Success bool             `json:"success"`
	Data    []OIPositionItem `json:"data"`
}

// GetOITopPositions retrieves positions with the highest OI increase
func (c *Client) GetOITopPositions() ([]OIPositionItem, error) {
	endpoint := "/api/oi/top"

	body, err := c.doRequest(endpoint)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	var response OIPositionsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON parsing failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned failure status")
	}

	log.Printf("✓ Fetched OI top positions: %d", len(response.Data))
	return response.Data, nil
}

// GetOILowPositions retrieves positions with the highest OI decrease
func (c *Client) GetOILowPositions() ([]OIPositionItem, error) {
	endpoint := "/api/oi/low"

	body, err := c.doRequest(endpoint)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	var response OIPositionsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON parsing failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned failure status")
	}

	log.Printf("✓ Fetched OI low positions: %d", len(response.Data))
	return response.Data, nil
}
