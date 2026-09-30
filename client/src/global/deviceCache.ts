const DB_NAME='aits-device-cache-v5'
const DB_VERSION=1
const STORES=['posts','meta','outbox','conversations','messages'] as const
const NATIVE_DB='aits_local'

type StoreName=typeof STORES[number]
export type OutboxAction={nonce:string;kind:'post'|'comment'|'chat'|'profile'|'avatar';created_at:number;payload:unknown}

type MetaRow={key:string;value:unknown}
type MessageRow={cache_key:string;conversation_id:number;sort_key:number;message:unknown}
type NativeRow={key:string;value:string}
type NativeStore={get:(key:string)=>Promise<string|null>;put:(key:string,value:string)=>Promise<void>;remove:(key:string)=>Promise<void>;list:(prefix:string)=>Promise<NativeRow[]>;clear:()=>Promise<void>}

declare global{
  interface Window{
    aitsNativeStorage?:NativeStore
    aitsNative?:{platform?:string;installed?:boolean;serverURL?:string;bootMode?:string}
    Capacitor?:{isNativePlatform?:()=>boolean;getPlatform?:()=>string;Plugins?:Record<string,any>;registerPlugin?:(name:string)=>any}
  }
}

let capacitorStorePromise:Promise<NativeStore|null>|null=null
function isCapacitorNative(){try{return Boolean(window.Capacitor?.isNativePlatform?.())}catch{return false}}
async function capacitorStore():Promise<NativeStore|null>{
  if(!isCapacitorNative())return null
  if(capacitorStorePromise)return capacitorStorePromise
  capacitorStorePromise=(async()=>{
    const plugin=window.Capacitor?.Plugins?.CapacitorSQLite||window.Capacitor?.registerPlugin?.('CapacitorSQLite')
    if(!plugin)return null
    try{
      await plugin.createConnection({database:NATIVE_DB,version:1,encrypted:false,mode:'no-encryption',readonly:false}).catch(()=>undefined)
      await plugin.open({database:NATIVE_DB,readonly:false}).catch(()=>undefined)
      await plugin.execute({database:NATIVE_DB,statements:'CREATE TABLE IF NOT EXISTS aits_kv (key TEXT PRIMARY KEY NOT NULL, value TEXT NOT NULL, updated_at INTEGER NOT NULL);',transaction:false})
      return {
        async get(key:string){const result=await plugin.query({database:NATIVE_DB,statement:'SELECT value FROM aits_kv WHERE key = ? LIMIT 1',values:[key]});return result?.values?.[0]?.value??null},
        async put(key:string,value:string){await plugin.run({database:NATIVE_DB,statement:'INSERT INTO aits_kv(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at',values:[key,value,Date.now()],transaction:true})},
        async remove(key:string){await plugin.run({database:NATIVE_DB,statement:'DELETE FROM aits_kv WHERE key = ?',values:[key],transaction:true})},
        async list(prefix:string){const result=await plugin.query({database:NATIVE_DB,statement:'SELECT key,value FROM aits_kv WHERE key LIKE ? ORDER BY key',values:[`${prefix}%`]});return (result?.values||[]) as NativeRow[]},
        async clear(){await plugin.run({database:NATIVE_DB,statement:'DELETE FROM aits_kv',values:[],transaction:true})},
      } satisfies NativeStore
    }catch{return null}
  })()
  return capacitorStorePromise
}
async function nativeStore():Promise<NativeStore|null>{return window.aitsNativeStorage||await capacitorStore()}

