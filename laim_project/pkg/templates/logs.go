package templates

const LogsHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8"/>
<title>Live Logs</title>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<style>
body { font-family: monospace; background:#111; color:#eee; margin:0; padding:12px; }
#log { height: 90vh; overflow:auto; border:1px solid #333; padding:10px; background:#000; }
.line { white-space:pre; padding:2px 0; }
.err { color:#ff6b6b; } .upload { color:#ffd580; } .ollama { color:#7dd3fc; } .chat { color:#d6b2ff; } .session { color:#9bff9b; }
</style>
</head>
<body>
<h3>Live Logs (auto-scroll)</h3>
<div id="log"></div>
<script>
const logEl = document.getElementById("log");
const evtSource = new EventSource("/logs/stream");
evtSource.onmessage = function(e) {
  const txt = e.data.replace(/\\n/g, "\n");
  const div = document.createElement("div");
  div.className = "line";
  if (txt.indexOf("[ERROR]") !== -1) div.classList.add("err");
  if (txt.indexOf("[UPLOAD]") !== -1) div.classList.add("upload");
  if (txt.indexOf("[OLLAMA]") !== -1) div.classList.add("ollama");
  if (txt.indexOf("[CHAT]") !== -1) div.classList.add("chat");
  if (txt.indexOf("[SESSION]") !== -1) div.classList.add("session");
  div.textContent = txt;
  logEl.appendChild(div);
  logEl.scrollTop = logEl.scrollHeight;
};
evtSource.onerror = function(e) {
  const div = document.createElement("div");
  div.className = "line err";
  div.textContent = "Connection lost. Retrying...";
  logEl.appendChild(div);
};
</script>
</body>
</html>`
