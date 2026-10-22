// Match the probe's existing 1024-based traffic units, including its GB/TB labels.
export const trafficLimitFactors = { MB: 2 ** 20, GB: 2 ** 30, TB: 2 ** 40 }
export type TrafficLimitUnit = keyof typeof trafficLimitFactors
export interface TrafficLimitInput { bytes: number; amount: number | string; unit: TrafficLimitUnit }

export function trafficLimitBytes(amount: number | string, unit: TrafficLimitUnit): number {
  return Math.round(Number(amount) * trafficLimitFactors[unit])
}

export function validTrafficLimit(input: TrafficLimitInput): boolean {
  return Number.isSafeInteger(input.bytes) && input.bytes >= 0 && (Number(input.amount) === 0 || input.bytes > 0)
}

export function trafficLimitInput(bytes: number, unit: TrafficLimitUnit = bytes >= 2 ** 40 ? 'TB' : bytes > 0 && bytes < 2 ** 30 ? 'MB' : 'GB'): TrafficLimitInput {
  const amount = bytes / trafficLimitFactors[unit]
  // Use the shortest decimal that maps back to the same whole byte. This avoids
  // showing 1.1000000000003638 for a saved 1.1 TB without rounding old allowances.
  for (let decimals = 0; decimals <= 15; decimals++) {
    const rounded = Number(amount.toFixed(decimals))
    if (trafficLimitBytes(rounded, unit) === bytes) return { bytes, amount: rounded, unit }
  }
  return { bytes, amount, unit }
}
