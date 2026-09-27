# machine-id: make sure /etc/machine-id holds an id (some tools open it
# before anything writes one).
if [ ! -s /etc/machine-id ]; then
    chmod u+w /etc/machine-id 2>/dev/null
    tr -dc 0-9a-f </dev/urandom 2>/dev/null | head -c 32 >/etc/machine-id && echo >>/etc/machine-id
fi
