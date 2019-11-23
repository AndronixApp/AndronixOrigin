#!/bin/bash
clear
echo "Setting up serives..."
echo "Downloading required necessary packages"
sudo apt install ffmpeg -y
echo "Setting up config files..."
echo "
echo "Starting Audio input service..."
echo ""
echo "Make sure you have started the server from the app with following config"
echo ""
echo "---------------------------"
echo "				|"
echo " TARGET ADDRESS: 127.0.0.1|"
echo " TARGET PORT: 55555	|" 
echo " MODE: Manual		|"
echo " FORMAT: G.726		|"
echo "			        |"
echo "---------------------------"
echo ""
ffplay rtp://127.0.0.1:55555
echo ""
echo " Black window will popup if Audio Input has been enabled" 
echo " To stop audio input close the window"
echo "If no window pops up check you have started server with proper config"
echo "Also open new Termux session and type pluseaudio --start"

" >> /usr/local/bin/startmic 
chmod +x /usr/local/bin/startmic 

echo "To start the mic input type: startmic"
