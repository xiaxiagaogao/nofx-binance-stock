package api

import (
	"net/http"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/logger"
	"nofx/store"

	"github.com/gin-gonic/gin"
)

// handleMacroThesis returns the latest macro thesis for a trader
func (s *Server) handleMacroThesis(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	st := trader.GetStore()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"thesis": nil})
		return
	}

	thesis, err := st.MacroThesis().GetLatest(trader.GetID())
	if err != nil {
		logger.Infof("⚠️ Failed to get macro thesis: %v", err)
		c.JSON(http.StatusOK, gin.H{"thesis": nil})
		return
	}

	if thesis == nil {
		c.JSON(http.StatusOK, gin.H{"thesis": nil})
		return
	}

	// Build response with parsed JSON fields
	sectorBias := thesis.ParseSectorBias()
	keyRisks := thesis.ParseKeyRisks()

	ageHours := time.Since(thesis.UpdatedAt).Hours()

	c.JSON(http.StatusOK, gin.H{
		"thesis": gin.H{
			"id":               thesis.ID,
			"market_regime":    thesis.MarketRegime,
			"thesis_text":      thesis.ThesisText,
			"sector_bias":      sectorBias,
			"key_risks":        keyRisks,
			"portfolio_intent": thesis.PortfolioIntent,
			"valid_hours":      thesis.ValidHours,
			"source":           thesis.Source,
			"age_hours":        ageHours,
			"is_stale":         thesis.IsStale(),
			"created_at":       thesis.CreatedAt.Format(time.RFC3339),
			"updated_at":       thesis.UpdatedAt.Format(time.RFC3339),
		},
	})
}

// handleCreateMacroThesis allows manual creation of a macro thesis
func (s *Server) handleCreateMacroThesis(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	st := trader.GetStore()
	if st == nil {
		SafeInternalError(c, "Store not available", nil)
		return
	}

	var req struct {
		MarketRegime    string            `json:"market_regime"`
		ThesisText      string            `json:"thesis_text"`
		SectorBias      map[string]string `json:"sector_bias"`
		KeyRisks        []string          `json:"key_risks"`
		PortfolioIntent string            `json:"portfolio_intent"`
		ValidHours      int               `json:"valid_hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request body")
		return
	}

	if req.ThesisText == "" {
		SafeBadRequest(c, "thesis_text is required")
		return
	}

	validHours := req.ValidHours
	if validHours <= 0 {
		validHours = 24
	}

	thesis := &store.MacroThesis{
		TraderID:        trader.GetID(),
		MarketRegime:    req.MarketRegime,
		ThesisText:      req.ThesisText,
		SectorBias:      store.EncodeSectorBias(req.SectorBias),
		KeyRisks:        store.EncodeKeyRisks(req.KeyRisks),
		PortfolioIntent: req.PortfolioIntent,
		ValidHours:      validHours,
		Source:          "manual",
	}

	if err := st.MacroThesis().Create(thesis); err != nil {
		SafeInternalError(c, "Create macro thesis", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "id": thesis.ID})
}

// handlePortfolioExposure returns current portfolio exposure metrics
func (s *Server) handlePortfolioExposure(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	// Get positions from exchange
	positions, err := trader.GetPositions()
	if err != nil {
		SafeInternalError(c, "Get positions", err)
		return
	}

	// Get strategy config for category mapping
	var riskConfig store.RiskControlConfig
	if sc := trader.GetStrategyConfig(); sc != nil {
		riskConfig = sc.RiskControl
	}
	// Fill defaults if no config
	if riskConfig.SymbolCategories == nil {
		defaults := store.GetDefaultStrategyConfig()
		riskConfig.SymbolCategories = defaults.RiskControl.SymbolCategories
	}

	// Build position infos for exposure calculation
	var posInfos []kernel.PositionInfo
	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		markPrice, _ := pos["mark_price"].(float64)
		quantity, _ := pos["quantity"].(float64)

		intentType := ""
		if it, ok := pos["intent_type"].(string); ok {
			intentType = it
		}

		posInfos = append(posInfos, kernel.PositionInfo{
			Symbol:     symbol,
			Side:       side,
			MarkPrice:  markPrice,
			Quantity:   quantity,
			IntentType: intentType,
		})
	}

	// Calculate portfolio exposure (reuse the same logic as the trading loop)
	exposure := computePortfolioExposure(posInfos, riskConfig)

	// Get session info
	session := kernel.GetUSTradingSession(time.Now().UTC())
	sessionScale := riskConfig.GetSessionRiskScale(session)

	c.JSON(http.StatusOK, gin.H{
		"exposure":             exposure,
		"session":              session,
		"session_scale_factor": sessionScale,
	})
}

// computePortfolioExposure calculates portfolio-level exposure from position infos.
// Mirrors calculatePortfolioExposure in auto_trader_loop.go.
func computePortfolioExposure(positions []kernel.PositionInfo, riskConfig store.RiskControlConfig) *kernel.PortfolioExposure {
	if len(positions) == 0 {
		return &kernel.PortfolioExposure{
			CategoryBreakdown: make(map[string]float64),
			NetDirection:      "balanced",
		}
	}

	exp := &kernel.PortfolioExposure{
		CategoryBreakdown: make(map[string]float64),
	}

	for _, p := range positions {
		notional := p.Quantity * p.MarkPrice
		cat := riskConfig.GetSymbolCategory(p.Symbol)
		if cat == "" {
			cat = "other"
		}
		exp.CategoryBreakdown[cat] += notional

		if strings.EqualFold(p.Side, "long") {
			exp.NetLongUSD += notional
		} else {
			exp.NetShortUSD += notional
		}

		switch p.IntentType {
		case "core_beta":
			exp.CoreBetaUSD += notional
		case "tactical_alpha":
			exp.TacticalAlphaUSD += notional
		case "hedge":
			exp.HedgeUSD += notional
		}
	}

	net := exp.NetLongUSD - exp.NetShortUSD
	total := exp.NetLongUSD + exp.NetShortUSD
	switch {
	case total > 0 && net/total > 0.2:
		exp.NetDirection = "net_long"
	case total > 0 && net/total < -0.2:
		exp.NetDirection = "net_short"
	default:
		exp.NetDirection = "balanced"
	}

	return exp
}
