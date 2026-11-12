export const planNameKey = (value: string) => value.trim().toLowerCase()
export function planNameError(name: string, plans: {ID:number;Name:string}[], id=0): 'required' | 'duplicate' | null {
 const normalized=name.trim()
 if (!normalized || [...normalized].length>200 || /[\u0000-\u001f\u007f-\u009f]/.test(normalized)) return 'required'
 // Unchanged historical duplicates must not block unrelated edits.
 if (id && plans.some(p=>p.ID===id && p.Name===name)) return null
 return plans.some(p=>p.ID!==id && planNameKey(p.Name)===planNameKey(name))?'duplicate':null
}
