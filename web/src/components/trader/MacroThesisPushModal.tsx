import { useState } from 'react'
import { api } from '../../lib/api'
import { t, type Language } from '../../i18n/translations'

interface MacroThesisPushModalProps {
  traderId: string
  language: Language
  onClose: () => void
  onSuccess: () => void
}

const REGIME_OPTIONS = ['risk_on', 'risk_off', 'cautious', 'neutral']
const INTENT_OPTIONS = [
  'aggressive_long',
  'selective_long',
  'preserve_cash',
  'reduce_exposure',
  'defensive',
]
const SECTOR_OPTIONS = [
  'semiconductor',
  'index',
  'ev_auto',
  'commodity',
  'tech_mega',
  'finance',
  'energy',
]
const BIAS_OPTIONS = ['bullish', 'bearish', 'neutral']

export function MacroThesisPushModal({
  traderId,
  language,
  onClose,
  onSuccess,
}: MacroThesisPushModalProps) {
  const [thesisText, setThesisText] = useState('')
  const [marketRegime, setMarketRegime] = useState('cautious')
  const [portfolioIntent, setPortfolioIntent] = useState('preserve_cash')
  const [validHours, setValidHours] = useState(24)
  const [keyRisksRaw, setKeyRisksRaw] = useState('')
  const [sectorBias, setSectorBias] = useState<Record<string, string>>({})
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSectorBias = (sector: string, bias: string) => {
    if (!bias) {
      const next = { ...sectorBias }
      delete next[sector]
      setSectorBias(next)
    } else {
      setSectorBias(prev => ({ ...prev, [sector]: bias }))
    }
  }

  const handleSubmit = async () => {
    if (!thesisText.trim()) {
      setError(t('fundManager.thesisRequired', language))
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const keyRisks = keyRisksRaw
        .split('\n')
        .map(s => s.trim())
        .filter(Boolean)

      await api.postMacroThesis(traderId, {
        thesis_text: thesisText.trim(),
        market_regime: marketRegime,
        portfolio_intent: portfolioIntent,
        sector_bias: Object.keys(sectorBias).length > 0 ? sectorBias : undefined,
        key_risks: keyRisks.length > 0 ? keyRisks : undefined,
        valid_hours: validHours,
      })
      onSuccess()
      onClose()
    } catch (e: any) {
      setError(e?.message || 'Failed to push thesis')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    // Backdrop
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.7)' }}
      onClick={e => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="w-full max-w-lg rounded-xl overflow-hidden flex flex-col"
        style={{
          background: '#1E2329',
          border: '1px solid rgba(255,255,255,0.08)',
          maxHeight: '90vh',
        }}
      >
        {/* Header */}
        <div
          className="flex items-center justify-between px-5 py-4"
          style={{ borderBottom: '1px solid rgba(255,255,255,0.06)' }}
        >
          <h2 className="text-sm font-semibold text-nofx-text-main">
            {t('fundManager.pushThesis', language)}
          </h2>
          <button
            onClick={onClose}
            className="text-nofx-text-muted hover:text-nofx-text-main text-lg leading-none"
          >
            ×
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-4">

          {/* Thesis text */}
          <div>
            <label className="block text-xs text-nofx-text-muted mb-1.5">
              {t('fundManager.thesis', language)}
              <span className="text-nofx-red ml-1">*</span>
            </label>
            <textarea
              rows={4}
              value={thesisText}
              onChange={e => setThesisText(e.target.value)}
              placeholder={t('fundManager.thesisPlaceholder', language)}
              className="w-full rounded-lg px-3 py-2 text-xs text-nofx-text-main resize-none focus:outline-none focus:ring-1 focus:ring-nofx-gold/40"
              style={{
                background: '#12161C',
                border: '1px solid rgba(255,255,255,0.08)',
              }}
            />
          </div>

          {/* Regime + Intent row */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-nofx-text-muted mb-1.5">
                {t('fundManager.regime', language)}
              </label>
              <select
                value={marketRegime}
                onChange={e => setMarketRegime(e.target.value)}
                className="w-full rounded-lg px-3 py-2 text-xs text-nofx-text-main focus:outline-none"
                style={{
                  background: '#12161C',
                  border: '1px solid rgba(255,255,255,0.08)',
                }}
              >
                {REGIME_OPTIONS.map(r => (
                  <option key={r} value={r}>{r.replace(/_/g, ' ')}</option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-xs text-nofx-text-muted mb-1.5">
                {t('fundManager.intent', language)}
              </label>
              <select
                value={portfolioIntent}
                onChange={e => setPortfolioIntent(e.target.value)}
                className="w-full rounded-lg px-3 py-2 text-xs text-nofx-text-main focus:outline-none"
                style={{
                  background: '#12161C',
                  border: '1px solid rgba(255,255,255,0.08)',
                }}
              >
                {INTENT_OPTIONS.map(i => (
                  <option key={i} value={i}>{i.replace(/_/g, ' ')}</option>
                ))}
              </select>
            </div>
          </div>

          {/* Sector bias */}
          <div>
            <label className="block text-xs text-nofx-text-muted mb-1.5">
              {t('fundManager.sectorBias', language)}
              <span className="text-nofx-text-muted/50 ml-1 font-normal">
                ({t('fundManager.optional', language)})
              </span>
            </label>
            <div className="grid grid-cols-2 gap-1.5">
              {SECTOR_OPTIONS.map(sector => (
                <div key={sector} className="flex items-center gap-2">
                  <span className="text-[11px] text-nofx-text-secondary w-24 shrink-0">
                    {sector}
                  </span>
                  <select
                    value={sectorBias[sector] || ''}
                    onChange={e => handleSectorBias(sector, e.target.value)}
                    className="flex-1 rounded px-2 py-1 text-[11px] text-nofx-text-main focus:outline-none"
                    style={{
                      background: '#12161C',
                      border: '1px solid rgba(255,255,255,0.06)',
                    }}
                  >
                    <option value="">—</option>
                    {BIAS_OPTIONS.map(b => (
                      <option key={b} value={b}>{b}</option>
                    ))}
                  </select>
                </div>
              ))}
            </div>
          </div>

          {/* Key risks */}
          <div>
            <label className="block text-xs text-nofx-text-muted mb-1.5">
              {t('fundManager.keyRisks', language)}
              <span className="text-nofx-text-muted/50 ml-1 font-normal">
                ({t('fundManager.onePerLine', language)})
              </span>
            </label>
            <textarea
              rows={3}
              value={keyRisksRaw}
              onChange={e => setKeyRisksRaw(e.target.value)}
              placeholder={t('fundManager.keyRisksPlaceholder', language)}
              className="w-full rounded-lg px-3 py-2 text-xs text-nofx-text-main resize-none focus:outline-none focus:ring-1 focus:ring-nofx-gold/40"
              style={{
                background: '#12161C',
                border: '1px solid rgba(255,255,255,0.08)',
              }}
            />
          </div>

          {/* Valid hours */}
          <div>
            <label className="block text-xs text-nofx-text-muted mb-1.5">
              {t('fundManager.validHours', language)}
            </label>
            <div className="flex items-center gap-2">
              <input
                type="number"
                min={1}
                max={168}
                value={validHours}
                onChange={e => setValidHours(Number(e.target.value))}
                className="w-20 rounded-lg px-3 py-2 text-xs text-nofx-text-main focus:outline-none"
                style={{
                  background: '#12161C',
                  border: '1px solid rgba(255,255,255,0.08)',
                }}
              />
              <span className="text-xs text-nofx-text-muted">h</span>
            </div>
          </div>

          {error && (
            <p className="text-xs text-nofx-red">{error}</p>
          )}
        </div>

        {/* Footer */}
        <div
          className="flex items-center justify-end gap-3 px-5 py-4"
          style={{ borderTop: '1px solid rgba(255,255,255,0.06)' }}
        >
          <button
            onClick={onClose}
            className="px-4 py-1.5 text-xs text-nofx-text-muted hover:text-nofx-text-main transition-colors"
          >
            {t('fundManager.cancel', language)}
          </button>
          <button
            onClick={handleSubmit}
            disabled={submitting || !thesisText.trim()}
            className="px-4 py-1.5 text-xs font-semibold rounded-lg transition-opacity disabled:opacity-40"
            style={{ background: '#F0B90B', color: '#12161C' }}
          >
            {submitting ? '…' : t('fundManager.push', language)}
          </button>
        </div>
      </div>
    </div>
  )
}
