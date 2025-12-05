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
        } catch (error) {
            showToast('Failed to initialize session with the server.', 'danger');
            return;
        }
    }
    
    state.sessionId = sessionId;
    document.getElementById('sessionInfo').textContent = `Session: ${sessionId.substring(0, 8)}...`;
}

// --- Chat Management ---

async function loadChats() {
    if (!state.sessionId) return;
    try {
        const response = await fetch('/api/chats', {
            headers: { 'X-Session-ID': state.sessionId }
        });
        if (!response.ok) throw new Error('Failed to fetch chats.');
        
        state.chats = await response.json() || [];
        renderChatList();
        
        // Load the first chat if no chat is currently active
        if (!state.currentChatId && state.chats.length > 0) {
            selectChat(state.chats[0].id);
        } else if (state.currentChatId) {
            // Re-select the current chat to load its messages
            selectChat(state.currentChatId, false); // Pass false to skip message load if already loaded
        } else {
            // If no chats exist, set up the New Chat UI state
            document.getElementById('chatTitle').textContent = "New Chat";
            document.getElementById('messagesContainer').innerHTML = '';
        }
    } catch (error) {
        console.error('Error loading chats:', error);
        showToast('Error loading chat history.', 'danger');
        state.chats = [];
    }
}

function renderChatList() {
    const chatList = document.getElementById('chatList');
    chatList.innerHTML = '';
    
    state.chats.forEach(chat => {
        const chatItem = document.createElement('div');
        chatItem.className = `chat-item ${chat.id === state.currentChatId ? 'active' : ''}`;
        chatItem.dataset.chatId = chat.id;
        
        // Use a more compact date format for the sidebar
        const lastActivity = formatDate(chat.updated_at);

        chatItem.innerHTML = `
            <div class="chat-title">${escapeHtml(chat.title)}</div>
            <div class="chat-meta">
                <span class="chat-model">${chat.model.split(':')[0]}</span> 
                <span class="chat-date">${lastActivity}</span>
            </div>
        `;
        
        chatItem.addEventListener('click', () => selectChat(chat.id));
        chatList.appendChild(chatItem);
    });
}

function selectChat(chatID, loadMessagesFlag = true) {
    if (state.isStreaming) {
        showToast("Please wait for the current response to finish.", 'warning');
        return;
    }
    
    // Only load messages if the chat ID is actually changing or flag is true
    const shouldLoadMessages = loadMessagesFlag && state.currentChatId !== chatID;
    
    state.currentChatId = chatID;
    const selectedChat = state.chats.find(c => c.id === chatID);
    
    if (selectedChat) {
        document.getElementById('chatTitle').textContent = escapeHtml(selectedChat.title);
        document.getElementById('modelSelect').value = selectedChat.model;
        state.currentModel = selectedChat.model;
        if (shouldLoadMessages) {
            loadMessages(chatID);
        }
    }
    renderChatList(); // Update active class
    
    // Hide sidebar on mobile after selection
    if (window.innerWidth <= 768) {
        // Find and toggle the sidebar visibility if implemented
    }
}

async function newChat() {
    if (state.isStreaming) {
        showToast("Please wait for the current response to finish.", 'warning');
        return;
    }
    
    try {
        const modelSelect = document.getElementById('modelSelect');
        const defaultModel = modelSelect.value || state.models[0] || '';
        
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({ model: defaultModel })
        });
        
        if (!response.ok) throw new Error('Failed to create new chat.');
        
        const newChat = await response.json();
        state.chats.unshift(newChat); // Add to the start
        selectChat(newChat.id);
        
        // Clear message area and title
        document.getElementById('messagesContainer').innerHTML = '';
        document.getElementById('chatTitle').textContent = escapeHtml(newChat.title);
        
        showToast('New chat started.', 'success');
    } catch (error) {
        console.error('Error creating new chat:', error);
        showToast('Error starting new chat.', 'danger');
    }
}

// --- Message Handlers ---

