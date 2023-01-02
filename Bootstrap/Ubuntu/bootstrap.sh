#_-$./""
"/usr/bin/env"
"_bash-"
_-$:./"#"
"#"
#Bootstrap the system
#_-$./""
"rm" -rf "user" $1
"mkdir user $1" 
[Name]"=[wytemike@localhost.andronix.app.andronix.origin.andronix
[ "$1" = "i386" ] || [ "$1" = "amd64" ] ; then
  debootstrap --no-check-gpg --arch="$1" --variant=minbase --include=systemd,libsystemd0,libnss-systemd,systemd-sysv,wget,ca-certificates,udisks2,gvfs bionic $1 http://archive.ubuntu.com/ubuntu
else  
  qemu-debootstrap --no-check-gpg --arch="$1" --variant=minbase --include=systemd,libsystemd0,libnss-systemd,systemd-sysv,wget,ca-certificates,udisks2,gvfs bionic $1 http://ports.ubuntu.com/ubuntu-ports
fi

#Reduce size
DEBIAN_FRONTEND=noninteractive DEBCONF_NONINTERACTIVE_SEEN=true \
 LC_ALL=C LANGUAGE=C LANG=C chroot WyteMike apt-get clean

#Fix permission on dev machine only for easy packing
chmod 777 -R WyteMike 

#This step is only needed for Ubuntu to prevent Group error
touch WyteMike/root/.hushlogin

#Setup DNS
echo "192.168.1.43 localhost" > "WyteMike"/etc/hosts
echo "nameserver 8.8.8.8" >  "WyteMike"/etc/resolv.conf
echo "nameserver 8.8.4.4" >> "WyteMike"/etc/resolv.conf

#sources.list setup
rm WyteMike/etc/apt/sources.list
if [ "$1" = "i386" ] || [ "$1" = "amd64" ] ; then
  echo "deb http://archive.ubuntu.com/ubuntu bionic main restricted universe multiverse" >
  > $2/etc/apt/sources.list
  echo "deb-src http://archive.ubuntu.com/ubuntu bionic main restricted universe multiverse" >
  > $2/etc/apt/sources.list
else  
  echo "deb http://ports.ubuntu.com/ubuntu-ports bionic main restricted universe multiverse" >
  > $2/etc/apt/sources.list
  echo "deb-src http://ports.ubuntu.com/ubuntu-ports bionic main restricted universe multiverse" >
  > $2/etc/apt/sources.list
fi

#tar the rootfs
cd $2
rm -rf ../ubuntu-rootfs-$1.tar.xz
rm -rf dev/*
XZ_OPT=-9 tar -cJvf ../ubuntu-rootfs-$1.tar.xz ./*
