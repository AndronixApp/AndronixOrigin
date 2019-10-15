#!/bin/bash
apt update
apt purge chromium-broswer* -y
apt purge chromium* -y
apt install software-properties-common
echo "deb http://ppa.launchpad.net/chromium-team/stable/ubuntu bionic main 
deb-src http://ppa.launchpad.net/chromium-team/stable/ubuntu bionic main" >> /etc/apt/sources.list
apt update && apt install chromium-browser
apt install chromium-browser
