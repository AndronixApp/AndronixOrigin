#!/data/data/com.termux/files/usr/bin/bash

echo "Uninstalling Parrot, please be patient..."

chmod 777 -R parrot-fs
rm -rf parrot-fs
rm -rf parrot-binds
rm -rf parrot.sh
rm -rf start-parrot.sh
rm -rf ssh-apt.sh
rm -rf de-apt.sh
rm -rf de-apt-xfce4.sh
rm -rf de-apt-mate.sh
rm -rf de-apt-lxqt.sh
rm -rf de-apt-lxde.sh
rm -rf UNI-parrot.sh

echo "Done"
