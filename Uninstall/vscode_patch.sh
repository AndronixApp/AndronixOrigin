#!/bin/bash

url="https://github.com/AndronixApp/andronix-external-app/releases/download/1.0.0/code-stable-"
ARCH=""

red="\033[0;31m"
yellow="\033[1;33m"
green="\033[0;32m"
ncolor="\033[0m"

echo "${green}Verifying architecture..."
case `dpkg --print-architecture` in
aarch64)
        ARCH="arm64" ;;
arm64)
        ARCH="arm64" ;;
arm)
        ARCH="armhf" ;;
*)
        echo -e "${red}Architecture not supported! Exiting...${ncolor}"; exit 1 ;;
esac

echo -e "${green}Detected architecture: $ARCH${ncolor}"

echo "Removing old VSCode/Headmelted repository..."
rm -rf /etc/apt/sources.list.d/headmelted_vscode.list

echo "Verifying dependencies..."
apt update -y
apt install sudo -y
sudo apt install wget tar -y

if [ -d "/opt" ]
then
    echo -e "${green}/opt exists. Installing VSCode in /opt${ncolor}"
    wget $url${ARCH}.tar.gz -O /opt/vscode.tar.gz
else
    echo -e "${yellow}/opt not found. Creating /opt directory and installing VSCode in /opt${ncolor}" 
    mkdir /opt
    wget $url${ARCH}.tar.gz -O /opt/vscode.tar.gz
fi

sudo mkdir /opt/vscode
sudo tar xf /opt/vscode.tar.gz -C /opt/vscode --strip-components=1
sudo rm -rf /opt/vscode.tar.gz

echo -e "alias code='/opt/vscode/code --no-sandbox'" >> ~/.bashrc

echo -e "${green}Installation complete! Run command ${red}code${ncolor} to launch VSCode"
