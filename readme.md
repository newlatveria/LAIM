🤖 LAIM: Local AI Management
Take Control of Your Local Intelligence.

LAIM (Local AI Management) is a high-performance orchestration tool built in Go 1.22 that empowers you to manage, deploy, and interact with Large AI Models (LAIMs) entirely on your own hardware, it provides a seamless bridge between raw local inference and a polished management experience.

🚀 Features
⚡ Go-Powered Performance: Built with Go 1.22 for minimal overhead and blazing-fast concurrency.

🌐 Web Dashboard: A built-in, lightweight web interface to monitor model health, resource usage, and active sessions.

🔒 Sovereign Privacy: 100% offline-first. Your data, your weights, your hardware—zero external API calls.

📦 Multi-Model Management: Effortlessly switch between Llama 3, Phi-4, Mistral, and more with a single command.

🔌 API Compatible: Drop-in compatibility for OpenAI-style endpoints, making it easy to integrate with your existing tools.

🛠️ Getting Started
Prerequisites
Go: Version 1.22 or higher.

Local Inference Engine: Designed to work best alongside Ollama.

Installation
Clone the repository and build the binary:

Bash

git clone https://github.com/newlatveria/LAIM.git
cd LAIM
go build -o laim main.go
Or install directly via Go:

Bash

go install github.com/newlatveria/LAIM@latest
📖 Usage Examples
1. Initialize the Environment
Launch the LAIM background service and the webolla management dashboard.

Bash

laim up
The dashboard will be available at http://localhost:8080 by default.

2. Pull and Manage Models
Download a new model directly through the LAIM interface:

Bash

laim pull llama3.2:3b
3. Run a Quick Inference
Test your model directly from the CLI:

Bash

laim chat "How can I optimize Go 1.22 code for high-concurrency AI workloads?"
4. Inspect Resource Allocation
Monitor how much VRAM and CPU your local models are currently consuming:

Bash

laim status
🏗️ Architecture
LAIM is architected for modularity. The webolla module handles the heavy lifting of web-based communication and model orchestration, ensuring that the core application remains lightweight and extensible.

LAIM (CLI/Core)
 └── webolla (Web Engine & Orchestrator)
      └── Local Inference (Ollama / GGUF)
🤝 Contributing
Welcome to the New Latveria collective. We welcome contributions to help make local AI more accessible and powerful.

Fork the repo.

Create your feature branch (git checkout -b feature/AmazingFeature).

Commit your changes (git commit -m 'Add some AmazingFeature').

Push to the branch (git push origin feature/AmazingFeature).

Open a Pull Request.

📜 License
Distributed under the MIT License. See LICENSE for more information.
