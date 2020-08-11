#!/bin/bash

#Get the necessary components
sudo apt-mark hold udisks2
sudo apt-get update -y
sudo apt-get install keyboard-configuration -y
sudo apt-get install tzdata -y
sudo apt-get install sudo wget nano inetutils-tools dialog -y
sudo apt-get install lxqt-core lxqt-config qterminal tigervnc-standalone-server dbus-x11 openbox -y
sudo apt-get install xfe -y
sudo apt-get clean

#Setup the necessary files
mkdir -p ~/.vnc
echo "#!/bin/bash
[ -r $HOME/.Xresources ] && xrdb $HOME/.Xresources
export PULSE_SERVER=127.0.0.1
XAUTHORITY=$HOME/.Xauthority
export XAUTHORITY                                                         
LANG=en_US.UTF-8
export LANG
echo $$ > /tmp/xsession.pid
dbus-launch --exit-with-session startlxqt &" > ~/.vnc/xstartup

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
echo "Connect to this address will open a window with LXQt Desktop Environment"
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

vncpasswd
vncserver-start
