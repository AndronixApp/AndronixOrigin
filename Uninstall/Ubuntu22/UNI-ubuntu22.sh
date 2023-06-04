#!/data/data/com.termux/files/usr/bin/bash

echo "Starting to uninstall, please be patient..."

chmod 777 -R ubuntu22-fs
rm -rf ubuntu22-fs
rm -rf ubuntu22-binds
rm -rf ubuntu22.sh
rm -rf start-ubuntu22.sh
rm -rf de-apt-xfce4.sh
rm -rf de-apt-mate.sh
rm -rf de-apt-lxqt.sh
rm -rf de-apt-lxde.sh
rm -rf UNI-ubuntu.sh
rm -rf ubuntu22*
echo "Done"
