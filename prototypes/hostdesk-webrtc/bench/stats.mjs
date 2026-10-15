// node stats.mjs <url>: open page 12 s, dump WebRTC inbound video stats
import { spawn } from 'node:child_process';
const url=process.argv[2]; const dbg=9600+Math.floor(Math.random()*90);
const chrome=spawn('google-chrome',['--headless=new','--no-sandbox',`--remote-debugging-port=${dbg}`,`--user-data-dir=/root/nivaroos-wip/hostdesk-alt-proto/run/st-${dbg}`,'--window-size=3398,1220','--autoplay-policy=no-user-gesture-required',...(process.env.FLAGS||'').split(' ').filter(Boolean),'about:blank'],{stdio:'ignore'});
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
let list; for(let i=0;i<50;i++){try{list=await (await fetch(`http://127.0.0.1:${dbg}/json/list`)).json();break;}catch{await sleep(200);}}
const ws=new WebSocket(list.find(t=>t.type==='page').webSocketDebuggerUrl); let id=0; const pend=new Map();
const send=(m,p={})=>new Promise(res=>{const i=++id;pend.set(i,res);ws.send(JSON.stringify({id:i,method:m,params:p}));});
ws.onmessage=ev=>{const m=JSON.parse(ev.data); if(m.id&&pend.has(m.id)){pend.get(m.id)(m.result);pend.delete(m.id);}};
await new Promise(r=>ws.onopen=r); await send('Page.navigate',{url}); await sleep(12000);
const r=await send('Runtime.evaluate',{expression:`(async()=>{let o;(await pc.getStats()).forEach(r=>{if(r.type==='inbound-rtp'&&r.kind==='video')o={fps:r.framesPerSecond,decoded:r.framesDecoded,dropped:r.framesDropped,decoder:r.decoderImplementation,decode_ms:+(r.totalDecodeTime/r.framesDecoded*1000).toFixed(1),jitterbuf_ms:+(r.jitterBufferDelay/r.jitterBufferEmittedCount*1000).toFixed(1),processing_ms:+(r.totalProcessingDelay/r.framesDecoded*1000).toFixed(1),nack:r.nackCount,pli:r.pliCount,lost:r.packetsLost,assembly_ms:+(r.totalAssemblyTime/r.framesAssembledFromMultiplePackets*1000).toFixed(1)}});return o;})()`,returnByValue:true,awaitPromise:true});
console.log(JSON.stringify(r.result.value)); chrome.kill(); process.exit(0);
