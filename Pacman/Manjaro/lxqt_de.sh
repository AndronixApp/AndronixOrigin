#!/bin/bash
echo "Installing LXQT Desktop"
echo " "
echo " "
echo "Updating the system "
echo " "
echo " "
sudo pacman -Syu --noconfirm
sudo pacman -S lxqt xscreensaver --noconfirm 
sudo pacman -S tigervnc --noconfirm
sudo pacman -Syu --noconfirm
