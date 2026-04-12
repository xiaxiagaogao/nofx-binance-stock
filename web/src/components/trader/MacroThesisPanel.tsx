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
    const interval = setInterval(fetchThesis, 30000)
    return () => { mounted = false; clearInterval(interval) }
  }, [traderId])

  if (loading || !thesis) return null

  // Regime styling
  const regimeStyles: Record<string, { bg: string; text: string; label: string }> = {
    risk_on:  { bg: 'rgba(14,203,129,0.12)', text: '#0ECB81', label: '🟢 Risk On' },
    risk_off: { bg: 'rgba(246,70,93,0.12)',   text: '#F6465D', label: '🔴 Risk Off' },
    cautious: { bg: 'rgba(240,185,11,0.12)',  text: '#F0B90B', label: '🟡 Cautious' },
  }
  const regime = regimeStyles[thesis.market_regime] ?? {
    bg: 'rgba(132,142,156,0.12)', text: '#848E9C', label: thesis.market_regime
  }

  // Intent styling
  const intentStyles: Record<string, { icon: string; color: string }> = {
    aggressive_long:  { icon: '⬆️', color: '#0ECB81' },
    selective_long:   { icon: '↗️', color: '#0ECB81' },
    preserve_cash:    { icon: '💰', color: '#F0B90B' },
    reduce_exposure:  { icon: '📉', color: '#F6465D' },
    defensive:        { icon: '🛡️', color: '#F6465D' },
  }
  const intent = intentStyles[thesis.portfolio_intent] ?? { icon: '📊', color: '#848E9C' }

  // Sector bias styling
  const biasColor = (bias: string) =>
    bias === 'bullish' ? '#0ECB81' : bias === 'bearish' ? '#F6465D' : '#848E9C'

  return (
    <div
      className="rounded-lg overflow-hidden"
      style={{
        background: 'linear-gradient(135deg, #1E2329 0%, #181C21 100%)',
        border: '1px solid rgba(255,255,255,0.06)',
      }}
    >
      {/* Header bar */}
      <div
        className="flex items-center justify-between px-4 py-2.5"
        style={{ borderBottom: '1px solid rgba(255,255,255,0.04)' }}
      >
        <h3 className="text-xs font-semibold text-nofx-text-main tracking-wide uppercase">
          {t('fundManager.macroThesis', language)}
        </h3>
        <div className="flex items-center gap-2 text-[10px] text-nofx-text-muted">
          <span>{thesis.age_hours.toFixed(1)}h</span>
          <span>·</span>
          <span className="uppercase">{thesis.source}</span>
          {thesis.is_stale && (
            <span className="px-1.5 py-0.5 rounded bg-nofx-red/10 text-nofx-red font-bold text-[9px]">
              {t('fundManager.stale', language)}
            </span>
          )}
        </div>
      </div>

      <div className="px-4 py-3">
        {/* Top row: Regime + Intent chips */}
        <div className="flex items-center gap-3 mb-3">
          <span
            className="px-2.5 py-1 rounded-md text-[11px] font-bold tracking-wide"
            style={{ background: regime.bg, color: regime.text }}
          >
            {regime.label}
          </span>
          {thesis.portfolio_intent && (
            <span
              className="px-2.5 py-1 rounded-md text-[11px] font-mono"
              style={{ background: `${intent.color}11`, color: intent.color }}
            >
              {intent.icon} {thesis.portfolio_intent.replace(/_/g, ' ')}
            </span>
          )}
        </div>

        {/* Thesis text — truncated to 2 lines */}
        <p
          className="text-[11px] text-nofx-text-secondary leading-relaxed mb-3"
          style={{
            display: '-webkit-box',
            WebkitLineClamp: 2,
            WebkitBoxOrient: 'vertical',
            overflow: 'hidden',
          }}
          title={thesis.thesis_text}
        >
          {thesis.thesis_text}
        </p>

        {/* Bottom row: Sector + Risks as compact tags */}
        <div className="flex items-center flex-wrap gap-1.5">
          {thesis.sector_bias && Object.entries(thesis.sector_bias).map(([sector, bias]) => (
            <span
              key={sector}
              className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-mono"
              style={{
                background: `${biasColor(bias)}0D`,
                color: biasColor(bias),
              }}
            >
              <span style={{ fontSize: '6px', lineHeight: 1 }}>●</span>
              {sector}
            </span>
          ))}

          {thesis.key_risks && thesis.key_risks.length > 0 && (
            <>
              <span className="text-[10px] text-nofx-text-muted mx-1">|</span>
              {thesis.key_risks.map((risk, i) => (
                <span
                  key={i}
                  className="px-2 py-0.5 rounded text-[10px]"
                  style={{ background: 'rgba(246,70,93,0.06)', color: '#F6465D' }}
                >
                  ⚠ {risk}
                </span>
              ))}
            </>
          )}
        </div>
      </div>
    </div>
  )
}
