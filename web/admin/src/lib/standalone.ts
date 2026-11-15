import type { Inbound } from './api'
import { entryEndpoint, type InboundChoice } from './entry-flow.ts'

export type StandaloneAuth = { username?: string; password?: string }
export const standaloneAuth = (ib: Inbound): StandaloneAuth | undefined => ib.Settings?.standalone as StandaloneAuth | undefined
export function standaloneConnection(choice: InboundChoice): string | undefined {
 const auth = standaloneAuth(choice.ib)
 const endpoint = entryEndpoint(choice)
 if (!auth || !endpoint.DisplayHost || !endpoint.DisplayPort) return undefined
 return JSON.stringify({type:choice.ib.Protocol === 'socks'?'socks5':'shadowsocks',server:endpoint.DisplayHost,port:endpoint.DisplayPort,...(choice.ib.Protocol === 'socks'?{username:auth.username,password:auth.password}:{method:choice.ib.Settings.cipher,password:choice.ib.Settings.server_key})},null,2)
}
export function standaloneKey(cipher: string): string {
 const bytes = new Uint8Array(cipher === '2022-blake3-aes-128-gcm'?16:32)
 crypto.getRandomValues(bytes)
 return btoa(String.fromCharCode(...bytes))
}

// Dedicated secret controls must not be defeated by the advanced JSON editor.
export function standaloneProtocolJSON(raw: string): string {
 const value=JSON.parse(raw||'{}')
 if(!value.standalone)return raw
 const {standalone:_auth,server_key:_key,...rest}=value
 return JSON.stringify(rest,null,2)
}
export function mergeStandaloneProtocolJSON(current: string, draft: string): string {
 const value=JSON.parse(current||'{}')
 if(!value.standalone)return draft
 const patch=JSON.parse(draft||'{}')
 if(!patch||typeof patch!=='object'||Array.isArray(patch))throw new Error('Protocol settings must be an object')
 return JSON.stringify({...patch,standalone:value.standalone,...(value.server_key?{server_key:value.server_key}: {})},null,2)
}
