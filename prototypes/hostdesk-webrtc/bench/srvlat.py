# server-side capture->encode latency: xdotool press on :97 -> first encoded frame whose (200,200) pixel is white
import subprocess,time,os,sys,threading
enc=sys.argv[1] if len(sys.argv)>1 else 'nvenc'
cap=['ffmpeg','-loglevel','error']+os.environ.get('PRE','').split()+['-loglevel','error','-f','x11grab','-draw_mouse','0','-framerate',os.environ.get('FR','60'),'-video_size','3398x1220','-i',':97']
e={'nvenc':['-c:v','h264_nvenc','-preset','p1','-tune','ull','-zerolatency','1','-rc','cbr','-b:v','12000k','-g','120','-bf','0','-delay','0','-flush_packets','1','-f','h264','-'],
   'raw':['-vf','crop=2:2:200:200','-f','rawvideo','-pix_fmt','rgb24','-'],
   'x264':['-c:v','libx264','-preset','ultrafast','-tune','zerolatency','-pix_fmt','yuv420p','-b:v','12000k','-g','120','-bf','0','-flush_packets','1','-f','h264','-']}[enc]
p=subprocess.Popen(cap+e,stdout=subprocess.PIPE)
src=p.stdout
if enc!='raw':
  d=subprocess.Popen(['ffmpeg','-loglevel','error','-flags','low_delay','-fflags','nobuffer','-probesize','32','-f','h264','-i','-','-vf','crop=2:2:200:200','-f','rawvideo','-pix_fmt','rgb24','-'],stdin=src,stdout=subprocess.PIPE); src=d.stdout
env=dict(os.environ,DISPLAY=':97'); res=[]
state={'want':None,'t0':0}
def reader():
  while True:
    b=src.read(12)
    if not b: return
    if state['want'] is not None and ((b[0]>200)==state['want']): res.append((time.time()-state['t0'])*1000); state['want']=None
threading.Thread(target=reader,daemon=True).start()
time.sleep(2); subprocess.run(['xdotool','mousemove','200','200'],env=env)
for i in range(10):
  for want,cmd in ((True,'mousedown'),(False,'mouseup')):
    state['t0']=time.time(); state['want']=want; subprocess.run(['xdotool',cmd,'1'],env=env); time.sleep(0.3)
p.kill()
res.sort(); print(enc,'n',len(res),'p50 %.1f'%res[len(res)//2],'min %.1f'%res[0],'max %.1f'%res[-1])
