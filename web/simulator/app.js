const API_BASE = window.location.origin;
const WS_URL = `ws://${window.location.host}/ws`;

let ws = null;
let audioCtx = null;
let analyser = null;
let gainNode = null;
let sessionBase = `session_${Date.now()}`;
let sessionId = `${sessionBase}_en`;

// DOM elements
const connectBtn = document.getElementById("connect-btn");
const wsStatus = document.getElementById("ws-status");
const wsLabel = document.getElementById("ws-label");
const chatForm = document.getElementById("chat-form");
const chatInput = document.getElementById("chat-input");
const messages = document.getElementById("messages");
const uploadForm = document.getElementById("upload-form");
const fileInput = document.getElementById("file-input");
const jobStatus = document.getElementById("job-status");
const completeness = document.getElementById("completeness");
const emotionLabel = document.getElementById("emotion-label");
const canvas = document.getElementById("avatar-canvas");
const ctx = canvas.getContext("2d");
const ttsBackend = document.getElementById("tts-backend");
const volumeSlider = document.getElementById("volume-slider");
const volumeValue = document.getElementById("volume-value");
const langSelect = document.getElementById("lang-select");

// Language switch: reset session so LLM gets a clean context for the new language
langSelect.addEventListener("change", () => {
  sessionId = `${sessionBase}_${langSelect.value}`;
  const messages = document.getElementById("messages");
  messages.innerHTML = "";
  addMessage("system", `Language switched to ${langSelect.value === "ja" ? "Japanese" : "English"}`);
});

// Volume control
volumeSlider.addEventListener("input", () => {
  const vol = parseInt(volumeSlider.value, 10);
  volumeValue.textContent = `${vol}%`;
  if (gainNode) {
    gainNode.gain.value = vol / 100;
  }
});

// Avatar state
let avatarState = "idle";
let avatarEmotion = "neutral";
let mouthOpen = 0;
let blinkTimer = 0;
let isBlinking = false;
let breathPhase = 0;

// WebSocket
connectBtn.addEventListener("click", () => {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.close();
    return;
  }
  connectWebSocket();
});

function connectWebSocket() {
  ws = new WebSocket(WS_URL);
  ws.binaryType = "arraybuffer";

  ws.onopen = () => {
    wsStatus.className = "status-dot online";
    wsLabel.textContent = "Connected";
    connectBtn.textContent = "Disconnect";
    fetchConfig();
    fetchCompleteness();
  };

  ws.onclose = () => {
    wsStatus.className = "status-dot offline";
    wsLabel.textContent = "Disconnected";
    connectBtn.textContent = "Connect";
  };

let streamingMessageDiv = null;

  ws.onmessage = (event) => {
    if (event.data instanceof ArrayBuffer) {
      playAudio(event.data);
      return;
    }
    const msg = JSON.parse(event.data);
    if (msg.type === "text") {
      // Final message handled via chat API response, clear the streaming container
      if (streamingMessageDiv) {
        streamingMessageDiv.remove();
        streamingMessageDiv = null;
      }
    } else if (msg.type === "text_chunk") {
      if (!streamingMessageDiv) {
        streamingMessageDiv = document.createElement("div");
        streamingMessageDiv.className = `message assistant streaming`;
        messages.appendChild(streamingMessageDiv);
      }
      streamingMessageDiv.textContent += msg.payload;
      messages.scrollTop = messages.scrollHeight;
    } else if (msg.type === "avatar_command") {
      avatarState = msg.payload.state;
      avatarEmotion = msg.payload.emotion || "neutral";
      emotionLabel.textContent = avatarEmotion;
    }
  };
}

