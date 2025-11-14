#!/data/data/com.termux/files/usr/bin/bash

# Variables
BLUE="\e[34m"         # Blue Color
NO_COLOR="\e[39m"     # No Color
isFsPresent=0         # Handles if the tarball is downloaded or skipped (overridden by -f)
distroName="Ubuntu"   # Name of the concerned distro
name="ubuntu"         # Name of the concerned distro
rootfsFolder=$name-fs # RootFS folder name
bindsFolder=$name-fs  # Bind folder name
tarball="${name}-rootfs.tar.xz"
folderSize=$(stat -c%s $rootfsFolder)

# Function used for sending updates to Andronix App
function sendUpdates() {
  message=$(echo "$1" | sed 's/ /\\ /g')
  am broadcast --user 0 -a app.andronix.PROGRESS --es app.andronix.PROGRESS_STATUS "$message" >/dev/null
}

#Welcome
echo "
    ___                __                           _
   /   |   ____   ____/ /   _____  ____    ____    (_)   _  __
  / /| |  / __ \ / __  /   / ___/ / __ \  / __ \  / /   | |/_/
 / ___ | / / / // /_/ /   / /    / /_/ / / / / / / /   _>  <
/_/  |_|/_/ /_/ \__,_/   /_/     \____/ /_/ /_/ /_/   /_/|_|
                                                              "

echo -e "\n${BLUE}Welcome to the Andronix ${distroName} installer.${NO_COLOR}"
echo -e "Join Andronix ${BLUE}Discord${NO_COLOR} @ https://chat.andronix.app (giveaways too ;)"
echo -e "Read our ${BLUE}Documentation${NO_COLOR} @ https://docs.andronix.app \n\n"

sendUpdates "Script Detected"

sleep 1

# Getting the -f (force download flag)
while getopts "f" OPTION; do
  # shellcheck disable=SC2220
  case $OPTION in
  f)
    echo -e "\nForcing the download..."
    rm -rf $rootfsFolder
    rm -rf $bindsFolder
    isFsPresent=0
    ;;
  esac
done

# Installing basic termux packages
pkg install wget -y

# Checking if the rootfsFolder is present
if [ -d "$rootfsFolder" ] && [ "$folderSize" -gt 6000 ]; then
  isFsPresent=1
  echo -e "\nRoot Files System already present. Skipping download..."
fi

if [ "$isFsPresent" != 1 ]; then
  if [ ! -f "$tarball" ]; then
    echo -e "\nDownloading Rootfs for ${distroName}. Please be patient...\n\n"
    case $(dpkg --print-architecture) in
    aarch64)
      arch="arm64"
      ;;
    arm)
      arch="armhf"
      ;;
    amd64)
      arch="amd64"
      ;;
    x86_64)
      arch="amd64"
      ;;
    i*86)
      arch="i386"
      ;;
    x86)
      arch="i386"
      ;;
    *)
      echo "There was an issue identifying the architecture of your device!"
      sendUpdates "Oops! Architecture error."
      exit 1
      ;;
    esac
    sendUpdates "Downloading the files..."
    wget -q --show-progress --progress=bar "https://github.com/Techriz/AndronixOrigin/blob/master/Rootfs/${distroName}/${arch}/ubuntu-rootfs-${arch}.tar.xz?raw=true" -O $tarball
  fi
  currentDirectory=$(pwd)
  mkdir -p "$rootfsFolder"
  cd "$rootfsFolder"
  sendUpdates "Decompressing the files..."
  echo -e "\nDecompressing Rootfs. This can take a while. Please be patient."
  proot --link2symlink tar -xJf "${currentDirectory}"/${tarball} || :
  cd "$currentDirectory"
fi

mkdir -p $bindsFolder
bin=start-ubuntu.sh
echo -e "\nWriting the launch script now! Just a few more steps!"
cat >$bin <<-EOM
#!/bin/bash
cd \$(dirname \$0)
## unset LD_PRELOAD in case termux-exec is installed
unset LD_PRELOAD
command="proot"
command+=" --link2symlink"
command+=" -0"
command+=" -r $rootfsFolder"
if [ -n "\$(ls -A $bindsFolder)" ]; then
    for f in $bindsFolder/* ;do
      . \$f
    done
fi
command+=" -b /dev"
command+=" -b /proc"
command+=" -b ubuntu-fs/root:/dev/shm"
## uncomment the following line to have access to the home directory of termux
#command+=" -b /data/data/com.termux/files/home:/root"
## uncomment the following line to mount /sdcard directly to /
#command+=" -b /sdcard"
command+=" -w /root"
command+=" /usr/bin/env -i"
command+=" HOME=/root"
command+=" PATH=/usr/local/sbin:/usr/local/bin:/bin:/usr/bin:/sbin:/usr/sbin:/usr/games:/usr/local/games"
command+=" TERM=\$TERM"
command+=" LANG=C.UTF-8"
command+=" /bin/bash --login"
com="\$@"
if [ -z "\$1" ];then
    exec \$command
else
    \$command -c "\$com"
fi
EOM

termux-fix-shebang $bin
chmod +x $bin
echo "Saving some space..."
rm $tarball
echo -e "\n\nYay! All done. You can now start ${distroName} with running ${BLUE}\n./${bin}${NO_COLOR}"
echo "You will then asked to configure some distro specific things."

sendUpdates "Installation Success!"

sleep 1
echo "Exiting...Bye!"
