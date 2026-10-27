import { Alert, Box, Button, Group, Modal, Stack, Text } from '@mantine/core'
import { useIsMutating } from '@tanstack/react-query'
import { useForm, type UseFormInput } from '@mantine/form'
import { createContext, useCallback, useContext, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { useBeforeUnload, useBlocker } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

const DraftStatusContext = createContext(false)
export function useSettingsHaveDrafts() { return useContext(DraftStatusContext) }

const DraftContext = createContext<(id: string, dirty: boolean) => void>(() => {})

// Each editor registers its own draft. Saving a test message or a different form
// must never clear another editor's unsaved changes.
export function useSettingsDirty(dirty: boolean) {
  const id = useId()
  const register = useContext(DraftContext)
  useEffect(() => { register(id, dirty); return () => register(id, false) }, [id, dirty, register])
}

export function useSettingsForm<Values extends object>(input: UseFormInput<Values>) {
  const form = useForm(input)
  const loaded = useRef(false)
  useSettingsDirty(form.isDirty())
  return {
    ...form,
    hydrate(values: Values) {
      if (!loaded.current || !form.isDirty()) {
        form.setValues(values)
        form.resetDirty(values)
        loaded.current = true
      }
    },
  }
}

export function SettingsDraftBoundary({ children, allowHashNavigation = false }: { children: ReactNode; allowHashNavigation?: boolean }) {
  const { t } = useTranslation()
  const [drafts, setDrafts] = useState<Set<string>>(() => new Set())
  const register = useCallback((id: string, dirty: boolean) => {
    setDrafts(previous => {
      if (previous.has(id) === dirty) return previous
      const next = new Set(previous)
      if (dirty) next.add(id); else next.delete(id)
      return next
    })
  }, [])
  const busy = useIsMutating() > 0
  const dirty = drafts.size > 0
  const blocker = useBlocker(({ currentLocation, nextLocation }) => dirty && !(allowHashNavigation && currentLocation.pathname === nextLocation.pathname && currentLocation.search === nextLocation.search))
  useBeforeUnload(useCallback((event: BeforeUnloadEvent) => {
    if (dirty) { event.preventDefault(); event.returnValue = '' }
  }, [dirty]))
  return <DraftContext.Provider value={register}><DraftStatusContext.Provider value={dirty}>
    <Stack>
      {dirty && <Alert color="yellow" role="status">{t('workspace.unsaved')}</Alert>}
      <Box component="fieldset" disabled={busy} inert={busy} aria-busy={busy} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>{children}</Box>
    </Stack>
    <Modal opened={blocker.state === 'blocked'} onClose={() => blocker.state === 'blocked' && blocker.reset()} title={t('workspace.unsaved')} centered>
      <Text size="sm">{t('workspace.leaveHint')}</Text>
      <Group justify="flex-end" mt="md">
        <Button variant="default" onClick={() => blocker.state === 'blocked' && blocker.reset()}>{t('workspace.stay')}</Button>
        <Button color="red" onClick={() => blocker.state === 'blocked' && blocker.proceed()}>{t('workspace.discard')}</Button>
      </Group>
    </Modal>
  </DraftStatusContext.Provider></DraftContext.Provider>
}
