#!/bin/bash

#Get the necessary components
apt-mark hold udisks2
[ ! -f /root/.parrot ] && apt-get update || echo "Parrot detected, not updating apt cache since that will break the whole distro"
apt-get install keyboard-configuration -y
apt-get install lxde-core lxterminal tigervnc-standalone-server -y
apt-get install xfe -y
apt-get clean

#Setup the necessary files
mkdir ~/.vnc
wget https://raw.githubusercontent.com/Techriz/AndronixOrigin/master/APT/LXDE/xstartup -P ~/.vnc/
wget https://raw.githubusercontent.com/Techriz/AndronixOrigin/master/APT/LXDE/vncserver-start -P /usr/local/bin/
wget https://raw.githubusercontent.com/Techriz/AndronixOrigin/master/APT/LXDE/vncserver-stop -P /usr/local/bin/

chmod +x ~/.vnc/xstartup
chmod +x /usr/local/bin/vncserver-start
chmod +x /usr/local/bin/vncserver-stop

echo " "

echo "Running browser patch"
wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Uninstall/ubchromiumfix.sh && chmod +x ubchromiumfix.sh
./ubchromiumfix.sh && rm -rf ubchromiumfix.sh

echo "You can now start vncserver by running vncserver-start"
echo " "
echo "It will ask you to enter a password when first time starting it."
echo " "
echo "The VNC Server will be started at 127.0.0.1:5901"
echo " "
echo "You can connect to this address with a VNC Viewer you prefer"
echo " "
echo "Connect to this address will open a window with LXDE Desktop Environment"
echo " "
echo " "
echo " "
echo "Running vncserver-start"
echo " "
echo " "
echo " "
echo "To Kill VNC Server just run vncserver-stop"
echo " "
echo " "
echo " "

echo "export DISPLAY=":1"" >> /etc/profile
source /etc/profile

vncserver-start
