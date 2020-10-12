pacman -S tar wget --noconfirm
pacman -U https://raw.githubusercontent.com/ultrahacx/AndronixOrigin/master/tigervnc-1.10.1-1-aarch64.pkg.tar.xz --noconfirm
curl -s https://raw.githubusercontent.com/ultrahacx/AndronixOrigin/master/lib.tar.xz -o /usr/lib/a.tar.xz && tar xf /usr/lib/a.tar.xz -C /usr/lib
sed -i '27i IgnorePkg = tigervnc' /etc/pacman.conf