function bytesToBase64(bytes:Uint8Array){let binary='';const chunk=0x8000;for(let i=0;i<bytes.length;i+=chunk)binary+=String.fromCharCode(...bytes.subarray(i,Math.min(bytes.length,i+chunk)));return btoa(binary)}
function base64ToBytes(value:string){const binary=atob(value),bytes=new Uint8Array(binary.length);for(let i=0;i<binary.length;i++)bytes[i]=binary.charCodeAt(i);return bytes}
async function encodeNative(value:unknown):Promise<unknown>{
  if(value instanceof Blob){const data=bytesToBase64(new Uint8Array(await value.arrayBuffer()));const file=value instanceof File?value:null;return {__aits_binary__:true,data,type:value.type||'',name:file?.name||'',last_modified:file?.lastModified||0}}
  if(Array.isArray(value))return Promise.all(value.map(item=>encodeNative(item)))
  if(value&&typeof value==='object'){const out:Record<string,unknown>={};for(const [key,item] of Object.entries(value as Record<string,unknown>))out[key]=await encodeNative(item);return out}
  return value
}
function decodeNative(value:unknown):unknown{
  if(Array.isArray(value))return value.map(decodeNative)
  if(value&&typeof value==='object'){
    const row=value as Record<string,unknown>
    if(row.__aits_binary__===true&&typeof row.data==='string'){
      const bytes=base64ToBytes(row.data),type=typeof row.type==='string'?row.type:'',name=typeof row.name==='string'?row.name:''
      if(name&&typeof File!=='undefined')return new File([bytes],name,{type,lastModified:Number(row.last_modified||0)||Date.now()})
      return new Blob([bytes],{type})
    }
    const out:Record<string,unknown>={};for(const [key,item] of Object.entries(row))out[key]=decodeNative(item);return out
  }
  return value
}
async function nativeWrite(key:string,value:unknown){const store=await nativeStore();if(!store)return false;await store.put(key,JSON.stringify(await encodeNative(value)));return true}
async function nativeRead<T>(key:string):Promise<{available:boolean;value:T|null}>{const store=await nativeStore();if(!store)return {available:false,value:null};const raw=await store.get(key);if(raw===null)return {available:true,value:null};try{return {available:true,value:decodeNative(JSON.parse(raw)) as T}}catch{return {available:true,value:null}}}
async function nativeList<T>(prefix:string):Promise<{available:boolean;rows:Array<{key:string;value:T}>}>{const store=await nativeStore();if(!store)return {available:false,rows:[]};const rows=await store.list(prefix);return {available:true,rows:rows.flatMap(row=>{try{return [{key:row.key,value:decodeNative(JSON.parse(row.value)) as T}]}catch{return []}})}}
async function nativeRemove(key:string){const store=await nativeStore();if(!store)return false;await store.remove(key);return true}

function openDB():Promise<IDBDatabase>{
  return new Promise((resolve,reject)=>{
    if(!('indexedDB' in window)){reject(new Error('IndexedDB unavailable'));return}
    const req=indexedDB.open(DB_NAME,DB_VERSION)
    req.onupgradeneeded=()=>{
      const db=req.result
      if(!db.objectStoreNames.contains('posts'))db.createObjectStore('posts',{keyPath:'id'})
      if(!db.objectStoreNames.contains('meta'))db.createObjectStore('meta',{keyPath:'key'})
      if(!db.objectStoreNames.contains('outbox'))db.createObjectStore('outbox',{keyPath:'nonce'})
      if(!db.objectStoreNames.contains('conversations'))db.createObjectStore('conversations',{keyPath:'id'})
      if(!db.objectStoreNames.contains('messages')){const store=db.createObjectStore('messages',{keyPath:'cache_key'});store.createIndex('conversation_id','conversation_id',{unique:false})}
    }
    req.onsuccess=()=>resolve(req.result)
    req.onerror=()=>reject(req.error)
  })
}

async function transaction<T>(storeName:StoreName,mode:IDBTransactionMode,run:(store:IDBObjectStore,done:(value:T)=>void,fail:(reason?:unknown)=>void)=>void):Promise<T>{
  const db=await openDB()
  try{return await new Promise<T>((resolve,reject)=>{const tx=db.transaction(storeName,mode);const store=tx.objectStore(storeName);let settled=false;const done=(value:T)=>{settled=true;resolve(value)};const fail=(reason?:unknown)=>{settled=true;reject(reason)};run(store,done,fail);tx.onerror=()=>{if(!settled)reject(tx.error)};tx.onabort=()=>{if(!settled)reject(tx.error)}})}finally{db.close()}
}

export async function putMeta(key:string,value:unknown){if(await nativeWrite(`meta:${key}`,value))return;await transaction<void>('meta','readwrite',(store,done)=>{store.put({key,value} satisfies MetaRow);store.transaction.oncomplete=()=>done()})}
export async function getMeta<T>(key:string):Promise<T|null>{const native=await nativeRead<T>(`meta:${key}`);if(native.available)return native.value;return transaction<T|null>('meta','readonly',(store,done,fail)=>{const req=store.get(key);req.onsuccess=()=>done((req.result?.value??null) as T|null);req.onerror=()=>fail(req.error)})}

export async function cachePosts<T extends {id:number}>(rows:T[]){if(!rows.length)return;const native=await nativeStore();if(native){for(const row of rows)await nativeWrite(`posts:${row.id}`,row);return}await transaction<void>('posts','readwrite',(store,done)=>{for(const row of rows)store.put(row);store.transaction.oncomplete=()=>done()})}
export async function readPosts<T>(limit=25):Promise<T[]>{const native=await nativeList<T>('posts:');if(native.available)return native.rows.map(row=>row.value).sort((a,b)=>Number((b as any).id||0)-Number((a as any).id||0)).slice(0,limit);return transaction<T[]>('posts','readonly',(store,done,fail)=>{const out:T[]=[];const req=store.openCursor(null,'prev');req.onsuccess=()=>{const cursor=req.result;if(!cursor||out.length>=limit){done(out);return}out.push(cursor.value as T);cursor.continue()};req.onerror=()=>fail(req.error)})}

