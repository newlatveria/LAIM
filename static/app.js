// --- Globals & Setup ---
const elements = {
    darkModeToggle: document.getElementById('dark-mode-toggle'),
    apiTypeSelect: document.getElementById('api-type-select'),
    modelSelect: document.getElementById('model-select'),
    promptInput: document.getElementById('prompt-input'),
    generateButton: document.getElementById('generate-button'),
    stopGenerateButton: document.getElementById('stop-generate-button'),
    responseOutput: document.getElementById('response-output'),
    chatInput: document.getElementById('chat-input'),
    sendChatButton: document.getElementById('send-chat-button'),
    stopChatButton: document.getElementById('stop-chat-button'),
    chatHistoryOutput: document.getElementById('chat-history-output'),
    loadingIndicator: document.getElementById('loading-indicator'),
    unifiedResponseOutput: document.getElementById('unified-response-output'),
    modelActionSelect: document.getElementById('model-action-select'),
    modelActionOutput: document.getElementById('model-action-output'),
    availableModelSelect: document.getElementById('available-model-select'),
    // Sections
    generateSection: document.getElementById('generate-section'),
    chatSection: document.getElementById('chat-section'),
    modelMgmtSection: document.getElementById('model-management-section'),
    settingsContainer: document.getElementById('advanced-settings-container'),
    modelSelectContainer: document.getElementById('common-model-select-container')
};

let currentReader = null;
let chatMessages = [];

// --- Dark Mode ---
if (localStorage.getItem('darkMode') === 'true') {
    document.body.classList.add('dark-mode');
    document.body.classList.remove('light-mode');
    elements.darkModeToggle.textContent = '☀️ Light Mode';
}
elements.darkModeToggle.addEventListener('click', () => {
    document.body.classList.toggle('dark-mode');
    document.body.classList.toggle('light-mode');
    const isDark = document.body.classList.contains('dark-mode');
    elements.darkModeToggle.textContent = isDark ? '☀️ Light Mode' : '🌙 Dark Mode';
    localStorage.setItem('darkMode', isDark);
});

// --- Tabs / Navigation ---
elements.apiTypeSelect.addEventListener('change', (e) => {
    const val = e.target.value;
    elements.generateSection.classList.add('hidden');
    elements.chatSection.classList.add('hidden');
    elements.modelMgmtSection.classList.add('hidden');
    
    // Hide common controls for model management
    if(val === 'model-management') {
        elements.modelMgmtSection.classList.remove('hidden');
        elements.settingsContainer.classList.add('hidden');
        elements.modelSelectContainer.classList.add('hidden');
        elements.unifiedResponseOutput.classList.add('hidden');
    } else {
        elements.settingsContainer.classList.remove('hidden');
        elements.modelSelectContainer.classList.remove('hidden');
        elements.unifiedResponseOutput.classList.remove('hidden');
        if(val === 'generate') elements.generateSection.classList.remove('hidden');
        if(val === 'chat') elements.chatSection.classList.remove('hidden');
    }
});

// --- API Interaction Helper ---
async function streamResponse(endpoint, payload, onChunk, onDone) {
    try {
        const response = await fetch(endpoint, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (!response.ok) throw new Error(await response.text());

        const reader = response.body.getReader();
        currentReader = reader;
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop();

            for (const line of lines) {
                if (line.startsWith('data: ')) {
                    const data = line.slice(6);
                    if (data === '[DONE]') break;
                    try {
                        onChunk(JSON.parse(data));
                    } catch (e) { console.error('Parse error', e); }
                }
            }
        }
    } catch (err) {
        if (err.name !== 'AbortError') alert("Error: " + err.message);
    } finally {
        currentReader = null;
        onDone();
    }
}

// --- Logic: Generate ---
elements.generateButton.addEventListener('click', async () => {
    const prompt = elements.promptInput.value.trim();
    if (!prompt) return alert('Enter a prompt');
    
    toggleLoading(true, elements.generateButton, elements.stopGenerateButton);
    elements.responseOutput.textContent = '';
    let fullText = '';

    await streamResponse('/api/ollama-action', {
        actionType: 'generate',
        model: elements.modelSelect.value,
        prompt: prompt,
        options: getSettings()
    }, (chunk) => {
        if (chunk.response) {
            fullText += chunk.response;
            elements.responseOutput.innerHTML = marked.parse(fullText);
        }
    }, () => toggleLoading(false, elements.generateButton, elements.stopGenerateButton));
});

