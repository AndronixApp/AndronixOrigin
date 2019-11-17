rm -rf /etc/resolv.conf
echo "nameserver 8.8.8.8" >> /etc/resolv.conf
chmod +x /usr/share/andronix/firstrun
apt update -y && apt install ffmpeg dialog -y
groupadd --gid 3003 permagrp
groupadd --gid 9997 permagrp2
groupadd --gid 50110 permagrp3
groupadd --gid 99909997 permagrp4
chmod 4755 /usr/bin/sudo
/usr/share/andronix/firstrun

