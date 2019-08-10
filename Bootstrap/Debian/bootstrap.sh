#!/usr/bin/env bash

#Bootstrap the system
rm -rf $2
mkdir $2
if [ "$1" = "i386" ] || [ "$1" = "amd64" ] ; then
  debootstrap --no-check-gpg --arch=$1 --variant=minbase --include=busybox,systemd,libsystemd0,wget,ca-certificates,neofetch,udisks2,gvfs buster $1 http://deb.debian.org/debian
else
  qemu-debootstrap --no-check-gpg --arch=$1 --variant=minbase --include=busybox,systemd,libsystemd0,wget,ca-certificates,neofetch,udisks2,gvfs buster $1 http://deb.debian.org/debian
fi

#Reduce size
DEBIAN_FRONTEND=noninteractive DEBCONF_NONINTERACTIVE_SEEN=true \
 LC_ALL=C LANGUAGE=C LANG=C chroot $2 apt-get clean

#Fix permission on dev machine only for easy packing
chmod 777 -R $2 

#Setup DNS
echo "127.0.0.1 localhost" > $2/etc/hosts
echo "nameserver 8.8.8.8" > $2/etc/resolv.conf
echo "nameserver 8.8.4.4" >> $2/etc/resolv.conf

#sources.list setup
rm $2/etc/apt/sources.list
echo "deb http://deb.debian.org/debian buster main contrib non-free" >> $2/etc/apt/sources.list
echo "deb-src http://deb.debian.org/debian buster main contrib non-free" >> $2/etc/apt/sources.list

#tar the rootfs
cd $2
rm -rf ../debian-rootfs-$1.tar.xz
rm -rf dev/*
XZ_OPT=-9 tar -cJvf ../debian-rootfs-$1.tar.xz ./*
