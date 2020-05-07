

#!/bin/bash
folder=manjaro-fs
folder2=androjaro-fs
if [ -d "$folder" ]; then

    rm -rf manjaro-fs/etc/pacman.d/mirrorlist
	cat > $folder/etc/pacman.d/mirrorlist <<- EOM
	##
	## Manjaro Linux repository mirrorlist
	## Generated on 02 May 2020 14:22
	##
	## Use pacman-mirrors to modify
	##
	## Location  : Germany
	## Time      : 99.99
	## Last Sync :
	Server = http://manjaro-arm.moson.eu/arm-stable/$repo/$arch/
	EOM
fi


if [ -d "$folder2" ]; then

    rm -rf androjaro-fs/etc/pacman.d/mirrorlist
	cat > $folder2/etc/pacman.d/mirrorlist <<- EOM
	##
	## Manjaro Linux repository mirrorlist
	## Generated on 02 May 2020 14:22
	##
	## Use pacman-mirrors to modify
	##
	## Location  : Germany
	## Time      : 99.99
	## Last Sync :
	Server = http://manjaro-arm.moson.eu/arm-stable/$repo/$arch/
	EOM
fi
