#!/data/data/com.termux/files/usr/bin/bash

echo "Starting to uninstall, please be patient..."

chmod 777 -R alpine-fs
rm -rf alpine-fs
rm -rf alpine-binds
rm -rf alpine.sh*
rm -rf start-alpine.sh
rm -rf UNI-alpine.sh*
echo "Done"
