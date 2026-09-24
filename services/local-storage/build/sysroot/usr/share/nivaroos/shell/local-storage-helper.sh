#!/bin/bash

UDEVILUmount(){
  $sudo_cmd udevil umount -f $1
}

#获磁盘的插入路径
#param 路径 /dev/sda
GetPlugInDisk() {
  fdisk -l | grep 'Disk' | grep 'sd' | awk -F , '{print substr($1,11,3)}'
}

#格式化fat32磁盘
#param 需要格式化的目录 /dev/sda1
#param 格式
FormatDisk() {
  if [ "$2" == "fat32" ]; then
    mkfs.vfat -F 32 $1
  elif [ "$2" == "ntfs" ]; then
    mkfs.ntfs $1
  elif [ "$2" == "ext4" ]; then
    mkfs.ext4 -m 1 -F $1
  elif [ "$2" == "exfat" ]; then
    mkfs.exfat $1
  else
    mkfs.ext4 -m 1 -F $1
  fi
}

#移除挂载点,删除已挂在的文件夹
UMountPointAndRemoveDir() {
  set -e
  DEVICE=$1
  MOUNT_POINT=$(mount | grep ${DEVICE} | awk '{ print $3 }')
  if [[ -z ${MOUNT_POINT} ]]; then
    echo "Warning: ${DEVICE} is not mounted"
  else
    umount -lf ${DEVICE}
    /bin/rmdir "${MOUNT_POINT}"
  fi
}

#添加分区只有一个分区
#param 路径   /dev/sdb
#param 要挂载的目录
AddPartition() {
  set -e

  parted -s $1 mklabel gpt

  parted -s $1 mkpart primary ext4 0 100%
  P=`lsblk -r $1 | sort | grep part | head -n 1 | awk '{print $1}'`
  mkfs.ext4 -m 1 -F /dev/${P}

  partprobe $1
}

#磁盘类型
GetDiskType() {
  fdisk $1 -l | grep Disklabel | awk -F: '{print $2}'
}

# $1=sda1
# $2=volume{1}

# NTFS: the kernel ntfs3 driver is several times faster than FUSE ntfs-3g
# (same switch the internal drives got). ntfs3 refuses a volume marked
# dirty (unplugged without ejecting, Windows Fast Startup/hibernation), so
# those still mount through ntfs-3g, which handles them.
mount_ntfs() {
  if grep -qw ntfs3 /proc/filesystems 2>/dev/null || modprobe ntfs3 2>/dev/null; then
    if mount -t ntfs3 -o rw,noatime,iocharset=utf8,prealloc,umask=000 "$1" "$2" 2>/dev/null; then
      return 0
    fi
    echo "ntfs3 could not mount $1 (dirty or hibernated volume?) - using ntfs-3g" | logger -t usb-mount.sh 2>/dev/null
  fi
  ntfs-3g "$1" "$2"
}