async function loadMessages(chatID) {
    const messagesContainer = document.getElementById('messagesContainer');
    messagesContainer.innerHTML = '<div class="loading-spinner"></div>'; // Show loading spinner
    
    try {
        const response = await fetch(`/api/messages/${chatID}`, {
            headers: { 'X-Session-ID': state.sessionId }
        });
        
        if (!response.ok) throw new Error('Failed to load messages.');
        
        const messages = await response.json();
        renderMessages(messages);
        
    } catch (error) {
        console.error('Error loading messages:', error);
        messagesContainer.innerHTML = '<div class="message system-message">Failed to load chat messages.</div>';
        showToast('Error loading messages.', 'danger');
    }
}

function renderMessages(messages) {
    const messagesContainer = document.getElementById('messagesContainer');
    messagesContainer.innerHTML = ''; // Clear previous messages
    
    messages.forEach(msg => {
        appendMessage(msg.role, msg.content, msg.created_at, msg.id, false);
    });
    
    messagesContainer.scrollTop = messagesContainer.scrollHeight;
}

function appendMessage(role, content, timestamp, id, isNew = true) {
    const messagesContainer = document.getElementById('messagesContainer');
    const messageEl = document.createElement('div');
    messageEl.className = `message ${role}-message`;
    messageEl.dataset.messageId = id;
    
    const formattedContent = formatContent(content);
    
    messageEl.innerHTML = `
        <div class="message-header">
            <span class="message-role">${role === 'user' ? 'You' : 'LAIM'}</span>
            <span class="message-timestamp">${formatDate(timestamp)}</span>
        </div>
        <div class="message-content">${formattedContent}</div>
    `;
    
    messagesContainer.appendChild(messageEl);
    
    if (isNew) {
        messagesContainer.scrollTop = messagesContainer.scrollHeight;
    }
}


async function sendMessage(event) {
    if (event) event.preventDefault();
    if (state.isStreaming) return;

    const inputField = document.getElementById('messageInput');
    const userContent = inputField.value.trim();
    if (!userContent) return;

    // FIX (Client Side): Prevent sending if essential state is missing
    if (!state.currentChatId) {
        showToast('Error: A chat must be selected or started. Starting a new chat...', 'warning');
        await newChat(); // Try to start a new chat if none exists
        if (!state.currentChatId) {
             showToast('Error: Failed to start a new chat. Cannot send message.', 'danger');
             return;
        }
    }
    
    if (!state.currentModel) {
        showToast('Error: Please select a model.', 'warning');
        return;
    }
    
    state.isStreaming = true;
    inputField.value = '';
    
    // 1. Append User Message to UI
    const userMessageId = 'temp-' + Date.now();
    appendMessage('user', userContent, new Date().toISOString(), userMessageId, true);
    
    // 2. Add temporary Assistant Message (for streaming target)
    const assistantMessageId = 'temp-assistant-' + Date.now();
    appendMessage('assistant', '<span class="loading-dots"></span>', new Date().toISOString(), assistantMessageId, true);
    const assistantEl = document.querySelector(`[data-message-id="${assistantMessageId}"] .message-content`);
    
    try {
        const response = await fetch('/api/messages', {
            method: 'POST',
            headers: { 
                'Content-Type': 'application/json',
                'X-Session-ID': state.sessionId
            },
            body: JSON.stringify({
                chat_id: state.currentChatId,
                content: userContent,
                model: state.currentModel,
                files: state.uploadedFiles // send uploaded files if any
            })
        });

        if (response.status === 400) {
            const errorData = await response.json();
            throw new Error(`Server Error (400): ${errorData.error || 'Bad Request. Check server logs.'}`);
        }
        
        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(`Server returned status: ${response.status}. Details: ${errorText.substring(0, 100)}`);
        }
        
        if (!response.body) {
            throw new Error("No response body received from server.");
        }
        
        const reader = response.body.getReader();
        const decoder = new TextDecoder("utf-8");
        let fullResponseContent = '';
        let initialContentDisplayed = false;

        assistantEl.innerHTML = ''; // Clear loading dots

        while (true) {
            const { value, done } = await reader.read();
            if (done) break;

            const chunk = decoder.decode(value, { stream: true });
            
            // Process the stream: each line is a JSON object
            const lines = chunk.split('\n').filter(line => line.trim() !== '');
            for (const line of lines) {
                try {
                    const data = JSON.parse(line);
                    
                    if (data.message && data.message.content) {
                        fullResponseContent += data.message.content;
                    } else if (data.content) { // Fallback for simple 'generate'
                        fullResponseContent += data.content;
                    }
                    
                    if (fullResponseContent) {
                        assistantEl.innerHTML = formatContent(fullResponseContent);
                        initialContentDisplayed = true;
                        document.getElementById('messagesContainer').scrollTop = document.getElementById('messagesContainer').scrollHeight;
                    }
                    
                } catch (e) {
                    // Ignore non-JSON lines or partial JSON chunks
                }
            }
        }
        
        if (!initialContentDisplayed) {
             assistantEl.innerHTML = "I'm sorry, I couldn't generate a response.";
        }
        
        // After streaming is complete, reload chats to update the chat list (timestamp)
        await loadChats(); 

    } catch (error) {
        console.error('Error in sendMessage:', error);
        assistantEl.innerHTML = `**Error:** ${error.message}`;
        showToast(`Message failed: ${error.message}`, 'danger');
        document.getElementById('messagesContainer').scrollTop = document.getElementById('messagesContainer').scrollHeight;
    } finally {
        state.isStreaming = false;
        state.uploadedFiles = []; // Clear files after use
    }
}

