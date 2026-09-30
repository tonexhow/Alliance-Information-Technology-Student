import {cp,rm,mkdir,access} from 'node:fs/promises'
import {resolve} from 'node:path'
import {fileURLToPath} from 'node:url'

const wrapper=resolve(fileURLToPath(new URL('..',import.meta.url)))
const source=resolve(wrapper,'../../client/dist')
const target=resolve(wrapper,'web')
try{await access(source)}catch{throw new Error('client/dist is missing. Run npm run build inside client first.')}
await rm(target,{recursive:true,force:true})
await mkdir(target,{recursive:true})
await cp(source,target,{recursive:true})
console.log(`Synced ${source} -> ${target}`)
