#!/data/data/com.termux/files/usr/bin/bash
pkg install wget pv proot tar -y
#Variables we need. Script is modular, change below variables to install different distro's
name="Alpine"
distro=alpine
folder=$distro-fs
tarball="alpine-rootfs.tar.gz"
echo " "
echo " "
echo "----------------------------------------------------------"
echo "|  NOTE THAT ALL THE PREVIOUS ALPINE DATA WILL BE ERASED |"
echo "----------------------------------------------------------"
echo "If you want to keep your old $distro press Ctrl - c now!! "
echo -n "5. "
sleep 1
echo -n "4. "
sleep 1
echo -n "3. "
sleep 1
echo -n "2. "
sleep 1 
echo -n "1. "
sleep 1 
echo "Removing $folder and $distro-binds"
chmod 777 $folder
rm -rf $distro-binds $folder
echo " "
echo "Proceeding with installation"
echo " "
echo  "Allow the Storage permission to termux"
echo " "
sleep 2
termux-setup-storage
clear

#Creating folders we need
mkdir -p $distro-binds $folder

#Performing a check for online or offline install
[ -f $tarball ] && check=1
if [ "$check" -eq "1" ] > /dev/null 2>&1; then
	echo "Local $distro rootfs found, extracting"
	echo ""
	if [ -x "$(command -v neofetch)" ]; then
		neofetch --ascii_distro Alpine -L
	fi
	echo ""
	pv $tarball | proot --link2symlink tar -zxf - -C $folder || :
else
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
	url=https://github.com/AndronixApp/AndronixOrigin/blob/master/Rootfs/Alpine/${archurl}/alpine-minirootfs-3.10.3-${archurl}.tar.gz?raw=true
	echo "Downloading and extracting $name"
	echo "Extraction happens in parallel"
	echo ""
	if [ -x "$(command -v neofetch)" ]; then
		neofetch --ascii_distro Alpine -L
	fi
	echo ""
	wget -qO- --tries=0 $url --show-progress --progress=bar:force:noscroll |proot --link2symlink tar -zxf - -C $folder --exclude='dev' || :
fi

bin=start-$distro.sh
if [ -d $folder/var ];then
	clear
	echo "---------------------------"
	echo "|  Writing launch script  |"
	echo "---------------------------"

	cat > $bin <<- EOM
	#!/data/data/com.termux/files/usr/bin/bash
	cd \$(dirname \$0)
	## unset LD_PRELOAD in case termux-exec is installed
	unset LD_PRELOAD
	command="proot"
	command+=" --link2symlink"
	command+=" -0"
	command+=" -r $folder"
	if [ -n "\$(ls -A $distro-binds)" ]; then
    		for f in $distro-binds/* ;do
      		. \$f
    	done
	fi
	command+=" -b /dev"
	command+=" -b /proc"
	command+=" -b $folder/root:/dev/shm"
	## uncomment the following line to have access to the home directory of termux
	#command+=" -b /data/data/com.termux/files/home:/root"
	## uncomment the following line to mount /sdcard directly to /
	#command+=" -b /sdcard"
	command+=" -w /root"
	command+=" /usr/bin/env -i"
	command+=" HOME=/root"
	command+=" PATH=/bin:/usr/bin:/sbin:/usr/sbin"
	command+=" TERM=\$TERM"
	command+=" LANG=en_US.UTF-8"
	command+=" LC_ALL=C"
	command+=" LANGUAGE=en_US"
	command+=" /bin/sh --login"
	com="\$@"
	if [ -z "\$1" ];then
    		exec \$command
	else
    		\$command -c "\$com"
	fi
	EOM

	echo "-------------------------------"
	echo "|  Checking for file presence  |"
	echo "-------------------------------"
	echo ""

	if test -f "$bin"; then
    		echo "Boot script present"
		chmod +x $bin
		termux-fix-shebang $bin
    		echo " "
	fi

	FD=$folder
	if [ -d "$FD" ]; then
  		echo "Boot container present"
	  	echo " "
	fi

	UFD=$distro-binds
	if [ -d "$UFD" ]; then
  		echo "Sub-Boot container present"
	  	echo " "
	fi

	echo "" > $folder/etc/fstab
	rm -rf $folder/etc/resolv.conf
	echo nameserver 8.8.8.8 > $folder/etc/resolv.conf
        ./$bin apk update
        ./$bin apk add --no-cache bash
        sed -i 's/ash/bash/g' $folder/etc/passwd
        sed -i 's/bin\/sh/bin\/bash/g' $bin
	echo "Installation Finished"
	rm -rf $folder/root/.bash_profile
  	echo "#!/bin/bash
              wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Installer/Alpine/alpine-xfce.sh -O /root/alpine-xfce.sh
              bash /root/alpine-xfce.sh
              rm -rf /root/alpine-xfce.sh
              clear" > $folder/root/.bash_profile  
   	bash $bin
else 
	echo "Installation unsuccessful"
	echo "Check network connectivity and contact devs on Discord if problems persist"
fi