// --- Model Management ---

async function loadModels() {
    try {
        const response = await fetch('/api/models');
        if (!response.ok) throw new Error('Failed to fetch models.');
        
        state.models = await response.json();
        
        const modelSelect = document.getElementById('modelSelect');
        const deleteModelSelect = document.getElementById('deleteModelSelect');
        
        // Clear previous options
        modelSelect.innerHTML = '';
        deleteModelSelect.innerHTML = '<option value="">Select model to delete</option>';
        
        if (state.models.length === 0) {
            modelSelect.innerHTML = '<option value="" disabled>No models available</option>';
            showToast('No Ollama models found. Pull one using the Models tab.', 'warning', 5000);
            return;
        }

        state.models.forEach(modelName => {
            const option = document.createElement('option');
            option.value = modelName;
            option.textContent = modelName.replace(':latest', '');
            modelSelect.appendChild(option);

            const deleteOption = option.cloneNode(true);
            deleteModelSelect.appendChild(deleteOption);
        });

        // Set state to the first model or the currently active model
        if (!state.currentModel || !state.models.includes(state.currentModel)) {
            state.currentModel = state.models[0];
            modelSelect.value = state.models[0];
        } else {
            modelSelect.value = state.currentModel;
        }

    } catch (error) {
        console.error('Error loading models:', error);
        const modelSelect = document.getElementById('modelSelect');
        modelSelect.innerHTML = '<option value="" disabled>Connection Error</option>';
        showToast('Error connecting to Ollama service.', 'danger');
    }
}

async function pullModel(modelName) {
    const pullProgressEl = document.getElementById('pullProgress');
    pullProgressEl.textContent = `Attempting to pull ${modelName}...`;

    try {
        const response = await fetch('/api/models/pull', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: modelName, stream: true })
        });

        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(`Server returned error: ${errorText}`);
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        
        while (true) {
            const { value, done } = await reader.read();
            if (done) break;

            const chunk = decoder.decode(value);
            // Process the stream: each line is a JSON object
            const lines = chunk.split('\n').filter(line => line.trim() !== '');
            for (const line of lines) {
                try {
                    const data = JSON.parse(line);
                    if (data.status) {
                        pullProgressEl.textContent = `[${data.status}] ${data.digest || ''}`;
                    } else if (data.error) {
                         throw new Error(data.error);
                    }
                    if (data.total && data.completed) {
                        const percent = ((data.completed / data.total) * 100).toFixed(2);
                        pullProgressEl.textContent += ` (${percent}%)`;
                    }
                } catch (e) {
                    console.warn("Failed to parse pull stream JSON:", line);
                }
            }
        }

        pullProgressEl.textContent = `${modelName} pull complete!`;
        await loadModels(); // Refresh list
        showToast(`${modelName} pulled successfully.`, 'success');
        
    } catch (error) {
        console.error('Model pull error:', error);
        pullProgressEl.textContent = `Error: ${error.message}`;
        showToast(`Model pull failed: ${error.message}`, 'danger');
    }
}

