import { emojiToRegionMap } from './regionHelper.ts'
import { COORD_MAP } from '../../../shared/country-coordinates.ts'
export { COORD_MAP } from '../../../shared/country-coordinates.ts'

const ISO_CODE_REGEX = /^[A-Z]{2}$/

/**
 * 从节点 region 字段提取 ISO 2 字母代码。
 * 支持：emoji 国旗、ISO 代码字符串、大小写、空白填充。
 */
export function getCountryCodeFromRegion(region: string | undefined | null): string | null {
  if (!region)
    return null
  const trimmed = region.trim()
  if (!trimmed)
    return null

  // 国旗 emoji → ISO 代码
  const info = emojiToRegionMap[trimmed]
  if (info)
    return info.code

  const upper = trimmed.toUpperCase()
  if (ISO_CODE_REGEX.test(upper) && upper in COORD_MAP)
    return upper

  return null
}

/**
 * 按 ISO 代码取坐标，返回 null 表示无法定位。
 */
export function getCoordByCode(code: string | null | undefined): [number, number] | null {
  if (!code)
    return null
  return COORD_MAP[code] ?? null
}
