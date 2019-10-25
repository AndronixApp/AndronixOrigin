#!/data/data/com.termux/files/usr/bin/bash
folder=alpine-fs
url=https://github.com/Techriz/AndronixOrigin/blob/master/Rootfs/${archurl}/alpine-minirootfs-3.10.3-${archurl}.tar.gz?raw=true
if [ -d "$folder" ]; then
	first=1
	echo "skipping downloading"
fi
tarball="alpine-rootfs.tar.gz"
if [ "$first" != 1 ];then
	if [ ! -f $tarball ]; then
		echo "Download Rootfs, this may take a while base on your internet speed."
		case `dpkg --print-architecture` in
		aarch64)
			archurl="aarch64" ;;
		arm)
			archurl="armhf" ;;
		amd64)
			archurl="x86_64" ;;
		x86_64)
			archurl="x86_64" ;;	
		i*86)
			archurl="x86" ;;
		x86)
			archurl="x86" ;;
		*)
			echo "unknown architecture"; exit 1 ;;
		esac
		wget $url -O $tarball
	fi
	cur=`pwd`
	mkdir -p "$folder"
	cd "$folder"
	echo "Decompressing Rootfs, please be patient."
	proot --link2symlink tar -xf ${cur}/${tarball} --exclude='dev' 2> /dev/null||:
	cd "$cur"
fi
mkdir -p alpine-binds
bin=start-alpine.sh
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
if [ -n "\$(ls -A alpine-binds)" ]; then
    for f in alpine-binds/* ;do
      . \$f
    done
fi
command+=" -b /dev"
command+=" -b /proc"
command+=" -b alpine-fs/root:/dev/shm"
## uncomment the following line to have access to the home directory of termux
#command+=" -b /data/data/com.termux/files/home:/root"
## uncomment the following line to mount /sdcard directly to / 
#command+=" -b /sdcard"
command+=" -w /root"
command+=" /usr/bin/env -i"
command+=" HOME=/root"
command+=" PATH=PATH=/bin:/usr/bin:/sbin:/usr/sbin"
command+=" TERM=\$TERM"
command+=" LANG=C.UTF-8"
command+=" /bin/sh --login"
com="\$@"
if [ -z "\$1" ];then
    exec \$command
else
    \$command -c "\$com"
fi
EOM

echo "fixing shebang of $bin"
termux-fix-shebang $bin
echo "making $bin executable"
chmod +x $bin
echo "removing image for some space"
rm $tarball
echo "Preparing additional component for the first time, please wait..."
rm $folder/etc/resolv.conf
echo nameserver 1.1.1.1 > $folder/etc/resolv.conf
echo "You can now launch Alpine with the ./${bin} script"
