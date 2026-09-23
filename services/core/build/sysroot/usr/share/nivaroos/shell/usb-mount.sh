#!/bin/bash

# copy to /casaOS/util/shell path
# chmod 755

log="logger -t usb-mount.sh -s "

ACTION=$1

DEVBASE=$2

DEVICE="/dev/${DEVBASE}"

# See if this drive is already mounted, and if so where.
#
# lsblk needs the device's /dev node and udev database entry, which are
# both already gone by the time a udev "remove" event runs this script -
# so on removal that lookup silently comes back empty, do_umount() below
# just logs a warning and does nothing, and the mount-point directory is
# never cleaned up (this is why orphaned /DATA/USB_Storage_* folders pile
# up after real unplugs). /proc/mounts still has the entry at that point
# since nothing has unmounted it yet, so that's what removal must use.
if [ "${ACTION}" = "remove" ]; then
  MOUNT_POINT=$(awk -v dev="${DEVICE}" '$1 == dev {print $2}' /proc/mounts | head -n1)
  # /proc/mounts escapes spaces in paths as \040 - undo that so the later
  # quoted uses below see the real path.
  MOUNT_POINT="${MOUNT_POINT//\\040/ }"
else
  MOUNT_POINT=$(lsblk -l -p -o name,mountpoint | grep "${DEVICE}" | awk '{print $2}')
fi


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

do_mount() {

  if [ -n "${MOUNT_POINT}" ]; then
    ${log} "Warning: ${DEVICE} is already mounted at ${MOUNT_POINT}"
    exit 1
  fi

  # Get info for this drive: $ID_FS_LABEL and $ID_FS_TYPE
  eval $(blkid -o udev ${DEVICE} | grep -i -e "ID_FS_LABEL" -e "ID_FS_TYPE")

  #ID_FS_LABEL=新加卷
  #ID_FS_LABEL_ENC=新加卷
  #ID_FS_TYPE=ntfs

  # Folder is "<label> <device base>" (e.g. "Backup sdb1") so the drive's
  # own name is what shows up in /DATA, not a generic USB_Storage_sdb1 -
  # falls back to just the device base for an unlabeled drive. Sanitized:
  # a filesystem label can contain '/' or other characters that aren't
  # safe as a single path component, and DEVBASE already guarantees this
  # is unique on its own, so no separate collision check is needed.
  SAFE_LABEL=$(echo "${ID_FS_LABEL}" | tr -s '/\\' '_')
  if [ -n "${SAFE_LABEL}" ]; then
    DEV_LABEL="${SAFE_LABEL} ${DEVBASE}"
  else
    DEV_LABEL="${DEVBASE}"
  fi

  MOUNT_POINT="/DATA/${DEV_LABEL}"

  ${log} "Mount point: ${MOUNT_POINT}"

  mkdir -p "${MOUNT_POINT}"


  # MOUNT_POINT="/DATA/USB_Storage1"
  # arr=("/DATA/USB_Storage1" "/DATA/USB_Storage2" "/DATA/USB_Storage3" "/DATA/USB_Storage4" "/DATA/USB_Storage5" "/DATA/USB_Storage6" "/DATA/USB_Storage7" "/DATA/USB_Storage8" "/DATA/USB_Storage9" "/DATA/USB_Storage10" "/DATA/USB_Storage11" "/DATA/USB_Storage12")
  # for folder in ${arr[@]}; do
  #   #如果文件夹不存在，创建文件夹
  #   if [ ! -d "$folder" ]; then
  #     mkdir -p ${folder}
  #     MOUNT_POINT=$folder
  #     break
  #   fi
  # done

  # ${log} "Mount point: ${MOUNT_POINT}"

  

  #  # Global mount options
  #  OPTS="rw,relatime"
  #
  #  # File system type specific mount options
  #  if [[ ${ID_FS_TYPE} == "vfat" ]]; then
  #    OPTS+=",users,gid=100,umask=000,shortname=mixed,utf8=1,flush"
  #  fi

  #  if ! mount -o ${OPTS} ${DEVICE} ${MOUNT_POINT}; then
  #    ${log} "Error mounting ${DEVICE} (status = $?)"
  #    rmdir "${MOUNT_POINT}"
  #    exit 1
  #  else
  #    # Track the mounted drives
  #    echo "${MOUNT_POINT}:${DEVBASE}" | cat >>"/var/log/usb-mount.track"
  #  fi
  #
  #  ${log} "Mounted ${DEVICE} at ${MOUNT_POINT}"

  # ${MOUNT_POINT} is quoted everywhere below - it can now contain a space
  # (the filesystem label), and an unquoted expansion here would word-split
  # it into two arguments and break every one of these commands.
  #
  # None of these checked their own exit code - if the mount/ntfs-3g command
  # itself failed (most commonly: another automounter, e.g. devmon/udisks2,
  # won the race for this same device and already mounted it somewhere else
  # first, like /media/$USER/<label> - device busy), this still fell through
  # to a "success" exit, leaving the mkdir -p'd directory above sitting in
  # /DATA empty and permanently unmounted, with nothing pointing at why.
  case ${ID_FS_TYPE} in
  vfat)
    mount -t vfat -o rw,relatime,users,gid=100,umask=000,shortname=mixed,utf8=1,flush ${DEVICE} "${MOUNT_POINT}"
    mount_status=$?
    ;;
  ext[2-4])
    mount -o noatime ${DEVICE} "${MOUNT_POINT}"
    mount_status=$?
    ;;
  exfat)
    mount -t exfat ${DEVICE} "${MOUNT_POINT}"
    mount_status=$?
    ;;
  ntfs)
    mount_ntfs "${DEVICE}" "${MOUNT_POINT}"
    mount_status=$?
    ;;
  iso9660)
    mount -t iso9660 ${DEVICE} "${MOUNT_POINT}"
    mount_status=$?
    ;;
  *)
    ${log} "Unsupported filesystem type for ${DEVICE}: ${ID_FS_TYPE}"
    /bin/rmdir "${MOUNT_POINT}" 2>/dev/null
    exit 0
    ;;
  esac

  if [ "${mount_status}" -ne 0 ]; then
    ${log} "Failed to mount ${DEVICE} at ${MOUNT_POINT} (exit ${mount_status}) - probably already mounted elsewhere by another automounter; removing the empty directory instead of leaving it behind"
    /bin/rmdir "${MOUNT_POINT}" 2>/dev/null
    exit 1
  fi
}

do_umount() {

  if [[ -z ${MOUNT_POINT} ]]; then
    ${log} "Warning: ${DEVICE} is not mounted"
  else
    umount -l "${DEVICE}"
    ${log} "Unmounted ${DEVICE} from ${MOUNT_POINT}"
    if [ -z "$(ls -A "${MOUNT_POINT}" 2>/dev/null)" ]; then
      /bin/rm -fr "${MOUNT_POINT}"
    fi
    sed -i.bak "\@${MOUNT_POINT}@d" /var/log/usb-mount.track 2>/dev/null
  fi

  # Safety net: sweep for any other USB mount-point folders left behind by
  # an earlier removal that, for whatever reason, didn't get cleaned up
  # above (a crash or power loss mid-unmount, a manually yanked drive from
  # before this fix existed, etc). AutoRemoveUnuseDir already existed in
  # helper.sh for exactly this - it just was never actually called from
  # anywhere.
  # shellcheck source=helper.sh
  source /usr/share/nivaroos/shell/helper.sh 2>/dev/null && AutoRemoveUnuseDir
}

case "${ACTION}" in
add)
  do_mount
  ;;
remove)
  do_umount
  ;;
*)
  exit 1
  ;;
esac
