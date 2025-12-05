// State management
let state = {
    sessionId: null,
    currentChatId: null,
    currentModel: '',
    chats: [],
    uploadedFiles: [],
    models: []
};

// Initialize app
document.addEventListener('DOMContentLoaded', async () => {
    await initSession();
    await loadModels();
    await loadChats();
    setupEventListeners();
    updateParameterValues();
});

// Session Management
async function initSession() {
    // Check for existing session
    let sessionId = localStorage.getItem('laim_session_id');
    
    if (!sessionId) {
        // Create new session
        const response = await fetch('/api/session', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' }
        });
        const data = await response.json();
        sessionId = data.session_id;
        localStorage.setItem('laim_session_id', sessionId);
    }
    
    state.sessionId = sessionId;
    document.getElementById('sessionInfo').textContent = `Session: ${sessionId.substring(0, 8)}...`;
}

// Chat Management
async function loadChats() {
    try {
        const response = await fetch('/api/chats', {
            headers: { 'X-Session-ID': state.sessionId }
        });
        state.chats = await response.json() || [];
        renderChatList();
    } catch (error) {
        console.error('Error loading chats:', error);
        state.chats = [];
    }
}

function renderChatList() {
    const chatList = document.getElementById('chatList');
    
    if (state.chats.length === 0) {
        chatList.innerHTML = '<div style="text-align: center; color: var(--text-secondary); padding: 2rem;">No chats yet. Start a new one!</div>';
        return;
    }
    
    chatList.innerHTML = state.chats.map(chat => `
        <div class="chat-item ${chat.id === state.currentChatId ? 'active' : ''}" 
             onclick="loadChat('${chat.id}')">
            <div class="chat-item-title">${escapeHtml(chat.title)}</div>
            <div class="chat-item-meta">
                <span>${chat.model}</span>
                <span>${formatDate(chat.updated_at)}</span>
            </div>
            <button class="chat-item-delete" onclick="event.stopPropagation(); deleteChat('${chat.id}')">Delete</button>
        </div>
    `).join('');
}

async function createNewChat() {
    const model = state.currentModel || state.models[0]?.name || '';
    const title = `Chat ${new Date().toLocaleString()}`;
    
    try {
        const response = await fetch('/api/chats', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({ title, model })
        });
        
        const chat = await response.json();
        state.chats.unshift(chat);
        renderChatList();
        loadChat(chat.id);
    } catch (error) {
        console.error('Error creating chat:', error);
        alert('Failed to create new chat');
    }
}

async function loadChat(chatId) {
    state.currentChatId = chatId;
    
    try {
        const response = await fetch(`/api/chats/${chatId}`, {
            headers: { 'X-Session-ID': state.sessionId }
        });
        const messages = await response.json();
        
        const chat = state.chats.find(c => c.id === chatId);
        if (chat) {
            document.getElementById('chatTitle').textContent = chat.title;
            state.currentModel = chat.model;
            document.getElementById('modelSelect').value = chat.model;
        }
        
        renderMessages(messages);
        renderChatList();
    } catch (error) {
        console.error('Error loading chat:', error);
    }
}

async function deleteChat(chatId) {
    if (!confirm('Are you sure you want to delete this chat?')) return;
    
    try {
        await fetch(`/api/chats/${chatId}`, {
            method: 'DELETE',
            headers: { 'X-Session-ID': state.sessionId }
        });
        
        state.chats = state.chats.filter(c => c.id !== chatId);
        
        if (state.currentChatId === chatId) {
            state.currentChatId = null;
            document.getElementById('messagesContainer').innerHTML = `
                <div class="welcome-message">
                    <h2>Chat Deleted</h2>
                    <p>Start a new conversation</p>
                </div>
            `;
        }
        
        renderChatList();
    } catch (error) {
        console.error('Error deleting chat:', error);
        alert('Failed to delete chat');
    }
}

