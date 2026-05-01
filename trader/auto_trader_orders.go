package trader

import (
	"fmt"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"time"
)

// executeDecisionWithRecord executes AI decision and records detailed information
func (at *AutoTrader) executeDecisionWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	// Paper mode: skip exchange execution; decision still saved to decision_records.
	if at.config.StrategyConfig != nil && at.config.StrategyConfig.PaperMode {
		switch decision.Action {
		case "open_long", "open_short", "add_long", "add_short", "close_long", "close_short":
			logger.Infof("  📝 [paper] %s %s | size=$%.2f lev=%dx SL=%.4f TP=%.4f conf=%d intent=%s",
				decision.Action, decision.Symbol, decision.PositionSizeUSD, decision.Leverage,
				decision.StopLoss, decision.TakeProfit, decision.Confidence, decision.IntentType)
			return nil
		case "hold", "wait":
			return nil
		default:
			return fmt.Errorf("unknown action: %s", decision.Action)
		}
	}

	switch decision.Action {
	case "open_long":
		return at.executeOpenLongWithRecord(decision, actionRecord)
	case "open_short":
		return at.executeOpenShortWithRecord(decision, actionRecord)
	case "add_long":
		return at.executeAddLongWithRecord(decision, actionRecord)
	case "add_short":
		return at.executeAddShortWithRecord(decision, actionRecord)
	case "close_long":
		return at.executeCloseLongWithRecord(decision, actionRecord)
	case "close_short":
		return at.executeCloseShortWithRecord(decision, actionRecord)
	case "hold", "wait":
		// No execution needed, just record
		return nil
	default:
		return fmt.Errorf("unknown action: %s", decision.Action)
	}
}

// executeOpenLongWithRecord executes open long position and records detailed information
func (at *AutoTrader) executeOpenLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  📈 Open long: %s", decision.Symbol)
	execSymbol := market.NormalizeForExchange(decision.Symbol, at.exchange)

	// ⚠️ Get current positions for multiple checks
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	// [CODE ENFORCED] Check max positions limit
	if err := at.enforceMaxPositions(len(positions)); err != nil {
		return err
	}

	// [CODE ENFORCED] Check category concentration limit
	if err := at.enforceMaxSameCategoryPositions(positions, decision.Symbol, "long"); err != nil {
		return err
	}

	// Check if there's already a position in the same symbol and direction
	for _, pos := range positions {
		if pos["symbol"] == execSymbol && pos["side"] == "long" {
			return fmt.Errorf("❌ %s already has long position, close it first", decision.Symbol)
		}
	}

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return err
	}

	// Get balance (needed for multiple checks)
	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("failed to get account balance: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Get equity for position value ratio check
	equity := 0.0
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		equity = eq
	} else if eq, ok := balance["totalWalletBalance"].(float64); ok && eq > 0 {
		equity = eq
	} else {
		equity = availableBalance // Fallback to available balance
	}

	// [CODE ENFORCED] Position Value Ratio Check: position_value <= equity × ratio
	adjustedPositionSize, wasCapped := at.enforcePositionValueRatio(decision.PositionSizeUSD, equity, decision.Symbol)
	if wasCapped {
		decision.PositionSizeUSD = adjustedPositionSize
	}

	// ⚠️ Auto-adjust position size if insufficient margin
	// Formula: totalRequired = positionSize/leverage + positionSize*0.001 + positionSize/leverage*0.01
	//        = positionSize * (1.01/leverage + 0.001)
	marginFactor := 1.01/float64(decision.Leverage) + 0.001
	maxAffordablePositionSize := availableBalance / marginFactor

	actualPositionSize := decision.PositionSizeUSD
	if actualPositionSize > maxAffordablePositionSize {
		// Use 98% of max to leave buffer for price fluctuation
		adjustedSize := maxAffordablePositionSize * 0.98
		logger.Infof("  ⚠️ Position size %.2f exceeds max affordable %.2f, auto-reducing to %.2f",
			actualPositionSize, maxAffordablePositionSize, adjustedSize)
		actualPositionSize = adjustedSize
		decision.PositionSizeUSD = actualPositionSize
	}

	// [CODE ENFORCED] Minimum position size check
	if err := at.enforceMinPositionSize(decision.PositionSizeUSD); err != nil {
		return err
	}

	// Calculate quantity with adjusted position size
	quantity := actualPositionSize / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// Open position
	order, err := at.trader.OpenLong(decision.Symbol, quantity, decision.Leverage)
	if err != nil {
		return err
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	logger.Infof("  ✓ Position opened successfully, order ID: %v, quantity: %.4f", order["orderId"], quantity)

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "open_long", quantity, marketData.CurrentPrice, decision.Leverage, 0)

	// Record position opening time
	posKey := decision.Symbol + "_long"
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	// Buffer AI-assigned intent so it can be persisted once the position row exists
	if decision.IntentType != "" || decision.EntryThesis != "" {
		at.rememberPendingIntent(execSymbol, "long", decision.IntentType, decision.EntryThesis)
	}

	// Set stop loss and take profit
	if err := at.trader.SetStopLoss(decision.Symbol, "LONG", quantity, decision.StopLoss); err != nil {
		logger.Infof("  ⚠ Failed to set stop loss: %v", err)
	}
	if err := at.trader.SetTakeProfit(decision.Symbol, "LONG", quantity, decision.TakeProfit); err != nil {
		logger.Infof("  ⚠ Failed to set take profit: %v", err)
	}

	return nil
}

