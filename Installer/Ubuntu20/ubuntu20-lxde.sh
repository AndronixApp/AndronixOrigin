#!/data/data/com.termux/files/usr/bin/bash
pkg install wget -y 
folder=ubuntu20-fs
dlink="https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/APT"
if [ -d "$folder" ]; then
	first=1
	echo "skipping downloading"
fi
tarball="ubuntu20-rootfs.tar.gz"

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
		*)
			echo "unknown architecture"; exit 1 ;;
		esac
		wget "https://github.com/AndronixApp/AndronixOrigin/raw/master/Rootfs/Ubuntu20/focal-${archurl}.tar.gz" -O $tarball

fi
	cur=`pwd`
	mkdir -p "$folder"
	cd "$folder"
	echo "Decompressing Rootfs, please be patient."
	proot --link2symlink tar -xf ${cur}/${tarball} --exclude=dev||:
	cd "$cur"
fi
mkdir -p ubuntu20-binds
bin=start-ubuntu20.sh
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
if [ -n "\$(ls -A ubuntu20-binds)" ]; then
    for f in ubuntu20-binds/* ;do
      . \$f
    done
fi
command+=" -b /dev"
command+=" -b /proc"
command+=" -b ubuntu20-fs/root:/dev/shm"
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

mkdir -p ubuntu20-fs/var/tmp
rm -rf ubuntu20-fs/usr/local/bin/*
echo "127.0.0.1 localhost localhost" > $folder/etc/hosts
wget -q https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Rootfs/Ubuntu19/.profile -O ubuntu20-fs/root/.profile.1 > /dev/null
cat $folder/root/.profile.1 >> $folder/root/.profile && rm -rf $folder/root/.profile.1
wget -q https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Rootfs/Ubuntu19/vnc -P ubuntu20-fs/usr/local/bin > /dev/null
wget -q https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Rootfs/Ubuntu19/vncpasswd -P ubuntu20-fs/usr/local/bin > /dev/null
wget -q https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Rootfs/Ubuntu19/vncserver-stop -P ubuntu20-fs/usr/local/bin > /dev/null
wget -q https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Rootfs/Ubuntu19/vncserver-start -P ubuntu20-fs/usr/local/bin > /dev/null

chmod +x ubuntu20-fs/root/.profile
chmod +x ubuntu20-fs/usr/local/bin/vnc
chmod +x ubuntu20-fs/usr/local/bin/vncpasswd
chmod +x ubuntu20-fs/usr/local/bin/vncserver-start
chmod +x ubuntu20-fs/usr/local/bin/vncserver-stop

echo "fixing shebang of $bin"
termux-fix-shebang $bin
echo "making $bin executable"
chmod +x $bin
echo "removing image for some space"
rm $tarball

#DE installation addition

wget --tries=20 $dlink/LXDE/lxde19.sh -O $folder/root/lxde19.sh
clear
echo "Setting up the installation of LXDE VNC"

rm -rf $folder/root/.bash_profile
echo "APT::Acquire::Retries \"3\";" > $folder/etc/apt/apt.conf.d/80-retries #Setting APT retry count
touch $folder/root/.hushlogin
echo "#!/bin/bash
rm -rf /etc/resolv.conf
echo 'nameserver 8.8.8.8
nameserver 1.1.1.1' > /etc/resolv.conf
mkdir -p ~/.vnc
apt update -y && apt install sudo wget -y > /dev/null
clear
if [ ! -f /root/lxde19.sh ]; then
    wget --tries=20 $dlink/LXDE/lxde19.sh -O /root/lxde19.sh
    bash ~/lxde19.sh
else
    bash ~/lxde19.sh
fi
clear
if [ ! -f /usr/local/bin/vncserver-start ]; then
    wget --tries=20  $dlink/XFCE4/vncserver-start -O /usr/local/bin/vncserver-start 
    wget --tries=20 $dlink/XFCE4/vncserver-stop -O /usr/local/bin/vncserver-stop
    chmod +x /usr/local/bin/vncserver-stop
    chmod +x /usr/local/bin/vncserver-start
fi
if [ ! -f /usr/bin/vncserver ]; then
    apt install tigervnc-standalone-server -y
fi
rm -rf /root/lxde19.sh
rm -rf ~/.bash_profile" > $folder/root/.bash_profile 
clear
bash $bin