# Usage (from local-storage, args passed separately - never via bash -c):
#   bash local-storage-helper.sh do_mount <block device> <mount point>
# The Go side validates both (lsblk-listed device, /mnt|/media|/DATA/<name>);
# they are still treated strictly as data here: always quoted, never eval'd.
do_mount() {
  set -e

  DEVICE="$1"
  MOUNT_POINT="$2"

  if [ -z "${DEVICE}" ] || [ -z "${MOUNT_POINT}" ]; then
    echo "usage: do_mount <device> <mount point>"
    exit 1
  fi
  if [ ! -b "${DEVICE}" ]; then
    echo "${DEVICE} is not a block device"
    exit 1
  fi
  case "${MOUNT_POINT}" in
  /mnt/*|/media/*|/DATA/*) ;;
  *)
    echo "refusing mount point outside /mnt, /media, /DATA: ${MOUNT_POINT}"
    exit 1
    ;;
  esac

  # See if this drive is already mounted, and if so where
  CURRENT=$(lsblk -o mountpoint -nr -- "${DEVICE}" | head -n 1)
  if [ -n "${CURRENT}" ]; then
    echo "${DEVICE} is already mounted at ${CURRENT}"
    exit 1
  fi

  # Filesystem type straight from blkid - no eval of device-controlled text
  # (the old code eval'd `blkid -o udev`, which carries the volume label).
  ID_FS_TYPE=$(blkid -o value -s TYPE -- "${DEVICE}") || true
  if [ -z "${ID_FS_TYPE}" ]; then
    echo "${DEVICE} does not have a filesystem or it might be corrupted. Please consider format it."
    exit 1
  fi

  # Mount point already in use by something else: make a unique one next to
  # it (same parent, device basename appended - stays inside the root).
  if awk -v mp="${MOUNT_POINT}" '$2 == mp { found = 1 } END { exit !found }' /proc/self/mounts; then
    MOUNT_POINT="${MOUNT_POINT}-$(basename -- "${DEVICE}")"
  fi

  echo "Mount point: ${MOUNT_POINT}"

  mkdir -p -- "${MOUNT_POINT}"

  case "${ID_FS_TYPE}" in
  vfat)
    mount -t vfat -o rw,relatime,users,gid=100,umask=000,shortname=mixed,utf8=1,flush -- "${DEVICE}" "${MOUNT_POINT}"
    ;;
  ext[2-4])
    mount -o noatime -- "${DEVICE}" "${MOUNT_POINT}"
    ;;
  exfat)
    mount -t exfat -- "${DEVICE}" "${MOUNT_POINT}"
    ;;
  ntfs)
    mount_ntfs "${DEVICE}" "${MOUNT_POINT}"
    ;;
  iso9660)
    mount -t iso9660 -- "${DEVICE}" "${MOUNT_POINT}"
    ;;
  *)
    echo "Unsupported filesystem type: ${ID_FS_TYPE}"
    /bin/rmdir -- "${MOUNT_POINT}"
    exit 1
    ;;
  esac
}

# $1=sda1
do_umount() {
  DEVBASE=$1
  DEVICE="${DEVBASE}"
  MOUNT_POINT=$(mount | grep ${DEVICE} | awk '{ print $3 }')

  if [[ -z ${MOUNT_POINT} ]]; then
    echo "Warning: ${DEVICE} is not mounted"
  else
    /bin/kill -9 $(lsof ${MOUNT_POINT})
    umount -l ${DEVICE}
    echo "Unmounted ${DEVICE} from ${MOUNT_POINT}"
    if [ "`ls -A ${MOUNT_POINT}`" = "" ]; then
      /bin/rm -fr "${MOUNT_POINT}"
    fi
    
    sed -i.bak "\@${MOUNT_POINT}@d" /var/log/usb-mount.track
  fi

}

USB_Start_Auto() {
  ((EUID)) && sudo_cmd="sudo"
  $sudo_cmd systemctl enable devmon@devmon
  $sudo_cmd systemctl start devmon@devmon
}

USB_Stop_Auto() {
  ((EUID)) && sudo_cmd="sudo"
  $sudo_cmd systemctl stop devmon@devmon
  $sudo_cmd systemctl disable devmon@devmon
  $sudo_cmd udevil clean
}

GetDeviceTree(){  
  cat /proc/device-tree/model
}

# Direct invocation: `bash local-storage-helper.sh <function> [args...]`.
# Only the functions local-storage calls are dispatchable; arguments are
# passed through as separate, quoted words. Sourcing the file (the old
# `source ...; func` style) still just defines the functions.
if [ "${BASH_SOURCE[0]}" = "$0" ] && [ $# -gt 0 ]; then
  case "$1" in
  do_mount|USB_Start_Auto|USB_Stop_Auto)
    fn="$1"
    shift
    "$fn" "$@"
    ;;
  *)
    echo "unknown command: $1" >&2
    exit 2
    ;;
  esac
fi
