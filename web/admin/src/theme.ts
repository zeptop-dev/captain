import { createTheme, rem } from '@mantine/core'

// One theme for admin and portal; the admin runs it in dark, the portal in light.
export const theme = createTheme({
  primaryColor: 'cyan',
  primaryShade: { light: 6, dark: 4 },
  defaultRadius: 'md',
  fontFamily: 'Inter, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif',
  fontFamilyMonospace: 'ui-monospace, SFMono-Regular, Menlo, monospace',
  headings: { fontWeight: '600' },
  components: {
    Card: { defaultProps: { withBorder: true, padding: 'lg' } },
    Paper: { defaultProps: { withBorder: true } },
    Table: { defaultProps: { highlightOnHover: true, verticalSpacing: 'sm' } },
    Badge: { defaultProps: { variant: 'light' } },
    TextInput: { styles: { label: { fontSize: rem(12), letterSpacing: '0.04em', textTransform: 'uppercase', opacity: 0.7 } } },
    NumberInput: { styles: { label: { fontSize: rem(12), letterSpacing: '0.04em', textTransform: 'uppercase', opacity: 0.7 } } },
    Select: { styles: { label: { fontSize: rem(12), letterSpacing: '0.04em', textTransform: 'uppercase', opacity: 0.7 } } },
    PasswordInput: { styles: { label: { fontSize: rem(12), letterSpacing: '0.04em', textTransform: 'uppercase', opacity: 0.7 } } },
  },
})
