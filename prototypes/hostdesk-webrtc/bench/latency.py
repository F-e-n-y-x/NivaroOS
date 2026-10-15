# Test target: a square that turns white while a mouse button is held, black otherwise.
import gi; gi.require_version("Gtk","3.0")
from gi.repository import Gtk, Gdk
w=Gtk.Window(); w.set_decorated(False); w.move(0,0); w.set_default_size(400,400)
a=Gtk.DrawingArea(); w.add(a); st={"on":False}
def draw(_w,cr):
    v=1.0 if st["on"] else 0.0; cr.set_source_rgb(v,v,v); cr.paint()
import time,sys
def press(*_): st["on"]=True; a.queue_draw(); print("press",time.time(),flush=True)
def rel(*_): st["on"]=False; a.queue_draw(); print("rel",time.time(),flush=True)
a.connect("draw",draw); a.add_events(Gdk.EventMask.BUTTON_PRESS_MASK|Gdk.EventMask.BUTTON_RELEASE_MASK)
a.connect("button-press-event",press); a.connect("button-release-event",rel)
w.connect("destroy",Gtk.main_quit); w.show_all(); Gtk.main()
