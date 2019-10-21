#!/bin/bash
echo "Removing distribution provided chromium packages and dependencies..."
apt purge chromium* chromium-browser* -y -qq && apt autoremove -y -qq
echo "Enabling PPA support..."
apt update -qq && apt install software-properties-common gnupg --no-install-recommends -y -qq
echo " Adding chromium-team stable ppa"
echo "deb http://ppa.launchpad.net/chromium-team/stable/ubuntu bionic main 
deb-src http://ppa.launchpad.net/chromium-team/stable/ubuntu bionic main" >> /etc/apt/sources.list
echo "Fetching and importing chromium-team GPG keys..."
apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 0EBEA9B02842F111
echo "Installing chromium-browser"
apt update && apt install chromium-browser --no-install-recommends -y -qq
echo "Patching application shortcuts..."
sed -i 's/chromium-browser %U/chromium-broswer --no-sandbox %U/g' /usr/share/applications/chromium-browser.desktop
echo 'alias chromium="chromium-browser --no-sandbox" >> /etc/profile'
echo "You can now start chromium by using the application icon or by typing chromium" && . /etc/profile