export async function cacheConversations<T extends {id:number}>(rows:T[]){const native=await nativeStore();if(native){for(const row of rows)await nativeWrite(`conversations:${row.id}`,row);return}await transaction<void>('conversations','readwrite',(store,done)=>{for(const row of rows)store.put(row);store.transaction.oncomplete=()=>done()})}
export async function readConversations<T>():Promise<T[]>{const native=await nativeList<T>('conversations:');if(native.available)return native.rows.map(row=>row.value);return transaction<T[]>('conversations','readonly',(store,done,fail)=>{const req=store.getAll();req.onsuccess=()=>done((req.result||[]) as T[]);req.onerror=()=>fail(req.error)})}

export async function cacheMessages<T extends {id:number;client_nonce?:string}>(conversationID:number,rows:T[]){if(!rows.length)return;const native=await nativeStore();if(native){for(const row of rows){if(row.id>0&&row.client_nonce)await nativeRemove(`messages:${conversationID}:nonce:${row.client_nonce}`);const token=row.id>0?`id:${row.id}`:`nonce:${row.client_nonce||crypto.randomUUID?.()||Date.now()}`;await nativeWrite(`messages:${conversationID}:${token}`,{sort_key:row.id>0?row.id:Date.now(),message:row})}return}await transaction<void>('messages','readwrite',(store,done)=>{for(const row of rows){if(row.id>0&&row.client_nonce)store.delete(`${conversationID}:nonce:${row.client_nonce}`);const token=row.id>0?`id:${row.id}`:`nonce:${row.client_nonce||crypto.randomUUID?.()||Date.now()}`;store.put({cache_key:`${conversationID}:${token}`,conversation_id:conversationID,sort_key:row.id>0?row.id:Date.now(),message:row} satisfies MessageRow)}store.transaction.oncomplete=()=>done()})}
export async function readMessages<T extends {id?:number;client_nonce?:string}>(conversationID:number):Promise<T[]>{
  const dedupe=(rows:Array<{sort_key:number;message:T}>)=>{rows.sort((a,b)=>a.sort_key-b.sort_key);const seenIds=new Set<number>(),seenNonces=new Set<string>();const out:T[]=[];for(const row of rows){const message=row.message,id=Number(message.id||0),nonce=String(message.client_nonce||'');if(id>0&&seenIds.has(id))continue;if(nonce&&seenNonces.has(nonce)){const idx=out.findIndex(item=>String(item.client_nonce||'')===nonce);if(idx>=0&&id>0)out[idx]=message;continue}if(id>0)seenIds.add(id);if(nonce)seenNonces.add(nonce);out.push(message)}return out}
  const native=await nativeList<{sort_key:number;message:T}>(`messages:${conversationID}:`);if(native.available)return dedupe(native.rows.map(row=>row.value))
  return transaction<T[]>('messages','readonly',(store,done,fail)=>{const index=store.index('conversation_id');const req=index.getAll(IDBKeyRange.only(conversationID));req.onsuccess=()=>{const rows=(req.result||[]) as MessageRow[];done(dedupe(rows.map(row=>({sort_key:row.sort_key,message:row.message as T}))))};req.onerror=()=>fail(req.error)})
}
export async function removeCachedMessage(conversationID:number,messageID:number){if(messageID<1)return;if(await nativeRemove(`messages:${conversationID}:id:${messageID}`))return;await transaction<void>('messages','readwrite',(store,done)=>{store.delete(`${conversationID}:id:${messageID}`);store.transaction.oncomplete=()=>done()})}

export async function queueOutbox(action:OutboxAction){if(await nativeWrite(`outbox:${action.nonce}`,action))return;await transaction<void>('outbox','readwrite',(store,done)=>{store.put(action);store.transaction.oncomplete=()=>done()})}
export async function listOutbox():Promise<OutboxAction[]>{const native=await nativeList<OutboxAction>('outbox:');if(native.available)return native.rows.map(row=>row.value).sort((a,b)=>a.created_at-b.created_at);return transaction<OutboxAction[]>('outbox','readonly',(store,done,fail)=>{const req=store.getAll();req.onsuccess=()=>done(((req.result||[]) as OutboxAction[]).sort((a,b)=>a.created_at-b.created_at));req.onerror=()=>fail(req.error)})}
export async function removeOutbox(nonce:string){if(await nativeRemove(`outbox:${nonce}`))return;await transaction<void>('outbox','readwrite',(store,done)=>{store.delete(nonce);store.transaction.oncomplete=()=>done()})}

export async function clearMemberCache(){const native=await nativeStore();if(native){await native.clear().catch(()=>undefined);return}if(!('indexedDB' in window))return;await new Promise<void>((resolve)=>{const req=indexedDB.deleteDatabase(DB_NAME);req.onsuccess=()=>resolve();req.onerror=()=>resolve();req.onblocked=()=>resolve()})}
