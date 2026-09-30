import { memo, useState, type CSSProperties, type FocusEvent, type MouseEvent, type ReactNode } from 'react'

type MentionHandler=(accountId:number,event:MouseEvent<HTMLElement>|FocusEvent<HTMLElement>)=>void
const emojiMap:Record<string,string>={heart:'❤️',thumbsup:'👍',like:'👍',laughing:'😂',joy:'😂',sob:'😭',cry:'😢',fire:'🔥',tada:'🎉',warning:'⚠️',white_check_mark:'✅',x:'❌',eyes:'👀',wave:'👋',clap:'👏',thinking:'🤔'}
function safeHref(value:string){const v=value.trim();return /^(https?:\/\/|#)/i.test(v)?v:'#'}
function safeSrc(value:string){const v=value.trim();return /^(https?:\/\/|\/|\.\.?\/|[a-z0-9_-]+(?:\/[a-z0-9_.-]+)*\.[a-z0-9]{2,8}(?:[?#].*)?$)/i.test(v)?v:''}
function attr(tag:string,name:string){return tag.match(new RegExp(`${name}\\s*=\\s*["']([^"']+)["']`,'i'))?.[1]||''}
function stripTags(value:string){return value.replace(/<!--[^]*?-->/g,'').replace(/<[^>]+>/g,'').trim()}
function safeStyle(raw:string):CSSProperties{
  const style:CSSProperties={}
  const color=/^(?:#[0-9a-f]{3,8}|(?:rgb|hsl)a?\([^)]{1,80}\)|[a-z]{1,32})$/i
  for(const declaration of raw.split(';')){
    const split=declaration.indexOf(':');if(split<1)continue
    const key=declaration.slice(0,split).trim().toLowerCase(),value=declaration.slice(split+1).trim();if(!value||/[{}<>]|url\s*\(/i.test(value))continue
    if(key==='color'&&color.test(value))style.color=value
    else if(key==='background-color'&&color.test(value))style.backgroundColor=value
    else if(key==='text-align'&&/^(left|center|right|justify)$/i.test(value))style.textAlign=value.toLowerCase() as CSSProperties['textAlign']
    else if(key==='font-weight'&&/^(normal|bold|[1-9]00)$/i.test(value))style.fontWeight=value as CSSProperties['fontWeight']
    else if(key==='font-style'&&/^(normal|italic|oblique)$/i.test(value))style.fontStyle=value as CSSProperties['fontStyle']
    else if(key==='text-decoration'&&/^(none|underline|line-through|overline)(?:\s+(?:underline|line-through|overline))*$/i.test(value))style.textDecoration=value
    else if(key==='font-size'&&/^(?:[0-9]{1,3}(?:\.[0-9]+)?(?:px|em|rem|%)|small|medium|large|x-large)$/i.test(value))style.fontSize=value
  }
  return style
}

function inline(text:string,onMention?:MentionHandler):ReactNode[]{
  const out:ReactNode[]=[];let rest=text;let key=0
  const rx=/(\\[*_~+^`]|!\[[^\]]*\]\([^\s)]+\)|\*\*\*[^*\n]+\*\*\*|___[^_\n]+___|\*\*[^*\n]+\*\*|__[^_\n]+__|~~[^~\n]+~~|==[^=\n]+==|\+\+[^+\n]+\+\+|`[^`\n]+`|\*[^*\n]+\*|_[^_\n]+_|\^[^^\n]+\^|~[^~\n]+~|\$[^$\n]+\$|\[\^[^\]]+\]|@\[[^\]]+\]\(account:\d+\)|\[[^\]]+\]\((?:https?:\/\/|#)[^\s)]+\)|<a\s+[^>]*href=["'][^"']+["'][^>]*>[^<]*<\/a>|<img\s+[^>]*src=["'][^"']+["'][^>]*\/?>|<span\s+[^>]*>[^<]*<\/span>|<\/?(?:b|strong|i|em|u|s|del|ins|mark|code|kbd|span)\s*\/?\s*>|<br\s*\/?>|@everyone\b|:[a-z0-9_+-]+:|https?:\/\/[^\s<>()]+)/i
  while(rest){
    const m=rest.match(rx);if(!m||m.index===undefined){out.push(rest);break}
    if(m.index>0)out.push(rest.slice(0,m.index));const t=m[0]
    if(t.startsWith('\\'))out.push(t.slice(1))
    else if(t.startsWith('![')){const z=t.match(/^!\[([^\]]*)\]\(([^\s)]+)\)$/);if(z){const src=safeSrc(z[2]);if(src)out.push(<img key={key++} src={src} alt={z[1]} loading="lazy"/>);else out.push(t)}}
    else if(t.startsWith('***')||t.startsWith('___'))out.push(<strong key={key++}><em>{inline(t.slice(3,-3),onMention)}</em></strong>)
    else if(t.startsWith('**')||t.startsWith('__'))out.push(<strong key={key++}>{inline(t.slice(2,-2),onMention)}</strong>)
    else if(t.startsWith('~~'))out.push(<del key={key++}>{inline(t.slice(2,-2),onMention)}</del>)
    else if(t.startsWith('=='))out.push(<mark key={key++}>{inline(t.slice(2,-2),onMention)}</mark>)
    else if(t.startsWith('++'))out.push(<ins key={key++}>{inline(t.slice(2,-2),onMention)}</ins>)
    else if(t.startsWith('`'))out.push(<code key={key++}>{t.slice(1,-1)}</code>)
    else if(t.startsWith('*')||t.startsWith('_'))out.push(<em key={key++}>{inline(t.slice(1,-1),onMention)}</em>)
    else if(t.startsWith('^'))out.push(<sup key={key++}>{inline(t.slice(1,-1),onMention)}</sup>)
    else if(t.startsWith('~'))out.push(<sub key={key++}>{inline(t.slice(1,-1),onMention)}</sub>)
    else if(t.startsWith('$'))out.push(<span key={key++} className="markdown-math">{t.slice(1,-1)}</span>)
    else if(t.startsWith('[^')){const z=t.match(/^\[\^([^\]]+)\]$/);if(z)out.push(<a key={key++} className="markdown-footnote-ref" href={`#fn-${z[1]}`}><sup>{z[1]}</sup></a>)}
    else if(t.startsWith('@[')){const z=t.match(/^@\[([^\]]+)\]\(account:(\d+)\)$/);if(z){const id=Number(z[2]);out.push(<button key={key++} type="button" className="markdown-mention" onMouseEnter={e=>onMention?.(id,e)} onFocus={e=>onMention?.(id,e)} onClick={e=>onMention?.(id,e)}>@{z[1]}</button>)}}
    else if(/^@everyone$/i.test(t))out.push(<strong key={key++} className="markdown-everyone">@everyone</strong>)
    else if(/^:[a-z0-9_+-]+:$/i.test(t)){const name=t.slice(1,-1).toLowerCase();out.push(emojiMap[name]||t)}
    else if(t.startsWith('[')){const z=t.match(/^\[([^\]]+)\]\(((?:https?:\/\/|#)[^\s)]+)\)$/);if(z)out.push(<a key={key++} href={safeHref(z[2])} target={z[2].startsWith('#')?undefined:'_blank'} rel="noreferrer">{z[1]}</a>)}
    else if(/^<a\s/i.test(t)){const href=safeHref(attr(t,'href'));out.push(<a key={key++} href={href} target={href.startsWith('#')?undefined:'_blank'} rel="noreferrer">{stripTags(t)}</a>)}
    else if(/^<img\s/i.test(t)){const src=safeSrc(attr(t,'src')),alt=attr(t,'alt'),width=Math.min(1200,Math.max(1,Number(attr(t,'width'))||0))||undefined,height=Math.min(1200,Math.max(1,Number(attr(t,'height'))||0))||undefined;if(src)out.push(<img key={key++} src={src} alt={alt} width={width} height={height} loading="lazy"/>);else out.push(stripTags(t))}
    else if(/^<span\s/i.test(t)){const z=t.match(/^<span([^>]*)>([^<]*)<\/span>$/i);if(z)out.push(<span key={key++} style={safeStyle(attr(z[1],'style'))}>{inline(z[2],onMention)}</span>);else out.push(stripTags(t))}
    else if(/^<br/i.test(t))out.push(<br key={key++}/>)
    else if(/^https?:\/\//i.test(t))out.push(<a key={key++} href={t} target="_blank" rel="noreferrer">{t}</a>)
    else {
      const open=t.match(/^<(b|strong|i|em|u|s|del|ins|mark|code|kbd|span)>$/i)
      if(open){const tag=open[1].toLowerCase(),after=rest.slice((m.index||0)+t.length),cm=after.match(new RegExp(`</${tag}>`,'i'));if(cm&&cm.index!==undefined){const children=inline(after.slice(0,cm.index),onMention);const node=tag==='b'||tag==='strong'?<strong key={key++}>{children}</strong>:tag==='i'||tag==='em'?<em key={key++}>{children}</em>:tag==='u'?<u key={key++}>{children}</u>:tag==='s'||tag==='del'?<del key={key++}>{children}</del>:tag==='ins'?<ins key={key++}>{children}</ins>:tag==='mark'?<mark key={key++}>{children}</mark>:tag==='kbd'?<kbd key={key++}>{children}</kbd>:tag==='span'?<span key={key++}>{children}</span>:<code key={key++}>{children}</code>;out.push(node);rest=after.slice(cm.index+cm[0].length);continue}else out.push(t)} else out.push(t)
    }
    rest=rest.slice((m.index||0)+t.length)
  }
  return out
}

function CodeBlock({code,language}:{code:string;language?:string}){const [copied,setCopied]=useState(false);return <div className="markdown-code-wrap"><button type="button" className="code-copy" onClick={()=>{void navigator.clipboard?.writeText(code);setCopied(true);window.setTimeout(()=>setCopied(false),1200)}}>{copied?'Copied':'Copy'}</button><pre className="markdown-code-block" data-language={language||undefined}><code>{code}</code></pre></div>}
function tableAlign(cell:string){if(/^:\s*-+\s*:$/.test(cell))return 'center';if(/^-+\s*:$/.test(cell))return 'right';return 'left'}
function cells(line:string){return line.trim().replace(/^\||\|$/g,'').split('|').map(v=>v.trim())}

function MarkdownComponent({text,onMention}:{text:string;onMention?:MentionHandler}){
  const source=text.replace(/\r\n?/g,'\n').replace(/<!--[^]*?-->/g,'')
  const lines=source.split('\n');const nodes:ReactNode[]=[];let i=0
  while(i<lines.length){const line=lines[i],trim=line.trim()
    if(trim.startsWith('```')){const language=trim.slice(3).trim(),buf:string[]=[];i++;while(i<lines.length&&!lines[i].trim().startsWith('```'))buf.push(lines[i++]);if(i<lines.length)i++;nodes.push(<CodeBlock key={`code-${i}`} code={buf.join('\n')} language={language}/>);continue}
    if(trim==='$$'){const buf:string[]=[];i++;while(i<lines.length&&lines[i].trim()!=='$$')buf.push(lines[i++]);if(i<lines.length)i++;nodes.push(<div key={`math-${i}`} className="markdown-math-block">{buf.join('\n')}</div>);continue}
    if(/^<pre[ >]/i.test(trim)||trim==='<pre>'){const buf:string[]=[];i++;while(i<lines.length&&!/<\/pre>/i.test(lines[i]))buf.push(lines[i++]);if(i<lines.length)i++;nodes.push(<CodeBlock key={`pre-${i}`} code={stripTags(buf.join('\n'))}/>);continue}
    if(/^<details/i.test(trim)){const buf:string[]=[line];i++;while(i<lines.length&&!/<\/details>/i.test(lines[i]))buf.push(lines[i++]);if(i<lines.length)buf.push(lines[i++]);const raw=buf.join('\n'),summary=raw.match(/<summary>([^]*?)<\/summary>/i)?.[1]||'Details',content=raw.replace(/[^]*?<\/summary>/i,'').replace(/<\/details>[^]*/i,'');nodes.push(<details key={`details-${i}`}><summary>{inline(stripTags(summary),onMention)}</summary><div>{inline(stripTags(content),onMention)}</div></details>);continue}
    if(/^<table/i.test(trim)){const buf:string[]=[line];i++;while(i<lines.length&&!/<\/table>/i.test(lines[i]))buf.push(lines[i++]);if(i<lines.length)buf.push(lines[i++]);const raw=buf.join('\n'),rows=[...raw.matchAll(/<tr[^>]*>([^]*?)<\/tr>/gi)];nodes.push(<div key={`html-table-${i}`} className="markdown-table-wrap"><table><tbody>{rows.map((r,ri)=><tr key={ri}>{[...r[1].matchAll(/<(th|td)([^>]*)>([^]*?)<\/\1>/gi)].map((c,ci)=>{const Tag=c[1].toLowerCase()==='th'?'th':'td',align=(attr(c[2],'align')||'left') as 'left'|'center'|'right';return Tag==='th'?<th key={ci} style={{textAlign:align}}>{inline(stripTags(c[3]),onMention)}</th>:<td key={ci} style={{textAlign:align}}>{inline(stripTags(c[3]),onMention)}</td>})}</tr>)}</tbody></table></div>);continue}
    if(/^<(ul|ol)>$/i.test(trim)){const ordered=/^<ol>/i.test(trim),buf:string[]=[];i++;while(i<lines.length&&!new RegExp(`</${ordered?'ol':'ul'}>`,'i').test(lines[i]))buf.push(lines[i++]);if(i<lines.length)i++;const lis=[...buf.join('\n').matchAll(/<li[^>]*>([^]*?)<\/li>/gi)].map(m=>stripTags(m[1]));nodes.push(ordered?<ol key={`html-ol-${i}`}>{lis.map((v,n)=><li key={n}>{inline(v,onMention)}</li>)}</ol>:<ul key={`html-ul-${i}`}>{lis.map((v,n)=><li key={n}>{inline(v,onMention)}</li>)}</ul>);continue}
    const htmlHeading=trim.match(/^<h([1-6])([^>]*)>([^]*?)<\/h\1>$/i);if(htmlHeading){const L=Number(htmlHeading[1]),content=inline(stripTags(htmlHeading[3]),onMention),style=safeStyle(attr(htmlHeading[2],'style'));nodes.push(L===1?<h1 key={i} style={style}>{content}</h1>:L===2?<h2 key={i} style={style}>{content}</h2>:L===3?<h3 key={i} style={style}>{content}</h3>:L===4?<h4 key={i} style={style}>{content}</h4>:L===5?<h5 key={i} style={style}>{content}</h5>:<h6 key={i} style={style}>{content}</h6>);i++;continue}
    const para=trim.match(/^<(p|div)([^>]*)>([^]*?)<\/\1>$/i);if(para){const style=safeStyle(attr(para[2],'style'));nodes.push(para[1].toLowerCase()==='div'?<div key={i} style={style}>{inline(stripTags(para[3]),onMention)}</div>:<p key={i} style={style}>{inline(stripTags(para[3]),onMention)}</p>);i++;continue}
    const htmlQuote=trim.match(/^<blockquote([^>]*)>([^]*?)<\/blockquote>$/i);if(htmlQuote){nodes.push(<blockquote key={i} style={safeStyle(attr(htmlQuote[1],'style'))}>{inline(stripTags(htmlQuote[2]),onMention)}</blockquote>);i++;continue}
    if(/^<hr\s*\/?\s*>$/i.test(trim)||/^\s*(---|___|\*\*\*)\s*$/.test(line)){nodes.push(<hr key={`hr-${i}`}/>);i++;continue}
    const callout=trim.match(/^>\s*\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*(.*)$/i);if(callout){const kind=callout[1].toUpperCase(),buf=[callout[2]];i++;while(i<lines.length&&/^\s*>/.test(lines[i]))buf.push(lines[i++].replace(/^\s*>\s?/,''));nodes.push(<aside key={`callout-${i}`} className={`markdown-callout ${kind.toLowerCase()}`}><strong>{kind}</strong><div>{buf.map((v,n)=><p key={n}>{inline(v,onMention)}</p>)}</div></aside>);continue}
    if(/^\s*>\s?/.test(line)){const buf:string[]=[];while(i<lines.length&&/^\s*>\s?/.test(lines[i]))buf.push(lines[i++].replace(/^\s*>\s?/,''));nodes.push(<blockquote key={`quote-${i}`}>{buf.map((v,n)=><p key={n}>{inline(v,onMention)}</p>)}</blockquote>);continue}
    const heading=line.match(/^\s*(#{1,6})\s+(.+)$/);if(heading){const L=heading[1].length,c=inline(heading[2],onMention),id=heading[2].toLowerCase().replace(/[^a-z0-9]+/g,'-').replace(/^-|-$/g,'');nodes.push(L===1?<h1 id={id} key={i}>{c}</h1>:L===2?<h2 id={id} key={i}>{c}</h2>:L===3?<h3 id={id} key={i}>{c}</h3>:L===4?<h4 id={id} key={i}>{c}</h4>:L===5?<h5 id={id} key={i}>{c}</h5>:<h6 id={id} key={i}>{c}</h6>);i++;continue}
    if(/^\s*[-*+]\s+\[[ xX]\]\s+/.test(line)){const items:ReactNode[]=[];while(i<lines.length&&/^\s*[-*+]\s+\[[ xX]\]\s+/.test(lines[i])){const m=lines[i].match(/^\s*[-*+]\s+\[([ xX])\]\s+(.+)$/)!;items.push(<li key={i} className="markdown-task"><input type="checkbox" checked={m[1].toLowerCase()==='x'} readOnly/>{inline(m[2],onMention)}</li>);i++}nodes.push(<ul key={`tasks-${i}`} className="markdown-task-list">{items}</ul>);continue}
    if(/^\s*[-*+]\s+/.test(line)){const items:ReactNode[]=[];while(i<lines.length&&/^\s*[-*+]\s+/.test(lines[i])){items.push(<li key={i}>{inline(lines[i].replace(/^\s*[-*+]\s+/,''),onMention)}</li>);i++}nodes.push(<ul key={`ul-${i}`}>{items}</ul>);continue}
    if(/^\s*\d+[.)]\s+/.test(line)){const items:ReactNode[]=[];while(i<lines.length&&/^\s*\d+[.)]\s+/.test(lines[i])){items.push(<li key={i}>{inline(lines[i].replace(/^\s*\d+[.)]\s+/,''),onMention)}</li>);i++}nodes.push(<ol key={`ol-${i}`}>{items}</ol>);continue}
    if(i+1<lines.length&&line.includes('|')&&/^\s*\|?\s*:?-+/.test(lines[i+1])){const heads=cells(line),aligns=cells(lines[i+1]).map(tableAlign),rows:string[][]=[];i+=2;while(i<lines.length&&lines[i].includes('|')&&lines[i].trim())rows.push(cells(lines[i++]));nodes.push(<div key={`table-${i}`} className="markdown-table-wrap"><table><thead><tr>{heads.map((h,n)=><th key={n} style={{textAlign:aligns[n] as 'left'|'center'|'right'}}>{inline(h,onMention)}</th>)}</tr></thead><tbody>{rows.map((r,ri)=><tr key={ri}>{r.map((c,ci)=><td key={ci} style={{textAlign:aligns[ci] as 'left'|'center'|'right'}}>{inline(c,onMention)}</td>)}</tr>)}</tbody></table></div>);continue}
    const foot=trim.match(/^\[\^([^\]]+)\]:\s*(.+)$/);if(foot){nodes.push(<p key={`foot-${i}`} id={`fn-${foot[1]}`} className="markdown-footnote"><sup>{foot[1]}</sup> {inline(foot[2],onMention)}</p>);i++;continue}
    if(!trim){nodes.push(<div className="markdown-spacer" key={`space-${i}`} aria-hidden="true"/>);i++;continue}
    nodes.push(<p key={`p-${i}`}>{inline(line,onMention)}</p>);i++
  }
  return <div className="markdown-body">{nodes}</div>
}


// Chat/post content is immutable most of the time. Avoid reparsing every message on each composer keystroke.
export const Markdown=memo(MarkdownComponent,(prev,next)=>prev.text===next.text)