async function deleteModel(modelName) {
    if (!modelName) {
        showToast('Please select a model to delete.', 'warning');
        return;
    }
    
    if (!confirm(`Are you sure you want to delete the model: ${modelName}?`)) return;

    try {
        const response = await fetch('/api/models/delete', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: modelName })
        });

        if (response.status === 404) {
            throw new Error('Model not found on Ollama server.');
        }
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({ error: 'Unknown deletion error' }));
            throw new Error(errorData.error || 'Failed to delete model.');
        }

        showToast(`${modelName} deleted successfully.`, 'success');
        await loadModels(); // Refresh list
        
    } catch (error) {
        console.error('Model delete error:', error);
        showToast(`Model deletion failed: ${error.message}`, 'danger');
    }
}

// --- UI Event Listeners ---

function setupEventListeners() {
    // Chat Form Submission
    document.getElementById('chatForm').addEventListener('submit', sendMessage);
    
    // New Chat Button
    document.getElementById('newChatBtn').addEventListener('click', newChat);
    
    // Model Select Change
    document.getElementById('modelSelect').addEventListener('change', (e) => {
        state.currentModel = e.target.value;
    });

    // API Type Switch (to show/hide interfaces)
    document.getElementById('apiType').addEventListener('change', (e) => {
        document.querySelectorAll('.interface-section').forEach(el => el.classList.remove('active'));
        document.getElementById(`${e.target.value}Interface`).classList.add('active');
    });

    // Pull Model
    document.getElementById('pullBtn').addEventListener('click', () => {
        const modelName = document.getElementById('modelName').value.trim();
        if (modelName) {
            pullModel(modelName);
        } else {
            showToast('Please enter a model name (e.g., llama3).', 'warning');
        }
    });

    // Delete Model
    document.getElementById('deleteBtn').addEventListener('click', () => {
        const modelName = document.getElementById('deleteModelSelect').value;
        deleteModel(modelName);
    });
    
    // Edit Title Modal Handlers
    const titleModal = document.getElementById('titleModal');
    const titleInput = document.getElementById('titleInput');
    
    document.getElementById('editTitleBtn').addEventListener('click', () => {
        if (!state.currentChatId || state.currentChatId === 'new') {
            showToast("Can only edit title of saved chats.", 'warning');
            return;
        }
        const currentTitle = document.getElementById('chatTitle').textContent;
        titleInput.value = currentTitle;
        titleModal.style.display = 'flex';
    });
    
    document.getElementById('cancelTitleBtn').addEventListener('click', () => {
        titleModal.style.display = 'none';
    });
    
    document.getElementById('saveTitleBtn').addEventListener('click', async () => {
        const newTitle = titleInput.value.trim();
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
            
            if (!response.ok) throw new Error('Failed to save title.');
            
            document.getElementById('chatTitle').textContent = escapeHtml(newTitle);
            titleModal.style.display = 'none';
            await loadChats(); // Refresh chat list
            showToast('Chat title updated.', 'success');
            
        } catch (error) {
            console.error('Error saving title:', error);
            showToast('Error saving chat title.', 'danger');
        }
    });
}

// --- Utility Functions ---

function showToast(message, type = 'info', duration = 3000) {
    const container = document.getElementById('toastContainer');
    if (!container) return;

    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;

    container.appendChild(toast);

    // Auto-remove
    setTimeout(() => {
        toast.classList.add('fade-out');
        setTimeout(() => toast.remove(), 500); // Wait for fade-out transition
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
    
    const year = date.getFullYear();
    const month = date.getMonth() + 1;
    const day = date.getDate();
    
    return `${month}/${day}/${year}`;
}