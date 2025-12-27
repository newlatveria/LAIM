#!/bin/bash

# ==========================================================
# 🚀 GRAND UNIFIED MASTER TOOLBOX v25.5
# THE COMPLETE EDITION - NO FEATURES REMOVED
# ==========================================================

# --- 1. PRE-FLIGHT & INSTALLER ---
if command -v apt &> /dev/null; then
    PKGMGR="apt"; INSTALL_CMD="sudo apt install -y"; UPDATE_CMD="sudo apt update"
elif command -v dnf &> /dev/null; then
    PKGMGR="dnf"; INSTALL_CMD="sudo dnf install -y"; UPDATE_CMD="sudo dnf check-update"
else
    echo "Unsupported Package Manager."; exit 1
fi

DEPS="bc curl pciutils xterm htop fastfetch radeontop stress-ng glmark2"
SCRIPT_PATH="/usr/local/bin/toolbox"

if [[ "$0" != "$SCRIPT_PATH" && "$1" != "--no-install" ]]; then
    echo -e "\033[1;36m--- Toolbox Installer ---\033[0m"
    read -p "Install to system (/usr/local/bin/toolbox) and setup dependencies? (y/n): " inst_choice
    if [[ "$inst_choice" == "y" ]]; then
        $UPDATE_CMD
        $INSTALL_CMD $DEPS 2>/dev/null || $INSTALL_CMD bc curl pciutils xterm htop
        sudo cp "$0" "$SCRIPT_PATH" && sudo chmod +x "$SCRIPT_PATH"
        echo -e "\033[1;32mInstallation complete! Run 'toolbox' from anywhere.\033[0m"
        exit 0
    fi
fi

# --- 2. CONFIGURATION & HARDWARE ---
printf '\033[8;50;115t'
RED='\033[1;31m'; GREEN='\033[1;32m'; YELLOW='\033[1;33m'; BLUE='\033[1;34m'; MAGENTA='\033[1;35m'; CYAN='\033[1;36m'; WHITE='\033[1;37m'; STD='\033[0m' 

GPU_VENDOR="Unknown"
if lspci | grep -qi "nvidia"; then GPU_VENDOR="Nvidia"
elif lspci | grep -qi "amd" || lspci | grep -qi "ati"; then GPU_VENDOR="AMD"
elif lspci | grep -qi "intel"; then GPU_VENDOR="Intel"; fi

lastmessage="System: $(hostname) | GPU: $GPU_VENDOR"
loadproject=""; PodName=""; thisfile=""

# --- 3. CORE UTILITIES ---
pause(){ echo ""; read -r -p "  Press [Enter] to continue..."; }

SpawnTerminal(){
    CMD="$1"; TITLE="$2"
    if [ -n "$DISPLAY" ]; then
        if command -v xterm &> /dev/null; then xterm -T "$TITLE" -e "bash -c \"$CMD\"" &
        elif command -v gnome-terminal &> /dev/null; then gnome-terminal --title="$TITLE" -- bash -c "$CMD" &
        else eval "$CMD"; pause; fi
    else eval "$CMD"; pause; fi
}

DrawHeader(){
    clear
    if command -v fastfetch &> /dev/null; then
        fastfetch --compact --structure OS:Host:Kernel:Uptime:Packages:DE:CPU:GPU:Memory
    fi
    echo -e "${BLUE}╠══════════════════════════════════════════════════════════════════════════════════════════════════${STD}"
    printf "${BLUE}║${STD} ${YELLOW}%-15s${STD} : %-25s ${YELLOW}%-15s${STD} : %-28s ${BLUE}║${STD}\n" "GPU Vendor" "$GPU_VENDOR" "Local IP" "$(hostname -I | awk '{print $1}')"
    echo -e "${BLUE}╚══════════════════════════════════════════════════════════════════════════════════════════════════${STD}"
}

# --- 4. MODULES (ALL PREVIOUS FEATURES RESTORED) ---

