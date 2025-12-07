#!/bin/bash

# Configuration
GO_VERSION="1.21" # Recommended Go version
OLLAMA_PORT="11434"

# --- Functions ---

check_dependency() {
    local cmd=$1
    local name=$2
    if command -v "$cmd" &> /dev/null; then
        echo -e "  ✅ $name is installed."
        return 0
    else
        echo -e "  ❌ $name not found."
        return 1
    fi
}

install_go() {
    echo -e "\n--- Installing Go (Version ${GO_VERSION} or newer) ---"
    # This is a simplified install; the best method varies by OS.
    # On Debian/Ubuntu:
    # sudo apt update && sudo apt install -y golang
    echo "Please visit https://go.dev/doc/install for the recommended installation method for your OS."
    read -p "Press Enter to continue after installing Go manually..."
    check_dependency "go" "Go"
}

check_go_module() {
    # Check if necessary Go modules are present
    echo -e "\n--- Checking Go Modules ---"
    if [ -f go.mod ]; then
        echo "  ✅ go.mod file found."
    else
        echo "  ❌ go.mod not found. Initializing module..."
        go mod init go-ollama-processor
    fi

    echo "  Installing required dependencies..."
    go get github.com/ollama/ollama/api
    go get code.sajari.com/docconv
    if [ $? -eq 0 ]; then
        echo -e "  ✅ Go dependencies installed successfully."
    else
        echo -e "  ❌ Failed to install Go dependencies. Check your Go installation."
    fi
}

install_ollama() {
    echo -e "\n--- Installing and Running Ollama ---"
    if check_dependency "ollama" "Ollama"; then
        echo "  Ollama is already installed."
    else
        echo "  Installing Ollama via standard script..."
        curl -fsSL https://ollama.com/install.sh | sh
    fi

    # Check if Ollama is running on the default port
    if curl -s http://localhost:${OLLAMA_PORT}/ &> /dev/null; then
        echo "  ✅ Ollama server is running."
    else
        echo "  ❌ Ollama server not detected on port ${OLLAMA_PORT}. Please start it manually."
    fi

    # Pull required models
    echo -e "\n  Pulling required models (llama3 for text, llava for images)..."
    ollama pull llama3
    ollama pull llava
    
    if [ $? -eq 0 ]; then
        echo -e "  ✅ Required models (llama3, llava) are available."
    else
        echo -e "  ❌ Failed to pull models. Check network connection or Ollama status."
    fi
}

run_server() {
    echo -e "\n--- Running the Go Web Server ---"
    if check_dependency "go" "Go"; then
        echo "  Starting server on http://localhost:8080..."
        go run main.go
    else
        echo "  ❌ Cannot run. Go is not installed."
    fi
}

# --- Main Menu Logic ---
while true; do
    echo -e "\n======================================="
    echo "       Ollama Processor Setup Menu"
    echo "======================================="
    echo "1. Check/Install Go"
    echo "2. Install/Update Go Dependencies (go mod)"
    echo "3. Check/Install Ollama & Pull Models (llama3, llava)"
    echo "4. Run the Web Server"
    echo "0. Exit"
    echo "---------------------------------------"
    read -p "Enter your choice: " choice

    case $choice in
        1) install_go ;;
        2) check_go_module ;;
        3) install_ollama ;;
        4) run_server ;;
        0) echo "Exiting setup script. Goodbye!"; break ;;
        *) echo "Invalid option. Please try again." ;;
    esac
done
