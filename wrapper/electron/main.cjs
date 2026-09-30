const {app,BrowserWindow,ipcMain,shell,net}=require('electron')
const Database=require('better-sqlite3')
const path=require('path')
const fs=require('fs')
const http=require('http')

const APP_URL=(process.env.AITS_APP_URL||process.env.DOMAIN||'https://aits.jpano.dev').replace(/\/$/,'')
let db=null,mainWindow=null,localServer=null,localURL='',showingOffline=false,reconnectTimer=null

function initDatabase(){
  const file=path.join(app.getPath('userData'),'aits-local.sqlite')
  db=new Database(file)
  db.pragma('journal_mode = WAL')
  db.exec('CREATE TABLE IF NOT EXISTS aits_kv (key TEXT PRIMARY KEY NOT NULL, value TEXT NOT NULL, updated_at INTEGER NOT NULL)')
}
function cachedMode(){
  try{
    const mode=db.prepare('SELECT value FROM aits_kv WHERE key=?').get('meta:last-mode')?.value
    if(mode&&JSON.parse(mode)==='member')return 'member'
    if(db.prepare('SELECT 1 FROM aits_kv WHERE key=?').get('meta:member-profile'))return 'member'
  }catch{}
  return 'visitor'
}
function installStorageIPC(){
  ipcMain.handle('aits:storage:get',(_event,key)=>db.prepare('SELECT value FROM aits_kv WHERE key=?').get(String(key))?.value??null)
  ipcMain.handle('aits:storage:put',(_event,key,value)=>{db.prepare('INSERT INTO aits_kv(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at').run(String(key),String(value),Date.now())})
  ipcMain.handle('aits:storage:remove',(_event,key)=>{db.prepare('DELETE FROM aits_kv WHERE key=?').run(String(key))})
  ipcMain.handle('aits:storage:list',(_event,prefix)=>db.prepare('SELECT key,value FROM aits_kv WHERE key LIKE ? ORDER BY key').all(`${String(prefix)}%`))
  ipcMain.handle('aits:storage:clear',()=>{db.prepare('DELETE FROM aits_kv').run()})
}
function contentType(file){const ext=path.extname(file).toLowerCase();return ({'.html':'text/html; charset=utf-8','.js':'text/javascript; charset=utf-8','.css':'text/css; charset=utf-8','.json':'application/json; charset=utf-8','.png':'image/png','.jpg':'image/jpeg','.jpeg':'image/jpeg','.svg':'image/svg+xml','.webp':'image/webp','.woff2':'font/woff2','.ico':'image/x-icon'}[ext]||'application/octet-stream')}
function startLocalServer(){
  const root=path.join(__dirname,'web')
  localServer=http.createServer((req,res)=>{
    const requestPath=decodeURIComponent((req.url||'/').split('?')[0])
    if(requestPath.startsWith('/api/')||requestPath.startsWith('/ws/')){res.writeHead(503,{'Content-Type':'application/json','Cache-Control':'no-store'});res.end(JSON.stringify({ok:false,error:'offline'}));return}
    let relative=requestPath==='/'?'index.html':requestPath.replace(/^\/+/, '')
    let file=path.resolve(root,relative)
    if(!file.startsWith(path.resolve(root))){res.writeHead(403);res.end();return}
    if(!fs.existsSync(file)||fs.statSync(file).isDirectory())file=path.join(root,'index.html')
    if(!fs.existsSync(file)){res.writeHead(503,{'Content-Type':'text/plain'});res.end('AITS offline shell is not packaged. Run npm run sync:web before packaging.');return}
    res.writeHead(200,{'Content-Type':contentType(file),'Cache-Control':'no-store'});fs.createReadStream(file).pipe(res)
  })
  return new Promise(resolve=>localServer.listen(0,'127.0.0.1',()=>{const address=localServer.address();localURL=`http://127.0.0.1:${address.port}`;resolve()}))
}
function remoteBootURL(){return `${APP_URL}${cachedMode()==='member'?'/member':'/'}`}
async function loadRemote(){if(!mainWindow)return;showingOffline=false;await mainWindow.loadURL(remoteBootURL()).catch(()=>loadOffline())}
async function loadOffline(){if(!mainWindow||!localURL)return;showingOffline=true;await mainWindow.loadURL(localURL).catch(()=>undefined)}
function beginReconnectWatch(){
  reconnectTimer=setInterval(async()=>{
    if(!showingOffline||!mainWindow)return
    try{const response=await net.fetch(`${APP_URL}/api/health`,{method:'GET',cache:'no-store'});if(response.ok)await loadRemote()}catch{}
  },7000)
}
async function createWindow(){
  mainWindow=new BrowserWindow({width:1320,height:840,minWidth:880,minHeight:600,show:false,backgroundColor:'#0f1115',autoHideMenuBar:true,webPreferences:{preload:path.join(__dirname,'preload.cjs'),contextIsolation:true,nodeIntegration:false,additionalArguments:[`--aits-boot-mode=${cachedMode()}`,`--aits-server-url=${APP_URL}`]}})
  mainWindow.once('ready-to-show',()=>mainWindow?.show())
  mainWindow.webContents.setWindowOpenHandler(({url})=>{void shell.openExternal(url);return {action:'deny'}})
  mainWindow.webContents.on('will-navigate',(event,url)=>{try{const target=new URL(url);const allowed=new URL(APP_URL);if(target.origin!==allowed.origin&&target.origin!==new URL(localURL).origin){event.preventDefault();void shell.openExternal(url)}}catch{}})
  mainWindow.webContents.on('did-fail-load',(_event,errorCode,_description,url,isMainFrame)=>{if(isMainFrame&&errorCode<0&&url.startsWith(APP_URL))void loadOffline()})
  mainWindow.on('closed',()=>{mainWindow=null})
  await loadRemote()
}

app.whenReady().then(async()=>{initDatabase();installStorageIPC();await startLocalServer();await createWindow();beginReconnectWatch();app.on('activate',()=>{if(BrowserWindow.getAllWindows().length===0)void createWindow()})})
app.on('window-all-closed',()=>{if(process.platform!=='darwin')app.quit()})
app.on('before-quit',()=>{if(reconnectTimer)clearInterval(reconnectTimer);localServer?.close();try{db?.close()}catch{}})
