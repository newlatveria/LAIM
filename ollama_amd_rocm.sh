#!/bin/bash

# Colors for formatting
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Config variables
ROCM_VERSION="6.1.1" 
# We default to Jammy (22.04) as it's the most stable base for ROCm currently
AMDGPU_REPO_URL="https://repo.radeon.com/amdgpu-install/6.1.1/ubuntu/jammy/amdgpu-install_6.1.60101-1_all.deb"

# --- UTILITIES ---

# Restore OS ID if script is interrupted
restore_os_id() {
    if [ -f /etc/os-release.backup_ollama ]; then
        echo "Restoring original OS release file..."
        sudo mv /etc/os-release.backup_ollama /etc/os-release
    fi
}

# Ensure we clean up on exit or ctrl+c
trap restore_os_id EXIT

check_gpu() {
    echo -e "${BLUE}--- Checking for AMD GPU ---${NC}"
    if lspci | grep -i "amd" | grep -i "vga\|display" > /dev/null; then
        GPU_NAME=$(lspci | grep -i "amd" | grep -i "vga\|display" | cut -d ':' -f 3)
        echo -e "${GREEN}✅ AMD GPU Detected:${NC}$GPU_NAME"
    else
        echo -e "${RED}❌ No AMD GPU detected via lspci.${NC}"
    fi
    echo ""
}

check_rocm() {
    echo -e "${BLUE}--- Checking ROCm Installation ---${NC}"
    if command -v rocminfo &> /dev/null; then
        echo -e "${GREEN}✅ ROCm appears to be installed (rocminfo found).${NC}"
        rocminfo | head -n 10
    else
        echo -e "${YELLOW}⚠️ ROCm command 'rocminfo' not found.${NC}"
        echo -e "${YELLOW}Would you like to install ROCm libraries now?${NC}"
        read -p "Install ROCm? (y/n): " install_choice
        if [[ "$install_choice" == "y" || "$install_choice" == "Y" ]]; then
            install_rocm
        fi
    fi
    echo ""
}

install_rocm() {
    echo -e "${BLUE}--- Installing ROCm (Pop!_OS Fix Applied) ---${NC}"
    
    # 1. Download installer
    echo "Downloading AMD Installer..."
    wget -qO amdgpu-install.deb $AMDGPU_REPO_URL
    sudo apt install ./amdgpu-install.deb -y
    rm amdgpu-install.deb
    
    # 2. Apply Pop!_OS Spoof Fix
    echo -e "${YELLOW}Applying temporary OS spoof (Pop -> Ubuntu) to satisfy AMD installer...${NC}"
    sudo cp /etc/os-release /etc/os-release.backup_ollama
    
    # Edit the file in place to change ID=pop to ID=ubuntu
    sudo sed -i 's/^ID=pop/ID=ubuntu/' /etc/os-release
    sudo sed -i 's/^ID_LIKE=ubuntu debian/ID_LIKE=debian/' /etc/os-release
    
    echo "Updating repositories..."
    sudo apt update
    
    echo "Installing ROCm HIP libraries..."
    # Using --no-dkms to protect Pop!_OS kernel
    sudo amdgpu-install --usecase=rocm --no-dkms -y
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✅ ROCm installed successfully.${NC}"
    else
        echo -e "${RED}❌ ROCm installation failed.${NC}"
    fi
    
    # Restore immediately handled by function, but we call it here to be sure before continuing
    restore_os_id
    echo -e "${GREEN}✅ Original OS ID restored.${NC}"
}

check_permissions() {
    echo -e "${BLUE}--- Checking User Permissions ---${NC}"
    USER_GROUPS=$(groups $USER)
    MISSING_GROUPS=0
    
    if [[ $USER_GROUPS != *"render"* ]]; then
        echo -e "${YELLOW}⚠️ You are NOT in the 'render' group.${NC}"
        sudo usermod -aG render $USER
        echo "✅ Added $USER to 'render' group."
        MISSING_GROUPS=1
    else
        echo -e "${GREEN}✅ User is in 'render' group.${NC}"
    fi

    if [[ $USER_GROUPS != *"video"* ]]; then
        echo -e "${YELLOW}⚠️ You are NOT in the 'video' group.${NC}"
        sudo usermod -aG video $USER
        echo "✅ Added $USER to 'video' group."
        MISSING_GROUPS=1
    else
        echo -e "${GREEN}✅ User is in 'video' group.${NC}"
    fi
    
    if [ $MISSING_GROUPS -eq 1 ]; then
        echo -e "${RED}IMPORTANT: You must REBOOT for group changes to take effect!${NC}"
    fi
    echo ""
}

configure_override() {
    echo -e "${BLUE}--- Configuring GPU Override (HSA_OVERRIDE_GFX_VERSION) ---${NC}"
    echo "1) RDNA 3 (RX 7900/7800/7700/7600) -> Use 11.0.0"
    echo "2) RDNA 2 (RX 6900/6800/6700/6600) -> Use 10.3.0"
    echo "3) RDNA 1 (RX 5700/5600/5500)      -> Use 10.1.0"
    echo "4) Skip"
    
    read -p "Select your GPU architecture (1-4): " arch_choice
    
    case $arch_choice in
        1) OVERRIDE="11.0.0" ;;
        2) OVERRIDE="10.3.0" ;;
        3) OVERRIDE="10.1.0" ;;
        *) OVERRIDE="" ;;
    esac
    
    if [ ! -z "$OVERRIDE" ]; then
        sudo mkdir -p /etc/systemd/system/ollama.service.d
        echo "[Service]" | sudo tee /etc/systemd/system/ollama.service.d/override.conf > /dev/null
        echo "Environment=\"HSA_OVERRIDE_GFX_VERSION=$OVERRIDE\"" | sudo tee -a /etc/systemd/system/ollama.service.d/override.conf > /dev/null
        sudo systemctl daemon-reload
        echo -e "${GREEN}✅ Override configured.${NC}"
    fi
    echo ""
}

restart_ollama() {
    echo -e "${BLUE}--- Restarting Ollama ---${NC}"
    sudo systemctl restart ollama
    echo -e "${GREEN}✅ Ollama service restarted.${NC}"
    echo ""
}

monitor_gpu() {
    echo -e "${BLUE}--- Monitoring GPU Usage ---${NC}"
    watch -n 1 "rocm-smi --showusage --showmeminfo || echo 'rocm-smi not found'"
}

# Menu
while true; do
    clear
    echo "============================================="
    echo -e "   ${GREEN}Ollama AMD Fix (Pop!_OS Edition v2)${NC}"
    echo "============================================="
    echo "1. Check Hardware"
    echo "2. Install ROCm (With Pop!_OS Fix)"
    echo "3. Fix Permissions"
    echo "4. Configure HSA_OVERRIDE"
    echo "5. Restart Ollama"
    echo "6. Monitor GPU"
    echo "7. Exit"
    echo "---------------------------------------------"
    read -p "Choose: " option
    
    case $option in
        1) check_gpu; read -p "Press Enter..." ;;
        2) install_rocm; read -p "Press Enter..." ;;
        3) check_permissions; read -p "Press Enter..." ;;
        4) configure_override; read -p "Press Enter..." ;;
        5) restart_ollama; read -p "Press Enter..." ;;
        6) monitor_gpu ;;
        7) exit 0 ;;
        *) echo "Invalid"; sleep 1 ;;
    esac
done
