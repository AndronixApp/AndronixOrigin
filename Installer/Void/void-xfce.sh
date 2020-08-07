#!/data/data/com.termux/files/usr/bin/bash
pkg install wget -y 
folder=void-fs
dlink="https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/XBPS"
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
rm $tarball

#DE installation addition

wget --tries=20 $dlink/XFCE4/xfce4_de.sh -O $folder/root/xfce4_de.sh > /dev/null
clear
echo "Setting up the installation of XFCE VNC"

echo "#!/bin/bash
xbps-install -Su -y && xbps-install -S xfce4 tigervnc wget sudo -y 
if [ ! -f /root/xfce4_de.sh ]; then
    wget --tries=20 $dlink/XFCE4/xfce4_de.sh -O /root/xfce4_de.sh > /dev/null
    bash ~/xfce4_de.sh 
else
    bash ~/xfce4_de.sh
fi
clear
if [ ! -f /usr/local/bin/vncserver-start ]; then
    wget --tries=20  $dlink/XFCE4/vncserver-start -O /usr/local/bin/vncserver-start > /dev/null
    wget --tries=20 $dlink/XFCE4/vncserver-stop -O /usr/local/bin/vncserver-stop > /dev/null
    chmod +x /usr/local/bin/vncserver-stop
    chmod +x /usr/local/bin/vncserver-start
fi
if [ ! -f /usr/bin/vncserver ]; then
    xbps-install -S xfce4 tigervnc wget -y  > /dev/null
fi
rm -rf ~/.bash_profile" > $folder/root/.bash_profile 

bash $bin
