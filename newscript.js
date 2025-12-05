// State management
let state = {
    sessionId: null,
    currentChatId: null,
    currentModel: '',
    chats: [],
    uploadedFiles: [],
    models: [],
    isGenerating: false
};

// Initialize app
document.addEventListener('DOMContentLoaded', async () => {
    await initSession();
    await loadModels();
    await loadChats();
    setupEventListeners();
    updateParameterValues();
    loadDraft();
    setupKeyboardShortcuts();
});

// Session Management
async function initSession() {
    let sessionId = localStorage.getItem('laim_session_id');
    
    if (!sessionId) {
        try {
            const response = await fetch('/api/session', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' }
            });
            const data = await response.json();
            sessionId = data.session_id;
            localStorage.setItem('laim_session_id', sessionId);
        } catch (err) {
        console.error('Failed to copy:', err);
        showToast('Failed to copy', 'error');
    }
}or) {
            console.error('Error creating session:', error);
            showToast('Failed to create session', 'error');
            return;
        }
    }
    
    state.sessionId = sessionId;
    document.getElementById('sessionInfo').textContent = `Session: ${sessionId.substring(0, 8)}...`;
}

// Chat Management
async function loadChats() {
    showLoadingState('chatList');
    
    try {
        const response = await fetch('/api/chats', {
            headers: { 'X-Session-ID': state.sessionId }
        });
        
        if (!response.ok) {
            throw new Error('Failed to load chats');
        }
        
        state.chats = await response.json() || [];
        renderChatList();
    } catch (error) {
        console.error('Error loading chats:', error);
        showToast('Failed to load chats', 'error');
        state.chats = [];
        renderChatList();
    }
}

