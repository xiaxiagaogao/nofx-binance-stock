package store

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// MacroThesis stores the AI fund manager's current macroeconomic market thesis per trader.
// A new record is created each time the AI updates its view; history is preserved.
type MacroThesis struct {
	ID              uint      `gorm:"primaryKey;autoIncrement"`
	TraderID        string    `gorm:"not null;index"`
	MarketRegime    string    `gorm:"not null"` // risk_on | risk_off | mixed | cautious
	ThesisText      string    `gorm:"not null;type:text"`
	SectorBias      string    `gorm:"type:text"` // JSON: {"semiconductor":"bullish","index":"neutral"}
	KeyRisks        string    `gorm:"type:text"` // JSON: ["Fed rate hike risk","earnings miss"]
	PortfolioIntent string    // e.g. "building_tech_long", "defensive_hedged", "reducing_exposure"
	ValidHours      int       `gorm:"default:24"`
	Source          string    `gorm:"default:'ai'"` // "ai" | "manual"
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (MacroThesis) TableName() string { return "macro_thesis" }

// MacroThesisStore handles persistence of macro thesis records.
type MacroThesisStore struct {
	db *gorm.DB
}

// NewMacroThesisStore creates a new store and auto-migrates the table.
func NewMacroThesisStore(db *gorm.DB) *MacroThesisStore {
	db.AutoMigrate(&MacroThesis{})
	return &MacroThesisStore{db: db}
}

// initTables ensures the table exists (called from store.go initTables chain).
func (s *MacroThesisStore) initTables() error {
	return s.db.AutoMigrate(&MacroThesis{})
}

// GetLatest returns the most recent thesis for a trader, or nil if none exists.
func (s *MacroThesisStore) GetLatest(traderID string) (*MacroThesis, error) {
	var thesis MacroThesis
	result := s.db.Where("trader_id = ?", traderID).
		Order("created_at DESC").
		First(&thesis)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get macro thesis: %w", result.Error)
	}
	return &thesis, nil
}

// Create persists a new thesis record (history is kept; no in-place updates).
func (s *MacroThesisStore) Create(thesis *MacroThesis) error {
	if err := s.db.Create(thesis).Error; err != nil {
		return fmt.Errorf("create macro thesis: %w", err)
	}
	return nil
}

// IsStale returns true if the thesis has exceeded its valid_hours window.
func (t *MacroThesis) IsStale() bool {
	return time.Since(t.UpdatedAt) > time.Duration(t.ValidHours)*time.Hour
}

// ParseSectorBias deserializes the JSON sector bias field.
func (t *MacroThesis) ParseSectorBias() map[string]string {
	if t.SectorBias == "" {
		return nil
	}
	var out map[string]string
	json.Unmarshal([]byte(t.SectorBias), &out) //nolint:errcheck
	return out
}

// ParseKeyRisks deserializes the JSON key risks field.
func (t *MacroThesis) ParseKeyRisks() []string {
	if t.KeyRisks == "" {
		return nil
	}
	var out []string
	json.Unmarshal([]byte(t.KeyRisks), &out) //nolint:errcheck
	return out
}

// EncodeSectorBias serializes a sector bias map to a JSON string for storage.
func EncodeSectorBias(bias map[string]string) string {
	if bias == nil {
		return ""
	}
	b, _ := json.Marshal(bias)
	return string(b)
}

// EncodeKeyRisks serializes a key risks slice to a JSON string for storage.
func EncodeKeyRisks(risks []string) string {
	if risks == nil {
		return ""
	}
	b, _ := json.Marshal(risks)
	return string(b)
}
