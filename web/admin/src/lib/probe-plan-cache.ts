import type { QueryClient } from '@tanstack/react-query'

// A saved plan invalidates the meaning of cached carrier results. Merely marking
// an inactive query stale would still render its old plan on the next visit.
export async function clearProbePlanResults(client: QueryClient, nodeID?: number) {
  const queryKey = nodeID === undefined ? ['network-quality'] : ['network-quality', nodeID]
  await client.cancelQueries({ queryKey })
  client.removeQueries({ queryKey })
}
