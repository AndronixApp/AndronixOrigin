#!/data/data/com.termux/files/usr/bin/bash
folder=void-fs
if [ -d "$folder" ]; then
  first=1
  echo "skipping downloading"
fi
tarball="void.tar.xz"



if [ "$first" != 1 ];then
  if [ ! -f $tarball ]; then
    echo "Download Rootfs, this may take a while base on your internet speed."
    case `dpkg --print-architecture` in
    aarch64)
      archurl="arm64" ;;
    arm)
      archurl="armhf" ;;
    amd64)
      archurl="amd64" ;;
    x86_64)
      archurl="amd64" ;;  
    i*86)
      archurl="i386" ;;
    x86)
      archurl="i386" ;;
    *)
      echo "unknown architecture"; exit 1 ;;
    esac
    wget "https://github.com/AndronixApp/AndronixOrigin/blob/master/Rootfs/Void/${archurl}/void_${archurl}.tar.xz?raw=true" -O $tarball
  fi
  mkdir -p "$folder"
  echo "Decompressing Rootfs, please be patient."
  proot --link2symlink tar -xJf ${tarball} -C $folder||:
fi

mkdir -p void-binds
bin=start-void.sh
echo "writing launch script"
cat > $bin <<- EOM
#!/bin/bash
cd \$(dirname \$0)
## unset LD_PRELOAD in case termux-exec is installed
unset LD_PRELOAD
command="proot"
command+=" --link2symlink"
command+=" --kill-on-exit"
command+=" -0"
command+=" -r $folder"
if [ -n "\$(ls -A void-binds)" ]; then
    for f in void-binds/* ;do
      . \$f
    done
fi
command+=" -b /dev"
command+=" -b /proc"
command+=" -b void-fs/root:/dev/shm"
## uncomment the following line to have access to the home directory of termux
#command+=" -b /data/data/com.termux/files/home:/root"
## uncomment the following line to mount /sdcard directly to / 
#command+=" -b /sdcard"
command+=" -w /root"
command+=" /usr/bin/env -i"
command+=" HOME=/root"
command+=" PATH=/usr/local/sbin:/usr/local/bin:/bin:/usr/bin:/sbin:/usr/sbin:/usr/games:/usr/local/games"
command+=" TERM=\$TERM"
command+=" LANG=C.UTF-8"
command+=" /bin/bash --login"
com="\$@"
if [ -z "\$1" ];then
    exec \$command
else
    \$command -c "\$com"
fi
EOM

echo "fixing shebang of $bin"
termux-fix-shebang $bin



echo "Fixing DNS for internet connection"
rm -rf void-fs/etc/resolv.conf
echo "nameserver 8.8.8.8
nameserver 8.8.4.4
nameserver 192.168.1.1
nameserver 127.0.0.1" > void-fs/etc/resolv.conf

echo "making $bin executable"
chmod +x $bin
echo "You can now launch Void with the ./${bin} script"
