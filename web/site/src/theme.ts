import { createTheme } from '@mantine/core'

// Public pages use a quiet system-font stack; configured fonts remain available.
export function buildTheme(primary = 'cyan', radius = 'lg', font?: string) {
  return createTheme({
  primaryColor: primary,
  primaryShade: 6,
  defaultRadius: radius,
  fontFamily: font || '-apple-system, BlinkMacSystemFont, "SF Pro Display", "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
  headings: { fontWeight: '700' },
  components: {
    Card: { defaultProps: { withBorder: true, padding: 'xl', radius: 'lg' } },
    Button: { defaultProps: { radius: 'md' } },
  },
  })
}

export const theme = buildTheme()
