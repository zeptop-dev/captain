// Empty selects all categories; 0 selects entries without a category. Category
// filters preserve the existing entry order and never inspect tags or groups.
export function matchesEntryCategory(entry: { CategoryID?: number | null }, category: string | null): boolean {
  return !category || (entry.CategoryID || 0) === Number(category)
}

// A batch operation must include only rows the administrator can currently see.
// This also discards entries removed by a background refresh.
export function visibleEntrySelection(selected: number[], visible: { ID: number }[]): number[] {
  const selection = new Set(selected)
  return visible.filter(entry => selection.has(entry.ID)).map(entry => entry.ID)
}
