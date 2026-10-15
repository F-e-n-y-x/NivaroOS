// node presstest.mjs <url>: press via xdotool on :97 (not CDP), sample page pixel (200,200) + counter hash
import { spawn, execSync } from 'node:child_process';
const url = process.argv[2]; const dbg = 9700 + Math.floor(Math.random()*90);
const chrome = spawn('google-chrome', ['--headless=new','--no-sandbox',`--remote-debugging-port=${dbg}`,`--user-data-dir=/root/nivaroos-wip/hostdesk-alt-proto/run/pt-${dbg}`,'--window-size=3398,1220','--autoplay-policy=no-user-gesture-required','about:blank'],{stdio:'ignore'});
const sleep = ms => new Promise(r => setTimeout(r, ms));
let list; for (let i=0;i<50;i++){try{list=await (await fetch(`http://127.0.0.1:${dbg}/json/list`)).json();break;}catch{await sleep(200);}}
const ws = new WebSocket(list.find(t=>t.type==='page').webSocketDebuggerUrl); let id=0; const pend=new Map();
const send=(m,p={})=>new Promise(res=>{const i=++id;pend.set(i,res);ws.send(JSON.stringify({id:i,method:m,params:p}));});
ws.onmessage=ev=>{const m=JSON.parse(ev.data); if(m.id&&pend.has(m.id)){pend.get(m.id)(m.result);pend.delete(m.id);}};
await new Promise(r=>ws.onopen=r); await send('Page.navigate',{url}); await sleep(6000);
const px=`(()=>{const c=[...document.querySelectorAll('canvas')].sort((a,b)=>b.width*b.height-a.width*a.height)[0];const t=document.createElement('canvas');t.width=2;t.height=1;const g=t.getContext('2d');g.drawImage(c,200,200,1,1,0,0,1,1);g.drawImage(c,610,105,1,1,1,0,1,1);const d=g.getImageData(0,0,2,1).data;return d[0]+','+d[4]+','+d[5]+','+d[6];})()`;
const t0=Date.now(); const env={...process.env,DISPLAY:':97'};
setTimeout(()=>execSync('xdotool mousemove 200 200 mousedown 1',{env}),1000);
setTimeout(()=>execSync('xdotool mouseup 1',{env}),2500);
while(Date.now()-t0<4000){const r=await send('Runtime.evaluate',{expression:px,returnByValue:true}); console.log(Date.now()-t0, r.result.value); await sleep(100);}
chrome.kill(); process.exit(0);
