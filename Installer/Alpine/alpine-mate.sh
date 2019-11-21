#!/bin/sh

apk update
DISPLAY=:99
RESOLUTION=1920x1080x24

apk add --no-cache ca-certificates curl openssl xvfb x11vnc mate-desktop-environment dbus-x11 bash

echo ""
[ ! -f /root/.vnc/passwd ] && echo "No previous VNC password found. Setting andronix as default password!" && mkdir -p /root/.vnc && x11vnc -storepasswd andronix /root/.vnc/passwd || echo "Previously generated password found. Keeping your old password"
echo ""

echo "#!/bin/bash
nohup /usr/local/bin/vnc"  > /usr/local/bin/vncserver-start

echo "#!/bin/bash
export DISPLAY=$DISPLAY
/usr/bin/Xvfb $DISPLAY -screen 0 $RESOLUTION -ac +extension GLX +render -noreset & 
sleep 2 && mate-session &
sleep 2 && x11vnc -xkb -noxrecord -noxfixes -noxdamage -display $DISPLAY -forever -bg -rfbauth /root/.vnc/passwd -users root -rfbport 5901 -noshm &
echo 'Connect to localhost:1 with a VNC client'" > /usr/local/bin/vnc

echo "#!/bin/bash
pkill dbus
pkill Xvfb
pkill pulse" > /usr/local/bin/vncserver-stop

echo '#!/bin/bash
read -sp "Provide a new VNC password: " PASSWORD
mkdir -p /root/.vnc && x11vnc -storepasswd $PASSWORD /root/.vnc/passwd' > /usr/local/bin/vncpasswd

chmod +x /usr/local/bin/*
echo "Use the helper scripts vncserver-start and vncserver-stop to start and stop MATE"
