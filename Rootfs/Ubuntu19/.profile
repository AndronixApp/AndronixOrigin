exit() {
        if pgrep -f tiger >/dev/null;then
                unset -f exit
                exit
        else
                pkill dbus
                pkill ssh-agent
                pkill gpg-agent
                pkill pulseaudio
                unset -f exit
                exit
        fi
}
