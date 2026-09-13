import { createTheme } from '@mantine/core'

// Portal: light, calm, large type. Same primary as the admin.
export function buildTheme(primary = 'cyan', radius = 'lg', font?: string) {
  return createTheme({
  primaryColor: primary,
  primaryShade: 6,
  defaultRadius: radius,
  fontFamily: font || 'Inter, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif',
  headings: { fontWeight: '700' },
  components: {
    Card: { defaultProps: { withBorder: true, padding: 'xl', radius: 'lg' } },
    Button: { defaultProps: { radius: 'md' } },
  },
  })
}

export const theme = buildTheme()
