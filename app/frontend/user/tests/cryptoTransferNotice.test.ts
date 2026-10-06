// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import test from 'node:test'
import assert from 'node:assert/strict'
import { resolveCryptoTransferDetails } from '../src/utils/cryptoTransferNotice.ts'

test('BSC aliases resolve to the same network and retain the exact token amount', () => {
  for (const chain of ['bsc', 'bep20', 'bnb-smart-chain', ' BEP20 ']) {
    assert.deepEqual(resolveCryptoTransferDetails({ chain, tokenId: 'bsc-usdt', chainAmount: '12.340000' }), {
      networkLabel: 'BNB Smart Chain（BSC / BEP20）',
      tokenLabel: 'USDT',
      exactAmount: '12.340000',
      isBsc: true,
      isTron: false,
    })
  }
})

test('TRON aliases agree with token chain prefixes', () => {
  for (const chain of ['tron', 'trc20', ' TRON ']) {
    assert.deepEqual(resolveCryptoTransferDetails({ chain, tokenId: 'trc20-usdt', chainAmount: '1.005' }), {
      networkLabel: 'TRON（TRC20）',
      tokenLabel: 'USDT',
      exactAmount: '1.005',
      isBsc: false,
      isTron: true,
    })
  }
})

test('token IDs can supply a missing network, including a chain name containing hyphens', () => {
  assert.deepEqual(resolveCryptoTransferDetails({ tokenId: 'bnb-smart-chain-usdt', chainAmount: '0.1' }), {
    networkLabel: 'BNB Smart Chain（BSC / BEP20）',
    tokenLabel: 'USDT',
    exactAmount: '0.1',
    isBsc: true,
    isTron: false,
  })
  assert.equal(resolveCryptoTransferDetails({ tokenId: 'tron-trx', chainAmount: '25' }).tokenLabel, 'TRX')
  assert.equal(resolveCryptoTransferDetails({ chain: 'bsc', tokenId: 'usdc', chainAmount: '25' }).tokenLabel, 'USDC')
})

test('the actual chain wins over a conflicting token prefix but suppresses the exact amount', () => {
  for (const [chain, tokenId, networkLabel] of [
    ['tron', 'bsc-usdt', 'TRON（TRC20）'],
    ['bep20', 'trc20-usdt', 'BNB Smart Chain（BSC / BEP20）'],
    ['Ethereum', 'bsc-usdt', 'Ethereum'],
  ]) {
    const details = resolveCryptoTransferDetails({ chain, tokenId, chainAmount: '23.45' })
    assert.equal(details.networkLabel, networkLabel)
    assert.equal(details.tokenLabel, 'USDT')
    assert.equal(details.exactAmount, '')
  }
})

test('unknown explicitly supplied networks keep their original label', () => {
  assert.deepEqual(resolveCryptoTransferDetails({ chain: 'Ethereum', tokenId: 'ethereum-usdt', chainAmount: '3.25' }), {
    networkLabel: 'Ethereum',
    tokenLabel: 'USDT',
    exactAmount: '3.25',
    isBsc: false,
    isTron: false,
  })
})

test('missing information never invents a token, network or payable amount', () => {
  assert.deepEqual(resolveCryptoTransferDetails(), {
    networkLabel: '', tokenLabel: '', exactAmount: '', isBsc: false, isTron: false,
  })
  for (const input of [
    {},
    { chain: 'bsc', chainAmount: '120' },
    { tokenId: 'usdt', chainAmount: '120' },
    { chain: 'bsc', tokenId: 'usdt' },
    { chain: ' ', tokenId: '', chainAmount: '120' },
    { chain: {}, tokenId: [], chainAmount: '120' },
    { chain: 'bsc', tokenId: 'bsc-', chainAmount: '120' },
  ]) {
    assert.equal(resolveCryptoTransferDetails(input).exactAmount, '')
  }
  assert.equal(resolveCryptoTransferDetails({ chain: 'bsc' }).tokenLabel, '')
  assert.equal(resolveCryptoTransferDetails({ tokenId: 'usdt' }).networkLabel, '')
})

test('positive decimal strings retain arbitrarily long precision and trailing zeros', () => {
  for (const amount of ['1', '001.2300', '9007199254740993123456789.123456789123456789000', '0.00000000000000000000000000000000000001']) {
    assert.equal(resolveCryptoTransferDetails({ chain: 'bsc', tokenId: 'bsc-usdt', chainAmount: amount }).exactAmount, amount)
  }
})

test('zero, negative, exponential and malformed amounts are not shown as exact payments', () => {
  for (const chainAmount of ['', ' ', '0', '0.00', '000.000', '-1', '-0.1', '+1', '1e3', '1E-8', 'NaN', 'Infinity', '1,000', '1.2.3', '.5', '5.', 1.23, 0, NaN, null, undefined, {}]) {
    assert.equal(resolveCryptoTransferDetails({ chain: 'bsc', tokenId: 'bsc-usdt', chainAmount }).exactAmount, '', String(chainAmount))
  }
})
