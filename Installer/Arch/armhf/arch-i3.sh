#!/data/data/com.termux/files/usr/bin/bash
pkg install wget -y 
folder=arch-fs
dlink="https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Pacman"
dlink2="https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/WM/Pacman"

if [ -d "$folder" ]; then
	first=1
	echo "skipping downloading"
fi
tarball="arch-rootfs.tar.gz"
if [ "$first" != 1 ];then
	if [ ! -f $tarball ]; then
		echo "Download Rootfs, this may take a while base on your internet speed."
		case `dpkg --print-architecture` in
		aarch64)
			archurl="aarch64" ;;
		arm)
			archurl="armv7" ;;
		*)
			echo "unknown architecture"; exit 1 ;;
		esac
		wget "http://os.archlinuxarm.org/os/ArchLinuxARM-${archurl}-latest.tar.gz" -O $tarball
	fi
	cur=`pwd`
	mkdir -p "$folder"
	cd "$folder"
	echo "Decompressing Rootfs, please be patient."
	proot --link2symlink tar -xf ${cur}/${tarball}||:
	cd "$cur"
fi
mkdir -p arch-binds
bin=start-arch.sh
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
if [ -n "\$(ls -A arch-binds)" ]; then
    for f in arch-binds/* ;do
      . \$f
    done
fi
command+=" -b /dev"
command+=" -b /proc"
command+=" -b arch-fs/root:/dev/shm"
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
echo "making $bin executable"
chmod +x $bin
echo "removing image for some space"
rm $tarball
echo "You can now launch Arch Linux with the ./${bin} script"
echo "Preparing additional component for the first time, please wait..."
wget "https://raw.githubusercontent.com/Techriz/AndronixOrigin/master/Installer/Arch/armhf/resolv.conf" -P arch-fs/root
wget "https://raw.githubusercontent.com/Techriz/AndronixOrigin/master/Installer/Arch/armhf/additional.sh" -P arch-fs/root
rm -rf arch-fs/root/.bash_profile


wget $dlink2/i3.sh -O $folder/root/i3.sh
echo " #!/bin/bash
bash ~/additional.sh
pacman -Syyuu --noconfirm && pacman -S wget sudo --noconfirm 
mkdir -p ~/.vnc
clear
if [ ! -f /root/i3.sh ]; then
    wget --tries=20 $dlink2/i3.sh -O /root/i3.sh
    bash ~/i3.sh
else
    bash ~/i3.sh
fi
clear
if [ ! -f /usr/bin/vncserver ]; then
    pacman -S tigervnc --noconfirm > /dev/null
fi
clear
echo 'Welcome to Arch Linux | i3'
rm -rf ~/.bash_profile" > $folder/root/.bash_profile 

bash $bin
