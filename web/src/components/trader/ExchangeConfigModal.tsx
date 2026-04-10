import React, { useState, useEffect } from 'react'
import type { Exchange } from '../../types'
import { t, type Language } from '../../i18n/translations'
import { api } from '../../lib/api'
import { getExchangeIcon } from '../common/ExchangeIcons'
import {
  WebCryptoEnvironmentCheck,
} from '../common/WebCryptoEnvironmentCheck'
import {
  BookOpen, Trash2, ExternalLink, UserPlus,
  Key, Shield, Copy, ArrowRight
} from 'lucide-react'
import { toast } from 'sonner'

// Supported exchange templates — Binance only
const SUPPORTED_EXCHANGE_TEMPLATES = [
  { exchange_type: 'binance', name: 'Binance Futures', type: 'cex' as const },
]

interface ExchangeConfigModalProps {
  allExchanges: Exchange[]
  editingExchangeId: string | null
  onSave: (
    exchangeId: string | null,
    exchangeType: string,
    accountName: string,
    apiKey: string,
    secretKey?: string,
    passphrase?: string,
    testnet?: boolean,
    hyperliquidWalletAddr?: string,
    asterUser?: string,
    asterSigner?: string,
    asterPrivateKey?: string,
    lighterWalletAddr?: string,
    lighterPrivateKey?: string,
    lighterApiKeyPrivateKey?: string,
    lighterApiKeyIndex?: number
  ) => Promise<void>
  onDelete: (exchangeId: string) => void
  onClose: () => void
  language: Language
}

// Binance is the only supported exchange
const BINANCE_TEMPLATE = SUPPORTED_EXCHANGE_TEMPLATES[0]


