// State management
let state = {
    sessionId: null,
    currentChatId: null,
    currentModel: '',
    chats: [],
    uploadedFiles: [],
    models: [],
    isStreaming: false,
};

// --- Initialization ---

document.addEventListener('DOMContentLoaded', async () => {
    // Add global toast container to the body for error display
    const toastContainer = document.createElement('div');
    toastContainer.id = 'toastContainer';
    document.body.appendChild(toastContainer);
    
    await initSession();
    await loadModels();
    await loadChats();
    setupEventListeners();
    updateParameterValues();
    
    // Default to 'chat' interface on load
    document.getElementById('chatInterface').classList.add('active');
    
    // Select the default model after loading
    const modelSelect = document.getElementById('modelSelect');
    if (state.models.length > 0 && modelSelect.value === '') {
        modelSelect.value = state.models[0];
        state.currentModel = state.models[0];
    }
});

// --- Session Management ---

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
        } catch (e) {
            showToast('Failed to initialize session. Check server connection.', 'danger');
            return;
        }
    }
    
    state.sessionId = sessionId;
    document.getElementById('sessionInfo').textContent = `Session ID: ${sessionId.substring(0, 8)}...`;
}

// --- Chat Management ---

async function loadChats() {
    if (!state.sessionId) return;
    try {
        const response = await fetch('/api/chats', {
            headers: { 'X-Session-ID': state.sessionId }
        });
        if (response.status === 401) {
             // Session expired or invalid, force re-init
             localStorage.removeItem('laim_session_id');
             await initSession();
             return;
        }
        state.chats = await response.json();
        renderChatList();
        
        // Load the most recent chat if available
        if (state.chats.length > 0) {
            switchChat(state.chats[0].id);
        }
    } catch (e) {
        showToast('Failed to load chats.', 'danger');
    }
}

function renderChatList() {
    const chatList = document.getElementById('chatList');
    chatList.innerHTML = '';
    
    if (state.chats.length === 0) {
        chatList.innerHTML = '<p class="empty-list-message">No chats yet. Start a new one!</p>';
        return;
    }
    
    state.chats.forEach(chat => {
        const chatItem = document.createElement('div');
        chatItem.className = `chat-item ${chat.id === state.currentChatId ? 'active' : ''}`;
        chatItem.dataset.chatId = chat.id;
        
        chatItem.innerHTML = `
            <span class="chat-title">${chat.title}</span>
            <span class="chat-date">${formatDate(chat.updated_at)}</span>
        `;
        
        chatItem.addEventListener('click', () => switchChat(chat.id));
        chatList.appendChild(chatItem);
    });
}

async function newChat() {
    try {
        // Use the currently selected model, or an empty string for the server to pick a default
        const model = document.getElementById('modelSelect').value || '';

        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId 
            },
            body: JSON.stringify({ model: model })
        });
        const newChat = await response.json();
        
        state.chats.unshift(newChat); // Add to the top
        renderChatList();
        switchChat(newChat.id);

    } catch (e) {
        showToast('Failed to create new chat.', 'danger');
    }
}

async function switchChat(chatId) {
    state.currentChatId = chatId;
    document.querySelectorAll('.chat-item').forEach(el => el.classList.remove('active'));
    document.querySelector(`.chat-item[data-chat-id="${chatId}"]`).classList.add('active');
    
    const chat = state.chats.find(c => c.id === chatId);
    document.getElementById('chatTitle').textContent = chat.title;
    document.getElementById('modelSelect').value = chat.model;
    state.currentModel = chat.model;

    await loadMessages(chatId);
}

async function loadMessages(chatID) {
    const chatWindow = document.getElementById('chatWindow');
    chatWindow.innerHTML = '<div class="loading-spinner"></div>';
    
    try {
        const response = await fetch(`/api/messages/${chatID}`, {
            headers: { 'X-Session-ID': state.sessionId }
        });
        const messages = await response.json();
        
        chatWindow.innerHTML = '';
        if (messages.length === 0) {
             chatWindow.innerHTML = '<div class="chat-loading-message">Start a conversation!</div>';
        } else {
             messages.forEach(msg => renderMessage(msg, false));
             chatWindow.scrollTop = chatWindow.scrollHeight;
        }

    } catch (e) {
        chatWindow.innerHTML = '<div class="chat-loading-message error">Failed to load messages.</div>';
        showToast('Failed to load chat history.', 'danger');
    }
}

