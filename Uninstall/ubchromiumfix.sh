#!/bin/bash
echo "Removing distribution provided chromium packages and dependencies..."
apt purge chromium* chromium-browser* -y -qq && apt autoremove -y -qq
sudo apt purge chromium* chromium-browser* -y -qq && apt autoremove -y -qq
echo "Enabling PPA support..."
[ ! -f .parrot ] && apt update -qq; apt install software-properties-common gnupg --no-install-recommends -y -qq
echo " Adding chromium-team stable ppa"
echo "deb http://ppa.launchpad.net/ultrahacx/chromium-universal/ubuntu bionic main 
deb-src http://ppa.launchpad.net/ultrahacx/chromium-universal/ubuntu bionic main " >> /etc/apt/sources.list
echo "Fetching and importing chromium-team GPG keys..."
apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 8FEA526CE21182D1
echo "Installing chromium-browser"
apt update -qq; apt install chromium-browser --no-install-recommends -y
echo "Patching application shortcuts..."
sed -i 's/chromium-browser %U/chromium-browser --no-sandbox %U/g' /usr/share/applications/chromium-browser.desktop