function renderMessages(messages) {
    const container = document.getElementById('messagesContainer');
    
    if (messages.length === 0) {
        container.innerHTML = `
            <div class="welcome-message">
                <h2>Start Chatting</h2>
                <p>Send a message to begin</p>
            </div>
        `;
        return;
    }
    
    container.innerHTML = messages.map(msg => {
        const filesHtml = msg.files && msg.files.length > 0 ? `
            <div class="message-files">
                ${msg.files.map(file => {
                    if (file.mime_type.startsWith('image/')) {
                        return `<div class="file-badge">
                            <img src="/api/files/${file.id}" alt="${escapeHtml(file.name)}">
                            ${escapeHtml(file.name)}
                        </div>`;
                    }
                    return `<div class="file-badge">📎 ${escapeHtml(file.name)}</div>`;
                }).join('')}
            </div>
        ` : '';
        
        return `
            <div class="message ${msg.role}">
                <div class="message-role">${msg.role}</div>
                <div class="message-content">${formatContent(msg.content)}</div>
                ${filesHtml}
                <div class="message-time">${formatDate(msg.created_at)}</div>
            </div>
        `;
    }).join('');
    
    container.scrollTop = container.scrollHeight;
}

// File Upload
async function handleFileUpload(files) {
    const formData = new FormData();
    
    for (let file of files) {
        formData.append('files', file);
    }
    
    try {
        const response = await fetch('/api/upload', {
            method: 'POST',
            body: formData
        });
        
        const uploadedFiles = await response.json();
        state.uploadedFiles.push(...uploadedFiles);
        renderFilePreview();
    } catch (error) {
        console.error('Error uploading files:', error);
        alert('Failed to upload files');
    }
}

function renderFilePreview() {
    const preview = document.getElementById('filePreview');
    
    if (state.uploadedFiles.length === 0) {
        preview.innerHTML = '';
        return;
    }
    
    preview.innerHTML = state.uploadedFiles.map((file, index) => {
        const isImage = file.mime_type.startsWith('image/');
        return `
            <div class="file-preview-item">
                ${isImage ? `<img src="/api/files/${file.id}" alt="${escapeHtml(file.name)}">` : '📎'}
                <span>${escapeHtml(file.name)}</span>
                <button class="file-remove" onclick="removeFile(${index})">×</button>
            </div>
        `;
    }).join('');
}

function removeFile(index) {
    state.uploadedFiles.splice(index, 1);
    renderFilePreview();
}

// Send Message
async function sendMessage() {
    const input = document.getElementById('userInput');
    const message = input.value.trim();
    
    if (!message) return;
    
    // Create chat if none exists
    if (!state.currentChatId) {
        await createNewChat();
    }
    
    const sendBtn = document.getElementById('sendBtn');
    sendBtn.disabled = true;
    sendBtn.textContent = 'Sending...';
    
    try {
        // Save user message
        const fileIds = state.uploadedFiles.map(f => f.id);
        const msgResponse = await fetch('/api/messages', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                chat_id: state.currentChatId,
                role: 'user',
                content: message,
                file_ids: fileIds
            })
        });
        
        input.value = '';
        state.uploadedFiles = [];
        renderFilePreview();
        
        // Reload messages to show user message
        await loadChat(state.currentChatId);
        
        // Get AI response
        const systemPrompt = document.getElementById('systemPrompt').value.trim();
        const options = {
            temperature: parseFloat(document.getElementById('temperature').value),
            top_p: parseFloat(document.getElementById('topP').value),
            num_predict: parseInt(document.getElementById('maxTokens').value)
        };
        
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                chat_id: state.currentChatId,
                model: state.currentModel,
                message: message,
                file_ids: fileIds,
                options: options
            })
        });
        
        // Stream response
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let assistantMessage = '';
        
        // Add assistant message placeholder
        const container = document.getElementById('messagesContainer');
        const messageDiv = document.createElement('div');
        messageDiv.className = 'message assistant';
        messageDiv.innerHTML = `
            <div class="message-role">assistant</div>
            <div class="message-content"></div>
        `;
        container.appendChild(messageDiv);
        const contentDiv = messageDiv.querySelector('.message-content');
        
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            
            const chunk = decoder.decode(value);
            const lines = chunk.split('\n');
            
            for (let line of lines) {
                if (line.trim()) {
                    try {
                        const data = JSON.parse(line);
                        if (data.message && data.message.content) {
                            assistantMessage += data.message.content;
                            contentDiv.innerHTML = formatContent(assistantMessage);
                            container.scrollTop = container.scrollHeight;
                        }
                    } catch (e) {
                        // Skip invalid JSON
                    }
                }
            }
        }
        
        // Save assistant message
        await fetch('/api/messages', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                chat_id: state.currentChatId,
                role: 'assistant',
                content: assistantMessage,
                file_ids: []
            })
        });
        
        // Reload chat list to update timestamp
        await loadChats();
        
    } catch (error) {
        console.error('Error sending message:', error);
        alert('Failed to send message');
    } finally {
        sendBtn.disabled = false;
        sendBtn.textContent = 'Send';
    }
}

