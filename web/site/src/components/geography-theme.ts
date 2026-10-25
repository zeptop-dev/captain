export function globePalette(scheme: 'light' | 'dark') {
  return scheme === 'light' ? {
    ocean: ['#f7f9f2', '#e8ede2', '#d9e1d0'], border: 'rgba(55,75,47,.18)', land: '51,71,42',
    marker: '#344c30', markerRing: 'rgba(52,76,48,.3)', homeMarker: '#267454', homeRing: 'rgba(38,116,84,.3)',
    label: '#344c30', labelBackground: 'rgba(250,252,246,.96)', labelBorder: 'rgba(55,75,47,.25)',
  } : {
    ocean: ['#101216', '#0b0d10', '#080a0d'], border: 'rgba(182,192,205,.1)', land: '225,230,236',
    marker: '#dce3e9', markerRing: 'rgba(220,227,233,.2)', homeMarker: '#62d7b0', homeRing: 'rgba(98,215,176,.22)',
    label: '#c9d0d8', labelBackground: 'rgba(10,12,15,.86)', labelBorder: 'rgba(178,188,201,.18)',
  }
}
