import socket,struct,time,sys
s=socket.socket(socket.AF_UNIX);s.connect(sys.argv[1])
s.recv(12);s.send(b'RFB 003.008\n');n=s.recv(1)[0];s.recv(n);s.send(b'\x01');s.recv(4);s.send(b'\x01')
h=s.recv(24);nl=struct.unpack('>I',h[20:24])[0];s.recv(nl)
t=time.time();print('ready',t,flush=True)
for i in range(6):
  for m in (1,0):
    print('send',m,time.time(),flush=True);s.send(struct.pack('>BBHH',5,m,200,200));time.sleep(0.3)