// Chat
chatForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const input = chatInput.value.trim();
  if (!input) return;

  addMessage("user", input);
  chatInput.value = "";

  try {
    const resp = await fetch(`${API_BASE}/api/vbot/chat`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        session_id: sessionId,
        input: input,
        include_audio: true,
        language: langSelect.value,
      }),
    });
    const data = await resp.json();

    if (data.text) {
      const latencyStr = `${data.latency.total_ms}ms (LLM: ${data.latency.llm_inference_ms}ms, TTS: ${data.latency.tts_synthesis_ms}ms)${data.cache_hit ? " [cached]" : ""}`;
      addMessage("assistant", data.text, latencyStr);
    }

    if (data.audio_base64) {
      const audioBytes = Uint8Array.from(atob(data.audio_base64), (c) =>
        c.charCodeAt(0),
      );
      playAudio(audioBytes.buffer);
    }

    if (data.emotion) {
      avatarEmotion = data.emotion;
      emotionLabel.textContent = avatarEmotion;
    }
  } catch (err) {
    addMessage("assistant", `Error: ${err.message}`);
  }
});

function addMessage(role, text, latency) {
  const div = document.createElement("div");
  div.className = `message ${role}`;
  div.textContent = text;
  if (latency) {
    const span = document.createElement("div");
    span.className = "latency";
    span.textContent = latency;
    div.appendChild(span);
  }
  messages.appendChild(div);
  messages.scrollTop = messages.scrollHeight;
}

// Audio playback + mouth animation
function playAudio(arrayBuffer) {
  if (!audioCtx) {
    audioCtx = new AudioContext();
    analyser = audioCtx.createAnalyser();
    analyser.fftSize = 256;
    gainNode = audioCtx.createGain();
    gainNode.gain.value = parseInt(volumeSlider.value, 10) / 100;
    analyser.connect(gainNode);
    gainNode.connect(audioCtx.destination);
  }

  audioCtx.decodeAudioData(arrayBuffer.slice(0), (buffer) => {
    const source = audioCtx.createBufferSource();
    source.buffer = buffer;
    source.connect(analyser);
    source.start(0);
    avatarState = "speaking";

    source.onended = () => {
      avatarState = "idle";
      mouthOpen = 0;
    };
  });
}

// Upload
uploadForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const file = fileInput.files[0];
  if (!file) return;

  const formData = new FormData();
  formData.append("file", file);

  try {
    const resp = await fetch(`${API_BASE}/api/vbot/ingest/upload`, {
      method: "POST",
      body: formData,
    });
    const data = await resp.json();
    jobStatus.textContent = `Job ${data.job_id}: ${data.status}`;

    if (data.job_id) {
      pollJob(data.job_id);
    }
  } catch (err) {
    jobStatus.textContent = `Error: ${err.message}`;
  }
});

async function pollJob(jobId) {
  const interval = setInterval(async () => {
    try {
      const resp = await fetch(`${API_BASE}/api/vbot/ingest/jobs/${jobId}`);
      const data = await resp.json();
      jobStatus.textContent = `Job ${jobId}: ${data.status} (${data.progress}%) | Chunks: ${data.done_chunks}/${data.total_chunks} | Traits: ${data.traits_found}`;

      if (data.status === "done" || data.status === "error") {
        clearInterval(interval);
        fetchCompleteness();
      }
    } catch (err) {
      clearInterval(interval);
    }
  }, 2000);
}

async function fetchConfig() {
  try {
    const resp = await fetch(`${API_BASE}/api/vbot/config`);
    const data = await resp.json();
    ttsBackend.textContent = `TTS: ${data.tts_backend || "unknown"}`;
  } catch (err) {
    /* ignore */
  }
}

async function fetchCompleteness() {
  try {
    const resp = await fetch(`${API_BASE}/api/vbot/personality/completeness`);
    const data = await resp.json();
    completeness.textContent = `Personality: ${data.overall_completeness}% complete. ${data.recommendation || ""}`;
  } catch (err) {
    /* ignore */
  }
}

