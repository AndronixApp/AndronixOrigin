#!/bin/bash
clear
echo "Setting up serives..."
echo "Downloading required necessary packages"
sudo apt install ffmpeg -y
echo "Setting up config files..."
cat <<EOT >> /usr/local/bin/startmic 
echo "Starting Audio input service..."
echo ""
echo "Make sure you have started the server from the app with following config"
echo ""
echo "---------------------------"
echo "                          |"
echo " TARGET ADDRESS: 127.0.0.1|"
echo " TARGET PORT: 55555       |" 
echo " MODE: Manual             |"
echo " FORMAT: G.722            |"
echo "                          |"
echo "---------------------------"
echo ""
ffplay -i rtp://@127.0.0.1:55555 > output.log 2>&1 < /dev/null &
echo ""
echo " Black window will popup if Audio Input has been enabled" 
echo " To stop audio input close the window"
echo "If no window pops up check you have started server with proper config"
echo "Also open new Termux session and type pluseaudio --start"
EOT
chmod +x /usr/local/bin/startmic 

echo "To start the mic input next time type: startmic"