function renderMessage(message, isStreaming = false) {
    const chatWindow = document.getElementById('chatWindow');
    let messageElement = document.getElementById(`msg-${message.id}`);
    
    if (!messageElement) {
        messageElement = document.createElement('div');
        messageElement.className = `message ${message.role}`;
        messageElement.id = `msg-${message.id}`;
        
        // Add metadata for non-streaming messages
        if (!isStreaming) {
            const roleName = message.role === 'user' ? 'You' : 'Model';
            const timestamp = formatDate(message.created_at);
            messageElement.innerHTML = `
                <div class="message-meta"><strong>${roleName}</strong> - <span>${timestamp}</span></div>
                <div class="message-content"></div>
            `;
        } else {
            // For streaming, we just need the content container
            messageElement.innerHTML = `<div class="message-content"></div>`;
        }
        
        chatWindow.appendChild(messageElement);
    }
    
    const contentElement = messageElement.querySelector('.message-content');
    
    // Add image/file previews to the user message immediately
    if (message.role === 'user' && message.files && message.files.length > 0) {
        let fileHtml = '<div class="message-files">';
        message.files.forEach(file => {
             // NOTE: The server stores the full Data URI, so we can use it directly as the image source
            if (file.mime_type.startsWith('image/')) {
                 fileHtml += `<img src="${file.content}" alt="${file.name}" class="file-message-thumbnail" title="${file.name}">`;
            } else {
                 fileHtml += `<span class="file-message-name" title="${file.name}">📎 ${file.name}</span>`;
            }
        });
        fileHtml += '</div>';
        
        // Prepend file HTML to the content
        contentElement.innerHTML = fileHtml + formatContent(message.content);
    } else {
        contentElement.innerHTML = formatContent(message.content);
    }
    
    // Scroll to the bottom
    chatWindow.scrollTop = chatWindow.scrollHeight;
    
    return messageElement;
}

async function sendMessage() {
    if (state.isStreaming || !state.currentChatId) return;

    const userInput = document.getElementById('userInput');
    const content = userInput.value;
    
    if (!content && state.uploadedFiles.length === 0) {
        showToast('Please enter a message or upload a file.', 'warning');
        return; 
    }
    
    state.isStreaming = true;
    
    // 1. Create temporary message structure for display
    const userMsg = {
        id: 'temp-' + Date.now(),
        chat_id: state.currentChatId,
        role: 'user',
        content: content,
        files: state.uploadedFiles,
        created_at: new Date().toISOString()
    };
    renderMessage(userMsg, false);
    userInput.value = '';
    
    // Clear files from state and UI immediately after display
    state.uploadedFiles = [];
    document.getElementById('filePreview').innerHTML = '';


    // 2. Prepare payload
    const payload = {
        chat_id: state.currentChatId,
        content: content,
        model: state.currentModel,
        files: userMsg.files, // Use the files from the temporary display object
    };
    
    // 3. Start streaming model response
    const modelResponseId = 'temp-model-' + Date.now();
    let modelContent = '';

    try {
        const response = await fetch('/api/messages', {
            method: 'POST',
            headers: { 
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId 
            },
            body: JSON.stringify(payload)
        });

        if (!response.ok) {
            const errorData = await response.json();
            showToast(`API Error: ${errorData.error}`, 'danger');
            state.isStreaming = false;
            return;
        }

        // Create a temporary message element for streaming
        const tempModelMsg = {
            id: modelResponseId,
            role: 'assistant',
            content: '',
            created_at: new Date().toISOString()
        };
        const modelElement = renderMessage(tempModelMsg, true);
        const contentElement = modelElement.querySelector('.message-content');
        
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            const chunk = decoder.decode(value, { stream: true });
            
            // Process the server's streaming NDJSON output
            chunk.split('\n').forEach(line => {
                if (line) {
                    try {
                        const data = JSON.parse(line);
                        if (data.done === true) {
                            // Final message has been processed/saved by the server
                            // Re-load messages to refresh and get the final saved response with ID and timestamp
                            loadMessages(state.currentChatId); 
                            return;
                        }

                        if (data.message && data.message.content) {
                            modelContent += data.message.content;
                            contentElement.innerHTML = formatContent(modelContent);
                            document.getElementById('chatWindow').scrollTop = document.getElementById('chatWindow').scrollHeight;
                        }
                    } catch (e) {
                         // Non-JSON line (like a final EOF line or partial chunk)
                    }
                }
            });
        }
        
    } catch (e) {
        console.error("Fetch error:", e);
        showToast('Network error or connection lost to Ollama.', 'danger');
        
        // Remove the temporary message if streaming failed to start
        document.getElementById(`msg-${modelResponseId}`)?.remove(); 
        
    } finally {
        state.isStreaming = false;
        // The final messages are reloaded inside the streaming loop upon receiving { "done": true }
    }
}

