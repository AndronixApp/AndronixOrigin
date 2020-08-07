#!/data/data/com.termux/files/usr/bin/bash
pkg install wget -y 
folder=manjaro-fs
dlink="https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Pacman/Manjaro"
dlink2="https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/WM/Pacman"
if [ -d "$folder" ]; then
    first=1
    echo "skipping downloading"
fi
tarball="manjaro.tar.xz"
if [ "$first" != 1 ];then
    wget --tries=20 https://github.com/AndronixApp/AndronixOrigin/raw/master/Rootfs/Manjaro/manjaro.partaa -O manjaro.partaa
    wget --tries=20 https://github.com/AndronixApp/AndronixOrigin/raw/master/Rootfs/Manjaro/manjaro.partab -O manjaro.partab
    wget --tries=20 https://github.com/AndronixApp/AndronixOrigin/raw/master/Rootfs/Manjaro/manjaro.partac -O manjaro.partac
    cat manjaro.parta* > manjaro.tar.xz
    cur=`pwd`
    mkdir -p "$folder"
    cd "$folder"
    echo "Decompressing Rootfs, please be patient."
    proot --link2symlink tar -xf ${cur}/$tarball --exclude='dev'|| :
    cd "$cur"
fi
mkdir -p manjaro-binds
bin=start-manjaro.sh
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
if [ -n "\$(ls -A manjaro-binds)" ]; then
    for f in manjaro-binds/* ;do
      . \$f
    done
fi
command+=" -b /dev"
command+=" -b /proc"
command+=" -b manjaro-fs/root:/dev/shm"
## uncomment the following line to have access to the home directory of termux
#command+=" -b /data/data/com.termux/files/home:/root"
## uncomment the following line to mount /sdcard directly to / 
#command+=" -b /sdcard"
command+=" -w /root"
command+=" /usr/bin/env -i"
command+=" HOME=/root"
command+=" PATH=/usr/local/sbin:/usr/local/bin:/bin:/usr/bin:/sbin:/usr/sbin:/usr/games:/usr/local/games"
command+=" TERM=\$TERM"
command+=" LANG=en_US.UTF-8"
command+=" LC_ALL=C"
command+=" LANGUAGE=en_US"
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
echo "fixing permissions for manjaro"
chmod 755 -R manjaro-fs

#DE installation

rm -rf $folder/root/.bash_profile
wget $dlink2/i3.sh -O $folder/root/i3.sh
rm -rf $folder/etc/resolv.conf
cat >$folder/etc/pacman.d/mirrorlist <<'EOL'
##
## Manjaro Linux repository mirrorlist
## Generated on 02 May 2020 14:22
##
## Use pacman-mirrors to modify
##
## Location  : Germany
## Time      : 99.99
## Last Sync :
Server = http://manjaro-arm.moson.eu/arm-stable/$repo/$arch/
EOL
rm -rf $folder/etc/resolv.conf && echo "nameserver 1.1.1.1" > $folder/etc/resolv.conf
wget --tries=20  $dlink/XFCE/vncserver-start -O $folder/usr/local/bin/vncserver-start 
wget --tries=20 $dlink/XFCE/vncserver-stop -O $folder/usr/local/bin/vncserver-stop
chmod +x $folder/usr/local/bin/vncserver-stop
chmod +x $folder/usr/local/bin/vncserver-start
echo " #!/bin/bash
echo 'pacman-key --init && pacman-key --populate && pacman -Syu --noconfirm' > /usr/local/bin/fix-repo
chmod +x /usr/local/bin/fix-repo
fix-repo
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
rm -rf ~/.bash_profile" > $folder/root/.bash_profile 
rm -rf manjaro.partaa manjaro.partab manjaro.partac manjaro.tar.xz manjaro.sh
bash $bin