// executeOpenShortWithRecord executes open short position and records detailed information
func (at *AutoTrader) executeOpenShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  📉 Open short: %s", decision.Symbol)
	execSymbol := market.NormalizeForExchange(decision.Symbol, at.exchange)

	// ⚠️ Get current positions for multiple checks
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	// [CODE ENFORCED] Check max positions limit
	if err := at.enforceMaxPositions(len(positions)); err != nil {
		return err
	}

	// [CODE ENFORCED] Check category concentration limit
	if err := at.enforceMaxSameCategoryPositions(positions, decision.Symbol, "short"); err != nil {
		return err
	}

	// Check if there's already a position in the same symbol and direction
	for _, pos := range positions {
		if pos["symbol"] == execSymbol && pos["side"] == "short" {
			return fmt.Errorf("❌ %s already has short position, close it first", decision.Symbol)
		}
	}

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return err
	}

	// Get balance (needed for multiple checks)
	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("failed to get account balance: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Get equity for position value ratio check
	equity := 0.0
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		equity = eq
	} else if eq, ok := balance["totalWalletBalance"].(float64); ok && eq > 0 {
		equity = eq
	} else {
		equity = availableBalance // Fallback to available balance
	}

	// [CODE ENFORCED] Position Value Ratio Check: position_value <= equity × ratio
	adjustedPositionSize, wasCapped := at.enforcePositionValueRatio(decision.PositionSizeUSD, equity, decision.Symbol)
	if wasCapped {
		decision.PositionSizeUSD = adjustedPositionSize
	}

	// ⚠️ Auto-adjust position size if insufficient margin
	// Formula: totalRequired = positionSize/leverage + positionSize*0.001 + positionSize/leverage*0.01
	//        = positionSize * (1.01/leverage + 0.001)
	marginFactor := 1.01/float64(decision.Leverage) + 0.001
	maxAffordablePositionSize := availableBalance / marginFactor

	actualPositionSize := decision.PositionSizeUSD
	if actualPositionSize > maxAffordablePositionSize {
		// Use 98% of max to leave buffer for price fluctuation
		adjustedSize := maxAffordablePositionSize * 0.98
		logger.Infof("  ⚠️ Position size %.2f exceeds max affordable %.2f, auto-reducing to %.2f",
			actualPositionSize, maxAffordablePositionSize, adjustedSize)
		actualPositionSize = adjustedSize
		decision.PositionSizeUSD = actualPositionSize
	}

	// [CODE ENFORCED] Minimum position size check
	if err := at.enforceMinPositionSize(decision.PositionSizeUSD); err != nil {
		return err
	}

	// Calculate quantity with adjusted position size
	quantity := actualPositionSize / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// Open position
	order, err := at.trader.OpenShort(decision.Symbol, quantity, decision.Leverage)
	if err != nil {
		return err
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	logger.Infof("  ✓ Position opened successfully, order ID: %v, quantity: %.4f", order["orderId"], quantity)

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "open_short", quantity, marketData.CurrentPrice, decision.Leverage, 0)

	// Record position opening time
	posKey := decision.Symbol + "_short"
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	// Buffer AI-assigned intent so it can be persisted once the position row exists
	if decision.IntentType != "" || decision.EntryThesis != "" {
		at.rememberPendingIntent(execSymbol, "short", decision.IntentType, decision.EntryThesis)
	}

	// Set stop loss and take profit
	if err := at.trader.SetStopLoss(decision.Symbol, "SHORT", quantity, decision.StopLoss); err != nil {
		logger.Infof("  ⚠ Failed to set stop loss: %v", err)
	}
	if err := at.trader.SetTakeProfit(decision.Symbol, "SHORT", quantity, decision.TakeProfit); err != nil {
		logger.Infof("  ⚠ Failed to set take profit: %v", err)
	}

	return nil
}