// --- Logic: Chat ---
elements.sendChatButton.addEventListener('click', async () => {
    const text = elements.chatInput.value.trim();
    if (!text) return;

    // UI Update
    addMessage('user', text);
    elements.chatInput.value = '';
    toggleLoading(true, elements.sendChatButton, elements.stopChatButton);

    // Build History
    const msgs = chatMessages.map(m => ({role: m.role, content: m.content}));
    msgs.push({ role: 'user', content: text });
    
    // Check for System Prompt
    const sysPrompt = document.getElementById('system-prompt-input').value.trim();
    if(sysPrompt && msgs.length === 1) msgs.unshift({ role: 'system', content: sysPrompt });

    let botResponse = '';
    const botMsgDiv = addMessage('assistant', '...'); // Placeholder

    await streamResponse('/api/ollama-action', {
        actionType: 'chat',
        model: elements.modelSelect.value,
        messages: msgs
    }, (chunk) => {
        if (chunk.message && chunk.message.content) {
            botResponse += chunk.message.content;
            botMsgDiv.innerHTML = marked.parse(botResponse);
            // Auto scroll
            elements.chatHistoryOutput.scrollTop = elements.chatHistoryOutput.scrollHeight;
        }
    }, () => {
        chatMessages.push({role: 'user', content: text});
        chatMessages.push({role: 'assistant', content: botResponse});
        toggleLoading(false, elements.sendChatButton, elements.stopChatButton);
    });
});

// --- Logic: Model Management ---
async function loadModels() {
    try {
        const res = await fetch('/api/models');
        const data = await res.json();
        elements.modelSelect.innerHTML = '';
        elements.modelActionSelect.innerHTML = '';
        
        if(data.models) {
            data.models.forEach(m => {
                const opt1 = new Option(m.name, m.name);
                const opt2 = new Option(m.name, m.name);
                elements.modelSelect.add(opt1);
                elements.modelActionSelect.add(opt2);
            });
        }
    } catch(e) { console.error("Could not load models", e); }
}

document.getElementById('refresh-models-button').addEventListener('click', loadModels);

// --- Utilities ---
function addMessage(role, text) {
    const div = document.createElement('div');
    div.className = `chat-message ${role}`;
    div.innerHTML = role === 'user' ? text : marked.parse(text);
    elements.chatHistoryOutput.appendChild(div);
    return div;
}

function toggleLoading(isLoading, startBtn, stopBtn) {
    elements.loadingIndicator.style.display = isLoading ? 'block' : 'none';
    if(isLoading) {
        startBtn.classList.add('hidden');
        stopBtn.classList.remove('hidden');
        elements.modelSelect.disabled = true;
    } else {
        startBtn.classList.remove('hidden');
        stopBtn.classList.add('hidden');
        elements.modelSelect.disabled = false;
    }
}

function getSettings() {
    return {
        temperature: parseFloat(document.getElementById('temperature-slider').value),
        num_predict: parseInt(document.getElementById('max-tokens-slider').value),
        top_p: parseFloat(document.getElementById('top-p-slider').value)
    };
}

// Sliders UI
['temperature', 'top-p', 'max-tokens'].forEach(id => {
    const slider = document.getElementById(`${id}-slider`);
    const label = document.getElementById(`${id}-value`);
    slider.addEventListener('input', () => label.textContent = slider.value);
});

// Stop Buttons
[elements.stopGenerateButton, elements.stopChatButton].forEach(btn => {
    btn.addEventListener('click', () => {
        if(currentReader) currentReader.cancel();
    });
});

// Init
document.addEventListener('DOMContentLoaded', loadModels);

// Export Chat
document.getElementById('export-chat-button').addEventListener('click', () => {
    if(chatMessages.length === 0) return alert("Empty chat");
    let md = `# Chat Export - ${new Date().toLocaleString()}\n\n`;
    chatMessages.forEach(m => md += `### ${m.role.toUpperCase()}\n${m.content}\n\n`);
    
    const blob = new Blob([md], {type: 'text/markdown'});
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `chat-${Date.now()}.md`;
    a.click();
});

// Clear Chat
document.getElementById('clear-chat-button').addEventListener('click', () => {
    if(confirm("Clear history?")) {
        chatMessages = [];
        elements.chatHistoryOutput.innerHTML = '';
    }
});

// Hardcoded available models for the "Pull" dropdown
const commonModels = ["llama2", "mistral", "codellama", "dolphin-phi", "neural-chat", "starling-lm", "orca-mini"];
const availSelect = document.getElementById('available-model-select');
commonModels.forEach(m => availSelect.add(new Option(m, m)));

document.getElementById('pull-available-model-button').addEventListener('click', () => performModelAction('pull', availSelect.value));
document.getElementById('pull-manual-model-button').addEventListener('click', () => performModelAction('pull', document.getElementById('model-action-input').value));
document.getElementById('delete-model-button').addEventListener('click', () => performModelAction('delete', document.getElementById('model-action-input').value || elements.modelActionSelect.value));

async function performModelAction(type, name) {
    if(!name) return alert("No model name specified");
    if(type === 'delete' && !confirm(`Delete ${name}?`)) return;

    elements.modelActionOutput.textContent = `Processing ${type} for ${name}...`;
    try {
        const res = await fetch('/api/ollama-action', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({ actionType: type, model: name })
        });
        const txt = await res.text();
        elements.modelActionOutput.textContent = txt;
        loadModels(); // Refresh list after action
    } catch(e) {
        elements.modelActionOutput.textContent = "Error: " + e.message;
    }
}
