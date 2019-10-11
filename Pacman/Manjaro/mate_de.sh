#!/bin/bash
echo "Installing MATE Desktop"
echo " "
echo " "
echo "Updating the system "
echo " "
echo " "
sudo pacman -Syu --noconfirm
sudo pacman -S mate  --noconfirm 
sudo pacman -S tigervnc --noconfirm
sudo pacman -Syu --noconfirm
