// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

interface CryptoTransferInput {
  chain?: unknown
  tokenId?: unknown
  chainAmount?: unknown
}

interface CryptoTransferDetails {
  networkLabel: string
  tokenLabel: string
  exactAmount: string
  isBsc: boolean
  isTron: boolean
}

const textValue = (value: unknown): string => typeof value === 'string' ? value.trim() : ''

const canonicalChain = (chain: string): string => {
  const normalized = chain.toLowerCase()
  if (['bsc', 'bep20', 'bnb-smart-chain'].includes(normalized)) return 'bsc'
  if (['tron', 'trc20'].includes(normalized)) return 'tron'
  return normalized
}

export const resolveCryptoTransferDetails = (input?: CryptoTransferInput): CryptoTransferDetails => {
  const actualChain = textValue(input?.chain)
  const tokenId = textValue(input?.tokenId)
  // Split at the last hyphen so chain names such as bnb-smart-chain stay intact.
  const tokenParts = tokenId.match(/^(.+)-([a-z][a-z0-9]*)$/i)
  const tokenChain = tokenParts?.[1] || ''
  const token = tokenParts?.[2] || (/^[a-z][a-z0-9]*$/i.test(tokenId) ? tokenId : '')
  const resolvedChain = actualChain || tokenChain
  const chainKey = canonicalChain(resolvedChain)
  const isBsc = chainKey === 'bsc'
  const isTron = chainKey === 'tron'
  const networkLabel = isBsc ? 'BNB Smart Chain（BSC / BEP20）' : isTron ? 'TRON（TRC20）' : resolvedChain
  const tokenLabel = token.toUpperCase()
  const conflictingChain = Boolean(actualChain && tokenChain && canonicalChain(actualChain) !== canonicalChain(tokenChain))
  const amount = textValue(input?.chainAmount)
  // Never parse payment amounts as Number: preserve provider precision and trailing zeros.
  const positiveDecimal = /^\d+(?:\.\d+)?$/.test(amount) && /[1-9]/.test(amount)
  const exactAmount = resolvedChain && tokenLabel && !conflictingChain && positiveDecimal ? amount : ''

  return { networkLabel, tokenLabel, exactAmount, isBsc, isTron }
}