function renderChatList() {
    const chatList = document.getElementById('chatList');
    
    if (state.chats.length === 0) {
        chatList.innerHTML = `
            <div style="text-align: center; color: var(--text-secondary); padding: 2rem;">
                <p>No chats yet</p>
                <p style="font-size: 0.875rem; margin-top: 0.5rem;">Start a new conversation!</p>
            </div>
        `;
        return;
    }
    
    chatList.innerHTML = state.chats.map(chat => `
        <div class="chat-item ${chat.id === state.currentChatId ? 'active' : ''}" 
             onclick="loadChat('${chat.id}')">
            <div class="chat-item-title">${escapeHtml(chat.title)}</div>
            <div class="chat-item-meta">
                <span>${escapeHtml(chat.model)}</span>
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
        
        if (!response.ok) {
            throw new Error('Failed to create chat');
        }
        
        const chat = await response.json();
        state.chats.unshift(chat);
        renderChatList();
        loadChat(chat.id);
        showToast('New chat created', 'success');
    } catch (error) {
        console.error('Error creating chat:', error);
        showToast('Failed to create new chat', 'error');
    }
}

async function loadChat(chatId) {
    state.currentChatId = chatId;
    showLoadingState('messagesContainer');
    
    try {
        const response = await fetch(`/api/chats/${chatId}`, {
            headers: { 'X-Session-ID': state.sessionId }
        });
        
        if (!response.ok) {
            throw new Error('Failed to load chat');
        }
        
        const messages = await response.json();
        
        const chat = state.chats.find(c => c.id === chatId);
        if (chat) {
            document.getElementById('chatTitle').textContent = chat.title;
            state.currentModel = chat.model;
            document.getElementById('modelSelect').value = chat.model;
        }
        
        renderMessages(messages);
        renderChatList();
        loadDraft();
    } catch (error) {
        console.error('Error loading chat:', error);
        showToast('Failed to load chat', 'error');
    }
}

async function deleteChat(chatId) {
    if (!confirm('Are you sure you want to delete this chat?')) return;
    
    try {
        const response = await fetch(`/api/chats/${chatId}`, {
            method: 'DELETE',
            headers: { 'X-Session-ID': state.sessionId }
        });
        
        if (!response.ok) {
            throw new Error('Failed to delete chat');
        }
        
        state.chats = state.chats.filter(c => c.id !== chatId);
        
        if (state.currentChatId === chatId) {
            state.currentChatId = null;
            document.getElementById('messagesContainer').innerHTML = `
                <div class="welcome-message">
                    <h2>Chat Deleted</h2>
                    <p>Start a new conversation</p>
                </div>
            `;
            clearDraft();
        }
        
        renderChatList();
        showToast('Chat deleted', 'success');
    } catch (error) {
        console.error('Error deleting chat:', error);
        showToast('Failed to delete chat', 'error');
    }
}

async function updateChatTitle(chatId, newTitle) {
    try {
        const response = await fetch(`/api/chats/${chatId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({ title: newTitle })
        });
        
        if (!response.ok) {
            throw new Error('Failed to update title');
        }
        
        const chat = state.chats.find(c => c.id === chatId);
        if (chat) {
            chat.title = newTitle;
            document.getElementById('chatTitle').textContent = newTitle;
            renderChatList();
        }
        
        showToast('Title updated', 'success');
    } catch (error) {
        console.error('Error updating title:', error);
        showToast('Failed to update title', 'error');
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
                            <img src="/api/files/${file.id}" alt="${escapeHtml(file.name)}" loading="lazy">
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
                <div class="message-actions">
                    <button class="btn-icon-small" onclick="copyToClipboard(\`${escapeHtml(msg.content).replace(/`/g, '\\`')}\`)" title="Copy">📋</button>
                    <span class="message-time">${formatDate(msg.created_at)}</span>
                </div>
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
    
    showToast('Uploading files...', 'info');
    
    try {
        const response = await fetch('/api/upload', {
            method: 'POST',
            headers: { 'X-Session-ID': state.sessionId },
            body: formData
        });
        
        if (!response.ok) {
            throw new Error('Upload failed');
        }
        
        const uploadedFiles = await response.json();
        state.uploadedFiles.push(...uploadedFiles);
        renderFilePreview();
        showToast(`${uploadedFiles.length} file(s) uploaded`, 'success');
    } catch (error) {
        console.error('Error uploading files:', error);
        showToast('Failed to upload files', 'error');
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
                ${isImage ? `<img src="/api/files/${file.id}" alt="${escapeHtml(file.name)}" loading="lazy">` : '📎'}
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
    
    if (!message || state.isGenerating) return;
    
    if (!state.currentChatId) {
        await createNewChat();
    }
    
    const sendBtn = document.getElementById('sendBtn');
    state.isGenerating = true;
    sendBtn.disabled = true;
    sendBtn.textContent = 'Sending...';
    
    try {
        const fileIds = state.uploadedFiles.map(f => f.id);
        const msgResponse = await fetch('/api/messages', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({
                chat_id: state.currentChatId,
                role: 'user',
                content: message,
                file_ids: fileIds
            })
        });
        
        if (!msgResponse.ok) {
            throw new Error('Failed to save message');
        }
        
        input.value = '';
        state.uploadedFiles = [];
        renderFilePreview();
        clearDraft();
        
        await loadChat(state.currentChatId);
        
        const options = {
            temperature: parseFloat(document.getElementById('temperature').value),
            top_p: parseFloat(document.getElementById('topP').value),
            num_predict: parseInt(document.getElementById('maxTokens').value)
        };
        
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({
                chat_id: state.currentChatId,
                model: state.currentModel,
                message: message,
                file_ids: fileIds,
                options: options
            })
        });
        
        if (!response.ok) {
            throw new Error('Failed to get AI response');
        }
        
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let assistantMessage = '';
        
        const container = document.getElementById('messagesContainer');
        const messageDiv = document.createElement('div');
        messageDiv.className = 'message assistant';
        messageDiv.innerHTML = `
            <div class="message-role">assistant</div>
            <div class="message-content"><span class="typing-indicator">●●●</span></div>
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
        
        await fetch('/api/messages', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({
                chat_id: state.currentChatId,
                role: 'assistant',
                content: assistantMessage,
                file_ids: []
            })
        });
        
        await loadChats();
        
    } catch (error) {
        console.error('Error sending message:', error);
        showToast('Failed to send message', 'error');
    } finally {
        state.isGenerating = false;
        sendBtn.disabled = false;
        sendBtn.textContent = 'Send';
    }
}

// Model Management
async function loadModels() {
    try {
        const response = await fetch('/api/models');
        if (!response.ok) {
            throw new Error('Failed to load models');
        }
        
        const data = await response.json();
        state.models = data.models || [];
        
        const select = document.getElementById('modelSelect');
        const deleteSelect = document.getElementById('deleteModelSelect');
        
        if (state.models.length === 0) {
            select.innerHTML = '<option value="">No models installed</option>';
            showToast('No models installed. Pull a model to get started.', 'warning');
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
        showToast('Failed to load models', 'error');
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
                <strong>${escapeHtml(model.name)}</strong>
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
        showToast('Please enter a model name', 'warning');
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
            headers: {
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({ name: modelName, stream: true })
        });
        
        if (!response.ok) {
            throw new Error('Pull failed');
        }
        
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
        showToast('Model pulled successfully', 'success');
        
    } catch (error) {
        console.error('Error pulling model:', error);
        progress.innerHTML += `\n\nError: ${error.message}`;
        showToast('Failed to pull model', 'error');
    } finally {
        pullBtn.disabled = false;
        pullBtn.textContent = 'Pull Model';
    }
}

async function deleteModel() {
    const select = document.getElementById('deleteModelSelect');
    const modelName = select.value;
    
    if (!modelName) {
        showToast('Please select a model to delete', 'warning');
        return;
    }
    
    if (!confirm(`Are you sure you want to delete ${modelName}?`)) {
        return;
    }
    
    try {
        const response = await fetch('/api/delete', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({ name: modelName })
        });
        
        if (!response.ok) {
            throw new Error('Delete failed');
        }
        
        await loadModels();
        showToast('Model deleted successfully', 'success');
    } catch (error) {
        console.error('Error deleting model:', error);
        showToast('Failed to delete model', 'error');
    }
}

// Generate Interface
async function generateText() {
    const prompt = document.getElementById('generatePrompt').value.trim();
    if (!prompt) return;
    
    const output = document.getElementById('generateOutput');
    const generateBtn = document.getElementById('generateBtn');
    
    state.isGenerating = true;
    generateBtn.disabled = true;
    generateBtn.textContent = 'Generating...';
    output.innerHTML = '<span class="typing-indicator">●●●</span>';
    
    try {
        const response = await fetch('/api/generate', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({
                model: state.currentModel,
                prompt: prompt,
                stream: true
            })
        });
        
        if (!response.ok) {
            throw new Error('Generation failed');
        }
        
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
        showToast('Generation failed', 'error');
    } finally {
        state.isGenerating = false;
        generateBtn.disabled = false;
        generateBtn.textContent = 'Generate Response';
    }
}

// Draft Management
function saveDraft() {
    if (!state.currentChatId) return;
    const draft = document.getElementById('userInput').value;
    localStorage.setItem(`draft_${state.currentChatId}`, draft);
}

function loadDraft() {
    if (!state.currentChatId) return;
    const draft = localStorage.getItem(`draft_${state.currentChatId}`);
    if (draft) {
        document.getElementById('userInput').value = draft;
    } else {
        document.getElementById('userInput').value = '';
    }
}

function clearDraft() {
    if (!state.currentChatId) return;
    localStorage.removeItem(`draft_${state.currentChatId}`);
}

// Keyboard Shortcuts
function setupKeyboardShortcuts() {
    document.addEventListener('keydown', (e) => {
        // Ctrl/Cmd + K - Focus search (if implemented)
        if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
            e.preventDefault();
            // Future: Focus search
        }
        
        // Ctrl/Cmd + N - New chat
        if ((e.ctrlKey || e.metaKey) && e.key === 'n') {
            e.preventDefault();
            createNewChat();
        }
        
        // Escape - Clear input
        if (e.key === 'Escape') {
            document.getElementById('userInput').value = '';
            clearDraft();
        }
    });
}

// Event Listeners
function setupEventListeners() {
    document.getElementById('newChatBtn').addEventListener('click', createNewChat);
    
    document.getElementById('sendBtn').addEventListener('click', sendMessage);
    document.getElementById('userInput').addEventListener('keypress', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            sendMessage();
        }
    });
    
    // Auto-save draft with debounce
    let draftTimeout;
    document.getElementById('userInput').addEventListener('input', () => {
        clearTimeout(draftTimeout);
        draftTimeout = setTimeout(saveDraft, 500);
    });
    
    document.getElementById('attachBtn').addEventListener('click', () => {
        document.getElementById('fileInput').click();
    });
    
    document.getElementById('fileInput').addEventListener('change', (e) => {
        if (e.target.files.length > 0) {
            handleFileUpload(Array.from(e.target.files));
        }
        e.target.value = ''; // Reset input
    });
    
    document.getElementById('modelSelect').addEventListener('change', (e) => {
        state.currentModel = e.target.value;
    });
    
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
    
    document.getElementById('generateBtn').addEventListener('click', generateText);
    
    document.getElementById('refreshModels').addEventListener('click', loadModels);
    document.getElementById('pullBtn').addEventListener('click', pullModel);
    document.getElementById('deleteBtn').addEventListener('click', deleteModel);
    
    document.getElementById('editTitleBtn').addEventListener('click', () => {
        if (!state.currentChatId) return;
        document.getElementById('titleModal').classList.add('active');
        const chat = state.chats.find(c => c.id === state.currentChatId);
        document.getElementById('titleInput').value = chat.title;
    });
    
    document.getElementById('saveTitleBtn').addEventListener('click', async () => {
        const newTitle = document.getElementById('titleInput').value.trim();
        if (!newTitle || !state.currentChatId) return;
        
        await updateChatTitle(state.currentChatId, newTitle);
        document.getElementById('titleModal').classList.remove('active');
    });
    
    document.getElementById('cancelTitleBtn').addEventListener('click', () => {
        document.getElementById('titleModal').classList.remove('active');
    });
    
    document.getElementById('toggleSidebar').addEventListener('click', () => {
        document.getElementById('sidebar').classList.toggle('collapsed');
    });
    
    document.getElementById('mobileSidebarToggle').addEventListener('click', () => {
        document.getElementById('sidebar').classList.toggle('open');
    });
    
    ['temperature', 'topP', 'maxTokens'].forEach(id => {
        const element = document.getElementById(id);
        element.addEventListener('input', debounce(updateParameterValues, 100));
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

// Toast Notifications
function showToast(message, type = 'info') {
    // Remove existing toasts
    const existingToast = document.querySelector('.toast');
    if (existingToast) {
        existingToast.remove();
    }
    
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    document.body.appendChild(toast);
    
    setTimeout(() => toast.classList.add('show'), 10);
    
    setTimeout(() => {
        toast.classList.remove('show');
        setTimeout(() => toast.remove(), 300);
    }, 3000);
}

// Loading States
function showLoadingState(containerId) {
    const container = document.getElementById(containerId);
    if (containerId === 'chatList') {
        container.innerHTML = `
            <div class="loading-skeleton">
                <div class="skeleton-item"></div>
                <div class="skeleton-item"></div>
                <div class="skeleton-item"></div>
            </div>
        `;
    } else if (containerId === 'messagesContainer') {
        container.innerHTML = `
            <div class="loading-skeleton">
                <div class="skeleton-message"></div>
                <div class="skeleton-message"></div>
            </div>
        `;
    }
}

// Utility Functions
function formatContent(content) {
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

function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
        const later = () => {
            clearTimeout(timeout);
            func(...args);
        };
        clearTimeout(timeout);
        timeout = setTimeout(later, wait);
    };
}

async function copyToClipboard(text) {
    try {
        await navigator.clipboard.writeText(text);
        showToast('Copied to clipboard', 'success');
    } catch (err