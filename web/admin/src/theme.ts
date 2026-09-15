import { createTheme, rem, type MantineColorsTuple } from '@mantine/core'

// Light-first console look: soft grey page, white cards with a hairline
// border, indigo accents. The colour scheme can still be flipped to dark from
// the sidebar. Shared by bosun's panel and Captain's admin console.
const brand: MantineColorsTuple = ['#eef2ff', '#e0e7ff', '#c7d2fe', '#a5b4fc', '#818cf8', '#6366f1', '#4f46e5', '#4338ca', '#3730a3', '#312e81']

const fieldLabel = { label: { fontSize: rem(12), fontWeight: 500, marginBottom: rem(4), color: 'var(--mantine-color-dimmed)' }, description: { marginTop: rem(4) } }
// Descriptions render under the control, so inputs in one row line up whatever their hints say.
const under = { inputWrapperOrder: ['label', 'input', 'description', 'error'] as ('label' | 'input' | 'description' | 'error')[] }

export const theme = createTheme({
  primaryColor: 'brand',
  colors: { brand },
  primaryShade: { light: 6, dark: 5 },
  defaultRadius: 'md',
  fontFamily: 'Inter, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif',
  fontFamilyMonospace: 'ui-monospace, SFMono-Regular, Menlo, monospace',
  headings: { fontWeight: '600' },
  components: {
    Card: { defaultProps: { withBorder: true, padding: 'lg', radius: 'lg', shadow: 'none' } },
    Paper: { defaultProps: { withBorder: true, radius: 'lg', shadow: 'none' } },
    Table: { defaultProps: { highlightOnHover: true, verticalSpacing: 'sm' } },
    Badge: { defaultProps: { variant: 'light', radius: 'sm' } },
    Tabs: { defaultProps: { variant: 'pills', radius: 'md' } },
    Alert: { defaultProps: { radius: 'lg', variant: 'light' } },
    TextInput: { styles: fieldLabel, defaultProps: under },
    NumberInput: { styles: fieldLabel, defaultProps: under },
    Select: { styles: fieldLabel, defaultProps: under },
    PasswordInput: { styles: fieldLabel, defaultProps: under },
    MultiSelect: { styles: fieldLabel, defaultProps: under },
    DateInput: { styles: fieldLabel, defaultProps: under },
    JsonInput: { styles: fieldLabel, defaultProps: under },
    Textarea: { styles: fieldLabel, defaultProps: under },
    Autocomplete: { styles: fieldLabel, defaultProps: under },
    TagsInput: { styles: fieldLabel, defaultProps: under },
    Modal: { defaultProps: { radius: 'lg', transitionProps: { duration: 150 } } },
    Drawer: { defaultProps: { transitionProps: { duration: 150 } } },
  },
})

// Page background behind cards; dark scheme keeps Mantine's default body.
export const pageBackground = 'light-dark(#f5f6f8, var(--mantine-color-dark-8))'