// --- File Handling Functions (CRITICAL FIX) ---

function handleFileUpload(event) {
    const files = Array.from(event.target.files);
    // Add new files to the existing list
    state.uploadedFiles = []; // Clear previous before adding new
    document.getElementById('filePreview').innerHTML = '';

    files.forEach(file => {
        // Simple size check (10MB)
        if (file.size > 10 * 1024 * 1024) {
            showToast(`File "${file.name}" is too large (max 10MB).`, 'warning');
            return;
        }

        const reader = new FileReader();
        reader.onload = (e) => {
            const fileData = {
                name: file.name,
                mime_type: file.type,
                content: e.target.result // CRITICAL: This is the Data URI required by the server.
            };
            state.uploadedFiles.push(fileData);
            renderFilePreview(fileData);
        };
        // CRITICAL: Read the file as a Data URL (Base64 string with prefix)
        reader.readAsDataURL(file); 
    });
    // Clear the input field so the change event fires again if the same file is selected
    event.target.value = '';
}

function renderFilePreview(file) {
    const previewContainer = document.getElementById('filePreview');
    const previewDiv = document.createElement('div');
    // We use a unique name here to identify the file for removal
    previewDiv.className = 'file-preview-item';
    previewDiv.dataset.fileName = file.name;
    
    let content = `<span>📎 ${file.name}</span>`;

    // Display image thumbnail for images
    if (file.mime_type.startsWith('image/')) {
        content = `<img src="${file.content}" alt="${file.name}" class="file-thumbnail">`;
    }

    // Add a remove button
    content += `<button onclick="removeUploadedFile('${file.name}')">x</button>`;

    previewDiv.innerHTML = content;
    previewContainer.appendChild(previewDiv);
}

// Allows user to remove an uploaded file before sending
function removeUploadedFile(fileName) {
    state.uploadedFiles = state.uploadedFiles.filter(f => f.name !== fileName);
    
    // Remove the preview element from the DOM
    const previewContainer = document.getElementById('filePreview');
    const itemToRemove = previewContainer.querySelector(`[data-file-name="${fileName}"]`);
    if (itemToRemove) {
        itemToRemove.remove();
    }
}


// --- Model Management ---

async function loadModels() {
    try {
        const response = await fetch('/api/models');
        state.models = await response.json();
        renderModelSelect();
        renderAvailableModelsList();
    } catch (e) {
        showToast('Failed to load Ollama models. Check Ollama server connection.', 'danger');
        document.getElementById('modelSelect').innerHTML = '<option value="">Ollama Error</option>';
    }
}