// executeAddLongWithRecord scales into an existing same-side long position.
// AI's leverage and intent_type are ignored — the existing position's metadata is preserved.
// SL/TP from the decision override existing exchange orders to cover the new total quantity.
func (at *AutoTrader) executeAddLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	return at.executeAddPositionWithRecord(decision, actionRecord, "long")
}

// executeAddShortWithRecord scales into an existing same-side short position.
func (at *AutoTrader) executeAddShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	return at.executeAddPositionWithRecord(decision, actionRecord, "short")
}

// executeAddPositionWithRecord shared logic for add_long / add_short.
func (at *AutoTrader) executeAddPositionWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction, side string) error {
	icon := "📈➕"
	posSideStr := "LONG"
	actionStr := "add_long"
	if side == "short" {
		icon = "📉➕"
		posSideStr = "SHORT"
		actionStr = "add_short"
	}
	logger.Infof("  %s Add %s: %s", icon, side, decision.Symbol)
	execSymbol := market.NormalizeForExchange(decision.Symbol, at.exchange)

	// 1. Find existing same-side position on exchange (gate)
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}
	var existing map[string]interface{}
	for _, pos := range positions {
		if pos["symbol"] == execSymbol && pos["side"] == side {
			existing = pos
			break
		}
	}
	if existing == nil {
		return fmt.Errorf("❌ %s has no existing %s position to add to — use open_%s instead", decision.Symbol, side, side)
	}

	existingQtyAbs := existing["positionAmt"].(float64)
	if existingQtyAbs < 0 {
		existingQtyAbs = -existingQtyAbs
	}
	existingEntry, _ := existing["entryPrice"].(float64)
	existingLeverage := decision.Leverage
	if lev, ok := existing["leverage"].(float64); ok && lev > 0 {
		existingLeverage = int(lev)
	}

	// 2. Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return err
	}

	// 3. Get balance / equity
	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("failed to get account balance: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}
	equity := 0.0
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		equity = eq
	} else if eq, ok := balance["totalWalletBalance"].(float64); ok && eq > 0 {
		equity = eq
	} else {
		equity = availableBalance
	}

	// 4. Cap by POST-ADD total notional (key difference from open_*)
	maxPosRatio := at.config.StrategyConfig.RiskControl.EffectiveMaxPositionValueRatio()
	maxAllowedTotal := equity * maxPosRatio
	existingNotional := existingQtyAbs * marketData.CurrentPrice
	requestedIncrement := decision.PositionSizeUSD
	postAddTotal := existingNotional + requestedIncrement
	adjustedIncrement := requestedIncrement
	if postAddTotal > maxAllowedTotal {
		adjustedIncrement = maxAllowedTotal - existingNotional
		if adjustedIncrement <= 0 {
			return fmt.Errorf("❌ %s position already at cap (existing %.2f USDT >= max %.2f USDT, ratio %.2f), cannot add",
				decision.Symbol, existingNotional, maxAllowedTotal, maxPosRatio)
		}
		logger.Infof("  ⚠️ Add increment capped %.2f → %.2f to keep total ≤ %.2f USDT (existing %.2f + add)",
			requestedIncrement, adjustedIncrement, maxAllowedTotal, existingNotional)
		decision.PositionSizeUSD = adjustedIncrement
	}

	// 5. Margin sufficiency on the increment
	marginFactor := 1.01/float64(existingLeverage) + 0.001
	maxAffordable := availableBalance / marginFactor
	if adjustedIncrement > maxAffordable {
		shrunk := maxAffordable * 0.98
		logger.Infof("  ⚠️ Add increment %.2f exceeds max affordable %.2f, auto-reducing to %.2f",
			adjustedIncrement, maxAffordable, shrunk)
		adjustedIncrement = shrunk
		decision.PositionSizeUSD = adjustedIncrement
	}

	// 6. Min size (applied to the increment, same threshold as opens)
	if err := at.enforceMinPositionSize(adjustedIncrement); err != nil {
		return err
	}

	// 7. Place market order for the increment
	addQuantity := adjustedIncrement / marketData.CurrentPrice
	actionRecord.Quantity = addQuantity
	actionRecord.Price = marketData.CurrentPrice

	var order map[string]interface{}
	if side == "long" {
		order, err = at.trader.OpenLong(decision.Symbol, addQuantity, existingLeverage)
	} else {
		order, err = at.trader.OpenShort(decision.Symbol, addQuantity, existingLeverage)
	}
	if err != nil {
		return err
	}
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	newQty := existingQtyAbs + addQuantity
	newAvg := (existingEntry*existingQtyAbs + marketData.CurrentPrice*addQuantity) / newQty
	logger.Infof("  ✓ Added: order ID %v, +qty %.4f @ %.4f → total %.4f, avg %.4f → %.4f (lev %dx)",
		order["orderId"], addQuantity, marketData.CurrentPrice, newQty, existingEntry, newAvg, existingLeverage)

	// 8. Record order — for Binance, recordAndConfirmOrder returns immediately and OrderSync
	//    handles trader_orders + posBuilder.handleOpen merging via UpdatePositionQuantityAndPrice.
	//    Do NOT manually call UpdatePositionQuantityAndPrice here — it would double-count
	//    against OrderSync's own merge (root cause of the v7 NVDA stale-quantity bug).
	at.recordAndConfirmOrder(order, decision.Symbol, actionStr, addQuantity, marketData.CurrentPrice, existingLeverage, 0)

	// 9. Replace SL/TP — cancel existing standing orders, place new ones for the NEW total qty.
	//     If AI omitted SL or TP, skip placing that side (leave naked, AI will set on a later cycle).
	if decision.StopLoss > 0 || decision.TakeProfit > 0 {
		if err := at.trader.CancelAllOrders(decision.Symbol); err != nil {
			logger.Infof("  ⚠ Failed to cancel existing SL/TP before add: %v (will still attempt to set new)", err)
		}
		if decision.StopLoss > 0 {
			if err := at.trader.SetStopLoss(decision.Symbol, posSideStr, newQty, decision.StopLoss); err != nil {
				logger.Infof("  ⚠ Failed to set new stop loss after add: %v", err)
			}
		}
		if decision.TakeProfit > 0 {
			if err := at.trader.SetTakeProfit(decision.Symbol, posSideStr, newQty, decision.TakeProfit); err != nil {
				logger.Infof("  ⚠ Failed to set new take profit after add: %v", err)
			}
		}
	}

	// intent_type / entry_thesis intentionally NOT updated — the original position's metadata is preserved.

	return nil
}

