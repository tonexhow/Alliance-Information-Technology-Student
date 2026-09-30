const {contextBridge,ipcRenderer}=require('electron')
const bootArg=process.argv.find(value=>value.startsWith('--aits-boot-mode='))
const serverArg=process.argv.find(value=>value.startsWith('--aits-server-url='))
const bootMode=bootArg?bootArg.slice('--aits-boot-mode='.length):'visitor'
const serverURL=serverArg?serverArg.slice('--aits-server-url='.length):''
contextBridge.exposeInMainWorld('aitsNative',{platform:'electron',installed:true,bootMode,serverURL})
contextBridge.exposeInMainWorld('aitsNativeStorage',{
  get:key=>ipcRenderer.invoke('aits:storage:get',key),
  put:(key,value)=>ipcRenderer.invoke('aits:storage:put',key,value),
  remove:key=>ipcRenderer.invoke('aits:storage:remove',key),
  list:prefix=>ipcRenderer.invoke('aits:storage:list',prefix),
  clear:()=>ipcRenderer.invoke('aits:storage:clear')
})