// Model Management
async function loadModels() {
    try {
        const response = await fetch('/api/models');
        const data = await response.json();
        state.models = data.models || [];
        
        const select = document.getElementById('modelSelect');
        const deleteSelect = document.getElementById('deleteModelSelect');
        
        if (state.models.length === 0) {
            select.innerHTML = '<option value="">No models installed</option>';
            return;
        }
        
        select.innerHTML = state.models.map(model => 
            `<option value="${model.name}">${model.name}</option>`
        ).join('');
        
        deleteSelect.innerHTML = '<option value="">Select model to delete</option>' +
            state.models.map(model => 
                `<option value="${model.name}">${model.name}</option>`
            ).join('');
        
        if (!state.currentModel && state.models.length > 0) {
            state.currentModel = state.models[0].name;
            select.value = state.currentModel;
        }
        
        renderInstalledModels();
    } catch (error) {
        console.error('Error loading models:', error);
    }
}

function renderInstalledModels() {
    const container = document.getElementById('installedModels');
    
    if (state.models.length === 0) {
        container.innerHTML = '<p>No models installed. Pull a model to get started.</p>';
        return;
    }
    
    container.innerHTML = state.models.map(model => `
        <div class="model-item">
            <div>
                <strong>${model.name}</strong>
                <div style="font-size: 0.875rem; color: var(--text-secondary);">
                    Size: ${formatBytes(model.size)}
                </div>
            </div>
        </div>
    `).join('');
}

async function pullModel() {
    const modelName = document.getElementById('modelName').value.trim();
    if (!modelName) {
        alert('Please enter a model name');
        return;
    }
    
    const pullBtn = document.getElementById('pullBtn');
    const progress = document.getElementById('pullProgress');
    
    pullBtn.disabled = true;
    pullBtn.textContent = 'Pulling...';
    progress.innerHTML = 'Starting pull...';
    
    try {
        const response = await fetch('/api/pull', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: modelName, stream: true })
        });
        
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            
            const chunk = decoder.decode(value);
            const lines = chunk.split('\n');
            
            for (let line of lines) {
                if (line.trim()) {
                    try {
                        const data = JSON.parse(line);
                        progress.innerHTML += `\n${data.status || JSON.stringify(data)}`;
                        progress.scrollTop = progress.scrollHeight;
                    } catch (e) {
                        // Skip invalid JSON
                    }
                }
            }
        }
        
        progress.innerHTML += '\n\nPull completed!';
        await loadModels();
        
    } catch (error) {
        console.error('Error pulling model:', error);
        progress.innerHTML += `\n\nError: ${error.message}`;
    } finally {
        pullBtn.disabled = false;
        pullBtn.textContent = 'Pull Model';
    }
}

async function deleteModel() {
    const select = document.getElementById('deleteModelSelect');
    const modelName = select.value;
    
    if (!modelName) {
        alert('Please select a model to delete');
        return;
    }
    
    if (!confirm(`Are you sure you want to delete ${modelName}?`)) {
        return;
    }
    
    try {
        await fetch('/api/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: modelName })
        });
        
        await loadModels();
        alert('Model deleted successfully');
    } catch (error) {
        console.error('Error deleting model:', error);
        alert('Failed to delete model');
    }
}

// Generate Interface
async function generateText() {
    const prompt = document.getElementById('generatePrompt').value.trim();
    if (!prompt) return;
    
    const output = document.getElementById('generateOutput');
    const generateBtn = document.getElementById('generateBtn');
    
    generateBtn.disabled = true;
    generateBtn.textContent = 'Generating...';
    output.textContent = '';
    
    try {
        const response = await fetch('/api/generate', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                model: state.currentModel,
                prompt: prompt,
                stream: true
            })
        });
        
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let fullText = '';
        
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            
            const chunk = decoder.decode(value);
            const lines = chunk.split('\n');
            
            for (let line of lines) {
                if (line.trim()) {
                    try {
                        const data = JSON.parse(line);
                        if (data.response) {
                            fullText += data.response;
                            output.textContent = fullText;
                        }
                    } catch (e) {
                        // Skip invalid JSON
                    }
                }
            }
        }
    } catch (error) {
        console.error('Error generating text:', error);
        output.textContent = 'Error: ' + error.message;
    } finally {
        generateBtn.disabled = false;
        generateBtn.textContent = 'Generate Response';
    }
}