// executeCloseLongWithRecord executes close long position and records detailed information
func (at *AutoTrader) executeCloseLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close long: %s", decision.Symbol)

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// Normalize symbol for database lookup
	normalizedSymbol := market.NormalizeForExchange(decision.Symbol, at.exchange)

	// Get entry price and quantity - prioritize local database for accurate quantity
	var entryPrice float64
	var quantity float64

	// First try to get from local database (more accurate for quantity)
	if at.store != nil {
		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "LONG"); err == nil && openPos != nil {
			quantity = openPos.Quantity
			entryPrice = openPos.EntryPrice
			logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
		}
	}

	// Fallback to exchange API if local data not found
	if quantity == 0 {
		positions, err := at.trader.GetPositions()
		if err == nil {
			for _, pos := range positions {
				if pos["symbol"] == normalizedSymbol && pos["side"] == "long" {
					if ep, ok := pos["entryPrice"].(float64); ok {
						entryPrice = ep
					}
					if amt, ok := pos["positionAmt"].(float64); ok && amt > 0 {
						quantity = amt
					}
					break
				}
			}
		}
		logger.Infof("  📊 Using exchange position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
	}

	// Close position
	order, err := at.trader.CloseLong(decision.Symbol, 0) // 0 = close all
	if err != nil {
		return err
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "close_long", quantity, marketData.CurrentPrice, 0, entryPrice)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}

