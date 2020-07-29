#!/bin/bash

echo "Removing distribution provided chromium packages and dependencies..."
apt purge chromium* chromium-browser* snapd -y -qq && apt autoremove -y -qq
sudo apt purge chromium* chromium-browser* -y -qq && apt autoremove -y -qq
apt update -qq; apt install software-properties-common gnupg --no-install-recommends -y -qq
echo "Adding Debian repo for Chromium installation"

echo "deb http://ftp.debian.org/debian buster main
deb http://ftp.debian.org/debian buster-updates main" > /etc/apt/sources.list

apt-key adv --keyserver keyserver.ubuntu.com --recv-keys DCC9EFBF77E11517
apt-key adv --keyserver keyserver.ubuntu.com --recv-keys 648ACFD622F3D138
apt-key adv --keyserver keyserver.ubuntu.com --recv-keys AA8E81B4331F7F50
apt-key adv --keyserver keyserver.ubuntu.com --recv-keys 112695A0E562B32A

apt update -y
apt install chromium -y

sed -i 's/chromium %U/chromium --no-sandbox %U/g' /usr/share/applications/chromium.desktop
