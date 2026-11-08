import { ManagedDomainFields, type ManagedDomainProps } from './ManagedDomainFields'
import type { NodeDomainCheck } from '../lib/use-node-domain-check'

export function NodeDomainFields(props: ManagedDomainProps & { shared: boolean; onSharedChange: (value: boolean) => void; check: NodeDomainCheck }) {
  return <ManagedDomainFields {...props} />
}
