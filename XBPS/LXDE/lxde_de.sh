#!/bin/bash

xbps-install -Su
xbps-install -S lxde xorg tigervnc wget 
mkdir ~/.vnc

wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/XBPS/XFCE4/vncserver-start -P /usr/local/bin/
wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/XBPS/XFCE4/vncserver-stop -P /usr/local/bin/
wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/XBPS/XFCE4/xstartup -P ~/.vnc/

chmod +x ~/.vnc/xstartup
chmod +x /usr/local/bin/vncserver-start
chmod +x /usr/local/bin/vncserver-stop

echo "export DISPLAY=":1"" >> /etc/profile
source /etc/profile

vncserver-start
echo "The VNC Server will be started at 127.0.0.1:5901"
echo " "
echo "You can connect to this address with a VNC Viewer you prefer"
echo " "
echo "Connect to this address will open a window with LXDE Desktop Environment"
echo " "
echo "To Kill VNC Server just run vncserver-stop"
