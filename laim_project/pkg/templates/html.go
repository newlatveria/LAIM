package templates

const HTML = `<!DOCTYPE html>
<html lang="en" data-theme="dark">
<head>
<meta charset="UTF-8" />
<title>Ollama Universal File Loader + Chat</title>
<meta name="viewport" content="width=device-width, initial-scale=1" />
<style>
:root {
  --bg: #111; --fg: #eaeaea; --accent:#3ea6ff; --card:#1e1e1e; --border:#333; --muted:#888;
}
html[data-theme=light] { --bg:#fafafa; --fg:#222; --accent:#0066cc; --card:#fff; --border:#ccc; --muted:#666; }
body { font-family: system-ui, sans-serif; background:var(--bg); color:var(--fg); margin:0; padding:20px; }
.container { max-width:900px; margin:auto; background:var(--card); padding:20px; border-radius:12px; border:1px solid var(--border); }
textarea, select, input[type=file] { width:100%; padding:10px; border-radius:6px; background:var(--bg); color:var(--fg); border:1px solid var(--border); }
button, input[type=submit] { width:100%; padding:12px; background:var(--accent); border:none; border-radius:6px; cursor:pointer; margin-top:10px; color:#000; font-size:1rem; }
.result { background:var(--bg); border:1px solid var(--border); padding:12px; white-space:pre-wrap; border-radius:6px; }
.msg-user { color:var(--accent); font-weight:bold; } .msg-assistant { color:#77dd77; }
#toggle-dark { cursor:pointer; } #drop-area { border:2px dashed var(--border); padding:20px; margin-top:10px; border-radius:10px; text-align:center; transition:border-color .15s ease; }
#drop-area.dragover { border-color:var(--accent); }
.progress { width:100%; background:#222; border-radius:6px; overflow:hidden; border:1px solid var(--border); margin-top:8px; }
.progress > div { height:14px; width:0%; background:linear-gradient(90deg,#3ea6ff,#77dd77); transition:width .2s ease; }
.small { font-size:.9rem; color:var(--muted); }
.chat-area { border:1px solid var(--border); padding:10px; border-radius:8px; max-height:260px; overflow:auto; background:#080808; }
.chat-row { margin-bottom:8px; }
.waiting { opacity:0.9; color:var(--muted); font-style:italic; }
</style>
<script>
document.addEventListener("DOMContentLoaded", () => {
  const saved = localStorage.getItem("theme") || "dark";
  document.documentElement.dataset.theme = saved;
  document.querySelector("#toggle-dark").onclick = () => {
    const current = document.documentElement.dataset.theme;
    const next = current === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    localStorage.setItem("theme", next);
  };

  // Drag & drop
  const dropArea = document.getElementById("drop-area");
  const fileInput = document.getElementById("file-input-normal");
  const folderInput = document.getElementById("file-input-folder");
  ['dragenter','dragover'].forEach(e=>dropArea.addEventListener(e,evt=>{evt.preventDefault(); dropArea.classList.add('dragover');}));
  ['dragleave','drop'].forEach(e=>dropArea.addEventListener(e,evt=>{evt.preventDefault(); dropArea.classList.remove('dragover');}));
  dropArea.addEventListener('drop', ev => {
    const dt = ev.dataTransfer;
    if (!dt) return;
    const files = dt.files;
    if (files && files.length>0) {
      const dtNew = new DataTransfer();
      for (let i=0;i<files.length;i++) dtNew.items.add(files[i]);
      fileInput.files = dtNew.files;
      folderInput.files = dtNew.files;
    }
  });

  // Upload XHR progress
  const uploadForm = document.getElementById("upload-form");
  const progressInner = document.getElementById("upload-progress-inner");
  const progressText = document.getElementById("upload-progress-text");

  uploadForm.addEventListener("submit", function(ev){
    ev.preventDefault();
    const formData = new FormData(uploadForm);
    const xhr = new XMLHttpRequest();
    xhr.open("POST", uploadForm.action, true);
    xhr.setRequestHeader("X-Requested-With", "XMLHttpRequest");
    xhr.upload.addEventListener("progress", function(e){
      if (e.lengthComputable) {
        const pct = Math.round((e.loaded / e.total) * 100);
        progressInner.style.width = pct + "%";
        progressText.textContent = pct + "%";
      } else {
        progressText.textContent = "Uploading…";
      }
    });
    xhr.onreadystatechange = function(){
      if (xhr.readyState===4) {
        if (xhr.status>=200 && xhr.status<300) {
          progressInner.style.width = "100%";
          progressText.textContent = "Complete";
          setTimeout(()=>{ window.location.reload(); }, 600);
        } else {
          progressText.textContent = "Error";
          alert("Upload failed: "+xhr.statusText);
        }
      }
    };
    progressInner.style.width = "0%";
    progressText.textContent = "0%";
    xhr.send(formData);
  });

  // CHAT AJAX with "Awaiting model response..." behavior
  const chatForm = document.getElementById("chat-form");
  const chatHistory = document.getElementById("chat-history");
  chatForm.addEventListener("submit", function(ev){
    ev.preventDefault();
    const formData = new FormData(chatForm);
    const userMsg = formData.get("user_prompt") || "";
    // append user message immediately
    const userDiv = document.createElement("div");
    userDiv.className = "chat-row msg-user";
    userDiv.textContent = "You: " + userMsg;
    chatHistory.appendChild(userDiv);
    // append waiting placeholder
    const waitingDiv = document.createElement("div");
    waitingDiv.className = "chat-row waiting";
    waitingDiv.textContent = "AI: ⏳ Awaiting model response...";
    chatHistory.appendChild(waitingDiv);
    chatHistory.scrollTop = chatHistory.scrollHeight;

    // send AJAX
    const xhr = new XMLHttpRequest();
    xhr.open("POST", chatForm.action, true);
    xhr.setRequestHeader("X-Requested-With", "XMLHttpRequest");
    xhr.onreadystatechange = function(){
      if (xhr.readyState===4) {
        if (xhr.status>=200 && xhr.status<300) {
          // replace waiting with assistant response
          waitingDiv.className = "chat-row msg-assistant";
          waitingDiv.textContent = "AI: " + xhr.responseText;
        } else {
          waitingDiv.className = "chat-row msg-assistant";
          waitingDiv.textContent = "AI: [ERROR receiving response]";
        }
        chatHistory.scrollTop = chatHistory.scrollHeight;
      }
    };
    xhr.send(formData);
    // clear input
    chatForm.querySelector('textarea[name="user_prompt"]').value = "";
  });

});
</script>
</head>
<body>
<div class="container">
<button id="toggle-dark">🌓 Toggle Dark Mode</button>
<h2>📂 Ollama Universal Processor + Continuous Chat</h2>

<div id="error" style="color:#ff6b6b; padding:10px; display:none;"></div>

<h3>Chat</h3>
<div id="chat-history" class="chat-area"></div>

<form id="chat-form" action="/chat" method="post">
<select id="model-select" name="model_select"></select>
<textarea name="user_prompt" placeholder="Ask something..."></textarea>
<input type="submit" value="💬 Send Message" />
</form>

<hr>

<form id="upload-form" action="/upload" method="post" enctype="multipart/form-data">
<label>Select Files:</label>
<input id="file-input-normal" type="file" name="file_upload" multiple />
<label>Or Select a Folder:</label>
<input id="file-input-folder" type="file" name="file_upload" webkitdirectory directory />

<div id="drop-area">Drag & drop files or folders here</div>

<label>Your Prompt:</label>
<textarea name="user_prompt"></textarea>

<select id="model-select-2" name="model_select"></select>

<div class="small">Upload Progress:</div>
<div class="progress"><div id="upload-progress-inner" style="width:0%"></div></div>
<div id="upload-progress-text" class="small">0%</div>

<input type="submit" value="📤 Upload + Process" />
</form>

<hr>
<div class="flex"><a href="/logs" target="_blank">View Live Logs</a><div style="flex:1"></div><div class="small">trace: <span id="trace">n/a</span></div></div>
</div>

<script>
// fetch model list and populate selects
fetch("/models").then(r=>r.json()).then(list=>{
  const sel = document.getElementById("model-select");
  const sel2 = document.getElementById("model-select-2");
  list.forEach(m=>{
    const o = document.createElement("option"); o.value = m; o.textContent = m;
    sel.appendChild(o);
    const o2 = document.createElement("option"); o2.value = m; o2.textContent = m;
    sel2.appendChild(o2);
  });
});
</script>
</body>
</html>
`