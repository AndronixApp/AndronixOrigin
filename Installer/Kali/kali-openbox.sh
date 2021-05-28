#!/data/data/com.termux/files/usr/bin/bash
pkg install wget -y 
folder=kali-fs
dlink="https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/APT"
dlink2="https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/WM/APT"
if [ -d "$folder" ]; then
	first=1
	echo "skipping downloading"
fi
tarball="kali-rootfs.tar.xz"
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

		if [ $archurl == "arm64" ]; then
			wget "https://github.com/AndronixApp/AndronixOrigin/releases/download/kali-arm64-tarball/kali-rootfs-arm64.tar.xz" -O $tarball
		else
			wget "https://github.com/Techriz/AndronixOrigin/blob/master/Rootfs/Kali/${archurl}/kali-rootfs-${archurl}.tar.xz?raw=true" -O $tarball
		fi
	fi
	cur=`pwd`
	mkdir -p "$folder"
	cd "$folder"
	echo "Decompressing Rootfs, please be patient."
	proot --link2symlink tar -xJf ${cur}/${tarball}||:
	cd "$cur"
fi
mkdir -p kali-binds
bin=start-kali.sh
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
if [ -n "\$(ls -A kali-binds)" ]; then
    for f in kali-binds/* ;do
      . \$f
    done
fi
command+=" -b /dev"
command+=" -b /proc"
command+=" -b kali-fs/root:/dev/shm"
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

cat > $folder/root/.bash_logout <<- EOM
#!/bin/bash
vncserver-stop
pkill dbus*
pkill ssh*
EOM

echo -e "\e[31m Patching mirrorlist temporarily until further source update. Don't worry about GPG errors\e[0m"
echo "deb [trusted=yes]  http://http.kali.org/kali kali-rolling main contrib non-free" > $folder/etc/apt/sources.list

echo "fixing shebang of $bin"
termux-fix-shebang $bin
echo "making $bin executable"
chmod +x $bin
echo "removing image for some space"
rm $tarball


#DE installation addition

wget --tries=20 $dlink2/openbox.sh -O $folder/openbox.sh
clear
echo "Setting up the installation of Openbox VNC"

echo "APT::Acquire::Retries \"3\";" > $folder/etc/apt/apt.conf.d/80-retries #Setting APT retry count
echo "#!/bin/bash
apt update -y && apt install wget sudo -y
clear
if [ ! -f /root/openbox.sh ]; then
    wget --tries=20 $dlink2/openbox.sh -O /root/openbox.sh
    bash ~/openbox.sh
else
    bash ~/openbox.sh
fi
clear

if [ ! -f /usr/bin/vncserver ]; then
    apt install tigervnc-standalone-server -y
fi
clear
echo ' Welcome to Andronix Kali | Openbox '
rm -rf ~/.bash_profile" > $folder/root/.bash_profile 

bash $bin
