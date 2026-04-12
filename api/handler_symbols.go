package api

import (
	"net/http"

	"nofx/market"

	"github.com/gin-gonic/gin"
)

// handleSymbols returns available trading symbols from Binance Futures
func (s *Server) handleSymbols(c *gin.Context) {
	apiClient := market.NewAPIClient()
	info, err := apiClient.GetExchangeInfo()
	if err != nil {
		SafeInternalError(c, "Get exchange info from Binance", err)
		return
	}

	// Return only TRADING symbols
	var symbols []market.SymbolInfo
	for _, sym := range info.Symbols {
		if sym.Status == "TRADING" {
			symbols = append(symbols, sym)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"symbols": symbols,
		"total":   len(symbols),
	})
}
