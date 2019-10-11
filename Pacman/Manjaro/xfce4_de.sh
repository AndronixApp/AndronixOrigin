#!/bin/bash
echo "Installing XFCE Desktop"
echo " "
echo " "
echo "Updating the system "
echo " "
echo " "
sudo pacman -Syu --noconfirm
sudo pacman -S xfce4 xfce4-goodies manjaro-xfce-settings --noconfirm 
sudo pacman -S tigervnc --noconfirm
sudo pacman -Syu --noconfirm
