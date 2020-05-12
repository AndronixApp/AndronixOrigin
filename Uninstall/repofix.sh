
#!/bin/bash
folder=manjaro-fs
folder2=androjaro-fs
if [ -d "$folder" ]; then

    rm -rf $folder/etc/pacman.d/mirrorlist
    wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Uninstall/mirrorlist -O $folder/etc/pacman.d/mirrorlist
    sed -i '1s/^/pacman-key --init \&\& pacman-key --populate \&\& pacman -Syu --noconfirm\n/' $folder/root/.bash_profile
fi

if [ -d "$folder2" ]; then

    rm -rf $folder2/etc/pacman.d/mirrorlist
    wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Uninstall/mirrorlist -O $folder2/etc/pacman.d/mirrorlist
    sed -i '1s/^/pacman-key --init \&\& pacman-key --populate \&\& pacman -Syu --noconfirm\n/' $folder2/root/.bash_profile

fi
