#!/bin/bash
echo "Installing LXDE Desktop"
echo " "
echo " "
echo "Updating the system "
echo " "
echo " "
sudo pacman -Syu --noconfirm
sudo pacman -S lxde --noconfirm 
sudo pacman -S tigervnc --noconfirm
sudo pacman -Syu --noconfirm
