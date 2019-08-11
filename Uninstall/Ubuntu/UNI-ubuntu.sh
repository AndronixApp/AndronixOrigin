#!/data/data/com.termux/files/usr/bin/bash

echo "Starting to uninstall, please be patient..."

chmod 777 -R ubuntu-fs
rm -rf ubuntu-fs
rm -rf ubuntu-binds
rm -rf ubuntu.sh
rm -rf start-ubuntu.sh
rm -rf ssh-apt.sh
rm -rf de-apt.sh
rm -rf de-apt-xfce4.sh
rm -rf de-apt-mate.sh
rm -rf de-apt-lxqt.sh
rm -rf de-apt-lxde.sh
rm -rf UNI-ubuntu.sh

echo "Done"
