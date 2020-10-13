#!/bin/bash
echo "Installing LXQT Desktop"
echo " "
echo " "
echo "Updating the system "
echo " "
echo " "
pacman -Suuyy --noconfirm
pacman -S lxqt xscreensaver sudo --noconfirm 
pacman -S tigervnc wget --noconfirm
pacman -S xorg --noconfirm
pacman -S wget tar sed --noconfirm

wget https://raw.githubusercontent.com/Techriz/AndronixOrigin/master/Pacman/Manjaro/LXQT/xstartup   -P ~/.vnc/
wget https://raw.githubusercontent.com/Techriz/AndronixOrigin/master/Pacman/vncserver-start -P /usr/local/bin/
wget https://raw.githubusercontent.com/Techriz/AndronixOrigin/master/Pacman/vncserver-stop -P /usr/local/bin/
chmod +x ~/.vnc/xstartup
chmod +x /usr/local/bin/vncserver-start
chmod +x /usr/local/bin/vncserver-stop

echo " "
echo "You can now start vncserver by running vncserver-start"
echo " "
echo "It will ask you to enter a password when first time starting it."
echo " "
echo "The VNC Server will be started at 127.0.0.1:5901"
echo " "
echo "You can connect to this address with a VNC Viewer you prefer"
echo " "
echo "Connect to this address will open a window with LXQT Desktop Environment"
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
wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Pacman/tigervnc-fix.sh -O ~/tigervnc-fix.sh && bash ~/tigervnc-fix.sh