// executeCloseShortWithRecord executes close short position and records detailed information
func (at *AutoTrader) executeCloseShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close short: %s", decision.Symbol)

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// Normalize symbol for database lookup
	normalizedSymbol := market.NormalizeForExchange(decision.Symbol, at.exchange)

	// Get entry price and quantity - prioritize local database for accurate quantity
	var entryPrice float64
	var quantity float64

	// First try to get from local database (more accurate for quantity)
	if at.store != nil {
		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "SHORT"); err == nil && openPos != nil {
			quantity = openPos.Quantity
			entryPrice = openPos.EntryPrice
			logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
		}
	}

	// Fallback to exchange API if local data not found
	if quantity == 0 {
		positions, err := at.trader.GetPositions()
		if err == nil {
			for _, pos := range positions {
				if pos["symbol"] == normalizedSymbol && pos["side"] == "short" {
					if ep, ok := pos["entryPrice"].(float64); ok {
						entryPrice = ep
					}
					if amt, ok := pos["positionAmt"].(float64); ok {
						quantity = -amt // positionAmt is negative for short
					}
					break
				}
			}
		}
		logger.Infof("  📊 Using exchange position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
	}

	// Close position
	order, err := at.trader.CloseShort(decision.Symbol, 0) // 0 = close all
	if err != nil {
		return err
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "close_short", quantity, marketData.CurrentPrice, 0, entryPrice)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}

// pendingIntentKey builds the map key used for pending position intents.
// Uses the exchange symbol form and lowercase side to match the position
// iteration in buildTradingContext.
func pendingIntentKey(symbol, side string) string {
	return symbol + "_" + side
}

// rememberPendingIntent stores an AI-assigned intent that will be persisted
// to the trader_positions row once it becomes visible (OrderSync creates the
// position row asynchronously on Binance).
func (at *AutoTrader) rememberPendingIntent(symbol, side, intentType, entryThesis string) {
	at.pendingIntentsMutex.Lock()
	defer at.pendingIntentsMutex.Unlock()
	at.pendingIntents[pendingIntentKey(symbol, side)] = pendingPositionIntent{
		IntentType:  intentType,
		EntryThesis: entryThesis,
	}
}

// consumePendingIntent returns and removes a pending intent, if any.
func (at *AutoTrader) consumePendingIntent(symbol, side string) (pendingPositionIntent, bool) {
	at.pendingIntentsMutex.Lock()
	defer at.pendingIntentsMutex.Unlock()
	key := pendingIntentKey(symbol, side)
	intent, ok := at.pendingIntents[key]
	if ok {
		delete(at.pendingIntents, key)
	}
	return intent, ok
}