# [RESCUE & GRAPHICS ROOM]
Graphics_Menu(){
    while true; do
        clear; echo -e "${RED}--- Graphics Repair Room ($GPU_VENDOR) ---${STD}"
        echo " 1. Auto-Install Drivers | 2. Purge Drivers | 3. Reinstall Mesa/Xorg | 99. Back"
        read -p "Select: " g_choice
        case $g_choice in
            1) if [ "$GPU_VENDOR" == "Nvidia" ]; then [ "$PKGMGR" == "apt" ] && sudo ubuntu-drivers autoinstall || $INSTALL_CMD akmod-nvidia; else $INSTALL_CMD mesa-vulkan-drivers; fi ;;
            2) [ "$PKGMGR" == "apt" ] && sudo apt purge '*nvidia*' '*amdgpu*' -y || sudo dnf remove '*nvidia*';;
            3) [ "$PKGMGR" == "apt" ] && $INSTALL_CMD --reinstall xserver-xorg-video-all || $INSTALL_CMD mesa-dri-drivers;;
            99) return ;;
        esac
    done
}

# [PODMAN MANAGER]
Podman_Menu(){
    while true; do
        clear; echo -e "${BLUE}--- Podman Project Manager ---${STD}"
        echo " Active Project: ${loadproject:-None} | Pod: ${PodName:-None}"
        echo " 1. New Project (Folder) | 2. Load Project | 3. Create Pod | 4. Compose Up | 5. List All | 99. Back"
        read -p "Select: " p_choice
        case $p_choice in
            1) read -p "Project Name: " np; mkdir -p "$HOME/container_projects/$np"; loadproject="$np" ;;
            2) ls "$HOME/container_projects/"; read -p "Load Project Name: " loadproject ;;
            3) read -p "New Pod Name: " PodName; sudo podman pod create --name "$PodName" ;;
            4) if [ -z "$loadproject" ]; then echo "Load a project first."; pause; continue; fi
               cd "$HOME/container_projects/$loadproject" && ls *.yml; read -p "YML File: " f; sudo podman-compose -f "$f" up -d ;;
            5) sudo podman ps -a --pod; pause ;;
            99) return ;;
        esac
    done
}

# [ANDROID ADB SUITE]
Android_Menu(){
    while true; do
        clear; echo -e "${GREEN}--- Android Device Manager (ADB/Scrcpy) ---${STD}"
        adb devices | grep "device$"
        echo " 1. Connect (Wireless/IP) | 2. Install APK | 3. Push File to SDCard | 4. Live Logcat | 5. Scrcpy Options | 99. Back"
        read -p "Select: " a_choice
        case $a_choice in
            1) read -p "IP: " ip; adb connect "$ip:5555"; pause ;;
            2) read -e -p "APK Path: " apk; adb install "${apk%\"}"; pause ;;
            3) read -e -p "File: " f; adb push "$f" /sdcard/Download/; pause ;;
            4) SpawnTerminal "adb logcat" "Logcat" ;;
            5) echo "1.Normal 2.High Quality 3.Record Screen"; read -p "> " m
               [ "$m" == "1" ] && nohup scrcpy >/dev/null 2>&1 &
               [ "$m" == "2" ] && nohup scrcpy -b 8M >/dev/null 2>&1 &
               [ "$m" == "3" ] && nohup scrcpy --record "rec_$(date +%s).mp4" >/dev/null 2>&1 & ;;
            99) return ;;
        esac
    done
}

# [AI OLLAMA MODULE]
Ollama_Config(){
    if ! command -v ollama &> /dev/null; then curl -fsSL https://ollama.com/install.sh | sh; fi
    echo "Apply RX 570 Polaris GPU Patch? (y/n)"; read patch
    if [ "$patch" == "y" ]; then
        sudo mkdir -p /etc/systemd/system/ollama.service.d
        echo -e "[Service]\nEnvironment=\"HSA_OVERRIDE_GFX_VERSION=8.0.3\"" | sudo tee /etc/systemd/system/ollama.service.d/override.conf
        sudo systemctl daemon-reload && sudo systemctl restart ollama
        lastmessage="Ollama Polaris Patch Applied."
    fi
}

