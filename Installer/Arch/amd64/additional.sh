systemctl disable systemd-resolved.service
rm /etc/resolv.conf
mv resolv.conf /etc
pacman-key --init
echo "disable-scdaemon" > /etc/pacman.d/gnupg/gpg-agent.conf
pacman-key --populate archlinux

echo ""
echo ""
echo "Changing some permissions, please be patient"
echo ""
echo ""
chmod 755 -R /bin /home /mnt /run /srv /tmp /var /boot /etc /lin /opt /root /sbin /sys /usr
echo "IMPORTANT"
echo ""
echo ""
echo "If you are using Android 9 and above, you will encounter this error:"
echo ""
echo "could not change the root directory (Function not implemented)"
echo ""
echo "Simply ignore it as it does not do anything harmful"
echo ""
echo ""
