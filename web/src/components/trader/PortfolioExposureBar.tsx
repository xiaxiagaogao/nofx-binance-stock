import { useEffect, useState } from 'react'
import { api } from '../../lib/api'
import { t, type Language } from '../../i18n/translations'
import type { FundManagerOverview } from '../../types'

interface PortfolioExposureBarProps {
  traderId: string
  language: Language
}

// Session display config
const SESSION_CONFIG: Record<string, { label: string; color: string; icon: string }> = {
  us_market_open: { label: 'US Open', color: '#0ECB81', icon: '\uD83D\uDFE2' },
  us_pre_market: { label: 'Pre-Market', color: '#F0B90B', icon: '\uD83D\uDFE1' },
  us_after_hours: { label: 'After Hours', color: '#F0B90B', icon: '\uD83D\uDFE0' },
  us_market_closed: { label: 'Closed', color: '#F6465D', icon: '\uD83D\uDD34' },
}

export function PortfolioExposureBar({ traderId, language }: PortfolioExposureBarProps) {
  const [data, setData] = useState<FundManagerOverview | null>(null)

  useEffect(() => {
    let mounted = true
    const fetchData = async () => {
      try {
        const res = await api.getPortfolioExposure(traderId)
        if (mounted) setData(res)
      } catch {
        // silent
      }
    }
    fetchData()
    const interval = setInterval(fetchData, 15000) // refresh every 15s
    return () => { mounted = false; clearInterval(interval) }
  }, [traderId])

  if (!data) return null

  const exposure = data.exposure
  const sessionCfg = SESSION_CONFIG[data.session] || SESSION_CONFIG.us_market_closed
  const hasPositions = exposure && (exposure.net_long_usd > 0 || exposure.net_short_usd > 0)

  const directionColor = exposure?.net_direction === 'net_long' ? '#0ECB81' :
    exposure?.net_direction === 'net_short' ? '#F6465D' : '#848E9C'

  return (
    <div
      className="rounded-lg px-4 py-3 flex flex-wrap items-center gap-x-5 gap-y-2 text-xs"
      style={{
        background: 'linear-gradient(135deg, #1E2329 0%, #181C21 100%)',
        border: '1px solid rgba(255,255,255,0.06)',
      }}
    >
      {/* Session + Scale */}
      <div className="flex items-center gap-2">
        <span>{sessionCfg.icon}</span>
        <span className="font-semibold" style={{ color: sessionCfg.color }}>
          {sessionCfg.label}
        </span>
        <span className="text-nofx-text-muted">
          {t('fundManager.riskScale', language)}: {data.session_scale_factor.toFixed(2)}x
        </span>
      </div>

      {hasPositions && exposure && (
        <>
          {/* Direction */}
          <div className="flex items-center gap-1.5">
            <span className="text-nofx-text-muted">{t('fundManager.direction', language)}:</span>
            <span className="font-bold" style={{ color: directionColor }}>
              {exposure.net_direction === 'net_long' ? t('fundManager.netLong', language) :
               exposure.net_direction === 'net_short' ? t('fundManager.netShort', language) :
               t('fundManager.balanced', language)}
            </span>
            <span className="text-nofx-text-muted font-mono">
              ({'\u2191'}${exposure.net_long_usd.toFixed(0)} | {'\u2193'}${exposure.net_short_usd.toFixed(0)})
            </span>
          </div>

          {/* Beta / Alpha / Hedge breakdown */}
          <div className="flex items-center gap-3">
            {exposure.core_beta_usd > 0 && (
              <span className="flex items-center gap-1">
                <span className="w-1.5 h-1.5 rounded-full" style={{ background: '#F0B90B' }} />
                <span className="text-nofx-text-muted">{t('fundManager.coreBeta', language)}:</span>
                <span className="font-mono text-nofx-gold">${exposure.core_beta_usd.toFixed(0)}</span>
              </span>
            )}
            {exposure.tactical_alpha_usd > 0 && (
              <span className="flex items-center gap-1">
                <span className="w-1.5 h-1.5 rounded-full" style={{ background: '#0ECB81' }} />
                <span className="text-nofx-text-muted">{t('fundManager.tacticalAlpha', language)}:</span>
                <span className="font-mono text-nofx-green">${exposure.tactical_alpha_usd.toFixed(0)}</span>
              </span>
            )}
            {exposure.hedge_usd > 0 && (
              <span className="flex items-center gap-1">
                <span className="w-1.5 h-1.5 rounded-full" style={{ background: '#848E9C' }} />
                <span className="text-nofx-text-muted">{t('fundManager.hedge', language)}:</span>
                <span className="font-mono text-nofx-text-main">${exposure.hedge_usd.toFixed(0)}</span>
              </span>
            )}
          </div>

          {/* Category breakdown */}
          {exposure.category_breakdown && Object.keys(exposure.category_breakdown).length > 0 && (
            <div className="flex items-center gap-2 flex-wrap">
              {Object.entries(exposure.category_breakdown)
                .sort(([, a], [, b]) => b - a)
                .map(([cat, usd]) => (
                  <span key={cat} className="px-1.5 py-0.5 rounded text-[10px] font-mono bg-white/5 text-nofx-text-muted">
                    {cat}: ${usd.toFixed(0)}
                  </span>
                ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