export function ExchangeConfigModal({
  allExchanges,
  editingExchangeId,
  onSave,
  onDelete,
  onClose,
  language,
}: ExchangeConfigModalProps) {
  const [apiKey, setApiKey] = useState('')
  const [secretKey, setSecretKey] = useState('')
  const [showGuide, setShowGuide] = useState(false)
  const [serverIP, setServerIP] = useState<{ public_ip: string; message: string } | null>(null)
  const [loadingIP, setLoadingIP] = useState(false)
  const [copiedIP, setCopiedIP] = useState(false)
  const [showBinanceGuide, setShowBinanceGuide] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [accountName, setAccountName] = useState('')

  const selectedExchange = editingExchangeId
    ? allExchanges?.find((e) => e.id === editingExchangeId)
    : null

  const binanceRegistrationLink = { url: 'https://www.binance.com/join?ref=NOFXENG', hasReferral: true }

  // Initialize form when editing
  useEffect(() => {
    if (editingExchangeId && selectedExchange) {
      setAccountName(selectedExchange.account_name || '')
      setApiKey(selectedExchange.apiKey || '')
      setSecretKey(selectedExchange.secretKey || '')
    }
  }, [editingExchangeId, selectedExchange])

  // Load server IP for Binance
  useEffect(() => {
    if (!serverIP) {
      setLoadingIP(true)
      api.getServerIP()
        .then((data) => setServerIP(data))
        .catch((err) => console.error('Failed to load server IP:', err))
        .finally(() => setLoadingIP(false))
    }
  }, [serverIP])

  const handleCopyIP = async (ip: string) => {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(ip)
        setCopiedIP(true)
        setTimeout(() => setCopiedIP(false), 2000)
        toast.success(t('ipCopied', language))
      } else {
        const textArea = document.createElement('textarea')
        textArea.value = ip
        textArea.style.position = 'fixed'
        textArea.style.left = '-999999px'
        document.body.appendChild(textArea)
        textArea.select()
        document.execCommand('copy')
        document.body.removeChild(textArea)
        setCopiedIP(true)
        setTimeout(() => setCopiedIP(false), 2000)
        toast.success(t('ipCopied', language))
      }
    } catch {
      toast.error(t('copyIPFailed', language))
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (isSaving) return

    const trimmedAccountName = accountName.trim()
    if (!trimmedAccountName) {
      toast.error(t('exchangeConfig.pleaseEnterAccountName', language))
      return
    }

    if (!apiKey.trim() || !secretKey.trim()) return

    const exchangeId = editingExchangeId || null

    setIsSaving(true)
    try {
      await onSave(exchangeId, 'binance', trimmedAccountName, apiKey.trim(), secretKey.trim(), '', false)
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4 overflow-y-auto backdrop-blur-sm">
      <div
        className="rounded-2xl w-full max-w-2xl relative my-8 shadow-2xl"
        style={{ background: 'linear-gradient(180deg, #1E2329 0%, #181A20 100%)', maxHeight: 'calc(100vh - 4rem)' }}
      >
        {/* Header */}
        <div className="flex items-center justify-between p-6 pb-2">
          <div className="flex items-center gap-3">
            <h3 className="text-xl font-bold" style={{ color: '#EAECEF' }}>
              {editingExchangeId ? t('editExchange', language) : t('addExchange', language)}
            </h3>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setShowGuide(true)}
              className="px-3 py-2 rounded-lg text-sm font-semibold transition-all hover:scale-105 flex items-center gap-2"
              style={{ background: 'rgba(240, 185, 11, 0.1)', color: '#F0B90B' }}
            >
              <BookOpen className="w-4 h-4" />
              {t('viewGuide', language)}
            </button>
            {editingExchangeId && (
              <button
                type="button"
                onClick={() => onDelete(editingExchangeId)}
                className="p-2 rounded-lg hover:bg-red-500/20 transition-colors"
                style={{ color: '#F6465D' }}
              >
                <Trash2 className="w-4 h-4" />
              </button>
            )}
            <button type="button" onClick={onClose} className="p-2 rounded-lg hover:bg-white/10 transition-colors" style={{ color: '#848E9C' }}>
              ✕
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="px-6 pb-6 overflow-y-auto" style={{ maxHeight: 'calc(100vh - 16rem)' }}>
          <form onSubmit={handleSubmit} className="space-y-5">
            {/* Exchange Header */}
            <div className="p-4 rounded-xl flex items-center gap-4" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
              {getExchangeIcon(BINANCE_TEMPLATE.exchange_type, { width: 48, height: 48 })}
              <div className="flex-1">
                <div className="font-semibold text-lg" style={{ color: '#EAECEF' }}>
                  {BINANCE_TEMPLATE.name}
                </div>
                <div className="text-xs" style={{ color: '#848E9C' }}>
                  {BINANCE_TEMPLATE.type.toUpperCase()} • {BINANCE_TEMPLATE.exchange_type}
                </div>
              </div>
              <a
                href={binanceRegistrationLink.url}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-2 px-4 py-2 rounded-lg transition-all hover:scale-105"
                style={{ background: 'rgba(240, 185, 11, 0.1)', border: '1px solid rgba(240, 185, 11, 0.3)' }}
              >
                <UserPlus className="w-4 h-4" style={{ color: '#F0B90B' }} />
                <span className="text-sm font-medium" style={{ color: '#F0B90B' }}>
                  {t('exchangeConfig.register', language)}
                </span>
                <span className="text-xs px-1.5 py-0.5 rounded" style={{ background: 'rgba(14, 203, 129, 0.2)', color: '#0ECB81' }}>
                  {t('exchangeConfig.bonus', language)}
                </span>
              </a>
            </div>

            {/* WebCrypto Check */}
            <WebCryptoEnvironmentCheck language={language} variant="card" onStatusChange={() => {}} />

            {/* Account Name */}
            <div className="space-y-2">
              <label className="flex items-center gap-2 text-sm font-semibold" style={{ color: '#EAECEF' }}>
                <Key className="w-4 h-4" style={{ color: '#F0B90B' }} />
                {t('exchangeConfig.accountName', language)} *
              </label>
              <input
                type="text"
                value={accountName}
                onChange={(e) => setAccountName(e.target.value)}
                placeholder={t('exchangeConfig.accountNamePlaceholder', language)}
                className="w-full px-4 py-3 rounded-xl text-base"
                style={{ background: '#0B0E11', border: '1px solid #2B3139', color: '#EAECEF' }}
                required
              />
            </div>

            {/* Binance API hint */}
            <div
              className="p-4 rounded-xl cursor-pointer transition-colors"
              style={{ background: '#1a3a52', border: '1px solid #2b5278' }}
              onClick={() => setShowBinanceGuide(!showBinanceGuide)}
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span style={{ color: '#58a6ff' }}>ℹ️</span>
                  <span className="text-sm font-medium" style={{ color: '#EAECEF' }}>
                    {t('exchangeConfig.useBinanceFuturesApi', language)}
                  </span>
                </div>
                <span style={{ color: '#8b949e' }}>{showBinanceGuide ? '▲' : '▼'}</span>
              </div>
              {showBinanceGuide && (
                <div className="mt-3 pt-3 text-sm" style={{ borderTop: '1px solid #2b5278', color: '#c9d1d9' }}>
                  <a
                    href="https://www.binance.com/zh-CN/support/faq/how-to-create-api-keys-on-binance-360002502072"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 hover:underline"
                    style={{ color: '#58a6ff' }}
                    onClick={(e) => e.stopPropagation()}
                  >
                    {t('exchangeConfig.viewTutorial', language)} <ExternalLink className="w-3 h-3" />
                  </a>
                </div>
              )}
            </div>

            {/* API Key */}
            <div className="space-y-2">
              <label className="flex items-center gap-2 text-sm font-semibold" style={{ color: '#EAECEF' }}>
                <Key className="w-4 h-4" style={{ color: '#F0B90B' }} />
                {t('apiKey', language)}
              </label>
              <input
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder={t('enterAPIKey', language)}
                className="w-full px-4 py-3 rounded-xl"
                style={{ background: '#0B0E11', border: '1px solid #2B3139', color: '#EAECEF' }}
                required
              />
            </div>

            {/* Secret Key */}
            <div className="space-y-2">
              <label className="flex items-center gap-2 text-sm font-semibold" style={{ color: '#EAECEF' }}>
                <Shield className="w-4 h-4" style={{ color: '#F0B90B' }} />
                {t('secretKey', language)}
              </label>
              <input
                type="password"
                value={secretKey}
                onChange={(e) => setSecretKey(e.target.value)}
                placeholder={t('enterSecretKey', language)}
                className="w-full px-4 py-3 rounded-xl"
                style={{ background: '#0B0E11', border: '1px solid #2B3139', color: '#EAECEF' }}
                required
              />
            </div>

            {/* Whitelist IP */}
            <div className="p-4 rounded-xl" style={{ background: 'rgba(240, 185, 11, 0.1)', border: '1px solid rgba(240, 185, 11, 0.2)' }}>
              <div className="text-sm font-semibold mb-2" style={{ color: '#F0B90B' }}>
                {t('whitelistIP', language)}
              </div>
              <div className="text-xs mb-3" style={{ color: '#848E9C' }}>
                {t('whitelistIPDesc', language)}
              </div>
              {loadingIP ? (
                <div className="text-xs" style={{ color: '#848E9C' }}>{t('loadingServerIP', language)}</div>
              ) : serverIP?.public_ip ? (
                <div className="flex items-center gap-2 p-3 rounded-lg" style={{ background: '#0B0E11' }}>
                  <code className="flex-1 text-sm font-mono" style={{ color: '#F0B90B' }}>{serverIP.public_ip}</code>
                  <button
                    type="button"
                    onClick={() => handleCopyIP(serverIP.public_ip)}
                    className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-xs font-semibold transition-all hover:scale-105"
                    style={{ background: 'rgba(240, 185, 11, 0.2)', color: '#F0B90B' }}
                  >
                    <Copy className="w-3 h-3" />
                    {copiedIP ? t('ipCopied', language) : t('copyIP', language)}
                  </button>
                </div>
              ) : null}
            </div>

            {/* Buttons */}
            <div className="flex gap-3 pt-4">
              <button type="button" onClick={onClose} className="flex-1 px-4 py-3 rounded-xl text-sm font-semibold transition-all hover:bg-white/5" style={{ background: '#2B3139', color: '#848E9C' }}>
                {t('cancel', language)}
              </button>
              <button
                type="submit"
                disabled={isSaving || !accountName.trim()}
                className="flex-1 flex items-center justify-center gap-2 px-4 py-3 rounded-xl text-sm font-bold transition-all hover:scale-[1.02] disabled:opacity-50 disabled:cursor-not-allowed"
                style={{ background: '#F0B90B', color: '#000' }}
              >
                {isSaving ? t('saving', language) : (
                  <>{t('saveConfig', language)} <ArrowRight className="w-4 h-4" /></>
                )}
              </button>
            </div>
          </form>
        </div>
      </div>

      {/* Binance Guide Modal */}
      {showGuide && (
        <div className="fixed inset-0 bg-black/75 flex items-center justify-center z-50 p-4" onClick={() => setShowGuide(false)}>
          <div className="rounded-2xl p-6 w-full max-w-4xl" style={{ background: '#1E2329' }} onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-xl font-bold flex items-center gap-2" style={{ color: '#EAECEF' }}>
                <BookOpen className="w-6 h-6" style={{ color: '#F0B90B' }} />
                {t('binanceSetupGuide', language)}
              </h3>
              <button onClick={() => setShowGuide(false)} className="px-4 py-2 rounded-lg text-sm font-semibold" style={{ background: '#2B3139', color: '#848E9C' }}>
                {t('closeGuide', language)}
              </button>
            </div>
            <div className="overflow-y-auto max-h-[80vh]">
              <img src="/images/guide.png" alt={t('binanceSetupGuide', language)} className="w-full h-auto rounded-lg" />
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
