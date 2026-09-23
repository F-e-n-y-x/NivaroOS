NivaroOS Guest Tools
====================

Drivers, the QEMU guest agent and the NivaroOS shared folder for your VM.
The shared folder is /DATA/VMs/share on the NivaroOS server.

Windows 10 / 11
  Double-click the "NivaroOS Guest Tools" CD drive in File Explorer
  (or NivaroOS-Setup.exe on it) and allow the administrator prompt.
  The shared folder then appears as a drive under "This PC".
  Installing Windows and the disk isn't found? Click "Load driver" in the
  installer and pick this disc's viostor\w11\amd64 (or w10) folder.

Linux
  Run in a terminal:
    sudo sh /media/$USER/NIVAROOS_TOOLS/linux/nivaroos-guest-setup.sh
  (or wherever this disc is mounted). The shared folder is mounted at
  /mnt/nivaroos-share, now and after every reboot, with a shortcut at
  ~/NivaroOS-Share.

Once the guest agent is installed, NivaroOS can run these setups for you
from the VM console (Share > Set up automatically).

Contents: virtio-win drivers and guest agent (Fedora virtio-win project),
WinFsp (github.com/winfsp/winfsp), NivaroOS setup scripts.