# --- 5. MAIN MENU ---
while true; do
    DrawHeader
    echo -e "\n ${GREEN}:: 1. MAINTENANCE ::${STD}                   ${RED}:: 2. RESCUE ROOM ::${STD}"
    printf " %-35s %-35s\n" "10. Install Core Tools" "20. Auto-Diagnostic Repair"
    printf " %-35s %-35s\n" "11. System Update ($PKGMGR)" "21. Graphics Repair ($GPU_VENDOR)"
    printf " %-35s %-35s\n" "12. Log Vacuum (Cleanup)" "22. Disk Analyzer (ncdu)"
    printf " %-35s %-35s\n" "13. Kill Zombie Processes" "23. GRUB Rescue Cheatsheet"

    echo -e "\n ${CYAN}:: 3. DEV, AI & CONTAINERS ::${STD}            ${MAGENTA}:: 4. HARDWARE & ANDROID ::${STD}"
    printf " %-35s %-35s\n" "30. Install/Config Ollama" "40. Android Manager (ADB)"
    printf " %-35s %-35s\n" "31. Podman Project Manager" "41. Monitor GPU ($GPU_VENDOR)"
    printf " %-35s %-35s\n" "32. Install Go (Latest)" "42. Install Docker Engine"
    printf " %-35s %-35s\n" "33. Manage Sudo Users" "43. GPU Stress Test (Glmark2)"

    echo -e "\n ${WHITE}80. Reboot System | 99. Exit Toolbox${STD}"
    echo " ----------------------------------------------------------------------------------------------------"
    echo -e "${YELLOW} Last Message: $lastmessage${STD}"
    read -r -p " Selection: " choice
    lastmessage=""

    case $choice in
        10) $INSTALL_CMD curl wget bc htop ncdu timeshift testdisk git mc; lastmessage="Tools Installed." ;;
        11) $UPDATE_CMD && sudo $PKGMGR upgrade -y ;;
        12) sudo journalctl --vacuum-time=2d; sudo $PKGMGR clean all 2>/dev/null || sudo apt clean ;;
        13) Z=$(ps -A -ostat,ppid,pid,cmd | grep -e '^[Zz]'); echo "$Z"; read -p "Parent PID to kill: " p; [ -n "$p" ] && sudo kill -9 $p ;;
        20) if [ "$PKGMGR" == "apt" ]; then sudo dpkg --configure -a; sudo apt --fix-broken install -y; fi; sudo update-grub 2>/dev/null || sudo grub2-mkconfig -o /boot/grub2/grub.cfg; pause ;;
        21) Graphics_Menu ;;
        22) if ! command -v ncdu &>/dev/null; then $INSTALL_CMD ncdu; fi; ncdu / ;;
        23) clear; echo "GRUB: set root=(hd0,1); insmod normal; normal"; pause ;;
        30) Ollama_Config ;;
        31) Podman_Menu ;;
        32) L=$(curl -s https://go.dev/dl/?mode=json | grep -o 'go[0-9.]*' | head -n 1); curl -L "https://go.dev/dl/${L}.linux-amd64.tar.gz" -o /tmp/go.tgz; sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tgz; grep -q "/usr/local/go/bin" ~/.bashrc || echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc; lastmessage="Go $L installed." ;;
        33) read -p "New Username: " nu; sudo adduser "$nu"; sudo usermod -aG sudo "$nu"; lastmessage="User $nu added to sudoers." ;;
        40) Android_Menu ;;
        41) if [ "$GPU_VENDOR" == "Nvidia" ]; then SpawnTerminal "watch -n 1 nvidia-smi" "Nvidia Monitor"; else if ! command -v radeontop &>/dev/null; then $INSTALL_CMD radeontop; fi; SpawnTerminal "sudo radeontop" "AMD/Intel Monitor"; fi ;;
        42) $INSTALL_CMD docker.io 2>/dev/null || $INSTALL_CMD docker ;;
        43) if ! command -v glmark2 &>/dev/null; then $INSTALL_CMD glmark2; fi; glmark2 ;;
        80) sudo reboot ;;
        99) exit 0 ;;
    esac
done