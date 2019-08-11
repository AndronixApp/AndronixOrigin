#!/data/data/com.termux/files/usr/bin/bash

echo "Starting to uninstall, please be patient..."

chmod 777 -R fedora-fs
rm -rf fedora-fs
rm -rf fedora-binds
rm -rf fedora.sh
rm -rf start-fedora.sh
rm -rf ssh-yum.sh
rm -rf de-yum.sh
rm -rf de-yum-xfce4.sh
rm -rf de-yum-mate.sh
rm -rf de-yum-lxqt.sh
rm -rf de-yum-lxde.sh
rm -rf UNI-fedora.sh

echo "Done"
