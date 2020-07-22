#!/data/data/com.termux/files/usr/bin/bash

echo "Starting to uninstall, please be patient..."

chmod 777 -R ubuntu20-fs
rm -rf ubuntu20-fs
rm -rf ubuntu20-binds
rm -rf ubuntu20.sh
rm -rf start-ubuntu20.sh
rm -rf de-apt-xfce4.sh
rm -rf de-apt-mate.sh
rm -rf de-apt-lxqt.sh
rm -rf de-apt-lxde.sh
rm -rf UNI-ubuntu.sh
rm -rf ubuntu20*
echo "Done"
