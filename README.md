# 🧠 LAIM (Local AI Management)

## 🚀 The Ultimate Desktop Hub for Ollama

**LAIM** is a high-performance, single-file Go application that delivers a comprehensive web interface for managing and interacting with your local Ollama instance. Experience real-time LLM output, seamless model switching, and powerful local AI administration, all from a lightweight, beautifully themed web UI. **No Python. No Node. Just Go, HTML, and pure speed.**

-----

## ✨ Features & Use Cases

LAIM is engineered to be your one-stop solution for local Large Language Models (LLMs).

| Feature | Core Functionality | Use Case Scenario |
| :--- | :--- | :--- |
| **Real-Time Streaming** | Uses Go's `http.Flusher` to push token chunks from Ollama to the browser immediately, minimizing perceived latency. | **Developer Workflow:** You're asking for a complex regular expression or a 50-line code function. Watch the code blocks stream instantly, allowing for quicker review and integration. |
| **Full Chat (`/api/chat`)** | Dedicated interface supporting multi-turn conversations while automatically maintaining context and history. | **Technical Consulting:** Maintain a lengthy, nuanced discussion with a model like `Mixtral` or `Llama2` on complex architecture design, ensuring every new question builds on previous context. |
| **System Prompt Injection** | Provides a dedicated input for defining the LLM's role, rules, and personality before the chat begins. | **Custom Persona:** Set the system prompt to `"You are a Senior Security Analyst who only answers in rhyming couplets."` to enforce a specific style or expertise. |
| **Model Administration** | Directly lists installed models and provides web controls to **Pull** new models from the registry or **Delete** unused models. | **Local Hardware Optimization:** Quickly delete an older, 13B parameter model to free up GPU/RAM resources for a newly pulled, higher-performing 7B model. |
| **Advanced Parameters** | Easily control generation with sliders for **Temperature**, **Top P**, and **Max Tokens (Num Predict)**. | **Creative Writing vs. Coding:** Set **Temperature** to `1.5` for brainstorming poem structures, then drop it to `0.2` and set **Top P** to `0.95` for generating deterministic JSON payloads. |
| **Markdown Rendering** | Converts the LLM's Markdown output (lists, headers, bold text, and especially code blocks) into rich HTML on the fly. | **Documentation Generation:** Request a detailed plan, and review the output with perfectly highlighted code snippets and well-structured headings, making it instantly readable. |
| **Export & History** | Allows exporting the entire chat history as a portable Markdown file (`.md`) and a one-click history clear. | **Collaboration:** Export a successful troubleshooting chat thread with your LLM to share with a teammate or save for a project log. |

-----

## ⚙️ Prerequisites

To run LAIM, you need the following running and installed:

1.  **Go Language:** Version 1.18 or higher.
      * [Go Installation Guide](https://go.dev/doc/install)
2.  **Ollama:** The Ollama server must be running and accessible on its default port, `http://localhost:11434`.
      * [Ollama Installation Guide](https://ollama.com/download)
3.  **Installed Model:** You must have at least one model pulled (e.g., `ollama pull mistral`) for the chat/generate features to work.

-----

## 📦 Installation (From GitHub)

Follow these steps to clone the repository and build the LAIM executable.

### **1. Clone the Repository**

Assuming your source file is the main entry point:

```bash
# Clone the repository (replace with the actual GitHub URL if necessary)
git clone https://github.com/your-username/laim-ollama-web.git
cd laim-ollama-web

# Rename the file to the project's standard entry point
mv ollama-web_1.go main.go
```

### **2. Build the Executable**

Compile the single Go file into a standalone, optimized binary.

```bash
# Compiles into a binary named 'laim'
go build -o laim ./main.go
```

### **3. Set Execution Permissions**

Ensure the compiled binary can be executed:

```bash
chmod +x laim
```

-----

## 🚀 Usage

### **1. Start the LAIM Server**

Execute the compiled binary from your terminal:

```bash
./laim
```

The server will log its status:

```
Server starting on http://localhost:8080
```

### **2. Access the Web UI**

Open your web browser and navigate to:

🌐 **[http://localhost:8080](https://www.google.com/search?q=http://localhost:8080)**

### **3. Optional: Change the Listening Port**

You can easily change the port by setting the `PORT` environment variable before launching the server:

```bash
# Run LAIM on port 9000
PORT=9000 ./laim
```

### **4. Using the Interface**

  * **Switch Mode:** Use the **"Select API Type"** dropdown to toggle between `Generate Text`, `Chat`, and `Model Management`.
  * **Generate Text:** Select your model, input a single prompt, and click **Generate Response**. Ideal for simple questions or one-off tasks.
  * **Chat:** Set an optional **System Prompt** for context, and start your conversation. Each message is added to the history, ensuring the model remembers previous turns.
  * **Model Management:**
      * Click **Refresh Installed Models List** to verify what's available.
      * Select a model from the **Available Models** list and click **Pull Selected Model** to download it.
      * Use the input box to manually enter a model name (e.g., `llama3`) for pulling or select an installed model to **Delete**.
