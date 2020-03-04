systemctl disable systemd-resolved.service
rm /etc/resolv.conf
mv resolv.conf /etc
pacman-key --init
echo "disable-scdaemon" > /etc/pacman.d/gnupg/gpg-agent.conf
pacman-key --populate archlinuxarm

echo ""
echo ""
echo "Changing some permissions, please be patient"
echo ""
echo ""
chmod 755 -R /bin /home /mnt /run /srv /tmp /var /boot /etc /opt /root /sbin /sys /usr
echo "IMPORTANT"
echo ""
echo ""
echo "Removing some unused packages, reclaims about 650MB"
yes | LC_ALL=C.UTF-8 pacman -Rncs linux-firmware
echo "If you are using Android 9 and above, you will encounter this error:"
echo ""
echo "could not change the root directory (Function not implemented)"
echo ""
echo "Simply ignore it as it does not do anything harmful"
echo ""
echo "updating Arch packages"
yes | LC_ALL=C.UTF-8 pacman -Suuyy 
echo ""
echo ""
