const commonCurrencies = ['USD', 'CNY', 'EUR', 'GBP', 'JPY', 'HKD', 'TWD', 'SGD', 'AUD', 'CAD', 'CHF', 'KRW', 'INR', 'RUB', 'TRY', 'BRL', 'CZK', 'PLN', 'SEK', 'NOK', 'DKK', 'NZD', 'ZAR', 'MXN', 'THB', 'IDR', 'MYR', 'VND', 'AED', 'SAR', 'PHP', 'UAH']

// Keep saved ISO codes selectable even if an older browser lacks their names.
export function currencyOptions(language: string, current = '') {
  const supported = typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('currency') : []
  const names = new Intl.DisplayNames([language], { type: 'currency' })
  return [...new Set([...commonCurrencies, ...supported, current])]
    .filter(value => /^[A-Z]{3}$/.test(value) && value !== 'XXX' && value !== 'XTS')
    .map(value => ({ value, label: `${value} · ${names.of(value) ?? value}` }))
}
