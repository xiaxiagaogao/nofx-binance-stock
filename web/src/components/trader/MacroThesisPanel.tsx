import { useEffect, useState } from 'react'
import { api } from '../../lib/api'
import { t, type Language } from '../../i18n/translations'
import type { MacroThesis } from '../../types'

interface MacroThesisPanelProps {
  traderId: string
  language: Language
}

export function MacroThesisPanel({ traderId, language }: MacroThesisPanelProps) {
  const [thesis, setThesis] = useState<MacroThesis | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let mounted = true
    const fetchThesis = async () => {
      try {
        const data = await api.getMacroThesis(traderId)
        if (mounted) setThesis(data?.thesis ?? null)
      } catch {
        // silent
      } finally {
        if (mounted) setLoading(false)
      }
    }
    fetchThesis()
    const interval = setInterval(fetchThesis, 30000) // refresh every 30s
    return () => { mounted = false; clearInterval(interval) }
  }, [traderId])

  if (loading) return null

  // Regime color
  const regimeColor = thesis?.market_regime === 'risk_on' ? '#0ECB81' :
    thesis?.market_regime === 'risk_off' ? '#F6465D' :
    thesis?.market_regime === 'cautious' ? '#F0B90B' : '#848E9C'

  return (
    <div
      className="rounded-lg p-4"
      style={{
        background: 'linear-gradient(135deg, #1E2329 0%, #181C21 100%)',
        border: '1px solid rgba(255,255,255,0.06)',
      }}
    >
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-semibold text-nofx-text-main flex items-center gap-2">
          {t('fundManager.macroThesis', language)}
        </h3>
        {thesis && (
          <div className="flex items-center gap-2 text-[10px]">
            <span className="text-nofx-text-muted">
              {thesis.age_hours.toFixed(1)}h · {thesis.source}
            </span>
            {thesis.is_stale && (
              <span className="px-1.5 py-0.5 rounded bg-nofx-red/10 text-nofx-red font-bold">
                {t('fundManager.stale', language)}
              </span>
            )}
          </div>
        )}
      </div>

      {!thesis ? (
        <p className="text-xs text-nofx-text-muted italic">
          {t('fundManager.noThesis', language)}
        </p>
      ) : (
        <div className="space-y-2 text-xs">
          {/* Regime */}
          <div className="flex items-center gap-2">
            <span className="text-nofx-text-muted w-16 shrink-0">{t('fundManager.regime', language)}</span>
            <span
              className="px-2 py-0.5 rounded font-bold uppercase tracking-wider text-[10px]"
              style={{ background: `${regimeColor}22`, color: regimeColor }}
            >
              {thesis.market_regime}
            </span>
          </div>

          {/* Thesis text */}
          <div className="flex gap-2">
            <span className="text-nofx-text-muted w-16 shrink-0">{t('fundManager.thesis', language)}</span>
            <span className="text-nofx-text-main leading-relaxed">{thesis.thesis_text}</span>
          </div>

          {/* Portfolio Intent */}
          {thesis.portfolio_intent && (
            <div className="flex items-center gap-2">
              <span className="text-nofx-text-muted w-16 shrink-0">{t('fundManager.intent', language)}</span>
              <span className="text-nofx-gold font-mono text-[11px]">{thesis.portfolio_intent}</span>
            </div>
          )}

          {/* Sector Bias */}
          {thesis.sector_bias && Object.keys(thesis.sector_bias).length > 0 && (
            <div className="flex gap-2">
              <span className="text-nofx-text-muted w-16 shrink-0">{t('fundManager.sectorBias', language)}</span>
              <div className="flex flex-wrap gap-1">
                {Object.entries(thesis.sector_bias).map(([sector, bias]) => (
                  <span
                    key={sector}
                    className="px-1.5 py-0.5 rounded text-[10px] font-mono"
                    style={{
                      background: bias === 'bullish' ? 'rgba(14,203,129,0.1)' :
                                  bias === 'bearish' ? 'rgba(246,70,93,0.1)' : 'rgba(132,142,156,0.1)',
                      color: bias === 'bullish' ? '#0ECB81' :
                             bias === 'bearish' ? '#F6465D' : '#848E9C',
                    }}
                  >
                    {sector}: {bias}
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Key Risks */}
          {thesis.key_risks && thesis.key_risks.length > 0 && (
            <div className="flex gap-2">
              <span className="text-nofx-text-muted w-16 shrink-0">{t('fundManager.keyRisks', language)}</span>
              <div className="flex flex-wrap gap-1">
                {thesis.key_risks.map((risk, i) => (
                  <span key={i} className="px-1.5 py-0.5 rounded bg-nofx-red/5 text-nofx-red text-[10px]">
                    {risk}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
