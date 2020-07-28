#!/bin/bash

echo "Removing distribution provided chromium packages and dependencies..."
apt purge chromium* chromium-browser* snapd -y -qq && apt autoremove -y -qq
sudo apt purge chromium* chromium-browser* -y -qq && apt autoremove -y -qq

echo "Adding Debian repo for Chromium installation"

cat <<EOT >> /etc/apt/preferences.d/chromium.pref
#Note: 2 blank lines are required between entries

Package: *
Pin: release a=eoan
Pin-Priority: 500

Package: *
Pin: origin "ftp.debian.org"
Pin-Priority: 300

# Pattern includes 'chromium', 'chromium-browser' and similarly                 
# named dependencies:                                                            Package: chromium*
Pin: origin "ftp.debian.org"
Pin-Priority: 700
EOT

echo "deb http://ftp.debian.org/debian buster main
deb http://ftp.debian.org/debian buster-updates main" > /etc/apt/sources.list

apt-key adv --keyserver keyserver.ubuntu.com --recv-keys DCC9EFBF77E11517
apt-key adv --keyserver keyserver.ubuntu.com --recv-keys 648ACFD622F3D138
apt-key adv --keyserver keyserver.ubuntu.com --recv-keys AA8E81B4331F7F50
apt-key adv --keyserver keyserver.ubuntu.com --recv-keys 112695A0E562B32A

apt update -y
apt install chromium -y

sed -i 's/chromium-browser %U/chromium --no-sandbox %U/g' /usr/share/application>
