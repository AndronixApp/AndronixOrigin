#!/data/data/com.termux/files/usr/bin/bash

echo "Uninstalling Kali, please be patient..."

chmod 777 -R kali-fs
rm -rf kali-fs
rm -rf kali-binds
rm -rf kali.sh
rm -rf start-kali.sh
rm -rf ssh-apt.sh
rm -rf de-apt.sh
rm -rf de-apt-xfce4.sh
rm -rf de-apt-mate.sh
rm -rf de-apt-lxqt.sh
rm -rf de-apt-lxde.sh
rm -rf UNI-kali.sh

echo "Done"
