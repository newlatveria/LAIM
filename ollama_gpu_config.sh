#!/bin/bash

# Configuration
OLLAMA_INSTALL_SCRIPT="https://ollama.com/install.sh"
OLLAMA_SERVICE_OVERRIDE="/etc/systemd/system/ollama.service.d/override.conf"

# --- Utility Functions ---

# Function to display the menu
show_menu() {
    echo "--- ⚙️ Ollama GPU Check & Setup Menu ---"
    echo "1) 🔍 Check GPU Type and Dependencies"
    echo "2) 🟢 Verify Ollama GPU Usage (Run a test model)"
    echo "3) 🛠️ Attempt Automated GPU Setup/Reinstall Ollama"
    echo "4) ⚙️ Advanced AMD (RX 570) Setup (ROCm GFX Override)"
    echo "5) 📁 Change Ollama Model Location" # New Option
    echo "6) 🚪 Exit"
    echo "----------------------------------------"
}

# Function to check for specific commands
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# --- New Model Location Function ---

change_model_location() {
    echo "--- Change Ollama Model Location ---"
    read -rp "Enter the FULL PATH to the new models directory (e.g., /mnt/ssd/ollama-models): " NEW_MODEL_PATH

    # 1. Path Validation
    if [ -z "$NEW_MODEL_PATH" ]; then
        echo "❌ Path cannot be empty. Aborting."
        return 1
    fi
    if ! sudo mkdir -p "$NEW_MODEL_PATH"; then
        echo "❌ Failed to create directory $NEW_MODEL_PATH. Check permissions or path existence."
        return 1
    fi

    echo "✅ Directory created/verified: $NEW_MODEL_PATH"

    # 2. Set Permissions
    echo "Ensuring 'ollama' user has correct ownership..."
    if ! sudo chown -R ollama:ollama "$NEW_MODEL_PATH"; then
        echo "⚠️ Warning: Failed to set ownership for the 'ollama' user/group. This may cause read/write errors."
        echo "Please ensure the 'ollama' user exists and has permissions manually."
    fi

    # 3. Create/Update Systemd Override
    echo "Writing OLLAMA_MODELS environment variable to systemd service override..."
    
    # Create the drop-in directory if it doesn't exist
    sudo mkdir -p /etc/systemd/system/ollama.service.d/

    # Write the override configuration to the file
    cat <<EOF | sudo tee "$OLLAMA_SERVICE_OVERRIDE" > /dev/null
[Service]
Environment="OLLAMA_MODELS=$NEW_MODEL_PATH"
EOF
    
    echo "✅ Service override file created/updated at: $OLLAMA_SERVICE_OVERRIDE"

    # 4. Apply Changes
    echo "Reloading systemd daemon and restarting Ollama service..."
    if command_exists systemctl; then
        sudo systemctl daemon-reload
        sudo systemctl restart ollama
        echo "✅ Ollama service restarted. New models will be downloaded to $NEW_MODEL_PATH."
        echo "   (You may need to manually move any existing models from the old path.)"
    else
        echo "❌ systemctl command not found. Cannot restart service automatically."
    fi
}

# --- Placeholder Functions (as defined in previous response) ---

detect_gpu() {
    echo "Detecting installed GPU hardware..."
    # ... (rest of the detect_gpu function from previous response)
}

verify_gpu_usage() {
    echo "Running model test to verify GPU usage..."
    # ... (rest of the verify_gpu_usage function from previous response)
}

install_dependencies() {
    echo "Installing/reinstalling dependencies for Ollama..."
    # ... (rest of the install_dependencies function from previous response)
}

reinstall_ollama() {
    echo "Running the official Ollama install script..."
    # ... (rest of the reinstall_ollama function from previous response)
}

advanced_amd_setup() {
    echo "Applying GFX override for older AMD cards..."
    # ... (Implementation for Option 4 from previous response would go here)
}


# --- Main Logic ---

while true; do
    show_menu
    read -rp "Enter choice [1-6]: " choice
    echo

    case $choice in
        1)
            detect_gpu
            ;;
        2)
            detect_gpu
            verify_gpu_usage
            ;;
        3)
            detect_gpu
            if [[ "$GPU_TYPE" == "NVIDIA" || "$GPU_TYPE" == "AMD" ]]; then
                echo "--- Starting Automatic Setup/Reinstallation ---"
                install_dependencies
                reinstall_ollama
                echo "--- Setup Complete. Please verify GPU usage (Option 2) ---"
            else
                echo "Cannot proceed with automatic setup: No supported NVIDIA or AMD GPU was reliably detected."
            fi
            ;;
        4)
            advanced_amd_setup
            ;;
        5)
            change_model_location
            ;;
        6)
            echo "Exiting script. Goodbye!"
            exit 0
            ;;
        *)
            echo "Invalid option. Please enter a number between 1 and 6."
            ;;
    esac
    echo
done