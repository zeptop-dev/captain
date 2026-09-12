import { createTheme } from '@mantine/core'

// Portal: light, calm, large type. Same primary as the admin.
export const theme = createTheme({
  primaryColor: 'cyan',
  primaryShade: 6,
  defaultRadius: 'lg',
  fontFamily: 'Inter, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif',
  headings: { fontWeight: '700' },
  components: {
    Card: { defaultProps: { withBorder: true, padding: 'xl', radius: 'lg' } },
    Button: { defaultProps: { radius: 'md' } },
  },
})