function renderModelSelect() {
    const modelSelect = document.getElementById('modelSelect');
    const deleteModelSelect = document.getElementById('deleteModelSelect');
    modelSelect.innerHTML = '';
    deleteModelSelect.innerHTML = '<option value="">Select model to delete</option>';
    
    if (state.models.length === 0) {
        modelSelect.innerHTML = '<option value="">No Models Available</option>';
        return;
    }

    state.models.forEach(model => {
        // Main Chat Model Select
        const option = document.createElement('option');
        option.value = model;
        option.textContent = model;
        modelSelect.appendChild(option);
        
        // Delete Model Select
        const deleteOption = option.cloneNode(true);
        deleteModelSelect.appendChild(deleteOption);
    });
    
    // Set the initial model state
    if (!state.currentModel && state.models.length > 0) {
        state.currentModel = state.models[0];
        modelSelect.value = state.currentModel;
    }
}

function renderAvailableModelsList() {
    const list = document.getElementById('availableModelsList');
    list.innerHTML = '';
    if (state.models.length === 0) {
        list.innerHTML = '<p class="empty-list-message">No models found. Pull one above!</p>';
        return;
    }
    
    state.models.forEach(model => {
        const item = document.createElement('div');
        item.className = 'model-item';
        item.textContent = model;
        list.appendChild(item);
    });
}

async function handlePullModel() {
    const modelNameInput = document.getElementById('modelName');
    const modelName = modelNameInput.value.trim();
    if (!modelName) {
        showToast('Please enter a model name to pull.', 'warning');
        return;
    }
    
    const pullBtn = document.getElementById('pullBtn');
    const pullProgress = document.getElementById('pullProgress');
    pullBtn.disabled = true;
    pullBtn.textContent = 'Pulling...';
    pullProgress.textContent = 'Starting pull...';

    try {
        const response = await fetch('/api/models/pull', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: modelName })
        });

        if (!response.ok) {
            const errorData = await response.json();
            showToast(`Pull failed: ${errorData.error || response.statusText}`, 'danger');
            return;
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        
        pullProgress.textContent = '';
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            const chunk = decoder.decode(value, { stream: true });
            
            chunk.split('\n').forEach(line => {
                if (line) {
                    try {
                        const data = JSON.parse(line);
                        let output = '';
                        if (data.status) {
                             output = data.status;
                        } else if (data.digest && data.total) {
                            const percent = ((data.completed / data.total) * 100).toFixed(2);
                            output = `${data.digest.split(':')[1].substring(0, 10)}...: ${data.completed} / ${data.total} (${percent}%)`;
                        } else if (data.error) {
                            showToast(`Pull error: ${data.error}`, 'danger');
                            output = `Error: ${data.error}`;
                        }
                        
                        if (output) {
                            pullProgress.textContent = output;
                        }
                    } catch (e) {
                         // Ignore non-JSON lines
                    }
                }
            });
        }
        
        showToast(`Model ${modelName} pulled successfully!`, 'success');
        modelNameInput.value = '';
        await loadModels(); // Refresh model lists

    } catch (e) {
        showToast('Network error during model pull.', 'danger');
    } finally {
        pullBtn.disabled = false;
        pullBtn.textContent = 'Pull Model';
    }
}

async function handleDeleteModel() {
    const modelName = document.getElementById('deleteModelSelect').value;
    if (!modelName) {
        showToast('Please select a model to delete.', 'warning');
        return;
    }

    if (!confirm(`Are you sure you want to delete model "${modelName}"? This cannot be undone.`)) {
        return;
    }

    const deleteBtn = document.getElementById('deleteBtn');
    deleteBtn.disabled = true;
    deleteBtn.textContent = 'Deleting...';

    try {
        const response = await fetch('/api/models/delete', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: modelName })
        });
        
        if (response.ok) {
            showToast(`Model ${modelName} deleted successfully.`, 'success');
            await loadModels(); // Refresh model lists
        } else {
            const errorData = await response.json();
            showToast(`Deletion failed: ${errorData.error || response.statusText}`, 'danger');
        }

    } catch (e) {
        showToast('Network error during model deletion.', 'danger');
    } finally {
        deleteBtn.disabled = false;
        deleteBtn.textContent = 'Delete Selected Model';
    }
}

