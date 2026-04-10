package api

import (
	"net/http"
	"strconv"

	"nofx/market"

	"github.com/gin-gonic/gin"
)

// handleKlines returns K-line data for a symbol via Binance Futures API
func (s *Server) handleKlines(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol parameter is required"})
		return
	}

	interval := c.DefaultQuery("interval", "5m")
	exchange := c.DefaultQuery("exchange", "binance")
	limitStr := c.DefaultQuery("limit", "1000")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 1000
	}
	if limit > 1500 {
		limit = 1500
	}

	symbol = market.NormalizeForExchange(symbol, exchange)

	apiClient := market.NewAPIClient()
	klines, err := apiClient.GetKlines(symbol, interval, limit)
	if err != nil {
		SafeInternalError(c, "Get klines from Binance", err)
		return
	}

	c.JSON(http.StatusOK, klines)
}

