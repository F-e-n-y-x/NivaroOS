// node shot.mjs <url> <out.png> [waitMs]
import { spawn } from 'node:child_process'; import { writeFileSync } from 'node:fs';
const [url,out,w='8000']=process.argv.slice(2); const dbg=9500+Math.floor(Math.random()*90);
const chrome=spawn('google-chrome',['--headless=new','--no-sandbox','--ignore-certificate-errors',`--remote-debugging-port=${dbg}`,`--user-data-dir=/root/nivaroos-wip/hostdesk-alt-proto/run/sh-${dbg}`,'--window-size=3398,1220','about:blank'],{stdio:'ignore'});
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
let list; for(let i=0;i<50;i++){try{list=await (await fetch(`http://127.0.0.1:${dbg}/json/list`)).json();break;}catch{await sleep(200);}}
const ws=new WebSocket(list.find(t=>t.type==='page').webSocketDebuggerUrl); let id=0; const pend=new Map();
const send=(m,p={})=>new Promise(res=>{const i=++id;pend.set(i,res);ws.send(JSON.stringify({id:i,method:m,params:p}));});
ws.onmessage=ev=>{const m=JSON.parse(ev.data); if(m.id&&pend.has(m.id)){pend.get(m.id)(m.result);pend.delete(m.id);}};
await new Promise(r=>ws.onopen=r); await send('Emulation.setDeviceMetricsOverride',{width:3398,height:1220,deviceScaleFactor:1,mobile:false}); await send('Page.navigate',{url}); await sleep(+w);
const r=await send('Runtime.evaluate',{expression:`JSON.stringify([...document.querySelectorAll('canvas')].map(c=>[c.id,c.width,c.height,c.style.display]))+' '+document.title+' '+(window.isSecureContext)`,returnByValue:true}); console.log(r.result.value);
const s=await send('Page.captureScreenshot',{format:'png',clip:{x:0,y:0,width:3398,height:1220,scale:0.33}}); writeFileSync(out,Buffer.from(s.data,'base64')); chrome.kill(); process.exit(0);
