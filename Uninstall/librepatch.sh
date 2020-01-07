#!/bin/bash
sudo apt install libreoffice --no-install-recommends -y 
rm -rf /usr/lib/libreoffice/program/oosplash 
wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Uninstall/oosplash?raw=true  -q -O /usr/lib/libreoffice/program/oosplash
chmod +x /usr/lib/libreoffice/program/oosplash
mkdir /prod && mkdir /prod/version
echo "Patch has been applied successfully"