// --- Event Listeners and UI ---

function setupEventListeners() {
    // Session/Chat listeners
    document.getElementById('newChatBtn').addEventListener('click', newChat);
    document.getElementById('sendBtn').addEventListener('click', sendMessage);
    document.getElementById('userInput').addEventListener('keypress', (e) => {
        if (e.key === 'Enter') {
            sendMessage();
        }
    });
    
    // Model Select listener
    document.getElementById('modelSelect').addEventListener('change', (e) => {
        state.currentModel = e.target.value;
        // Optionally update the model in the DB for the current chat here if desired
    });
    
    // Interface switch listener
    document.getElementById('apiType').addEventListener('change', (e) => {
        const type = e.target.value;
        document.getElementById('chatInterface').classList.remove('active');
        document.getElementById('modelsInterface').classList.remove('active');
        document.getElementById(`${type}Interface`).classList.add('active');
    });

    // Model management listeners
    document.getElementById('pullBtn').addEventListener('click', handlePullModel);
    document.getElementById('deleteBtn').addEventListener('click', handleDeleteModel);
    
    // Chat Title Edit listeners
    document.getElementById('editTitleBtn').addEventListener('click', showTitleModal);
    document.getElementById('saveTitleBtn').addEventListener('click', saveChatTitle);
    document.getElementById('cancelTitleBtn').addEventListener('click', hideTitleModal);
    
    // NEW: File upload listeners
    document.getElementById('fileUpload').addEventListener('change', handleFileUpload);
    document.getElementById('uploadBtn').addEventListener('click', () => document.getElementById('fileUpload').click());
}

function showTitleModal() {
    const chat = state.chats.find(c => c.id === state.currentChatId);
    if (!chat) return;
    
    const modal = document.getElementById('titleModal');
    const titleInput = document.getElementById('titleInput');
    
    titleInput.value = chat.title;
    modal.style.display = 'flex';
}

function hideTitleModal() {
    document.getElementById('titleModal').style.display = 'none';
}

async function saveChatTitle() {
    const newTitle = document.getElementById('titleInput').value.trim();
    if (!newTitle) {
        showToast('Title cannot be empty.', 'warning');
        return;
    }
    
    try {
        const response = await fetch('/api/chat/title', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({
                chat_id: state.currentChatId,
                title: newTitle
            })
        });

        if (response.ok) {
            const chat = state.chats.find(c => c.id === state.currentChatId);
            if (chat) {
                chat.title = newTitle;
                document.getElementById('chatTitle').textContent = newTitle;
                renderChatList();
                showToast('Chat title updated.', 'success');
            }
        } else {
            const errorData = await response.json();
            showToast(`Failed to update title: ${errorData.error}`, 'danger');
        }
    } catch (e) {
        showToast('Network error while updating title.', 'danger');
    } finally {
        hideTitleModal();
    }
}

function showToast(message, type = 'info', duration = 3000) {
    const toastContainer = document.getElementById('toastContainer');
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    
    toastContainer.appendChild(toast);
    
    setTimeout(() => {
        toast.classList.add('hide');
        toast.addEventListener('transitionend', () => toast.remove());
    }, duration);
}


function updateParameterValues() {
    // Placeholder function for model parameter updates
}

function formatContent(content) {
    if (!content) return '';
    
    // Basic markdown rendering
    let htmlContent = escapeHtml(content);
    
    // Code blocks
    htmlContent = htmlContent.replace(/```(\w+)?\n([\s\S]*?)```/g, (match, lang, code) => {
        return `<pre><code class="language-${lang || 'text'}">${code}</code></pre>`;
    });
    
    // Inline code
    htmlContent = htmlContent.replace(/`([^`]+)`/g, '<code>$1</code>');
    
    // Bold
    htmlContent = htmlContent.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    
    // Italic
    htmlContent = htmlContent.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    
    // Line breaks
    htmlContent = htmlContent.replace(/\n/g, '<br>');
    
    return htmlContent;
}

function escapeHtml(text) {
    if (!text) return '';
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
    
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}