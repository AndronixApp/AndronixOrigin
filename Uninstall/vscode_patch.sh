#!/bin/sh

echo "Detecting architecture...";
MACHINE_MTYPE="$(uname -m)";
ARCH="${MACHINE_MTYPE}";
REPO_VENDOR="headmelted";

echo "Ensuring curl is installed";
apt-get install -y gnupg curl;

if [ "$ARCH" = "amd64" ] || [ "$ARCH" = "i386" ]; then REPO_VENDOR="microsoft"; fi;

echo "Architecture detected as $ARCH...";

if [ "${REPO_VENDOR}" = "headmelted" ]; then
  gpg_key=https://packagecloud.io/headmelted/codebuilds/gpgkey;
  repo_name="stretch";
  repo_entry="deb https://packagecloud.io/headmelted/codebuilds/debian/ ${repo_name} main";
  code_executable_name="code-oss";
else
  gpg_key=https://packages.microsoft.com/keys/microsoft.asc;
  repo_name="stable"
  repo_entry="deb https://packages.microsoft.com/repos/vscode ${repo_name} main";
  code_executable_name="code-insiders";
fi;

echo "Retrieving GPG key [${REPO_VENDOR}] ($gpg_key)...";
# import the gpg key
curl -L "${gpg_key}" 2> /dev/null | apt-key add - &>/dev/null
echo "done."

echo "Removing any previous entry to headmelted repository";
rm -rf /etc/apt/sources.list.d/headmelted_codebuilds.list;
rm -rf /etc/apt/sources.list.d/codebuilds.list;
  
echo "Installing [${REPO_VENDOR}] repository...";
echo "${repo_entry}" > /etc/apt/sources.list.d/${REPO_VENDOR}_vscode.list;
  
echo "Updating APT cache..."
apt-get update -yq;
echo "Done!"

if [ $? -eq 0 ]; then
  echo "Repository install complete.";
else
  echo "Repository install failed.";
  exit 1;
fi;

echo "Installing Visual Studio Code from [${repo_name}]...";
apt-get install -t ${repo_name} -y ${code_executable_name};
#apt-get install -t ${repo_name} -y --allow-unauthenticated ${code_executable_name};

if [ $? -eq 0 ]; then
  echo "Visual Studio Code install complete.";
else
  echo "Visual Studio Code install failed.";
  exit 1;
fi;

echo "Installing git...";
apt-get install -y git;

if [ $? -eq 0 ]; then
  echo "git install complete.";
else
  echo "git install failed.";
  exit 1;
fi;

echo "Installing any dependencies that may have been missed...";
apt-get install -y -f;

if [ $? -eq 0 ]; then
  echo "Missed dependency install complete.";
else
  echo "Missed dependency install failed.";
  exit 1;
fi;

echo "

Installation complete!

You can start code at any time by calling \"${code_executable_name}\" within a terminal.

A shortcut should also now be available in your desktop menus (depending on your distribution).

";
sed -i 's/code-oss /code-oss --user-data-dir=\/root /g' /usr/share/applications/code-oss*
echo "alias code-oss='code-oss --user-data-dir=/root'" >> /etc/profile