// Event Listeners
function setupEventListeners() {
    // New chat
    document.getElementById('newChatBtn').addEventListener('click', createNewChat);
    
    // Send message
    document.getElementById('sendBtn').addEventListener('click', sendMessage);
    document.getElementById('userInput').addEventListener('keypress', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            sendMessage();
        }
    });
    
    // File upload
    document.getElementById('attachBtn').addEventListener('click', () => {
        document.getElementById('fileInput').click();
    });
    
    document.getElementById('fileInput').addEventListener('change', (e) => {
        if (e.target.files.length > 0) {
            handleFileUpload(Array.from(e.target.files));
        }
    });
    
    // Model selection
    document.getElementById('modelSelect').addEventListener('change', (e) => {
        state.currentModel = e.target.value;
    });
    
    // API type switching
    document.getElementById('apiType').addEventListener('change', (e) => {
        document.querySelectorAll('.interface-panel').forEach(panel => {
            panel.classList.remove('active');
        });
        
        const selectedType = e.target.value;
        if (selectedType === 'chat') {
            document.getElementById('chatInterface').classList.add('active');
        } else if (selectedType === 'generate') {
            document.getElementById('generateInterface').classList.add('active');
        } else if (selectedType === 'models') {
            document.getElementById('modelsInterface').classList.add('active');
        }
    });
    
    // Generate
    document.getElementById('generateBtn').addEventListener('click', generateText);
    
    // Model management
    document.getElementById('refreshModels').addEventListener('click', loadModels);
    document.getElementById('pullBtn').addEventListener('click', pullModel);
    document.getElementById('deleteBtn').addEventListener('click', deleteModel);
    
    // Title editing
    document.getElementById('editTitleBtn').addEventListener('click', () => {
        if (!state.currentChatId) return;
        document.getElementById('titleModal').classList.add('active');
        const chat = state.chats.find(c => c.id === state.currentChatId);
        document.getElementById('titleInput').value = chat.title;
    });
    
    document.getElementById('saveTitleBtn').addEventListener('click', async () => {
        const newTitle = document.getElementById('titleInput').value.trim();
        if (!newTitle || !state.currentChatId) return;
        
        // Update in database (you'd need to add an endpoint for this)
        const chat = state.chats.find(c => c.id === state.currentChatId);
        if (chat) {
            chat.title = newTitle;
            document.getElementById('chatTitle').textContent = newTitle;
            renderChatList();
        }
        
        document.getElementById('titleModal').classList.remove('active');
    });
    
    document.getElementById('cancelTitleBtn').addEventListener('click', () => {
        document.getElementById('titleModal').classList.remove('active');
    });
    
    // Sidebar toggle
    document.getElementById('toggleSidebar').addEventListener('click', () => {
        document.getElementById('sidebar').classList.toggle('collapsed');
    });
    
    document.getElementById('mobileSidebarToggle').addEventListener('click', () => {
        document.getElementById('sidebar').classList.toggle('open');
    });
    
    // Parameter sliders
    ['temperature', 'topP', 'maxTokens'].forEach(id => {
        document.getElementById(id).addEventListener('input', updateParameterValues);
    });
}

function updateParameterValues() {
    document.getElementById('tempValue').textContent = 
        document.getElementById('temperature').value;
    document.getElementById('topPValue').textContent = 
        document.getElementById('topP').value;
    document.getElementById('maxTokensValue').textContent = 
        document.getElementById('maxTokens').value;
}

// Utility Functions
function formatContent(content) {
    // Basic markdown rendering
    content = escapeHtml(content);
    
    // Code blocks
    content = content.replace(/```(\w+)?\n([\s\S]*?)```/g, (match, lang, code) => {
        return `<pre><code class="language-${lang || 'text'}">${code}</code></pre>`;
    });
    
    // Inline code
    content = content.replace(/`([^`]+)`/g, '<code>$1</code>');
    
    // Bold
    content = content.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    
    // Italic
    content = content.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    
    // Line breaks
    content = content.replace(/\n/g, '<br>');
    
    return content;
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function formatDate(dateString) {
    const date = new Date(dateString);
    const now = new Date();
    const diff = now - date;
    
    if (diff < 60000) return 'Just now';
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
    if (diff < 604800000) return `${Math.floor(diff / 86400000)}d ago`;
    
    return date.toLocaleDateString();
}

function formatBytes(bytes) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
}