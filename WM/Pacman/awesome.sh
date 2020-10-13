#!/bin/bash
clear
echo "Installing AwesomeWM"
sleep 2
pacman -Syu --noconfirm
pacman -S awesome tigervnc wget feh nano dbus xorg xterm xfce4-terminal pcmanfm shotwell cairo-dock --noconfirm
pacman -S tigervnc wget tar sed --noconfirm
clear

read -p "Want to install default browser ? (y/n)" choice
case "$choice" in 
  y|Y ) pacman -S epiphany --noconfirm ;;
  n|N ) echo "Ok... Not epiphany browser";;
  * ) echo "invalid";;
esac

read -p "What to install chromium browser ? (y/n) [ Chromium might not work on arm/arm32/armhf devices ] " choice
case "$choice" in 
  y|Y ) pacman -S chromium --noconfirm ;;
  n|N ) echo "Ok...Not installing Chromium";;
  * ) echo "invalid";;
esac

mkdir -p ~/.vnc

wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/WM/wallpaper.jpg -O /usr/share/wallpaper.jpg
echo "#!/bin/bash
[ -r ~/.Xresources ] && xrdb ~/.Xresources
export PULSE_SERVER=127.0.0.1
export DISPLAY=:1
XAUTHORITY=~/.Xauthority
export XAUTHORITY
dbus-launch awesome &
dbus-launch cairo-dock & " > ~/.vnc/xstartup
chmod +x ~/.vnc/xstartup

wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Pacman/Manjaro/XFCE/vncserver-start -O /usr/local/bin/vncserver-start
wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Pacman/Manjaro/XFCE/vncserver-stop -O /usr/local/bin/vncserver-stop
chmod +x /usr/local/bin/vncserver-start
chmod +x /usr/local/bin/vncserver-stop


echo "You can now start vncserver by running vncserver-start"
echo ""
echo "It will ask you to enter a password when first time starting it."
echo ""
echo "The VNC Server will be started at 127.0.0.1:5901"
echo ""
echo "You can connect to this address with a VNC Viewer you prefer"
echo ""
echo ""
echo ""
echo "Running vncserver-start"
echo ""
echo ""
echo ""
echo "To Kill VNC Server just run vncserver-stop"
echo ""
echo ""
echo ""

wget https://raw.githubusercontent.com/AndronixApp/AndronixOrigin/master/Pacman/tigervnc-fix.sh -O ~/tigervnc-fix.sh && bash ~/tigervnc-fix.sh