// Avatar rendering
function drawAvatar() {
  ctx.clearRect(0, 0, 400, 400);

  // Background glow
  const gradient = ctx.createRadialGradient(200, 200, 50, 200, 200, 200);
  gradient.addColorStop(0, "rgba(168, 85, 247, 0.1)");
  gradient.addColorStop(1, "rgba(0, 0, 0, 0)");
  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, 400, 400);

  // Audio-reactive ring
  if (analyser && avatarState === "speaking") {
    const bufferLength = analyser.frequencyBinCount;
    const dataArray = new Uint8Array(bufferLength);
    analyser.getByteFrequencyData(dataArray);

    const avg = dataArray.reduce((a, b) => a + b, 0) / bufferLength;
    mouthOpen = Math.min(avg / 128, 1);

    // Draw visualizer ring
    ctx.strokeStyle = `rgba(168, 85, 247, ${0.3 + mouthOpen * 0.5})`;
    ctx.lineWidth = 2;
    ctx.beginPath();
    for (let i = 0; i < bufferLength; i++) {
      const angle = (i / bufferLength) * Math.PI * 2 - Math.PI / 2;
      const radius = 130 + (dataArray[i] / 255) * 40;
      const x = 200 + Math.cos(angle) * radius;
      const y = 200 + Math.sin(angle) * radius;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    ctx.closePath();
    ctx.stroke();
  } else {
    mouthOpen = Math.max(mouthOpen - 0.05, 0);
  }

  // Breathing animation
  breathPhase += 0.02;
  const breathScale = 1 + Math.sin(breathPhase) * 0.01;
  ctx.save();
  ctx.translate(200, 200);
  ctx.scale(breathScale, breathScale);
  ctx.translate(-200, -200);

  // Face circle
  ctx.fillStyle = "#2a2a4e";
  ctx.beginPath();
  ctx.arc(200, 200, 100, 0, Math.PI * 2);
  ctx.fill();
  ctx.strokeStyle = "#a855f7";
  ctx.lineWidth = 2;
  ctx.stroke();

  // Eyes
  const eyeY = 180;
  const eyeSize = isBlinking ? 1 : 8;

  // Eye color based on emotion
  const eyeColor =
    {
      neutral: "#e0e0e0",
      sarcastic: "#a855f7",
      smug: "#ec4899",
      annoyed: "#ef4444",
    }[avatarEmotion] || "#e0e0e0";

  ctx.fillStyle = eyeColor;
  ctx.beginPath();
  ctx.ellipse(170, eyeY, 12, eyeSize, 0, 0, Math.PI * 2);
  ctx.fill();
  ctx.beginPath();
  ctx.ellipse(230, eyeY, 12, eyeSize, 0, 0, Math.PI * 2);
  ctx.fill();

  // Pupils (only when not blinking)
  if (!isBlinking) {
    ctx.fillStyle = "#1a1a2e";
    ctx.beginPath();
    ctx.arc(170, eyeY, 4, 0, Math.PI * 2);
    ctx.fill();
    ctx.beginPath();
    ctx.arc(230, eyeY, 4, 0, Math.PI * 2);
    ctx.fill();
  }

  // Mouth
  const mouthY = 225;
  const mouthWidth = 30;
  const mouthHeight = mouthOpen * 15;

  if (mouthHeight > 1) {
    ctx.fillStyle = "#1a1a2e";
    ctx.beginPath();
    ctx.ellipse(200, mouthY, mouthWidth, mouthHeight, 0, 0, Math.PI * 2);
    ctx.fill();
  } else {
    // Closed mouth — smirk based on emotion
    ctx.strokeStyle = eyeColor;
    ctx.lineWidth = 2;
    ctx.beginPath();
    if (avatarEmotion === "smug" || avatarEmotion === "sarcastic") {
      ctx.moveTo(175, mouthY);
      ctx.quadraticCurveTo(200, mouthY + 10, 225, mouthY - 3);
    } else {
      ctx.moveTo(180, mouthY);
      ctx.quadraticCurveTo(200, mouthY + 8, 220, mouthY);
    }
    ctx.stroke();
  }

  ctx.restore();

  // Blink logic
  blinkTimer++;
  if (blinkTimer > 120 + Math.random() * 180) {
    isBlinking = true;
    blinkTimer = 0;
    setTimeout(() => {
      isBlinking = false;
    }, 150);
  }

  requestAnimationFrame(drawAvatar);
}

// Start
drawAvatar();
